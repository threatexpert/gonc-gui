package receivedfile

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"

	"gonc-gui/internal/httpdownload"
)

var ErrUnavailable = errors.New("received file is no longer available")

func Reveal(saveDir string, file httpdownload.FileInfo) error {
	if states := Check(saveDir, []httpdownload.FileInfo{file}); len(states) != 1 || !states[0].Available {
		return ErrUnavailable
	}
	target, contained := resolveContainedPath(saveDir, file)
	if !contained {
		return ErrUnavailable
	}
	name, args, err := commandFor(runtime.GOOS, target)
	if err != nil {
		return err
	}
	if err = exec.Command(name, args...).Start(); err == nil || runtime.GOOS != "linux" {
		return err
	}
	return exec.Command("xdg-open", filepath.Dir(target)).Start()
}

func commandFor(goos, target string) (string, []string, error) {
	switch goos {
	case "windows":
		return "explorer.exe", []string{"/select,", target}, nil
	case "darwin":
		return "open", []string{"-R", target}, nil
	case "linux":
		uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(target)}).String()
		return "dbus-send", []string{
			"--session",
			"--dest=org.freedesktop.FileManager1",
			"--type=method_call",
			"/org/freedesktop/FileManager1",
			"org.freedesktop.FileManager1.ShowItems",
			"array:string:" + uri,
			"string:",
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported reveal platform: %s", goos)
	}
}
