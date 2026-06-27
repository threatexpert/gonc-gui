//go:build windows

package taskbar

import (
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

type command struct {
	state int
	done  uint32
	total uint32
}

var (
	once     sync.Once
	commands chan command
)

func SetProgress(done, total uint64) {
	if total == 0 || done >= total {
		Clear()
		return
	}
	if total > uint64(^uint32(0)) {
		done = done * 10000 / total
		total = 10000
	}
	enqueue(command{state: win.TBPF_NORMAL, done: uint32(done), total: uint32(total)})
}

func Clear() {
	enqueue(command{state: win.TBPF_NOPROGRESS})
}

func enqueue(cmd command) {
	once.Do(func() {
		commands = make(chan command, 16)
		go taskbarWorker(commands)
	})
	select {
	case commands <- cmd:
	default:
		select {
		case <-commands:
		default:
		}
		commands <- cmd
	}
}

func taskbarWorker(ch <-chan command) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hr := win.CoInitializeEx(nil, win.COINIT_APARTMENTTHREADED)
	coInitialized := win.SUCCEEDED(hr)
	if coInitialized {
		defer win.CoUninitialize()
	}

	var ptr unsafe.Pointer
	if win.FAILED(win.CoCreateInstance(&win.CLSID_TaskbarList, nil, win.CLSCTX_INPROC_SERVER, &win.IID_ITaskbarList3, &ptr)) || ptr == nil {
		for range ch {
		}
		return
	}
	taskbar := (*win.ITaskbarList3)(ptr)
	defer releaseTaskbar(taskbar)

	hwnd := findMainWindow()
	for cmd := range ch {
		if hwnd == 0 {
			hwnd = findMainWindow()
		}
		if hwnd == 0 {
			continue
		}
		if cmd.state == win.TBPF_NORMAL {
			taskbar.SetProgressValue(hwnd, cmd.done, cmd.total)
		}
		taskbar.SetProgressState(hwnd, cmd.state)
	}
}

func releaseTaskbar(taskbar *win.ITaskbarList3) {
	if taskbar == nil || taskbar.LpVtbl == nil || taskbar.LpVtbl.Release == 0 {
		return
	}
	syscall.Syscall(taskbar.LpVtbl.Release, 1, uintptr(unsafe.Pointer(taskbar)), 0, 0)
}

func findMainWindow() win.HWND {
	user32 := windows.NewLazySystemDLL("user32.dll")
	enumWindows := user32.NewProc("EnumWindows")
	getWindowThreadProcessID := user32.NewProc("GetWindowThreadProcessId")
	isWindowVisible := user32.NewProc("IsWindowVisible")
	getWindow := user32.NewProc("GetWindow")

	pid := uintptr(os.Getpid())
	var found win.HWND
	cb := windows.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		if found != 0 {
			return 0
		}
		visible, _, _ := isWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		owner, _, _ := getWindow.Call(hwnd, uintptr(win.GW_OWNER))
		if owner != 0 {
			return 1
		}
		var windowPID uint32
		getWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&windowPID)))
		if uintptr(windowPID) != pid {
			return 1
		}
		found = win.HWND(hwnd)
		return 0
	})
	enumWindows.Call(cb, 0)
	return found
}
