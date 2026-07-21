package sharecontent

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestManagerCreatesUniqueUTF8Text(t *testing.T) {
	m := newManagerAt(filepath.Join(t.TempDir(), "generated"))
	first, err := m.CreateText("text", "娴ｇ姴銈絓nhello")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.CreateText("text", "second")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("generated paths collided")
	}
	got, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "娴ｇ姴銈絓nhello" {
		t.Fatalf("content = %q", got)
	}
	pattern := regexp.MustCompile(`^text-\d{8}-\d{6}\.\d{3}-[0-9a-f]{12}\.txt$`)
	if !pattern.MatchString(filepath.Base(first)) {
		t.Fatalf("name = %q", filepath.Base(first))
	}
}

func TestManagerCreatesDecodablePNG(t *testing.T) {
	m := newManagerAt(filepath.Join(t.TempDir(), "generated"))
	source := image.NewRGBA(image.Rect(0, 0, 2, 1))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	source.Set(1, 0, color.RGBA{G: 255, A: 255})

	path, err := m.CreatePNG("clipboard", source)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode generated PNG: %v", err)
	}
	if decoded.Bounds() != source.Bounds() {
		t.Fatalf("bounds = %v, want %v", decoded.Bounds(), source.Bounds())
	}
	pattern := regexp.MustCompile(`^clipboard-\d{8}-\d{6}\.\d{3}-[0-9a-f]{12}\.png$`)
	if !pattern.MatchString(filepath.Base(path)) {
		t.Fatalf("name = %q", filepath.Base(path))
	}
}

func TestManagerReleasesOwnedFiles(t *testing.T) {
	m := newManagerAt(filepath.Join(t.TempDir(), "generated"))
	path, err := m.CreateText("text", "temporary")
	if err != nil {
		t.Fatal(err)
	}
	if !m.Owns(path) {
		t.Fatal("created path is not owned")
	}
	if err := m.Release([]string{path}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("released file still exists: %v", err)
	}
	if m.Owns(path) {
		t.Fatal("released path is still owned")
	}
}

func TestManagerNeverDeletesUnownedPath(t *testing.T) {
	m := newManagerAt(filepath.Join(t.TempDir(), "generated"))
	external := filepath.Join(t.TempDir(), "user.txt")
	if err := os.WriteFile(external, []byte("user"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Release([]string{external}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("external removed: %v", err)
	}
}

func TestManagerCleanupRemovesOnlyOwnedRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "generated")
	external := filepath.Join(parent, "user.txt")
	if err := os.WriteFile(external, []byte("user"), 0600); err != nil {
		t.Fatal(err)
	}
	m := newManagerAt(root)
	path, err := m.CreateText("text", "temporary")
	if err != nil {
		t.Fatal(err)
	}

	if err := m.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("manager root still exists: %v", err)
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("external file removed: %v", err)
	}
	if m.Owns(path) {
		t.Fatal("cleaned path is still owned")
	}
}

func TestManagerCleanupDoesNotDeleteAnotherManagersFiles(t *testing.T) {
	first := NewManager()
	t.Cleanup(func() { _ = first.Cleanup() })
	second := NewManager()
	t.Cleanup(func() { _ = second.Cleanup() })

	firstPath, err := first.CreateText("text", "first")
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := second.CreateText("text", "second")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(firstPath) == filepath.Dir(secondPath) {
		t.Fatalf("managers share root %q", filepath.Dir(firstPath))
	}

	if err := first.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("second manager's file removed: %v", err)
	}
	if !second.Owns(secondPath) {
		t.Fatal("second manager lost ownership of its file")
	}
}

func TestManagerCleanupPreservesUnownedFileInsideConfiguredRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared")
	m := newManagerAt(root)
	owned, err := m.CreateText("text", "owned")
	if err != nil {
		t.Fatal(err)
	}
	unowned := filepath.Join(root, "unowned.txt")
	if err := os.WriteFile(unowned, []byte("user"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := m.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("owned file still exists: %v", err)
	}
	if got, err := os.ReadFile(unowned); err != nil || string(got) != "user" {
		t.Fatalf("unowned file changed: content %q, err %v", got, err)
	}
}

func TestManagerReleaseRetainsOwnershipWhenDeletionFails(t *testing.T) {
	m := newManagerAt(filepath.Join(t.TempDir(), "generated"))
	path, err := m.CreateText("text", "temporary")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "child"), []byte("block removal"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := m.Release([]string{path}); err == nil {
		t.Fatal("release succeeded despite failed deletion")
	}
	if !m.Owns(path) {
		t.Fatal("failed release discarded ownership")
	}
}
