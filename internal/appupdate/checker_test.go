package appupdate

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		current string
		latest  string
		want    int
		wantErr bool
	}{
		{"v1.2.16", "1.2.17", -1, false},
		{"1.10.0", "1.9.9", 1, false},
		{"1.2", "1.2.0", 0, false},
		{"1.2.17", "v1.2.16", 1, false},
		{"1.2.0-beta", "1.2.0", 0, true},
		{"", "1.2.0", 0, true},
	}
	for _, tt := range tests {
		got, err := compareVersions(tt.current, tt.latest)
		if (err != nil) != tt.wantErr {
			t.Fatalf("compareVersions(%q, %q) error = %v", tt.current, tt.latest, err)
		}
		if err == nil && got != tt.want {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", tt.current, tt.latest, got, tt.want)
		}
	}
}

func TestAssetName(t *testing.T) {
	tests := []struct{ goos, goarch, want string }{
		{"windows", "amd64", "gonc-gui-windows-amd64.zip"},
		{"windows", "arm64", "gonc-gui-windows-arm64.zip"},
		{"darwin", "amd64", "gonc-gui-macos-amd64.zip"},
		{"darwin", "arm64", "gonc-gui-macos-arm64.zip"},
		{"linux", "amd64", "gonc-gui-ubuntu-amd64.tar.gz"},
		{"linux", "arm64", ""},
	}
	for _, tt := range tests {
		if got := assetName(tt.goos, tt.goarch); got != tt.want {
			t.Fatalf("assetName(%q, %q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

func TestCheckFollowsRedirectAndSelectsAsset(t *testing.T) {
	const document = `{
		"app":"gonc-gui",
		"version":"1.2.17",
		"assets":[{
			"name":"gonc-gui-windows-amd64.zip",
			"versioned_url":"https://gonc.download/gonc-gui/v1.2.17/gonc-gui-1.2.17-windows-amd64.zip"
		}]
	}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			http.Redirect(w, r, "/real.json", http.StatusFound)
		case "/real.json":
			_, _ = w.Write([]byte(document))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := Check(context.Background(), server.Client(), server.URL+"/manifest.json", "v1.2.16", "windows", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !result.UpdateAvailable || result.LatestVersion != "1.2.17" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.DownloadURL != "https://gonc.download/gonc-gui/v1.2.17/gonc-gui-1.2.17-windows-amd64.zip" {
		t.Fatalf("unexpected URL: %q", result.DownloadURL)
	}
}

func TestCheckErrors(t *testing.T) {
	validManifest := func(version, name, downloadURL string) string {
		return fmt.Sprintf(`{"app":"gonc-gui","version":%q,"assets":[{"name":%q,"versioned_url":%q}]}`, version, name, downloadURL)
	}
	const validURL = "https://gonc.download/gonc-gui/v1.2.17/gonc-gui-1.2.17-windows-amd64.zip"

	tests := []struct {
		name     string
		status   int
		body     string
		goos     string
		goarch   string
		wantCode string
		wantIs   error
	}{
		{
			name:   "HTTP 500",
			status: http.StatusInternalServerError,
			body:   "server-controlled detail",
			goos:   "windows", goarch: "amd64",
			wantCode: ErrorNetwork, wantIs: errNetwork,
		},
		{
			name: "wrong app",
			body: `{"app":"another-app","version":"1.2.17","assets":[]}`,
			goos: "windows", goarch: "amd64",
			wantCode: ErrorInvalidManifest, wantIs: errInvalidManifest,
		},
		{
			name: "invalid version",
			body: validManifest("1.2.0-beta", "gonc-gui-windows-amd64.zip", validURL),
			goos: "windows", goarch: "amd64",
			wantCode: ErrorInvalidManifest, wantIs: errInvalidManifest,
		},
		{
			name: "missing selected asset",
			body: validManifest("1.2.17", "gonc-gui-windows-arm64.zip", validURL),
			goos: "windows", goarch: "amd64",
			wantCode: ErrorInvalidManifest, wantIs: errInvalidManifest,
		},
		{
			name: "non-HTTPS URL",
			body: validManifest("1.2.17", "gonc-gui-windows-amd64.zip", "http://gonc.download/release.zip"),
			goos: "windows", goarch: "amd64",
			wantCode: ErrorInvalidManifest, wantIs: errInvalidManifest,
		},
		{
			name: "unsupported platform",
			body: validManifest("1.2.17", "gonc-gui-linux-arm64.tar.gz", validURL),
			goos: "linux", goarch: "arm64",
			wantCode: ErrorUnsupportedPlatform, wantIs: errUnsupportedPlatform,
		},
		{
			name: "oversized body",
			body: strings.Repeat("x", maxManifestBytes+1),
			goos: "windows", goarch: "amd64",
			wantCode: ErrorInvalidManifest, wantIs: errInvalidManifest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			_, err := Check(context.Background(), server.Client(), server.URL, "1.2.16", tt.goos, tt.goarch)
			if !errors.Is(err, tt.wantIs) {
				t.Fatalf("Check() error = %v, want errors.Is(_, %v)", err, tt.wantIs)
			}
			if !strings.Contains(err.Error(), tt.wantCode) {
				t.Fatalf("Check() error = %q, want public code %q", err, tt.wantCode)
			}
			if strings.Contains(err.Error(), "server-controlled detail") {
				t.Fatalf("Check() exposed response body: %q", err)
			}
		})
	}
}

func TestCheckNetworkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := server.Client()
	endpoint := server.URL
	server.Close()

	_, err := Check(context.Background(), client, endpoint, "1.2.16", "windows", "amd64")
	if !errors.Is(err, errNetwork) {
		t.Fatalf("Check() error = %v, want errors.Is(_, errNetwork)", err)
	}
	if !strings.Contains(err.Error(), ErrorNetwork) {
		t.Fatalf("Check() error = %q, want public code %q", err, ErrorNetwork)
	}
}

func TestCheckNoUpdateClearsDownloadURL(t *testing.T) {
	for _, latest := range []string{"1.2.16", "1.2.15"} {
		t.Run(latest, func(t *testing.T) {
			document := fmt.Sprintf(`{"app":"gonc-gui","version":%q,"assets":[{"name":"gonc-gui-windows-amd64.zip","versioned_url":"https://gonc.download/release.zip"}]}`, latest)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(document))
			}))
			defer server.Close()

			result, err := Check(context.Background(), server.Client(), server.URL, "1.2.16", "windows", "amd64")
			if err != nil {
				t.Fatal(err)
			}
			if result.UpdateAvailable || result.DownloadURL != "" {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
}
