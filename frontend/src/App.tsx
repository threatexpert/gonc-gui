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
import {BrowserOpenURL, EventsOff, EventsOn, OnFileDrop, OnFileDropOff} from '../wailsjs/runtime/runtime';

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
  status: string;
  network: string;
  mode: string;
  peer: string;
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

const text = {
  zh: {
    brand: 'Gonc \u4f20\u8f93',
    subtitle: '\u70b9\u5bf9\u70b9\u6587\u4ef6\u4f20\u8f93',
    send: '\u53d1\u9001',
    receive: '\u63a5\u6536',
    running: '\u8fd0\u884c\u4e2d',
    idle: '\u7a7a\u95f2',
    sender: '\u53d1\u9001\u65b9',
    receiver: '\u63a5\u6536\u65b9',
    sendTitle: '\u901a\u8fc7 gonc P2P \u5206\u4eab\u6587\u4ef6',
    receiveTitle: '\u6d4f\u89c8\u5bf9\u7aef\u6587\u4ef6\u5e76\u6309\u9700\u4e0b\u8f7d',
    stop: '\u505c\u6b62',
    start: '\u5f00\u59cb',
    p2pStatus: 'P2P \u72b6\u6001',
    peer: '\u5bf9\u7aef',
    network: '\u7f51\u7edc',
    speed: '\u901f\u5ea6',
    passphrase: '\u53e3\u4ee4',
    passPlaceholder: '\u4e24\u7aef\u4f7f\u7528\u76f8\u540c\u53e3\u4ee4',
    generate: '\u751f\u6210',
    copy: '\u590d\u5236',
    copied: '\u53e3\u4ee4\u5df2\u590d\u5236',
    sharedList: '\u8981\u53d1\u9001\u7684\u6587\u4ef6\u548c\u76ee\u5f55',
    addFiles: '\u6dfb\u52a0\u6587\u4ef6',
    addFolder: '\u6dfb\u52a0\u76ee\u5f55',
    stopBeforeEdit: '\u8bf7\u5148\u505c\u6b62\u53d1\u9001\u4efb\u52a1\uff0c\u518d\u4fee\u6539\u5206\u4eab\u5217\u8868\u3002',
    dropHint: '\u53ef\u5c06\u6587\u4ef6\u6216\u76ee\u5f55\u62d6\u653e\u5230\u8fd9\u91cc\uff0c\u4e5f\u53ef\u4ee5\u7528\u4e0a\u9762\u7684\u6309\u94ae\u6dfb\u52a0\u3002',
    remove: '\u79fb\u9664',
    saveDir: '\u4fdd\u5b58\u76ee\u5f55',
    savePlaceholder: '\u9009\u62e9\u4e0b\u8f7d\u6587\u4ef6\u4fdd\u5b58\u7684\u4f4d\u7f6e',
    choose: '\u9009\u62e9',
    currentDir: '\u5f53\u524d\u76ee\u5f55',
    parent: '\u4e0a\u7ea7\u76ee\u5f55',
    localHTTP: '\u672c\u5730 HTTP',
    waitingHTTP: '\u7b49\u5f85 -httplocal \u7aef\u53e3\u5efa\u7acb',
    useUDP: '\u4f7f\u7528 UDP \u534f\u8bae',
    remoteFiles: '\u8fdc\u7aef\u6587\u4ef6',
    refresh: '\u5237\u65b0',
    stopDownload: '\u505c\u6b62\u4e0b\u8f7d',
    downloadSelected: '\u4e0b\u8f7d\u9009\u4e2d',
    noSelection: '\u8bf7\u5148\u52fe\u9009\u8981\u4e0b\u8f7d\u7684\u6587\u4ef6\u6216\u76ee\u5f55\u3002',
    noList: '\u5c1a\u672a\u8bfb\u53d6\u76ee\u5f55',
    files: '\u4e2a\u6587\u4ef6',
    folders: '\u4e2a\u76ee\u5f55',
    selected: '\u5df2\u9009',
    listHint: '\u672c\u5730 HTTP \u5efa\u7acb\u540e\u4f1a\u81ea\u52a8\u8bfb\u53d6\u5f53\u524d\u76ee\u5f55\uff1b\u53ef\u70b9\u51fb\u76ee\u5f55\u8fdb\u5165\uff0c\u5e76\u52fe\u9009\u9700\u8981\u4e0b\u8f7d\u7684\u9879\u3002',
    activity: '\u6d3b\u52a8\u65e5\u5fd7',
    clear: '\u6e05\u7a7a',
    logHint: '\u4f20\u8f93\u5f00\u59cb\u540e\u65e5\u5fd7\u4f1a\u663e\u793a\u5728\u8fd9\u91cc\u3002',
    file: '\u6587\u4ef6',
    dir: '\u76ee\u5f55',
    goncMissing: '\u672a\u627e\u5230 gonc \u53ef\u6267\u884c\u6587\u4ef6\u3002\u8bf7\u786e\u8ba4\u53d1\u5e03\u76ee\u5f55\u4e2d\u5305\u542b bundled/gonc/\u5f53\u524d\u5e73\u53f0/gonc(.exe)\uff0c\u6216\u5df2\u628a gonc \u52a0\u5165 PATH\u3002',
    senderLockedDrop: '\u53d1\u9001\u4efb\u52a1\u8fd0\u884c\u4e2d\uff0c\u4e0d\u80fd\u4fee\u6539\u5206\u4eab\u5217\u8868\u3002',
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
    sendTitle: 'Share files through gonc P2P',
    receiveTitle: 'Browse peer files and download selected items',
    stop: 'Stop',
    start: 'Start',
    p2pStatus: 'P2P status',
    peer: 'Peer',
    network: 'Network',
    speed: 'Speed',
    passphrase: 'Passphrase',
    passPlaceholder: 'Same passphrase on both sides',
    generate: 'Generate',
    copy: 'Copy',
    copied: 'Passphrase copied',
    sharedList: 'Files and folders to send',
    addFiles: 'Add Files',
    addFolder: 'Add Folder',
    stopBeforeEdit: 'Stop the sender before changing the shared list.',
    dropHint: 'Drag and drop files or folders into this list, or add them with the buttons above.',
    remove: 'Remove',
    saveDir: 'Save directory',
    savePlaceholder: 'Choose where downloaded files will be saved',
    choose: 'Choose',
    currentDir: 'Current directory',
    parent: 'Parent directory',
    localHTTP: 'Local HTTP',
    waitingHTTP: 'Waiting for -httplocal endpoint',
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
    listHint: 'After the local HTTP endpoint appears, this directory is loaded automatically. Click folders to browse and tick items to download.',
    activity: 'Activity',
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
    EventsOn('p2p:report', (report: P2PReport) => setP2PReport(report));
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
        <aside className="rail">
          <div className="brand">
            <div className="brand-mark">G</div>
            <div>
              <h1>{t.brand}</h1>
              <p>{t.subtitle}</p>
            </div>
          </div>

          <div className="mode-switch" role="tablist" aria-label="Transfer mode">
            <button className={mode === 'send' ? 'active' : ''} onClick={() => setMode('send')}>{t.send}</button>
            <button className={mode === 'receive' ? 'active' : ''} onClick={() => setMode('receive')}>{t.receive}</button>
          </div>

          <div className="status-block">
            <span className={status.running ? 'dot running' : 'dot'} />
            <span>{status.running ? t.running : t.idle}</span>
          </div>
        </aside>

        <section className="main-pane">
          <header className="topbar">
            <div>
              <p className="eyebrow">{mode === 'send' ? t.sender : t.receiver}</p>
              <h2>{mode === 'send' ? t.sendTitle : t.receiveTitle}</h2>
            </div>
            <div className="actions">
              {status.running ? (
                <button className="danger" onClick={stop}>{t.stop}</button>
              ) : (
                <button className="primary" disabled={!canStart} onClick={start}>{t.start}</button>
              )}
            </div>
          </header>

          {error && <div className="alert">{error}</div>}

          <section className="status-grid">
            <Metric label={t.p2pStatus} value={p2pReport?.status || (status.running ? 'starting' : 'idle')} />
            <Metric label={t.peer} value={p2pReport?.peer || '-'} />
            <Metric label={t.network} value={p2pReport?.network || '-'} />
            <Metric label={t.speed} value={formatRate(activeSpeed)} />
          </section>

          <section className="form-grid">
            <div className="field wide">
              <label>{t.passphrase}</label>
              <div className="inline">
                <input
                  type={passwordVisible ? 'text' : 'password'}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder={t.passPlaceholder}
                />
                <button className="secondary" onClick={generatePassword}>{t.generate}</button>
                <button className="secondary" disabled={!password} onClick={copyPassword}>{t.copy}</button>
              </div>
            </div>

            {mode === 'send' ? (
              <div className="field wide">
                <label>{t.sharedList}</label>
                <div className="button-row">
                  <button className="secondary" disabled={status.running} onClick={addFiles}>{t.addFiles}</button>
                  <button className="secondary" disabled={status.running} onClick={addFolder}>{t.addFolder}</button>
                </div>
                {status.running && <p className="muted">{t.stopBeforeEdit}</p>}
                <div className="drop-zone">
                  <div className="path-list">
                    {sharePaths.length === 0 ? (
                      <p className="muted">{t.dropHint}</p>
                    ) : sharePaths.map((path) => (
                      <div className="path-row" key={path}>
                        <span>{path}</span>
                        <button disabled={status.running} onClick={() => removeSharePath(path)} aria-label={`${t.remove} ${path}`}>{t.remove}</button>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            ) : (
              <>
                <div className="field wide">
                  <label>{t.saveDir}</label>
                  <div className="inline">
                    <input value={saveDir} onChange={(event) => setSaveDir(event.target.value)} placeholder={t.savePlaceholder} />
                    <button className="secondary" onClick={chooseSaveDir}>{t.choose}</button>
                  </div>
                </div>
                <div className="field">
                  <label>{t.currentDir}</label>
                  <input value={currentRemotePath} readOnly />
                </div>
                <div className="field">
                  <label>{t.localHTTP}</label>
                  {status.localHTTPUrl ? (
                    <button className="link-button" onClick={() => BrowserOpenURL(status.localHTTPUrl)}>{status.localHTTPUrl}</button>
                  ) : (
                    <div className="placeholder-link">{t.waitingHTTP}</div>
                  )}
                </div>
              </>
            )}

            <label className="check">
              <input type="checkbox" checked={useUDP} onChange={(event) => setUseUDP(event.target.checked)} />
              <span>{t.useUDP}</span>
            </label>
          </section>

          {mode === 'receive' && (
            <section className="remote-pane">
              <div className="log-header">
                <h3>{t.remoteFiles}</h3>
                <div className="button-row">
                  <button className="secondary" disabled={!status.localHTTPUrl} onClick={() => loadRemoteFiles()}>{t.refresh}</button>
                  {status.downloading ? (
                    <button className="danger" onClick={stopDownload}>{t.stopDownload}</button>
                  ) : (
                    <button className="primary" disabled={!canDownload} onClick={startDownload}>{t.downloadSelected}</button>
                  )}
                </div>
              </div>
              <div className="remote-summary">
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

function shallowEntries(files: RemoteFile[], currentPath: string): VisibleEntry[] {
  const current = normalizeRemotePath(currentPath);
  const byPath = new Map<string, VisibleEntry>();
  for (const file of files) {
    const filePath = normalizeRemotePath(file.path);
    if (filePath === current) {
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
