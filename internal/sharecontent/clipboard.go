package sharecontent

import (
	"errors"
	"os"
)

type ClipboardKind string

const (
	ClipboardFiles ClipboardKind = "files"
	ClipboardImage ClipboardKind = "image"
	ClipboardText  ClipboardKind = "text"
)

type ClipboardResult struct {
	Paths []string
	Kind  ClipboardKind
}

var (
	ErrClipboardEmpty         = errors.New("GONC_CLIPBOARD_EMPTY")
	ErrClipboardUnsupported   = errors.New("GONC_CLIPBOARD_UNSUPPORTED")
	ErrClipboardBusy          = errors.New("GONC_CLIPBOARD_BUSY")
	ErrClipboardAccess        = errors.New("GONC_CLIPBOARD_ACCESS")
	ErrClipboardInvalidPaths  = errors.New("GONC_CLIPBOARD_INVALID_PATHS")
	ErrClipboardTemporaryFile = errors.New("GONC_CLIPBOARD_TEMPORARY_FILE")
)

func existingPaths(paths []string) []string {
	existing := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			existing = append(existing, path)
		}
	}
	return existing
}
