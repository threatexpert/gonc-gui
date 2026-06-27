//go:build !windows

package main

func isAdministrator() bool {
	return false
}
