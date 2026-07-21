import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {
  appendUniquePaths,
  dropHintMode,
  nextAddPickerState,
  removePath,
  textCanSubmit,
} from '../src/sendContentState.js';

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
  assert.match(app, /await UpdateSharePaths\(proposed\)/);
  assert.match(app, /setSharePaths\(proposed\)/);
  assert.match(app, /commitSharePaths\(proposed/);
  assert.doesNotMatch(app, /if \(sharePaths\.length === 0\)\s*\{\s*return;\s*\}\s*UpdateSharePaths/);
});

test('picker operations use a synchronous pending guard against rapid duplicate actions', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /pickerPendingRef\.current = true/);
  assert.match(app, /if \(pickerPendingRef\.current \|\| shareMutationPendingRef\.current\)/);
  assert.match(app, /pickerPendingRef\.current = false/);
});

test('rapid share mutations are queued and picker failures stay visible in the dialog', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  assert.match(app, /shareMutationQueueRef\.current/);
  assert.match(app, /const current = sharePathsRef\.current;\s*const proposed = propose\(current\)/);
  assert.match(app, /onFailure\?\.\(message\)/);
  assert.match(app, /releaseAfterSuccess/);
  assert.doesNotMatch(app, /lastShareMutationErrorRef/);
  assert.match(app, /textShareFailed/);
});

test('picker remains usable in short windows', () => {
  const css = readFileSync('src/App.css', 'utf8');
  assert.match(css, /\.add-picker-dialog\s*\{[^}]*max-height:[^}]*overflow-y:\s*auto/s);
});

test('queued actions clear stale errors before execution so failures are not batched away', () => {
  const app = readFileSync('src/App.tsx', 'utf8');
  const queueStart = app.indexOf('async function queueSharePathMutation');
  const clearError = app.indexOf("setError('');", queueStart);
  const operationStart = app.indexOf('const operation =', queueStart);
  assert.ok(queueStart >= 0 && clearError > queueStart && clearError < operationStart);
});
