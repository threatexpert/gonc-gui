# Android Send Content Picker Design

## Goal

Replace the Android sender's permanently visible file and folder buttons with a list-centered add experience. The sender must support files, folders, media, authored text, and clipboard content while preserving the existing live share-list update behavior and without changing `gonetcat`.

## Scope

This design changes only the Android application under `gonc-gui/android`.

Included:

- empty and populated send-list interaction;
- file, folder, image/video media, authored text, and clipboard import;
- image and video thumbnails;
- per-item removal, clear-all, and post-add scrolling;
- temporary-file ownership, naming, and cleanup;
- Android tests and Debug APK verification.

Excluded:

- changes to `gonetcat` or `mobilegonc.aar`;
- audio selection;
- clipboard files other than clipboard images;
- a custom media browser;
- editing or renaming a list item;
- persistence of generated send content across a fresh application launch.

## Confirmed Interaction

### Empty list

The two permanent **Add files** and **Add folder** buttons are removed. The empty list is one full-width clickable surface with this Chinese text:

`点击这里添加文件、文件夹、媒体或文字`

The English resource communicates the same meaning. Clipboard remains available in the picker but is omitted from the empty-state sentence to keep it concise.

### Populated list

The section title is `发送内容 · N`, where `N` is the number of roots in the send list. A quiet `清空` action appears at the upper right. Rows remain list-based. The last row is a full-width clickable entry with this Chinese text:

`点击这里继续添加`

Every content row has:

- a 48dp leading visual;
- a one-line, ellipsized display name;
- a secondary type and size description;
- a compact circular `×` removal control on the right.

The leading visual is an actual thumbnail for images and videos. A video thumbnail has a centered play overlay. Files, folders, and authored text use concise vector type icons. If thumbnail generation fails, the row falls back to its type icon.

### Add-type dialog

Clicking the empty state or final add row opens a native dialog titled:

`想加入什么内容？`

It contains five icon-and-label choices in this order:

1. 文件
2. 文件夹
3. 媒体
4. 文字
5. 剪贴板

`媒体` means images and videos only. `剪贴板` accepts text and images only.

## Architecture

The implementation extends the current native Java seams rather than introducing a second UI framework.

### `SendController`

`SendController` remains the owner of `shareItems`, list rendering, removal, clear-all, and live-session synchronization. It will:

- render the new empty and populated list states;
- open the add-type dialog;
- open the authored-text dialog;
- accept imported `ShareItem` instances from the host;
- delete owned temporary files when their items leave the list;
- request the main scroll container to reveal the final add row after a real addition;
- retain the list and owned files when the sender is merely stopped.

Clear-all and per-item removal remain available while sending. Both call the existing `session.updateShareItems(...)` path immediately. Clearing the list does not stop the active sender; a receiver refresh then sees an empty share root.

### `ModuleHost` and `MainActivity`

`ModuleHost` gains narrow callbacks for file, folder, and media selection and for revealing a send-list descendant after rendering. `MainActivity` continues to own Android intents, URI permissions, clipboard access, and activity results.

The existing request-code and `onActivityResult` structure is retained. This avoids an AndroidX Activity Result migration that is unrelated to the feature.

### Generated content helper

A small Android-only helper owns creation of authored-text and clipboard files. It will:

- create an isolated directory below `Context.getCacheDir()`;
- write text as UTF-8;
- copy clipboard image URI bytes immediately while permission is valid;
- infer a safe image extension from MIME type, falling back to a neutral binary extension when necessary;
- return a `ShareItem` marked with the exact app-owned file it may delete.

`ShareItem` will carry explicit owned-file metadata. Cleanup must check this metadata and the expected generated-content directory before deleting. User-selected URIs and source documents are never deleted.

### Thumbnail loader

A bounded Android-only thumbnail loader produces 48dp previews off the main thread. It will:

- load image thumbnails through `ContentResolver` or a bounded decode fallback;
- extract a representative video frame through platform media APIs;
- work for content selected through any entry point, including external Android shares and clipboard imports;
- cache a bounded number of bitmaps in memory;
- associate work with a render generation so a stale result cannot update a row created by a later full render;
- fall back silently to the row's vector type icon when preview loading fails.

No full-resolution image is decoded solely for list presentation.

## Content Import Flows

### Files

Reuse the existing multi-file `ACTION_OPEN_DOCUMENT` flow with persistable read permission when the provider supports it. Imported URIs are converted through the existing `loadShareItem(...)` path.

### Folder

Reuse the existing `ACTION_OPEN_DOCUMENT_TREE` flow and tree `ShareItem` representation.

### Media

On platform versions with the native system photo picker, open it for multiple images and videos. On older supported versions, fall back to a system document picker constrained to image and video MIME types. No broad storage permission or custom media query is introduced.

Cancellation is silent. If some returned media cannot be read, successfully loaded items are added and a short message reports the failed count.

### Authored text

The text choice opens a native multiline dialog titled `添加文字`. The dialog has cancel and add actions. An empty value remains in the dialog and is not submitted. A successful add writes an UTF-8 text file and appends it to the list.

### Clipboard

Clipboard import supports:

- plain/coerced text, written to an UTF-8 text file;
- a clipboard item URI whose MIME type is an image, copied immediately to an owned cache file.

It does not import video, audio, or arbitrary file clipboard items. Unsupported or empty clipboard content displays:

`剪贴板中没有可添加的文字或图片`

When a clipboard exposes both usable text and an image, the image is preferred because it preserves the richer content.

## Names and Deduplication

Generated names use source, local timestamp to the second, and a random short identifier:

- `text-20260721-154530-7f3a.txt`
- `clipboard-text-20260721-154531-9c21.txt`
- `clipboard-image-20260721-154532-a92c.png`

The random identifier is generated independently for each creation, and file creation must be exclusive. A rare collision retries with a new identifier.

Existing URI-based deduplication remains in place for user-selected files, folders, and media. Generated content has a unique URI and is therefore a distinct list entry even when its bytes match an earlier entry.

After at least one genuinely new item is committed, the Activity posts a scroll request after rendering to reveal `点击这里继续添加`. Selecting only duplicate URIs does not scroll.

## Cleanup Lifecycle

Owned generated files are deleted when:

- their individual row is removed;
- clear-all removes them;
- a fresh-launch reset discards the send list;
- creation fails before the item is committed.

Stopping a send session does not clear the list, so it also does not delete its owned files. Normal files, folders, media documents, and external provider content are never deleted by list cleanup.

## Error Handling

- Picker cancellation makes no state change and shows no error.
- Empty authored text is not created or appended.
- Empty or unsupported clipboard content shows the localized clipboard message.
- Temporary-file creation or clipboard-image copying failure leaves the list unchanged and shows a concise localized error.
- A thumbnail failure changes only presentation and falls back to an icon.
- Partial media import appends successful items and reports the number that failed.

## Testing and Verification

Android unit and source-contract tests cover:

- empty and populated list structure and exact localized strings;
- absence of the permanent file/folder buttons;
- five ordered add-dialog choices;
- clear-all and circular `×` actions;
- removal and clear-all while a session is active;
- URI deduplication and scroll only after a real addition;
- unique generated names and exclusive creation;
- UTF-8 authored and clipboard text;
- clipboard image acceptance and non-image rejection;
- safe owned-file cleanup without deletion of user content;
- image/video thumbnail classification, bounded caching, stale-result rejection, and icon fallback;
- system media picker selection with the image/video-only fallback;
- partial import behavior.

Verification commands include the complete Android unit-test suite and Debug APK assembly. If no Android device or emulator is available, the handoff explicitly states that device-level visual validation was not performed.

## Acceptance Criteria

The feature is complete when:

- Android no longer shows permanent file and folder add buttons;
- the approved empty and populated list interactions are present;
- the add dialog offers file, folder, media, text, and clipboard in the confirmed order;
- images and videos show thumbnails when available and safe fallbacks otherwise;
- text and clipboard imports create unique, safely owned temporary files;
- clear-all, per-item removal, and live session updates remain usable while sending;
- a real addition reveals the final continue-add entry;
- no `gonetcat` or `mobilegonc.aar` change is required;
- Android tests and Debug APK assembly pass.
