//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

func platformDownloadsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}
