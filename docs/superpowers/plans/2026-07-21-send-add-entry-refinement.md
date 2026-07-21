# Send Add Entry Refinement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the persistent add/drop hint the only add-content entry, automatically reveal it after successful additions, and add outline SVG icons to the picker choices.

**Architecture:** Keep the existing picker and transactional share coordinator unchanged. Add small pure helpers for detecting a real addition and scrolling a list element, then let `App.tsx` schedule the DOM scroll only after `appendSharePaths` succeeds. Render a single semantic hint button inside the drop zone and a focused icon component for the four inline SVGs.

**Tech Stack:** React 18, TypeScript 4.6, CSS, Node built-in test runner, existing Wails v2 desktop application.

## Global Constraints

- Modify only `gonc-gui`; do not modify `gonetcat`.
- Remove the standalone `+ Add` button.
- The exact Chinese hint is `点击这里添加，也可拖放文件或目录到这里`.
- The exact English hint is `Click here to add, or drop files or folders here`.
- The entire hint row is a semantic button and the only add-content entry.
- Keep the row inside the existing Wails drop target in both empty and populated states.
- Disable the row while send startup, share mutation, or picker work is pending.
- Scroll immediately to the bottom only after at least one unique path is successfully committed.
- Do not scroll after cancellation, failure, duplicate-only add, removal, or clear-all.
- Use inline decorative outline SVG icons with `currentColor`; add no icon dependency.
- Preserve all pre-existing uncommitted changes, including `frontend/src/App.css`, `frontend/package.json.md5`, and `frontend/wailsjs/go/models.ts`; stage only task-owned hunks/files.
- Preserve existing clipboard, temporary-file, transaction, clear-all, per-item removal, localization, and accessibility behavior.

---

## File Map

- Modify `frontend/src/sendContentState.ts`: pure addition/scroll helpers.
- Modify `frontend/tests/sendContentState.test.ts`: behavior and source-contract coverage.
- Create `frontend/src/SendContentOptionIcon.tsx`: four decorative inline outline SVGs.
- Modify `frontend/src/App.tsx`: single hint-button entry, successful-add scrolling, and picker icons.
- Modify `frontend/src/App.css`: hint-button, icon, picker layout, and focus/disabled styling.

### Task 1: Successful-Addition Scroll Behavior

**Files:**
- Modify: `frontend/src/sendContentState.ts`
- Modify: `frontend/tests/sendContentState.test.ts`
- Modify: `frontend/src/App.tsx`

**Interfaces:**
- Produces: `addedUniquePaths(current: string[], proposed: string[]): boolean`.
- Produces: `scrollListToBottom(list: {scrollTop: number; scrollHeight: number}): void`.
- Consumes: existing `appendUniquePaths` and `queueSharePathMutation`.

- [ ] **Step 1: Write failing pure behavior tests**

Add tests that prove only a longer committed unique list qualifies and that the scroll helper assigns the maximum offset:

```ts
test('only a newly committed unique path requests bottom scrolling', () => {
  assert.equal(addedUniquePaths(['a'], ['a', 'b']), true);
  assert.equal(addedUniquePaths(['a'], ['a']), false);
  assert.equal(addedUniquePaths(['a', 'b'], ['a']), false);
});

test('scroll helper reveals the final list row', () => {
  const list = {scrollTop: 10, scrollHeight: 240};
  scrollListToBottom(list);
  assert.equal(list.scrollTop, 240);
});
```

Add a focused App source contract requiring a `pathListRef`, a successful-add guard, and `requestAnimationFrame` scheduling after the awaited mutation result. Assert remove and clear handlers do not call the scroll helper.

- [ ] **Step 2: Run tests and verify RED**

From `frontend`, run: `npm run test:send-content`

Expected: FAIL because `addedUniquePaths` and `scrollListToBottom` are not exported and App has no list ref.

- [ ] **Step 3: Implement pure helpers**

```ts
export function addedUniquePaths(current: string[], proposed: string[]): boolean {
  return proposed.length > current.length;
}

export function scrollListToBottom(list: {scrollTop: number; scrollHeight: number}) {
  list.scrollTop = list.scrollHeight;
}
```

- [ ] **Step 4: Wire scrolling after successful addition**

Add `const pathListRef = useRef<HTMLDivElement | null>(null)`. In `appendSharePaths`, record whether the queued proposal grows the current unique list. Await `queueSharePathMutation`; only when it returns success and the proposal grew, schedule:

```ts
window.requestAnimationFrame(() => {
  const list = pathListRef.current;
  if (list) {
    scrollListToBottom(list);
  }
});
```

Attach `ref={pathListRef}` to `.path-list`. Do not add this scheduling to remove or clear handlers. Because scheduling occurs after the awaited transaction, a running sender scrolls only after backend synchronization and frontend commit succeed.

- [ ] **Step 5: Verify and commit**

Run:

```powershell
npm run test:send-content
npm test
npm run build
```

Expected: focused tests pass, full frontend tests pass, and production build exits 0.

Stage only `sendContentState.ts`, its test, and the intended `App.tsx` hunks. Commit:

```powershell
git commit -m "feat: reveal add hint after additions"
```

### Task 2: Clickable Hint Entry and Picker Icons

**Files:**
- Create: `frontend/src/SendContentOptionIcon.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/App.css`
- Modify: `frontend/tests/sendContentState.test.ts`

**Interfaces:**
- Produces: `type SendContentOptionKind = 'file' | 'folder' | 'text' | 'clipboard'`.
- Produces: `SendContentOptionIcon({kind}: {kind: SendContentOptionKind}): JSX.Element`.
- Consumes: existing `openAddPicker`, pending state, `dropHintMode`, and picker handlers.

- [ ] **Step 1: Write failing UI contract tests**

Update the source-contract test to require:

```ts
assert.doesNotMatch(app, /className="primary-light add-share-content"/);
assert.match(app, /className={`drop-hint-action \$\{dropHintMode\(sharePaths\)\}`}/);
assert.match(app, /onClick={openAddPicker}/);
assert.match(app, /disabled={shareMutationPending \|\| pickerPending \|\| startPending}/);
assert.match(app, /点击这里添加，也可拖放文件或目录到这里/);
assert.match(app, /Click here to add, or drop files or folders here/);
for (const kind of ['file', 'folder', 'text', 'clipboard']) {
  assert.match(app, new RegExp(`SendContentOptionIcon kind="${kind}"`));
}
```

Read `SendContentOptionIcon.tsx` and assert it contains one `<svg`, `viewBox="0 0 24 24"`, `fill="none"`, `stroke="currentColor"`, and `aria-hidden="true"`. Assert CSS supplies pointer cursor, full width, empty/compact sizing, focus-visible styling, icon sizing, and label alignment.

- [ ] **Step 2: Run tests and verify RED**

Run: `npm run test:send-content`

Expected: FAIL because the standalone Add button still exists, the hint is a paragraph, and the icon component is absent.

- [ ] **Step 3: Implement the icon component**

Create a small switch-based component. Use one decorative SVG wrapper with `viewBox="0 0 24 24"`, `fill="none"`, `stroke="currentColor"`, `strokeWidth={1.8}`, `strokeLinecap="round"`, `strokeLinejoin="round"`, and `aria-hidden="true"`. Render these outline concepts:

- File: page outline with folded corner.
- Folder: folder tab and body outline.
- Text: three horizontal text lines.
- Clipboard: clipboard outline with top clip.

Do not import an icon package or create image assets.

- [ ] **Step 4: Replace the add entry and decorate picker choices**

Replace the persistent `<p>` and standalone Add button with one button inside `.path-list`:

```tsx
<button
  ref={addPickerButtonRef}
  type="button"
  className={`drop-hint-action ${dropHintMode(sharePaths)}`}
  disabled={shareMutationPending || pickerPending || startPending}
  onClick={openAddPicker}
>
  {t.dropHint}
</button>
```

Use one localized `dropHint` value in both states with the exact approved copy; remove `dropHintCompact` and the unused `add` copy only if no other consumer remains. Place `<SendContentOptionIcon kind="..." />` before the visible label in each of the four picker buttons.

- [ ] **Step 5: Style the semantic hint and icons**

Remove obsolete `.add-share-content`, `.drop-hint`, and `.drop-hint.compact` rules. Add `.drop-hint-action` as a full-width, borderless, transparent button with centered text, `cursor: pointer`, inherited font, and colors matching the old hint. Give `.empty` a 78px minimum height and stronger weight; give `.compact` a 28px minimum height, 11px font, and quieter opacity. Add hover, `:focus-visible`, and `:disabled` styles; disabled must use `cursor: not-allowed`.

Make picker buttons flex-column or inline-flex with an 18–22px SVG before the label. Preserve the four-column desktop and two-column narrow layout.

- [ ] **Step 6: Verify full feature and working-tree ownership**

Run:

```powershell
npm run test:send-content
npm test
npm run build
Set-Location ..
go test ./...
git diff --check
git status --short
```

Expected: all tests/build pass. The known pre-existing uncommitted frontend files remain present and are not staged unless a task-owned hunk shares `App.css`; use selective staging so only the new hint/icon CSS enters the commit.

- [ ] **Step 7: Commit**

Stage `SendContentOptionIcon.tsx`, intended `App.tsx`/test hunks, and only intended `App.css` hunks. Commit:

```powershell
git commit -m "feat: make send hint the add entry"
```

Do not stage `frontend/package.json.md5`, `frontend/wailsjs/go/models.ts`, or the pre-existing `.about-error` CSS hunk.

