//go:build windows

package main

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPlatformDownloadsDirUsesWindowsKnownFolder(t *testing.T) {
	want, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, 0)
	if err != nil {
		t.Skipf("Windows Downloads known folder is unavailable: %v", err)
	}
	got, err := platformDownloadsDir()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("platform Downloads dir = %q, want Known Folder path %q", got, want)
	}
}
