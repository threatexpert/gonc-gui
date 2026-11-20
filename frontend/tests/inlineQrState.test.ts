import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {readLastMode, saveLastMode, startupFileMode, lastModeStorageKey} from '../src/pagePreference.js';
import {receivePassphraseForStart} from '../src/inlineQrState.js';

test('empty receive passphrase is generated and displayed before connecting with the same value', async () => {
  for (const input of ['', '   ']) {
    const events: string[] = [];
    const passphrase = await receivePassphraseForStart(input, async () => {
      events.push('generate');
      return ' generated123 ';
    }, value => events.push(`display:${value}`));
    events.push(`connect:${passphrase}`);
    assert.deepEqual(events, ['generate', 'display:generated123', 'connect:generated123']);
  }
});

test('configured receive passphrase is reused; clearing it restores generation', async () => {
  let generations = 0;
  let displayed = 'custom123';
  const generate = async () => { generations++; return 'fresh123'; };
  assert.equal(await receivePassphraseForStart(' custom123 ', generate, value => { displayed = value; }), 'custom123');
  assert.equal(generations, 0);
  assert.equal(displayed, 'custom123');
  assert.equal(await receivePassphraseForStart('', generate, value => { displayed = value; }), 'fresh123');
  assert.equal(generations, 1);
  assert.equal(displayed, 'fresh123');
});

test('receive generation failure never proceeds to connecting or changing the field', async () => {
  for (const generate of [async () => { throw new Error('generation failed'); }, async () => ' ']) {
    let connected = false;
    await assert.rejects(async () => {
      await receivePassphraseForStart('', generate, () => assert.fail('must not change field'));
      connected = true;
    });
    assert.equal(connected, false);
  }
});

test('last page is persisted in local storage with a safe default', () => {
  const entries = new Map<string, string>();
  const storage = {getItem: (key: string) => entries.get(key) ?? null, setItem: (key: string, value: string) => { entries.set(key, value); }};
  assert.equal(readLastMode(storage), 'send');
  for (const mode of ['send', 'receive', 'vpnServer', 'vpnClient'] as const) {
    saveLastMode(storage, mode);
    assert.equal(readLastMode(storage), mode);
  }
  entries.set(lastModeStorageKey, 'invalid');
  assert.equal(readLastMode(storage), 'send');
  assert.equal(readLastMode({getItem: () => { throw new Error('blocked'); }}), 'send');
  assert.doesNotThrow(() => saveLastMode({setItem: () => { throw new Error('full'); }}, 'receive'));
});

test('startup files prefer send, but a late response does not undo manual navigation', () => {
  assert.equal(startupFileMode('vpnClient', false, false), 'vpnClient');
  assert.equal(startupFileMode('vpnClient', true, false), 'send');
  assert.equal(startupFileMode('receive', true, true), 'receive');
  const source = readFileSync('src/App.tsx', 'utf8');
  assert.match(source, /readLastMode\(window.localStorage\)/);
  assert.match(source, /saveLastMode\(window.localStorage, mode\)/);
  const startup = source.slice(source.indexOf('    StartupSharePaths()'), source.indexOf('    StartupSharePaths()') + 650);
  assert.match(startup, /startupFileMode/);
  assert.match(startup, /appendSharePaths\(paths\)/);
  assert.doesNotMatch(startup, /StartTransfer|start\(/);
});

test('startup fit runs once and status bar retains a single maximum-direction speed', () => {
  const source = readFileSync('src/App.tsx', 'utf8');
  assert.match(source, /if \(startupWindowFitted.current\) return/);
  assert.match(source, /startupWindowFitted.current = true/);
  assert.match(source, /FitStartupWindow\(window.screen.availWidth, window.screen.availHeight\)/);
  assert.doesNotMatch(source, /WindowMaximise|WindowFullscreen|WindowToggleMaximise/);
  assert.match(source, /<span className="status-segment">\{formatRate\(transferSpeed\)\}<\/span>/);
  assert.match(source, /mode === 'vpnClient' \? Math.max\(freshSpeed\(vpnClientTraffic\?\.time, vpnClientTraffic\?\.inBps, nowTick\), freshSpeed\(vpnClientTraffic\?\.time, vpnClientTraffic\?\.outBps, nowTick\)\)/);
});
import {createVpnDisconnectCoordinator, vpnDisconnectView, type VpnDisconnectPhase} from '../src/vpnDisconnectState.js';
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

test('disposed desktop passphrase reveals permanently reject new work', () => {
  const timers = fakePassphraseTimers();
  const hidden: string[] = [];
  const coordinator = createPassphraseRevealCoordinator(timers.schedule, timers.cancel);

  assert.equal(coordinator.reveal('send', () => hidden.push('send')), true);
  const queuedBeforeDispose = timers.scheduled.get(1)?.callback;
  coordinator.dispose();
  const scheduledAfterDispose = timers.scheduled.size;
  const cancelledAfterDispose = [...timers.cancelled];

  assert.equal(coordinator.reveal('receive', () => hidden.push('receive')), false);
  queuedBeforeDispose?.();
  assert.equal(timers.scheduled.size, scheduledAfterDispose);
  assert.deepEqual(timers.cancelled, cancelledAfterDispose);
  assert.deepEqual(hidden, []);
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
  assert.match(source, /const \[passwordRevealCoordinator\] = useState\(\(\) => createPassphraseRevealCoordinator\(/);
  assert.doesNotMatch(source, /useRef\(createPassphraseRevealCoordinator/);
  assert.match(source, /passwordRevealCoordinator\.reveal\(targetMode/);
  assert.match(source, /passwordRevealCoordinator\.dispose\(\)/);
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

test('VPN authorization cancellation is localized and startup always releases the button gate', () => {
  const source = readFileSync('src/App.tsx', 'utf8');
  const start = source.indexOf('  async function start()');
  const end = source.indexOf('  async function stop()', start);
  assert.ok(start >= 0 && end > start);
  const startup = source.slice(start, end);
  assert.match(startup, /await StartTransfer\(/);
  assert.match(startup, /finally\s*\{\s*startPendingRef\.current = false;\s*setStartPending\(false\);/);
  assert.match(source, /message\.includes\('VPN administrator authorization canceled'\)/);
  assert.match(source, /return t\.vpnAuthorizationCanceled;/);
});

test('VPN disconnect immediately becomes pending and rejects duplicate clicks and restart', async () => {
  const phases: VpnDisconnectPhase[] = [];
  const coordinator = createVpnDisconnectCoordinator((phase) => phases.push(phase));
  let finish!: () => void;
  let calls = 0;
  const operation = () => {
    calls++;
    return new Promise<void>((resolve) => { finish = resolve; });
  };
  const first = coordinator.run(operation);
  assert.deepEqual(phases, ['pending']);
  assert.equal(coordinator.isPending(), true);
  assert.equal(await coordinator.run(operation), false);
  assert.equal(coordinator.reset(), false);
  assert.equal(calls, 1);
  finish();
  assert.equal(await first, true);
  assert.equal(coordinator.isPending(), false);
  assert.deepEqual(phases, ['pending', 'stopped']);
  assert.equal(coordinator.reset(), true);
  assert.deepEqual(phases, ['pending', 'stopped', 'idle']);
});

test('VPN disconnect failure releases the gate and allows retry', async () => {
  const phases: VpnDisconnectPhase[] = [];
  const coordinator = createVpnDisconnectCoordinator((phase) => phases.push(phase));
  const failure = new Error('stop failed');
  await assert.rejects(coordinator.run(async () => { throw failure; }), failure);
  assert.equal(coordinator.isPending(), false);
  assert.deepEqual(phases, ['pending', 'failed']);
  assert.equal(await coordinator.run(async () => {}), true);
  assert.deepEqual(phases, ['pending', 'failed', 'pending', 'stopped']);
});

test('VPN disconnect intent takes precedence over transient connection reports', () => {
  for (const running of [true, false]) {
    assert.equal(vpnDisconnectView('pending', running), 'pending');
    assert.equal(vpnDisconnectView('stopped', running), 'stopped');
    assert.equal(vpnDisconnectView('idle', running), 'idle');
  }
  assert.equal(vpnDisconnectView('failed', true), 'failed');
  assert.equal(vpnDisconnectView('failed', false), 'stopped');
});

test('VPN UI keeps shutdown feedback visible and suppresses late restoration notifications', () => {
  const source = readFileSync('src/App.tsx', 'utf8');
  assert.match(source, /disabled=\{mode === 'vpnClient' && vpnDisconnectPending\}/);
  assert.match(source, /aria-busy=\{mode === 'vpnClient' && vpnDisconnectPending\}/);
  assert.match(source, /vpnDisconnectPending \? t\.vpnDisconnecting : stopLabel/);
  assert.match(source, /vpnDisconnectDisplay === 'pending' \? t\.vpnDisconnectingSubtitle/);
  assert.match(source, /vpnDisconnectDisplay === 'stopped' \? t\.vpnDisconnectedSubtitle/);
  assert.match(source, /vpnDisconnectDisplay === 'failed' \? t\.vpnDisconnectFailedSubtitle/);
  assert.match(source, /vpnDisconnectPhase !== 'stopped'\) \|\| vpnDisconnectPending/);
  assert.match(source, /await vpnDisconnectCoordinator\.run\(stopCurrentMode\)/);
  assert.match(source, /if \(mode === 'vpnClient' && vpnDisconnectCoordinator\.isPending\(\)\) return;/);
  assert.match(source, /if \(vpnStopRequested\.current \|\| !vpnDetails\?\.running\) return;/);
  assert.match(source, /if \(vpnConnectionPhase === 'connected'\) \{\s*if \(!vpnDetails.tunnelOnly\) notifyVpnStarted\(\);\s*notifyVpnTunnelRestored\(\);/);
  assert.doesNotMatch(source, /handleVpnClientTunnelReport/);
  // Only a new explicit connection should clear the user's stop intent.
  assert.equal(source.match(/vpnStopRequested\.current = false/g)?.length, 1);
});


test('receive timeout classification uses the exact stable code', async () => {
  const {isPeerWaitTimeout} = await import('../src/receiveConnectionState.js');
  assert.equal(isPeerWaitTimeout('error:hello-timeout'), true);
  for (const status of ['error:MQTT subscription timeout', 'error:hello-timeout network', 'error:handshake timeout', 'wait', 'connected']) {
    assert.equal(isPeerWaitTimeout(status), false);
  }
});

test('late failed paths cannot replace a connected receiver; disconnect allows retry', async () => {
  const {acceptReceiveReport} = await import('../src/receiveConnectionState.js');
  const connected = {topic: 'lan-winner', status: 'connected'};
  const stale = {topic: 'public-loser', status: 'error:hello-timeout'};
  assert.equal(acceptReceiveReport(connected, stale), connected);
  assert.equal(acceptReceiveReport(connected, {...stale, status: 'wait'}), connected);
  const disconnected = {topic: connected.topic, status: 'disconnected'};
  assert.equal(acceptReceiveReport(connected, disconnected), disconnected);
  const retry = {topic: 'retry', status: 'wait'};
  assert.equal(acceptReceiveReport(disconnected, retry), retry);
});
