package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gonc-gui/internal/httpdownload"
	"gonc-gui/internal/receivedfile"
)

func TestReceivedFileAPIsCheckAndRevalidate(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "ready.txt")
	if err := os.WriteFile(target, []byte("ready"), 0644); err != nil {
		t.Fatal(err)
	}
	file := httpdownload.FileInfo{Path: "/ready.txt", Size: 5}

	states := (&App{}).CheckReceivedFiles(root, []httpdownload.FileInfo{file})
	if len(states) != 1 || states[0].RemotePath != file.Path || !states[0].Available {
		t.Fatalf("states = %+v", states)
	}

	if err := os.WriteFile(target, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	result := (&App{}).RevealReceivedFile(root, file)
	if !result.Unavailable || result.Error == "" {
		t.Fatalf("result = %+v, want unavailable error", result)
	}
}

func TestClassifyRevealErrorDistinguishesUnavailableFromLaunchFailure(t *testing.T) {
	unavailable := classifyRevealError(receivedfile.ErrUnavailable)
	if !unavailable.Unavailable || unavailable.Error == "" {
		t.Fatalf("unavailable result = %+v", unavailable)
	}

	launch := classifyRevealError(errors.New("file manager failed to start"))
	if launch.Unavailable || launch.Error == "" {
		t.Fatalf("launch result = %+v", launch)
	}
}
