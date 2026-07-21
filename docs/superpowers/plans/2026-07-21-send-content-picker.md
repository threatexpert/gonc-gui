# Send Content Picker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the desktop send screen's permanent file/folder controls with one content picker that adds files, folders, authored text, and supported clipboard content, while allowing a running share to become empty.

**Architecture:** Keep the frontend's ordered path list as the single transfer model. A focused Go package owns generated temporary files and platform clipboard extraction; `App` exposes narrow Wails bindings and the existing dynamic source accepts a valid empty `OSFileSource`. Pure TypeScript helpers define list proposals and picker state so synchronization and UI behavior can be tested outside React.

**Tech Stack:** Go 1.25, Wails v2.12, React 18, TypeScript 4.6, Win32 clipboard APIs through `golang.org/x/sys/windows`, BMP decoding through `golang.org/x/image/bmp`, Node's built-in test runner.

## Global Constraints

- All implementation changes are limited to `gonc-gui`.
- Do not modify or replace `gonetcat`; stop and request user approval if its behavior proves insufficient.
- Do not invoke PowerShell, AppleScript, `pbpaste`, `xclip`, `wl-paste`, or other external programs to read the clipboard.
- Starting a new send session requires at least one item; a running send session may be updated to an empty list.
- User-selected and clipboard-referenced files must never be deleted by temporary-file cleanup.
- Clipboard precedence is files/folders, then image, then Unicode text.
- Preserve Chinese and English UI copy and the row-based send list.

---

## File Map

- Create `internal/sharecontent/manager.go` and tests: generated-file lifecycle.
- Create `internal/sharecontent/clipboard*.go` and tests: platform clipboard extraction.
- Modify `internal/goncrunner/runner.go` and tests: valid empty dynamic source.
- Modify `app.go` and add `app_share_content_test.go`: Wails bindings and cleanup.
- Create `frontend/src/sendContentState.ts`, tests, and test config: pure state logic.
- Modify `frontend/src/App.tsx` and `frontend/src/App.css`: picker and list UI.
- Regenerate `frontend/wailsjs/go/main/App.js` and `App.d.ts`.
- Modify `README.md` and `README_zh.md`: capability matrix.

### Task 1: Generated Share File Manager

**Files:**
- Create: `internal/sharecontent/manager.go`
- Create: `internal/sharecontent/manager_test.go`

**Interfaces:**
- Produces: `NewManager() *Manager`
- Produces: `(*Manager).CreateText(prefix, text string) (string, error)`
- Produces: `(*Manager).CreatePNG(prefix string, image image.Image) (string, error)`
- Produces: `(*Manager).Release(paths []string) error`
- Produces: `(*Manager).Cleanup() error`
- Produces: `(*Manager).Owns(path string) bool`

- [ ] **Step 1: Write failing manager tests**

Test unique basename patterns, exact UTF-8 bytes, decodable PNG output, owned-file release, external-file preservation, and root cleanup. Use a test-only constructor:

```go
func TestManagerCreatesUniqueUTF8Text(t *testing.T) {
    m := newManagerAt(filepath.Join(t.TempDir(), "generated"))
    first, err := m.CreateText("text", "浣犲ソ\nhello")
    if err != nil { t.Fatal(err) }
    second, err := m.CreateText("text", "second")
    if err != nil { t.Fatal(err) }
    if first == second { t.Fatal("generated paths collided") }
    if got, _ := os.ReadFile(first); string(got) != "浣犲ソ\nhello" {
        t.Fatalf("content = %q", got)
    }
    pattern := regexp.MustCompile(`^text-\d{8}-\d{6}\.\d{3}-[0-9a-f]{12}\.txt$`)
    if !pattern.MatchString(filepath.Base(first)) { t.Fatalf("name = %q", filepath.Base(first)) }
}

func TestManagerNeverDeletesUnownedPath(t *testing.T) {
    m := newManagerAt(filepath.Join(t.TempDir(), "generated"))
    external := filepath.Join(t.TempDir(), "user.txt")
    if err := os.WriteFile(external, []byte("user"), 0600); err != nil { t.Fatal(err) }
    if err := m.Release([]string{external}); err != nil { t.Fatal(err) }
    if _, err := os.Stat(external); err != nil { t.Fatalf("external removed: %v", err) }
}
```

- [ ] **Step 2: Verify tests fail**

Run: `go test ./internal/sharecontent -run Manager -v`

Expected: FAIL because the package and manager do not exist.

- [ ] **Step 3: Implement the manager**

Use a mutex-protected registry and lazy root creation. Generate six random bytes with `crypto/rand`, encode 12 lowercase hex characters, write mode `0600`, and register only after successful close.

```go
type Manager struct {
    mu    sync.Mutex
    root  string
    owned map[string]struct{}
}

func (m *Manager) CreateText(prefix, text string) (string, error) {
    return m.create(prefix, ".txt", func(file *os.File) error {
        _, err := io.WriteString(file, text)
        return err
    })
}
```

`Release` must resolve absolute paths, require registry membership and `filepath.Rel` containment, ignore unowned paths, and retain registry entries when deletion fails. `Cleanup` removes only the manager-owned resolved root.

- [ ] **Step 4: Verify and commit**

Run: `go test ./internal/sharecontent -v`

Expected: PASS.

```powershell
git add internal/sharecontent/manager.go internal/sharecontent/manager_test.go
git commit -m "feat: manage generated share files"
```

### Task 2: Native Clipboard Extraction

**Files:**
- Create: `internal/sharecontent/clipboard.go`
- Create: `internal/sharecontent/clipboard_windows.go`
- Create: `internal/sharecontent/clipboard_windows_test.go`
- Create: `internal/sharecontent/clipboard_other.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: manager creation methods from Task 1.
- Produces: `ClipboardKind` constants `ClipboardFiles`, `ClipboardImage`, `ClipboardText`.
- Produces: `ClipboardResult struct { Paths []string; Kind ClipboardKind }`.
- Produces: `ErrClipboardEmpty`, `ErrClipboardUnsupported`, `ErrClipboardBusy`.
- Produces: `(*Manager).ImportNativeClipboard() (ClipboardResult, error)`.

- [ ] **Step 1: Write failing helper tests**

Test precedence independently of the real clipboard, existing-path filtering, and DIB decoding. The Windows DIB test encodes a 2x2 image with `bmp.Encode`, removes the 14-byte BMP file header, calls `decodeDIB`, and checks dimensions.

```go
func TestChooseClipboardPayloadPrefersFilesThenImageThenText(t *testing.T) {
    got := chooseClipboardPayload(clipboardFormats{files: []string{"a"}, png: []byte{1}, text: "text"})
    if got.kind != ClipboardFiles { t.Fatalf("kind = %q", got.kind) }
    got = chooseClipboardPayload(clipboardFormats{png: []byte{1}, text: "text"})
    if got.kind != ClipboardImage { t.Fatalf("kind = %q", got.kind) }
    got = chooseClipboardPayload(clipboardFormats{text: "text"})
    if got.kind != ClipboardText { t.Fatalf("kind = %q", got.kind) }
}
```

- [ ] **Step 2: Verify tests fail**

Run: `go test ./internal/sharecontent -run 'Clipboard|DIB' -v`

Expected: FAIL because clipboard types and helpers do not exist.

- [ ] **Step 3: Implement shared and non-Windows behavior**

Define result types, sentinel errors, and `existingPaths` in `clipboard.go`. In `clipboard_other.go`:

```go
//go:build !windows

package sharecontent

func (m *Manager) ImportNativeClipboard() (ClipboardResult, error) {
    return ClipboardResult{}, ErrClipboardUnsupported
}
```

- [ ] **Step 4: Implement Windows extraction**

Use `windows.NewLazySystemDLL` procedures for `OpenClipboard`, `CloseClipboard`, `IsClipboardFormatAvailable`, `GetClipboardData`, `RegisterClipboardFormatW`, `GlobalLock`, `GlobalSize`, `GlobalUnlock`, and `DragQueryFileW`. Retry clipboard opening five times at 10 ms; always close it.

Use exact precedence:

```go
func (m *Manager) ImportNativeClipboard() (ClipboardResult, error) {
    if err := openClipboardWithRetry(); err != nil { return ClipboardResult{}, err }
    defer procCloseClipboard.Call()

    if paths, ok, err := readHDrop(); ok || err != nil {
        paths = existingPaths(paths)
        if err != nil { return ClipboardResult{}, err }
        if len(paths) > 0 { return ClipboardResult{Paths: paths, Kind: ClipboardFiles}, nil }
    }
    if img, ok, err := readClipboardImage(); ok || err != nil {
        if err != nil { return ClipboardResult{}, err }
        path, err := m.CreatePNG("clipboard-image", img)
        return ClipboardResult{Paths: []string{path}, Kind: ClipboardImage}, err
    }
    if text, ok, err := readUnicodeText(); ok || err != nil {
        if err != nil { return ClipboardResult{}, err }
        if text != "" {
            path, err := m.CreateText("clipboard-text", text)
            return ClipboardResult{Paths: []string{path}, Kind: ClipboardText}, err
        }
    }
    return ClipboardResult{}, ErrClipboardEmpty
}
```

Try registered `PNG`, then `CF_DIBV5`, then `CF_DIB`. Validate DIB header, dimensions, depth, compression, palette and buffer bounds; prepend a 14-byte BMP header and decode with `bmp.Decode`. Add `golang.org/x/image v0.27.0`, whose module declares Go 1.23 and is already available in the workspace module cache.

- [ ] **Step 5: Verify Windows and other builds**

```powershell
go test ./internal/sharecontent -v
go test ./...
$env:GOOS='darwin'; go test ./internal/sharecontent; Remove-Item Env:GOOS
$env:GOOS='linux'; go test ./internal/sharecontent; Remove-Item Env:GOOS
```

Expected: PASS; non-Windows code contains no external-process fallback.

- [ ] **Step 6: Commit**

```powershell
git add go.mod go.sum internal/sharecontent/clipboard.go internal/sharecontent/clipboard_windows.go internal/sharecontent/clipboard_windows_test.go internal/sharecontent/clipboard_other.go
git commit -m "feat: import native clipboard content"
```

### Task 3: App Bindings and Empty Running Shares

**Files:**
- Modify: `internal/goncrunner/runner.go`
- Modify: `internal/goncrunner/runner_test.go`
- Modify: `app.go`
- Create: `app_share_content_test.go`
- Regenerate: `frontend/wailsjs/go/main/App.js`
- Regenerate: `frontend/wailsjs/go/main/App.d.ts`

**Interfaces:**
- Produces: `CreateTextShare(text string) (string, error)`.
- Produces: `ImportClipboard() (sharecontent.ClipboardResult, error)`.
- Produces: `ReleaseGeneratedSharePaths(paths []string) error`.
- Changes `UpdateSharePaths` to accept an empty slice for a running sender.

- [ ] **Step 1: Write failing empty-source and App tests**

```go
func TestDynamicFileSourceCanBecomeEmptyAndRecover(t *testing.T) {
    file := filepath.Join(t.TempDir(), "one.txt")
    if err := os.WriteFile(file, []byte("one"), 0600); err != nil { t.Fatal(err) }
    source, err := newDynamicFileSource([]string{file})
    if err != nil { t.Fatal(err) }
    if err := source.UpdatePaths(nil); err != nil { t.Fatal(err) }
    entries, err := source.ReadDir("/")
    if err != nil || len(entries) != 0 { t.Fatalf("entries=%v err=%v", entries, err) }
    if info, err := source.Stat("/"); err != nil || !info.IsDir() { t.Fatalf("root=%v err=%v", info, err) }
    if err := source.UpdatePaths([]string{file}); err != nil { t.Fatal(err) }
}
```

Also test empty authored text rejection, whitespace preservation, Wails text fallback only for `ErrClipboardUnsupported`, and removal of App's empty-slice guard.

- [ ] **Step 2: Verify tests fail**

Run: `go test ./internal/goncrunner . -run 'DynamicFileSourceCanBecomeEmpty|TextShare|Clipboard|UpdateSharePaths' -v`

Expected: FAIL.

- [ ] **Step 3: Implement valid empty source in gonc-gui only**

```go
func (s *dynamicFileSource) UpdatePaths(paths []string) error {
    var next *httpfileshare.OSFileSource
    var err error
    if len(paths) == 0 {
        next = &httpfileshare.OSFileSource{}
    } else {
        next, err = httpfileshare.NewOSFileSource(paths)
        if err != nil { return err }
    }
    s.mu.Lock()
    s.source = next
    s.mu.Unlock()
    return nil
}
```

Do not change initial `validateRequest`.

- [ ] **Step 4: Add App ownership and bindings**

Add `shareContent *sharecontent.Manager` to `App` and initialize it in `NewApp`. Implement:

```go
func (a *App) CreateTextShare(text string) (string, error) {
    if text == "" { return "", errors.New("text content is empty") }
    return a.shareContent.CreateText("text", text)
}

func (a *App) ImportClipboard() (sharecontent.ClipboardResult, error) {
    result, err := a.shareContent.ImportNativeClipboard()
    if err == nil { return result, nil }
    if !errors.Is(err, sharecontent.ErrClipboardUnsupported) {
        return sharecontent.ClipboardResult{}, err
    }
    text, err := wailsruntime.ClipboardGetText(a.ctx)
    if err != nil { return sharecontent.ClipboardResult{}, fmt.Errorf("read clipboard text: %w", err) }
    if text == "" { return sharecontent.ClipboardResult{}, sharecontent.ErrClipboardUnsupported }
    path, err := a.shareContent.CreateText("clipboard-text", text)
    return sharecontent.ClipboardResult{Paths: []string{path}, Kind: sharecontent.ClipboardText}, err
}

func (a *App) ReleaseGeneratedSharePaths(paths []string) error {
    return a.shareContent.Release(paths)
}
```

Remove the empty guard from `App.UpdateSharePaths`. After transfer cleanup, call manager cleanup and combine errors with `errors.Join`.

- [ ] **Step 5: Regenerate bindings**

Run: `wails generate module`

Expected: generated declarations for all three methods. If unavailable, use `go run github.com/wailsapp/wails/v2/cmd/wails@v2.12.0 generate module`; do not hand-edit before proving generation unavailable.

- [ ] **Step 6: Verify and commit**

Run: `go test ./...`

Expected: PASS and no `gonetcat` source modification.

```powershell
git add app.go app_share_content_test.go internal/goncrunner/runner.go internal/goncrunner/runner_test.go frontend/wailsjs/go/main/App.js frontend/wailsjs/go/main/App.d.ts
git commit -m "feat: expose send content bindings"
```

### Task 4: Transactional Frontend State

**Files:**
- Create: `frontend/src/sendContentState.ts`
- Create: `frontend/tests/sendContentState.test.ts`
- Create: `frontend/tsconfig.send-content-test.json`
- Modify: `frontend/package.json`

**Interfaces:**
- Produces: `appendUniquePaths(current, added): string[]`.
- Produces: `removePath(current, removed): string[]`.
- Produces: `AddPickerState = 'closed' | 'choose' | 'text'`.
- Produces: `textCanSubmit(text): boolean`.
- Produces: `dropHintMode(paths): 'empty' | 'compact'`.

- [ ] **Step 1: Write failing pure tests**

```ts
test('paths can be cleared while preserving deterministic append order', () => {
  assert.deepEqual(appendUniquePaths(['a'], ['b', 'a', 'c']), ['a', 'b', 'c']);
  assert.deepEqual(removePath(['a'], 'a'), []);
});
test('authored text rejects only an empty string', () => {
  assert.equal(textCanSubmit(''), false);
  assert.equal(textCanSubmit('   '), true);
});
```

Also test picker transitions and hint modes.

- [ ] **Step 2: Verify failure**

Run from `frontend`: `npm run test:send-content`

Expected: FAIL because script/config/module do not exist.

- [ ] **Step 3: Implement helpers and configuration**

```ts
export type AddPickerState = 'closed' | 'choose' | 'text';
export const appendUniquePaths = (current: string[], added: string[]) =>
  Array.from(new Set([...current, ...added]));
export const removePath = (current: string[], removed: string) =>
  current.filter((path) => path !== removed);
export const textCanSubmit = (text: string) => text.length > 0;
export const dropHintMode = (paths: string[]) => paths.length === 0 ? 'empty' : 'compact';
```

Mirror the existing Node-test tsconfig for the two new files. Add `test:send-content` and aggregate `test` scripts.

- [ ] **Step 4: Verify and commit**

Run: `npm run test:send-content`

Expected: PASS.

```powershell
git add frontend/src/sendContentState.ts frontend/tests/sendContentState.test.ts frontend/tsconfig.send-content-test.json frontend/package.json
git commit -m "test: define send content state"
```

### Task 5: Picker UI and Live Synchronization

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.css`
- Modify: `frontend/tests/sendContentState.test.ts`

**Interfaces:**
- Consumes all Task 3 bindings and Task 4 helpers.
- Preserves `sharePaths` as the displayed, last-successfully-synchronized list.

- [ ] **Step 1: Add failing source-contract tests**

Read `App.tsx` and require four localized choices, `add-picker-backdrop`, clear-all, empty and compact hints, and transactional ordering. Assert the old effect that returns for an empty list is gone.

```ts
const app = readFileSync('src/App.tsx', 'utf8');
assert.match(app, /addFileChoice/);
assert.match(app, /addFolderChoice/);
assert.match(app, /addTextChoice/);
assert.match(app, /addClipboardChoice/);
assert.match(app, /className="add-picker-backdrop"/);
assert.match(app, /function clearSharePaths/);
assert.doesNotMatch(app, /if \(sharePaths\.length === 0\)\s*\{\s*return;\s*\}\s*UpdateSharePaths/);
```

- [ ] **Step 2: Verify failure**

Run: `npm run test:send-content`

Expected: FAIL.

- [ ] **Step 3: Implement localization and transactional updates**

Add Chinese/English copy for Add, picker title, four choices, clear, text modal, drop hint, and clipboard errors. Replace direct list mutations with:

```ts
async function commitSharePaths(proposed: string[], generatedOnFailure: string[] = []) {
  setError('');
  if (sendRunning) {
    try {
      await UpdateSharePaths(proposed);
    } catch (err) {
      if (generatedOnFailure.length > 0) {
        await ReleaseGeneratedSharePaths(generatedOnFailure).catch(() => undefined);
      }
      setError(`\${t.shareUpdateFailed} \${localizeError(String(err))}`);
      return false;
    }
  }
  setSharePaths(proposed);
  return true;
}
```

Remove the share-list synchronization effect. File/folder selection, drag/drop, single remove, and clear-all must propose a full list and await this function. Release removed paths after successful commit; backend ownership checks protect user files.

- [ ] **Step 4: Implement picker flows**

Use `addPickerState`, `authoredText`, `pickerError`, and pending state. Text submission calls `CreateTextShare`; clipboard calls `ImportClipboard`. Render accessible dialogs with Escape handling, close button, autofocus, backdrop-target checking, and disabled controls while pending.

- [ ] **Step 5: Implement final list markup and CSS**

Always render one `+ Add` button. Keep row layout and per-item remove. Put clear-all at upper right. Render a centered empty drop hint or quiet compact hint after populated rows. Add four-column picker options, focus-visible styles, and two-column mobile fallback.

- [ ] **Step 6: Verify and commit**

```powershell
npm test
npm run build
```

Expected: tests PASS and production build exits 0.

```powershell
git add frontend/src/App.tsx frontend/src/App.css frontend/tests/sendContentState.test.ts
git commit -m "feat: add send content picker"
```

### Task 6: Cleanup, Documentation, and Full Verification

**Files:**
- Modify if a failing test requires it: `app.go`
- Modify if a failing test requires it: `internal/sharecontent/manager.go`
- Modify if a failing test requires it: `frontend/src/App.tsx`
- Modify: `README.md`
- Modify: `README_zh.md`

- [ ] **Step 1: Add lifecycle integration tests**

Test generated file 鈫?synchronized list 鈫?empty synchronized list 鈫?release 鈫?removed file. Test shutdown cleanup for remaining owned files. Never weaken ownership validation.

- [ ] **Step 2: Verify boundary behavior**

Run: `go test . ./internal/sharecontent ./internal/goncrunner -run 'Generated|Cleanup|DynamicFileSource' -v`

Expected: PASS. If zero-value `OSFileSource` cannot serve an empty root, stop and request approval before any `gonetcat` change.

- [ ] **Step 3: Document the capability matrix**

Document File/Folder/Text on all desktop platforms; clipboard text on all; native clipboard files/images on Windows; explicit unsupported message elsewhere. Mention Add picker, drag/drop, single remove, and clear-all.

- [ ] **Step 4: Run full verification**

```powershell
git diff --check
go test ./...
Set-Location frontend
npm test
npm run build
Set-Location ..
git status --short
```

Expected: all checks pass; only intended files under `D:\threatexpert.cn\open\gonc-gui` changed.

- [ ] **Step 5: Perform visual smoke test**

Run `wails dev`. Verify empty hint, one Add button, four-option picker, text creation, clipboard import, compact populated hint, per-row removal, clear-all, running share becoming empty after receiver refresh, Escape/focus, Chinese/English, and width below 720 px.

- [ ] **Step 6: Commit final documentation/corrections**

```powershell
git add README.md README_zh.md
git commit -m "docs: describe send content picker"
```

Include other files only if Step 2 exposed a necessary correction; do not create an empty commit.
