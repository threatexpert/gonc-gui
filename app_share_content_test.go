package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"gonc-gui/internal/sharecontent"
)

func TestCreateTextShareRejectsEmptyText(t *testing.T) {
	app := NewApp(nil)
	if _, err := app.CreateTextShare(""); err == nil || err.Error() != "text content is empty" {
		t.Fatalf("error = %v, want empty text error", err)
	}
}

func TestCreateTextSharePreservesWhitespace(t *testing.T) {
	app := NewApp(nil)
	t.Cleanup(func() { _ = app.shareContent.Cleanup() })

	want := "  first line\r\nsecond line\t "
	path, err := app.CreateTextShare(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestReleaseGeneratedSharePathsRemovesOwnedText(t *testing.T) {
	app := NewApp(nil)
	t.Cleanup(func() { _ = app.shareContent.Cleanup() })
	path, err := app.CreateTextShare("temporary")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ReleaseGeneratedSharePaths([]string{path}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("released file still exists: %v", err)
	}
}

func TestCleanupRemovesGeneratedContentAfterTransfersStop(t *testing.T) {
	app := NewApp(nil)
	path, err := app.CreateTextShare("temporary")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("generated file remains after cleanup: %v", err)
	}
}

func TestImportClipboardFallsBackToWailsTextOnlyWhenNativeUnsupported(t *testing.T) {
	app := NewApp(nil)
	t.Cleanup(func() { _ = app.shareContent.Cleanup() })
	app.importNativeClipboard = func() (sharecontent.ClipboardResult, error) {
		return sharecontent.ClipboardResult{}, sharecontent.ErrClipboardUnsupported
	}
	textCalls := 0
	app.clipboardGetText = func(context.Context) (string, error) {
		textCalls++
		return "  clipboard text  ", nil
	}

	result, err := app.ImportClipboard()
	if err != nil {
		t.Fatal(err)
	}
	if textCalls != 1 {
		t.Fatalf("clipboard text calls = %d, want 1", textCalls)
	}
	if result.Kind != sharecontent.ClipboardText || len(result.Paths) != 1 {
		t.Fatalf("result = %#v", result)
	}
	got, err := os.ReadFile(result.Paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "  clipboard text  " {
		t.Fatalf("content = %q", got)
	}
}

func TestImportClipboardDoesNotFallbackForNativeErrors(t *testing.T) {
	app := NewApp(nil)
	want := errors.New("native clipboard failed")
	app.importNativeClipboard = func() (sharecontent.ClipboardResult, error) {
		return sharecontent.ClipboardResult{}, want
	}
	app.clipboardGetText = func(context.Context) (string, error) {
		t.Fatal("Wails text fallback was called")
		return "", nil
	}

	_, err := app.ImportClipboard()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestImportClipboardEmptyFallbackRemainsUnsupported(t *testing.T) {
	app := NewApp(nil)
	app.importNativeClipboard = func() (sharecontent.ClipboardResult, error) {
		return sharecontent.ClipboardResult{}, sharecontent.ErrClipboardUnsupported
	}
	app.clipboardGetText = func(context.Context) (string, error) { return "", nil }

	_, err := app.ImportClipboard()
	if !errors.Is(err, sharecontent.ErrClipboardUnsupported) {
		t.Fatalf("error = %v, want unsupported", err)
	}
}

func TestUpdateSharePathsAllowsEmptySliceToReachRunner(t *testing.T) {
	app := NewApp(nil)
	err := app.UpdateSharePaths(nil)
	if err == nil || err.Error() != "no share session is running" {
		t.Fatalf("error = %v, want runner not-running error", err)
	}
}
