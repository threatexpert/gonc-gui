# Received File Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make size-matched local files visibly actionable in the current desktop and Android receive lists, with quiet desktop reveal controls and Android file-browser actions.

**Architecture:** Refresh only the visible remote directory after a download task ends or directory navigation completes. Desktop delegates secure path resolution, size checking, and platform reveal to Go; Android reuses `HttpReceiver` destination rules to resolve readable URIs and delegates system intents to a focused helper. Availability is current-session UI state, not persistent history or an integrity guarantee.

**Tech Stack:** Go, Wails v2, React 18/TypeScript/CSS, Android Java (minSdk 26, targetSdk 35), SAF/MediaStore/FileProvider, JUnit 4.

## Global Constraints

- Actionable means: expected target exists, is readable, is not a directory, and exactly matches the remote byte size. Modification time is ignored; zero-byte files must actually exist.
- Publish no new availability during a download. Keep existing markers visible but disable every desktop locate and Android open/open-with/share/info action until task termination, then refresh once; also refresh once after entering a directory while idle.
- Disconnect preserves actions; only a newly established receive connection resets them. Android persists no received-file browsing history.
- Desktop never opens a file. Its icon-only reveal action appears on hover and keyboard focus.
- Android file icon/name taps open immediately. The overflow menu contains only Open, Open with, Share, and File information.
- Declare `android.permission.REQUEST_INSTALL_PACKAGES`, never `INSTALL_PACKAGES`; Android owns APK source authorization and installation confirmation, with no Gonc warning.
- Normalize paths, enforce desktop save-root containment, use direct process arguments, revalidate before actions, and discard stale asynchronous results.

## File Structure

- `internal/httpdownload/localpath.go`: shared canonical desktop target resolver.
- `internal/receivedfile/`: desktop size checks and platform reveal.
- `app.go`: narrow Wails APIs and DTO.
- `frontend/src/App.tsx`, `App.css`: desktop current-session state and quiet row UI.
- `ReceivedFileMatcher.java`: pure Android availability predicate.
- `ReceivedFileActions.java`: Android MIME, FileProvider, open, chooser, share, and info actions.
- `HttpReceiver.java`: read-only target lookup using existing destination rules.
- `ReceiveController.java`: Android current-run state, refresh triggers, and row controls.
- `received_file_paths.xml`, `AndroidManifest.xml`, localized strings: Android platform declarations and copy.

---

### Task 4: Android Read-Only Destination Resolution

**Files:**
- Create: `android/app/src/main/java/cn/threatexpert/gonc/ReceivedFileMatcher.java`
- Create: `android/app/src/test/java/cn/threatexpert/gonc/ReceivedFileMatcherTest.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/HttpReceiver.java:61-70,1070-1380,1550-1570`

**Interfaces:**
- Consumes: existing `RemoteFile`, `TargetResolver`, SAF tree, MediaStore, and remembered resume URI rules.
- Produces: pure `ReceivedFileMatcher.isAvailable(...)`, `ReceivedTarget`, and `findReceivedTargets(Context, Uri, List<RemoteFile>)` keyed by normalized path.

- [ ] **Step 1: Write the failing pure JVM test**

```java
@Test public void requiresReadableRegularMatchingTarget() {
    assertTrue(ReceivedFileMatcher.isAvailable(true, true, false, 5, 5));
    assertTrue(ReceivedFileMatcher.isAvailable(true, true, false, 0, 0));
    assertFalse(ReceivedFileMatcher.isAvailable(false, true, false, 0, 0));
    assertFalse(ReceivedFileMatcher.isAvailable(true, false, false, 5, 5));
    assertFalse(ReceivedFileMatcher.isAvailable(true, true, true, 5, 5));
    assertFalse(ReceivedFileMatcher.isAvailable(true, true, false, 4, 5));
}
```

Run from `android`: `.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.ReceivedFileMatcherTest`

Expected: FAIL because `ReceivedFileMatcher` is undefined.

- [ ] **Step 2: Implement the pure matcher**

```java
final class ReceivedFileMatcher {
    private ReceivedFileMatcher() {}
    static boolean isAvailable(boolean exists, boolean readable, boolean directory, long actualSize, long expectedSize) {
        return exists && readable && !directory && actualSize >= 0 && expectedSize >= 0 && actualSize == expectedSize;
    }
}
```

- [ ] **Step 3: Add read-only lookup without creating targets**

```java
static Map<String, ReceivedTarget> findReceivedTargets(Context context, Uri tree, List<RemoteFile> files) {
    Map<String, ReceivedTarget> found = new LinkedHashMap<>();
    TargetResolver resolver = new TargetResolver(context.getApplicationContext(), tree);
    for (RemoteFile file : files) {
        if (file == null || file.isDir) continue;
        DocumentInfo info = resolver.findExisting(file.path, file.name);
        boolean readable = info != null && resolver.canRead(info.uri);
        if (info != null && ReceivedFileMatcher.isAvailable(true, readable, false, info.size, file.size)) {
            found.put(normalizePath(file.path), new ReceivedTarget(info.uri, displayName(context, info.uri), info.size, info.modifiedMs));
        }
    }
    return found;
}
```

`findExisting` first checks the remembered URI, then traverses existing SAF children or queries the existing Downloads relative directory and current collision variants. It must never call `createDocument`, `insert`, `mkdirs`, or `ensure*`. `canRead` opens and closes an `AssetFileDescriptor` in mode `r`; directory MIME targets are rejected.

- [ ] **Step 4: Verify and commit**

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.ReceivedFileMatcherTest
.\gradlew.bat compileDebugJavaWithJavac
git add -- app/src/main/java/cn/threatexpert/gonc/ReceivedFileMatcher.java app/src/main/java/cn/threatexpert/gonc/HttpReceiver.java app/src/test/java/cn/threatexpert/gonc/ReceivedFileMatcherTest.java
git commit -m "feat: resolve received Android files"
```

Expected: tests PASS, Java compiles, and lookup has no write path.

---

### Task 5: Android System File Actions and APK Handoff

**Files:**
- Create: `android/app/src/main/java/cn/threatexpert/gonc/ReceivedFileActions.java`
- Create: `android/app/src/test/java/cn/threatexpert/gonc/ReceivedFileActionsTest.java`
- Create: `android/app/src/main/res/xml/received_file_paths.xml`
- Modify: `android/app/src/main/AndroidManifest.xml:1-20,75-80`
- Modify: `android/app/src/main/res/values/strings.xml`
- Modify: `android/app/src/main/res/values-zh/strings.xml`

**Interfaces:**
- Consumes: Task 4 `ReceivedTarget`.
- Produces: `open`, `openWith`, `share`, `showInfo`, `fallbackMimeType`, and safe conversion of legacy `file://` targets through FileProvider.

- [ ] **Step 1: Write failing MIME tests**

```java
@Test public void recognizesApkAndCommonDocuments() {
    assertEquals("application/vnd.android.package-archive", ReceivedFileActions.fallbackMimeType("release.apk"));
    assertEquals("application/pdf", ReceivedFileActions.fallbackMimeType("manual.PDF"));
    assertEquals("application/octet-stream", ReceivedFileActions.fallbackMimeType("payload.unknownext"));
}
```

Run: `.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.ReceivedFileActionsTest`

Expected: FAIL because `ReceivedFileActions` is undefined.

- [ ] **Step 2: Implement MIME and intent actions**

```java
static String fallbackMimeType(String name) {
    String lower = name == null ? "" : name.toLowerCase(Locale.ROOT);
    if (lower.endsWith(".apk")) return "application/vnd.android.package-archive";
    String guessed = URLConnection.guessContentTypeFromName(lower);
    return guessed == null ? "application/octet-stream" : guessed;
}

static void open(Context context, ReceivedTarget target, boolean chooser) {
    Uri uri = shareableUri(context, target.uri);
    String mime = context.getContentResolver().getType(uri);
    if (mime == null || mime.isEmpty() || "application/octet-stream".equals(mime)) mime = fallbackMimeType(target.displayName);
    Intent view = new Intent(Intent.ACTION_VIEW).setDataAndType(uri, mime).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
    context.startActivity(chooser ? Intent.createChooser(view, context.getString(R.string.open_with)) : view);
}

static void share(Context context, ReceivedTarget target) {
    Uri uri = shareableUri(context, target.uri);
    Intent send = new Intent(Intent.ACTION_SEND).setType(fallbackMimeType(target.displayName))
            .putExtra(Intent.EXTRA_STREAM, uri).addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
    context.startActivity(Intent.createChooser(send, context.getString(R.string.share_file)));
}
```

Convert `file://` with `FileProvider.getUriForFile(context, context.getPackageName()+".received-files", file)`. `showInfo` uses `AlertDialog` for display name, formatted size, remote modification time when present, and save-location label. Controller owns exception handling.

- [ ] **Step 3: Declare provider and installer-request permission**

```xml
<uses-permission android:name="android.permission.REQUEST_INSTALL_PACKAGES" />
<provider
    android:name="androidx.core.content.FileProvider"
    android:authorities="${applicationId}.received-files"
    android:exported="false"
    android:grantUriPermissions="true">
    <meta-data android:name="android.support.FILE_PROVIDER_PATHS" android:resource="@xml/received_file_paths" />
</provider>
```

```xml
<paths xmlns:android="http://schemas.android.com/apk/res/android">
    <external-path name="gonc_downloads" path="Download/Gonc/" />
</paths>
```

Add Chinese/English strings for Open, Open with, Share, File information, Received, action menu accessibility, no handler, no longer accessible, and information fields. Add no Gonc APK warning.

- [ ] **Step 4: Verify permission boundary and commit**

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.ReceivedFileActionsTest
.\gradlew.bat processDebugMainManifest
$merged = 'app\build\intermediates\merged_manifest\debug\processDebugMainManifest\AndroidManifest.xml'
$text = Get-Content -Raw $merged
if ($text -notmatch 'android.permission.REQUEST_INSTALL_PACKAGES') { throw 'permission missing' }
if ($text -match 'android.permission.INSTALL_PACKAGES') { throw 'forbidden permission present' }
git add -- app/src/main/java/cn/threatexpert/gonc/ReceivedFileActions.java app/src/test/java/cn/threatexpert/gonc/ReceivedFileActionsTest.java app/src/main/res/xml/received_file_paths.xml app/src/main/AndroidManifest.xml app/src/main/res/values/strings.xml app/src/main/res/values-zh/strings.xml
git commit -m "feat: add Android received file actions"
```

Expected: tests PASS; merged manifest contains only the request permission and a non-exported provider.

---

### Task 6: Android Current-Session Browser UI

During initial and repeated downloads, keep received checkmarks visible but disable file icon/name/whitespace and overflow actions. Re-enable them only after the whole-task completion refresh.

**Files:**
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/ReceiveController.java:42-95,189-225,340-540,1203-1245,1395-1435,1590-1665`

**Interfaces:**
- Consumes: Task 4 lookup and Task 5 actions.
- Produces: per-`receiveRunId` target map refreshed only on directory entry and task completion.

- [ ] **Step 1: Add stale-safe state and refresh**

```java
private final Map<String, HttpReceiver.ReceivedTarget> receivedTargets = new LinkedHashMap<>();
private long receivedTargetCheckId;

private void refreshReceivedTargets(long runId, String checkedPath) {
    long checkId = ++receivedTargetCheckId;
    List<HttpReceiver.RemoteFile> snapshot = new ArrayList<>(visibleRemoteFiles());
    Uri tree = saveTreeUri;
    new Thread(() -> {
        Map<String, HttpReceiver.ReceivedTarget> checked = HttpReceiver.findReceivedTargets(context(), tree, snapshot);
        host.mainHandler().post(() -> {
            if (!isCurrentRun(runId) || checkId != receivedTargetCheckId || !normalizeRemotePath(remoteCurrentPath).equals(normalizeRemotePath(checkedPath))) return;
            for (HttpReceiver.RemoteFile file : snapshot) receivedTargets.remove(normalizeRemotePath(file.path));
            receivedTargets.putAll(checked);
            host.requestRender();
        });
    }, "gonc-received-file-check").start();
}
```

Clear the map/increment check ID only after `startP2PReceive` returns a new session. Do not clear on stop, stopped callback, or error.

- [ ] **Step 2: Wire exactly two refresh triggers**

```java
// After a directory list is merged:
refreshReceivedTargets(runId, targetPath);

// At the end of download onComplete after receiveDownload = null and final counters:
refreshReceivedTargets(runId, remoteCurrentPath);
```

Never call from progress. A new download retains existing entries until final refresh.

- [ ] **Step 3: Add direct-open row controls without breaking checkbox selection**

```java
HttpReceiver.ReceivedTarget target = receivedTargets.get(normalizedPath);
if (!file.isDir && target != null) {
    View.OnClickListener open = v -> openReceivedFile(file, false);
    icon.setOnClickListener(open);
    labels.setOnClickListener(open);
    labels.setClickable(true);
    labels.setFocusable(true);
    row.addView(text("✓", 14, Color.rgb(32, 151, 102), Typeface.BOLD));
    Button more = quietTouchButton("⋮");
    more.setContentDescription(string(R.string.received_file_actions));
    more.setOnClickListener(v -> showReceivedFileMenu(more, file));
    row.addView(more, new LinearLayout.LayoutParams(dp(42), dp(42)));
}
```

Keep checkbox behavior independent and folder navigation unchanged. The overflow menu has exactly four localized actions.

- [ ] **Step 4: Revalidate asynchronously before every action**

```java
private void withCurrentReceivedTarget(HttpReceiver.RemoteFile file, Consumer<HttpReceiver.ReceivedTarget> action) {
    long runId = receiveRunId;
    new Thread(() -> {
        Map<String, HttpReceiver.ReceivedTarget> checked = HttpReceiver.findReceivedTargets(context(), saveTreeUri, Collections.singletonList(file));
        host.mainHandler().post(() -> {
            if (!isCurrentRun(runId)) return;
            HttpReceiver.ReceivedTarget target = checked.get(normalizeRemotePath(file.path));
            if (target == null) {
                receivedTargets.remove(normalizeRemotePath(file.path));
                host.toast(R.string.toast_received_file_unavailable);
                host.requestRender();
                return;
            }
            action.accept(target);
        });
    }, "gonc-received-file-action-check").start();
}
```

Use it for open/open-with/share/info. `ActivityNotFoundException` shows no-handler and retains state; permission/stale failures remove state. APK follows the same open path without a Gonc warning.

- [ ] **Step 5: Verify, smoke test, and commit**

```powershell
.\gradlew.bat testDebugUnitTest
.\gradlew.bat assembleDebug
git add -- app/src/main/java/cn/threatexpert/gonc/ReceiveController.java
git commit -m "feat: browse received Android files"
```

Expected: suite PASS and debug APK builds. On device verify batch updates, directory refresh, disconnect retention/new-connection reset, icon/name open versus checkbox select, four menu actions, deletion invalidation, no-handler retention, and system-controlled APK flow.

---

### Task 7: Full Verification and Documentation Consistency

> Execute this final task only after Tasks 1-6, including the desktop tasks described below, are complete.

**Files:**
- Verify: all Task 1-6 files
- Modify only if conflicting: `README.md`, `README_zh.md`, `android/README.md`

**Interfaces:**
- Consumes: complete feature.
- Produces: clean tests/builds, verified permission boundary, and accurate user docs.

- [ ] **Step 1: Format and check diffs**

```powershell
gofmt -w internal/httpdownload/localpath.go internal/httpdownload/localpath_test.go internal/receivedfile/receivedfile.go internal/receivedfile/receivedfile_test.go internal/receivedfile/reveal.go app.go app_received_files_test.go
git diff --check
```

Expected: no whitespace errors.

- [ ] **Step 2: Run complete desktop verification**

```powershell
go test ./... -count=1
Push-Location frontend
npm run build
Pop-Location
wails build -clean
```

Expected: all tests and both frontend/Wails builds PASS.

- [ ] **Step 3: Run complete Android verification**

```powershell
Push-Location android
.\gradlew.bat testDebugUnitTest
.\gradlew.bat assembleDebug
Pop-Location
```

Expected: all tests PASS and `android/app/build/outputs/apk/debug/app-debug.apk` exists.

- [ ] **Step 4: Recheck merged manifest**

```powershell
$merged = 'android\app\build\intermediates\merged_manifest\debug\processDebugMainManifest\AndroidManifest.xml'
$text = Get-Content -Raw $merged
if ($text -notmatch 'android.permission.REQUEST_INSTALL_PACKAGES') { throw 'REQUEST_INSTALL_PACKAGES missing' }
if ($text -match 'android.permission.INSTALL_PACKAGES') { throw 'INSTALL_PACKAGES must not be present' }
if ($text -notmatch '\.received-files' -or $text -notmatch 'android:exported="false"') { throw 'FileProvider misconfigured' }
```

Expected: no output.

- [ ] **Step 5: Check docs and finish**

Confirm docs describe desktop locate-not-open, Android current-session actions, size-based availability, and system-controlled APK installation. Update only conflicting paragraphs, then run:

```powershell
git add -- README.md README_zh.md android/README.md
git diff --cached --quiet || git commit -m "docs: describe received file actions"
git status --short
```

Expected: clean worktree; no docs commit if existing text did not conflict.

### Task 1: Canonical Desktop Target Resolution

**Files:**
- Create: `internal/httpdownload/localpath.go`
- Create: `internal/httpdownload/localpath_test.go`
- Modify: `internal/httpdownload/downloader.go:344-365,944-959`

**Interfaces:**
- Consumes: `httpdownload.FileInfo`.
- Produces: `ResolveLocalPath(saveDir string, file FileInfo) (string, error)` for the downloader and Task 2.

- [ ] **Step 1: Write failing tests**

```go
func TestResolveLocalPathUsesNormalizedRemotePath(t *testing.T) {
    root := t.TempDir()
    got, err := ResolveLocalPath(root, FileInfo{Name: "report.txt", Path: "/docs/report.txt"})
    if err != nil { t.Fatal(err) }
    if want := filepath.Join(root, "docs", "report.txt"); got != want { t.Fatalf("got %q want %q", got, want) }
}

func TestResolveLocalPathRejectsEscape(t *testing.T) {
    _, err := ResolveLocalPath(t.TempDir(), FileInfo{Name: "x", Path: "../../outside.exe"})
    if err == nil || !strings.Contains(err.Error(), "escapes save directory") { t.Fatalf("error = %v", err) }
}

func TestResolveLocalPathSanitizesRootFilename(t *testing.T) {
    root := t.TempDir()
    got, err := ResolveLocalPath(root, FileInfo{Name: "../report.txt", Path: "/"})
    if err != nil || got != filepath.Join(root, "report.txt") { t.Fatalf("path=%q error=%v", got, err) }
}
```

- [ ] **Step 2: Verify failure**

Run: `go test ./internal/httpdownload -run TestResolveLocalPath -count=1`

Expected: FAIL because `ResolveLocalPath` is undefined.

- [ ] **Step 3: Extract and reuse the resolver**

```go
func ResolveLocalPath(saveDir string, file FileInfo) (string, error) {
    root, err := filepath.Abs(saveDir)
    if err != nil { return "", err }
    remotePath := strings.TrimPrefix(path.Clean(file.Path), "/")
    if remotePath == "." || remotePath == "" { remotePath = safeLocalFilename(file.Name) }
    if remotePath == "" { return "", errors.New("remote file has no safe local name") }
    target := filepath.Clean(filepath.Join(root, filepath.FromSlash(remotePath)))
    rel, err := filepath.Rel(root, target)
    if err != nil { return "", err }
    if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
        return "", fmt.Errorf("remote path escapes save directory: %s", file.Path)
    }
    return target, nil
}

func (d *Downloader) localPath(file FileInfo) (string, error) {
    return ResolveLocalPath(d.root, file)
}
```

- [ ] **Step 4: Verify and commit**

```powershell
go test ./internal/httpdownload -count=1
git add -- internal/httpdownload/localpath.go internal/httpdownload/localpath_test.go internal/httpdownload/downloader.go
git commit -m "refactor: share received file path resolution"
```

Expected: tests PASS and one focused commit is created.

---

### Task 2: Desktop Availability and Secure Reveal Backend

**Files:**
- Create: `internal/receivedfile/receivedfile.go`
- Create: `internal/receivedfile/receivedfile_test.go`
- Create: `internal/receivedfile/reveal.go`
- Modify: `app.go:1-80,131-153`
- Create: `app_received_files_test.go`
- Regenerate: `frontend/wailsjs/go/main/App.js`, `App.d.ts`, `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Consumes: Task 1 `ResolveLocalPath`.
- Produces: `Check(saveDir, files) []State`, `Reveal(saveDir, file) error`, Wails `CheckReceivedFiles` and `RevealReceivedFile`.

- [ ] **Step 1: Write failing size/existence tests**

```go
func TestCheckRequiresExistingRegularMatchingFile(t *testing.T) {
    root := t.TempDir()
    os.WriteFile(filepath.Join(root, "ready.txt"), []byte("ready"), 0644)
    os.WriteFile(filepath.Join(root, "wrong.txt"), []byte("x"), 0644)
    os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0644)
    states := Check(root, []httpdownload.FileInfo{
        {Path: "/ready.txt", Size: 5}, {Path: "/wrong.txt", Size: 5},
        {Path: "/empty.txt", Size: 0}, {Path: "/missing.txt", Size: 0},
        {Path: "/folder", IsDir: true},
    })
    assertAvailable(t, states, "/ready.txt", true)
    assertAvailable(t, states, "/wrong.txt", false)
    assertAvailable(t, states, "/empty.txt", true)
    assertAvailable(t, states, "/missing.txt", false)
    assertAvailable(t, states, "/folder", false)
}
```

Run: `go test ./internal/receivedfile -count=1`

Expected: FAIL because the package does not exist.

- [ ] **Step 2: Implement checks without returning local paths**

```go
type State struct { RemotePath string `json:"remotePath"`; Available bool `json:"available"` }

func Check(saveDir string, files []httpdownload.FileInfo) []State {
    states := make([]State, 0, len(files))
    for _, file := range files {
        state := State{RemotePath: file.Path}
        target, err := httpdownload.ResolveLocalPath(saveDir, file)
        if err == nil && !file.IsDir {
            if info, statErr := os.Stat(target); statErr == nil && info.Mode().IsRegular() && info.Size() == file.Size {
                handle, openErr := os.Open(target)
                if openErr == nil {
                    state.Available = true
                    _ = handle.Close()
                }
            }
        }
        states = append(states, state)
    }
    return states
}
```

- [ ] **Step 3: Write failing reveal tests**

```go
func TestCommandForUsesDirectArguments(t *testing.T) {
    name, args, err := commandFor("windows", `C:\Downloads\safe name.exe`)
    if err != nil || name != "explorer.exe" || !reflect.DeepEqual(args, []string{"/select,", `C:\Downloads\safe name.exe`}) { t.Fatal(name, args, err) }
    name, args, err = commandFor("darwin", "/Users/me/Downloads/safe name")
    if err != nil || name != "open" || !reflect.DeepEqual(args, []string{"-R", "/Users/me/Downloads/safe name"}) { t.Fatal(name, args, err) }
}

func TestRevealRejectsChangedFileBeforeStartingProcess(t *testing.T) {
    root := t.TempDir()
    file := httpdownload.FileInfo{Path: "/tool.exe", Size: 4}
    os.WriteFile(filepath.Join(root, "tool.exe"), []byte("changed"), 0644)
    if err := Reveal(root, file); !errors.Is(err, ErrUnavailable) { t.Fatalf("error=%v", err) }
}
```

- [ ] **Step 4: Implement revalidation and reveal commands**

```go
var ErrUnavailable = errors.New("received file is no longer available")

func Reveal(saveDir string, file httpdownload.FileInfo) error {
    if states := Check(saveDir, []httpdownload.FileInfo{file}); len(states) != 1 || !states[0].Available { return ErrUnavailable }
    target, err := httpdownload.ResolveLocalPath(saveDir, file)
    if err != nil { return err }
    name, args, err := commandFor(runtime.GOOS, target)
    if err != nil { return err }
    if err = exec.Command(name, args...).Start(); err == nil || runtime.GOOS != "linux" { return err }
    return exec.Command("xdg-open", filepath.Dir(target)).Start()
}

func commandFor(goos, target string) (string, []string, error) {
    switch goos {
    case "windows": return "explorer.exe", []string{"/select,", target}, nil
    case "darwin": return "open", []string{"-R", target}, nil
    case "linux":
        uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(target)}).String()
        return "dbus-send", []string{"--session", "--dest=org.freedesktop.FileManager1", "--type=method_call", "/org/freedesktop/FileManager1", "org.freedesktop.FileManager1.ShowItems", "array:string:" + uri, "string:"}, nil
    default: return "", nil, fmt.Errorf("unsupported reveal platform: %s", goos)
    }
}
```

- [ ] **Step 5: Expose narrow App APIs and test them**

```go
type ReceivedFileState struct { RemotePath string `json:"remotePath"`; Available bool `json:"available"` }

func (a *App) CheckReceivedFiles(saveDir string, files []httpdownload.FileInfo) []ReceivedFileState {
    checked := receivedfile.Check(saveDir, files)
    out := make([]ReceivedFileState, len(checked))
    for i, state := range checked { out[i] = ReceivedFileState(state) }
    return out
}

func (a *App) RevealReceivedFile(saveDir string, file httpdownload.FileInfo) error {
    return receivedfile.Reveal(saveDir, file)
}
```

In `app_received_files_test.go`, create a matching temp file, verify `CheckReceivedFiles`, change the size, then verify `RevealReceivedFile` returns `ErrUnavailable` before process launch.

- [ ] **Step 6: Verify, generate bindings, and commit**

```powershell
go test ./internal/receivedfile . -count=1
wails build -clean
git add -- internal/receivedfile app.go app_received_files_test.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/models.ts
git commit -m "feat: add desktop received file checks"
```

Expected: tests and Wails build PASS; generated bindings include both new methods and `main.ReceivedFileState`.

---

### Task 3: Desktop Receive-List Interaction

**Files:**
- Modify: `frontend/src/App.tsx:1-110,700-820,990-1040,1320-1390,1640-1670,2316-2412`
- Modify: `frontend/src/App.css:700-790,1170-1210`

**Interfaces:**
- Consumes: Task 2 Wails APIs.
- Produces: current-connection `Map<string, {saveDir: string; size: number}>` and hover/focus reveal UI.

- [ ] **Step 1: Add imports/state and verify the incomplete slice fails to build**

```tsx
import {CheckReceivedFiles, RevealReceivedFile /* existing imports */} from '../wailsjs/go/main/App';
type ReceivedLocalState = {saveDir: string; size: number};
const [receivedLocalFiles, setReceivedLocalFiles] = useState<Map<string, ReceivedLocalState>>(new Map());
const receivedCheckGeneration = useRef(0);
```

Run from `frontend`: `npm run build`

Expected: FAIL on unused/incomplete state until the next steps wire refresh and rendering.

- [ ] **Step 2: Implement visible-directory refresh and lifecycle**

```tsx
async function refreshVisibleReceivedFiles(entries: VisibleEntry[], root = saveDir) {
  const generation = ++receivedCheckGeneration.current;
  const files = entries.filter((entry) => !entry.is_dir);
  const checked = await CheckReceivedFiles(root, files as any);
  if (generation !== receivedCheckGeneration.current) return;
  const paths = new Set(files.map((file) => normalizeRemotePath(file.path)));
  setReceivedLocalFiles((current) => {
    const next = new Map(current);
    paths.forEach((path) => next.delete(path));
    checked.forEach((state) => {
      const path = normalizeRemotePath(state.remotePath);
      const remote = files.find((file) => normalizeRemotePath(file.path) === path);
      if (state.available && remote) next.set(path, {saveDir: root, size: remote.size});
    });
    return next;
  });
}
```

Call after a directory list is applied and after a terminal download status followed by `refreshStatus`; never from progress. Clear the map and increment generation only after `StartTransfer` succeeds in receive mode. Do not clear on stop/disconnect.

While `receivedDownloadActive.current` or `status.downloading` is true, keep existing markers but disable the reveal control. Re-enable it only after the once-only terminal refresh completes.

- [ ] **Step 3: Add reveal error invalidation**

```tsx
async function revealReceivedFile(file: VisibleEntry) {
  const path = normalizeRemotePath(file.path);
  const local = receivedLocalFiles.get(path);
  if (!local) return;
  try { await RevealReceivedFile(local.saveDir, file as any); }
  catch (err) {
    setReceivedLocalFiles((current) => { const next = new Map(current); next.delete(path); return next; });
    setDownloadError(`${t.localFileUnavailable} ${localizeError(String(err))}`);
  }
}
```

Add Chinese/English keys for unavailable and platform-specific reveal tooltips selected from `runtimePlatform`.

- [ ] **Step 4: Render marker and hover/focus-only button**

```tsx
<span className={`type-icon file ${local ? 'local-available' : ''}`}>
  {local && <span className="local-available-dot" aria-hidden="true" />}
</span>
<strong className={local ? 'local-filename' : ''}>{file.name}</strong>
{local && <button className="reveal-received-file" aria-label={revealFileLabel} title={revealFileLabel} onClick={() => revealReceivedFile(file)}><span className="reveal-folder-icon" aria-hidden="true" /></button>}
```

```css
.remote-row { position: relative; }
.local-available-dot { position:absolute; right:-2px; bottom:-2px; width:6px; height:6px; border:1px solid #fff; border-radius:50%; background:#20a36a; }
.reveal-received-file { position:absolute; right:5px; opacity:0; pointer-events:none; transition:opacity 120ms ease; }
.remote-row:hover .reveal-received-file, .remote-row:focus-within .reveal-received-file { opacity:.78; pointer-events:auto; }
.reveal-received-file:hover, .reveal-received-file:focus-visible { opacity:1; }
```

Draw the folder/location glyph in CSS, include no visible button text, reserve space so metadata is not covered, and retain filename non-click behavior.

- [ ] **Step 5: Build, smoke test, and commit**

```powershell
npm run build
git add -- src/App.tsx src/App.css
git commit -m "feat: reveal received desktop files"
```

Expected: build PASS. In `wails dev`, verify batch-only updates, hover and keyboard focus, no filename open, and disconnect retention.

---
