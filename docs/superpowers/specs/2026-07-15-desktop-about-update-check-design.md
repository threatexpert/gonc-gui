# Desktop About and Update Check Design

## Scope

Add an About dialog to the Wails desktop application, matching the purpose and content of the existing Android About dialog. The dialog lets the user manually check for a newer release and open its platform-specific download URL in the system browser.

The application will not download, install, replace, or restart itself. The Android application is unchanged.

## User Interface

Add an About button to the desktop header. Opening it displays a modal containing:

- application name and current version;
- the same open-source description used by the Android About dialog;
- links to `https://github.com/threatexpert/gonc` and `https://github.com/threatexpert/gonc-gui`;
- a Check for updates button;
- an area for update status and actions;
- a Close button.

All new labels, status messages, and errors are available in Chinese and English through the existing desktop localization object.

The update area has these states:

1. Idle: only Check for updates is shown.
2. Checking: the button is disabled and indicates progress.
3. Up to date: the dialog reports that the installed version is current.
4. Update available: the latest version is shown with a Go to download button.
5. Error: a localized, actionable message is shown and the user can retry.

Clicking a project link or Go to download opens the URL with the operating system's default browser.

## Architecture

### Go backend

The Go backend owns network access and platform selection. This avoids browser CORS restrictions and keeps operating-system details out of the React UI.

Add an update-checking unit with a narrow interface that:

1. sends an HTTPS GET request to `https://www.gonc.cc/gui/manifest.json`;
2. follows normal HTTP redirects;
3. applies a finite request timeout;
4. rejects non-success HTTP responses and malformed or incomplete JSON;
5. validates that the manifest describes `gonc-gui`;
6. selects the asset for the current `runtime.GOOS` and `runtime.GOARCH`;
7. compares the manifest version with the current application version supplied by the caller;
8. returns a small result object suitable for Wails binding.

The platform mapping is:

| Runtime | Asset name |
| --- | --- |
| `windows/amd64` | `gonc-gui-windows-amd64.zip` |
| `windows/arm64` | `gonc-gui-windows-arm64.zip` |
| `darwin/amd64` | `gonc-gui-macos-amd64.zip` |
| `darwin/arm64` | `gonc-gui-macos-arm64.zip` |
| `linux/amd64` | `gonc-gui-ubuntu-amd64.tar.gz` |

Unsupported runtime combinations return an explicit unsupported-platform error.

The backend result contains the installed version, latest version, whether an update is available, and the selected asset's `versioned_url`. It does not expose or use the moving `latest` asset URL for the browser action, so the displayed version and opened artifact remain consistent.

### React frontend

The React application owns modal visibility and the update-check UI state. It calls the Wails-bound backend method only when the user clicks Check for updates. It stores the returned version and URL in component state until the dialog closes or another check begins.

The frontend uses the Wails runtime browser-opening function for both source links and the download action. It does not fetch the manifest itself.

## Version Comparison

Normalize versions by trimming whitespace and accepting an optional leading `v`. Compare dot-separated numeric release components numerically, so `1.10.0` is newer than `1.9.9`. Missing trailing components compare as zero.

The initial implementation supports stable numeric releases used by this repository. A manifest version containing unsupported prerelease or nonnumeric syntax is rejected instead of being guessed. Equal versions and an older remote version both report that no update is available.

## Error Handling and Safety

- Use HTTPS only and do not accept a download URL with a non-HTTPS scheme.
- Set a bounded response size before decoding the manifest.
- Apply a request timeout so the UI cannot remain in the checking state indefinitely.
- Treat redirects as normal, while relying on Go's standard redirect limit.
- Validate the selected asset name and required fields before returning it.
- Never open a URL automatically after a check; opening the browser requires a separate user click.
- Do not download or execute release content inside the application.

User-facing errors distinguish network/service failure, invalid update information, and unsupported platform. Detailed internal errors may be logged, but the dialog remains concise.

## Testing

Go unit tests cover:

- optional `v` prefix and numeric version ordering;
- equal and remotely older versions;
- platform-to-asset selection;
- redirect handling;
- successful manifest parsing;
- timeout or HTTP failure;
- malformed manifest, wrong app name, missing asset, and unsafe URL.

Frontend verification covers opening and closing the modal, the checking state, current-version result, update-available result, retry after failure, and opening the selected URL only after user action. The production frontend build and Go test suite must pass.

## Release Compatibility

The existing release workflow already publishes the required manifest fields: `app`, `tag`, `version`, and per-platform assets with `name`, `versioned_url`, `sha256`, and `size`. No new update server or release artifact is required.

The desktop build already injects the current version from the repository `VERSION` file into the frontend as `__APP_VERSION__`. The frontend passes that exact value to the backend check method, so the displayed and compared versions cannot diverge and no second build-time version source is introduced.
