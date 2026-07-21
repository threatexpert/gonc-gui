# Send Add Entry Refinement Design

## Goal

Make the send list's persistent drag hint the only entry point for adding content, keep that entry visible after additions, and improve the content picker with lightweight icons.

This refinement changes only `gonc-gui`. It does not change clipboard extraction, transfer semantics, temporary-file ownership, or `gonetcat`.

## Persistent Add and Drop Entry

Remove the standalone `+ Add` button. The final row of the send list is always a full-width semantic button with this Chinese copy:

`点击这里添加，也可拖放文件或目录到这里`

The English copy is `Click here to add, or drop files or folders here`. The entire row opens the existing add-content picker. It visually retains the quiet drag-hint treatment, uses the pointer cursor, supports keyboard activation through native button semantics, and has a visible focus state.

The row is prominent and centered when the list is empty. When items exist, it remains as the compact final row below them. It stays inside the existing Wails drop target so files and folders can still be dropped anywhere in the list area.

The row is disabled while the send startup reservation, a share-list mutation, or a picker operation is pending. Disabled styling communicates that the action is temporarily unavailable and preserves the existing concurrency guarantees.

## Automatic Scrolling

The path-list element owns a React ref. After a successful operation adds at least one new unique path and commits the new list, the list scrolls to its maximum vertical offset so the final add/drop row is visible.

Scrolling is immediate rather than animated. It does not occur when the picker is cancelled, an operation fails, or every proposed path is a duplicate and the committed list is unchanged. Removal and clear-all do not trigger scrolling.

The scroll request is tied to the committed list transition, not the initial file dialog or clipboard result, so running transfers scroll only after backend synchronization succeeds.

## Picker Icons

The File, Folder, Text, and Clipboard options each receive a small inline outline SVG before the label. The icons use `currentColor`, consistent stroke width, rounded caps and joins, and no external icon package or asset files.

Each icon is decorative because the adjacent visible label names the action; it is hidden from assistive technology with `aria-hidden="true"`. Buttons retain their existing localized labels, disabled behavior, hover styling, and focus styling.

## Error Handling and Accessibility

Opening the picker from the hint row uses the existing guarded `openAddPicker` path. Pending actions cannot open duplicate dialogs. Automatic scrolling never changes focus and does not run after failed synchronization.

The semantic button supplies Enter and Space activation automatically. The picker remains labelled by its existing title, and the SVG icons do not create duplicate accessible names.

## Verification

Frontend tests verify that the standalone Add button is absent; the full-width hint button exists in empty and populated states through the single persistent render path; the exact Chinese copy is present; the button invokes the picker and respects pending state; each picker option contains a decorative inline SVG; and scrolling occurs only after a successful unique addition.

The existing transaction-coordinator tests, complete frontend test suite, TypeScript build, Go tests, and production build must remain green. Native visual smoke should verify empty and populated list presentation, pointer/focus behavior, bottom visibility after adding, icon alignment, and narrow-window layout when an interactive desktop is available.

## Non-Goals

- Changing the list row format or clear/remove controls.
- Adding animated scrolling.
- Introducing an icon dependency.
- Changing clipboard or generated-file behavior.
- Modifying `gonetcat`.
