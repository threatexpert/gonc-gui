package main

import (
	"testing"
	"time"
)

func TestStopHTTPDownloadWaitsForCurrentDownloader(t *testing.T) {
	done := make(chan struct{})
	cancelled := make(chan struct{})
	app := &App{
		downloadID:     7,
		downloadCancel: func() { close(cancelled) },
		downloadDone:   done,
	}

	stopped := make(chan error, 1)
	go func() {
		stopped <- app.StopHTTPDownload()
	}()

	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("StopHTTPDownload did not cancel the downloader")
	}

	returnedEarly := false
	select {
	case <-stopped:
		returnedEarly = true
	case <-time.After(50 * time.Millisecond):
	}

	app.clearDownload(7, done)
	if returnedEarly {
		t.Fatal("StopHTTPDownload returned before downloader completion")
	}

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("StopHTTPDownload returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("StopHTTPDownload did not return after downloader completion")
	}
}

func TestClearDownloadPreservesNewerDownloadState(t *testing.T) {
	oldDone := make(chan struct{})
	newDone := make(chan struct{})
	newCancel := func() {}
	app := &App{
		downloadID:     8,
		downloadCancel: newCancel,
		downloadDone:   newDone,
	}

	app.clearDownload(7, oldDone)

	select {
	case <-oldDone:
	default:
		t.Fatal("stale downloader completion signal was not closed")
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.downloadID != 8 || app.downloadDone != newDone || app.downloadCancel == nil {
		t.Fatal("stale downloader completion changed newer download state")
	}
}
