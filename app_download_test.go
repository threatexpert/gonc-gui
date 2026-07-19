package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gonc-gui/internal/httpdownload"
)

func TestClientTaskDownloadSinkTagsEveryEvent(t *testing.T) {
	input := []httpdownload.Event{
		{Type: "status", Level: "info", Message: "starting"},
		{Type: "progress", DoneBytes: 7, TotalBytes: 9},
		{Type: "log", Level: "warn", Message: "retrying"},
	}
	var received []httpdownload.Event
	sink := clientTaskDownloadSink(42, func(event httpdownload.Event) {
		received = append(received, event)
	})
	for _, event := range input {
		sink(event)
	}
	if len(received) != len(input) {
		t.Fatalf("received %d events, want %d", len(received), len(input))
	}
	for i, event := range received {
		if event.ClientTaskID != 42 {
			t.Fatalf("event %d client task ID = %d, want 42", i, event.ClientTaskID)
		}
		if event.Type != input[i].Type || event.Message != input[i].Message {
			t.Fatalf("event %d payload changed: %#v", i, event)
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"clientTaskId":42`) {
			t.Fatalf("event %d JSON missing clientTaskId: %s", i, encoded)
		}
	}
}

func TestStartHTTPDownloadRejectsMissingClientTaskID(t *testing.T) {
	err := (&App{}).StartHTTPDownload("", "/", nil, nil, nil, true, 0)
	if err == nil || !strings.Contains(err.Error(), "client task ID") {
		t.Fatalf("error = %v, want missing client task ID", err)
	}
}

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
