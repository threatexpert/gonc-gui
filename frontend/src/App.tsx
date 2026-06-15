import {useEffect, useMemo, useRef, useState} from 'react';
import './App.css';
import {
  GeneratePassword,
  RemoteFiles,
  SelectFiles,
  SelectFolder,
  StartHTTPDownload,
  StartTransfer,
  Status,
  StopHTTPDownload,
  StopTransfer
} from '../wailsjs/go/main/App';
import {ClipboardGetText, EventsOff, EventsOn, OnFileDrop, OnFileDropOff} from '../wailsjs/runtime/runtime';

type Mode = 'send' | 'receive';
type Lang = 'zh' | 'en';

type LogEvent = {
  type: string;
  level: string;
  message: string;
  time: string;
  localUrl?: string;
  inBps?: number;
  outBps?: number;
};

type P2PReport = {
  topic: string;
  status: string;
  network: string;
  mode: string;
  peer: string;
  timestamp: number;
  pid: number;
};

type DownloadEvent = {
  type: string;
  level: string;
  message: string;
  time: string;
  totalFiles?: number;
  doneFiles?: number;
  totalBytes?: number;
  doneBytes?: number;
  bytesPerSecond?: number;
};

type RemoteFile = {
  name: string;
  is_dir: boolean;
  mod_time: string;
  size: number;
  path: string;
};

type RemoteList = {
  serverUrl: string;
  files: RemoteFile[];
  fileCount: number;
  dirCount: number;
  totalSize: number;
};

type AppStatus = {
  running: boolean;
  localHTTPUrl: string;
  downloading: boolean;
  defaultSaveDir: string;
};

type VisibleEntry = RemoteFile & {
  synthetic?: boolean;
};

const appVersion = 'v1.0.0';

const text = {
  zh: {
    brand: 'Gonc 传输',
    subtitle: '点对点文件传输',
    send: '发送',
    receive: '接收',
    running: '运行中',
    idle: '空闲',
    sender: '发送方',
    receiver: '接收方',
    sendTitle: '发送文件',
    receiveTitle: '接收文件',
    stop: '停止',
    start: '开始',
    startShare: '开始分享',
    startReceive: '开始接收',
    receiveAll: '接收全部',
    connectedReceivers: '已连接',
    connectingReceivers: '正在建立',
    connections: '连接',
    establishing: '建立中',
    waitingConnection: '等待连接',
    newConnection: '有新连接',
    connectedShort: '已连接',
    connectionFailed: '连接失败',
    disconnected: '已断开',
    transferSpeed: '传输速度',
    receiverUnit: '个接收端',
    connectingYes: '有',
    connectingNo: '无',
    p2pStatus: 'P2P 状态',
    peer: '对端',
    network: '网络',
    speed: '速度',
    passphrase: '口令',
    passPlaceholder: '两端使用相同口令',
    generate: '生成',
    copy: '复制',
    paste: '粘贴',
    copied: '口令已复制',
    sharedList: '文件',
    addFiles: '添加文件',
    addFolder: '添加目录',
    stopBeforeEdit: '请先停止发送任务，再修改分享列表。',
    dropHint: '拖放文件或目录到这里',
    remove: '移除',
    saveDir: '保存目录',
    savePlaceholder: '选择下载文件保存的位置',
    choose: '选择',
    currentDir: '当前目录',
    parent: '上级目录',
    useUDP: '使用 UDP 协议',
    remoteFiles: '远端文件',
    refresh: '刷新',
    stopDownload: '停止下载',
    downloadSelected: '下载选中',
    noSelection: '请先勾选要下载的文件或目录。',
    noList: '尚未读取目录',
    files: '个文件',
    folders: '个目录',
    selected: '已选',
    listHint: '连接建立后会自动读取对方分享的文件目录；可点击目录进入，并勾选需要下载的项。',
    activity: '活动日志',
    diagnostics: '状态和日志',
    clear: '清空',
    logHint: '传输开始后日志会显示在这里。',
    file: '文件',
    dir: '目录',
    goncMissing: '未找到 gonc 可执行文件。请确认发布目录中包含 bundled/gonc/当前平台/gonc(.exe)，或已把 gonc 加入 PATH。',
    senderLockedDrop: '发送任务运行中，不能修改分享列表。',
  },
  en: {
    brand: 'Gonc Transfer',
    subtitle: 'P2P file transfer',
    send: 'Send',
    receive: 'Receive',
    running: 'Running',
    idle: 'Idle',
    sender: 'Sender',
    receiver: 'Receiver',
    sendTitle: 'Send files',
    receiveTitle: 'Receive files',
    stop: 'Stop',
    start: 'Start',
    startShare: 'Start Sharing',
    startReceive: 'Start Receiving',
    receiveAll: 'Receive All',
    connectedReceivers: 'Connected',
    connectingReceivers: 'Establishing',
    connections: 'Connections',
    establishing: 'Establishing',
    waitingConnection: 'Waiting',
    newConnection: 'New Connection',
    connectedShort: 'Connected',
    connectionFailed: 'Failed',
    disconnected: 'Disconnected',
    transferSpeed: 'Transfer Speed',
    receiverUnit: 'receivers',
    connectingYes: 'Yes',
    connectingNo: 'No',
    p2pStatus: 'P2P status',
    peer: 'Peer',
    network: 'Network',
    speed: 'Speed',
    passphrase: 'Passphrase',
    passPlaceholder: 'Same passphrase on both sides',
    generate: 'Generate',
    copy: 'Copy',
    paste: 'Paste',
    copied: 'Passphrase copied',
    sharedList: 'Files',
    addFiles: 'Add Files',
    addFolder: 'Add Folder',
    stopBeforeEdit: 'Stop the sender before changing the shared list.',
    dropHint: 'Drop files or folders here',
    remove: 'Remove',
    saveDir: 'Save directory',
    savePlaceholder: 'Choose where downloaded files will be saved',
    choose: 'Choose',
    currentDir: 'Current directory',
    parent: 'Parent directory',
    useUDP: 'Use UDP protocol',
    remoteFiles: 'Remote Files',
    refresh: 'Refresh',
    stopDownload: 'Stop Download',
    downloadSelected: 'Download Selected',
    noSelection: 'Select files or folders to download first.',
    noList: 'No list loaded',
    files: 'files',
    folders: 'folders',
    selected: 'selected',
    listHint: 'After the connection is ready, shared files from the peer are loaded automatically. Click folders to browse and tick items to download.',
    activity: 'Activity',
    diagnostics: 'Status and Logs',
    clear: 'Clear',
    logHint: 'Logs will appear here after a transfer starts.',
    file: 'FILE',
    dir: 'DIR',
    goncMissing: 'gonc executable was not found. Make sure bundled/gonc/current-platform/gonc(.exe) exists, or put gonc in PATH.',
    senderLockedDrop: 'The sender is running. Stop it before changing the shared list.',
  }
};

function detectLang(): Lang {
  const lang = navigator.language.toLowerCase();
  return lang === 'zh-cn' || lang.startsWith('zh-hans') ? 'zh' : 'en';
}

function App() {
  const [lang] = useState<Lang>(detectLang);
  const t = text[lang];
  const [mode, setMode] = useState<Mode>('send');
  const [password, setPassword] = useState('');
  const [sharePaths, setSharePaths] = useState<string[]>([]);
  const [saveDir, setSaveDir] = useState('');
  const [currentRemotePath, setCurrentRemotePath] = useState('/');
  const [useUDP, setUseUDP] = useState(false);
  const [status, setStatus] = useState<AppStatus>({running: false, localHTTPUrl: '', downloading: false, defaultSaveDir: ''});
  const [logs, setLogs] = useState<LogEvent[]>([]);
  const [error, setError] = useState('');
  const [p2pReport, setP2PReport] = useState<P2PReport | null>(null);
  const [p2pReports, setP2PReports] = useState<Record<string, P2PReport>>({});
  const [remoteList, setRemoteList] = useState<RemoteList | null>(null);
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [downloadProgress, setDownloadProgress] = useState<DownloadEvent | null>(null);
  const [traffic, setTraffic] = useState<LogEvent | null>(null);
  const [passwordVisible, setPasswordVisible] = useState(false);
  const [nowTick, setNowTick] = useState(Date.now());
  const passwordTimer = useRef<number | null>(null);

  const visibleEntries = useMemo(() => shallowEntries(remoteList?.files || [], currentRemotePath), [remoteList, currentRemotePath]);
  const activeSpeed = Math.max(
    freshSpeed(downloadProgress?.time, downloadProgress?.bytesPerSecond, nowTick),
    freshSpeed(traffic?.time, traffic?.inBps, nowTick),
    freshSpeed(traffic?.time, traffic?.outBps, nowTick)
  );
  const canStart = !status.running && password.trim().length > 0 && (mode === 'receive' || sharePaths.length > 0);
  const canDownload = Boolean(mode === 'receive' && status.localHTTPUrl && saveDir && selectedPaths.size > 0 && !status.downloading);
  const canDownloadAll = Boolean(mode === 'receive' && status.localHTTPUrl && saveDir && remoteList && visibleEntries.length > 0 && !status.downloading);
  const primaryLabel = mode === 'send' ? t.startShare : t.startReceive;
  const p2pSessions = useMemo(() => Object.values(p2pReports), [p2pReports]);
  const connectedCount = p2pSessions.filter((report) => report.topic && report.status === 'connected').length;
  const connectingCount = p2pSessions.filter((report) => report.topic && report.status === 'connecting').length;
  const transferSpeed = mode === 'send'
    ? freshSpeed(traffic?.time, traffic?.outBps, nowTick)
    : activeSpeed;
  const sendStatusLabel = connectingCount > 0 ? t.newConnection : (status.running ? t.waitingConnection : t.idle);
  const receiveStatus = receiveConnectionStatus(p2pReport, status.running, Boolean(status.localHTTPUrl), t);
  const statusTone = mode === 'receive' ? receiveStatus.tone : (status.running ? 'running' : 'idle');

  useEffect(() => {
    const timer = window.setInterval(() => setNowTick(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    refreshStatus();
    EventsOn('gonc:event', (event: LogEvent) => {
      if (event.type === 'traffic') {
        setTraffic(event);
        return;
      }
      setLogs((current) => [...current.slice(-399), event]);
      if (event.localUrl) {
        setStatus((current) => ({...current, localHTTPUrl: event.localUrl || current.localHTTPUrl}));
        if (mode === 'receive') {
          window.setTimeout(() => loadRemoteFiles('/', true), 700);
        }
      }
      if (event.type === 'status' || event.type === 'local_http') {
        if (event.message.includes('stopped') || event.message.includes('finished')) {
          setP2PReport((current) => current ? {...current, status: event.message.includes('stopped') ? 'stopped' : 'finished'} : null);
        }
        refreshStatus();
      }
    });
    EventsOn('p2p:report', (report: P2PReport) => {
      setP2PReport(report);
      setP2PReports((current) => ({...current, [p2pSessionKey(report)]: report}));
    });
    EventsOn('download:event', (event: DownloadEvent) => {
      if (event.type === 'progress') {
        setDownloadProgress(event);
      } else {
        setLogs((current) => [...current.slice(-399), event]);
      }
      if (event.type === 'status') {
        refreshStatus();
      }
    });
    OnFileDrop((_x, _y, paths) => {
      if (mode === 'send' && !status.running) {
        appendSharePaths(paths);
      } else if (mode === 'send' && status.running) {
        setError(t.senderLockedDrop);
      }
    }, true);
    return () => {
      EventsOff('gonc:event');
      EventsOff('p2p:report');
      EventsOff('download:event');
      OnFileDropOff();
      if (passwordTimer.current) {
        window.clearTimeout(passwordTimer.current);
      }
    };
  }, [mode, status.running, t.senderLockedDrop]);

  async function refreshStatus() {
    try {
      const next = await Status();
      setStatus({
        running: next.running,
        localHTTPUrl: next.localHTTPUrl,
        downloading: next.downloading,
        defaultSaveDir: next.defaultSaveDir,
      });
      if (!saveDir && next.defaultSaveDir) {
        setSaveDir(next.defaultSaveDir);
      }
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  async function addFiles() {
    setError('');
    appendSharePaths(await SelectFiles() || []);
  }

  async function addFolder() {
    setError('');
    const selected = await SelectFolder(t.addFolder);
    appendSharePaths(selected ? [selected] : []);
  }

  async function chooseSaveDir() {
    setError('');
    const selected = await SelectFolder(t.saveDir);
    if (selected) {
      setSaveDir(selected);
    }
  }

  async function generatePassword() {
    setError('');
    setPassword(await GeneratePassword());
    revealPasswordTemporarily();
  }

  async function copyPassword() {
    if (password) {
      await navigator.clipboard.writeText(password);
      revealPasswordTemporarily();
      appendLog('status', 'info', t.copied);
    }
  }

  async function pastePassword() {
    setError('');
    try {
      const value = await ClipboardGetText();
      setPassword(value.trim());
      revealPasswordTemporarily();
    } catch (err) {
      try {
        const value = await navigator.clipboard.readText();
        setPassword(value.trim());
        revealPasswordTemporarily();
      } catch {
        setError(localizeError(String(err)));
      }
    }
  }

  function revealPasswordTemporarily() {
    setPasswordVisible(true);
    if (passwordTimer.current) {
      window.clearTimeout(passwordTimer.current);
    }
    passwordTimer.current = window.setTimeout(() => setPasswordVisible(false), 5000);
  }

  async function start() {
    setError('');
    setLogs([]);
    setP2PReport(null);
    setP2PReports({});
    setRemoteList(null);
    setSelectedPaths(new Set());
    setDownloadProgress(null);
    setTraffic(null);
    setCurrentRemotePath('/');
    try {
      await StartTransfer({
        mode,
        password,
        sharePaths,
        saveDir,
        goncPath: '',
        downloadSubPath: currentRemotePath,
        useUDP
      });
      await refreshStatus();
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  async function stop() {
    setError('');
    try {
      await StopHTTPDownload();
      await StopTransfer();
      setP2PReport((current) => current ? {...current, status: 'stopped'} : null);
      setP2PReports({});
      setTraffic(null);
      await refreshStatus();
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  async function loadRemoteFiles(path = currentRemotePath, silent = false) {
    if (!silent) {
      setError('');
    }
    try {
      const normalized = normalizeRemotePath(path);
      const list = await RemoteFiles(normalized);
      setCurrentRemotePath(normalized);
      setSelectedPaths(new Set());
      setRemoteList(list);
    } catch (err) {
      if (!silent) {
        setError(localizeError(String(err)));
      }
    }
  }

  async function startDownload() {
    setError('');
    if (selectedPaths.size === 0) {
      setError(t.noSelection);
      return;
    }
    try {
      await StartHTTPDownload(saveDir, currentRemotePath, Array.from(selectedPaths));
      await refreshStatus();
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  async function startDownloadAll() {
    setError('');
    try {
      await StartHTTPDownload(saveDir, currentRemotePath, []);
      await refreshStatus();
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  async function stopDownload() {
    setError('');
    try {
      await StopHTTPDownload();
      await refreshStatus();
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  function appendSharePaths(paths: string[]) {
    setSharePaths((current) => {
      const next = new Set(current);
      paths.filter(Boolean).forEach((path) => next.add(path));
      return Array.from(next);
    });
  }

  function appendLog(type: string, level: string, message: string) {
    setLogs((current) => [
      ...current.slice(-399),
      {type, level, message, time: new Date().toISOString()}
    ]);
  }

  function removeSharePath(path: string) {
    setSharePaths((current) => current.filter((item) => item !== path));
  }

  function toggleSelected(path: string) {
    setSelectedPaths((current) => {
      const next = new Set(current);
      if (next.has(path)) {
        next.delete(path);
      } else {
        next.add(path);
      }
      return next;
    });
  }

  function localizeError(message: string) {
    if (message.includes('gonc executable was not found') || message.includes('selected gonc executable')) {
      return t.goncMissing;
    }
    return message;
  }

  return (
    <main className="shell">
      <section className="workspace">
        <header className="app-header">
          <div className="brand">
          <div className="brand-mark">G</div>
          <div>
            <div className="brand-title">
              <h1>{t.brand}</h1>
              <span>{appVersion}</span>
            </div>
            <p>{t.subtitle}</p>
          </div>
        </div>
        {mode === 'send' && (
          <div className={`status-block ${statusTone}`}>
            <span className={`dot ${statusTone}`} />
            <span>{sendStatusLabel}</span>
            <span className="status-divider" />
            <span>{t.connectedShort} {connectedCount}</span>
            <span>{formatRate(transferSpeed)}</span>
          </div>
        )}
        </header>

        <div className="mode-switch" role="tablist" aria-label="Transfer mode">
          <button className={mode === 'send' ? 'active' : ''} onClick={() => setMode('send')}>{t.send}</button>
          <button className={mode === 'receive' ? 'active' : ''} onClick={() => setMode('receive')}>{t.receive}</button>
        </div>

        <section className="main-pane">
          {error && <div className="alert">{error}</div>}

          <section className="flow-panel">
            {mode === 'send' ? (
              <>
                <div className="field">
                  <div className="field-heading">
                    <label>{t.sharedList}</label>
                    <span>{sharePaths.length} {t.files}</span>
                  </div>
                  <div className="file-actions">
                    <button className="primary-light" disabled={status.running} onClick={addFiles}>{t.addFiles}</button>
                    <button className="secondary" disabled={status.running} onClick={addFolder}>{t.addFolder}</button>
                  </div>
                  <div className="drop-zone">
                    <div className="path-list">
                      {sharePaths.length === 0 ? (
                        <p className="drop-hint">{t.dropHint}</p>
                      ) : sharePaths.map((path) => (
                        <div className="path-row" key={path}>
                          <span>{path}</span>
                          <button disabled={status.running} onClick={() => removeSharePath(path)} aria-label={`${t.remove} ${path}`}>{t.remove}</button>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </>
            ) : (
              <>
                <div className="field">
                  <label>{t.saveDir}</label>
                  <div className="inline">
                    <input value={saveDir} onChange={(event) => setSaveDir(event.target.value)} placeholder={t.savePlaceholder} />
                    <button className="secondary" onClick={chooseSaveDir}>{t.choose}</button>
                  </div>
                </div>
              </>
            )}

            <div className="field">
              <label>{t.passphrase}</label>
              <div className="inline password-line">
                <input
                  type={passwordVisible ? 'text' : 'password'}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder={t.passPlaceholder}
                />
                {mode === 'send' ? (
                  <>
                    <button className="secondary" onClick={generatePassword}>{t.generate}</button>
                    <button className="secondary" disabled={!password} onClick={copyPassword}>{t.copy}</button>
                  </>
                ) : (
                  <button className="secondary" onClick={pastePassword}>{t.paste}</button>
                )}
              </div>
            </div>

            <label className="check">
              <input type="checkbox" checked={useUDP} onChange={(event) => setUseUDP(event.target.checked)} />
              <span>{t.useUDP}</span>
            </label>

            <div className="primary-actions">
              {status.running ? (
                <button className="danger big-action" onClick={stop}>{t.stop}</button>
              ) : (
                <button className="primary big-action" disabled={!canStart} onClick={start}>{primaryLabel}</button>
              )}
            </div>
          </section>

          {mode === 'receive' && (
            <section className="remote-pane">
              <div className="log-header">
                <h3>{t.remoteFiles}</h3>
                <div className="button-row">
                  <span className={`connection-chip ${receiveStatus.tone}`}>
                    <span className={`dot ${receiveStatus.tone}`} />
                    {receiveStatus.label}
                  </span>
                  <button className="secondary" disabled={!status.localHTTPUrl} onClick={() => loadRemoteFiles()}>{t.refresh}</button>
                  {status.downloading ? (
                    <button className="danger" onClick={stopDownload}>{t.stopDownload}</button>
                  ) : (
                    <>
                      <button className="primary" disabled={!canDownloadAll} onClick={startDownloadAll}>{t.receiveAll}</button>
                      <button className="secondary" disabled={!canDownload} onClick={startDownload}>{t.downloadSelected}</button>
                    </>
                  )}
                </div>
              </div>
              <div className="remote-summary">
                <span>{t.currentDir}: {currentRemotePath}</span>
                <span>{remoteList ? `${visibleEntries.filter((item) => !item.is_dir).length} ${t.files}` : t.noList}</span>
                <span>{remoteList ? `${visibleEntries.filter((item) => item.is_dir).length} ${t.folders}` : '-'}</span>
                <span>{selectedPaths.size} {t.selected}</span>
              </div>
              {downloadProgress && (
                <div className="progress">
                  <div>
                    {formatPercent(downloadProgress.doneBytes || 0, downloadProgress.totalBytes || 0)}
                    {' - '}{downloadProgress.doneFiles || 0}/{downloadProgress.totalFiles || 0} {t.files}
                    {' - '}{formatBytes(downloadProgress.doneBytes || 0)} / {formatBytes(downloadProgress.totalBytes || 0)}
                    {' - '}{formatRate(freshSpeed(downloadProgress.time, downloadProgress.bytesPerSecond, nowTick))}
                  </div>
                  <progress max={downloadProgress.totalBytes || 1} value={downloadProgress.doneBytes || 0} />
                </div>
              )}
              <div className="remote-list">
                {!remoteList ? (
                  <p className="muted">{t.listHint}</p>
                ) : (
                  <>
                    {currentRemotePath !== '/' && (
                      <div className="remote-row nav-row">
                        <span>{t.dir}</span>
                        <button className="folder-link" onClick={() => loadRemoteFiles(parentPath(currentRemotePath))}>{t.parent}</button>
                        <em />
                      </div>
                    )}
                    {visibleEntries.map((file) => (
                      <div className="remote-row" key={file.path}>
                        <input
                          type="checkbox"
                          checked={selectedPaths.has(file.path)}
                          onChange={() => toggleSelected(file.path)}
                        />
                        <span>{file.is_dir ? t.dir : t.file}</span>
                        {file.is_dir ? (
                          <button className="folder-link" onClick={() => loadRemoteFiles(file.path)}>{file.name}</button>
                        ) : (
                          <strong>{file.name}</strong>
                        )}
                        <em>{file.is_dir ? '' : formatBytes(file.size)}</em>
                      </div>
                    ))}
                  </>
                )}
              </div>
            </section>
          )}

          <details className="diagnostics">
            <summary>{t.diagnostics}</summary>
            <section className="status-grid">
              <Metric label={t.p2pStatus} value={p2pReport?.status || (status.running ? 'starting' : 'idle')} />
              <Metric label={t.peer} value={p2pReport?.peer || '-'} />
              <Metric label={t.network} value={p2pReport?.network || '-'} />
              <Metric label={t.speed} value={formatRate(activeSpeed)} />
            </section>
            <section className="log-pane">
            <div className="log-header">
              <h3>{t.activity}</h3>
              <button className="ghost" onClick={() => setLogs([])}>{t.clear}</button>
            </div>
            <div className="logs">
              {logs.length === 0 ? (
                <p className="muted">{t.logHint}</p>
              ) : logs.map((log, index) => (
                <div className={`log-line ${log.level}`} key={`${log.time}-${index}`}>
                  <time>{new Date(log.time).toLocaleTimeString()}</time>
                  <span>{log.message}</span>
                </div>
              ))}
            </div>
            </section>
          </details>
        </section>
      </section>
    </main>
  );
}

function Metric({label, value}: { label: string; value: string }) {
  return (
    <div className="metric">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}

function p2pSessionKey(report: P2PReport) {
  return report.topic || report.peer || `${report.pid || 'p2p'}-${report.timestamp || Date.now()}-${report.status}`;
}

function receiveConnectionStatus(report: P2PReport | null, running: boolean, localHTTPReady: boolean, t: typeof text.zh) {
  if (!running) {
    return {label: t.idle, tone: 'idle'};
  }
  const reportStatus = report?.status || '';
  if (reportStatus.startsWith('error:')) {
    return {label: t.connectionFailed, tone: 'error'};
  }
  if (reportStatus === 'disconnected' || reportStatus === 'stopped' || reportStatus === 'finished') {
    return {label: t.disconnected, tone: 'idle'};
  }
  if (reportStatus === 'connecting') {
    return {label: t.establishing, tone: 'connecting'};
  }
  if (reportStatus === 'connected') {
    return {label: t.connectedShort, tone: 'connected'};
  }
  if (reportStatus === 'wait') {
    return {label: t.waitingConnection, tone: 'waiting'};
  }
  if (localHTTPReady && !reportStatus) {
    return {label: t.connectedShort, tone: 'connected'};
  }
  return {label: t.waitingConnection, tone: 'waiting'};
}

function shallowEntries(files: RemoteFile[], currentPath: string): VisibleEntry[] {
  const current = normalizeRemotePath(currentPath);
  const byPath = new Map<string, VisibleEntry>();
  for (const file of files) {
    const filePath = normalizeRemotePath(file.path);
    if (filePath === current) {
      if (current === '/' && !file.is_dir) {
        byPath.set(filePath, file);
      }
      continue;
    }
    const rel = relativeRemotePath(filePath, current);
    if (!rel) {
      continue;
    }
    const first = rel.split('/')[0];
    if (!first) {
      continue;
    }
    if (rel.includes('/')) {
      const dirPath = joinRemotePath(current, first);
      if (!byPath.has(dirPath)) {
        byPath.set(dirPath, {name: first, is_dir: true, mod_time: '', size: 0, path: dirPath, synthetic: true});
      }
      continue;
    }
    byPath.set(filePath, file);
  }
  return Array.from(byPath.values()).sort((a, b) => {
    if (a.is_dir !== b.is_dir) {
      return a.is_dir ? -1 : 1;
    }
    return a.name.localeCompare(b.name);
  });
}

function normalizeRemotePath(value: string) {
  const normalized = `/${value || '/'}`.replace(/\\/g, '/').replace(/\/+/g, '/');
  if (normalized.length > 1) {
    return normalized.replace(/\/$/, '');
  }
  return '/';
}

function relativeRemotePath(filePath: string, currentPath: string) {
  if (currentPath === '/') {
    return filePath.replace(/^\//, '');
  }
  if (!filePath.startsWith(`${currentPath}/`)) {
    return '';
  }
  return filePath.slice(currentPath.length + 1);
}

function joinRemotePath(base: string, name: string) {
  return normalizeRemotePath(`${base}/${name}`);
}

function parentPath(value: string) {
  const normalized = normalizeRemotePath(value);
  if (normalized === '/') {
    return '/';
  }
  const index = normalized.lastIndexOf('/');
  return index <= 0 ? '/' : normalized.slice(0, index);
}

function formatBytes(value: number) {
  if (!value) {
    return '0 B';
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let size = value;
  let index = 0;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${size.toFixed(index === 0 ? 0 : 1)} ${units[index]}`;
}

function formatRate(value: number) {
  return `${formatBytes(value)}/s`;
}

function formatPercent(done: number, total: number) {
  if (!total) {
    return '0.0%';
  }
  return `${Math.min(100, (done / total) * 100).toFixed(1)}%`;
}

function freshSpeed(time: string | undefined, value: number | undefined, now: number) {
  if (!time || !value) {
    return 0;
  }
  const eventTime = Date.parse(time);
  if (!Number.isFinite(eventTime) || now - eventTime > 3000) {
    return 0;
  }
  return value;
}

export default App;

