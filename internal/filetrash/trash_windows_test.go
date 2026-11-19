//go:build windows

package filetrash

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

func TestRecycleSinkRejectsPermanentDelete(t *testing.T) {
	sink := &recycleSink{vtable: &recycleVTable}
	hr, _, _ := syscall.SyscallN(recycleVTable[11], uintptr(unsafe.Pointer(sink)), 0, 0)
	if int32(hr) >= 0 {
		t.Fatal("permanent delete allowed")
	}
	hr, _, _ = syscall.SyscallN(recycleVTable[11], uintptr(unsafe.Pointer(sink)), 0x80, 0)
	if hr != 0 {
		t.Fatalf("recycle vetoed: %x", hr)
	}
}

func TestSystemRecycleIntegration(t *testing.T) {
	if os.Getenv("GONC_TEST_RECYCLE") != "1" {
		t.Skip("opt-in desktop Shell test")
	}
	file := filepath.Join(t.TempDir(), "gonc-recycle-test.txt")
	if err := os.WriteFile(file, []byte("disposable integration test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Move(file); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("file not recycled: %v", err)
	}
}
