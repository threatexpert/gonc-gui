import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {
  fileTransferReportBelongsToRun,
  inlineQrShouldMask,
  isCurrentQrGeneration,
  isFileTransferMode,
  latchSuccessfulConnection,
  maskAfterPassphraseUpdate,
  normalizedQrPassphrase,
  qrGenerationRequest,
  isCurrentQrRequest,
  transferStartGate,
} from '../src/inlineQrState.js';
import {
  createPassphraseRevealCoordinator,
  hiddenPassphraseVisibility,
  initialPassphraseRevealVersions,
  isCurrentPassphraseReveal,
  nextPassphraseRevealVersions,
  withPassphraseVisibility,
} from '../src/passphraseVisibility.js';

function fakePassphraseTimers() {
  let nextTimer = 1;
  const scheduled = new Map<number, {callback: () => void; delay: number}>();
  const cancelled: number[] = [];
  return {
    scheduled,
    cancelled,
    schedule(callback: () => void, delay: number) {
      const timer = nextTimer++;
      scheduled.set(timer, {callback, delay});
      return timer;
    },
    cancel(timer: number) {
      cancelled.push(timer);
    },
  };
}

test('all desktop passphrase modes begin hidden and reveal independently', () => {
  const hidden = hiddenPassphraseVisibility();
  assert.deepEqual(hidden, {
    send: false,
    receive: false,
    vpnServer: false,
    vpnClient: false,
  });

  const sendVisible = withPassphraseVisibility(hidden, 'send', true);
  assert.deepEqual(sendVisible, {
    send: true,
    receive: false,
    vpnServer: false,
    vpnClient: false,
  });

  const receiveVisible = withPassphraseVisibility(sendVisible, 'receive', true);
  assert.equal(receiveVisible.send, true);
  assert.equal(receiveVisible.receive, true);
  assert.equal(withPassphraseVisibility(receiveVisible, 'send', false).receive, true);
});

test('each desktop passphrase mode owns its reveal generation', () => {
  const initial = initialPassphraseRevealVersions();
  const firstSend = nextPassphraseRevealVersions(initial, 'send');
  const receive = nextPassphraseRevealVersions(firstSend, 'receive');
  const secondSend = nextPassphraseRevealVersions(receive, 'send');

  assert.equal(isCurrentPassphraseReveal(secondSend, 'send', firstSend.send), false);
  assert.equal(isCurrentPassphraseReveal(secondSend, 'send', secondSend.send), true);
  assert.equal(isCurrentPassphraseReveal(secondSend, 'receive', receive.receive), true);
  assert.equal(secondSend.vpnServer, 0);
  assert.equal(secondSend.vpnClient, 0);
});

test('desktop passphrase reveal schedules hiding after exactly 5000ms', () => {
  const timers = fakePassphraseTimers();
  const hidden: string[] = [];
  const coordinator = createPassphraseRevealCoordinator(timers.schedule, timers.cancel);

  coordinator.reveal('send', () => hidden.push('send'));

  assert.equal(timers.scheduled.get(1)?.delay, 5000);
  timers.scheduled.get(1)?.callback();
  assert.deepEqual(hidden, ['send']);
});

test('desktop passphrase modes keep independent hide timers', () => {
  const timers = fakePassphraseTimers();
  const coordinator = createPassphraseRevealCoordinator(timers.schedule, timers.cancel);

  coordinator.reveal('send', () => undefined);
  coordinator.reveal('receive', () => undefined);

  assert.deepEqual(timers.cancelled, []);
  assert.equal(timers.scheduled.get(1)?.delay, 5000);
  assert.equal(timers.scheduled.get(2)?.delay, 5000);
});

test('repeating a reveal cancels and reschedules only that mode', () => {
  const timers = fakePassphraseTimers();
  const coordinator = createPassphraseRevealCoordinator(timers.schedule, timers.cancel);

  coordinator.reveal('send', () => undefined);
  coordinator.reveal('send', () => undefined);

  assert.deepEqual(timers.cancelled, [1]);
  assert.equal(timers.scheduled.get(2)?.delay, 5000);
});

test('a cancelled stale reveal cannot hide or discard the newer timer', () => {
  const timers = fakePassphraseTimers();
  const hidden: string[] = [];
  const coordinator = createPassphraseRevealCoordinator(timers.schedule, timers.cancel);

  coordinator.reveal('send', () => hidden.push('old'));
  coordinator.reveal('send', () => hidden.push('new'));
  timers.scheduled.get(1)?.callback();

  assert.deepEqual(hidden, []);
  coordinator.dispose();
  assert.deepEqual(timers.cancelled, [1, 2]);
});

test('disposing desktop passphrase reveals clears every pending timer', () => {
  const timers = fakePassphraseTimers();
  const coordinator = createPassphraseRevealCoordinator(timers.schedule, timers.cancel);

  coordinator.reveal('send', () => undefined);
  coordinator.reveal('receive', () => undefined);
  coordinator.reveal('vpnServer', () => undefined);
  coordinator.reveal('vpnClient', () => undefined);
  coordinator.dispose();

  assert.deepEqual(timers.cancelled, [1, 2, 3, 4]);
});

test('desktop mode subscription cleanup does not own passphrase timers', () => {
  const source = readFileSync('src/App.tsx', 'utf8');
  const refreshIndex = source.indexOf('    refreshStatus();');
  const effectStart = source.lastIndexOf('useEffect(() => {', refreshIndex);
  const effectEnd = source.indexOf('  }, [mode]);', effectStart);
  assert.ok(refreshIndex >= 0 && effectStart >= 0 && effectEnd > effectStart);
  const modeEffect = source.slice(effectStart, effectEnd);
  assert.doesNotMatch(modeEffect, /passwordRevealCoordinator|passwordTimers|passwordTimer|clearTimeout/);
  assert.match(source, /type=\{passwordVisibility\[mode\] \? 'text' : 'password'\}/);
  assert.match(source, /createPassphraseRevealCoordinator/);
  assert.match(source, /passwordRevealCoordinator\.current\.reveal\(targetMode/);
  assert.match(source, /passwordRevealCoordinator\.current\.dispose\(\)/);
  assert.match(source, /function revealPasswordTemporarily\(targetMode: Mode\)/);
  assert.match(source, /scanPasswordMode\.current = mode/);

  const decodeStart = source.indexOf('async function decodeScanRegion');
  const decodeEnd = source.indexOf('  function decodeWholeScan', decodeStart);
  assert.ok(decodeStart >= 0 && decodeEnd > decodeStart);
  const decodeSource = source.slice(decodeStart, decodeEnd);
  const targetCapture = decodeSource.indexOf('const targetMode = scanPasswordMode.current;');
  const firstDecodeAwait = decodeSource.indexOf('await decodeQrFromImageRegion');
  assert.ok(targetCapture >= 0 && firstDecodeAwait >= 0 && targetCapture < firstDecodeAwait);
});

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

test('stopped transfer mask clears only for a different normalized passphrase', () => {
  assert.equal(maskAfterPassphraseUpdate(true, false, 'old', 'old'), true);
  assert.equal(maskAfterPassphraseUpdate(true, false, ' old ', 'old'), true);
  assert.equal(maskAfterPassphraseUpdate(true, false, 'old', 'new'), false);
});

test('running transfer passphrase update cannot clear its mask', () => {
  assert.equal(maskAfterPassphraseUpdate(true, true, 'old', 'new'), true);
  assert.equal(maskAfterPassphraseUpdate(false, false, 'old', 'new'), false);
});

test('late asynchronous QR result cannot replace current passphrase', () => {
  assert.equal(isCurrentQrGeneration(3, 4, 'old', 'new'), false);
  assert.equal(isCurrentQrGeneration(4, 4, 'new', 'new'), true);
  assert.equal(normalizedQrPassphrase('  secret  '), 'secret');
});

test('same generation token rejects a result for the previous rendered passphrase', () => {
  const oldRequest = qrGenerationRequest(4, 'old');
  assert.equal(isCurrentQrRequest(oldRequest, 4, 'new'), false);
  assert.equal(isCurrentQrRequest(oldRequest, 4, 'old'), true);
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
