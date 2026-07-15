package appupdate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	ErrorNetwork             = "update_network"
	ErrorInvalidManifest     = "update_invalid_manifest"
	ErrorUnsupportedPlatform = "update_unsupported_platform"
	maxManifestBytes         = 1 << 20
)

type Result struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	UpdateAvailable bool   `json:"updateAvailable"`
	DownloadURL     string `json:"downloadUrl"`
}

type manifest struct {
	App     string  `json:"app"`
	Version string  `json:"version"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name         string `json:"name"`
	VersionedURL string `json:"versioned_url"`
}

var (
	errNetwork             = errors.New("update service unavailable")
	errInvalidManifest     = errors.New("invalid update manifest")
	errUnsupportedPlatform = errors.New("unsupported update platform")
)

func Check(ctx context.Context, client *http.Client, endpoint, currentVersion, goos, goarch string) (Result, error) {
	if _, err := normalizeVersion(currentVersion); err != nil {
		return Result{}, fmt.Errorf("%s: %w", ErrorInvalidManifest, errInvalidManifest)
	}
	name := assetName(goos, goarch)
	if name == "" {
		return Result{}, fmt.Errorf("%s: %w", ErrorUnsupportedPlatform, errUnsupportedPlatform)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", ErrorInvalidManifest, errInvalidManifest)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", ErrorNetwork, errNetwork)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("%s: %w: HTTP %d", ErrorNetwork, errNetwork, resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, maxManifestBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil || len(data) > maxManifestBytes {
		return Result{}, fmt.Errorf("%s: %w", ErrorInvalidManifest, errInvalidManifest)
	}
	var doc manifest
	if err := json.Unmarshal(data, &doc); err != nil || doc.App != "gonc-gui" {
		return Result{}, fmt.Errorf("%s: %w", ErrorInvalidManifest, errInvalidManifest)
	}
	order, err := compareVersions(currentVersion, doc.Version)
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", ErrorInvalidManifest, errInvalidManifest)
	}
	var downloadURL string
	for _, item := range doc.Assets {
		if item.Name != name {
			continue
		}
		parsed, err := url.Parse(item.VersionedURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return Result{}, fmt.Errorf("%s: %w", ErrorInvalidManifest, errInvalidManifest)
		}
		downloadURL = item.VersionedURL
		break
	}
	if downloadURL == "" {
		return Result{}, fmt.Errorf("%s: %w", ErrorInvalidManifest, errInvalidManifest)
	}
	result := Result{CurrentVersion: currentVersion, LatestVersion: doc.Version, UpdateAvailable: order < 0}
	if result.UpdateAvailable {
		result.DownloadURL = downloadURL
	}
	return result, nil
}

func normalizeVersion(version string) ([]uint64, error) {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return nil, errors.New("empty version")
	}

	parts := strings.Split(version, ".")
	numbers := make([]uint64, len(parts))
	for i, part := range parts {
		if part == "" {
			return nil, errors.New("empty version component")
		}
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid version component: %w", err)
		}
		numbers[i] = number
	}
	return numbers, nil
}

func compareVersions(current, latest string) (int, error) {
	currentParts, err := normalizeVersion(current)
	if err != nil {
		return 0, err
	}
	latestParts, err := normalizeVersion(latest)
	if err != nil {
		return 0, err
	}

	length := len(currentParts)
	if len(latestParts) > length {
		length = len(latestParts)
	}
	for i := 0; i < length; i++ {
		var currentPart, latestPart uint64
		if i < len(currentParts) {
			currentPart = currentParts[i]
		}
		if i < len(latestParts) {
			latestPart = latestParts[i]
		}
		if currentPart < latestPart {
			return -1, nil
		}
		if currentPart > latestPart {
			return 1, nil
		}
	}
	return 0, nil
}

func assetName(goos, goarch string) string {
	switch goos + "/" + goarch {
	case "windows/amd64":
		return "gonc-gui-windows-amd64.zip"
	case "windows/arm64":
		return "gonc-gui-windows-arm64.zip"
	case "darwin/amd64":
		return "gonc-gui-macos-amd64.zip"
	case "darwin/arm64":
		return "gonc-gui-macos-arm64.zip"
	case "linux/amd64":
		return "gonc-gui-ubuntu-amd64.tar.gz"
	default:
		return ""
	}
}
