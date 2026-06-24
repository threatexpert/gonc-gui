package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image/png"
	"time"

	"github.com/kbinani/screenshot"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// CaptureScreen hides the app window, grabs the whole virtual desktop (the
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

	// Hide our own window so it does not cover the QR, give the compositor a
	// moment to repaint, then capture the full virtual desktop.
	wailsruntime.WindowHide(a.ctx)
	time.Sleep(150 * time.Millisecond)
	img, err := screenshot.CaptureRect(bounds)
	wailsruntime.WindowShow(a.ctx)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
