# File-transfer Inline Passphrase QR Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add live inline passphrase QR codes to desktop file transfer and session-state QR presentation to Android file transfer, with non-scannable masking after a successful connection.

**Architecture:** Desktop uses a focused React QR component plus pure TypeScript state helpers and an explicit `clientRunId` carried on file-transfer P2P reports so stale sessions cannot change masking. Android uses a reusable QR view/cache and pure Java state policy; the existing controller run guards and structured connection metrics drive send masking and receive QR retirement. Existing enlarged QR dialogs remain the only place that always shows a clear QR after masking.

**Tech Stack:** Go 1.x, Wails v2, React 18, TypeScript 4.6, `qrcode` 1.5, Android Java (minSdk 26), ZXing, Gradle/JUnit 4.

## Global Constraints

- Scope is desktop send/receive and Android send/receive only; do not change VPN screens, VPN QR payloads, scanning, or enlarged-dialog behavior.
- Inline QR payload is exactly `passphrase.trim()`.
- Empty or failed QR generation keeps a fixed-size empty placeholder and never shows stale QR data.
- Clear and masked inline QR controls open the existing clear enlarged passphrase QR dialog.
- Masking must make the main-screen QR ordinarily unscannable, not merely lower its opacity.
- Desktop and Android send mask after the first successful connection and remain masked for that run after disconnect.
- Android receive shows QR only during the initial waiting/starting/connecting/negotiating phase; success or any abnormal state retires it for that run.
- Do not create a Git branch or worktree; the user previously requested direct work in the current checkout.

---

## File structure

- `frontend/src/inlineQrState.ts`: pure desktop transfer-mode, generation ownership, and connected-latch rules.
- `frontend/src/TransferInlineQr.tsx`: live inline QR generation and clickable clear/masked/placeholder rendering.
- `frontend/tests/inlineQrState.test.ts`: Node tests for desktop state rules.
- `frontend/tsconfig.inline-qr-test.json`: emits only the pure helper and its test into ignored cache output.
- `frontend/src/App.tsx`: transfer run identity, session latches, layout integration, and dialog action wiring.
- `frontend/src/App.css`: desktop two-column sizing, masking, focus, and narrow-window behavior.
- `app.go`: `clientRunId` request boundary and tagged P2P event serialization.
- `app_transfer_run_test.go`: Go tests for required file-transfer IDs and report tagging.
- `android/app/src/main/java/cn/threatexpert/gonc/TransferInlineQrState.java`: pure Android send/receive QR state policy.
- `android/app/src/main/java/cn/threatexpert/gonc/PassphraseQrView.java`: bitmap cache, clear/masked rendering, placeholder, click/accessibility behavior.
- `android/app/src/main/java/cn/threatexpert/gonc/SendController.java`: post-start QR-only send section and connected latch.
- `android/app/src/main/java/cn/threatexpert/gonc/ReceiveController.java`: initial-wait QR in the session bar and per-run retirement.
- `android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java`: Android state and cache-key tests.

---

### Task 1: Desktop inline QR state module and test runner

**Files:**
- Create: `frontend/src/inlineQrState.ts`
- Create: `frontend/tests/inlineQrState.test.ts`
- Create: `frontend/tsconfig.inline-qr-test.json`
- Modify: `frontend/package.json`

**Interfaces:**
- Produces: `isFileTransferMode(mode: string): mode is 'send' | 'receive'`
- Produces: `latchSuccessfulConnection(previous: boolean, status: string): boolean`
- Produces: `isCurrentQrGeneration(requestId: number, currentId: number, generatedPassphrase: string, currentPassphrase: string): boolean`
- Produces: `normalizedQrPassphrase(value: string): string`

- [ ] **Step 1: Write the failing pure-state test**

```ts
import test from 'node:test';
import assert from 'node:assert/strict';
import {
  isCurrentQrGeneration,
  isFileTransferMode,
  latchSuccessfulConnection,
  normalizedQrPassphrase,
} from '../src/inlineQrState.js';

test('limits inline QR to file-transfer modes', () => {
  assert.equal(isFileTransferMode('send'), true);
  assert.equal(isFileTransferMode('receive'), true);
  assert.equal(isFileTransferMode('vpnServer'), false);
  assert.equal(isFileTransferMode('vpnClient'), false);
});

test('connection masking latches for the whole run', () => {
  assert.equal(latchSuccessfulConnection(false, 'connected'), true);
  assert.equal(latchSuccessfulConnection(true, 'disconnected'), true);
  assert.equal(latchSuccessfulConnection(false, 'connecting'), false);
});

test('late asynchronous QR result cannot replace current passphrase', () => {
  assert.equal(isCurrentQrGeneration(3, 4, 'old', 'new'), false);
  assert.equal(isCurrentQrGeneration(4, 4, 'new', 'new'), true);
  assert.equal(normalizedQrPassphrase('  secret  '), 'secret');
});
```

- [ ] **Step 2: Add the isolated TypeScript test config and script, then verify RED**

`frontend/tsconfig.inline-qr-test.json`:

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ES2020",
    "moduleResolution": "Node",
    "strict": true,
    "skipLibCheck": true,
    "rootDir": ".",
    "outDir": "node_modules/.cache/gonc-inline-qr-tests"
  },
  "include": ["src/inlineQrState.ts", "tests/inlineQrState.test.ts"]
}
```

Add to `frontend/package.json`:

```json
"test:inline-qr": "tsc -p tsconfig.inline-qr-test.json && node --test node_modules/.cache/gonc-inline-qr-tests/tests/inlineQrState.test.js"
```

Run: `npm run test:inline-qr`

Expected: FAIL because `frontend/src/inlineQrState.ts` does not exist.

- [ ] **Step 3: Implement the minimal pure helper**

```ts
export type FileTransferMode = 'send' | 'receive';

export function normalizedQrPassphrase(value: string) {
  return value.trim();
}

export function isFileTransferMode(mode: string): mode is FileTransferMode {
  return mode === 'send' || mode === 'receive';
}

export function latchSuccessfulConnection(previous: boolean, status: string) {
  return previous || status.trim().toLowerCase() === 'connected';
}

export function isCurrentQrGeneration(
  requestId: number,
  currentId: number,
  generatedPassphrase: string,
  currentPassphrase: string,
) {
  return requestId === currentId
    && generatedPassphrase === normalizedQrPassphrase(currentPassphrase);
}
```

- [ ] **Step 4: Run tests and the normal frontend compiler**

Run: `npm run test:inline-qr && npm run build`

Expected: all three Node tests PASS; `tsc && vite build` exits 0.

- [ ] **Step 5: Commit**

```bash
git add frontend/package.json frontend/src/inlineQrState.ts frontend/tests/inlineQrState.test.ts frontend/tsconfig.inline-qr-test.json
git commit -m "test: define desktop inline QR state"
```

---

### Task 2: Explicit desktop file-transfer run identity

**Files:**
- Modify: `app.go`
- Create: `app_transfer_run_test.go`
- Regenerate: `frontend/wailsjs/go/main/App.d.ts`
- Regenerate: `frontend/wailsjs/go/main/App.js`
- Regenerate: `frontend/wailsjs/go/models.ts`

**Interfaces:**
- Extends: `TransferRequest.ClientRunID int64` serialized as `clientRunId`
- Produces: `ClientP2PStatusReport` with the existing report fields plus `clientRunId`
- Produces: `tagClientP2PReport(clientRunID int64, report goncrunner.P2PStatusReport) ClientP2PStatusReport`
- Produces: `validateTransferClientRunID(mode goncrunner.Mode, clientRunID int64) error`
- Requirement: send/receive `StartTransfer` rejects `clientRunId <= 0`; VPN modes remain compatible with `0`

- [ ] **Step 1: Write failing Go tests for the boundary**

```go
func TestTagClientP2PReportPreservesPayloadAndRunID(t *testing.T) {
	report := goncrunner.P2PStatusReport{Status: "connected", Side: "send", Topic: "peer-1"}
	tagged := tagClientP2PReport(42, report)
	data, err := json.Marshal(tagged)
	if err != nil { t.Fatal(err) }
	if !bytes.Contains(data, []byte(`"clientRunId":42`)) || tagged.Status != "connected" {
		t.Fatalf("tagged report = %s", data)
	}
}

func TestTransferClientRunIDIsRequiredOnlyForFileModes(t *testing.T) {
	for _, mode := range []goncrunner.Mode{goncrunner.ModeSend, goncrunner.ModeReceive} {
		err := validateTransferClientRunID(mode, 0)
		if err == nil || !strings.Contains(err.Error(), "client run ID") {
			t.Fatalf("mode %s error = %v", mode, err)
		}
	}
	if err := validateTransferClientRunID(goncrunner.ModeVPNClient, 0); err != nil {
		t.Fatalf("VPN validation error = %v", err)
	}
}
```

- [ ] **Step 2: Run the focused tests to verify RED**

Run: `go test . -run 'Test(TagClientP2PReport|TransferClientRunID)' -count=1`

Expected: FAIL because the field, wrapper, and validation helper do not exist.

- [ ] **Step 3: Add request validation and tagged report serialization**

Implement these shapes in `app.go`:

```go
type TransferRequest struct {
	// existing fields...
	ClientRunID int64 `json:"clientRunId,omitempty"`
}

type ClientP2PStatusReport struct {
	goncrunner.P2PStatusReport
	ClientRunID int64 `json:"clientRunId"`
}

func tagClientP2PReport(clientRunID int64, report goncrunner.P2PStatusReport) ClientP2PStatusReport {
	return ClientP2PStatusReport{P2PStatusReport: report, ClientRunID: clientRunID}
}
```

Implement and call this helper before starting a runner:

```go
func validateTransferClientRunID(mode goncrunner.Mode, clientRunID int64) error {
	if (mode == goncrunner.ModeSend || mode == goncrunner.ModeReceive) && clientRunID <= 0 {
		return errors.New("client run ID is required for file transfer")
	}
	return nil
}
```

Emit `tagClientP2PReport(req.ClientRunID, report)` from the P2P callback.

- [ ] **Step 4: Verify Go and regenerate Wails bindings**

Run:

```powershell
go test . -run 'Test(TagClientP2PReport|TransferClientRunID)' -count=1
wails generate module
```

Expected: focused tests PASS; generated `main.TransferRequest` exposes `clientRunId`.

- [ ] **Step 5: Run full Go tests and commit**

Run: `go test ./... -count=1`

Expected: all Go packages PASS.

```bash
git add app.go app_transfer_run_test.go frontend/wailsjs/go/main/App.d.ts frontend/wailsjs/go/main/App.js frontend/wailsjs/go/models.ts
git commit -m "feat: tag desktop transfer reports with run ID"
```

---

### Task 3: Desktop inline QR component, layout, and masking

**Files:**
- Create: `frontend/src/TransferInlineQr.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.css`
- Modify: `frontend/tests/inlineQrState.test.ts`

**Interfaces:**
- Consumes: Task 1 helpers and Task 2 `P2PReport.clientRunId`
- Produces: `TransferInlineQr({passphrase, masked, onActivate, onError})`
- Produces: per-mode `sendQrHasConnected` / `receiveQrHasConnected` state and positive `clientRunId` on every desktop file-transfer start

- [ ] **Step 1: Extend the pure tests for mode-specific masking and run ownership**

Add tests that require:

```ts
assert.equal(fileTransferReportBelongsToRun(7, 7), true);
assert.equal(fileTransferReportBelongsToRun(6, 7), false);
assert.equal(inlineQrShouldMask('send', true, false), true);
assert.equal(inlineQrShouldMask('receive', false, true), true);
assert.equal(inlineQrShouldMask('vpnServer', true, true), false);
```

Run: `npm run test:inline-qr`

Expected: FAIL because `fileTransferReportBelongsToRun` and `inlineQrShouldMask` do not exist.

- [ ] **Step 2: Implement the minimal additional helpers and verify GREEN**

Add exact-ID and mode-specific helpers to `inlineQrState.ts`, then run:

`npm run test:inline-qr`

Expected: all tests PASS.

- [ ] **Step 3: Build the isolated React QR component**

Implement `TransferInlineQr.tsx` with this contract:

```tsx
type Props = {
  passphrase: string;
  masked: boolean;
  onActivate: () => void;
  onError: (message: string) => void;
};

export function TransferInlineQr({passphrase, masked, onActivate, onError}: Props) {
  const normalized = normalizedQrPassphrase(passphrase);
  const generation = useRef(0);
  const onErrorRef = useRef(onError);
  const [dataUrl, setDataUrl] = useState('');

  useEffect(() => { onErrorRef.current = onError; }, [onError]);
  useEffect(() => {
    const requestId = ++generation.current;
    if (!normalized) {
      setDataUrl('');
      return;
    }
    QRCode.toDataURL(normalized, QR_OPTIONS).then((next) => {
      if (isCurrentQrGeneration(requestId, generation.current, normalized, normalized)) {
        setDataUrl(next);
      }
    }).catch((error) => {
      if (requestId === generation.current) {
        setDataUrl('');
        onErrorRef.current(String(error));
      }
    });
  }, [normalized]);

  return <button type="button" className={`transfer-inline-qr${masked ? ' masked' : ''}`}
    disabled={!dataUrl} onClick={onActivate} aria-label="View passphrase QR code">
    {dataUrl && <img src={dataUrl} alt="" />}
  </button>;
}
```

Keep `QR_OPTIONS` black/white with margin `2` and a sufficiently large encoded width so CSS scaling remains crisp.

- [ ] **Step 4: Integrate run identity and latches in `App.tsx`**

- Extend `P2PReport` with `clientRunId?: number`.
- Keep a monotonic `transferRunSequence` ref and active send/receive run refs.
- On a send/receive `start()`, allocate a positive run ID, reset only that mode’s QR latch, and pass `clientRunId` in `StartTransfer`.
- If start rejects, invalidate the captured run ID without changing a newer run.
- In `p2p:report`, apply file-transfer reports only when `clientRunId` exactly matches the active mode run; latch on normalized `connected` and never clear on disconnect.
- Keep VPN report behavior unchanged.

- [ ] **Step 5: Replace the desktop transfer setup markup with a two-column grid**

For send/receive only, wrap the existing passphrase field and UDP checkbox in:

```tsx
<div className="transfer-setup-grid">
  <div className="transfer-setup-fields">{/* existing passphrase + UDP */}</div>
  <div className="transfer-inline-qr-column">
    <TransferInlineQr
      passphrase={activePassword}
      masked={inlineQrShouldMask(mode, sendQrHasConnected, receiveQrHasConnected)}
      onActivate={showPasswordQr}
      onError={(message) => setError(localizeError(message))}
    />
  </div>
</div>
```

Render the pre-existing passphrase/VPN layout unchanged for VPN modes.

- [ ] **Step 6: Add desktop sizing and masking CSS**

Implement:

```css
.transfer-setup-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: stretch;
  gap: 16px;
}

.transfer-inline-qr-column {
  display: grid;
  min-width: 132px;
  place-items: center;
}

.transfer-inline-qr {
  position: relative;
  display: grid;
  height: min(100%, 220px);
  max-width: 220px;
  min-height: 132px;
  aspect-ratio: 1;
  place-items: center;
  overflow: hidden;
  padding: 0;
  background: #fff;
}

.transfer-inline-qr.masked img { filter: blur(10px); transform: scale(.92); }
.transfer-inline-qr.masked::after {
  position: absolute;
  inset: 18%;
  background: rgba(226, 232, 240, .82);
  content: "";
}
```

Add a narrow-window rule that shrinks the QR column to `112px` without stacking or changing VPN layout. Preserve visible keyboard focus on the clickable shell.

- [ ] **Step 7: Verify desktop behavior and commit**

Run:

```powershell
Push-Location frontend
npm run test:inline-qr
npm run build
Pop-Location
go test ./... -count=1
wails build -clean
git diff --check
```

Expected: all commands exit 0. Restore only deterministic Wails generated whitespace noise if the generator introduces it; do not discard semantic binding changes.

```bash
git add frontend/src/TransferInlineQr.tsx frontend/src/inlineQrState.ts frontend/src/App.tsx frontend/src/App.css frontend/tests/inlineQrState.test.ts frontend/wailsjs
git commit -m "feat: show inline desktop transfer QR"
```

---

### Task 4: Android reusable QR presentation and pure state policy

**Files:**
- Create: `android/app/src/main/java/cn/threatexpert/gonc/TransferInlineQrState.java`
- Create: `android/app/src/main/java/cn/threatexpert/gonc/PassphraseQrView.java`
- Create: `android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java`
- Modify: `android/app/src/main/res/values/strings.xml`
- Modify: `android/app/src/main/res/values-zh/strings.xml`

**Interfaces:**
- Produces: `latchSendConnected(boolean latched, int connectedCount)`
- Produces: `retireReceiveQr(boolean retired, String normalizedState)`
- Produces: `showReceiveQr(boolean retired, String normalizedState)`
- Produces: `TransferInlineQrState.CacheKey(String passphrase, int pixelSize)` equality by normalized passphrase and size
- Produces: `PassphraseQrView.create(Context context, UiKit ui, String passphrase, int sizeDp, boolean masked, Runnable onClick)`

- [ ] **Step 1: Write failing Android state tests**

```java
@Test public void sendMaskLatchesAfterFirstConnection() {
    assertFalse(TransferInlineQrState.latchSendConnected(false, 0));
    assertTrue(TransferInlineQrState.latchSendConnected(false, 1));
    assertTrue(TransferInlineQrState.latchSendConnected(true, 0));
}

@Test public void receiveQrRetiresAfterSuccessOrAbnormalState() {
    assertTrue(TransferInlineQrState.showReceiveQr(false, "starting"));
    assertTrue(TransferInlineQrState.showReceiveQr(false, "connecting"));
    assertFalse(TransferInlineQrState.showReceiveQr(false, "reconnecting"));
    assertTrue(TransferInlineQrState.retireReceiveQr(false, "connected"));
    assertTrue(TransferInlineQrState.retireReceiveQr(false, "error"));
    assertFalse(TransferInlineQrState.showReceiveQr(true, "waiting"));
}

@Test public void cacheKeyUsesTrimmedPassphraseAndSize() {
    assertEquals(new TransferInlineQrState.CacheKey(" secret ", 112),
            new TransferInlineQrState.CacheKey("secret", 112));
    assertNotEquals(new TransferInlineQrState.CacheKey("secret", 112),
            new TransferInlineQrState.CacheKey("secret", 220));
}
```

- [ ] **Step 2: Run focused tests to verify RED**

Run: `gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.TransferInlineQrStateTest`

Expected: compile FAIL because the state class and key do not exist.

- [ ] **Step 3: Implement the pure state policy**

Use explicit normalized-state sets:

```java
private static final Set<String> INITIAL_RECEIVE_STATES =
        new HashSet<>(Arrays.asList("wait", "waiting", "starting", "preparing", "connecting", "negotiating"));

static boolean latchSendConnected(boolean latched, int connectedCount) {
    return latched || connectedCount > 0;
}

static boolean showReceiveQr(boolean retired, String state) {
    return !retired && INITIAL_RECEIVE_STATES.contains(normalize(state));
}

static boolean retireReceiveQr(boolean retired, String state) {
    String clean = normalize(state);
    return retired || !INITIAL_RECEIVE_STATES.contains(clean);
}
```

Treat `reconnecting`, disconnected/closed/stopped, failure/error/lost/timeout, and every unknown non-initial state as retirement states. This whitelist policy prevents a later waiting/reconnecting report from restoring the QR in the same run.

- [ ] **Step 4: Implement cached clear/masked Android QR rendering**

`PassphraseQrView` must:

- keep a small cache keyed by trimmed passphrase and pixel size;
- use `QrCodes.encode()` for clear content;
- create the masked bitmap on API 26+ by downscaling to a very small bitmap, scaling back with filtering, and drawing a neutral opaque center cover so finder/data modules are destroyed;
- create a fixed square placeholder for empty/failed generation;
- wrap the image in a clickable/focusable container with `R.string.view_passphrase_qr` content description;
- invoke the supplied enlarged-dialog callback without logging the passphrase.

Add localized `view_passphrase_qr` strings in English and Chinese.

- [ ] **Step 5: Verify focused tests and compile Android**

Run:

```powershell
gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.TransferInlineQrStateTest
gradlew.bat assembleDebug
```

Expected: focused tests PASS; debug APK compilation succeeds.

- [ ] **Step 6: Commit**

```bash
git add android/app/src/main/java/cn/threatexpert/gonc/TransferInlineQrState.java android/app/src/main/java/cn/threatexpert/gonc/PassphraseQrView.java android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java android/app/src/main/res/values/strings.xml android/app/src/main/res/values-zh/strings.xml
git commit -m "feat: add Android inline passphrase QR view"
```

---

### Task 5: Android send QR-only sharing state

**Files:**
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/SendController.java`
- Modify: `android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java`

**Interfaces:**
- Consumes: `PassphraseQrView.create(...)` and `TransferInlineQrState.latchSendConnected(...)`
- Produces: a per-run `sendQrHasConnected` latch reset at start/stop

- [ ] **Step 1: Add failing lifecycle assertions**

Extend the pure test to exercise the whole sequence:

```java
boolean latched = false;                 // new sharing run
latched = latchSendConnected(latched, 0);
assertFalse(latched);                    // clear QR
latched = latchSendConnected(latched, 1);
assertTrue(latched);                     // masked QR
latched = latchSendConnected(latched, 0);
assertTrue(latched);                     // disconnect stays masked
assertFalse(TransferInlineQrState.newSendRunLatch());
```

Run the focused test and expect FAIL because `newSendRunLatch()` does not exist.

- [ ] **Step 2: Add the reset helper and verify GREEN**

Implement `newSendRunLatch()` returning `false`, then rerun the focused test.

- [ ] **Step 3: Integrate the send controller lifecycle**

- Add `boolean sendQrHasConnected`.
- In `start()`, reset it only for the new run before creating the session.
- After `host.updateMetricsFromReport(...)`, set it through `latchSendConnected`; request a full render when it changes.
- In `stop()`, `onStopped()`, `onError()`, and fresh-launch reset, clear it after the session ends.
- Keep existing run-ID callback guards.

- [ ] **Step 4: Replace only the active-session passphrase field**

At the top of `passwordField()`:

```java
if (session != null) {
    LinearLayout qrOnly = u.column();
    qrOnly.setGravity(Gravity.CENTER_HORIZONTAL);
    qrOnly.addView(PassphraseQrView.create(
            host.context(), u, password, 220, sendQrHasConnected,
            () -> host.showPassphraseQr(password.trim())));
    return qrOnly;
}
```

Leave the pre-sharing branch byte-for-byte behaviorally equivalent. Keep protocol and Stop Sharing outside the QR-only field.

- [ ] **Step 5: Verify and commit**

Run:

```powershell
gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.TransferInlineQrStateTest
gradlew.bat testDebugUnitTest
gradlew.bat assembleDebug
```

Expected: all Android unit tests and debug assembly PASS.

```bash
git add android/app/src/main/java/cn/threatexpert/gonc/SendController.java android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java
git commit -m "feat: show Android send session QR"
```

---

### Task 6: Android receive initial-connection QR retirement

**Files:**
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/ReceiveController.java`
- Modify: `android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java`

**Interfaces:**
- Consumes: `showReceiveQr(...)`, `retireReceiveQr(...)`, and `PassphraseQrView.create(...)`
- Produces: per-run `receiveQrRetired` reset only by a genuinely new Connect run

- [ ] **Step 1: Add failing sequence tests for retirement**

```java
boolean retired = TransferInlineQrState.newReceiveRunRetired();
assertFalse(retired);
assertTrue(showReceiveQr(retired, "starting"));
retired = retireReceiveQr(retired, "error: timeout");
assertTrue(retired);
assertFalse(showReceiveQr(retired, "waiting"));
assertFalse(showReceiveQr(retired, "reconnecting"));
```

Run the focused test and expect FAIL because `newReceiveRunRetired()` does not exist or abnormal prefix handling is incomplete.

- [ ] **Step 2: Implement reset and abnormal-prefix rules, then verify GREEN**

Support exact/prefix normalization for `error:*`, `fail*`, `lost*`, and `timeout*`; rerun the focused test.

- [ ] **Step 3: Integrate receive controller lifecycle**

- Add `boolean receiveQrRetired`.
- Reset it immediately before starting a genuinely new receive run.
- After every structured P2P report updates `receiveMetrics`, call `retireReceiveQr(receiveQrRetired, receiveConnectionState())`.
- Set it on `onReady`, `onStopped`, and `onError` before rendering.
- Do not clear it for transient later events in the same run.
- Existing `runId`/`isActiveRun` guards remain the stale-event boundary.

- [ ] **Step 4: Switch the session-bar control by state**

In `receiveSessionBarView()`:

```java
if (TransferInlineQrState.showReceiveQr(receiveQrRetired, receiveConnectionState())) {
    row.addView(PassphraseQrView.create(
            context(), host.ui(), receivePassword, 104, false,
            () -> showPasswordQr()));
} else {
    Button passphrase = secondaryButton(string(R.string.passphrase));
    passphrase.setOnClickListener(v -> showPasswordQr());
    row.addView(passphrase, new LinearLayout.LayoutParams(
            ViewGroup.LayoutParams.WRAP_CONTENT, dp(38)));
}
```

Allow the row to grow while the QR is present. Do not alter the pre-Connect password panel.

- [ ] **Step 5: Verify and commit**

Run:

```powershell
gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.TransferInlineQrStateTest
gradlew.bat testDebugUnitTest
gradlew.bat assembleDebug
```

Expected: all commands exit 0.

```bash
git add android/app/src/main/java/cn/threatexpert/gonc/ReceiveController.java android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java
git commit -m "feat: show Android receive waiting QR"
```

---

### Task 7: Cross-platform final verification

**Files:**
- Modify only if verification exposes a real formatting, generated-binding, or documentation issue.

**Interfaces:**
- Verifies all previous task contracts together.

- [ ] **Step 1: Verify formatting and repository cleanliness**

Run:

```powershell
$goFiles = @('app.go', 'app_transfer_run_test.go')
$formatted = gofmt -d $goFiles
if ($formatted) { $formatted; exit 1 }
git diff --check
```

Expected: no formatting diff and exit 0.

- [ ] **Step 2: Run complete desktop tests and builds**

```powershell
go test ./... -count=1
Push-Location frontend
npm run test:inline-qr
npm run build
Pop-Location
wails build -clean
```

Expected: Go tests PASS, Node state tests PASS, Vite production build PASS, and `build/bin/gonc-gui.exe` exists.

- [ ] **Step 3: Run complete Android tests and APK build**

```powershell
Push-Location android
.\gradlew.bat testDebugUnitTest assembleDebug --rerun-tasks
Pop-Location
```

Expected: `BUILD SUCCESSFUL` and `android/app/build/outputs/apk/debug/app-debug.apk` exists.

- [ ] **Step 4: Perform visual/state smoke where environments permit**

Desktop send/receive checklist:

- clear QR before first connection;
- live passphrase update and stable empty footprint;
- mask after first connection and remain masked after disconnect;
- masked click opens clear large dialog;
- VPN modes unchanged;
- narrow window keeps controls usable.

Android checklist:

- send idle UI unchanged;
- send active clear QR, first connection mask, disconnect remains masked, tap opens clear dialog;
- receive idle UI unchanged;
- receive initial waiting QR, success/abnormal switches to Passphrase button and never reappears in that run.

If no interactive Windows/Android environment is available, record that limitation explicitly rather than claiming the smoke test passed.

- [ ] **Step 5: Final diff and commit if verification required changes**

Run:

```powershell
git diff --check
git status --short
```

Expected: clean worktree. If verification caused legitimate tracked changes, commit only those files with `git commit -m "chore: finalize inline transfer QR"` and rerun the affected verification command.
