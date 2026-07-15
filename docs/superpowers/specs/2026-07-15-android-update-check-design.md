# Android Update Check Design

## Scope

Extend the existing Android About dialog with the same manual update-check behavior as the desktop application. The Android app checks release metadata, reports whether a newer version exists, and opens the selected APK's versioned HTTPS URL in the system browser only after a separate user click.

The app does not download, install, replace, restart, or request package-install permissions.

## User Interface

Keep the existing About dialog structure and content: application name and installed version, Android-equivalent open-source description, and links to the Gonc and gonc-gui repositories.

Add a status area and actions with these states:

1. Idle: show Check for updates.
2. Checking: disable the check action and show Checking for updates.
3. Current: report that the installed version is current and allow another check.
4. Available: show the latest version plus Go to download; retain Check for updates for retry.
5. Error: show a localized network, invalid-manifest, or unsupported-platform message and allow retry.

The dialog must remain dismissible while a request is running. A late result must not attempt to update dismissed dialog views. Clicking Go to download is the only action that opens the APK URL.

All new copy is defined in both `values/strings.xml` and `values-zh/strings.xml`.

## Architecture

Create a focused Java unit, `AndroidUpdateChecker`, rather than adding networking and parsing logic directly to `MainActivity`.

The checker:

- requests `https://www.gonc.cc/gui/manifest.json` on a background thread;
- follows normal HTTP redirects;
- uses finite connect and read timeouts;
- accepts at most 1 MiB of response data;
- requires a successful HTTP response;
- validates `app` equals `gonc-gui`;
- accepts stable dot-separated numeric versions with an optional leading `v`;
- selects the exact `gonc-gui-android-arm64.apk` asset;
- requires a nonempty absolute HTTPS `versioned_url`;
- returns an immutable result containing installed version, latest version, update availability, and download URL;
- maps failures to stable categories that `MainActivity` localizes.

`MainActivity` owns dialog state and lifecycle. It reads the installed `versionName` through the existing `appVersionName()` method, starts the checker only after the user taps Check for updates, and posts results back to the UI thread. A request generation token and dialog visibility check prevent stale or dismissed results from changing the UI.

Project links and the update download link use the existing browser-opening behavior. No downloader, file provider, package installer, storage permission, or unknown-source permission is introduced.

## Version Comparison

Trim surrounding whitespace, remove one optional lowercase `v`, and parse each dot-separated component as a nonnegative decimal integer. Reject blank, prerelease, or nonnumeric versions. Compare components numerically and treat missing trailing components as zero.

An equal or older remote version reports Current. Only a strictly newer remote version reports Available and exposes its URL to the dialog.

## Error Handling and Safety

- Network, timeout, response-read, and non-success HTTP failures map to a network/service message.
- Oversized, malformed, wrong-app, invalid-version, missing-asset, and unsafe-URL responses map to an invalid update information message.
- A runtime architecture other than Android arm64 maps to an unsupported-platform message; the currently published APK and Gradle ABI filter are arm64-only.
- Error details from remote responses are not displayed.
- The button returns to a retryable state after every success or failure.
- URLs never open automatically.

## Testing

JVM unit tests cover:

- optional `v`, numeric ordering, equal versions, trailing zeros, and invalid versions;
- valid manifest parsing and exact Android arm64 asset selection;
- equal and remotely older releases;
- redirects and successful response handling;
- HTTP failure, oversized response, malformed JSON, wrong app, missing asset, invalid version, and non-HTTPS URL;
- stable failure-category mapping.

Build verification includes Android unit tests, debug APK assembly, and confirmation that the resulting APK contains the new localized update strings and checker class. Manual smoke testing covers Current, Available, Error/retry, explicit browser opening, and dismissing the dialog while a request is active.

## Existing-Change Safety

Android source, resource, manifest, AAR, and test files already contain user-owned uncommitted changes. Implementation must preserve those changes and stage only update-check-related hunks and newly created files.
