//go:build windows

package sharecontent

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/bmp"
)

func TestChooseClipboardPayloadPrefersFilesThenImageThenText(t *testing.T) {
	got := chooseClipboardPayload(clipboardFormats{files: []string{"a"}, png: []byte{1}, text: "text"})
	if got.kind != ClipboardFiles {
		t.Fatalf("kind = %q, want %q", got.kind, ClipboardFiles)
	}

	got = chooseClipboardPayload(clipboardFormats{png: []byte{1}, text: "text"})
	if got.kind != ClipboardImage {
		t.Fatalf("kind = %q, want %q", got.kind, ClipboardImage)
	}

	got = chooseClipboardPayload(clipboardFormats{text: "text"})
	if got.kind != ClipboardText {
		t.Fatalf("kind = %q, want %q", got.kind, ClipboardText)
	}
}

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
