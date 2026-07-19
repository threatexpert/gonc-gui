# Inline QR Visual Refinement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make desktop and Android inline passphrase QR controls smaller and quieter, replace real-QR masking with a passphrase-independent blurred pseudo-QR, and clear desktop masking only after a stopped run's passphrase actually changes.

**Architecture:** Keep the existing session/run isolation and enlarged clear-QR dialogs. Desktop adds one pure mask-reset rule, renders a decorative CSS matrix instead of the real QR while masked, and tightens the two-column layout. Android changes the shared QR view to a `104dp`/`2dp` compact presentation and generates a fixed decorative masked bitmap without encoding the passphrase.

**Tech Stack:** React 18, TypeScript 4.6, CSS, Node test runner, Android Java (minSdk 26), ZXing, Gradle/JUnit 4, Go/Wails v2.

## Global Constraints

- Scope is desktop file send/receive and Android file send/receive only; do not change VPN screens, QR scanning, connection protocols, passphrase payloads, or enlarged clear-QR dialog behavior.
- Desktop file send/receive passphrase rows must not contain a QR button; VPN QR buttons remain unchanged.
- Desktop inline QR footprint is exactly `112px × 112px` and its bottom edge aligns with the bottom of the `Use UDP protocol` row.
- Masked inline presentation must contain no passphrase-derived pixels or bytes; show a fixed blurred decorative pseudo-QR and no opaque center square.
- Stopping alone does not clear the desktop mask; a different normalized passphrase while stopped clears it, while the same passphrase or a change during a running session does not.
- Android send and receive inline QR content is exactly `104dp`; shared frame padding is exactly `2dp`.
- Android send active QR is horizontally centered; its idle passphrase UI remains unchanged.
- Keep `clientRunId`, asynchronous generation ownership, start serialization, Android run guards, cache byte budget, clear-QR caching security, and receive QR retirement policy unchanged.
- Do not create a Git branch or worktree; work directly in the current checkout as explicitly requested.

---

## File structure

- `frontend/src/inlineQrState.ts`: add the pure stopped-passphrase mask reset rule.
- `frontend/tests/inlineQrState.test.ts`: cover stop-only, different-passphrase, same-normalized-passphrase, and running-session cases.
- `frontend/src/App.tsx`: track previous file-transfer passphrases, clear latches only on valid changes, and remove only file-mode QR buttons.
- `frontend/src/TransferInlineQr.tsx`: hide the real QR image while masked and render a decorative pseudo-QR element.
- `frontend/src/App.css`: fixed `112px` footprint, UDP-bottom alignment, compact decorative blur, and removal of the center cover.
- `android/app/src/main/java/cn/threatexpert/gonc/TransferInlineQrState.java`: make masked production cache identity independent of passphrase and expose exact shared size/padding policy.
- `android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java`: test passphrase-independent masked identity and exact `104dp`/`2dp` constants.
- `android/app/src/main/java/cn/threatexpert/gonc/PassphraseQrView.java`: compact padding and fixed decorative blurred masked bitmap.
- `android/app/src/main/java/cn/threatexpert/gonc/SendController.java`: use shared `104dp` size while preserving centered active layout.
- `android/app/src/main/java/cn/threatexpert/gonc/ReceiveController.java`: consume the shared `104dp` size without changing receive state behavior.

---

### Task 1: Desktop mask reset, action cleanup, and compact pseudo-QR

**Files:**
- Modify: `frontend/src/inlineQrState.ts`
- Modify: `frontend/tests/inlineQrState.test.ts`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/TransferInlineQr.tsx`
- Modify: `frontend/src/App.css`

**Interfaces:**
- Produces: `maskAfterPassphraseUpdate(masked: boolean, running: boolean, previousPassphrase: string, nextPassphrase: string): boolean`
- Consumes: existing `normalizedQrPassphrase`, `TransferInlineQr`, `showPasswordQr`, and send/receive connected latches
- Requirement: file send/receive QR action buttons are removed; VPN buttons and enlarged dialog calls are unchanged

- [ ] **Step 1: Write failing mask-reset tests**

Extend `frontend/tests/inlineQrState.test.ts`:

```ts
import {
  maskAfterPassphraseUpdate,
  // existing imports remain
} from '../src/inlineQrState.js';

test('stopped transfer mask clears only for a different normalized passphrase', () => {
  assert.equal(maskAfterPassphraseUpdate(true, false, 'old', 'old'), true);
  assert.equal(maskAfterPassphraseUpdate(true, false, ' old ', 'old'), true);
  assert.equal(maskAfterPassphraseUpdate(true, false, 'old', 'new'), false);
});

test('running transfer passphrase update cannot clear its mask', () => {
  assert.equal(maskAfterPassphraseUpdate(true, true, 'old', 'new'), true);
  assert.equal(maskAfterPassphraseUpdate(false, false, 'old', 'new'), false);
});
```

- [ ] **Step 2: Run the focused frontend test to verify RED**

Run from `frontend/`:

`npm run test:inline-qr`

Expected: TypeScript compile FAIL because `maskAfterPassphraseUpdate` is not exported.

- [ ] **Step 3: Implement the minimal pure mask rule**

Add to `frontend/src/inlineQrState.ts`:

```ts
export function maskAfterPassphraseUpdate(
  masked: boolean,
  running: boolean,
  previousPassphrase: string,
  nextPassphrase: string,
) {
  if (!masked || running) {
    return masked;
  }
  return normalizedQrPassphrase(previousPassphrase)
    === normalizedQrPassphrase(nextPassphrase);
}
```

- [ ] **Step 4: Verify GREEN before UI integration**

Run: `npm run test:inline-qr`

Expected: all existing tests plus the two new mask-reset tests PASS.

- [ ] **Step 5: Integrate previous-passphrase tracking without clearing on stop**

In `frontend/src/App.tsx`, add refs beside the existing transfer QR latch state:

```tsx
const previousSendQrPassphrase = useRef(sendPassword);
const previousReceiveQrPassphrase = useRef(receivePassword);
```

Add effects after the existing state/effect declarations:

```tsx
useEffect(() => {
  const previous = previousSendQrPassphrase.current;
  setSendQrHasConnected((masked) => maskAfterPassphraseUpdate(
    masked, sendRunning, previous, sendPassword,
  ));
  previousSendQrPassphrase.current = sendPassword;
}, [sendPassword, sendRunning]);

useEffect(() => {
  const previous = previousReceiveQrPassphrase.current;
  setReceiveQrHasConnected((masked) => maskAfterPassphraseUpdate(
    masked, receiveRunning, previous, receivePassword,
  ));
  previousReceiveQrPassphrase.current = receivePassword;
}, [receivePassword, receiveRunning]);
```

Import `maskAfterPassphraseUpdate`. Do not clear either latch in `stop()`; the effects preserve the mask when only `sendRunning`/`receiveRunning` becomes false because the passphrase is unchanged. Manual edits, generation, paste, and scan already flow through the corresponding password setters.

- [ ] **Step 6: Remove only the desktop file-mode QR action buttons**

In the `mode === 'send'` and `mode === 'receive'` branches of `passphraseField`, delete these two controls:

```tsx
<button className="secondary" disabled={!sendPassword} onClick={showPasswordQr}>{t.qr}</button>
<button className="secondary" disabled={!receivePassword} onClick={showPasswordQr}>{t.qr}</button>
```

Keep the `vpnServerPassword` and `vpnClientPassword` QR buttons byte-for-byte behaviorally unchanged. Keep `showPasswordQr` because the inline QR still calls it.

- [ ] **Step 7: Render a passphrase-independent decorative element while masked**

Change the content of `TransferInlineQr` to:

```tsx
{visibleDataUrl && (
  masked
    ? <span className="transfer-inline-qr-mask" aria-hidden="true" />
    : <img src={visibleDataUrl} alt="" />
)}
```

The decorative span contains no data URL and no passphrase-derived attributes. Leave `onClick={onActivate}` and the existing enlarged-dialog path unchanged.

- [ ] **Step 8: Tighten desktop sizing, alignment, and mask CSS**

Update the QR layout rules in `frontend/src/App.css`:

```css
.transfer-inline-qr-column {
  display: grid;
  width: 112px;
  align-self: end;
  place-items: end center;
}

.transfer-inline-qr {
  position: relative;
  display: grid;
  box-sizing: border-box;
  width: 112px;
  height: 112px;
  place-items: center;
  overflow: hidden;
  border: 1px solid #d8e2ee;
  padding: 0;
  background: #fff;
}

.transfer-inline-qr-mask {
  width: 88px;
  height: 88px;
  background:
    repeating-conic-gradient(#101826 0 25%, #fff 0 50%) 0 0 / 12px 12px;
  filter: blur(4px);
  transform: scale(.94);
}
```

Delete `.transfer-inline-qr.masked img` and `.transfer-inline-qr.masked::after`. Remove the redundant narrow-window QR width override or leave it with the same exact `112px` values; do not stack the two columns.

- [ ] **Step 9: Run desktop verification**

Run:

```powershell
Push-Location frontend
npm run test:inline-qr
npm run build
Pop-Location
go test ./... -count=1
git diff --check
```

Expected: all commands exit 0; frontend tests include the new reset cases; Vite transforms the production bundle; Go tests remain green.

- [ ] **Step 10: Commit Task 1**

```bash
git add frontend/src/inlineQrState.ts frontend/tests/inlineQrState.test.ts frontend/src/App.tsx frontend/src/TransferInlineQr.tsx frontend/src/App.css
git commit -m "fix: refine desktop inline QR presentation"
```

---

### Task 2: Android compact frame and passphrase-independent mask

**Files:**
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/TransferInlineQrState.java`
- Modify: `android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/PassphraseQrView.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/SendController.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/ReceiveController.java`

**Interfaces:**
- Produces: `inlineQrSizeDp()` returning `104`
- Produces: `inlineQrFramePaddingDp()` returning `2`
- Changes: `productionCacheKey(passphrase, pixelSize, true)` must be equal for every non-empty passphrase of the same size
- Consumes: existing 4 MiB bitmap cache, 512px cap, clear-cache lifecycle, error callbacks, and enlarged dialog callbacks

- [ ] **Step 1: Write failing Android policy tests**

Extend `TransferInlineQrStateTest.java`:

```java
@Test public void inlineQrUsesSharedCompactDimensions() {
    assertEquals(104, TransferInlineQrState.inlineQrSizeDp());
    assertEquals(2, TransferInlineQrState.inlineQrFramePaddingDp());
}

@Test public void maskedBitmapCacheIdentityDoesNotDependOnPassphrase() {
    assertEquals(
            TransferInlineQrState.productionCacheKey("first-secret", 416, true),
            TransferInlineQrState.productionCacheKey("second-secret", 416, true));
    assertNotEquals(
            TransferInlineQrState.productionCacheKey("first-secret", 416, false),
            TransferInlineQrState.productionCacheKey("second-secret", 416, false));
}
```

- [ ] **Step 2: Run the focused Android test to verify RED**

Run from `android/`:

`.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.TransferInlineQrStateTest`

Expected: compile FAIL because the compact dimension helpers do not exist and the masked cache keys still differ by passphrase digest.

- [ ] **Step 3: Implement exact size/padding and masked cache identity**

Add to `TransferInlineQrState.java`:

```java
private static final int INLINE_QR_SIZE_DP = 104;
private static final int INLINE_QR_FRAME_PADDING_DP = 2;
private static final String MASKED_PATTERN_ID = "decorative-inline-qr-mask";

static int inlineQrSizeDp() {
    return INLINE_QR_SIZE_DP;
}

static int inlineQrFramePaddingDp() {
    return INLINE_QR_FRAME_PADDING_DP;
}
```

When building `BitmapCacheKey`, hash `MASKED_PATTERN_ID` instead of the normalized passphrase when `masked == true`. Clear/unmasked keys continue hashing `passphrase.trim()` exactly as before.

- [ ] **Step 4: Verify the Android policy tests GREEN**

Run:

`.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.TransferInlineQrStateTest`

Expected: all `TransferInlineQrStateTest` cases PASS.

- [ ] **Step 5: Replace real-QR masking with a decorative bitmap**

In `PassphraseQrView.createEntry`, preserve empty handling, then bypass ZXing for masked presentation:

```java
if (passphrase.isEmpty()) {
    return new CacheEntry(placeholder(pixelSize), false);
}
if (masked) {
    return new CacheEntry(decorativeMaskedQr(pixelSize), false);
}
try {
    return new CacheEntry(QrCodes.encode(passphrase, pixelSize), false);
} catch (WriterException error) {
    return new CacheEntry(placeholder(pixelSize), true);
}
```

Replace the existing `masked(Bitmap clear, int pixelSize)` method with a deterministic, passphrase-independent method:

```java
private static Bitmap decorativeMaskedQr(int pixelSize) {
    Bitmap pattern = Bitmap.createBitmap(29, 29, Bitmap.Config.ARGB_8888);
    Canvas canvas = new Canvas(pattern);
    canvas.drawColor(Color.WHITE);
    Paint ink = new Paint();
    ink.setColor(Color.rgb(16, 24, 38));
    int seed = 0x51f15e;
    for (int y = 0; y < 29; y++) {
        for (int x = 0; x < 29; x++) {
            seed = seed * 1103515245 + 12345;
            if (((seed >>> 28) & 1) == 1) {
                canvas.drawRect(x, y, x + 1, y + 1, ink);
            }
        }
    }
    Bitmap sampled = Bitmap.createScaledBitmap(
            pattern, Math.min(MASK_SAMPLE_SIZE, pixelSize),
            Math.min(MASK_SAMPLE_SIZE, pixelSize), true);
    return Bitmap.createScaledBitmap(sampled, pixelSize, pixelSize, true);
}
```

Do not call `QrCodes.encode` on the masked path. Do not draw a center cover. The fixed seed is decorative only and contains no passphrase-derived input.

- [ ] **Step 6: Apply the exact compact frame padding**

In `PassphraseQrView.create`, replace:

```java
int padding = ui.dp(6);
```

with:

```java
int padding = ui.dp(TransferInlineQrState.inlineQrFramePaddingDp());
```

Keep the border, rounded background, click callback, disabled failure behavior, cache budget, and `ImageView` display layout unchanged.

- [ ] **Step 7: Use one shared `104dp` size in both controllers**

In `SendController.passwordField()` use:

```java
PassphraseQrView.create(
        host.context(), u, password, TransferInlineQrState.inlineQrSizeDp(),
        sendQrHasConnected,
        () -> host.showPassphraseQr(password.trim()),
        () -> host.toast(R.string.inline_qr_generation_failed))
```

Keep `qrOnly.setGravity(Gravity.CENTER_HORIZONTAL)`.

In `ReceiveController.receiveSessionBarView()` replace the literal `104` with `TransferInlineQrState.inlineQrSizeDp()`. Do not change `showReceiveQr`, `receiveQrRetired`, or the fallback Passphrase button.

- [ ] **Step 8: Run Android verification**

Run from `android/`:

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.TransferInlineQrStateTest
.\gradlew.bat testDebugUnitTest
.\gradlew.bat assembleDebug
```

Expected: focused and full unit tests PASS; `assembleDebug` reports `BUILD SUCCESSFUL`; the debug APK exists.

- [ ] **Step 9: Commit Task 2**

```bash
git add android/app/src/main/java/cn/threatexpert/gonc/TransferInlineQrState.java android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java android/app/src/main/java/cn/threatexpert/gonc/PassphraseQrView.java android/app/src/main/java/cn/threatexpert/gonc/SendController.java android/app/src/main/java/cn/threatexpert/gonc/ReceiveController.java
git commit -m "fix: compact Android inline QR presentation"
```

---

### Task 3: Cross-platform final verification

**Files:**
- Modify only if verification exposes a real defect or the Wails package checksum legitimately changes.

**Interfaces:**
- Verifies the desktop and Android refinements together without changing session protocols or VPN behavior.

- [ ] **Step 1: Verify formatting and clean diffs**

Run:

```powershell
$formatted = gofmt -d app.go app_transfer_run_test.go
if ($formatted) { $formatted; exit 1 }
git diff --check
git status --short
```

Expected: no Go formatting output, no whitespace errors, and no uncommitted semantic changes.

- [ ] **Step 2: Run complete desktop tests and builds**

```powershell
go test ./... -count=1
Push-Location frontend
npm run test:inline-qr
npm run build
Pop-Location
wails build -clean
```

Expected: Go tests PASS; all inline QR tests PASS; Vite and Wails builds exit 0; `build/bin/gonc-gui.exe` exists. Inspect generated `frontend/wailsjs/go/models.ts` noise before restoring it, and preserve `clientRunId` lines.

- [ ] **Step 3: Run complete Android tests and build without cache shortcuts**

```powershell
Push-Location android
.\gradlew.bat testDebugUnitTest assembleDebug --rerun-tasks
Pop-Location
```

Expected: `BUILD SUCCESSFUL`, zero JUnit failures/errors, and `android/app/build/outputs/apk/debug/app-debug.apk` exists.

- [ ] **Step 4: Perform static and visual state checks**

Desktop checklist:

- send/receive passphrase rows have no QR button; VPN rows retain theirs;
- inline control is `112px × 112px` and bottom-aligned with the UDP row;
- masked main-screen DOM renders the decorative span, not the real QR image;
- no center cover is present;
- stop alone stays masked; changing to a different normalized passphrase clears it;
- masked click still opens the clear enlarged dialog.

Android checklist:

- receive QR content remains `104dp` with `2dp` frame padding;
- send active QR is `104dp`, horizontally centered, and idle UI is unchanged;
- masked path never calls `QrCodes.encode` and draws no center cover;
- masked click still opens the clear enlarged dialog;
- receive retirement behavior is unchanged.

If no interactive desktop/Android environment is used, record visual smoke as not executed rather than passed.

- [ ] **Step 5: Final repository check and conditional commit**

Run:

```powershell
git diff --check
git status --short
```

Expected: clean worktree. If Wails regenerates `frontend/package.json.md5`, compare it with the actual MD5 of `frontend/package.json`; commit it only when the value legitimately differs. Commit any verified correction with `git commit -m "chore: finalize inline QR refinement"`, then rerun the affected verification command.
