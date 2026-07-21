//go:build windows

package sharecontent

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/image/bmp"
	"golang.org/x/sys/windows"
)

const (
	cfDIB         = 8
	cfUnicodeText = 13
	cfHDrop       = 15
	cfDIBV5       = 17

	maxClipboardBytes = 512 << 20
	maxDroppedFiles   = 1 << 16
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	shell32               = windows.NewLazySystemDLL("shell32.dll")
	procOpenClipboard     = user32.NewProc("OpenClipboard")
	procCloseClipboard    = user32.NewProc("CloseClipboard")
	procIsFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	procGetClipboardData  = user32.NewProc("GetClipboardData")
	procRegisterFormatW   = user32.NewProc("RegisterClipboardFormatW")
	procGlobalLock        = kernel32.NewProc("GlobalLock")
	procGlobalSize        = kernel32.NewProc("GlobalSize")
	procGlobalUnlock      = kernel32.NewProc("GlobalUnlock")
	procDragQueryFileW    = shell32.NewProc("DragQueryFileW")
)

type clipboardFormats struct {
	files []string
	png   []byte
	dibV5 []byte
	dib   []byte
	text  string
}

type clipboardPayload struct {
	kind ClipboardKind
}

func chooseClipboardPayload(formats clipboardFormats) clipboardPayload {
	if len(formats.files) != 0 {
		return clipboardPayload{kind: ClipboardFiles}
	}
	if len(formats.png) != 0 || len(formats.dibV5) != 0 || len(formats.dib) != 0 {
		return clipboardPayload{kind: ClipboardImage}
	}
	if formats.text != "" {
		return clipboardPayload{kind: ClipboardText}
	}
	return clipboardPayload{}
}

func (m *Manager) ImportNativeClipboard() (ClipboardResult, error) {
	if err := openClipboardWithRetry(); err != nil {
		return ClipboardResult{}, err
	}
	defer procCloseClipboard.Call()

	if paths, ok, err := readHDrop(); ok || err != nil {
		if err != nil {
			return ClipboardResult{}, err
		}
		paths = existingPaths(paths)
		if len(paths) > 0 {
			return ClipboardResult{Paths: paths, Kind: ClipboardFiles}, nil
		}
	}
	if img, ok, err := readClipboardImage(); ok || err != nil {
		if err != nil {
			return ClipboardResult{}, err
		}
		path, err := m.CreatePNG("clipboard-image", img)
		return ClipboardResult{Paths: []string{path}, Kind: ClipboardImage}, err
	}
	if text, ok, err := readUnicodeText(); ok || err != nil {
		if err != nil {
			return ClipboardResult{}, err
		}
		if text != "" {
			path, err := m.CreateText("clipboard-text", text)
			return ClipboardResult{Paths: []string{path}, Kind: ClipboardText}, err
		}
	}
	return ClipboardResult{}, ErrClipboardEmpty
}

func openClipboardWithRetry() error {
	for attempt := 0; attempt < 5; attempt++ {
		if opened, _, _ := procOpenClipboard.Call(0); opened != 0 {
			return nil
		}
		if attempt < 4 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return ErrClipboardBusy
}

func readHDrop() ([]string, bool, error) {
	if !clipboardFormatAvailable(cfHDrop) {
		return nil, false, nil
	}
	handle, _, callErr := procGetClipboardData.Call(cfHDrop)
	if handle == 0 {
		return nil, true, win32Error("GetClipboardData(CF_HDROP)", callErr)
	}

	count, _, _ := procDragQueryFileW.Call(handle, ^uintptr(0), 0, 0)
	if count > maxDroppedFiles {
		return nil, true, fmt.Errorf("clipboard contains too many dropped files: %d", count)
	}
	paths := make([]string, 0, count)
	for index := uintptr(0); index < count; index++ {
		length, _, _ := procDragQueryFileW.Call(handle, index, 0, 0)
		if length == 0 || length > windows.MAX_PATH*128 {
			return nil, true, fmt.Errorf("invalid dropped file path length: %d", length)
		}
		buffer := make([]uint16, length+1)
		written, _, _ := procDragQueryFileW.Call(handle, index, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		runtime.KeepAlive(buffer)
		if written != length {
			return nil, true, fmt.Errorf("dropped file path length changed from %d to %d", length, written)
		}
		paths = append(paths, windows.UTF16ToString(buffer))
	}
	return paths, true, nil
}

func readClipboardImage() (image.Image, bool, error) {
	var firstErr error
	if pngName, err := windows.UTF16PtrFromString("PNG"); err == nil {
		pngFormat, _, _ := procRegisterFormatW.Call(uintptr(unsafe.Pointer(pngName)))
		runtime.KeepAlive(pngName)
		if pngFormat != 0 {
			if data, ok, err := readClipboardMemory(uint32(pngFormat)); ok {
				if err == nil {
					img, decodeErr := png.Decode(bytes.NewReader(data))
					if decodeErr == nil {
						return img, true, nil
					}
					err = fmt.Errorf("decode clipboard PNG: %w", decodeErr)
				}
				firstErr = err
			}
		}
	}

	for _, format := range []uint32{cfDIBV5, cfDIB} {
		data, ok, err := readClipboardMemory(format)
		if !ok {
			continue
		}
		if err == nil {
			var img image.Image
			img, err = decodeDIB(data)
			if err == nil {
				return img, true, nil
			}
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("decode clipboard DIB format %d: %w", format, err)
		}
	}
	if firstErr != nil {
		return nil, true, firstErr
	}
	return nil, false, nil
}

func readUnicodeText() (string, bool, error) {
	data, ok, err := readClipboardMemory(cfUnicodeText)
	if !ok || err != nil {
		return "", ok, err
	}
	if len(data)%2 != 0 {
		return "", true, fmt.Errorf("clipboard Unicode text has odd byte length %d", len(data))
	}
	text := make([]uint16, len(data)/2)
	for index := range text {
		text[index] = binary.LittleEndian.Uint16(data[index*2:])
		if text[index] == 0 {
			text = text[:index]
			break
		}
	}
	return windows.UTF16ToString(text), true, nil
}

func clipboardFormatAvailable(format uint32) bool {
	available, _, _ := procIsFormatAvailable.Call(uintptr(format))
	return available != 0
}

func readClipboardMemory(format uint32) ([]byte, bool, error) {
	if !clipboardFormatAvailable(format) {
		return nil, false, nil
	}
	handle, _, callErr := procGetClipboardData.Call(uintptr(format))
	if handle == 0 {
		return nil, true, win32Error("GetClipboardData", callErr)
	}
	size, _, callErr := procGlobalSize.Call(handle)
	if size == 0 {
		return nil, true, win32Error("GlobalSize", callErr)
	}
	if size > maxClipboardBytes {
		return nil, true, fmt.Errorf("clipboard format %d is too large: %d bytes", format, size)
	}
	pointer, _, callErr := procGlobalLock.Call(handle)
	if pointer == 0 {
		return nil, true, win32Error("GlobalLock", callErr)
	}
	defer procGlobalUnlock.Call(handle)

	data := make([]byte, int(size))
	var bytesRead uintptr
	if err := windows.ReadProcessMemory(windows.CurrentProcess(), pointer, &data[0], size, &bytesRead); err != nil {
		return nil, true, fmt.Errorf("copy clipboard memory: %w", err)
	}
	if bytesRead != size {
		return nil, true, fmt.Errorf("copied %d of %d clipboard bytes", bytesRead, size)
	}
	runtime.KeepAlive(handle)
	return data, true, nil
}

func decodeDIB(data []byte) (image.Image, error) {
	if len(data) < 40 {
		return nil, fmt.Errorf("DIB header is truncated: %d bytes", len(data))
	}
	headerSize := binary.LittleEndian.Uint32(data[0:4])
	if headerSize != 40 && headerSize != 108 && headerSize != 124 {
		return nil, fmt.Errorf("unsupported DIB header size %d", headerSize)
	}
	if uint64(headerSize) > uint64(len(data)) {
		return nil, fmt.Errorf("DIB header size %d exceeds buffer size %d", headerSize, len(data))
	}
	width := int64(int32(binary.LittleEndian.Uint32(data[4:8])))
	height := int64(int32(binary.LittleEndian.Uint32(data[8:12])))
	if width <= 0 || height == 0 || height == -1<<31 {
		return nil, fmt.Errorf("invalid DIB dimensions %dx%d", width, height)
	}
	if height < 0 {
		height = -height
	}
	planes := binary.LittleEndian.Uint16(data[12:14])
	depth := binary.LittleEndian.Uint16(data[14:16])
	compression := binary.LittleEndian.Uint32(data[16:20])
	if planes != 1 {
		return nil, fmt.Errorf("invalid DIB plane count %d", planes)
	}
	if depth != 8 && depth != 24 && depth != 32 {
		return nil, fmt.Errorf("unsupported DIB depth %d", depth)
	}
	if compression != 0 {
		defaultMasks := headerSize > 40 && binary.LittleEndian.Uint32(data[40:44]) == 0xff0000 &&
			binary.LittleEndian.Uint32(data[44:48]) == 0xff00 &&
			binary.LittleEndian.Uint32(data[48:52]) == 0xff &&
			binary.LittleEndian.Uint32(data[52:56]) == 0xff000000
		if compression != 3 || !defaultMasks {
			return nil, fmt.Errorf("unsupported DIB compression %d", compression)
		}
	}

	paletteEntries := uint64(0)
	if depth == 8 {
		paletteEntries = uint64(binary.LittleEndian.Uint32(data[32:36]))
		if paletteEntries == 0 {
			paletteEntries = 256
		}
		if paletteEntries > 256 {
			return nil, fmt.Errorf("invalid DIB palette size %d", paletteEntries)
		}
	}
	pixelOffset := uint64(headerSize) + paletteEntries*4
	rowBytes := (uint64(width)*uint64(depth) + 31) / 32 * 4
	pixelBytes := rowBytes * uint64(height)
	if pixelOffset > uint64(len(data)) || pixelBytes > uint64(len(data))-pixelOffset {
		return nil, fmt.Errorf("DIB pixel data exceeds buffer bounds")
	}
	if uint64(len(data)) > uint64(^uint32(0))-14 {
		return nil, fmt.Errorf("DIB is too large: %d bytes", len(data))
	}

	bmpData := make([]byte, 14+len(data))
	copy(bmpData[0:2], "BM")
	binary.LittleEndian.PutUint32(bmpData[2:6], uint32(len(bmpData)))
	binary.LittleEndian.PutUint32(bmpData[10:14], uint32(14+pixelOffset))
	copy(bmpData[14:], data)
	img, err := bmp.Decode(bytes.NewReader(bmpData))
	if err != nil {
		return nil, fmt.Errorf("decode DIB as BMP: %w", err)
	}
	return img, nil
}

func win32Error(operation string, err error) error {
	if err == nil || err == windows.ERROR_SUCCESS {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
