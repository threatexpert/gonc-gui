import test from 'node:test';
import assert from 'node:assert/strict';
import {
  fileTransferReportBelongsToRun,
  inlineQrShouldMask,
  isCurrentQrGeneration,
  isFileTransferMode,
  latchSuccessfulConnection,
  normalizedQrPassphrase,
  transferStartGate,
} from '../src/inlineQrState.js';

test('limits inline QR to file-transfer modes', () => {
  assert.equal(isFileTransferMode('send'), true);
  assert.equal(isFileTransferMode('receive'), true);
  assert.equal(isFileTransferMode('vpnServer'), false);
  assert.equal(isFileTransferMode('vpnClient'), false);
});

test('connection masking latches for the whole run', () => {
  assert.equal(latchSuccessfulConnection(false, 'connected'), true);
  assert.equal(latchSuccessfulConnection(true, 'disconnected'), true);
  assert.equal(latchSuccessfulConnection(false, 'connecting'), false);
});

test('late asynchronous QR result cannot replace current passphrase', () => {
  assert.equal(isCurrentQrGeneration(3, 4, 'old', 'new'), false);
  assert.equal(isCurrentQrGeneration(4, 4, 'new', 'new'), true);
  assert.equal(normalizedQrPassphrase('  secret  '), 'secret');
});

test('file-transfer reports only belong to their exact run', () => {
  assert.equal(fileTransferReportBelongsToRun(7, 7), true);
  assert.equal(fileTransferReportBelongsToRun(6, 7), false);
  assert.equal(fileTransferReportBelongsToRun(0, 0), false);
});

test('inline QR masking uses only the active file-transfer mode', () => {
  assert.equal(inlineQrShouldMask('send', true, false), true);
  assert.equal(inlineQrShouldMask('receive', false, true), true);
  assert.equal(inlineQrShouldMask('vpnServer', true, true), false);
});

test('transfer start gate synchronously rejects a concurrent start', () => {
  assert.deepEqual(transferStartGate(false), {accepted: true, pending: true});
  assert.deepEqual(transferStartGate(true), {accepted: false, pending: true});
});
