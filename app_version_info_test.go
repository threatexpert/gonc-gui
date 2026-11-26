package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The Windows exe's version resource comes from wails.json; keep it in step
// with VERSION, which the app, the Android build and the release names use.
func TestWailsProductVersionMatchesVERSION(t *testing.T) {
	raw, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimPrefix(strings.TrimSpace(string(raw)), "v")
	data, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	var project struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	if err := json.Unmarshal(data, &project); err != nil {
		t.Fatal(err)
	}
	if project.Info.ProductVersion != want {
		t.Fatalf("wails.json info.productVersion = %q, want %q from VERSION", project.Info.ProductVersion, want)
	}
}
