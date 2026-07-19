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
	if err := (&App{}).RevealReceivedFile(root, file); !errors.Is(err, receivedfile.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}
