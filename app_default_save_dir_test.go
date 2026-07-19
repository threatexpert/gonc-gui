package main

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestDefaultSaveDirUsesPlatformDownloadsDirectory(t *testing.T) {
	configuredDownloads := `D:\Relocated Downloads`
	got := defaultSaveDirFrom(
		func() (string, error) { return configuredDownloads, nil },
		func() (string, error) { return `C:\Users\tester`, nil },
		func() (string, error) { return `C:\work`, nil },
	)
	want := filepath.Join(configuredDownloads, "GoncTransfer")
	if got != want {
		t.Fatalf("default save dir = %q, want relocated Downloads path %q", got, want)
	}
}

func TestDefaultSaveDirFallsBackWhenPlatformLookupFails(t *testing.T) {
	home := `C:\Users\tester`
	got := defaultSaveDirFrom(
		func() (string, error) { return "", errors.New("known folder unavailable") },
		func() (string, error) { return home, nil },
		func() (string, error) { return `C:\work`, nil },
	)
	want := filepath.Join(home, "Downloads", "GoncTransfer")
	if got != want {
		t.Fatalf("fallback save dir = %q, want %q", got, want)
	}
}
