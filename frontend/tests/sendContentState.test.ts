import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync, readFileSync} from 'node:fs';
import {
  addedUniquePaths,
  appendUniquePaths,
  clipboardErrorKey,
  createSharePathTransactionCoordinator,
  createShareStartCoordinator,
  latchSendRunningAfterStart,
  dropHintMode,
  nextAddPickerState,
  removePath,
  scrollListToBottom,
  textCanSubmit,
  type SharePathTransactionDependencies,
} from '../src/sendContentState.js';

type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T | PromiseLike<T>) => void;
  reject: (reason?: unknown) => void;
};

function deferred<T = void>(): Deferred<T> {
  let resolve!: Deferred<T>['resolve'];
  let reject!: Deferred<T>['reject'];
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return {promise, resolve, reject};
}

const flushPromises = () => new Promise<void>((resolve) => setTimeout(resolve, 0));

const createCoordinator = (deps: SharePathTransactionDependencies) =>
  createSharePathTransactionCoordinator(deps);

test('paths can be cleared while preserving deterministic append order', () => {
  assert.deepEqual(appendUniquePaths(['a'], ['b', 'a', 'c']), ['a', 'b', 'c']);
  assert.deepEqual(removePath(['a'], 'a'), []);
});

test('authored text rejects only an empty string', () => {
  assert.equal(textCanSubmit(''), false);
  assert.equal(textCanSubmit('   '), true);
});

test('picker transitions between closed, chooser, and authored text states', () => {
  assert.equal(nextAddPickerState('closed', 'open'), 'choose');
  assert.equal(nextAddPickerState('choose', 'text'), 'text');
  assert.equal(nextAddPickerState('text', 'back'), 'choose');
  assert.equal(nextAddPickerState('choose', 'close'), 'closed');
  assert.equal(nextAddPickerState('text', 'close'), 'closed');
});

test('drop hint is empty without paths and compact with paths', () => {
  assert.equal(dropHintMode([]), 'empty');
  assert.equal(dropHintMode(['a']), 'compact');
});

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

test('App scrolls only after a successful unique addition', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /const pathListRef = useRef<HTMLDivElement \| null>\(null\)/);
  assert.match(app, /ref=\{pathListRef\}/);
  const appendStart = app.indexOf('async function appendSharePaths');
  const appendEnd = app.indexOf('  function appendLog', appendStart);
  const appendSource = app.slice(appendStart, appendEnd);
  assert.match(appendSource, /addedUniquePaths/);
  assert.match(appendSource, /await queueSharePathMutation/);
  assert.match(appendSource, /if \(succeeded && added\)/);
  assert.match(appendSource, /requestAnimationFrame/);
  assert.match(appendSource, /scrollListToBottom/);

  const removeStart = app.indexOf('async function removeSharePath');
  const removeEnd = app.indexOf('  function openAddPicker', removeStart);
  assert.doesNotMatch(app.slice(removeStart, removeEnd), /scrollListToBottom|requestAnimationFrame/);
});

test('clipboard stable codes classify every actionable failure without backend prose', () => {
  assert.equal(clipboardErrorKey('Error: GONC_CLIPBOARD_EMPTY'), 'empty');
  assert.equal(clipboardErrorKey('Error: GONC_CLIPBOARD_UNSUPPORTED'), 'platformUnsupported');
  assert.equal(clipboardErrorKey('Error: GONC_CLIPBOARD_BUSY'), 'busy');
  assert.equal(clipboardErrorKey('Error: GONC_CLIPBOARD_ACCESS: GetClipboardData failed'), 'access');
  assert.equal(clipboardErrorKey('Error: GONC_CLIPBOARD_INVALID_PATHS'), 'invalidPaths');
  assert.equal(clipboardErrorKey('Error: GONC_CLIPBOARD_TEMPORARY_FILE: create failed'), 'temporaryFile');
  assert.equal(clipboardErrorKey('other clipboard failure'), null);

  const app = readFileSync('src/App.tsx', 'utf8');
  for (const key of ['clipboardEmpty', 'clipboardPlatformUnsupported', 'clipboardBusy', 'clipboardAccess', 'clipboardInvalidPaths', 'clipboardTemporaryFile']) {
    assert.equal((app.match(new RegExp(`${key}:`, 'g')) || []).length, 2, `${key} must have zh/en text`);
  }
  for (const key of ['platformUnsupported', 'busy', 'access', 'invalidPaths', 'temporaryFile']) {
    assert.match(app, new RegExp(`case '${key}':\\s*return t\\.`));
  }
  assert.doesNotMatch(app, /localized === raw \? t\.clipboardImportFailed/);
});

test('send content picker exposes all choices and persistent list controls', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /addFileChoice/);
  assert.match(app, /addFolderChoice/);
  assert.match(app, /addTextChoice/);
  assert.match(app, /addClipboardChoice/);
  assert.match(app, /className="add-picker-backdrop"/);
  assert.match(app, /function clearSharePaths/);
  assert.doesNotMatch(app, /className="primary-light add-share-content"/);
  assert.match(app, /className={`drop-hint-action \$\{dropHintMode\(sharePaths\)\}`}/);
  assert.match(app, /onClick=\{openAddPicker\}/);
  assert.match(app, /disabled=\{shareMutationPending \|\| pickerPending \|\| startPending\}/);
  assert.match(app, /点击这里添加，也可拖放文件或目录到这里/);
  assert.match(app, /Click here to add, or drop files or folders here/);
  assert.doesNotMatch(app, /dropHintCompact/);
  for (const kind of ['file', 'folder', 'text', 'clipboard']) {
    assert.match(app, new RegExp(`SendContentOptionIcon kind="${kind}"`));
  }
});

test('picker uses decorative inline outline SVG icons', () => {
  const iconPath = 'src/SendContentOptionIcon.tsx';
  assert.equal(existsSync(iconPath), true, 'icon component must exist');
  const icon = readFileSync(iconPath, 'utf8');
  assert.match(icon, /<svg/);
  assert.match(icon, /viewBox="0 0 24 24"/);
  assert.match(icon, /fill="none"/);
  assert.match(icon, /stroke="currentColor"/);
  assert.match(icon, /aria-hidden="true"/);
  for (const kind of ['file', 'folder', 'text', 'clipboard']) {
    assert.match(icon, new RegExp(`kind === '${kind}'|case '${kind}'`));
  }

  const css = readFileSync('src/App.css', 'utf8');
  assert.match(css, /\.drop-hint-action\s*\{[^}]*width:\s*100%[^}]*cursor:\s*pointer/s);
  assert.match(css, /\.drop-hint-action\.empty\s*\{[^}]*min-height:\s*78px/s);
  assert.match(css, /\.drop-hint-action\.compact\s*\{[^}]*min-height:\s*28px/s);
  assert.match(css, /\.drop-hint-action:focus-visible/);
  assert.match(css, /\.drop-hint-action:disabled\s*\{[^}]*cursor:\s*not-allowed/s);
  assert.match(css, /\.add-picker-option-icon\s*\{/);
  assert.match(css, /\.add-picker-options button\s*\{[^}]*flex-direction:\s*row/s);
});

test('running share list changes synchronize transactionally instead of by effect', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /createSharePathTransactionCoordinator/);
  assert.match(app, /update: UpdateSharePaths/);
  assert.match(app, /shareCoordinatorRef\.current!\.enqueue/);
  assert.doesNotMatch(app, /if \(sharePaths\.length === 0\)\s*\{\s*return;\s*\}\s*UpdateSharePaths/);
});

test('picker operations use a synchronous pending guard against rapid duplicate actions', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /pickerPendingRef\.current = true/);
  assert.match(app, /if \(pickerPendingRef\.current \|\| shareMutationPendingRef\.current \|\| startPendingRef\.current\)/);
  assert.match(app, /pickerPendingRef\.current = false/);
});

test('App delegates rapid share mutations and picker failures to the coordinator result', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /shareCoordinatorRef\.current!\.enqueue/);
  assert.match(app, /releaseAfterSuccess/);
  assert.doesNotMatch(app, /async function commitSharePaths/);
  assert.match(app, /textShareFailed/);
});

test('picker remains usable in short windows', () => {
  const css = readFileSync('src/App.css', 'utf8');
  assert.match(css, /\.add-picker-dialog\s*\{[^}]*max-height:[^}]*overflow-y:\s*auto/s);
});

test('coordinator serializes rapid mutations against the last committed list', async () => {
  let current = ['a'];
  const updates: string[][] = [];
  const commits: string[][] = [];
  const pending: boolean[] = [];
  const gates = [deferred(), deferred()];
  const coordinator = createCoordinator({
    getPaths: () => current,
    isRunning: () => true,
    update: async (paths: string[]) => {
      updates.push([...paths]);
      await gates[updates.length - 1].promise;
    },
    commit: (paths: string[]) => {
      current = paths;
      commits.push([...paths]);
    },
    release: async () => undefined,
    clearError: () => undefined,
    showError: () => undefined,
    formatError: String,
    onPendingChange: (value: boolean) => { pending.push(value); },
  });

  const first = coordinator.enqueue({propose: (paths: string[]) => appendUniquePaths(paths, ['b'])});
  const second = coordinator.enqueue({propose: (paths: string[]) => appendUniquePaths(paths, ['c'])});
  await flushPromises();
  assert.deepEqual(updates, [['a', 'b']]);
  assert.deepEqual(commits, []);
  assert.deepEqual(pending, [true]);

  gates[0].resolve();
  await flushPromises();
  assert.deepEqual(updates, [['a', 'b'], ['a', 'b', 'c']]);
  gates[1].resolve();
  assert.deepEqual(await Promise.all([first, second]), [
    {ok: true, changed: true},
    {ok: true, changed: true},
  ]);
  assert.deepEqual(commits, [['a', 'b'], ['a', 'b', 'c']]);
  assert.deepEqual(pending, [true, false]);
});

test('startup waits for an admitted mutation then reads the latest committed paths', async () => {
  let current = ['a'];
  const updateGate = deferred();
  const starts: string[][] = [];
  const coordinator = createCoordinator({
    getPaths: () => current,
    isRunning: () => true,
    update: async () => { await updateGate.promise; },
    commit: (paths) => { current = paths; },
    release: async () => undefined,
    clearError: () => undefined,
    showError: () => undefined,
    formatError: String,
    onPendingChange: () => undefined,
  });
  const mutation = coordinator.enqueue({propose: (paths) => [...paths, 'b']});
  const startup = createShareStartCoordinator(coordinator, () => current);
  const start = startup.run(async (paths) => { starts.push([...paths]); });
  await flushPromises();
  assert.deepEqual(starts, []);
  updateGate.resolve();
  await Promise.all([mutation, start]);
  assert.deepEqual(starts, [['a', 'b']]);
});

test('startup reservation synchronously rejects later list mutations', async () => {
  let current = ['a'];
  const startGate = deferred();
  const coordinator = createCoordinator({
    getPaths: () => current,
    isRunning: () => false,
    update: async () => undefined,
    commit: (paths) => { current = paths; },
    release: async () => undefined,
    clearError: () => undefined,
    showError: () => undefined,
    formatError: String,
    onPendingChange: () => undefined,
  });
  const startup = createShareStartCoordinator(coordinator, () => current);
  const start = startup.run(async () => { await startGate.promise; });
  assert.equal(startup.isPending(), true);
  assert.deepEqual(
    await coordinator.enqueue({propose: (paths) => [...paths, 'b']}),
    {ok: false, changed: false, error: 'startupPending'},
  );
  assert.deepEqual(current, ['a']);
  startGate.resolve();
  await start;
  assert.equal(startup.isPending(), false);
});

test('startup rejection releases generated content that cannot be admitted', async () => {
  const released: string[][] = [];
  const coordinator = createCoordinator({
    getPaths: () => ['a'],
    isRunning: () => false,
    update: async () => undefined,
    commit: () => undefined,
    release: async (paths) => { released.push([...paths]); },
    clearError: () => undefined,
    showError: () => undefined,
    formatError: String,
    onPendingChange: () => undefined,
  });
  const gate = deferred();
  const startup = createShareStartCoordinator(coordinator, () => ['a']);
  const start = startup.run(async () => { await gate.promise; });
  await coordinator.enqueue({propose: (paths) => [...paths, 'generated.txt'], generatedOnFailure: ['generated.txt']});
  assert.deepEqual(released, [['generated.txt']]);
  gate.resolve();
  await start;
});

test('successful send startup latches the running predicate before mutations resume', () => {
  let running = false;
  latchSendRunningAfterStart('send', (value) => { running = value; });
  assert.equal(running, true);

  running = false;
  latchSendRunningAfterStart('receive', (value) => { running = value; });
  assert.equal(running, false);

  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /await StartTransfer\([\s\S]*?latchSendRunningAfterStart\(mode, \(running\) => \{ sendRunningRef\.current = running; \}\)/);
});

test('a later queued success clears an earlier operation failure when execution begins', async () => {
  let current = ['a'];
  let updateCount = 0;
  const events: string[] = [];
  const coordinator = createCoordinator({
    getPaths: () => current,
    isRunning: () => true,
    update: async () => {
      updateCount += 1;
      if (updateCount === 1) {
        throw new Error('offline');
      }
    },
    commit: (paths: string[]) => { current = paths; events.push(`commit:${paths.join(',')}`); },
    release: async () => undefined,
    clearError: () => { events.push('clear'); },
    showError: (message: string) => { events.push(`error:${message}`); },
    formatError: () => 'offline',
    onPendingChange: () => undefined,
  });

  const failed = coordinator.enqueue({propose: (paths: string[]) => [...paths, 'b']});
  const succeeded = coordinator.enqueue({propose: (paths: string[]) => [...paths, 'c']});
  assert.deepEqual(await failed, {ok: false, changed: false, error: 'offline'});
  assert.deepEqual(await succeeded, {ok: true, changed: true});
  assert.deepEqual(events, ['clear', 'error:offline', 'clear', 'commit:a,c']);
});

test('running clear sends an empty full list before committing it', async () => {
  let current = ['a'];
  const gate = deferred();
  const events: string[] = [];
  const coordinator = createCoordinator({
    getPaths: () => current,
    isRunning: () => true,
    update: async (paths: string[]) => {
      events.push(`update:${JSON.stringify(paths)}`);
      await gate.promise;
    },
    commit: (paths: string[]) => {
      current = paths;
      events.push(`commit:${JSON.stringify(paths)}`);
    },
    release: async () => undefined,
    clearError: () => undefined,
    showError: () => undefined,
    formatError: String,
    onPendingChange: () => undefined,
  });

  const result = coordinator.enqueue({propose: () => []});
  await flushPromises();
  assert.deepEqual(events, ['update:[]']);
  assert.deepEqual(current, ['a']);
  gate.resolve();
  assert.deepEqual(await result, {ok: true, changed: true});
  assert.deepEqual(events, ['update:[]', 'commit:[]']);
});

test('stopped coordinator skips backend update and commits locally', async () => {
  let current = ['a'];
  let updateCalls = 0;
  const coordinator = createCoordinator({
    getPaths: () => current,
    isRunning: () => false,
    update: async () => { updateCalls += 1; },
    commit: (paths: string[]) => { current = paths; },
    release: async () => undefined,
    clearError: () => undefined,
    showError: () => undefined,
    formatError: String,
    onPendingChange: () => undefined,
  });

  assert.deepEqual(await coordinator.enqueue({propose: (paths: string[]) => [...paths, 'b']}), {ok: true, changed: true});
  assert.equal(updateCalls, 0);
  assert.deepEqual(current, ['a', 'b']);
});

test('failed update retains the prior list, reports its own error, and rolls back generated paths', async () => {
  let current = ['a'];
  const released: string[][] = [];
  const visibleErrors: string[] = [];
  const coordinator = createCoordinator({
    getPaths: () => current,
    isRunning: () => true,
    update: async () => { throw new Error('offline'); },
    commit: (paths: string[]) => { current = paths; },
    release: async (paths: string[]) => { released.push([...paths]); },
    clearError: () => undefined,
    showError: (message: string) => { visibleErrors.push(message); },
    formatError: (error: unknown) => `mapped:${String(error)}`,
    onPendingChange: () => undefined,
  });

  const result = await coordinator.enqueue({
    propose: (paths: string[]) => [...paths, 'generated.txt'],
    generatedOnFailure: ['generated.txt'],
  });
  assert.deepEqual(result, {ok: false, changed: false, error: 'mapped:Error: offline'});
  assert.deepEqual(current, ['a']);
  assert.deepEqual(released, [['generated.txt']]);
  assert.deepEqual(visibleErrors, ['mapped:Error: offline']);
});

test('successful removal cleanup completes inside the queue before the next mutation', async () => {
  let current = ['generated.txt'];
  const releaseGate = deferred();
  const updates: string[][] = [];
  const coordinator = createCoordinator({
    getPaths: () => current,
    isRunning: () => true,
    update: async (paths: string[]) => { updates.push([...paths]); },
    commit: (paths: string[]) => { current = paths; },
    release: async () => { await releaseGate.promise; },
    clearError: () => undefined,
    showError: () => undefined,
    formatError: String,
    onPendingChange: () => undefined,
  });

  const removal = coordinator.enqueue({
    propose: () => [],
    releaseAfterSuccess: (before: string[]) => before,
  });
  const addition = coordinator.enqueue({propose: (paths: string[]) => [...paths, 'b']});
  await flushPromises();
  assert.deepEqual(updates, [[]]);
  releaseGate.resolve();
  await removal;
  await addition;
  assert.deepEqual(updates, [[], ['b']]);
});

test('no-op skips backend and commit while releasing an unused generated path', async () => {
  let updateCalls = 0;
  let commitCalls = 0;
  const released: string[][] = [];
  const coordinator = createCoordinator({
    getPaths: () => ['a'],
    isRunning: () => true,
    update: async () => { updateCalls += 1; },
    commit: () => { commitCalls += 1; },
    release: async (paths: string[]) => { released.push([...paths]); },
    clearError: () => undefined,
    showError: () => undefined,
    formatError: String,
    onPendingChange: () => undefined,
  });

  const result = await coordinator.enqueue({
    propose: (paths: string[]) => [...paths],
    generatedOnFailure: ['unused.txt'],
  });
  assert.deepEqual(result, {ok: true, changed: false});
  assert.equal(updateCalls, 0);
  assert.equal(commitCalls, 0);
  assert.deepEqual(released, [['unused.txt']]);
});

test('text modal disables editing and submission for mutation, picker, or startup pending', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /textarea[^>]*disabled=\{pickerPending \|\| shareMutationPending \|\| startPending\}/s);
  assert.match(app, /type="submit"[^>]*disabled=\{pickerPending \|\| shareMutationPending \|\| startPending \|\| !textCanSubmit\(authoredText\)\}/s);
  assert.match(app, /className="add-picker-close" disabled=\{pickerPending \|\| shareMutationPending \|\| startPending\}/);
  assert.match(app, /type="button" className="secondary" disabled=\{pickerPending \|\| shareMutationPending \|\| startPending\}/);
  assert.match(app, /function closeAddPicker[\s\S]*?if \(\(pickerPendingRef\.current \|\| shareMutationPendingRef\.current \|\| startPendingRef\.current\) && !force\)/);
  assert.match(app, /if \(event\.key === 'Escape'\)[\s\S]*?closeAddPicker\(\)/);
});
