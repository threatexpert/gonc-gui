# Android Borderless Inline QR Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the private frame around Android file-transfer inline QR views and keep the active-send QR reliably centered at `104dp x 104dp`.

**Architecture:** Make `PassphraseQrView` return the clickable `ImageView` directly instead of wrapping it in a decorated `FrameLayout`. Both controllers supply explicit `104dp x 104dp` `LinearLayout.LayoutParams`; the send controller additionally sets child gravity to horizontal center, while the receive session row retains its existing outer status frame.

**Tech Stack:** Android Java, minSdk 26, ZXing, Gradle, JUnit 4 source-contract tests.

## Global Constraints

- Scope is Android file send and file receive only; desktop and VPN screens are unchanged.
- Both inline QR views are exactly `104dp x 104dp` with no private background, border, or padding.
- The receive session row's existing rounded background and border remain unchanged.
- The active-send QR is horizontally centered; the pre-sharing passphrase UI remains unchanged.
- Tapping a valid inline QR still opens the existing enlarged clear-QR dialog.
- QR payload generation, caching, masking, accessibility description, failure behavior, session transitions, and receive QR retirement rules remain unchanged.
- Do not create a Git branch or worktree; work directly in the current checkout.
- Preserve the user's existing uncommitted `frontend/wailsjs/go/models.ts` change and do not include it in commits.

---

## File structure

- `android/app/src/test/java/cn/threatexpert/gonc/AndroidInlineQrLayoutContractTest.java`: new source-contract regression coverage for the shared borderless view and explicit send/receive sizing and alignment.
- `android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java`: retain the exact shared `104dp` sizing assertion and remove the obsolete frame-padding assertion.
- `android/app/src/main/java/cn/threatexpert/gonc/PassphraseQrView.java`: return a directly clickable `ImageView`; retain bitmap, cache, mask, accessibility, and click behavior.
- `android/app/src/main/java/cn/threatexpert/gonc/TransferInlineQrState.java`: remove the obsolete frame-padding constant and accessor.
- `android/app/src/main/java/cn/threatexpert/gonc/SendController.java`: add the QR with explicit square layout parameters and centered child gravity.
- `android/app/src/main/java/cn/threatexpert/gonc/ReceiveController.java`: add the QR with explicit square layout parameters without changing the receive session row.

---

### Task 1: Lock the borderless and centered layout contract

**Files:**
- Create: `android/app/src/test/java/cn/threatexpert/gonc/AndroidInlineQrLayoutContractTest.java`
- Modify: `android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java`

**Interfaces:**
- Consumes: Java source files under `android/app/src/main/java/cn/threatexpert/gonc/`
- Verifies: `PassphraseQrView.create(...)` returns the image without decoration; send and receive call sites use `TransferInlineQrState.inlineQrSizeDp()` for explicit square layout parameters; send child gravity is `Gravity.CENTER_HORIZONTAL`

- [ ] **Step 1: Write the failing source-contract test**

Create `AndroidInlineQrLayoutContractTest.java`:

```java
package cn.threatexpert.gonc;

import org.junit.Test;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;

import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

public class AndroidInlineQrLayoutContractTest {
    private static String source(String fileName) throws Exception {
        return new String(Files.readAllBytes(Paths.get(
                "src/main/java/cn/threatexpert/gonc/" + fileName)),
                StandardCharsets.UTF_8);
    }

    @Test
    public void sharedInlineQrHasNoPrivateFrame() throws Exception {
        String source = source("PassphraseQrView.java");
        assertFalse(source.contains("FrameLayout"));
        assertFalse(source.contains("container.setPadding"));
        assertFalse(source.contains("container.setBackground"));
        assertTrue(source.contains("return image;"));
    }

    @Test
    public void sendInlineQrUsesExplicitSquareSizeAndCenteredChildGravity() throws Exception {
        String source = source("SendController.java");
        int start = source.indexOf("private View passwordField()");
        int end = source.indexOf("private View protocolToggle()", start);
        assertTrue(start >= 0 && end > start);
        String method = source.substring(start, end);
        assertTrue(method.contains(
                "int qrSize = u.dp(TransferInlineQrState.inlineQrSizeDp());"));
        assertTrue(method.contains(
                "new LinearLayout.LayoutParams(qrSize, qrSize)"));
        assertTrue(method.contains("qrParams.gravity = Gravity.CENTER_HORIZONTAL;"));
        assertTrue(method.contains("qrOnly.addView(qr, qrParams);"));
    }

    @Test
    public void receiveInlineQrUsesExplicitSquareSize() throws Exception {
        String source = source("ReceiveController.java");
        int start = source.indexOf("private View receiveSessionBarView()");
        int end = source.indexOf("private View receiveProgressContent()", start);
        assertTrue(start >= 0 && end > start);
        String method = source.substring(start, end);
        assertTrue(method.contains(
                "int qrSize = dp(TransferInlineQrState.inlineQrSizeDp());"));
        assertTrue(method.contains(
                "row.addView(qr, new LinearLayout.LayoutParams(qrSize, qrSize));"));
        assertTrue(method.contains("row.setBackground(rounded("));
    }
}
```

- [ ] **Step 2: Remove the obsolete padding expectation**

Change `TransferInlineQrStateTest.inlineQrUsesSharedCompactDimensions()` to:

```java
@Test
public void inlineQrUsesSharedCompactDimensions() {
    assertEquals(104, TransferInlineQrState.inlineQrSizeDp());
}
```

- [ ] **Step 3: Run the focused tests to verify RED**

Run from `android/`:

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.AndroidInlineQrLayoutContractTest --tests cn.threatexpert.gonc.TransferInlineQrStateTest
```

Expected: `AndroidInlineQrLayoutContractTest` fails because `PassphraseQrView` still contains a framed `FrameLayout`, and the controllers do not yet use explicit QR child parameters.

---

### Task 2: Remove the shared frame and make controller layout explicit

**Files:**
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/PassphraseQrView.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/TransferInlineQrState.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/SendController.java`
- Modify: `android/app/src/main/java/cn/threatexpert/gonc/ReceiveController.java`

**Interfaces:**
- Preserves: `PassphraseQrView.create(Context, UiKit, String, int, boolean, Runnable, Runnable): View`
- Consumes: `TransferInlineQrState.inlineQrSizeDp(): int`, returning `104`
- Removes: `TransferInlineQrState.inlineQrFramePaddingDp()` because no frame remains

- [ ] **Step 1: Return the clickable image directly**

In `PassphraseQrView.java`, remove the `FrameLayout` import. After configuring the bitmap, apply the current actionable state to `image` itself:

```java
boolean actionable = !cleanPassphrase.isEmpty() && !entry.failed;
image.setEnabled(actionable);
image.setClickable(actionable);
image.setFocusable(actionable);
image.setImportantForAccessibility(actionable
        ? View.IMPORTANT_FOR_ACCESSIBILITY_AUTO
        : View.IMPORTANT_FOR_ACCESSIBILITY_NO);
if (actionable) {
    image.setContentDescription(context.getString(R.string.view_passphrase_qr));
    image.setOnClickListener(view -> {
        if (onClick != null) {
            onClick.run();
        }
    });
}
return image;
```

Delete the `FrameLayout container`, its padding/background configuration, and `container.addView(...)`. Keep QR generation, bitmap caching, mask generation, and error callback code unchanged.

- [ ] **Step 2: Remove the obsolete padding policy**

Delete these members from `TransferInlineQrState.java`:

```java
private static final int INLINE_QR_FRAME_PADDING_DP = 2;

static int inlineQrFramePaddingDp() {
    return INLINE_QR_FRAME_PADDING_DP;
}
```

Keep `INLINE_QR_SIZE_DP = 104` and `inlineQrSizeDp()` unchanged.

- [ ] **Step 3: Add the active-send QR with explicit centered parameters**

Replace the active-session branch body in `SendController.passwordField()` with:

```java
LinearLayout qrOnly = u.column();
View qr = PassphraseQrView.create(
        host.context(), u, password, TransferInlineQrState.inlineQrSizeDp(),
        sendQrHasConnected,
        () -> host.showPassphraseQr(password.trim()),
        () -> host.toast(R.string.inline_qr_generation_failed));
int qrSize = u.dp(TransferInlineQrState.inlineQrSizeDp());
LinearLayout.LayoutParams qrParams = new LinearLayout.LayoutParams(qrSize, qrSize);
qrParams.gravity = Gravity.CENTER_HORIZONTAL;
qrOnly.addView(qr, qrParams);
return qrOnly;
```

This leaves the full-width passphrase area available but constrains and centers its only child.

- [ ] **Step 4: Add the receive QR with explicit square parameters**

Replace only the QR branch inside `ReceiveController.receiveSessionBarView()` with:

```java
int qrSize = dp(TransferInlineQrState.inlineQrSizeDp());
View qr = PassphraseQrView.create(
        context(), host.ui(), receivePassword,
        TransferInlineQrState.inlineQrSizeDp(), false,
        () -> showPasswordQr(),
        () -> host.toast(R.string.inline_qr_generation_failed));
row.addView(qr, new LinearLayout.LayoutParams(qrSize, qrSize));
```

Do not change the row padding/background, status label weighting, disconnect button, QR visibility rule, or Passphrase fallback button.

- [ ] **Step 5: Run the focused tests to verify GREEN**

Run from `android/`:

```powershell
.\gradlew.bat testDebugUnitTest --tests cn.threatexpert.gonc.AndroidInlineQrLayoutContractTest --tests cn.threatexpert.gonc.TransferInlineQrStateTest
```

Expected: both test classes pass with zero failures.

- [ ] **Step 6: Commit the tested layout fix**

```powershell
git add android/app/src/test/java/cn/threatexpert/gonc/AndroidInlineQrLayoutContractTest.java android/app/src/test/java/cn/threatexpert/gonc/TransferInlineQrStateTest.java android/app/src/main/java/cn/threatexpert/gonc/PassphraseQrView.java android/app/src/main/java/cn/threatexpert/gonc/TransferInlineQrState.java android/app/src/main/java/cn/threatexpert/gonc/SendController.java android/app/src/main/java/cn/threatexpert/gonc/ReceiveController.java
git commit -m "fix: center borderless Android inline QR"
```

---

### Task 3: Full Android verification

**Files:**
- Modify only if verification exposes a defect in the scoped Android QR changes.

**Interfaces:**
- Verifies the borderless shared QR without changing transfer state or QR generation behavior.

- [ ] **Step 1: Run the full Android unit suite and debug assembly**

Run from `android/`:

```powershell
.\gradlew.bat testDebugUnitTest assembleDebug --rerun-tasks
```

Expected: `BUILD SUCCESSFUL`, zero JUnit failures/errors, and `app/build/outputs/apk/debug/app-debug.apk` exists.

- [ ] **Step 2: Check the final diff and repository state**

Run from the repository root:

```powershell
git diff --check
git status --short
```

Expected: no whitespace errors. The pre-existing `frontend/wailsjs/go/models.ts` modification may remain; no Android implementation files should remain uncommitted after the scoped commit.

- [ ] **Step 3: Perform Android visual smoke when a device is available**

Verify:

- active-send QR is centered and exactly the same visual size as the receive QR;
- send and receive QRs have no private white rounded frame or extra gap;
- the receive session status row retains its own border and background;
- tapping clear or masked inline QR still opens the enlarged clear QR;
- the pre-sharing send UI and receive QR retirement behavior are unchanged.

If no Android device/emulator is available, report visual smoke as not executed rather than passed.
