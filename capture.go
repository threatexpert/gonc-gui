package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image/png"
	"runtime"
	"time"

	"github.com/kbinani/screenshot"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// CaptureScreen moves the app window out of the way, grabs the whole virtual desktop (the
// union of every active display, so a QR shown on a secondary monitor is
// included), then restores the window and returns the screenshot as a PNG
// data URL for in-app region selection and QR decoding.
func (a *App) CaptureScreen() (string, error) {
	if a.ctx == nil {
		return "", errors.New("application is not ready")
	}

	n := screenshot.NumActiveDisplays()
	if n <= 0 {
		return "", errors.New("no active display found")
	}

	bounds := screenshot.GetDisplayBounds(0)
	for i := 1; i < n; i++ {
		bounds = bounds.Union(screenshot.GetDisplayBounds(i))
	}

	// Move our own window out of the way so it does not cover the QR, give the
	// compositor a moment to repaint, then capture the full virtual desktop.
	// On Linux/Wayland, WindowHide can leave the GTK window unrecoverable when
	// the screenshot portal is cancelled, so use minimise there instead.
	a.prepareCaptureWindow()
	defer a.restoreCaptureWindow()
	time.Sleep(captureHideDelay())
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func captureHideDelay() time.Duration {
	if runtime.GOOS == "linux" {
		return 350 * time.Millisecond
	}
	return 150 * time.Millisecond
}

func (a *App) prepareCaptureWindow() {
	if runtime.GOOS == "linux" {
		wailsruntime.WindowMinimise(a.ctx)
		return
	}
	wailsruntime.WindowHide(a.ctx)
}

func (a *App) restoreCaptureWindow() {
	if a.ctx == nil {
		return
	}
	showCaptureWindow(a.ctx)
	if runtime.GOOS != "linux" {
		return
	}
	// Some Linux window managers show the GTK window but leave it behind other
	// windows after a hide/screenshot cycle. Toggle always-on-top briefly to
	// force a raise, then repeat once after the compositor settles.
	raiseCaptureWindow(a.ctx)
	ctx := a.ctx
	go func() {
		time.Sleep(250 * time.Millisecond)
		showCaptureWindow(ctx)
		raiseCaptureWindow(ctx)
	}()
}

func showCaptureWindow(ctx context.Context) {
	wailsruntime.WindowShow(ctx)
	wailsruntime.WindowUnminimise(ctx)
}

func raiseCaptureWindow(ctx context.Context) {
	wailsruntime.WindowSetAlwaysOnTop(ctx, true)
	time.Sleep(40 * time.Millisecond)
	wailsruntime.WindowSetAlwaysOnTop(ctx, false)
}
