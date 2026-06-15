import {useEffect, useMemo, useRef, useState} from 'react';
import './App.css';
import {
  GeneratePassword,
  LocateGonc,
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

type LogEvent = {
  type: string;
  level: string;
  message: string;
  time: string;
  localUrl?: string;
  inBytes?: number;
  outBytes?: number;
  inBps?: number;
  outBps?: number;
  elapsed?: string;
};

type P2PReport = {
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
  currentFile?: string;
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
  goncPath: string;
  localHTTPUrl: string;
  downloading: boolean;
};

const defaultStatus: AppStatus = {
  running: false,
  goncPath: '',
  localHTTPUrl: '',
  downloading: false
};

function App() {
  const [mode, setMode] = useState<Mode>('send');
  const [password, setPassword] = useState('');
  const [sharePaths, setSharePaths] = useState<string[]>([]);
  const [saveDir, setSaveDir] = useState('');
  const [downloadSubPath, setDownloadSubPath] = useState('/');
  const [useUDP, setUseUDP] = useState(false);
  const [goncPath, setGoncPath] = useState('');
  const [status, setStatus] = useState<AppStatus>(defaultStatus);
  const [logs, setLogs] = useState<LogEvent[]>([]);
  const [error, setError] = useState('');
  const [p2pReport, setP2PReport] = useState<P2PReport | null>(null);
  const [remoteList, setRemoteList] = useState<RemoteList | null>(null);
  const [downloadProgress, setDownloadProgress] = useState<DownloadEvent | null>(null);
  const [traffic, setTraffic] = useState<LogEvent | null>(null);
  const [passwordVisible, setPasswordVisible] = useState(false);
  const passwordTimer = useRef<number | null>(null);

  const canStart = useMemo(() => {
    if (status.running || password.trim().length === 0) {
      return false;
    }
    if (mode === 'send') {
      return sharePaths.length > 0;
    }
    return true;
  }, [mode, password, sharePaths.length, status.running]);

  const canDownload = Boolean(mode === 'receive' && status.localHTTPUrl && saveDir && !status.downloading);
  const activeSpeed = status.downloading
    ? (downloadProgress?.bytesPerSecond || 0)
    : (mode === 'send' ? (traffic?.outBps || 0) : (traffic?.inBps || 0));

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
          window.setTimeout(() => loadRemoteFiles(true), 700);
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
        setError('Stop the current sender before changing the shared file list.');
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
  }, [mode, status.running]);

  async function refreshStatus() {
    try {
      const next = await Status();
      setStatus(next);
      if (!goncPath && next.goncPath) {
        setGoncPath(next.goncPath);
      }
    } catch (err) {
      setError(String(err));
    }
  }

  async function addFiles() {
    setError('');
    const selected = await SelectFiles();
    appendSharePaths(selected || []);
  }

  async function addFolder() {
    setError('');
    const selected = await SelectFolder('Select folder to send');
    appendSharePaths(selected ? [selected] : []);
  }

  async function chooseSaveDir() {
    setError('');
    const selected = await SelectFolder('Select save directory');
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
      appendLog('status', 'info', 'passphrase copied');
    }
  }

  function revealPasswordTemporarily() {
    setPasswordVisible(true);
    if (passwordTimer.current) {
      window.clearTimeout(passwordTimer.current);
    }
    passwordTimer.current = window.setTimeout(() => setPasswordVisible(false), 5000);
  }

  async function checkGoncPath() {
    setError('');
    try {
      const located = await LocateGonc(goncPath);
      setGoncPath(located);
      setStatus((current) => ({...current, goncPath: located}));
      appendLog('status', 'info', `gonc found: ${located}`);
    } catch (err) {
      setError(String(err));
    }
  }

  async function start() {
    setError('');
    setLogs([]);
    setP2PReport(null);
    setRemoteList(null);
    setDownloadProgress(null);
    setTraffic(null);
    try {
      await StartTransfer({
        mode,
        password,
        sharePaths,
        saveDir,
        goncPath,
        downloadSubPath,
        useUDP
      });
      await refreshStatus();
    } catch (err) {
      setError(String(err));
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
      setError(String(err));
    }
  }

  async function loadRemoteFiles(silent = false) {
    if (!silent) {
      setError('');
    }
    try {
      const list = await RemoteFiles(downloadSubPath || '/');
      setRemoteList(list);
    } catch (err) {
      if (!silent) {
        setError(String(err));
      }
    }
  }

  async function startDownload() {
    setError('');
    try {
      await StartHTTPDownload(saveDir, downloadSubPath || '/');
      await refreshStatus();
    } catch (err) {
      setError(String(err));
    }
  }

  async function stopDownload() {
    setError('');
    try {
      await StopHTTPDownload();
      await refreshStatus();
    } catch (err) {
      setError(String(err));
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

  return (
    <main className="shell">
      <section className="workspace">
        <aside className="rail">
          <div className="brand">
            <div className="brand-mark">G</div>
            <div>
              <h1>Gonc Transfer</h1>
              <p>P2P file transfer</p>
            </div>
          </div>

          <div className="mode-switch" role="tablist" aria-label="Transfer mode">
            <button className={mode === 'send' ? 'active' : ''} onClick={() => setMode('send')}>
              Send
            </button>
            <button className={mode === 'receive' ? 'active' : ''} onClick={() => setMode('receive')}>
              Receive
            </button>
          </div>

          <div className="status-block">
            <span className={status.running ? 'dot running' : 'dot'} />
            <span>{status.running ? 'Running' : 'Idle'}</span>
          </div>
        </aside>

        <section className="main-pane">
          <header className="topbar">
            <div>
              <p className="eyebrow">{mode === 'send' ? 'Sender' : 'Receiver'}</p>
              <h2>{mode === 'send' ? 'Share files through gonc P2P' : 'Connect and download through local HTTP'}</h2>
            </div>
            <div className="actions">
              <button className="secondary" onClick={checkGoncPath}>Check gonc</button>
              {status.running ? (
                <button className="danger" onClick={stop}>Stop</button>
              ) : (
                <button className="primary" disabled={!canStart} onClick={start}>Start</button>
              )}
            </div>
          </header>

          {error && <div className="alert">{error}</div>}

          <section className="status-grid">
            <Metric label="P2P status" value={p2pReport?.status || (status.running ? 'starting' : 'idle')} />
            <Metric label="Peer" value={p2pReport?.peer || '-'} />
            <Metric label="Network" value={p2pReport?.network || '-'} />
            <Metric label="Speed" value={formatRate(activeSpeed)} />
          </section>

          <section className="form-grid">
            <div className="field wide">
              <label>Passphrase</label>
              <div className="inline">
                <input
                  type={passwordVisible ? 'text' : 'password'}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="Same passphrase on both sides"
                />
                <button className="secondary" onClick={generatePassword}>Generate</button>
                <button className="secondary" disabled={!password} onClick={copyPassword}>Copy</button>
              </div>
            </div>

            <div className="field wide">
              <label>gonc executable</label>
              <input
                value={goncPath}
                onChange={(event) => setGoncPath(event.target.value)}
                placeholder={status.goncPath || 'Auto detect from bundled files, sibling gonetcat/bin, or PATH'}
              />
            </div>

            {mode === 'send' ? (
              <div className="field wide">
                <label>Files and folders to send</label>
                <div className="button-row">
                  <button className="secondary" disabled={status.running} onClick={addFiles}>Add Files</button>
                  <button className="secondary" disabled={status.running} onClick={addFolder}>Add Folder</button>
                </div>
                {status.running && <p className="muted">Stop the sender before changing the shared list.</p>}
                <div className="drop-zone">
                  <div className="path-list">
                    {sharePaths.length === 0 ? (
                      <p className="muted">Drag and drop files or folders into this list, or add them with the buttons above.</p>
                    ) : sharePaths.map((path) => (
                      <div className="path-row" key={path}>
                        <span>{path}</span>
                        <button disabled={status.running} onClick={() => removeSharePath(path)} aria-label={`Remove ${path}`}>Remove</button>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            ) : (
              <>
                <div className="field wide">
                  <label>Save directory</label>
                  <div className="inline">
                    <input value={saveDir} readOnly placeholder="Choose where downloaded files will be saved" />
                    <button className="secondary" onClick={chooseSaveDir}>Choose</button>
                  </div>
                </div>
                <div className="field">
                  <label>Remote subpath</label>
                  <input
                    value={downloadSubPath}
                    onChange={(event) => setDownloadSubPath(event.target.value)}
                    placeholder="/"
                  />
                </div>
                <div className="field">
                  <label>Local HTTP</label>
                  {status.localHTTPUrl ? (
                    <button className="link-button" onClick={() => BrowserOpenURL(status.localHTTPUrl)}>
                      {status.localHTTPUrl}
                    </button>
                  ) : (
                    <div className="placeholder-link">Waiting for -httplocal endpoint</div>
                  )}
                </div>
              </>
            )}

            <label className="check">
              <input
                type="checkbox"
                checked={useUDP}
                onChange={(event) => setUseUDP(event.target.checked)}
              />
              <span>Use UDP protocol</span>
            </label>
          </section>

          {mode === 'receive' && (
            <section className="remote-pane">
              <div className="log-header">
                <h3>Remote Files</h3>
                <div className="button-row">
                  <button className="secondary" disabled={!status.localHTTPUrl} onClick={() => loadRemoteFiles()}>Refresh</button>
                  {status.downloading ? (
                    <button className="danger" onClick={stopDownload}>Stop Download</button>
                  ) : (
                    <button className="primary" disabled={!canDownload} onClick={startDownload}>Download All</button>
                  )}
                </div>
              </div>
              <div className="remote-summary">
                <span>{remoteList ? `${remoteList.fileCount} files` : 'No list loaded'}</span>
                <span>{remoteList ? `${remoteList.dirCount} folders` : '-'}</span>
                <span>{remoteList ? formatBytes(remoteList.totalSize) : '-'}</span>
              </div>
              {downloadProgress && (
                <div className="progress">
                  <div>
                    {formatPercent(downloadProgress.doneBytes || 0, downloadProgress.totalBytes || 0)}
                    {' '}· {downloadProgress.doneFiles || 0}/{downloadProgress.totalFiles || 0} files
                    {' '}· {formatBytes(downloadProgress.doneBytes || 0)} / {formatBytes(downloadProgress.totalBytes || 0)}
                    {' '}· {formatRate(downloadProgress.bytesPerSecond || 0)}
                  </div>
                  <progress max={downloadProgress.totalBytes || 1} value={downloadProgress.doneBytes || 0} />
                </div>
              )}
              <div className="remote-list">
                {!remoteList ? (
                  <p className="muted">After the local HTTP endpoint appears, refresh to read the remote JSON file list.</p>
                ) : remoteList.files.slice(0, 120).map((file) => (
                  <div className="remote-row" key={file.path}>
                    <span>{file.is_dir ? 'DIR' : 'FILE'}</span>
                    <strong>{file.path}</strong>
                    <em>{file.is_dir ? '' : formatBytes(file.size)}</em>
                  </div>
                ))}
              </div>
            </section>
          )}

          <section className="log-pane">
            <div className="log-header">
              <h3>Activity</h3>
              <button className="ghost" onClick={() => setLogs([])}>Clear</button>
            </div>
            <div className="logs">
              {logs.length === 0 ? (
                <p className="muted">Logs will appear here after a transfer starts.</p>
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

export default App;
