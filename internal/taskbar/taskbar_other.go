//go:build !windows

package taskbar

func SetProgress(done, total uint64) {}

func Clear() {}
