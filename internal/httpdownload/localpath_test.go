package httpdownload

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLocalPathUsesNormalizedRemotePath(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveLocalPath(root, FileInfo{Name: "report.txt", Path: "/docs/report.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "docs", "report.txt"); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveLocalPathRejectsEscape(t *testing.T) {
	_, err := ResolveLocalPath(t.TempDir(), FileInfo{Name: "x", Path: "../../outside.exe"})
	if err == nil || !strings.Contains(err.Error(), "escapes save directory") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveLocalPathSanitizesRootFilename(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveLocalPath(root, FileInfo{Name: "../report.txt", Path: "/"})
	if err != nil || got != filepath.Join(root, "report.txt") {
		t.Fatalf("path=%q error=%v", got, err)
	}
}
