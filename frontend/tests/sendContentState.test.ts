import test from 'node:test';
import assert from 'node:assert/strict';
import {
  appendUniquePaths,
  dropHintMode,
  removePath,
  textCanSubmit,
  type AddPickerState,
} from '../src/sendContentState.js';

test('paths can be cleared while preserving deterministic append order', () => {
  assert.deepEqual(appendUniquePaths(['a'], ['b', 'a', 'c']), ['a', 'b', 'c']);
  assert.deepEqual(removePath(['a'], 'a'), []);
});

test('authored text rejects only an empty string', () => {
  assert.equal(textCanSubmit(''), false);
  assert.equal(textCanSubmit('   '), true);
});

test('picker state supports closed, chooser, and authored text transitions', () => {
  const transitions: AddPickerState[] = ['closed', 'choose', 'text', 'closed'];
  assert.deepEqual(transitions, ['closed', 'choose', 'text', 'closed']);
});

test('drop hint is empty without paths and compact with paths', () => {
  assert.equal(dropHintMode([]), 'empty');
  assert.equal(dropHintMode(['a']), 'compact');
});
