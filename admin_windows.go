//go:build windows

package main

import "golang.org/x/sys/windows"

func isAdministrator() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
