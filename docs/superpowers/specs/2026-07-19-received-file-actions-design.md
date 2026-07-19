# Received File Actions Design

## Scope

Make files in the desktop and Android receive lists visibly usable after they exist locally. The remote file list remains the single source of navigation; this feature does not add a download-history page, persistent Android history, or a general-purpose file manager.

The desktop application exposes a quiet action that locates a completed file in the platform file manager. It never opens a received file directly. The Android application follows normal file-browser behavior: tapping a locally available file opens it through the system, including APK files. Android relies on the system package installer and its own confirmation and unknown-source protections rather than adding a Gonc warning.

## Availability Rule

Local availability is a deliberately lightweight usability condition, not a content-integrity claim. A remote file is locally available when all of the following are true:

- the expected local target exists;
- the target is a readable regular file rather than a directory;
- its size equals the size reported by the current remote list.

An empty remote file is available only when a corresponding zero-byte local file actually exists. Modification time is displayed as remote metadata but is not part of the availability decision because filesystem and Android document-provider timestamp behavior is inconsistent.

This rule intentionally accepts a pre-existing or damaged same-name file when its size matches. The UI therefore communicates local availability, not cryptographic verification.

## Refresh Timing and Session Lifetime

Do not update per-file availability while a download is running. Refresh the visible directory as one batch at these two points:

1. after a download task ends, including a task that ends with some failed files;
2. after the user enters a remote directory and its list is available.

Each batch resolves and checks only the files displayed in the current directory, performs checks off the UI thread where necessary, and applies the result to the rows together. A failed check for one file does not block other rows.

Disconnecting preserves the last connection's availability state and actions so users can work with files they just received. The state resets only when a new receive connection is successfully established. Android keeps no cross-process or cross-launch browsing history. Re-rendering the current Activity may retain current-session state in memory, but no availability index or received-file history is persisted for restoration after process exit.

Before every locate or open action, recheck that the target still exists, is readable, and has the expected size. Entering the directory again also rechecks all visible files, so an externally deleted or replaced file loses its available state.

## Desktop Interaction

The normal row remains visually quiet:

- unavailable files keep their current appearance;
- locally available files gain a small solid green dot on or beside the file icon and slightly stronger filename contrast;
- do not tint the whole row green or add a textual Completed label;
- folders never receive a local-availability marker.

Only while the pointer is over a locally available file row, fade in a small icon-only locate button at the far right. The button uses a folder/location glyph and an accessible label and tooltip appropriate to the platform:

- Windows: Show in File Explorer;
- macOS: Show in Finder;
- Linux: Show in File Manager.

The filename and row do not open the file. Keyboard focus must reveal the same locate button so the action is not hover-only for keyboard and assistive-technology users.

The backend receives the expected target plus the current save-root context, canonicalizes it, rechecks availability, and then invokes the platform file manager:

- Windows uses Explorer's select-file behavior;
- macOS uses Finder reveal behavior, such as `open -R`;
- Linux uses a supported file-manager reveal interface when available and otherwise opens the parent directory.

If the target is no longer available, remove the row marker and show a short localized message. Failure to launch the file manager shows an error but does not affect other rows.

## Android Interaction

Android treats an available row like a conventional file browser:

- the file icon and filename acquire a clickable appearance;
- a quiet received checkmark appears at the right;
- tapping the file icon or filename immediately opens the file through the system;
- the selection checkbox remains a separate target and continues to control receive selection;
- starting another download does not disable files already known to be available, but it does not reveal newly completed files until the task ends;
- folders retain their current navigation behavior.

An overflow menu on an available file provides:

- Open;
- Open with, which explicitly presents the system chooser;
- Share;
- File information.

Delete, move, and rename are out of scope. APK files follow the same tap-to-open behavior without a Gonc warning; Android's package installer owns installation consent and unknown-source handling.

The app resolves the file to its actual `content://` URI or local file target, determines an appropriate MIME type with a safe fallback, grants temporary read access, and sends the corresponding system intent. Share grants read-only URI access. File information shows display name, size, remote modification time when present, and the user-facing save-location label; it does not expose an unusable raw content URI as a filesystem path.

If no application can handle a file, keep the availability state and show a localized message. If URI permission is no longer valid or the target fails the final size check, clear the state and report that the file is no longer accessible.

## Target Resolution

Desktop target resolution derives the expected absolute path from the save root used for the applicable receive session and the normalized remote relative path. It must reuse the downloader's existing path normalization and containment rules so a remote path cannot escape the save root.

Android target resolution reuses `HttpReceiver`'s destination rules for the selected SAF tree or the default Gonc Downloads location. The resolver must return the exact document or MediaStore URI associated with the normalized remote path when available. It must not search arbitrary device storage. Any short-lived mapping used to resolve provider-assigned URIs is scoped to the current connection state exposed by the UI and is not presented as received-file history.

Changing the save location affects subsequent downloads and checks. A row already marked available retains its resolved target until it is rechecked; after recheck, it follows the active resolver's result. No full save-directory scan is performed.

## State Boundaries

Keep platform-independent row state small: normalized remote path, availability, and a platform-owned local target reference. Remote path is the lookup key only within the current connection generation. Every asynchronous refresh captures that generation and discards its result if a newer connection has become active.

Download progress remains task-level. Ending a task schedules one visible-directory availability refresh after the downloader's completion callback has settled. Partial failure is allowed: matching successful or pre-existing files become available, while missing or size-mismatched rows remain unavailable.

## Error Handling and Safety

- Do not reveal newly available locate or open actions until the active download task ends; previously available actions remain usable.
- Normalize remote paths and enforce save-root containment before desktop filesystem access.
- Treat a directory, unreadable target, missing target, or size mismatch as unavailable.
- Revalidate immediately before an external action to close the gap between rendering and clicking.
- Pass arguments directly to platform process APIs; do not construct shell command strings from filenames.
- On Android, grant only temporary read URI permission to open, chooser, and share intents.
- Do not request package-install permission as part of this feature. APK handling is delegated to the installed system handler.
- Do not let late directory checks from an old connection update the new connection's rows.

## Testing

Shared behavior tests cover:

- no availability action before a download task ends;
- one batch refresh after task completion;
- refresh after entering a directory;
- existing same-name, same-size files becoming available;
- size mismatch, missing file, unreadable target, and directory targets remaining unavailable;
- a real zero-byte file becoming available without treating a missing file as zero-byte;
- partial download failure exposing only rows that pass the local check;
- disconnect preserving actions and a newly established connection resetting state;
- stale asynchronous results being rejected by connection generation;
- external deletion or replacement clearing availability on re-entry or action.

Desktop tests cover path normalization and containment, Windows/macOS/Linux command argument construction, the Linux parent-directory fallback, no direct file-open action, hover/focus visibility, and localized accessible labels.

Android JVM tests cover destination URI resolution, MIME selection and fallback, open/open-with/share intent construction with read-only grants, unavailable-handler errors, expired permissions, checkbox-versus-open click targets, direct APK handoff without an app warning, and absence of persisted browsing history. Manual smoke testing covers SAF and default Downloads destinations on a supported Android device, Windows Explorer selection, and macOS Finder reveal.

## Out of Scope

- Persistent received-file history or a local-files database;
- per-file live availability updates during download;
- content-hash verification solely for enabling file actions;
- desktop direct-open behavior;
- Android delete, move, rename, or directory-wide device scanning;
- a custom Android APK warning or installer UI.
