import test from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {acceptVPNDetails, formatVPNConnectedAt, formatVPNDuration, vpnDuration, vpnRates, vpnPaths, vpnPhase, vpnRetrySeconds, vpnAttemptStage, type VPNDetails} from '../src/vpnDetails.js';
import {retainVPNConfigurationFailure, vpnConfigurationFailureSummary, vpnFailureLogIndex} from '../src/vpnDetails.js';
import {initialPeerIPv6State, peerIPv6Label} from '../src/vpnDetails.js';

const sample = (patch: Partial<VPNDetails> = {}): VPNDetails => ({
  revision: 1, running: true, connected: true, ready: true, everReady: true, sessionStartedAt: '2026-11-15T10:00:00Z',
  connectionState: '', connectionError: '', retryRemainingMs: 0, tunnelStatus: '',
  lastConnectedAt: '2026-11-15T10:00:00Z', durationSeconds: 10, inBytes: 123, outBytes: 456,
  inBps: 12, outBps: 45, trafficAt: '2026-11-15T10:00:10Z', tunnelOnly: false,
  routeState: 'applied', configuredRoutes: [], routes: [], bypassIps: [], dnsServers: [], ...patch,
});

test('IPv6 exit wording distinguishes local routing and SOCKS5-only reasons', () => {
  assert.equal(initialPeerIPv6State(true, true), 'tunnel_only');
  assert.equal(initialPeerIPv6State(true, false), 'tunnel_only');
  assert.equal(initialPeerIPv6State(false, false), 'disabled');
  assert.equal(initialPeerIPv6State(false, true), 'waiting');
  const source = readFileSync('src/App.tsx', 'utf8');
  const labels = {
    peerIpv6Disabled: '未检测（本机未配置 IPv6 路由）',
    peerIpv6TunnelOnly: '未检测（仅 SOCKS5 模式）',
    peerIpv6Waiting: '等待隧道连接',
    peerIpv6Checking: '检测中',
    peerIpv6Available: '可用',
    peerIpv6Unavailable: '检测未通过',
  };
  for (const [key, value] of Object.entries(labels)) assert.ok(source.includes(`${key}: '${value}'`));
  for (const [state, expected] of [
    ['disabled', labels.peerIpv6Disabled], ['tunnel_only', labels.peerIpv6TunnelOnly],
    ['waiting', labels.peerIpv6Waiting], ['checking', labels.peerIpv6Checking],
    ['available', labels.peerIpv6Available], ['unavailable', labels.peerIpv6Unavailable],
    ['', '-'], ['new-state', 'new-state'], [' TUNNEL_ONLY ', labels.peerIpv6TunnelOnly],
  ]) assert.equal(peerIPv6Label(state, labels), expected);
  assert.ok(source.includes("peerIpv6Disabled: 'Not checked (no local IPv6 routes configured)'"));
  assert.ok(source.includes("peerIpv6TunnelOnly: 'Not checked (SOCKS5-only mode)'"));
  assert.ok(source.includes("peerIpv6Waiting: 'Waiting for tunnel connection'"));
  assert.match(source, /setVpnClientPeerIPv6\(initialPeerIPv6State\(vpnClientTunnelOnly, hasIPv6Routes\(vpnClientRouteCIDRs\)\)\)/);
});

test('configuration failure persists through cleanup and profile changes until the next connection', () => {
  const failure = retainVPNConfigurationFailure(null, sample({routeState:'error', configurationError:'DNS route failed'}), 7);
  assert.deepEqual(failure, {reason:'DNS route failed', runId:7});
  for (const routeState of ['stopping', 'stopped', 'pending']) {
    assert.strictEqual(retainVPNConfigurationFailure(failure, sample({routeState, running:false}), 7), failure);
  }
  assert.strictEqual(retainVPNConfigurationFailure(failure, undefined, 0), failure);
  assert.strictEqual(retainVPNConfigurationFailure(failure, sample({routeState:'error'}), 7), failure);
  assert.equal(retainVPNConfigurationFailure(null, sample({routeState:'pending'}), 8), null);
  const source = readFileSync('src/App.tsx', 'utf8');
  assert.match(source, /activeVPNRun.current = clientRunId;\s*setVpnConfigurationFailure\(null\)/);
  assert.equal((source.match(/setVpnConfigurationFailure\(null\)/g) || []).length, 1);
  assert.match(source, /retainVPNConfigurationFailure\(current, vpnSnapshot\?\.data, activeVPNRun.current\)/);
});

test('missing failure reasons still show a log entry point and long details are expandable', () => {
  assert.deepEqual(retainVPNConfigurationFailure(null, sample({routeState:'error'}), 4), {reason:'', runId:4});
  assert.equal(vpnConfigurationFailureSummary('', 'zh'), '');
  const source = readFileSync('src/App.tsx', 'utf8');
  assert.match(source, /vpnConfigurationSummary \|\| t.vpnConfigurationFailureUnknown/);
  assert.match(source, /onClick=\{showVPNFailureLog\}.*t.vpnViewActivityLog/);
  assert.match(source, /<details key=\{vpnConfigurationFailureReason\}>/);
  assert.match(source, /<pre>\{vpnConfigurationFailureReason\}<\/pre>/);
  assert.match(source, /setLogOpen\(true\)/);
});

test('configuration failure summaries retain useful targets without dumping diagnostics', () => {
  const reason = 'configure DNS: configure VPN DNS route for 2001:4860:4860::8888: route not installed\nRIB: details';
  assert.equal(vpnConfigurationFailureSummary(reason, 'zh'), '无法配置 DNS 路由：2001:4860:4860::8888');
  assert.equal(vpnConfigurationFailureSummary(reason, 'en'), 'Could not configure DNS route: 2001:4860:4860::8888');
  assert.equal(vpnConfigurationFailureSummary('route 0.0.0.0/1 was not installed on utun4; VPN is not ready', 'zh'), '路由验证失败：0.0.0.0/1（utun4）');
  assert.equal(vpnConfigurationFailureSummary('Permission denied\nlong diagnostic', 'en'), 'Permission denied');
  assert.equal(vpnConfigurationFailureSummary('x'.repeat(200), 'en'), 'x'.repeat(160) + '…');
});

test('failure log location is scoped to the failing VPN run, including retained previous profiles', () => {
  const logs = [
    {mode:'vpnClient', clientRunId:7, message:'System VPN start failed: DNS'},
    {mode:'vpnClient', clientRunId:8, message:'System VPN start failed: newer session'},
    {mode:'send', clientRunId:7, message:'System VPN start failed: wrong mode'},
    {mode:'vpnClient', clientRunId:7, message:'unrelated cleanup log'},
  ];
  assert.equal(vpnFailureLogIndex(logs, 7), 0);
  assert.equal(vpnFailureLogIndex(logs, 8), 1);
  assert.equal(vpnFailureLogIndex([], 7), -1);
  assert.equal(vpnFailureLogIndex(logs, 0), -1);
  assert.equal(vpnFailureLogIndex(logs, 9), -1);
  assert.equal(vpnFailureLogIndex([...logs, {mode:'vpnClient', clientRunId:7, message:'System VPN pause failed: cleanup'}], 7), 4);
});

test('obsolete snapshots cannot replace the current configuration failure', () => {
  const current = {data:sample({revision:10, routeState:'error', configurationError:'current error'}), receivedAt:1};
  assert.strictEqual(acceptVPNDetails(current, 8, 7, sample({revision:11, configurationError:'old session'}), 2), current);
  assert.strictEqual(acceptVPNDetails(current, 8, 8, sample({revision:9, configurationError:'old revision'}), 2), current);
});

test('VPN attempt labels match file reception, including fallback stages', () => {
  for (const tunnelStatus of ['wait', 'waiting', 'ready', 'idle', ' WAIT ']) {
    assert.equal(vpnAttemptStage(sample({tunnelStatus})), 'waitingPeer');
  }
  assert.equal(vpnAttemptStage(sample({tunnelStatus:'negotiating'})), 'negotiatingConnection');
  for (const tunnelStatus of ['connecting', 'discovering', '', 'unknown', 'connected']) {
    assert.equal(vpnAttemptStage(sample({tunnelStatus})), 'establishing');
  }
  assert.equal(vpnAttemptStage(undefined), 'establishing');
  const source = readFileSync('src/App.tsx', 'utf8');
  assert.match(source, /connecting: t\[vpnAttempt\]/);
  assert.match(source, /reconnecting: t\[vpnAttempt\]/);
  assert.match(source, /retrying: t\[vpnAttempt\]/);
  assert.match(source, /vpnAttemptStage\(vpnDetails\)/);
});

test('attempt stages cannot supersede failure, retry delay or VPN readiness', () => {
  for (const tunnelStatus of ['wait', 'connecting', 'negotiating', 'connected']) {
    const data = sample({tunnelStatus, connected:false, ready:false, everReady:false, routeState:'pending'});
    assert.equal(vpnPhase({...data,connectionState:'failed'},true), 'failed');
    assert.equal(vpnPhase({...data,connectionState:'retryWait',retryRemainingMs:10000},true), 'retryWait');
    assert.equal(vpnPhase({...data,connectionState:'retrying'},true), 'retrying');
    assert.equal(vpnPhase({...data,connected:true},true), 'configuring');
    assert.equal(vpnPhase({...data,connected:true,ready:true,routeState:'applied'},true), 'connected');
    assert.equal(vpnPhase({...data,routeState:'error'},true), 'error');
  }
});

test('retry status and countdown follow backend delay and monotonic elapsed time', () => {
  const data = sample({connected:false, ready:false, everReady:false, connectionState:'retryWait', connectionError:'timeout', retryRemainingMs:10000});
  const snapshot = {data, receivedAt:100};
  assert.equal(vpnPhase(data, true), 'retryWait');
  assert.equal(vpnRetrySeconds(snapshot, 100), 10);
  assert.equal(vpnRetrySeconds(snapshot, 1100), 9);
  assert.equal(vpnRetrySeconds(snapshot, 10099), 1);
  assert.equal(vpnRetrySeconds(snapshot, 10100), 0);
  assert.equal(vpnRetrySeconds(snapshot, 20000), 0);
  assert.equal(vpnPhase({...data, retryRemainingMs:0}, true), 'retrying');
  assert.equal(vpnPhase({...data, connectionState:'retrying'}, true), 'retrying');
  assert.equal(vpnPhase({...data, connectionState:'failed'}, true), 'failed');
  assert.equal(vpnPhase({...data, running:false, connectionState:'failed'}, false), 'failed');
  assert.equal(vpnRetrySeconds({data:{...data,running:false},receivedAt:100}, 100), 0);
  assert.equal(vpnRetrySeconds(null, 100), 0);
});

test('retry presentation does not override a healthy tunnel or manual stop', () => {
  assert.equal(vpnPhase(sample({connectionState:'retryWait',retryRemainingMs:10000}), true), 'connected');
  assert.equal(vpnRetrySeconds({data:sample({connectionState:'retryWait',retryRemainingMs:10000}),receivedAt:100},100), 0);
  const source = readFileSync('src/App.tsx', 'utf8');
  assert.match(source, /vpnDisconnectDisplay === 'idle' && \['failed', 'retryWait', 'retrying'\]/);
  assert.match(source, /vpnRetrySeconds\(vpnSnapshot, performance.now\(\)\)/);
  assert.match(source, /vpnDetails\?\.everReady \? t.vpnRetryInterruptedIn : t.vpnRetryFailedIn/);
  assert.match(source, /vpnShowFailure && <p className="vpn-connection-error"/);
  assert.match(source, /localizeError\(vpnDetails.connectionError\)/);
});

test('system VPN is connected only after this connection is ready', () => {
  assert.equal(vpnPhase(undefined, true), 'connecting');
  assert.equal(vpnPhase(sample({connected:false, ready:false, everReady:false, routeState:'pending'}), true), 'connecting');
  for (const routeState of ['pending', 'applying']) {
    assert.equal(vpnPhase(sample({ready:false, everReady:false, routeState}), true), 'configuring');
  }
  assert.equal(vpnPhase(sample(), true), 'connected');
  // No nonempty-DNS requirement: system DNS may legitimately remain unchanged.
  assert.equal(vpnPhase(sample({dnsServers:[]}), true), 'connected');
  assert.equal(vpnPhase(sample({ready:false}), true), 'restoring');
  assert.equal(vpnPhase(sample({routeState:'applying'}), true), 'restoring');
});

test('reconnection cannot inherit old readiness and configuration failure stays visible', () => {
  assert.equal(vpnPhase(sample({connected:false, ready:false, routeState:'paused'}), true), 'reconnecting');
  assert.equal(vpnPhase(sample({ready:false, routeState:'pending'}), true), 'restoring');
  assert.equal(vpnPhase(sample({ready:false, routeState:'applying'}), true), 'restoring');
  assert.equal(vpnPhase(sample({ready:false, routeState:'error'}), true), 'error');
  assert.equal(vpnPhase(sample({running:false, ready:false, routeState:'error'}), false), 'error');
  assert.equal(vpnPhase(sample({running:false, ready:false, routeState:'stopped'}), false), 'idle');
  assert.equal(vpnPhase(undefined, true), 'connecting');
});

test('SOCKS5 mode waits for local service Ready, not system routes', () => {
  const tunnel = sample({tunnelOnly:true, routeState:'tunnelOnly', ready:false, everReady:false});
  assert.equal(vpnPhase(tunnel, true), 'connecting');
  assert.equal(vpnPhase({...tunnel, ready:true, everReady:true}, true), 'connected');
  assert.equal(vpnPhase({...tunnel, everReady:true}, true), 'reconnecting');
  assert.equal(vpnPhase({...tunnel, ready:true, everReady:true, connected:false}, true), 'reconnecting');
});

test('all primary VPN indicators and notifications use the readiness-aware status', () => {
  const source = readFileSync('src/App.tsx', 'utf8');
  assert.match(source, /vpnPhase\(vpnDetails, vpnClientRunning\)/);
  assert.doesNotMatch(source, /receiveConnectionStatus\(vpnClientP2PReport/);
  assert.match(source, /vpnClientStatus.tone === 'connected' \? 'shieldCheck' : 'shield'/);
  assert.match(source, /vpnClient: vpnClientRunning \? vpnClientStatus.tone : null/);
  assert.match(source, /vpnConnectionPhase === 'connected'/);
  assert.doesNotMatch(source, /event.message.includes\('System VPN started'\)/);
});

test('duration switches units and preserves minutes after many days', () => {
  for (const [seconds, zh, en] of [
    [0, '0 秒', '0s'], [59, '59 秒', '59s'], [60, '1 分 0 秒', '1m 0s'],
    [3599, '59 分 59 秒', '59m 59s'], [3600, '1 小时 0 分', '1h 0m'],
    [86400, '1 天 0 小时 0 分', '1d 0h 0m'],
    [123*86400+5*3600+7*60+59, '123 天 5 小时 7 分', '123d 5h 7m'],
  ] as const) {
    assert.equal(formatVPNDuration(seconds, 'zh'), zh);
    assert.equal(formatVPNDuration(seconds, 'en'), en);
  }
  assert.equal(formatVPNDuration(null, 'zh'), '—');
});

test('duration uses backend baseline and elapsed clock time, not UI tick counts', () => {
  const snapshot = {data: sample(), receivedAt: 100};
  assert.equal(vpnDuration(snapshot, 100), 10);
  assert.equal(vpnDuration(snapshot, 3600100), 3610);
  assert.equal(vpnDuration({...snapshot, data: sample({connected:false})}, 5100), 15);
  assert.equal(vpnDuration({...snapshot, data: sample({running:false})}, 5100), 10);
  assert.equal(vpnDuration({...snapshot, data: sample({sessionStartedAt:''})}, 5100), null);
});

test('old sessions and out-of-order snapshots cannot overwrite current data', () => {
  const snapshot = acceptVPNDetails(null, 12, 12, sample({revision:3}), 100)!;
  assert.equal(acceptVPNDetails(snapshot, 12, 11, sample({revision:100}), 200), snapshot);
  assert.equal(acceptVPNDetails(snapshot, 12, 12, sample({revision:2}), 200), snapshot);
  assert.equal(acceptVPNDetails(snapshot, 12, 12, sample({revision:3}), 200), snapshot);
  assert.equal(acceptVPNDetails(null, 0, 0, sample(), 100), null);
  assert.equal(acceptVPNDetails(snapshot, 12, 12, sample({revision:4}), 200)?.data.revision, 4);
});

test('dual rates preserve directions and distinguish missing samples from zero', () => {
  const now = Date.parse(sample().trafficAt);
  assert.deepEqual(vpnRates(sample(), now, false), {upload:45, download:12});
  assert.equal(vpnRates(sample(), now+3001, false), null);
  assert.equal(vpnRates(undefined, now, false), null);
  assert.deepEqual(vpnRates(sample({inBps:0,outBps:0}),now,false), {upload:0,download:0});
  assert.deepEqual(vpnRates(sample(),now,true), {upload:0,download:0});
  assert.deepEqual(vpnRates(sample({connected:false}),now,false), {upload:0,download:0});
});

test('paths preserve raw entries and order, collapse to four lines and expand on demand', () => {
  const routes = ['10.0.0.0/8', '192.168.0.0/16', 'fc00::/7', '0.0.0.0/1', '128.0.0.0/1'];
  assert.deepEqual(vpnPaths(sample({routes}), false), {lines: routes.slice(0, 4), hasMore: true});
  assert.deepEqual(vpnPaths(sample({routes}), true), {lines: routes, hasMore: true});
  assert.deepEqual(vpnPaths(sample({routes: routes.slice(0, 3)}), false), {lines: routes.slice(0, 3), hasMore: false});
  assert.deepEqual(vpnPaths(sample({routes: routes.slice(0, 4)}), false), {lines: routes.slice(0, 4), hasMore: false});
  assert.deepEqual(vpnPaths(sample({routes, tunnelOnly:true}), true), {lines: [], hasMore: false});
  assert.deepEqual(vpnPaths(undefined, false), {lines: [], hasMore: false});
  assert.deepEqual(vpnPaths(sample({routes:['custom', 'custom']}), false), {lines:['custom', 'custom'], hasMore:false});
});

test('last connection time includes date and time, missing value stays empty', () => {
  assert.equal(formatVPNConnectedAt('', 'zh'), '—');
  assert.match(formatVPNConnectedAt('2026-11-15T10:00:00Z','en'), /2026/);
  assert.match(formatVPNConnectedAt('2026-11-15T10:00:00Z','zh'), /\d+:\d+:\d+/);
});

test('VPN card and activity drawer are wired to per-session backend details', () => {
  const source = readFileSync('src/App.tsx','utf8');
  for (const key of ['vpnUpload','vpnDownload','vpnUploaded','vpnDownloaded','vpnDuration','vpnLatestConnection']) {
    assert.ok(source.includes(`label={t.${key}}`), key);
  }
  assert.match(source, /setVpnSnapshot\(null\)/);
  assert.match(source, /activeVPNRun\.current = clientRunId/);
  assert.match(source, /acceptVPNDetails\(current, activeVPNRun\.current/);
  assert.match(source, /event\.mode === 'vpnClient' && !fileTransferReportBelongsToRun/);
  assert.match(source, /aria-expanded={vpnPathsExpanded}/);
  assert.match(source, /label="DNS" value={vpnDetails\?\.routeState === 'applied' \? vpnDetails.dnsServers/);
  assert.match(source, /t.vpnSystemDNS : '—'/);
  assert.doesNotMatch(source, /vpnTrafficHint|vpnRouteSummary|vpnRouteScope|vpnRouteCaution/);
  assert.match(source, /label={t.vpnUpload} value={vpnUploadText}/);
  assert.match(source, /label={t.vpnDownload} value={vpnDownloadText}/);
  assert.match(source, /vpnTunnelOnlyPath: '仅 SOCKS5 隧道'/);
  assert.ok(source.includes("`${t.vpnTunnelOnlyPath} · ${vpnClientSocks5Endpoint || '—'}`"));
  const cardStart = source.indexOf('{vpnClientRunning && (', source.indexOf('card vpn-hero'));
  assert.ok(cardStart >= 0, 'details only render while the VPN session is running or stopping');
  assert.doesNotMatch(source, /vpnClientRunning \|\| vpnDetails/);
  const card = source.slice(cardStart);
  assert.ok(card.indexOf('label={t.peer}') < card.indexOf('label={t.vpnUpload}'));
  assert.ok(card.indexOf('label={t.network}') < card.indexOf('label={t.vpnUpload}'));
  assert.ok(card.indexOf('label={t.connectionRoute}') < card.indexOf('label={t.vpnUpload}'));
  const profileStart = source.indexOf('  function applyVpnProfile(');
  const profileEnd = source.indexOf('  function persistVpnProfiles(', profileStart);
  const applyProfile = source.slice(profileStart, profileEnd);
  assert.match(applyProfile, /!vpnClientRunning && !startPendingRef\.current/);
  assert.match(applyProfile, /activeVPNRun\.current = 0/);
  assert.match(applyProfile, /setVpnSnapshot\(null\)/);
});

test('profile subtitles remain static while connection status belongs to the status card', () => {
  const source = readFileSync('src/App.tsx', 'utf8');
  const strip = source.slice(source.indexOf('<ProfileStrip'), source.indexOf('selected={selectedVpnProfile}', source.indexOf('<ProfileStrip')));
  assert.doesNotMatch(strip, /vpnClientStatus|vpnClientRunning/);
  assert.match(strip, /profile.useUdp/);
  assert.match(strip, /profile.tunnelOnly/);
  assert.match(source, /vpnPaths: '路由'/);
  assert.match(source, /vpnPaths: 'Routes'/);
  assert.match(source, /<p>\{vpnClientStatus.label\}<\/p>/);
});

test('IPv6 checkbox is removed and legacy import flags cannot override routes', () => {
  const source = readFileSync('src/App.tsx', 'utf8');
  assert.doesNotMatch(source, /setVpnClientEnableIPv6|label={t.routeIpv6}/);
  assert.match(source, /enableIpv6: hasIPv6Routes\(vpnClientRouteCIDRs\)/);
  assert.match(source, /routeIpv6: hasIPv6Routes\(String\(profile.routeCidrs/);
  assert.match(source, /peerIpv6Unavailable: '检测未通过'/);
  assert.match(source, /peerIpv6Unavailable: 'Check failed'/);
  assert.doesNotMatch(source, /保持接管，自动复测|routes retained, retrying/);
});

test('VPN status groups connection, traffic and network fields without mixing them', () => {
  const source = readFileSync('src/App.tsx', 'utf8');
  const group = (label: string) => {
    const start = source.indexOf(`<section className="vpn-detail-group" aria-label={t.${label}}>`);
    assert.ok(start >= 0, label);
    const section = source.slice(start, source.indexOf('</section>', start));
    assert.doesNotMatch(section, /<h[1-6]\b/, 'groups have no visible classification titles');
    return section;
  };
  const connection = group('vpnConnectionInfo');
  for (const key of ['peer', 'network', 'connectionRoute', 'vpnDuration']) {
    assert.ok(connection.includes(`label={t.${key}}`), key);
  }
  assert.doesNotMatch(connection, /label="DNS"|label={t.vpnUpload}|label={t.peerIpv6}/);
  const traffic = group('vpnTrafficStats');
  for (const key of ['vpnUpload', 'vpnDownload', 'vpnUploaded', 'vpnDownloaded']) {
    assert.ok(traffic.includes(`label={t.${key}}`), key);
  }
  assert.doesNotMatch(traffic, /label={t.vpnDuration}|label="DNS"/);
  const network = group('vpnNetworkConfig');
  assert.ok(network.indexOf('label="DNS"') < network.indexOf('label={t.peerIpv6}'));
  assert.ok(network.indexOf('label={t.peerIpv6}') < network.indexOf('className="vpn-paths"'));
  assert.match(network, /peerIpv6Label\(vpnClientPeerIPv6, t\)/);
  assert.match(network, /aria-expanded={vpnPathsExpanded}/);
  assert.match(source, /vpnDetails\?\.tunnelOnly \?\? vpnClientTunnelOnly/);
  const tunnel = network.slice(network.indexOf('{vpnStatusTunnelOnly ? ('), network.indexOf(') : ('));
  assert.match(tunnel, /vpnTunnelOnlyPath/);
  assert.match(tunnel, /vpnClientSocks5Endpoint/);
  assert.doesNotMatch(tunnel, /label="DNS"|peerIpv6|vpn-paths/);
});
