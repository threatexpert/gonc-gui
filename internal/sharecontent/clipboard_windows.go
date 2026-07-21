//go:build windows

package sharecontent

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/bits"
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

type clipboardBackend interface {
	open() error
	close()
	readFiles() ([]string, bool, error)
	readImage() (image.Image, bool, error)
	readText() (string, bool, error)
}

type win32ClipboardBackend struct{}

func (win32ClipboardBackend) open() error { return openClipboardWithRetry() }
func (win32ClipboardBackend) close()      { procCloseClipboard.Call() }
func (win32ClipboardBackend) readFiles() ([]string, bool, error) {
	return readHDrop()
}
func (win32ClipboardBackend) readImage() (image.Image, bool, error) {
	return readClipboardImage()
}
func (win32ClipboardBackend) readText() (string, bool, error) {
	return readUnicodeText()
}

func (m *Manager) ImportNativeClipboard() (ClipboardResult, error) {
	return m.importNativeClipboard(win32ClipboardBackend{})
}

func (m *Manager) importNativeClipboard(backend clipboardBackend) (ClipboardResult, error) {
	if err := backend.open(); err != nil {
		if errors.Is(err, ErrClipboardBusy) {
			return ClipboardResult{}, err
		}
		return ClipboardResult{}, fmt.Errorf("%w: %v", ErrClipboardAccess, err)
	}
	closed := false
	closeClipboard := func() {
		if !closed {
			backend.close()
			closed = true
		}
	}
	defer closeClipboard()

	invalidPaths := false
	if paths, ok, err := backend.readFiles(); ok || err != nil {
		if err != nil {
			return ClipboardResult{}, fmt.Errorf("%w: %v", ErrClipboardAccess, err)
		}
		originalCount := len(paths)
		paths = existingPaths(paths)
		if len(paths) > 0 {
			return ClipboardResult{Paths: paths, Kind: ClipboardFiles}, nil
		}
		invalidPaths = originalCount > 0
	}
	if img, ok, err := backend.readImage(); ok || err != nil {
		if err != nil {
			return ClipboardResult{}, fmt.Errorf("%w: %v", ErrClipboardAccess, err)
		}
		closeClipboard()
		path, err := m.CreatePNG("clipboard-image", img)
		if err != nil {
			return ClipboardResult{}, fmt.Errorf("%w: %v", ErrClipboardTemporaryFile, err)
		}
		return ClipboardResult{Paths: []string{path}, Kind: ClipboardImage}, nil
	}
	if text, ok, err := backend.readText(); ok || err != nil {
		if err != nil {
			return ClipboardResult{}, fmt.Errorf("%w: %v", ErrClipboardAccess, err)
		}
		if text != "" {
			closeClipboard()
			path, err := m.CreateText("clipboard-text", text)
			if err != nil {
				return ClipboardResult{}, fmt.Errorf("%w: %v", ErrClipboardTemporaryFile, err)
			}
			return ClipboardResult{Paths: []string{path}, Kind: ClipboardText}, nil
		}
	}
	if invalidPaths {
		return ClipboardResult{}, ErrClipboardInvalidPaths
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
	text, err := decodeUnicodeText(data)
	return text, true, err
}

func decodeUnicodeText(data []byte) (string, error) {
	if len(data)%2 != 0 {
		return "", fmt.Errorf("clipboard Unicode text has odd byte length %d", len(data))
	}
	text := make([]uint16, len(data)/2)
	for index := range text {
		text[index] = binary.LittleEndian.Uint16(data[index*2:])
		if text[index] == 0 {
			text = text[:index]
			break
		}
	}
	return windows.UTF16ToString(text), nil
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
	topDown := height < 0
	if height < 0 {
		height = -height
	}
	planes := binary.LittleEndian.Uint16(data[12:14])
	depth := binary.LittleEndian.Uint16(data[14:16])
	compression := binary.LittleEndian.Uint32(data[16:20])
	if planes != 1 {
		return nil, fmt.Errorf("invalid DIB plane count %d", planes)
	}
	bitfields := compression == 3 && (depth == 16 || depth == 32)
	if depth != 8 && depth != 24 && depth != 32 && !(bitfields && depth == 16) {
		return nil, fmt.Errorf("unsupported DIB depth %d", depth)
	}
	if compression != 0 && !bitfields {
		return nil, fmt.Errorf("unsupported DIB compression %d", compression)
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
	var masks [4]uint32
	if bitfields {
		maskEnd := 56
		if headerSize == 40 {
			maskEnd = 52
		}
		if len(data) < maskEnd {
			return nil, fmt.Errorf("DIB bit masks are truncated")
		}
		for index := 0; index < 3; index++ {
			masks[index] = binary.LittleEndian.Uint32(data[40+index*4 : 44+index*4])
		}
		if headerSize > 40 {
			masks[3] = binary.LittleEndian.Uint32(data[52:56])
		}
		if err := validateBitfieldMasks(depth, masks); err != nil {
			return nil, err
		}
		if headerSize == 40 {
			pixelOffset += 12
		}
	}
	rowBytes := (uint64(width)*uint64(depth) + 31) / 32 * 4
	pixelBytes := rowBytes * uint64(height)
	if pixelOffset > uint64(len(data)) || pixelBytes > uint64(len(data))-pixelOffset {
		return nil, fmt.Errorf("DIB pixel data exceeds buffer bounds")
	}
	if uint64(len(data)) > uint64(^uint32(0))-14 {
		return nil, fmt.Errorf("DIB is too large: %d bytes", len(data))
	}
	if bitfields {
		return decodeBitfields(data[pixelOffset:pixelOffset+pixelBytes], int(width), int(height), topDown, depth, [3]uint32{masks[0], masks[1], masks[2]}, int(rowBytes))
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

func validateBitfieldMasks(depth uint16, masks [4]uint32) error {
	var used uint32
	for index, mask := range masks {
		if index == 3 && mask == 0 {
			continue
		}
		if mask == 0 || mask&used != 0 {
			return fmt.Errorf("invalid overlapping or empty DIB bit masks %#x", masks)
		}
		shift := bits.TrailingZeros32(mask)
		normalized := mask >> shift
		if normalized&(normalized+1) != 0 || (depth < 32 && mask>>depth != 0) {
			return fmt.Errorf("invalid non-contiguous DIB bit mask %#x", mask)
		}
		used |= mask
	}
	return nil
}

func decodeBitfields(data []byte, width, height int, topDown bool, depth uint16, masks [3]uint32, rowBytes int) (image.Image, error) {
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	bytesPerPixel := int(depth / 8)
	for sourceY := 0; sourceY < height; sourceY++ {
		destinationY := height - 1 - sourceY
		if topDown {
			destinationY = sourceY
		}
		for x := 0; x < width; x++ {
			offset := sourceY*rowBytes + x*bytesPerPixel
			var packed uint32
			if depth == 16 {
				packed = uint32(binary.LittleEndian.Uint16(data[offset : offset+2]))
			} else {
				packed = binary.LittleEndian.Uint32(data[offset : offset+4])
			}
			result.SetRGBA(x, destinationY, color.RGBA{
				R: bitfieldChannel(packed, masks[0]),
				G: bitfieldChannel(packed, masks[1]),
				B: bitfieldChannel(packed, masks[2]),
				A: 255,
			})
		}
	}
	return result, nil
}

func bitfieldChannel(value, mask uint32) uint8 {
	shift := bits.TrailingZeros32(mask)
	maximum := mask >> shift
	component := (value & mask) >> shift
	return uint8((uint64(component)*255 + uint64(maximum)/2) / uint64(maximum))
}

func win32Error(operation string, err error) error {
	if err == nil || err == windows.ERROR_SUCCESS {
		return fmt.Errorf("%s failed", operation)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
