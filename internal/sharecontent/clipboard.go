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
	ErrClipboardEmpty       = errors.New("clipboard is empty")
	ErrClipboardUnsupported = errors.New("native clipboard is unsupported")
	ErrClipboardBusy        = errors.New("clipboard is busy")
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
