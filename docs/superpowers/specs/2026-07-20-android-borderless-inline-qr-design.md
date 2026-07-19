# Android Borderless Inline QR Design

## Goal

Remove the unnecessary Android inline QR decoration and make the active-send QR visibly centered, while preserving the current QR size, behavior, and session state rules.

## Scope

- Android file send and file receive screens only.
- Desktop and VPN screens are unchanged.
- QR payload generation, caching, masking, session transitions, and the enlarged QR dialog are unchanged.

## Presentation

- Replace the shared framed QR container with a directly clickable `ImageView`.
- Keep the inline QR content and view footprint fixed at `104dp x 104dp`.
- Remove the QR view's white rounded background, border, and padding on both send and receive screens.
- Keep the entire `104dp x 104dp` QR view clickable and keyboard/accessibility focusable when a valid QR is available.
- Preserve the existing accessibility description and failure behavior.

## Send layout

- During active sharing, place the borderless QR at the horizontal center of the passphrase area.
- Give the QR explicit `104dp x 104dp` layout parameters instead of relying on the vertical `LinearLayout` default, which expands children to full width.
- Before sharing starts, keep the existing passphrase input and action controls unchanged.

## Receive layout

- Show the same borderless `104dp x 104dp` QR inside the existing receive session row during the initial connection phase.
- Keep the receive session row's own rounded status background and border. Only the QR's private frame is removed.
- Preserve the current replacement of the QR with the Passphrase button after success, failure, disconnect, or another terminal state.

## Interaction and state

- Tapping either inline QR continues to open the existing enlarged clear-QR dialog.
- The connected-state decorative mask remains clickable and retains its current generation and cache isolation behavior.
- No connection, passphrase, or QR retirement state rules change.

## Verification

- Add an Android unit/static layout test that prevents the send QR from falling back to a full-width child and verifies the shared inline QR has no frame padding.
- Run Android unit tests and assemble the debug APK.
- Visually verify on Android that the active-send QR is centered, both inline QRs have no private frame, and the receive session row remains unchanged. If device visual verification is unavailable, report that limitation explicitly.
