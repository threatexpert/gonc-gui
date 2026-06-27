//go:build windows

package vpnprofile

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

func protect(data []byte) ([]byte, error) {
	in := dataBlob(data)
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, 0, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobBytes(out), nil
}

func unprotect(data []byte) ([]byte, error) {
	in := dataBlob(data)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, 0, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobBytes(out), nil
}

func dataBlob(data []byte) windows.DataBlob {
	if len(data) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{
		Size: uint32(len(data)),
		Data: &data[0],
	}
}

func blobBytes(blob windows.DataBlob) []byte {
	if blob.Size == 0 || blob.Data == nil {
		return nil
	}
	data := unsafe.Slice(blob.Data, blob.Size)
	out := make([]byte, len(data))
	copy(out, data)
	return out
}
