package main

import wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

const defaultWindowWidth = 980
const defaultWindowHeight = 680

// FitStartupWindow only shrinks the normal window. The webview supplies the
// current screen's work area in logical pixels, excluding the taskbar/dock.
func (a *App) FitStartupWindow(availableWidth, availableHeight int) {
	fitStartupWindow(availableWidth, availableHeight, func(width, height int) {
		wailsruntime.WindowSetSize(a.ctx, width, height)
	}, func() {
		wailsruntime.WindowCenter(a.ctx)
	})
}

func fitStartupWindow(availableWidth, availableHeight int, resize func(int, int), center func()) {
	if availableWidth <= 32 || availableHeight <= 64 {
		return
	}
	// Leave breathing room around the window and room for native decorations.
	width := min(defaultWindowWidth, availableWidth-32)
	height := min(defaultWindowHeight, availableHeight-64)
	if width == defaultWindowWidth && height == defaultWindowHeight {
		return
	}
	// Keep these native calls ordered: centering before resizing can leave the
	// smaller window off-screen. Neither operation changes maximisation state.
	resize(width, height)
	center()
}
