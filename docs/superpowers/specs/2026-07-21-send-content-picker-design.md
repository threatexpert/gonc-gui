# Send Content Picker Design

## Goal

Improve the desktop send screen so every kind of content is added through one compact picker. Support files, folders, authored text, and clipboard content while preserving the existing path-list transfer model and the persistent drag-and-drop affordance.

All implementation changes are limited to `gonc-gui`. Any required change to `gonetcat` is out of scope and must receive separate user approval before work continues.

## User Interface

The send list always has one `+ Add` button, whether the list is empty or populated. Clicking it opens a modal titled `What do you want to add?` with four actions: File, Folder, Text, and Clipboard.

The list keeps its current row-based presentation. Each row can be removed individually. A clear button (`x`) appears at the upper-right of the list area and removes every item. The clear button is hidden or disabled when there is nothing to clear.

When the list is empty, the drop area prominently displays `You can drag files or folders here`. When it contains items, the same hint remains visible in a quieter row below the items. Dragging files or folders into this region continues to append them.

Selecting Text opens a second modal with a multiline editor, Cancel, and Add to send list actions. Selecting Clipboard immediately attempts an import. A successful import closes the picker. A failed or unsupported import leaves the list unchanged and shows an actionable error in the picker.

The same behavior is provided in Chinese and English.

## Content Model and Data Flow

The React send list remains an ordered list of local filesystem paths. Existing files, selected folders, and dropped paths enter the list directly. Authored text and clipboard images or text are first materialized as files by the Go backend, which returns their paths to the frontend for normal list insertion.

Generated names use a content prefix, local timestamp with sub-second precision, and a cryptographically random suffix. Examples include `text-20260721-153012.123-a8f39c.txt` and `clipboard-image-20260721-153012.123-b71d20.png`. This avoids collisions without inspecting or mutating user files.

Clipboard import uses the following precedence so one user action has a predictable result:

1. Files or folders already present in the clipboard.
2. A bitmap image, written as PNG.
3. Unicode text, written as UTF-8 text.

If a clipboard file list contains several paths, all valid paths are appended. Duplicate paths continue to be removed by the frontend's existing append logic.

## Platform Support

File and folder selection use the existing Wails dialogs on every supported desktop platform. Authored text and clipboard text use the existing framework and Go filesystem capabilities on every platform.

On Windows, `gonc-gui` adds a native clipboard reader for file-drop lists, PNG/DIB-family bitmap data, and Unicode text. It uses stable Win32 clipboard APIs through the project's existing Windows dependencies. It does not invoke PowerShell or other external programs.

On macOS and Linux, non-text clipboard formats are enabled only if they can be accessed directly and reliably through the current framework or native application dependencies. The implementation must not add command-line fallbacks such as AppleScript, `pbpaste`, `xclip`, or `wl-paste`. When a non-text format is unavailable, the UI explains that the clipboard content type is unsupported on the current platform. This is an explicit capability difference, not a silent failure.

## Temporary File Lifecycle

The backend owns an application-specific temporary directory and a registry of generated paths. Authored text, clipboard text, and clipboard images are created only inside that directory. User-selected or clipboard-referenced files are never registered as generated files and are never deleted by the application.

When a generated item is removed, the backend removes it after the active share source no longer references it. If an operating-system file handle prevents immediate removal, cleanup is deferred. Application shutdown removes the remaining generated files and the application-specific directory after transfer shutdown. Cleanup is restricted to paths proven to be owned by the registry and contained by the resolved temporary directory.

## Running Transfer and Empty Lists

Starting a new send session still requires at least one item. Once a send session is running, users may append items, remove any item, remove the last item, or clear the entire list.

Every list mutation is sent to the running dynamic file source, including an empty list. `gonc-gui` represents the empty state with a valid zero-root `OSFileSource`, whose virtual root can be listed successfully and contains no entries. A receiver refresh therefore returns an empty directory rather than stale entries or an error. This uses the existing public behavior of the current `gonetcat` dependency and requires no modification to that repository.

While a send session is running, each add, remove, or clear action first submits the proposed complete path list to the backend. The frontend commits that proposed list only after synchronization succeeds. If synchronization fails, the UI reports the error and keeps the last successfully synchronized list. A newly generated temporary file from a failed add is released for cleanup.

## Error Handling

The picker distinguishes an empty clipboard, an unsupported clipboard format, a clipboard access failure, invalid clipboard paths, and temporary-file creation failure. Errors are localized and do not close the picker or change the current send list.

Empty authored text is rejected in the text modal. Whitespace is preserved when non-empty content is stored. Invalid or vanished clipboard file paths are omitted; if none remain and no lower-priority supported representation can be imported, the operation reports an error.

## Verification

Backend tests cover unique generated names, UTF-8 text materialization, ownership-safe cleanup, clipboard representation precedence, invalid clipboard paths, Windows clipboard decoding helpers, and updating a running dynamic source to and from an empty list without changing `gonetcat`.

Frontend-focused tests cover picker state transitions, authored-text validation, appending returned paths, single-item removal, clear-all, rollback on synchronization failure, and the persistent drop hint. The TypeScript build, frontend test suite, Go tests, and production frontend build must pass before completion is claimed.

## Non-Goals

- Editing or previewing selected content.
- Replacing the row-based file list with LocalSend's summary-card layout.
- Sending rich-text/HTML as a distinct content type.
- Copying clipboard-referenced user files into temporary storage.
- Modifying `gonetcat` without separate approval.
