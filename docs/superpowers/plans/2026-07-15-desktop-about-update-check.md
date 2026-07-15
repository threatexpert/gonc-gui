# Desktop About and Update Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a localized desktop About dialog that manually checks the redirecting Gonc release manifest and opens the correct platform download in the system browser.

**Architecture:** A focused `internal/appupdate` package performs bounded manifest fetching, validation, platform selection, and numeric version comparison. The Wails `App` exposes one check method, while React owns modal and request state and uses the Wails runtime only to open user-selected HTTPS links.

**Tech Stack:** Go 1.25, `net/http`, `encoding/json`, Wails v2.12, React 18, TypeScript 4.6, Vite 3, CSS.

## Global Constraints

- Manifest endpoint: `https://www.gonc.cc/gui/manifest.json`; normal HTTP redirects must be followed.
- Supported assets: `windows/amd64`, `windows/arm64`, `darwin/amd64`, `darwin/arm64`, and `linux/amd64` using the exact filenames in the design.
- Only stable dot-separated numeric versions with an optional leading `v` are accepted.
- Only HTTPS `versioned_url` values may be returned to the UI.
- Manifest response body is limited to 1 MiB and the request timeout is 10 seconds.
- The app checks only after a user click and never downloads, installs, replaces, restarts, or automatically opens a URL.
- Android code and behavior remain unchanged.
- Preserve unrelated uncommitted workspace changes; stage only files named by each task.

---

### Task 1: Manifest checker and version logic

**Files:**
- Create: `internal/appupdate/checker.go`
- Create: `internal/appupdate/checker_test.go`

**Interfaces:**
- Consumes: a caller-owned `context.Context`, `*http.Client`, endpoint URL, current version, GOOS, and GOARCH.
- Produces: `appupdate.Check(ctx context.Context, client *http.Client, endpoint, currentVersion, goos, goarch string) (Result, error)`, `Result`, and stable error-code constants used by the Wails layer and UI.

- [ ] **Step 1: Write failing unit tests for version comparison and platform mapping**

Create `internal/appupdate/checker_test.go` in package `appupdate` with table-driven tests that call `compareVersions` and `assetName`:

```go
package appupdate

import "testing"

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
```

- [ ] **Step 2: Run the tests and verify the new API is missing**

Run: `go test ./internal/appupdate`

Expected: FAIL because `compareVersions` and `assetName` are undefined.

- [ ] **Step 3: Implement strict numeric comparison and exact asset mapping**

Create `internal/appupdate/checker.go` with `normalizeVersion`, `compareVersions`, and `assetName`. `normalizeVersion` must trim whitespace, remove one optional lowercase `v`, split on `.`, reject empty/nonnumeric/negative components, and return `[]uint64`. `compareVersions` pads the shorter slice with zero and returns `-1`, `0`, or `1` for current older than, equal to, or newer than latest.

Define the public contract in the same file:

```go
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
```

Implement `assetName` as a switch over `goos + "/" + goarch` using the five mappings from Global Constraints.

- [ ] **Step 4: Run focused tests**

Run: `go test ./internal/appupdate -run 'TestCompareVersions|TestAssetName' -v`

Expected: all table entries PASS.

- [ ] **Step 5: Add failing HTTP, redirect, validation, and selection tests**

Extend `checker_test.go` with `httptest.Server` cases. Use a redirect endpoint `/manifest.json` pointing to `/real.json`, serve a valid manifest containing the Windows amd64 asset, and assert:

```go
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
```

Add table cases that assert `errors.Is` against private sentinel causes wrapped with the public codes for: HTTP 500/network failure, wrong `app`, invalid version, missing selected asset, non-HTTPS `versioned_url`, unsupported platform, and a body larger than 1 MiB. Add cases confirming equal and remotely older versions return `UpdateAvailable == false` and an empty `DownloadURL`.

- [ ] **Step 6: Run tests and verify `Check` is missing**

Run: `go test ./internal/appupdate -v`

Expected: FAIL because `Check` and the sentinel errors used by the tests are undefined.

- [ ] **Step 7: Implement bounded manifest checking**

In `checker.go`, define private sentinels and implement the exported function:

```go
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
```

Keep internal wrapped errors free of response bodies or URLs so Wails does not expose server-controlled text.

- [ ] **Step 8: Run package and repository tests**

Run: `go test ./internal/appupdate -v`

Expected: PASS including redirect, size bound, invalid manifest, and platform cases.

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 9: Commit the checker**

```powershell
git add -- internal/appupdate/checker.go internal/appupdate/checker_test.go
git commit -m "feat: add desktop release checker"
```

---

### Task 2: Wails update-check binding

**Files:**
- Modify: `app.go`
- Generated by Wails build: `frontend/wailsjs/go/main/App.js`
- Generated by Wails build: `frontend/wailsjs/go/main/App.d.ts`
- Generated by Wails build: `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: `appupdate.Check(context.Context, *http.Client, string, string, string, string) (appupdate.Result, error)` from Task 1.
- Produces: `func (a *App) CheckForUpdate(currentVersion string) (appupdate.Result, error)` and the generated TypeScript promise `CheckForUpdate(arg1: string): Promise<appupdate.Result>`.

- [ ] **Step 1: Add a failing main-package contract test**

Create a compile-time method-signature assertion in a new `app_update_test.go`:

```go
package main

import (
	"testing"

	"gonc-gui/internal/appupdate"
)

func TestAppExposesCheckForUpdate(t *testing.T) {
	app := NewApp(nil)
	var method func(string) (appupdate.Result, error) = app.CheckForUpdate
	if method == nil {
		t.Fatal("CheckForUpdate method is nil")
	}
}
```

- [ ] **Step 2: Run the contract test and verify the method is missing**

Run: `go test . -run TestAppExposesCheckForUpdate -v`

Expected: FAIL because `(*App).CheckForUpdate` is undefined.

- [ ] **Step 3: Add the Wails method with fixed endpoint, platform, and timeout**

Modify `app.go` imports to include `net/http` and `gonc-gui/internal/appupdate`. Add:

```go
const updateManifestURL = "https://www.gonc.cc/gui/manifest.json"

func (a *App) CheckForUpdate(currentVersion string) (appupdate.Result, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	client := &http.Client{Timeout: 10 * time.Second}
	return appupdate.Check(ctx, client, updateManifestURL, currentVersion, runtime.GOOS, runtime.GOARCH)
}
```

Using `http.Client` without a custom `CheckRedirect` intentionally enables Go's standard redirect following and redirect limit.

- [ ] **Step 4: Run the contract and full Go tests**

Run: `go test . -run TestAppExposesCheckForUpdate -v`

Expected: PASS; this is a compile-time contract test and does not call the network.

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 5: Generate Wails bindings through a normal build**

Run: `wails build -m -nopackage`

Expected: Wails reports a successful frontend and application build, and generated bindings contain `CheckForUpdate` plus `appupdate.Result`.

Verify:

```powershell
rg -n "CheckForUpdate|namespace appupdate|class Result" frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
```

Expected: matches in all necessary generated files; do not hand-edit generated bindings.

- [ ] **Step 6: Commit the binding**

```powershell
git add -- app.go app_update_test.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
git commit -m "feat: expose desktop update check"
```

---

### Task 3: Localized desktop About dialog

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.css`

**Interfaces:**
- Consumes: `CheckForUpdate(appVersion): Promise<appupdate.Result>` from Task 2 and Wails runtime `BrowserOpenURL(url: string): void`.
- Produces: an About button and modal with idle, checking, current, available, and error states in Chinese and English.

- [ ] **Step 1: Add imports, state types, and localized copy**

In `App.tsx`, import `CheckForUpdate` from `../wailsjs/go/main/App` and `BrowserOpenURL` from `../wailsjs/runtime/runtime`.

Add:

```ts
type UpdateState =
  | {kind: 'idle'}
  | {kind: 'checking'}
  | {kind: 'current'; latestVersion: string}
  | {kind: 'available'; latestVersion: string; downloadUrl: string}
  | {kind: 'error'; message: string};

const goncSourceUrl = 'https://github.com/threatexpert/gonc';
const guiSourceUrl = 'https://github.com/threatexpert/gonc-gui';
```

Add exact Chinese and English localization keys to the existing `text` object: `about`, `aboutTitle`, `aboutDescription`, `checkForUpdates`, `checkingForUpdates`, `upToDate`, `updateAvailable`, `goToDownload`, `updateNetworkError`, `updateManifestError`, and `updatePlatformError`. Use concise messages and interpolate the latest version in JSX rather than embedding it in translated strings.

- [ ] **Step 2: Confirm TypeScript fails before state and handlers exist**

Temporarily reference `aboutOpen` in the returned JSX, then run: `npm run build --prefix frontend`

Expected: FAIL with `Cannot find name 'aboutOpen'`.

- [ ] **Step 3: Implement modal state and event handlers**

Inside `App`, add:

```ts
const [aboutOpen, setAboutOpen] = useState(false);
const [updateState, setUpdateState] = useState<UpdateState>({kind: 'idle'});

function openAbout() {
  setUpdateState({kind: 'idle'});
  setAboutOpen(true);
}

function closeAbout() {
  if (updateState.kind !== 'checking') {
    setAboutOpen(false);
  }
}

async function checkForUpdates() {
  setUpdateState({kind: 'checking'});
  try {
    const result = await CheckForUpdate(appVersion);
    if (result.updateAvailable && result.downloadUrl) {
      setUpdateState({kind: 'available', latestVersion: result.latestVersion, downloadUrl: result.downloadUrl});
    } else {
      setUpdateState({kind: 'current', latestVersion: result.latestVersion});
    }
  } catch (error) {
    const message = String(error);
    setUpdateState({
      kind: 'error',
      message: message.includes('update_unsupported_platform')
        ? t.updatePlatformError
        : message.includes('update_invalid_manifest')
          ? t.updateManifestError
          : t.updateNetworkError,
    });
  }
}
```

Project and download links must call `BrowserOpenURL` only from their click handlers.

- [ ] **Step 4: Add the header button and accessible modal markup**

Wrap the existing optional status block and new About button in `.header-actions`. Add a ghost About button that calls `openAbout`.

Near the existing QR modal markup, render the About modal only when `aboutOpen`:

```tsx
<div className="qr-backdrop" role="presentation" onClick={closeAbout}>
  <section className="about-dialog" role="dialog" aria-modal="true" aria-label={t.aboutTitle} onClick={(event) => event.stopPropagation()}>
    <img className="about-mark" src={appIconUrl} alt="" aria-hidden="true" />
    <h2>{t.aboutTitle}</h2>
    <div className="about-version">{t.brand} {appVersion}</div>
    <p>{t.aboutDescription}</p>
    <button className="about-link" onClick={() => BrowserOpenURL(goncSourceUrl)}>{goncSourceUrl}</button>
    <button className="about-link" onClick={() => BrowserOpenURL(guiSourceUrl)}>{guiSourceUrl}</button>
    <div className="about-update" aria-live="polite">
      {updateState.kind === 'current' && <p>{t.upToDate} ({updateState.latestVersion})</p>}
      {updateState.kind === 'available' && <p>{t.updateAvailable}: {updateState.latestVersion}</p>}
      {updateState.kind === 'error' && <p className="about-error">{updateState.message}</p>}
    </div>
    <div className="about-actions">
      <button className="secondary" disabled={updateState.kind === 'checking'} onClick={checkForUpdates}>
        {updateState.kind === 'checking' ? t.checkingForUpdates : t.checkForUpdates}
      </button>
      {updateState.kind === 'available' && (
        <button className="primary" onClick={() => BrowserOpenURL(updateState.downloadUrl)}>{t.goToDownload}</button>
      )}
      <button className="primary" disabled={updateState.kind === 'checking'} onClick={closeAbout}>{t.close}</button>
    </div>
  </section>
</div>
```

- [ ] **Step 5: Style the header actions and Android-inspired dialog**

In `App.css`, add focused rules for `.header-actions`, `.about-dialog`, `.about-mark`, `.about-version`, `.about-link`, `.about-update`, `.about-error`, and `.about-actions`. Reuse the existing backdrop, colors, 6–8 px radii, button classes, and shadows. Set the dialog width to `min(430px, 100%)`, make source links wrap, and add a narrow-screen media rule that stacks `.header-actions` and `.about-actions` without horizontal overflow.

- [ ] **Step 6: Build and perform UI behavior checks**

Run: `npm run build --prefix frontend`

Expected: TypeScript and Vite build PASS.

Run: `wails dev`, then manually verify:

1. About opens and closes by button, backdrop, and Close.
2. Both languages show complete localized copy.
3. Source URLs open only after their buttons are clicked.
4. Check for updates disables controls while pending.
5. Version `v1.2.17` against live `1.2.16` reports current, not downgrade.
6. In the WebView developer console, temporarily replace `window.go.main.App.CheckForUpdate` with `() => Promise.resolve({currentVersion: 'v1.2.17', latestVersion: '1.2.18', updateAvailable: true, downloadUrl: 'https://example.com/gonc.zip'})`; a new check renders Go to download, and only that button opens the supplied URL. Restore the function by restarting `wails dev`.
7. Error state remains retryable and the modal does not overflow at the minimum supported window size.

Stop the dev process after verification.

- [ ] **Step 7: Commit the About UI**

```powershell
git add -- frontend/src/App.tsx frontend/src/App.css
git commit -m "feat: add desktop about dialog"
```

---

### Task 4: Final regression and scope verification

**Files:**
- Verify only; no expected source changes.

**Interfaces:**
- Consumes: all deliverables from Tasks 1–3.
- Produces: evidence that the complete feature builds, tests, and preserves Android scope.

- [ ] **Step 1: Run formatting and static checks**

Run: `gofmt -w internal/appupdate/checker.go internal/appupdate/checker_test.go app.go app_update_test.go`

Run: `go vet ./...`

Expected: PASS with no diagnostics.

- [ ] **Step 2: Run all automated tests and production builds**

Run: `go test ./...`

Expected: PASS.

Run: `npm run build --prefix frontend`

Expected: PASS.

Run: `wails build -m -nopackage`

Expected: PASS and produce the desktop binary under `build/bin`.

- [ ] **Step 3: Verify manifest redirect behavior independently**

Run:

```powershell
$response = Invoke-WebRequest -MaximumRedirection 10 -Uri 'https://www.gonc.cc/gui/manifest.json'
$manifest = $response.Content | ConvertFrom-Json
if ($manifest.app -ne 'gonc-gui' -or !$manifest.version) { throw 'Unexpected update manifest' }
```

Expected: command exits successfully with a parsed `gonc-gui` manifest.

- [ ] **Step 4: Audit the final diff and Android exclusion**

Run:

```powershell
git diff --check HEAD~3..HEAD
git diff --name-only HEAD~3..HEAD
```

Expected: no whitespace errors and no paths under `android/`. Confirm unrelated pre-existing working-tree changes are still unstaged and uncommitted.

- [ ] **Step 5: Commit verification-only formatting if required**

If `gofmt` changed tracked feature files, stage only those exact files and commit:

```powershell
git add -- internal/appupdate/checker.go internal/appupdate/checker_test.go app.go app_update_test.go
git commit -m "chore: format desktop update check"
```

If formatting produced no diff, do not create an empty commit.
