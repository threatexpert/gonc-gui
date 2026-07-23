package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gonc-gui/internal/vpnhelper"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if handled, code := maybeRunVPNHelper(); handled {
		os.Exit(code)
	}
	// Create an instance of the app structure
	app := NewApp(collectStartupSharePaths(os.Args[1:]))

	// Create application with options
	err := wails.Run(&options.App{
		Title:  "Gonc Transfer",
		Width:  980,
		Height: 680,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 246, G: 248, B: 251, A: 1},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop: true,
		},
		OnStartup:     app.startup,
		OnBeforeClose: app.beforeClose,
		OnShutdown:    app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

func collectStartupSharePaths(args []string) []string {
	var paths []string
	seen := make(map[string]struct{})
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		path, err := filepath.Abs(arg)
		if err != nil {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func maybeRunVPNHelper() (bool, int) {
	if len(os.Args) < 2 || os.Args[1] != "--vpn-helper" {
		return false, 0
	}
	fs := flag.NewFlagSet("vpn-helper", flag.ContinueOnError)
	connectAddr := fs.String("connect", "", "helper callback address")
	token := fs.String("token", "", "helper authentication token")
	if err := fs.Parse(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return true, 2
	}
	if err := vpnhelper.Run(*connectAddr, *token); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return true, 1
	}
	return true, 0
}
