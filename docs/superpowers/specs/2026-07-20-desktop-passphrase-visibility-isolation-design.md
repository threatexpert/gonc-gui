# Desktop Passphrase Visibility Isolation Design

## Goal

Make temporary desktop passphrase visibility independent for file send, file receive, VPN client, and VPN server. Every reveal lasts five seconds for only the mode that initiated it, including while the user visits another mode.

## Root cause

The desktop currently stores one global `passwordVisible` boolean and one global timeout for all four modes. The event-subscription effect depends on `mode`; its cleanup cancels that timeout during every mode change without resetting the boolean. Consequently, a revealed passphrase appears revealed in another mode and can remain visible indefinitely after its timeout is cancelled.

## Visibility ownership

- Maintain separate visibility state for `send`, `receive`, `vpnClient`, and `vpnServer`.
- Maintain a separate timeout handle for each mode.
- The passphrase input reads only the visibility state belonging to the currently rendered mode.
- All four modes begin hidden.
- Revealing one mode must not reveal, hide, extend, or cancel another mode.

## Five-second behavior

- A reveal action makes only its target mode visible and schedules that mode to hide after exactly `5000ms`.
- Repeating a reveal action in the same mode cancels only that mode's previous timeout and starts a new five-second interval.
- A stale timeout from an earlier reveal must not hide a newer reveal.
- Changing modes does not cancel or restart any visibility timeout.
- Returning to a mode before its deadline shows the passphrase for the remaining time; returning after the deadline shows stars.
- Application unmount clears every outstanding visibility timeout.

## Action ownership

- Random generation, copy, paste, password scan, and passphrase QR activation reveal only the mode that initiated the action.
- An asynchronous action captures its target mode when the action starts. Switching modes before clipboard, scanning, password generation, or QR generation completes must not apply visibility to the newly selected mode.
- Existing passphrase value ownership remains unchanged: each action continues to update or read the passphrase belonging to its initiating mode.

## Boundaries

- Desktop only; Android visibility behavior is unchanged.
- No changes to passphrase values, QR payloads, QR masking, connection state, transfer state, VPN profiles, or layout.
- Do not alter when an action is available or whether a running mode permits editing.

## Verification

- Add pure state tests covering independent initial state for all four modes.
- Test that revealing send does not reveal receive or either VPN mode.
- Test that revealing receive does not extend or cancel send.
- Test that a repeated reveal in one mode supersedes its earlier timeout ownership.
- Test that hiding one mode leaves the other three unchanged.
- Add a focused integration/source contract confirming mode changes do not clear visibility timeouts and that the rendered input selects visibility by active mode.
- Run the focused visibility tests, the full frontend test suite, and the frontend production build.
- Perform desktop interaction smoke if an interactive environment is available. If it is unavailable, report the smoke test as not executed.
