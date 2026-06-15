package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"gonc-gui/internal/goncrunner"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx    context.Context
	runner *goncrunner.Runner
}

type TransferRequest struct {
	Mode            string   `json:"mode"`
	Password        string   `json:"password"`
	SharePaths      []string `json:"sharePaths"`
	SaveDir         string   `json:"saveDir"`
	GoncPath        string   `json:"goncPath"`
	DownloadSubPath string   `json:"downloadSubPath"`
	NoCompress      bool     `json:"noCompress"`
}

type AppStatus struct {
	Running  bool   `json:"running"`
	GoncPath string `json:"goncPath"`
}

func NewApp() *App {
	return &App{
		runner: goncrunner.New(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) SelectFiles() ([]string, error) {
	return wailsruntime.OpenMultipleFilesDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select files to send",
	})
}

func (a *App) SelectFolder(title string) (string, error) {
	if title == "" {
		title = "Select folder"
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: title,
	})
}

func (a *App) GeneratePassword() (string, error) {
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (a *App) Status() AppStatus {
	path, _ := a.LocateGonc("")
	return AppStatus{
		Running:  a.runner.IsRunning(),
		GoncPath: path,
	}
}

func (a *App) LocateGonc(preferred string) (string, error) {
	return findGonc(preferred)
}

func (a *App) StartTransfer(req TransferRequest) error {
	if a.ctx == nil {
		return errors.New("application is not ready")
	}

	goncPath, err := findGonc(req.GoncPath)
	if err != nil {
		return err
	}

	return a.runner.Start(a.ctx, goncPath, goncrunner.Request{
		Mode:            goncrunner.Mode(req.Mode),
		Password:        req.Password,
		SharePaths:      req.SharePaths,
		SaveDir:         req.SaveDir,
		DownloadSubPath: req.DownloadSubPath,
		NoCompress:      req.NoCompress,
	}, func(event goncrunner.Event) {
		wailsruntime.EventsEmit(a.ctx, "gonc:event", event)
	})
}

func (a *App) StopTransfer() error {
	return a.runner.Stop()
}

func findGonc(preferred string) (string, error) {
	if preferred != "" {
		if fileExists(preferred) {
			return preferred, nil
		}
		return "", errors.New("selected gonc executable does not exist")
	}

	name := "gonc"
	if runtime.GOOS == "windows" {
		name = "gonc.exe"
	}

	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(base, "gonc", runtime.GOOS+"-"+runtime.GOARCH, name),
			filepath.Join(base, "bundled", "gonc", runtime.GOOS+"-"+runtime.GOARCH, name),
			filepath.Join(base, name),
		)
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "bundled", "gonc", runtime.GOOS+"-"+runtime.GOARCH, name),
			filepath.Join(wd, "..", "gonetcat", "bin", name),
			filepath.Join(wd, "..", "gonetcat", name),
		)
	}

	if path, err := exec.LookPath(name); err == nil {
		candidates = append(candidates, path)
	}

	for _, candidate := range candidates {
		if fileExists(candidate) {
			abs, err := filepath.Abs(candidate)
			if err == nil {
				return abs, nil
			}
			return candidate, nil
		}
	}
	return "", errors.New("gonc executable was not found; build gonetcat first or set a custom gonc path")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
