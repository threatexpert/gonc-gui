//go:build windows

package main

import "golang.org/x/sys/windows"

func platformDownloadsDir() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_Downloads, 0)
}
