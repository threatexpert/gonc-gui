export type VPNDetails = {
  revision: number;
  running: boolean;
  connected: boolean;
  ready: boolean;
  everReady: boolean;
  connectionState: string;
  tunnelStatus: string;
  connectionError: string;
  retryRemainingMs: number;
  sessionStartedAt: string;
  lastConnectedAt: string;
  durationSeconds: number;
  inBytes: number;
  outBytes: number;
  inBps: number;
  outBps: number;
  trafficAt: string;
  tunnelOnly: boolean;
  routeState: string;
  configuredRoutes: string[];
  routes: string[];
  bypassIps: string[];
  dnsServers: string[];
};

export type VPNSnapshot = {data: VPNDetails; receivedAt: number};

// Match the file receiver's pre-connection labels. In particular discovering
// and unknown stages use "establishing"; connected never implies VPN readiness.
export function vpnAttemptStage(data: VPNDetails | undefined): 'waitingPeer' | 'negotiatingConnection' | 'establishing' {
  const status = (data?.tunnelStatus || '').trim().toLowerCase();
  if (['wait', 'waiting', 'ready', 'idle'].includes(status)) return 'waitingPeer';
  if (status === 'negotiating') return 'negotiatingConnection';
  return 'establishing';
}

export type VPNPhase = 'idle' | 'connecting' | 'configuring' | 'connected' | 'reconnecting' | 'restoring' | 'error' | 'retryWait' | 'retrying' | 'failed';

// Use one revisioned backend snapshot, not independently arriving tunnel and
// route events. A previous connection's applied routes cannot make a new one ready.
export function vpnPhase(data: VPNDetails | undefined, running: boolean): VPNPhase {
  if (data?.routeState === 'error') return 'error';
  if (!data?.connected && data?.connectionState === 'failed') return 'failed';
  if (!running) return 'idle';
  if (!data?.running) return 'connecting';
  if (!data.connected && data.connectionState === 'retryWait') return data.retryRemainingMs > 0 ? 'retryWait' : 'retrying';
  if (!data.connected && data.connectionState === 'retrying') return 'retrying';
  if (!data.connected) return data.everReady ? 'reconnecting' : 'connecting';
  if (data.ready && (data.tunnelOnly || data.routeState === 'applied')) return 'connected';
  if (data.tunnelOnly) return data.everReady ? 'reconnecting' : 'connecting';
  return data.everReady ? 'restoring' : 'configuring';
}

// The backend supplies the actual scheduled delay. Interpolate using the same
// monotonic clock as snapshot receipt; no guessed delay or wall-clock deadline.
export function vpnRetrySeconds(snapshot: VPNSnapshot | null, now: number): number {
  if (!snapshot?.data.running || snapshot.data.connected || snapshot.data.connectionState !== 'retryWait') return 0;
  const remaining = snapshot.data.retryRemainingMs - Math.max(0, now - snapshot.receivedAt);
  return Number.isFinite(remaining) ? Math.max(0, Math.ceil(remaining / 1000)) : 0;
}

export function acceptVPNDetails(current: VPNSnapshot | null, activeRun: number, eventRun: number, data: VPNDetails, now: number): VPNSnapshot | null {
  if (activeRun <= 0 || eventRun !== activeRun || (current && data.revision <= current.data.revision)) return current;
  return {data, receivedAt: now};
}

export function vpnDuration(snapshot: VPNSnapshot | null, now: number): number | null {
  if (!snapshot?.data.sessionStartedAt) return null;
  const extra = snapshot.data.running ? Math.max(0, now - snapshot.receivedAt) / 1000 : 0;
  return Math.max(0, Math.floor(snapshot.data.durationSeconds + extra));
}

export function formatVPNDuration(seconds: number | null, lang: 'zh' | 'en'): string {
  if (seconds === null || !Number.isFinite(seconds)) return '—';
  const total = Math.max(0, Math.floor(seconds));
  const d = Math.floor(total / 86400), h = Math.floor(total / 3600) % 24;
  const m = Math.floor(total / 60) % 60, s = total % 60;
  if (lang === 'zh') {
    if (d) return `${d} 天 ${h} 小时 ${m} 分`;
    if (h) return `${h} 小时 ${m} 分`;
    if (m) return `${m} 分 ${s} 秒`;
    return `${s} 秒`;
  }
  if (d) return `${d}d ${h}h ${m}m`;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m ${s}s`;
  return `${s}s`;
}

export function vpnRates(data: VPNDetails | undefined, now: number, stopping: boolean): {upload: number; download: number} | null {
  if (stopping || (data && (!data.running || !data.connected))) return {upload: 0, download: 0};
  const time = Date.parse(data?.trafficAt || '');
  if (!data || !Number.isFinite(time) || now - time > 3000 || now < time) return null;
  return {upload: Math.max(0, data.outBps), download: Math.max(0, data.inBps)};
}

export function vpnPaths(data: VPNDetails | undefined, expanded: boolean): {lines: string[]; hasMore: boolean} {
  const routes = data?.tunnelOnly ? [] : data?.routes || [];
  return {lines: expanded ? routes : routes.slice(0, 4), hasMore: routes.length > 4};
}

export function formatVPNConnectedAt(value: string | undefined, lang: 'zh' | 'en'): string {
  const date = new Date(value || '');
  return Number.isFinite(date.getTime()) ? date.toLocaleString(lang === 'zh' ? 'zh-CN' : 'en-GB', {hour12: false}) : '—';
}
