package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"gonc-gui/internal/filetrash"
	"gonc-gui/internal/httpdownload"
	"gonc-gui/internal/receivecompare"
)

func (a *App) StartReceivedComparison(saveDir, scope, kind string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.receiveLocalHTTPURL == "" {
		return "", errors.New("connect to the sender first")
	}
	if a.downloadCancel != nil || a.compareCancel != nil {
		return "", errors.New("wait for the current download or comparison task")
	}
	mode := "permanent"
	if filetrash.Available() {
		mode = "trash"
	}
	id := fmt.Sprint(a.compareSequence + 1)
	job, err := receivecompare.New(id, saveDir, scope, mode, kind)
	if err != nil {
		return "", err
	}
	a.compareSequence++
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	a.compareJob = job
	a.compareCancel = cancel
	a.compareDone = done
	a.compareURL = a.receiveLocalHTTPURL
	serverURL := a.compareURL
	go func() {
		defer a.finishComparison(done, cancel)
		// A long tree scan must not inherit RemoteFiles' 30-second UI timeout.
		job.Scan(ctx, func(ctx context.Context, visit func(httpdownload.FileInfo) error) error {
			return httpdownload.ListComparisonScope(ctx, serverURL, job.Scope(), job.Root(), visit)
		})
	}()
	return id, nil
}

func (a *App) finishComparison(done chan struct{}, cancel context.CancelFunc) {
	cancel()
	a.mu.Lock()
	if a.compareDone == done {
		a.compareCancel = nil
		a.compareDone = nil
	}
	close(done)
	a.mu.Unlock()
}

func (a *App) comparison(id string) (*receivecompare.Job, error) {
	if a.compareJob == nil || id != fmt.Sprint(a.compareSequence) {
		return nil, errors.New("comparison expired; compare again")
	}
	return a.compareJob, nil
}

func (a *App) ReceivedComparisonPage(id, query string, page int) (receivecompare.Snapshot, error) {
	a.mu.Lock()
	job, err := a.comparison(id)
	a.mu.Unlock()
	if err != nil {
		return receivecompare.Snapshot{}, err
	}
	return job.Page(query, page), nil
}

func (a *App) SelectReceivedComparison(id, query string, ids []int, all, selected bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	job, err := a.comparison(id)
	if err != nil {
		return err
	}
	return job.Select(query, ids, all, selected)
}

func (a *App) DeleteReceivedComparison(id, mode string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	job, err := a.comparison(id)
	if err != nil {
		return err
	}
	if a.downloadCancel != nil || a.compareCancel != nil {
		return errors.New("wait for the current download or comparison task")
	}
	if a.receiveLocalHTTPURL == "" || a.receiveLocalHTTPURL != a.compareURL {
		return errors.New("sender connection changed; compare again")
	}
	if mode == "trash" && !filetrash.Available() {
		return errors.New("system trash is unavailable; explicitly select permanent deletion")
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, err := job.BeginDelete(parent, mode)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	a.compareCancel = job.Cancel
	a.compareDone = done
	serverURL := a.compareURL
	remove := os.Remove
	if mode == "trash" {
		remove = filetrash.Move
	}
	go func() {
		defer a.finishComparison(done, func() {})
		job.Delete(ctx, func(ctx context.Context, p string) (bool, error) {
			return httpdownload.RemotePathMissing(ctx, serverURL, p)
		}, remove)
	}()
	return nil
}

func (a *App) CancelReceivedComparison(id string, discard bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.comparison(id); err != nil {
		return err
	}
	if a.compareCancel != nil {
		a.compareCancel()
	}
	if discard {
		a.compareJob = nil
		a.compareURL = ""
	}
	return nil
}

func (a *App) ReceiveReceivedComparison(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	job, err := a.comparison(id)
	if err != nil {
		return err
	}
	if a.downloadCancel != nil || a.compareCancel != nil {
		return errors.New("wait for the current download or comparison task")
	}
	if a.receiveLocalHTTPURL == "" || a.receiveLocalHTTPURL != a.compareURL {
		return errors.New("sender connection changed; compare again")
	}
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, err := job.BeginReceive(parent)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	a.compareCancel = job.Cancel
	a.compareDone = done
	serverURL := a.compareURL
	go func() {
		defer a.finishComparison(done, func() {})
		job.Receive(ctx, func(ctx context.Context, file httpdownload.FileInfo) (httpdownload.FileInfo, error) {
			checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return httpdownload.RefreshComparisonFile(checkCtx, serverURL, file)
		}, func(ctx context.Context, root string, file httpdownload.FileInfo) error {
			downloader, err := httpdownload.New(httpdownload.Config{ServerURL: serverURL, SaveDir: root, CachedFiles: []httpdownload.FileInfo{file}, Concurrency: 1, Resume: true, MaxAttempts: 3})
			if err != nil {
				return err
			}
			return downloader.Start(ctx, func(event httpdownload.Event) { job.RecordReceiveProgress(file.Path, event) })
		})
	}()
	return nil
}

// Stop/reconnect invalidates the old result set and waits for in-flight file
// actions before a new peer or download can reuse the destination.
func (a *App) stopReceivedComparison() error {
	a.mu.Lock()
	// Close the gate before waiting: a new scan must not start against the peer
	// that StopTransfer/StartTransfer is in the process of replacing.
	a.receiveLocalHTTPURL = ""
	if a.compareCancel != nil {
		a.compareCancel()
	}
	done := a.compareDone
	a.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			return errors.New("comparison is still stopping")
		}
	}
	a.mu.Lock()
	a.compareJob = nil
	a.compareURL = ""
	a.mu.Unlock()
	return nil
}
