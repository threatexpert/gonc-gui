# File-transfer inline passphrase QR design

Date: 2026-07-19

## Scope

Improve passphrase QR presentation for desktop and Android file-transfer modes only:

- desktop send and receive;
- Android send and receive.

VPN server/client screens, VPN profile QR codes, QR scanning, and the existing enlarged passphrase QR dialog are out of scope.

## Shared behavior

- Every inline QR encodes only the trimmed passphrase. It contains no protocol, device, endpoint, or profile wrapper.
- An inline QR is a square black-on-white QR with its required quiet zone. It has no visible title, passphrase text, explanatory copy, or decorative border.
- Clicking or tapping an inline QR performs the same action as the existing QR button and opens the existing enlarged, clear passphrase QR dialog.
- Inline QR controls have an accessible action label equivalent to “View passphrase QR code”.
- Empty passphrases retain the same QR footprint as an empty placeholder so surrounding controls do not move.
- A stale QR must never remain visible after the current passphrase becomes empty or changes.

## Desktop layout

### Modes

The inline layout applies only while the selected desktop mode is send or receive. VPN layouts and behavior remain unchanged.

### Placement

- The transfer setup area below the shared-file/save-directory controls and above the primary Start Sharing/Connect button becomes a two-column layout.
- The left column contains the current passphrase label, input, passphrase actions, explanatory text, and UDP option without changing their behavior.
- The right column contains the inline QR.
- The QR column begins below the Add Files/Add Folder controls in send mode or below the Choose/Open Folder controls in receive mode.
- The QR column ends above the primary Start Sharing/Connect button.
- The QR remains square, uses the available setup-block height as its preferred size, and is capped by the available column width. If the desktop window narrows, the QR shrinks before the left-side controls are compressed.
- The existing QR button remains in the passphrase action row and continues to open the enlarged dialog.

### Live generation

- The inline QR updates as the passphrase changes.
- Desktop QR generation is asynchronous. Each generation request receives a monotonically increasing generation token.
- A completed generation result is applied only when its token is still current and its normalized passphrase still matches the current value.
- The inline QR state is separate from the enlarged-dialog QR state. Live editing cannot open, close, or replace an already-open dialog.

### Connected-session masking

- A clear inline QR is shown before the current send or receive session has ever connected successfully.
- The first successful peer connection latches a session-local `hasConnected` state.
- Once latched, the inline QR is strongly blurred and covered so it is not directly scannable from the main window.
- The mask remains after later disconnection or connection failure. It resets only when that transfer session is stopped and a new session is started.
- The masked QR remains clickable and opens the clear enlarged QR dialog.

## Android send behavior

### Before sharing

The existing send passphrase UI remains unchanged. No inline QR is added before the user presses Start Sharing.

### Sharing started, no successful peer yet

- The passphrase section hides its hint, input, and Random/Copy/Scan/QR action buttons.
- The section contains one centered, clear inline QR.
- The protocol option and Stop Sharing button remain outside this QR-only section and retain existing behavior.
- Tapping the QR opens the existing enlarged clear QR dialog.

### After a successful peer connection

- The send controller latches that the current sharing run has connected at least once, using its structured P2P/connection metrics rather than log-message guessing.
- The inline QR becomes strongly blurred and covered so it cannot be scanned from the main screen.
- It stays masked even if all peers later disconnect.
- Tapping it still opens the clear enlarged dialog.
- Stopping sharing clears the latch and restores the original pre-sharing passphrase UI. A subsequent new sharing run begins with a clear QR.

## Android receive behavior

### Before connecting

The current receive passphrase UI remains completely unchanged. No inline QR is shown before the user presses Connect.

### Waiting for a peer

- After Connect, while the initial normalized connection state is waiting, starting, connecting, or negotiating, the session bar shows a small clear inline QR in place of its current Passphrase button.
- Tapping this QR opens the existing enlarged clear QR dialog.

### Connected or abnormal state

- On successful connection, the QR is immediately replaced by the existing Passphrase button.
- Failed, disconnected, stopped, closed, timed-out, or otherwise abnormal states also show the Passphrase button and do not show the inline QR.
- The first successful or abnormal state retires the inline QR for the remainder of that receive run. A later reconnecting/waiting report cannot make it reappear.
- The decision uses normalized structured connection state. It does not infer state from display labels or localized text.

## Android sizing and caching

- The send-session QR is centered and sized for convenient nearby scanning without changing the surrounding card width.
- The receive waiting-state QR fits the session bar while remaining large enough for nearby scanning; the row may grow vertically while it is present.
- QR bitmaps are cached by trimmed passphrase and requested pixel size so frequent metrics renders do not repeatedly encode the same QR.
- A passphrase or size change generates a new bitmap. Generation failure produces the fixed-size empty placeholder and never reuses a bitmap for another passphrase.

## Masking behavior

- “Masked” means more than reduced opacity: the QR modules receive a strong blur plus a neutral cover sufficient to prevent ordinary camera scanning from the main UI.
- The clickable container and accessibility action remain available while the image is masked.
- The enlarged dialog is intentionally unmasked because opening it is an explicit user action.

## State lifecycle

- Desktop send and receive each own a per-run `hasConnected` latch.
- Android send owns a per-run `hasConnected` latch.
- Android receive owns a per-run `qrRetired` latch. Success or any abnormal state sets it, and only a genuinely new Connect run clears it.
- Session latches are reset at the start of a genuinely new transfer run, not merely when a transient disconnect report arrives.
- Stale events from an older run cannot mask or unmask the current run’s QR.

## Failure handling

- Empty passphrase: fixed-size empty placeholder, no QR click action.
- QR encoding failure: fixed-size empty placeholder; report through the platform’s existing error/log path without exposing the passphrase.
- Late desktop QR result: discard silently when its generation token or passphrase is stale.
- Unknown connection state: prefer the safe non-scannable choice. Desktop retains its latched mask; Android receive shows the Passphrase button.

## Testing and verification

### Desktop

- Inline QR appears only in send/receive modes.
- Empty passphrase keeps the placeholder footprint.
- Rapid passphrase changes cannot allow an older asynchronous result to replace the current QR.
- Inline and enlarged-dialog state remain independent.
- Send and receive QRs mask after first successful connection and remain masked after disconnect until a new run.
- Narrow-window layout preserves usable passphrase inputs and actions.

### Android

- Send UI is unchanged before sharing.
- Send shows only a clear inline QR after sharing starts and before first connection.
- First connection latches masking; subsequent disconnect does not unmask it; stopping resets the latch.
- Receive UI is unchanged before Connect.
- Receive initial waiting/connecting/negotiating states show the QR in the session bar.
- Receive connected and abnormal states show the Passphrase button.
- Receive success/abnormal retirement prevents later reconnecting or waiting events from restoring the QR in the same run.
- QR cache keys include both passphrase and size; failures cannot reuse stale content.
- Tapping clear or masked inline QR invokes the enlarged-dialog action.

### Build verification

- Desktop frontend TypeScript/Vite production build.
- Go tests and Wails clean production build.
- Android JVM unit tests and debug APK assembly.
- Manual visual smoke at representative desktop sizes and narrow/normal Android widths when interactive environments are available.
