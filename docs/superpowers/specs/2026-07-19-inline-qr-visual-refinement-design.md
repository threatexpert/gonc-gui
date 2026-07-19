# Inline QR Visual Refinement Design

## Goal

Refine the existing desktop and Android inline passphrase QR experience so the controls are smaller, visually quieter, and consistent across platforms. Preserve all existing session isolation, enlarged clear-QR dialogs, and VPN behavior.

## Scope

- Desktop file send and file receive screens.
- Android file send and file receive screens.
- No changes to VPN screens, QR scanning, passphrase payloads, connection protocols, or enlarged QR dialog behavior.

## Desktop

### Passphrase actions

- Remove the passphrase-row QR button from file send and file receive.
- Keep the existing QR buttons for VPN server and VPN client unchanged.
- The inline QR remains clickable and continues to open the existing enlarged clear passphrase QR dialog.

### Size and alignment

- Use a fixed inline QR footprint of `112px × 112px` at normal and narrow desktop widths.
- Keep the file-transfer setup in two columns.
- Bottom-align the inline QR footprint with the bottom edge of the `Use UDP protocol` row.
- Preserve a fixed empty footprint while the QR is empty or being generated.

### Mask appearance

- Do not blur or otherwise render the real passphrase QR on the main screen after masking.
- Replace it with a fixed decorative pseudo-QR pattern that is independent of the passphrase and is not a valid QR payload.
- Apply blur to the decorative pattern so it reads visually as a blurred QR.
- Remove the opaque center square/cover entirely.
- The masked control remains clickable and opens the real clear QR only in the existing enlarged dialog.

### Mask reset

- Stopping a send or receive run does not by itself remove the mask.
- After the run has stopped, an actual passphrase change removes the mask. This includes manual editing, random generation, paste, or scan results.
- Setting the same passphrase again does not count as a change.
- Starting a new run retains the existing behavior: its inline QR begins clear and masks again after the first successful connection.
- Passphrase changes cannot occur through the disabled file-transfer input while a run is active; defensive state logic must not let a running-session change unmask an active connected QR.

## Android

### Shared compact frame

- Keep the receive inline QR content size at `104dp`.
- Change the shared QR container padding from `6dp` to `2dp`, keeping the existing light border and rounded background.
- The outer frame must sit close to the QR edge while retaining a usable click target through the `104dp` content size.

### Send

- Reduce the active-send QR content from `220dp` to `104dp`.
- Keep the QR horizontally centered in the active sharing passphrase area.
- Use the same compact frame as receive.
- Before sharing starts, preserve the existing send passphrase UI exactly.

### Android mask appearance

- Do not derive the masked bitmap from the real passphrase QR.
- Render a fixed decorative pseudo-QR bitmap that is independent of the passphrase, then soften it so it appears blurred.
- Do not draw an opaque center square.
- The masked control remains clickable and opens the existing real clear QR dialog.
- Clear QR generation and cache security constraints remain unchanged for the enlarged action and unmasked inline state.

### Receive

- Preserve the current receive state policy: the compact QR replaces the Passphrase button only during the initial connection phase.
- Preserve its current `104dp` QR content size and only tighten the surrounding frame.
- Success or abnormal state still retires the inline QR for that run and restores the Passphrase button.

## State and implementation boundaries

- Keep `clientRunId`, asynchronous QR generation ownership, start serialization, and Android run guards unchanged.
- Add a small pure state rule for clearing a desktop send/receive mask only when the corresponding run is not active and the passphrase value actually changed.
- The decorative masked pattern must contain no passphrase-derived bytes and must be reusable by all masked states of a given display size.
- Removing file-transfer QR buttons must not remove the shared enlarged-dialog function because the inline QR still calls it.

## Verification

- Add a failing-then-passing desktop state test for mask reset: stop alone remains masked; a different passphrase while stopped clears it; the same passphrase or a change while running does not clear it.
- Extend desktop tests/build checks to confirm file modes no longer render the passphrase-row QR action while VPN modes retain it through static/component inspection where the current test stack permits.
- Add Android pure tests proving masked presentation keys/policy are passphrase-independent and both send/receive use `104dp` content sizing through controller constants or an equivalent testable policy.
- Run the full frontend build, Go tests, Wails build, Android unit tests, and Android debug assembly.
- Perform visual smoke where possible for desktop bottom alignment, pseudo-QR blur, Android centering, and compact frame. If no interactive environment is used, report that limitation explicitly.
