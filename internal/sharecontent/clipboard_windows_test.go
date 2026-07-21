//go:build windows

package sharecontent

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unicode/utf16"

	"golang.org/x/image/bmp"
)

func TestExistingPathsFiltersMissingEntries(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.txt")

	got := existingPaths([]string{file, missing, dir, ""})
	if len(got) != 2 || got[0] != file || got[1] != dir {
		t.Fatalf("existingPaths() = %#v, want [%q %q]", got, file, dir)
	}
}

func TestImportClipboardPrefersExistingFiles(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "existing.txt")
	if err := os.WriteFile(existing, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	backend := &fakeClipboardBackend{
		paths:   []string{existing},
		filesOK: true,
		image:   image.NewRGBA(image.Rect(0, 0, 1, 1)),
		imageOK: true,
		text:    "text",
		textOK:  true,
	}

	got, err := newManagerAt(t.TempDir()).importNativeClipboard(backend)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != ClipboardFiles || !reflect.DeepEqual(got.Paths, []string{existing}) {
		t.Fatalf("result = %#v, want existing file", got)
	}
	if !reflect.DeepEqual(backend.calls, []string{"open", "files", "close"}) {
		t.Fatalf("calls = %#v", backend.calls)
	}
}

func TestImportClipboardFallsBackFromMissingFilesToImage(t *testing.T) {
	manager := newManagerAt(t.TempDir())
	backend := &fakeClipboardBackend{
		paths:   []string{filepath.Join(t.TempDir(), "missing")},
		filesOK: true,
		image:   image.NewRGBA(image.Rect(0, 0, 1, 1)),
		imageOK: true,
		text:    "text",
		textOK:  true,
	}

	got, err := manager.importNativeClipboard(backend)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != ClipboardImage || len(got.Paths) != 1 || !manager.Owns(got.Paths[0]) {
		t.Fatalf("result = %#v, want owned image", got)
	}
	if !reflect.DeepEqual(backend.calls, []string{"open", "files", "image", "close"}) {
		t.Fatalf("calls = %#v", backend.calls)
	}
}

func TestImportClipboardFallsBackToUnicodeText(t *testing.T) {
	manager := newManagerAt(t.TempDir())
	backend := &fakeClipboardBackend{text: "中文😀", textOK: true}

	got, err := manager.importNativeClipboard(backend)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != ClipboardText || len(got.Paths) != 1 {
		t.Fatalf("result = %#v, want text", got)
	}
	contents, err := os.ReadFile(got.Paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "中文😀" {
		t.Fatalf("text = %q", contents)
	}
	if !reflect.DeepEqual(backend.calls, []string{"open", "files", "image", "text", "close"}) {
		t.Fatalf("calls = %#v", backend.calls)
	}
}

func TestDecodeUnicodeText(t *testing.T) {
	for _, test := range []struct {
		name string
		text string
		want string
	}{
		{name: "Chinese", text: "中文", want: "中文"},
		{name: "surrogate pair", text: "😀", want: "😀"},
		{name: "embedded NUL", text: "前\x00后", want: "前"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := decodeUnicodeText(encodeUTF16LE(test.text))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("text = %q, want %q", got, test.want)
			}
		})
	}
	if _, err := decodeUnicodeText([]byte{1}); err == nil {
		t.Fatal("odd byte length succeeded, want error")
	}
}

type fakeClipboardBackend struct {
	calls   []string
	paths   []string
	filesOK bool
	image   image.Image
	imageOK bool
	text    string
	textOK  bool
}

func (backend *fakeClipboardBackend) open() error {
	backend.calls = append(backend.calls, "open")
	return nil
}

func (backend *fakeClipboardBackend) close() {
	backend.calls = append(backend.calls, "close")
}

func (backend *fakeClipboardBackend) readFiles() ([]string, bool, error) {
	backend.calls = append(backend.calls, "files")
	return backend.paths, backend.filesOK, nil
}

func (backend *fakeClipboardBackend) readImage() (image.Image, bool, error) {
	backend.calls = append(backend.calls, "image")
	return backend.image, backend.imageOK, nil
}

func (backend *fakeClipboardBackend) readText() (string, bool, error) {
	backend.calls = append(backend.calls, "text")
	return backend.text, backend.textOK, nil
}

func encodeUTF16LE(text string) []byte {
	encoded := utf16.Encode([]rune(text))
	data := make([]byte, len(encoded)*2)
	for index, value := range encoded {
		binary.LittleEndian.PutUint16(data[index*2:], value)
	}
	return data
}

func TestDecodeDIB(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 2, 2))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	source.Set(1, 0, color.RGBA{G: 255, A: 255})
	source.Set(0, 1, color.RGBA{B: 255, A: 255})
	source.Set(1, 1, color.RGBA{R: 255, G: 255, A: 255})

	var encoded bytes.Buffer
	if err := bmp.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	bmpBytes := encoded.Bytes()
	if len(bmpBytes) <= 14 {
		t.Fatalf("encoded BMP is only %d bytes", len(bmpBytes))
	}

	got, err := decodeDIB(bmpBytes[14:])
	if err != nil {
		t.Fatalf("decodeDIB() error = %v", err)
	}
	if got.Bounds().Dx() != 2 || got.Bounds().Dy() != 2 {
		t.Fatalf("decoded dimensions = %dx%d, want 2x2", got.Bounds().Dx(), got.Bounds().Dy())
	}
}

func TestDecodeDIBRejectsMalformedData(t *testing.T) {
	for _, data := range [][]byte{
		nil,
		make([]byte, 39),
		append([]byte{124, 0, 0, 0}, make([]byte, 40)...),
	} {
		if _, err := decodeDIB(data); err == nil {
			t.Fatalf("decodeDIB(%d bytes) succeeded, want error", len(data))
		}
	}
}

func TestDecodeDIBBITFIELDS16(t *testing.T) {
	tests := []struct {
		name  string
		masks [3]uint32
		red   uint16
		green uint16
		blue  uint16
		white uint16
	}{
		{name: "RGB565", masks: [3]uint32{0xf800, 0x07e0, 0x001f}, red: 0xf800, green: 0x07e0, blue: 0x001f, white: 0xffff},
		{name: "RGB555", masks: [3]uint32{0x7c00, 0x03e0, 0x001f}, red: 0x7c00, green: 0x03e0, blue: 0x001f, white: 0x7fff},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pixels := make([]byte, 8)
			// Positive DIB heights store the bottom row first.
			binary.LittleEndian.PutUint16(pixels[0:2], test.blue)
			binary.LittleEndian.PutUint16(pixels[2:4], test.white)
			binary.LittleEndian.PutUint16(pixels[4:6], test.red)
			binary.LittleEndian.PutUint16(pixels[6:8], test.green)
			got, err := decodeDIB(makeBITFIELDSDIB(2, 2, 16, test.masks, pixels))
			if err != nil {
				t.Fatalf("decodeDIB() error = %v", err)
			}
			assertRGB(t, got.At(0, 0), color.RGBA{R: 255, A: 255})
			assertRGB(t, got.At(1, 0), color.RGBA{G: 255, A: 255})
			assertRGB(t, got.At(0, 1), color.RGBA{B: 255, A: 255})
			assertRGB(t, got.At(1, 1), color.RGBA{R: 255, G: 255, B: 255, A: 255})
		})
	}
}

func TestDecodeDIBBITFIELDS32(t *testing.T) {
	masks := [3]uint32{0x00ff0000, 0x0000ff00, 0x000000ff}
	pixels := make([]byte, 8)
	binary.LittleEndian.PutUint32(pixels[0:4], 0x00ff0000)
	binary.LittleEndian.PutUint32(pixels[4:8], 0x0000ff00)

	got, err := decodeDIB(makeBITFIELDSDIB(2, -1, 32, masks, pixels))
	if err != nil {
		t.Fatalf("decodeDIB() error = %v", err)
	}
	assertRGB(t, got.At(0, 0), color.RGBA{R: 255, A: 255})
	assertRGB(t, got.At(1, 0), color.RGBA{G: 255, A: 255})
}

func TestDecodeDIBBITFIELDSScalingDoesNotOverflow(t *testing.T) {
	masks := [3]uint32{0xffffff80, 0x00000070, 0x0000000f}
	pixels := make([]byte, 4)
	binary.LittleEndian.PutUint32(pixels, masks[0])

	got, err := decodeDIB(makeBITFIELDSDIB(1, -1, 32, masks, pixels))
	if err != nil {
		t.Fatalf("decodeDIB() error = %v", err)
	}
	assertRGB(t, got.At(0, 0), color.RGBA{R: 255, A: 255})
}

func TestDecodeDIBRejectsInvalidHeadersAndBounds(t *testing.T) {
	zeroWidth := makeRGBDIB(0, 1, 24, 0, make([]byte, 4))
	minHeight := makeRGBDIB(1, -1<<31, 24, 0, make([]byte, 4))
	shortRows := makeRGBDIB(2, 2, 24, 0, make([]byte, 4))
	badPalette := makeRGBDIB(1, 1, 8, 257, nil)
	overlappingMasks := makeBITFIELDSDIB(1, 1, 16, [3]uint32{0x7c00, 0x7c00, 0x001f}, make([]byte, 4))
	nonContiguousMask := makeBITFIELDSDIB(1, 1, 16, [3]uint32{0x5000, 0x03e0, 0x001f}, make([]byte, 4))
	shortTopDown := makeRGBDIB(2, -2, 24, 0, make([]byte, 4))
	bitfields24 := makeBITFIELDSDIB(2, 1, 24, [3]uint32{0xff0000, 0xff00, 0xff}, make([]byte, 8))

	for name, data := range map[string][]byte{
		"zero width":          zeroWidth,
		"minimum height":      minHeight,
		"short rows":          shortRows,
		"oversized palette":   badPalette,
		"overlapping masks":   overlappingMasks,
		"non-contiguous mask": nonContiguousMask,
		"short top-down rows": shortTopDown,
		"24-bit bitfields":    bitfields24,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeDIB(data); err == nil {
				t.Fatal("decodeDIB() succeeded, want error")
			}
		})
	}
}

func makeBITFIELDSDIB(width, height int32, depth uint16, masks [3]uint32, pixels []byte) []byte {
	data := make([]byte, 52+len(pixels))
	binary.LittleEndian.PutUint32(data[0:4], 40)
	binary.LittleEndian.PutUint32(data[4:8], uint32(width))
	binary.LittleEndian.PutUint32(data[8:12], uint32(height))
	binary.LittleEndian.PutUint16(data[12:14], 1)
	binary.LittleEndian.PutUint16(data[14:16], depth)
	binary.LittleEndian.PutUint32(data[16:20], 3)
	binary.LittleEndian.PutUint32(data[20:24], uint32(len(pixels)))
	for index, mask := range masks {
		binary.LittleEndian.PutUint32(data[40+index*4:44+index*4], mask)
	}
	copy(data[52:], pixels)
	return data
}

func makeRGBDIB(width, height int32, depth uint16, colorsUsed uint32, pixels []byte) []byte {
	data := make([]byte, 40+len(pixels))
	binary.LittleEndian.PutUint32(data[0:4], 40)
	binary.LittleEndian.PutUint32(data[4:8], uint32(width))
	binary.LittleEndian.PutUint32(data[8:12], uint32(height))
	binary.LittleEndian.PutUint16(data[12:14], 1)
	binary.LittleEndian.PutUint16(data[14:16], depth)
	binary.LittleEndian.PutUint32(data[20:24], uint32(len(pixels)))
	binary.LittleEndian.PutUint32(data[32:36], colorsUsed)
	copy(data[40:], pixels)
	return data
}

func assertRGB(t *testing.T, got color.Color, want color.RGBA) {
	t.Helper()
	r, g, b, a := got.RGBA()
	if uint8(r>>8) != want.R || uint8(g>>8) != want.G || uint8(b>>8) != want.B || uint8(a>>8) != want.A {
		t.Fatalf("color = rgba(%d,%d,%d,%d), want %v", r>>8, g>>8, b>>8, a>>8, want)
	}
}
