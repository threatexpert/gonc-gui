import test from 'node:test';
import assert from 'node:assert/strict';
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
