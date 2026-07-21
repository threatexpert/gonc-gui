import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {
  appendUniquePaths,
  clipboardErrorKey,
  createSharePathTransactionCoordinator,
  dropHintMode,
  nextAddPickerState,
  removePath,
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

test('clipboard errors distinguish empty content from unsupported platform formats', () => {
  assert.equal(clipboardErrorKey('clipboard is empty'), 'empty');
  assert.equal(clipboardErrorKey('Error: native clipboard is unsupported'), 'platformUnsupported');
  assert.equal(clipboardErrorKey('clipboard format is unsupported'), 'platformUnsupported');
  assert.equal(clipboardErrorKey('clipboard is busy'), 'busy');
  assert.equal(clipboardErrorKey('other clipboard failure'), null);

  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /clipboardPlatformUnsupported: '当前平台不支持从剪贴板导入文件或图片。请复制文字后重试。'/);
  assert.match(app, /clipboardPlatformUnsupported: 'This platform does not support importing clipboard files or images\. Copy text and try again\.'/);
  assert.match(app, /case 'platformUnsupported':\s*return t\.clipboardPlatformUnsupported/);
});

test('send content picker exposes all choices and persistent list controls', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /addFileChoice/);
  assert.match(app, /addFolderChoice/);
  assert.match(app, /addTextChoice/);
  assert.match(app, /addClipboardChoice/);
  assert.match(app, /className="add-picker-backdrop"/);
  assert.match(app, /function clearSharePaths/);
  assert.match(app, /dropHintCompact/);
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
  assert.match(app, /if \(pickerPendingRef\.current \|\| shareMutationPendingRef\.current\)/);
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

test('text modal disables editing and submission for either pending source', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /textarea[^>]*disabled=\{pickerPending \|\| shareMutationPending\}/s);
  assert.match(app, /type="submit"[^>]*disabled=\{pickerPending \|\| shareMutationPending \|\| !textCanSubmit\(authoredText\)\}/s);
  assert.match(app, /className="add-picker-close" disabled=\{pickerPending \|\| shareMutationPending\}/);
  assert.match(app, /type="button" className="secondary" disabled=\{pickerPending \|\| shareMutationPending\}/);
  assert.match(app, /function closeAddPicker[\s\S]*?if \(\(pickerPendingRef\.current \|\| shareMutationPendingRef\.current\) && !force\)/);
  assert.match(app, /if \(event\.key === 'Escape'\)[\s\S]*?closeAddPicker\(\)/);
});
