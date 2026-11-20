package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed build/appicon.png
var desktopIcon []byte

// 128px version of build/appicon.png for GTK/X11's _NET_WM_ICON request size limit.
//
//go:embed build/linux/windowicon.png
var windowIcon []byte

const desktopID = "cn.threatexpert.gonc-gui"

func linuxWindowOptions() *linux.Options {
	if runtime.GOOS != "linux" {
		return nil
	}
	if os.Geteuid() != 0 {
		if err := registerDesktopIcon(); err != nil {
			fmt.Fprintln(os.Stderr, "Register Linux desktop icon:", err)
		}
	}
	return &linux.Options{
		Icon:             windowIcon,
		ProgramName:      desktopID,
		WebviewGpuPolicy: linux.WebviewGpuPolicyNever, // Wails' default when Linux options are nil.
	}
}

func registerDesktopIcon() error {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(dataHome) {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	filename := desktopID + ".desktop"
	entry := filepath.Join(dataHome, "applications", filename)
	const marker = "X-Gonc-Generated=true\n"
	old, err := os.ReadFile(entry)
	if err == nil && !bytes.Contains(old, []byte("\n"+marker)) {
		return nil // A desktop entry supplied by the user takes precedence.
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if old == nil {
		dirs := os.Getenv("XDG_DATA_DIRS")
		if dirs == "" {
			dirs = "/usr/local/share:/usr/share"
		}
		for _, dir := range filepath.SplitList(dirs) {
			if filepath.IsAbs(dir) {
				if _, err := os.Stat(filepath.Join(dir, "applications", filename)); err == nil {
					return nil
				}
			}
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	icon := filepath.Join(dataHome, desktopID, "appicon.png")
	if err := writeDesktopFile(icon, desktopIcon); err != nil {
		return err
	}
	// The desktop entry parser unescapes values before parsing Exec's quoted arguments.
	value := strings.NewReplacer(`\`, `\\`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	arg := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`", "%", "%%")
	// GTK captures the X11 WM_CLASS instance before Wails sets the Wayland program name.
	content := "[Desktop Entry]\nType=Application\nName=Gonc Transfer\nNoDisplay=true\n" +
		"Exec=\"" + value.Replace(arg.Replace(exe)) + "\"\nIcon=" + value.Replace(icon) +
		"\nStartupWMClass=" + value.Replace(filepath.Base(exe)) + "\n" + marker
	return writeDesktopFile(entry, []byte(content))
}

func writeDesktopFile(path string, content []byte) error {
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, content) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".gonc-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(content)
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
