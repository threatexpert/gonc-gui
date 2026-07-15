# Android Update Check Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a manual, localized update check to the existing Android About dialog and open a newer APK's versioned URL only after explicit user action.

**Architecture:** A focused, package-private `AndroidUpdateChecker` performs bounded HTTP fetching, JSON validation, Android arm64 asset selection, and numeric version comparison without depending on `Activity`. `MainActivity` owns dialog views and lifecycle, runs the checker off the UI thread, ignores stale results, and reuses the existing external-browser helper.

**Tech Stack:** Java 17, Android SDK 35/minSdk 26, `HttpURLConnection`, `org.json`, JUnit 4, Gradle Android plugin.

## Global Constraints

- Endpoint is exactly `https://www.gonc.cc/gui/manifest.json`; normal HTTP redirects must be followed.
- Connect timeout and read timeout are each 10 seconds; response body is limited to 1 MiB.
- Manifest `app` must equal `gonc-gui`.
- Versions are stable dot-separated nonnegative integers with one optional lowercase `v`; prerelease and nonnumeric versions are rejected.
- Select only asset `gonc-gui-android-arm64.apk`; only an absolute HTTPS `versioned_url` is accepted.
- Only a strictly newer remote version exposes a download URL; equal or older remote versions report current.
- The app never downloads, installs, replaces, restarts, or requests install/storage/unknown-source permissions for updates.
- A URL opens only after the user taps Go to download.
- Preserve all pre-existing Android working-tree changes and selectively stage only update-check hunks and new files.

---

### Task 1: Testable Android release checker

**Files:**
- Create: `android/app/src/main/java/cn/threatexpert/gonc/AndroidUpdateChecker.java`
- Create: `android/app/src/test/java/cn/threatexpert/gonc/AndroidUpdateCheckerTest.java`
- Modify: `android/app/build.gradle`

**Interfaces:**
- Consumes: endpoint, installed version, and `Build.SUPPORTED_ABIS` supplied by `MainActivity`.
- Produces: `AndroidUpdateChecker.check(String endpoint, String currentVersion, String[] supportedAbis)`, immutable `Result`, `Failure`, and `FailureKind` for Task 2.

- [ ] **Step 1: Add the real JSON implementation to the JVM test classpath**

Add this exact dependency beside JUnit in `android/app/build.gradle`:

```groovy
testImplementation "org.json:json:20240303"
```

This affects local unit tests only; production continues using Android's built-in `org.json`.

- [ ] **Step 2: Write failing version and platform tests**

Create `AndroidUpdateCheckerTest.java` in package `cn.threatexpert.gonc` with JUnit tables that assert:

```java
@Test
public void compareVersionsUsesNumericComponents() throws Exception {
    assertTrue(AndroidUpdateChecker.compareVersions("v1.2.16", "1.2.17") < 0);
    assertTrue(AndroidUpdateChecker.compareVersions("1.10.0", "1.9.9") > 0);
    assertEquals(0, AndroidUpdateChecker.compareVersions("1.2", "1.2.0"));
}

@Test
public void compareVersionsRejectsUnsupportedSyntax() {
    assertInvalidVersion("", "1.2.0");
    assertInvalidVersion("1.2.0-beta", "1.2.0");
    assertInvalidVersion("1.two.0", "1.2.0");
}

@Test
public void arm64SupportRequiresExactAbi() {
    assertTrue(AndroidUpdateChecker.supportsArm64(new String[]{"arm64-v8a", "armeabi-v7a"}));
    assertFalse(AndroidUpdateChecker.supportsArm64(new String[]{"armeabi-v7a"}));
}
```

The test helper catches `AndroidUpdateChecker.Failure` and asserts `kind == FailureKind.INVALID_MANIFEST`.

- [ ] **Step 3: Run the focused test and observe RED**

Run:

```powershell
android\gradlew.bat -p android testDebugUnitTest --tests cn.threatexpert.gonc.AndroidUpdateCheckerTest
```

Expected: compilation FAIL because `AndroidUpdateChecker` does not exist.

- [ ] **Step 4: Implement strict version parsing and arm64 detection**

Create `AndroidUpdateChecker.java` as a package-private final class. Define:

```java
static final int CONNECT_TIMEOUT_MS = 10_000;
static final int READ_TIMEOUT_MS = 10_000;
static final int MAX_MANIFEST_BYTES = 1 << 20;
static final String APP_NAME = "gonc-gui";
static final String ANDROID_ASSET = "gonc-gui-android-arm64.apk";

enum FailureKind { NETWORK, INVALID_MANIFEST, UNSUPPORTED_PLATFORM }

static final class Failure extends Exception {
    final FailureKind kind;
    Failure(FailureKind kind, String message) { super(message); this.kind = kind; }
    Failure(FailureKind kind, String message, Throwable cause) { super(message, cause); this.kind = kind; }
}

static final class Result {
    final String currentVersion;
    final String latestVersion;
    final boolean updateAvailable;
    final String downloadUrl;
    Result(String currentVersion, String latestVersion, boolean updateAvailable, String downloadUrl) {
        this.currentVersion = currentVersion;
        this.latestVersion = latestVersion;
        this.updateAvailable = updateAvailable;
        this.downloadUrl = downloadUrl;
    }
}
```

Implement `compareVersions` by parsing trimmed versions, removing one optional lowercase `v`, rejecting empty components and non-digits, parsing with `Long.parseLong`, padding missing trailing components with zero, and returning `-1`, `0`, or `1`. Implement `supportsArm64` as an exact `arm64-v8a` array membership check.

- [ ] **Step 5: Run focused tests and observe GREEN**

Run the Step 3 command.

Expected: all version and ABI tests PASS.

- [ ] **Step 6: Add failing manifest parsing and HTTP tests**

Extend the test class with helpers that create a loopback `com.sun.net.httpserver.HttpServer`. Cover:

- `/manifest.json` responds 302 to `/real.json`, which returns a newer valid manifest;
- exact selection of `gonc-gui-android-arm64.apk` among multiple assets;
- equal and older remote versions return `updateAvailable == false` and empty download URL;
- HTTP 500 maps to `NETWORK`;
- a handler that closes an advertised-but-incomplete response body maps to `NETWORK`;
- response larger than `MAX_MANIFEST_BYTES`, malformed JSON, wrong app, missing asset, invalid remote version, and `http://` versioned URL map to `INVALID_MANIFEST`;
- ABI without `arm64-v8a` maps to `UNSUPPORTED_PLATFORM` before an HTTP request.

The valid assertion is:

```java
AndroidUpdateChecker.Result result = AndroidUpdateChecker.check(
        serverUrl + "/manifest.json", "1.2.16", new String[]{"arm64-v8a"});
assertTrue(result.updateAvailable);
assertEquals("1.2.17", result.latestVersion);
assertEquals(
        "https://gonc.download/gonc-gui/v1.2.17/gonc-gui-1.2.17-android-arm64.apk",
        result.downloadUrl);
```

- [ ] **Step 7: Run tests and observe RED for missing check/parse behavior**

Run the Step 3 command.

Expected: FAIL because `check` and manifest/HTTP behavior are not implemented.

- [ ] **Step 8: Implement bounded redirected fetching and manifest validation**

Implement `check` with `HttpURLConnection`:

```java
static Result check(String endpoint, String currentVersion, String[] supportedAbis) throws Failure {
    compareVersions(currentVersion, currentVersion);
    if (!supportsArm64(supportedAbis)) {
        throw new Failure(FailureKind.UNSUPPORTED_PLATFORM, "arm64-v8a is required");
    }
    String json = fetch(endpoint);
    return parseManifest(json, currentVersion);
}
```

`fetch` must set `setInstanceFollowRedirects(true)`, both exact timeouts, `GET`, and an `Accept: application/json` header; require status 200–299; read at most `MAX_MANIFEST_BYTES + 1`; map connection, HTTP, timeout, and read exceptions to `NETWORK`; map oversize to `INVALID_MANIFEST`; and always disconnect.

`parseManifest` uses `JSONObject`/`JSONArray`, validates the exact app and exact asset name, parses the URL with `java.net.URI`, requires scheme `https` and a nonempty host, and validates both versions. It validates the selected asset even for equal/older releases, but returns an empty download URL unless the remote version is strictly newer. Catch JSON, URI, and numeric exceptions and wrap them as `INVALID_MANIFEST` without including remote response contents.

- [ ] **Step 9: Run focused and full Android JVM tests**

Run:

```powershell
android\gradlew.bat -p android testDebugUnitTest --tests cn.threatexpert.gonc.AndroidUpdateCheckerTest
android\gradlew.bat -p android testDebugUnitTest
```

Expected: both commands PASS with no test failures.

- [ ] **Step 10: Commit the checker**

```powershell
git add -- android/app/build.gradle android/app/src/main/java/cn/threatexpert/gonc/AndroidUpdateChecker.java android/app/src/test/java/cn/threatexpert/gonc/AndroidUpdateCheckerTest.java
git diff --cached --check
git commit -m "feat: add Android release checker"
```

---

### Task 2: Android About dialog update states

**Files:**
- Modify selectively: `android/app/src/main/java/cn/threatexpert/gonc/MainActivity.java`
- Modify selectively: `android/app/src/main/res/values/strings.xml`
- Modify selectively: `android/app/src/main/res/values-zh/strings.xml`

**Interfaces:**
- Consumes: `AndroidUpdateChecker.check(...)`, `Result`, `FailureKind`, existing `appVersionName()`, `openSourceUrl(String)`, and `Build.SUPPORTED_ABIS`.
- Produces: localized manual Check for updates, Current, Available, Error/retry, and Go to download behavior inside `showSourceDialog()`.

- [ ] **Step 1: Add exact localized update strings**

Add these resource names in both files:

```xml
<!-- English values -->
<string name="check_for_updates">Check for updates</string>
<string name="checking_for_updates">Checking for updates…</string>
<string name="update_current">You are using the latest version.</string>
<string name="update_available">A new version is available: %1$s</string>
<string name="go_to_download">Go to download</string>
<string name="update_network_error">Could not check for updates. Check your network and try again.</string>
<string name="update_manifest_error">The update information is invalid. Please try again later.</string>
<string name="update_platform_error">Updates are not available for this platform.</string>

<!-- Chinese values -->
<string name="check_for_updates">检查更新</string>
<string name="checking_for_updates">正在检查更新…</string>
<string name="update_current">当前已是最新版本。</string>
<string name="update_available">发现新版本：%1$s</string>
<string name="go_to_download">前往下载</string>
<string name="update_network_error">无法检查更新，请检查网络后重试。</string>
<string name="update_manifest_error">更新信息无效，请稍后重试。</string>
<string name="update_platform_error">当前平台暂不提供更新。</string>
```

- [ ] **Step 2: Add request generation state and a testable error-resource mapping**

Add `private int aboutUpdateGeneration;` to `MainActivity` and a package-private static mapper:

```java
static int updateFailureMessage(AndroidUpdateChecker.FailureKind kind) {
    if (kind == AndroidUpdateChecker.FailureKind.UNSUPPORTED_PLATFORM) {
        return R.string.update_platform_error;
    }
    if (kind == AndroidUpdateChecker.FailureKind.INVALID_MANIFEST) {
        return R.string.update_manifest_error;
    }
    return R.string.update_network_error;
}
```

Add unit assertions for this mapper to a new `android/app/src/test/java/cn/threatexpert/gonc/MainActivityUpdateTest.java`, then run its focused Gradle test first and observe RED because the mapper is missing; implement the mapper and rerun to GREEN.

- [ ] **Step 3: Extend the About dialog layout**

In `showSourceDialog()`:

- retain title, version, description, and both source links;
- wrap the existing `box` in a `ScrollView` before `dialog.setContentView` so added actions remain reachable;
- add a muted, initially hidden multiline status `TextView` with `View.ACCESSIBILITY_LIVE_REGION_POLITE`;
- add a secondary Check for updates button;
- add a primary Go to download button, initially `GONE`;
- keep the download URL in a one-element local `String[]` initialized to empty;
- clicking Go to download calls `openSourceUrl(downloadUrl[0])` only if nonempty.

- [ ] **Step 4: Implement background checking and stale-result protection**

On Check click:

1. increment `aboutUpdateGeneration` and capture it as `requestGeneration`;
2. clear the download URL, hide Go to download, show Checking text, and disable Check;
3. start a named `Thread` that calls the checker with the fixed endpoint, `appVersionName()`, and `Build.SUPPORTED_ABIS`;
4. post success/failure to `runOnUiThread`;
5. before touching views, require `dialog.isShowing()` and `requestGeneration == aboutUpdateGeneration`;
6. on success, show Current or Available; only Available stores the result URL and shows Go to download;
7. on failure, select the localized resource through `updateFailureMessage`;
8. always restore Check text and enabled state for an accepted result.

Set an `OnDismissListener` that increments `aboutUpdateGeneration`, invalidating late callbacks. Do not prevent the dialog from closing while checking.

- [ ] **Step 5: Run Android unit tests and assemble the debug APK**

Run:

```powershell
android\gradlew.bat -p android testDebugUnitTest
android\gradlew.bat -p android assembleDebug
```

Expected: tests PASS and `android/app/build/outputs/apk/debug/app-debug.apk` exists.

- [ ] **Step 6: Verify resources and compiled checker are packaged**

Run:

```powershell
$apk='android/app/build/outputs/apk/debug/app-debug.apk'
if (!(Test-Path $apk)) { throw 'Debug APK missing' }
$aapt=(Get-ChildItem "$env:ANDROID_HOME\build-tools" -Recurse -Filter aapt2.exe | Sort-Object FullName -Descending | Select-Object -First 1).FullName
& $aapt dump resources $apk | Select-String 'check_for_updates|go_to_download|update_available'
```

Expected: all three resource names appear. The successful DEX build of `MainActivity` referencing `AndroidUpdateChecker` proves the checker class is packaged into the APK.

- [ ] **Step 7: Selectively stage only update-check hunks and commit**

Use `git add -p` for the three pre-dirty files. Inspect the cached diff and confirm it excludes all pre-existing Android changes:

```powershell
git add -p -- android/app/src/main/java/cn/threatexpert/gonc/MainActivity.java android/app/src/main/res/values/strings.xml android/app/src/main/res/values-zh/strings.xml
git add -- android/app/src/test/java/cn/threatexpert/gonc/MainActivityUpdateTest.java
git diff --cached --check
git diff --cached --name-only
git commit -m "feat: add Android About update check"
```

---

### Task 3: Final Android regression and live-manifest verification

**Files:**
- Verify only; no expected source changes.

**Interfaces:**
- Consumes: Tasks 1–2.
- Produces: fresh evidence that the checker, About integration, APK, manifest redirect, and working-tree preservation are correct.

- [ ] **Step 1: Run fresh Android tests and builds**

Run:

```powershell
android\gradlew.bat -p android testDebugUnitTest --no-build-cache
android\gradlew.bat -p android assembleDebug --no-build-cache
```

Expected: both PASS.

- [ ] **Step 2: Verify the live redirecting manifest**

Run:

```powershell
$response=Invoke-WebRequest -MaximumRedirection 10 -Uri 'https://www.gonc.cc/gui/manifest.json'
$manifest=$response.Content | ConvertFrom-Json
$asset=$manifest.assets | Where-Object name -eq 'gonc-gui-android-arm64.apk'
if ($manifest.app -ne 'gonc-gui' -or !$manifest.version -or !$asset.versioned_url.StartsWith('https://')) {
  throw 'Unexpected Android update manifest'
}
```

Expected: exits successfully and selects one HTTPS Android arm64 asset.

- [ ] **Step 3: Audit committed scope and preserved working changes**

Resolve the committed plan as the implementation base and inspect the exact implementation range:

```powershell
$base=git log --format=%H --grep '^docs: plan Android update check$' -1
if (!$base) { throw 'Android implementation plan commit not found' }
git diff --check "$base..HEAD"
git diff --name-only "$base..HEAD"
git status --short -- android
```

Expected: feature range contains only checker, tests, build test dependency, and selectively committed About/resource hunks. Pre-existing Android changes remain unstaged.

- [ ] **Step 4: Perform device smoke checks when an Android device is available**

Install the debug APK and verify:

1. About shows Check for updates.
2. Live version comparison shows Current for an equal/older manifest.
3. A test endpoint returning a newer manifest shows Go to download.
4. Only tapping Go to download opens the versioned HTTPS APK URL.
5. Network failure displays a retryable localized error.
6. Dismissing About during a request produces no crash or stale UI update.

If no Android device is available, report this exact environment limitation; automated tests and APK assembly remain mandatory.
