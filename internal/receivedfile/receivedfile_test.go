package receivedfile

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"gonc-gui/internal/httpdownload"
)

func TestCheckRequiresExistingRegularMatchingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ready.txt"), []byte("ready"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wrong.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder"), 0755); err != nil {
		t.Fatal(err)
	}

	states := Check(root, []httpdownload.FileInfo{
		{Path: "/ready.txt", Size: 5},
		{Path: "/wrong.txt", Size: 5},
		{Path: "/empty.txt", Size: 0},
		{Path: "/missing.txt", Size: 0},
		{Path: "/folder", IsDir: true},
	})
	assertAvailable(t, states, "/ready.txt", true)
	assertAvailable(t, states, "/wrong.txt", false)
	assertAvailable(t, states, "/empty.txt", true)
	assertAvailable(t, states, "/missing.txt", false)
	assertAvailable(t, states, "/folder", false)
}

func TestCheckRejectsLinkToMatchingFileOutsideSaveRoot(t *testing.T) {
	root := t.TempDir()
	externalRoot := t.TempDir()
	externalFile := filepath.Join(externalRoot, "payload.txt")
	if err := os.WriteFile(externalFile, []byte("ready"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := createDirectoryLink(externalRoot, filepath.Join(root, "outside")); err != nil {
		t.Fatalf("create directory link: %v", err)
	}

	file := httpdownload.FileInfo{Path: "/outside/payload.txt", Size: 5}
	states := Check(root, []httpdownload.FileInfo{file})
	assertAvailable(t, states, file.Path, false)
	if err := Reveal(root, file); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Reveal error = %v, want ErrUnavailable", err)
	}
}

func createDirectoryLink(target, link string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd.exe", "/c", "mklink", "/J", link, target).Run()
	}
	return os.Symlink(target, link)
}

func TestCommandForUsesDirectArguments(t *testing.T) {
	name, args, err := commandFor("windows", `C:\Downloads\safe name.exe`)
	if err != nil || name != "explorer.exe" || !reflect.DeepEqual(args, []string{"/select,", `C:\Downloads\safe name.exe`}) {
		t.Fatal(name, args, err)
	}

	name, args, err = commandFor("darwin", "/Users/me/Downloads/safe name")
	if err != nil || name != "open" || !reflect.DeepEqual(args, []string{"-R", "/Users/me/Downloads/safe name"}) {
		t.Fatal(name, args, err)
	}
}

func TestRevealRejectsChangedFileBeforeStartingProcess(t *testing.T) {
	root := t.TempDir()
	file := httpdownload.FileInfo{Path: "/tool.exe", Size: 4}
	if err := os.WriteFile(filepath.Join(root, "tool.exe"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Reveal(root, file); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error=%v", err)
	}
}

func TestRevealPathRejectsMissingPathBeforeStartingProcess(t *testing.T) {
	err := RevealPath(filepath.Join(t.TempDir(), "missing.txt"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error=%v, want ErrUnavailable", err)
	}
}

func assertAvailable(t *testing.T, states []State, remotePath string, want bool) {
	t.Helper()
	for _, state := range states {
		if state.RemotePath == remotePath {
			if state.Available != want {
				t.Fatalf("availability for %q = %v, want %v", remotePath, state.Available, want)
			}
			return
		}
	}
	t.Fatalf("missing state for %q", remotePath)
}
