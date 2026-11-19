//go:build windows

package filetrash

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	ole "github.com/go-ole/go-ole"
	"golang.org/x/sys/windows"
)

func Available() bool { return true }

var shellCreateItem = windows.NewLazySystemDLL("shell32.dll").NewProc("SHCreateItemFromParsingName")
var fileOperationID = ole.NewGUID("{3AD05575-8857-4850-9277-11B85BDB8E09}")
var fileOperationIID = ole.NewGUID("{947AAB5F-0A5C-4C13-B4D6-4BF7836FC9F8}")
var shellItemIID = ole.NewGUID("{43826D1E-E718-42EE-BC55-A1E261C37BFE}")
var progressSinkIID = ole.NewGUID("{04B0F1A7-9490-44BC-96E1-4296A31252E2}")

type recycleSink struct {
	vtable    *[19]uintptr
	result    uintptr
	completed bool
}

// PreDeleteItem is invoked again if the Shell changes the operation. Reject
// any non-recycling operation, even if the user disabled the Recycle Bin.
var recycleVTable = [19]uintptr{
	syscall.NewCallback(func(this *recycleSink, id *ole.GUID, out **recycleSink) uintptr {
		if *id != *ole.IID_IUnknown && *id != *progressSinkIID {
			return 0x80004002
		}
		*out = this
		return 0
	}),
	syscall.NewCallback(func(uintptr) uintptr { return 1 }),
	syscall.NewCallback(func(uintptr) uintptr { return 1 }),
	syscall.NewCallback(func(uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(this *recycleSink, flags, item uintptr) uintptr {
		if flags&0x80 == 0 {
			this.result = 0x80004004
			return 0x80004004
		}
		return 0
	}),
	syscall.NewCallback(func(this *recycleSink, flags, item, hr, newItem uintptr) uintptr {
		this.result = hr
		this.completed = true
		return 0
	}),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr) uintptr { return 0 }),
}

func hresult(hr uintptr) error {
	if int32(hr) < 0 {
		return fmt.Errorf("recycle operation failed (HRESULT 0x%08X)", uint32(hr))
	}
	return nil
}

func invoke(obj *ole.IUnknown, slot int, args ...uintptr) error {
	vtable := (*[23]uintptr)(unsafe.Pointer(obj.RawVTable))
	params := append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)
	hr, _, _ := syscall.SyscallN(vtable[slot], params...)
	runtime.KeepAlive(obj)
	return hresult(hr)
}

// Use IFileOperation rather than SHFileOperation's best-effort ALLOWUNDO.
// WANTNUKEWARNING prevents the Shell silently substituting permanent deletion
// when a volume/item cannot be recycled; any such action requires Shell consent.
// Flags: SILENT | NOCONFIRMATION | NOERRORUI | WANTNUKEWARNING |
// NO_CONNECTED_ELEMENTS | RECYCLEONDELETE | EARLYFAILURE | ADDUNDORECORD.
func Move(target string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	if err != nil {
		if e, ok := err.(*ole.OleError); !ok || e.Code() != 1 {
			return err
		}
	}
	defer ole.CoUninitialize()
	op, err := ole.CreateInstance(fileOperationID, fileOperationIID)
	if err != nil {
		return err
	}
	defer op.Release()
	if err = invoke(op, 5, 0x4|0x10|0x400|0x4000|0x2000|0x80000|0x100000|0x20000000); err != nil {
		return err
	}
	name, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	var item *ole.IUnknown
	hr, _, _ := shellCreateItem.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(shellItemIID)), uintptr(unsafe.Pointer(&item)))
	runtime.KeepAlive(name)
	if err = hresult(hr); err != nil {
		return err
	}
	defer item.Release()
	sink := &recycleSink{vtable: &recycleVTable}
	if err = invoke(op, 18, uintptr(unsafe.Pointer(item)), uintptr(unsafe.Pointer(sink))); err != nil {
		return err
	}
	performErr := invoke(op, 21)
	var aborted int32
	abortErr := invoke(op, 22, uintptr(unsafe.Pointer(&aborted)))
	runtime.KeepAlive(sink)
	if performErr != nil {
		return performErr
	}
	if abortErr != nil {
		return abortErr
	}
	if aborted != 0 {
		return fmt.Errorf("recycle operation cancelled or unavailable; file was not recycled")
	}
	if err = hresult(sink.result); err != nil {
		return err
	}
	if !sink.completed {
		return fmt.Errorf("system trash did not complete the operation")
	}
	return nil
}
