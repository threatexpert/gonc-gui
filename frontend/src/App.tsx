import {useEffect, useMemo, useState} from 'react';
import './App.css';
import {
  GeneratePassword,
  LocateGonc,
  SelectFiles,
  SelectFolder,
  StartTransfer,
  Status,
  StopTransfer
} from '../wailsjs/go/main/App';
import {EventsOff, EventsOn} from '../wailsjs/runtime/runtime';

type Mode = 'send' | 'receive';

type LogEvent = {
  type: string;
  level: string;
  message: string;
  time: string;
};

type AppStatus = {
  running: boolean;
  goncPath: string;
};

const defaultStatus: AppStatus = {
  running: false,
  goncPath: ''
};

function App() {
  const [mode, setMode] = useState<Mode>('send');
  const [password, setPassword] = useState('');
  const [sharePaths, setSharePaths] = useState<string[]>([]);
  const [saveDir, setSaveDir] = useState('');
  const [downloadSubPath, setDownloadSubPath] = useState('');
  const [noCompress, setNoCompress] = useState(false);
  const [goncPath, setGoncPath] = useState('');
  const [status, setStatus] = useState<AppStatus>(defaultStatus);
  const [logs, setLogs] = useState<LogEvent[]>([]);
  const [error, setError] = useState('');

  const canStart = useMemo(() => {
    if (status.running || password.trim().length === 0) {
      return false;
    }
    if (mode === 'send') {
      return sharePaths.length > 0;
    }
    return saveDir.length > 0;
  }, [mode, password, saveDir, sharePaths.length, status.running]);

  useEffect(() => {
    refreshStatus();
    EventsOn('gonc:event', (event: LogEvent) => {
      setLogs((current) => [...current.slice(-399), event]);
      if (event.type === 'status') {
        refreshStatus();
      }
    });
    return () => EventsOff('gonc:event');
  }, []);

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
    try {
      await StartTransfer({
        mode,
        password,
        sharePaths,
        saveDir,
        goncPath,
        downloadSubPath,
        noCompress
      });
      await refreshStatus();
    } catch (err) {
      setError(String(err));
    }
  }

  async function stop() {
    setError('');
    try {
      await StopTransfer();
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
              <h2>{mode === 'send' ? 'Share files through gonc P2P' : 'Download from a peer'}</h2>
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

          <section className="form-grid">
            <div className="field wide">
              <label>Passphrase</label>
              <div className="inline">
                <input
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  placeholder="Same passphrase on both sides"
                />
                <button className="secondary" onClick={generatePassword}>Generate</button>
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
                  <button className="secondary" onClick={addFiles}>Add Files</button>
                  <button className="secondary" onClick={addFolder}>Add Folder</button>
                </div>
                <div className="path-list">
                  {sharePaths.length === 0 ? (
                    <p className="muted">No files or folders selected.</p>
                  ) : sharePaths.map((path) => (
                    <div className="path-row" key={path}>
                      <span>{path}</span>
                      <button onClick={() => removeSharePath(path)} aria-label={`Remove ${path}`}>Remove</button>
                    </div>
                  ))}
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
              </>
            )}

            <label className="check">
              <input
                type="checkbox"
                checked={noCompress}
                onChange={(event) => setNoCompress(event.target.checked)}
              />
              <span>Disable compression</span>
            </label>
          </section>

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

export default App;
