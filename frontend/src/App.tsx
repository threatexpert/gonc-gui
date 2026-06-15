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
  localHTTPUrl: string;
  downloading: boolean;
};

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
    sendTitle: '通过 gonc P2P 分享文件',
    receiveTitle: '连接并通过本地 HTTP 下载',
    stop: '停止',
    start: '开始',
    p2pStatus: 'P2P 状态',
    peer: '对端',
    network: '网络',
    speed: '速度',
    passphrase: '口令',
    passPlaceholder: '两端使用相同口令',
    generate: '生成',
    copy: '复制',
    copied: '口令已复制',
    sharedList: '要发送的文件和目录',
    addFiles: '添加文件',
    addFolder: '添加目录',
    stopBeforeEdit: '请先停止发送任务，再修改分享列表。',
    dropHint: '可将文件或目录拖放到这里，也可以用上面的按钮添加。',
    remove: '移除',
    saveDir: '保存目录',
    savePlaceholder: '选择下载文件保存的位置',
    choose: '选择',
    remoteSubpath: '远端子目录',
    localHTTP: '本地 HTTP',
    waitingHTTP: '等待 -httplocal 端口建立',
    useUDP: '使用 UDP 协议',
    remoteFiles: '远端文件',
    refresh: '刷新',
    stopDownload: '停止下载',
    downloadAll: '全部下载',
    noList: '尚未读取目录',
    files: '个文件',
    folders: '个目录',
    listHint: '本地 HTTP 建立后会自动读取远端 JSON 文件列表，也可以手动刷新。',
    activity: '活动日志',
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
    sendTitle: 'Share files through gonc P2P',
    receiveTitle: 'Connect and download through local HTTP',
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
    remoteSubpath: 'Remote subpath',
    localHTTP: 'Local HTTP',
    waitingHTTP: 'Waiting for -httplocal endpoint',
    useUDP: 'Use UDP protocol',
    remoteFiles: 'Remote Files',
    refresh: 'Refresh',
    stopDownload: 'Stop Download',
    downloadAll: 'Download All',
    noList: 'No list loaded',
    files: 'files',
    folders: 'folders',
    listHint: 'After the local HTTP endpoint appears, the remote JSON file list is loaded automatically. You can refresh manually too.',
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
  const [downloadSubPath, setDownloadSubPath] = useState('/');
  const [useUDP, setUseUDP] = useState(false);
  const [status, setStatus] = useState<AppStatus>({running: false, localHTTPUrl: '', downloading: false});
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
    return mode === 'receive' || sharePaths.length > 0;
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
      });
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  async function addFiles() {
    setError('');
    const selected = await SelectFiles();
    appendSharePaths(selected || []);
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
    setDownloadProgress(null);
    setTraffic(null);
    try {
      await StartTransfer({
        mode,
        password,
        sharePaths,
        saveDir,
        goncPath: '',
        downloadSubPath,
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

  async function loadRemoteFiles(silent = false) {
    if (!silent) {
      setError('');
    }
    try {
      const list = await RemoteFiles(downloadSubPath || '/');
      setRemoteList(list);
    } catch (err) {
      if (!silent) {
        setError(localizeError(String(err)));
      }
    }
  }

  async function startDownload() {
    setError('');
    try {
      await StartHTTPDownload(saveDir, downloadSubPath || '/');
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
            <button className={mode === 'send' ? 'active' : ''} onClick={() => setMode('send')}>
              {t.send}
            </button>
            <button className={mode === 'receive' ? 'active' : ''} onClick={() => setMode('receive')}>
              {t.receive}
            </button>
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
                    <input value={saveDir} readOnly placeholder={t.savePlaceholder} />
                    <button className="secondary" onClick={chooseSaveDir}>{t.choose}</button>
                  </div>
                </div>
                <div className="field">
                  <label>{t.remoteSubpath}</label>
                  <input
                    value={downloadSubPath}
                    onChange={(event) => setDownloadSubPath(event.target.value)}
                    placeholder="/"
                  />
                </div>
                <div className="field">
                  <label>{t.localHTTP}</label>
                  {status.localHTTPUrl ? (
                    <button className="link-button" onClick={() => BrowserOpenURL(status.localHTTPUrl)}>
                      {status.localHTTPUrl}
                    </button>
                  ) : (
                    <div className="placeholder-link">{t.waitingHTTP}</div>
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
                    <button className="primary" disabled={!canDownload} onClick={startDownload}>{t.downloadAll}</button>
                  )}
                </div>
              </div>
              <div className="remote-summary">
                <span>{remoteList ? `${remoteList.fileCount} ${t.files}` : t.noList}</span>
                <span>{remoteList ? `${remoteList.dirCount} ${t.folders}` : '-'}</span>
                <span>{remoteList ? formatBytes(remoteList.totalSize) : '-'}</span>
              </div>
              {downloadProgress && (
                <div className="progress">
                  <div>
                    {formatPercent(downloadProgress.doneBytes || 0, downloadProgress.totalBytes || 0)}
                    {' '}· {downloadProgress.doneFiles || 0}/{downloadProgress.totalFiles || 0} {t.files}
                    {' '}· {formatBytes(downloadProgress.doneBytes || 0)} / {formatBytes(downloadProgress.totalBytes || 0)}
                    {' '}· {formatRate(downloadProgress.bytesPerSecond || 0)}
                  </div>
                  <progress max={downloadProgress.totalBytes || 1} value={downloadProgress.doneBytes || 0} />
                </div>
              )}
              <div className="remote-list">
                {!remoteList ? (
                  <p className="muted">{t.listHint}</p>
                ) : remoteList.files.slice(0, 120).map((file) => (
                  <div className="remote-row" key={file.path}>
                    <span>{file.is_dir ? t.dir : t.file}</span>
                    <strong>{file.path}</strong>
                    <em>{file.is_dir ? '' : formatBytes(file.size)}</em>
                  </div>
                ))}
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
