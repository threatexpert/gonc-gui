package main

import (
	"path/filepath"
	"testing"
)

func TestWebviewUserDataPathIsPerInstallLocation(t *testing.T) {
	configDir := filepath.Join("C:", "Users", "demo", "AppData", "Roaming")
	release := filepath.Join("C:", "Users", "demo", "Downloads", "gonc-gui-1.2.25", "gonc-gui.exe")
	dev := filepath.Join("D:", "projects", "gonc-gui", "build", "bin", "gonc-gui.exe")

	releasePath := webviewUserDataPathFor(configDir, release)
	if releasePath != webviewUserDataPathFor(configDir, release) {
		t.Fatalf("path is not stable for the same executable")
	}
	if releasePath == webviewUserDataPathFor(configDir, dev) {
		t.Fatalf("different install locations share %q", releasePath)
	}
	if releasePath != webviewUserDataPathFor(configDir, filepath.Join("C:", "USERS", "demo", "Downloads", "gonc-gui-1.2.25", "GONC-GUI.EXE")) {
		t.Fatalf("path changes with the letter case of the executable path")
	}
	wantParent := filepath.Join(configDir, "Gonc", "gonc-gui", "webview")
	if filepath.Dir(releasePath) != wantParent {
		t.Fatalf("path %q is not under %q", releasePath, wantParent)
	}
}
