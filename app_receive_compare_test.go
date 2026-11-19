package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gonc-gui/internal/httpdownload"
)

func waitCompare(t *testing.T, a *App, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s, err := a.ReceivedComparisonPage(id, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		a.mu.Lock()
		busy := a.compareCancel != nil
		a.mu.Unlock()
		if s.Phase == "ready" && !busy {
			return
		}
		if s.Phase == "error" {
			t.Fatal(s.Error)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("comparison did not complete")
}

func TestComparisonLifecycleAndDownloadInterlock(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"path":"/keep.txt","is_dir":false}`)
	}))
	defer s.Close()
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "old.txt"), []byte("old"), 0600)
	a := &App{ctx: context.Background(), receiveLocalHTTPURL: s.URL}
	id, err := a.StartReceivedComparison(root, "/", "localOnly")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if _, err := a.StartReceivedComparison(root, "/", "localOnly"); err == nil {
		t.Fatal("simultaneous scan allowed")
	}
	if err := a.StartHTTPDownload(root, "/", nil, nil, nil, true, 1); err == nil {
		t.Fatal("download during scan allowed")
	}
	if err := a.DeleteReceivedComparison(id, "permanent"); err == nil {
		t.Fatal("delete before scan completion allowed")
	}
	close(release)
	waitCompare(t, a, id)
	if err := a.SelectReceivedComparison(id, "", nil, true, true); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.receiveLocalHTTPURL = "http://another-peer.invalid"
	a.mu.Unlock()
	if err := a.DeleteReceivedComparison(id, "permanent"); err == nil {
		t.Fatal("deletion after peer change allowed")
	}
	if err := a.stopReceivedComparison(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReceivedComparisonPage(id, "", 0); err == nil {
		t.Fatal("stale results survived disconnect")
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); err != nil {
		t.Fatalf("local file changed: %v", err)
	}
}

func TestCancellingComparisonUnblocksDownload(t *testing.T) {
	entered := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer s.Close()
	a := &App{ctx: context.Background(), receiveLocalHTTPURL: s.URL}
	id, err := a.StartReceivedComparison(t.TempDir(), "/", "localOnly")
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := a.CancelReceivedComparison(id, false); err != nil {
		t.Fatal(err)
	}
	if err := a.stopReceivedComparison(); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.compareCancel != nil || a.compareDone != nil || a.compareJob != nil {
		t.Fatal("comparison resources retained")
	}
}

func TestComparisonReceivesOnlySelectedFiles(t *testing.T) {
	for _, kind := range []string{"localMissing", "sizeDifferent", "differences"} {
		t.Run(kind, func(t *testing.T) {
			remoteTime := time.Unix(1700000000, 123456700) // representable on NTFS
			files := map[string]string{"/selected.txt": "fresh remote contents", "/unselected.txt": "unselected contents", "/empty.txt": ""}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					w.Header().Set("Content-Type", "application/x-ndjson")
					encoder := json.NewEncoder(w)
					for p, content := range files {
						encoder.Encode(httpdownload.FileInfo{Path: p, Size: int64(len(content)), ModTime: remoteTime})
					}
					return
				}
				if r.URL.Query().Get("manifest") != "" {
					http.NotFound(w, r)
					return
				}
				content, ok := files[r.URL.Path]
				if !ok {
					http.NotFound(w, r)
					return
				}
				http.ServeContent(w, r, "file.txt", remoteTime, strings.NewReader(content))
			}))
			defer s.Close()
			root := t.TempDir()
			if kind != "localMissing" {
				os.WriteFile(filepath.Join(root, "selected.txt"), []byte("old"), 0600)
				os.WriteFile(filepath.Join(root, "unselected.txt"), []byte("keep"), 0600)
			}
			if kind == "differences" {
				// Same size, different contents and timestamp: must still be repaired.
				os.WriteFile(filepath.Join(root, "selected.txt"), []byte(strings.Repeat("x", len(files["/selected.txt"]))), 0600)
				if err := os.Chtimes(filepath.Join(root, "selected.txt"), remoteTime.Truncate(time.Second), remoteTime.Truncate(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			a := &App{ctx: context.Background(), receiveLocalHTTPURL: s.URL}
			id, err := a.StartReceivedComparison(root, "/", kind)
			if err != nil {
				t.Fatal(err)
			}
			waitCompare(t, a, id)
			snapshot, err := a.ReceivedComparisonPage(id, "", 0)
			if err != nil {
				t.Fatal(err)
			}
			var selected []int
			for _, item := range snapshot.Items {
				if item.Path == "/selected.txt" || item.Path == "/empty.txt" {
					selected = append(selected, item.ID)
				}
			}
			if err = a.SelectReceivedComparison(id, "", selected, false, true); err != nil {
				t.Fatal(err)
			}
			if err = a.DeleteReceivedComparison(id, "permanent"); err == nil {
				t.Fatal("wrong-purpose deletion allowed")
			}
			if err = a.ReceiveReceivedComparison(id); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(8 * time.Second)
			for {
				snapshot, err = a.ReceivedComparisonPage(id, "", 0)
				if err != nil {
					t.Fatal(err)
				}
				if snapshot.Phase == "done" {
					break
				}
				if time.Now().After(deadline) {
					a.CancelReceivedComparison(id, false)
					t.Fatal("receive timed out")
				}
				time.Sleep(5 * time.Millisecond)
			}
			if snapshot.Received != len(selected) || snapshot.Failed != 0 || snapshot.Skipped != 0 {
				t.Fatalf("result: %+v", snapshot)
			}
			content, err := os.ReadFile(filepath.Join(root, "selected.txt"))
			if err != nil || string(content) != files["/selected.txt"] {
				t.Fatalf("content=%q, error=%v", content, err)
			}
			if kind == "differences" {
				info, err := os.Stat(filepath.Join(root, "selected.txt"))
				if err != nil || !info.ModTime().Equal(remoteTime) {
					t.Fatalf("repair did not preserve sender time: %v", err)
				}
			}
			content, err = os.ReadFile(filepath.Join(root, "unselected.txt"))
			if kind == "localMissing" && !os.IsNotExist(err) || kind != "localMissing" && (err != nil || string(content) != "keep") {
				t.Fatal("unselected file changed")
			}
		})
	}
}
