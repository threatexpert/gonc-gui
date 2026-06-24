import {useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent} from 'react';
import QRCode from 'qrcode';
import jsQR from 'jsqr';
import './App.css';
import {
  CaptureScreen,
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
type DownloadMode = 'resume' | 'overwrite';

type LogEvent = {
  type: string;
  level: string;
  message: string;
  time: string;
  mode?: string;
  localUrl?: string;
  inBps?: number;
  outBps?: number;
};

type P2PReport = {
  topic: string;
  status: string;
  network: string;
  mode: string;
  side?: Mode;
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
  totalDirs?: number;
  doneDirs?: number;
  skippedFiles?: number;
  resumedFiles?: number;
  failedFiles?: number;
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
  sendRunning: boolean;
  receiveRunning: boolean;
  localHTTPUrl: string;
  downloading: boolean;
  defaultSaveDir: string;
};

type VisibleEntry = RemoteFile & {
  synthetic?: boolean;
};

const appVersion = 'v1.0.4';

const text = {
  zh: {
    brand: 'Gonc',
    subtitle: '点对点安全传输工具',
    send: '发送文件',
    receive: '接收文件',
    running: '运行中',
    idle: '空闲',
    sender: '发送方',
    receiver: '接收方',
    sendTitle: '发送文件',
    receiveTitle: '接收文件',
    stop: '停止',
    start: '开始',
    startShare: '开始分享',
    startReceive: '连接对方',
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
    connectionRoute: '连接方式',
    directRoute: '直连',
    relayRoute: '中继',
    speed: '速度',
    passphrase: '口令',
    senderPassphrase: '口令（已为你生成高强度随机口令,建议直接使用。口令是连接安全的唯一凭据,请通过安全渠道分享给接收方）',
    passPlaceholder: '两端使用相同口令',
    senderPasswordHint: '口令相同即可连接，谁生成谁扫码都行。口令仅需分享给接收方。双方用口令哈希在公共 MQTT 服务器碰头,网络地址以口令 AES 加密后交换——该服务器看不到口令也解不出地址。随后建立点对点直连,数据不经中转;连接基于口令完成双向认证与密钥协商,TLS 加密、无需 CA 证书,杜绝中间人窃听篡改。',
    receiverPasswordHint: '口令相同即可连接，谁生成谁扫码都行。口令用于发现双方网络地址。双方用口令哈希在公共 MQTT 服务器碰头,网络地址以口令 AES 加密后交换——该服务器看不到口令也解不出地址。随后建立点对点直连,数据不经中转;连接基于口令完成双向认证与密钥协商,TLS 加密、无需 CA 证书,杜绝中间人窃听篡改。',
    generate: '更换',
    copy: '复制',
    copyLogs: '复制日志',
    qr: '二维码',
    scan: '截图扫码',
    scanTitle: '框选屏幕上的二维码',
    scanHint: '拖动鼠标框选二维码区域，或点击「识别整张」。多显示器会一并截取。',
    scanWhole: '识别整张',
    scanAgain: '重新截图',
    scanNotFound: '未识别到二维码，请重新框选或重新截图。',
    scanSuccess: '已从二维码识别口令',
    paste: '粘贴',
    copied: '口令已复制',
    logsCopied: '日志已复制',
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
    remoteFiles: '对方分享的文件',
    refresh: '刷新',
    stopDownload: '停止下载',
    downloadSelected: '下载选中',
    downloadMode: '下载方式',
    resumeDownload: '续传',
    overwriteDownload: '覆盖',
    noSelection: '请先勾选要下载的文件或目录。',
    noList: '尚未读取目录',
    files: '个文件',
    folders: '个目录',
    selected: '已选',
    completed: '完成',
    skipped: '跳过',
    resumed: '续传',
    failed: '失败',
    listHint: '连接建立后会自动读取对方分享的文件目录；可点击目录进入，并勾选需要下载的项。',
    activity: '活动日志',
    diagnostics: '状态和日志',
    clear: '清空',
    close: '关闭',
    logHint: '传输开始后日志会显示在这里。',
    file: '文件',
    dir: '目录',
    goncMissing: '未找到 gonc 可执行文件。请确认发布目录中包含 bundled/gonc/当前平台/gonc(.exe)，或已把 gonc 加入 PATH。',
    senderLockedDrop: '发送任务运行中，不能修改分享列表。',
    weakPassword: '口令强度不足。请使用至少 8 位，并同时包含字母和数字的口令。',
  },
  en: {
    brand: 'Gonc',
    subtitle: 'Secure peer-to-peer transfer tool',
    send: 'Send Files',
    receive: 'Receive Files',
    running: 'Running',
    idle: 'Idle',
    sender: 'Sender',
    receiver: 'Receiver',
    sendTitle: 'Send files',
    receiveTitle: 'Receive files',
    stop: 'Stop',
    start: 'Start',
    startShare: 'Start Sharing',
    startReceive: 'Connect',
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
    connectionRoute: 'Route',
    directRoute: 'Direct',
    relayRoute: 'Relay',
    speed: 'Speed',
    passphrase: 'Passphrase',
    senderPassphrase: 'Passphrase (a high-strength random passphrase has been generated for you; using it directly is recommended. This is the only credential for connection security, so share it with the receiver through a secure channel)',
    passPlaceholder: 'Same passphrase on both sides',
    senderPasswordHint: 'The same passphrase is all you need to connect; either side can generate it or scan the QR. Share the passphrase only with the receiver. Both sides meet on the public MQTT server using a passphrase hash, and exchange network addresses encrypted with passphrase-derived AES, so the server cannot see the passphrase or decrypt the addresses. A direct peer-to-peer connection is then established; data is not relayed. The connection uses the passphrase for mutual authentication and key negotiation, with TLS encryption and no CA certificate required, preventing man-in-the-middle eavesdropping or tampering.',
    receiverPasswordHint: 'The same passphrase is all you need to connect; either side can generate it or scan the QR. The passphrase is used to discover each side\'s network address. Both sides meet on the public MQTT server using a passphrase hash, and exchange network addresses encrypted with passphrase-derived AES, so the server cannot see the passphrase or decrypt the addresses. A direct peer-to-peer connection is then established; data is not relayed. The connection uses the passphrase for mutual authentication and key negotiation, with TLS encryption and no CA certificate required, preventing man-in-the-middle eavesdropping or tampering.',
    generate: 'Change',
    copy: 'Copy',
    copyLogs: 'Copy Logs',
    qr: 'QR',
    scan: 'Scan',
    scanTitle: 'Select the QR code on screen',
    scanHint: 'Drag to select the QR area, or click "Whole image". All monitors are captured.',
    scanWhole: 'Whole image',
    scanAgain: 'Recapture',
    scanNotFound: 'No QR code found. Try selecting again or recapture.',
    scanSuccess: 'Passphrase read from QR code',
    paste: 'Paste',
    copied: 'Passphrase copied',
    logsCopied: 'Activity copied',
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
    remoteFiles: 'Peer Shared Files',
    refresh: 'Refresh',
    stopDownload: 'Stop Download',
    downloadSelected: 'Download Selected',
    downloadMode: 'Download Mode',
    resumeDownload: 'Resume',
    overwriteDownload: 'Overwrite',
    noSelection: 'Select files or folders to download first.',
    noList: 'No list loaded',
    files: 'files',
    folders: 'folders',
    selected: 'selected',
    completed: 'Done',
    skipped: 'Skipped',
    resumed: 'Resumed',
    failed: 'Failed',
    listHint: 'After the connection is ready, shared files from the peer are loaded automatically. Click folders to browse and tick items to download.',
    activity: 'Activity',
    diagnostics: 'Status and Logs',
    clear: 'Clear',
    close: 'Close',
    logHint: 'Logs will appear here after a transfer starts.',
    file: 'FILE',
    dir: 'DIR',
    goncMissing: 'gonc executable was not found. Make sure bundled/gonc/current-platform/gonc(.exe) exists, or put gonc in PATH.',
    senderLockedDrop: 'The sender is running. Stop it before changing the shared list.',
    weakPassword: 'Passphrase is too weak. Use at least 8 characters with both letters and digits.',
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
  const [sendPassword, setSendPassword] = useState('');
  const [receivePassword, setReceivePassword] = useState('');
  const [sharePaths, setSharePaths] = useState<string[]>([]);
  const [saveDir, setSaveDir] = useState('');
  const [currentRemotePath, setCurrentRemotePath] = useState('/');
  const [useUDP, setUseUDP] = useState(false);
  const [status, setStatus] = useState<AppStatus>({running: false, sendRunning: false, receiveRunning: false, localHTTPUrl: '', downloading: false, defaultSaveDir: ''});
  const [logs, setLogs] = useState<LogEvent[]>([]);
  const [error, setError] = useState('');
  const [receiveP2PReport, setReceiveP2PReport] = useState<P2PReport | null>(null);
  const [sendP2PReports, setSendP2PReports] = useState<Record<string, P2PReport>>({});
  const [remoteList, setRemoteList] = useState<RemoteList | null>(null);
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [downloadProgress, setDownloadProgress] = useState<DownloadEvent | null>(null);
  const [downloadMode, setDownloadMode] = useState<DownloadMode>('resume');
  const [sendTraffic, setSendTraffic] = useState<LogEvent | null>(null);
  const [receiveTraffic, setReceiveTraffic] = useState<LogEvent | null>(null);
  const [passwordVisible, setPasswordVisible] = useState(false);
  const [qrDataUrl, setQrDataUrl] = useState('');
  const [qrPassword, setQrPassword] = useState('');
  const [scanImage, setScanImage] = useState('');
  const [scanBusy, setScanBusy] = useState(false);
  const [scanError, setScanError] = useState('');
  const [scanRect, setScanRect] = useState<{x: number; y: number; w: number; h: number} | null>(null);
  const scanImgRef = useRef<HTMLImageElement | null>(null);
  const scanDragStart = useRef<{x: number; y: number} | null>(null);
  const [nowTick, setNowTick] = useState(Date.now());
  const passwordTimer = useRef<number | null>(null);
  const activePassword = mode === 'send' ? sendPassword : receivePassword;

  const visibleEntries = useMemo(() => shallowEntries(remoteList?.files || [], currentRemotePath), [remoteList, currentRemotePath]);
  const activeSpeed = Math.max(
    freshSpeed(downloadProgress?.time, downloadProgress?.bytesPerSecond, nowTick),
    freshSpeed(receiveTraffic?.time, receiveTraffic?.inBps, nowTick),
    freshSpeed(receiveTraffic?.time, receiveTraffic?.outBps, nowTick)
  );
  const sendRunning = status.sendRunning;
  const receiveRunning = status.receiveRunning;
  const currentRunning = mode === 'send' ? sendRunning : receiveRunning;
  const canStart = !currentRunning && activePassword.trim().length > 0 && (mode === 'receive' || sharePaths.length > 0);
  const canDownload = Boolean(mode === 'receive' && status.localHTTPUrl && saveDir && selectedPaths.size > 0 && !status.downloading);
  const canDownloadAll = Boolean(mode === 'receive' && status.localHTTPUrl && saveDir && remoteList && visibleEntries.length > 0 && !status.downloading);
  const primaryLabel = mode === 'send' ? t.startShare : t.startReceive;
  const p2pSessions = useMemo(() => Object.values(sendP2PReports), [sendP2PReports]);
  const connectedCount = p2pSessions.filter((report) => report.topic && report.status === 'connected').length;
  const connectingCount = p2pSessions.filter((report) => report.topic && report.status === 'connecting').length;
  const transferSpeed = mode === 'send'
    ? freshSpeed(sendTraffic?.time, sendTraffic?.outBps, nowTick)
    : activeSpeed;
  const sendStatusLabel = connectingCount > 0 ? t.newConnection : (sendRunning ? t.waitingConnection : t.idle);
  const receiveStatus = receiveConnectionStatus(receiveP2PReport, receiveRunning, Boolean(status.localHTTPUrl), t);
  const statusTone = mode === 'receive' ? receiveStatus.tone : (sendRunning ? 'running' : 'idle');

  useEffect(() => {
    const timer = window.setInterval(() => setNowTick(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    let cancelled = false;
    GeneratePassword()
      .then((value) => {
        if (!cancelled) {
          setSendPassword((current) => current || value);
        }
      })
      .catch((err) => setError(localizeError(String(err))));
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    refreshStatus();
    EventsOn('gonc:event', (event: LogEvent) => {
      if (event.type === 'traffic') {
        if (event.mode === 'send') {
          setSendTraffic(event);
        } else if (event.mode === 'receive') {
          setReceiveTraffic(event);
        }
        return;
      }
      setLogs((current) => [...current.slice(-399), event]);
      if (event.mode === 'receive' && event.localUrl) {
        setStatus((current) => ({...current, localHTTPUrl: event.localUrl || current.localHTTPUrl}));
        if (mode === 'receive') {
          window.setTimeout(() => loadRemoteFiles('/', true), 700);
        }
      }
      if (event.type === 'status' || event.type === 'local_http') {
        if (event.message.includes('stopped') || event.message.includes('finished')) {
          if (event.mode === 'receive') {
            setReceiveP2PReport((current) => current ? {...current, status: event.message.includes('stopped') ? 'stopped' : 'finished'} : null);
          }
        }
        refreshStatus();
      }
    });
    EventsOn('p2p:report', (report: P2PReport) => {
      if (report.side === 'send') {
        setSendP2PReports((current) => ({...current, [p2pSessionKey(report)]: report}));
      } else if (report.side === 'receive') {
        setReceiveP2PReport(report);
      }
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
      if (mode === 'send' && !sendRunning) {
        appendSharePaths(paths);
      } else if (mode === 'send' && sendRunning) {
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
  }, [mode, sendRunning, t.senderLockedDrop]);

  async function refreshStatus() {
    try {
      const next = await Status();
      setStatus({
        running: next.running,
        sendRunning: next.sendRunning,
        receiveRunning: next.receiveRunning,
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
    setSendPassword(await GeneratePassword());
    revealPasswordTemporarily();
  }

  async function generateReceivePassword() {
    setError('');
    setReceivePassword(await GeneratePassword());
    revealPasswordTemporarily();
  }

  async function copyPassword() {
    if (activePassword) {
      await navigator.clipboard.writeText(activePassword);
      revealPasswordTemporarily();
      appendLog('status', 'info', t.copied);
    }
  }

  async function copyLogs() {
    if (logs.length === 0) {
      return;
    }
    const content = logs
      .map((log) => `[${new Date(log.time).toLocaleTimeString()}] ${log.level.toUpperCase()} ${log.message}`)
      .join('\n');
    await navigator.clipboard.writeText(content);
    appendLog('status', 'info', t.logsCopied);
  }

  async function pastePassword() {
    setError('');
    try {
      const value = await ClipboardGetText();
      setReceivePassword(value.trim());
      revealPasswordTemporarily();
    } catch (err) {
      try {
        const value = await navigator.clipboard.readText();
        setReceivePassword(value.trim());
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

  async function showPasswordQr() {
    const password = activePassword.trim();
    if (!password) {
      return;
    }
    setError('');
    try {
      setQrPassword(password);
      setQrDataUrl(await QRCode.toDataURL(password, {
        width: 280,
        margin: 2,
        color: {
          dark: '#101826',
          light: '#ffffff',
        },
      }));
      revealPasswordTemporarily();
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  function closePasswordQr() {
    setQrDataUrl('');
    setQrPassword('');
  }

  async function startScreenScan() {
    setError('');
    setScanError('');
    setScanRect(null);
    setScanBusy(true);
    try {
      const dataUrl = await CaptureScreen();
      setScanImage(dataUrl);
    } catch (err) {
      setError(localizeError(String(err)));
    } finally {
      setScanBusy(false);
    }
  }

  function closeScreenScan() {
    setScanImage('');
    setScanRect(null);
    setScanError('');
    scanDragStart.current = null;
  }

  async function decodeScanRegion(sx: number, sy: number, sw: number, sh: number) {
    const img = scanImgRef.current;
    if (!img || sw < 2 || sh < 2) {
      return;
    }
    setScanBusy(true);
    setScanError('');
    try {
      const canvas = document.createElement('canvas');
      canvas.width = Math.round(sw);
      canvas.height = Math.round(sh);
      const ctx = canvas.getContext('2d');
      if (!ctx) {
        throw new Error('canvas is unavailable');
      }
      ctx.drawImage(img, sx, sy, sw, sh, 0, 0, canvas.width, canvas.height);
      const data = ctx.getImageData(0, 0, canvas.width, canvas.height);
      const result = jsQR(data.data, data.width, data.height);
      if (result && result.data) {
        const decoded = result.data.trim();
        if (mode === 'send') {
          setSendPassword(decoded);
        } else {
          setReceivePassword(decoded);
        }
        revealPasswordTemporarily();
        closeScreenScan();
        appendLog('status', 'info', t.scanSuccess);
      } else {
        setScanError(t.scanNotFound);
      }
    } catch (err) {
      setScanError(localizeError(String(err)));
    } finally {
      setScanBusy(false);
    }
  }

  function decodeWholeScan() {
    const img = scanImgRef.current;
    if (!img) {
      return;
    }
    setScanRect(null);
    decodeScanRegion(0, 0, img.naturalWidth, img.naturalHeight);
  }

  function scanPointerDown(event: ReactPointerEvent<HTMLDivElement>) {
    const img = scanImgRef.current;
    if (!img) {
      return;
    }
    event.currentTarget.setPointerCapture(event.pointerId);
    const rect = img.getBoundingClientRect();
    const x = event.clientX - rect.left;
    const y = event.clientY - rect.top;
    scanDragStart.current = {x, y};
    setScanRect({x, y, w: 0, h: 0});
    setScanError('');
  }

  function scanPointerMove(event: ReactPointerEvent<HTMLDivElement>) {
    const img = scanImgRef.current;
    const start = scanDragStart.current;
    if (!img || !start) {
      return;
    }
    const rect = img.getBoundingClientRect();
    const cx = Math.max(0, Math.min(event.clientX - rect.left, rect.width));
    const cy = Math.max(0, Math.min(event.clientY - rect.top, rect.height));
    setScanRect({
      x: Math.min(start.x, cx),
      y: Math.min(start.y, cy),
      w: Math.abs(cx - start.x),
      h: Math.abs(cy - start.y),
    });
  }

  function scanPointerUp() {
    const img = scanImgRef.current;
    const rect = scanRect;
    scanDragStart.current = null;
    if (!img || !rect || rect.w < 4 || rect.h < 4) {
      return;
    }
    const scale = img.naturalWidth / img.getBoundingClientRect().width;
    decodeScanRegion(rect.x * scale, rect.y * scale, rect.w * scale, rect.h * scale);
  }

  async function start() {
    setError('');
    const passphrase = activePassword.trim();
    if (mode === 'send') {
      setSendP2PReports({});
      setSendTraffic(null);
    } else {
      setReceiveP2PReport(null);
      setRemoteList(null);
      setSelectedPaths(new Set());
      setDownloadProgress(null);
      setReceiveTraffic(null);
      setCurrentRemotePath('/');
    }
    try {
      await StartTransfer({
        mode,
        password: passphrase,
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
      await StopTransfer(mode);
      if (mode === 'send') {
        setSendP2PReports({});
        setSendTraffic(null);
      } else {
        setReceiveP2PReport((current) => current ? {...current, status: 'stopped'} : null);
        setReceiveTraffic(null);
      }
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
      await StartHTTPDownload(saveDir, currentRemotePath, Array.from(selectedPaths), downloadMode === 'resume');
      await refreshStatus();
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  async function startDownloadAll() {
    setError('');
    try {
      await StartHTTPDownload(saveDir, currentRemotePath, [], downloadMode === 'resume');
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
    if (message.includes('password is too weak')) {
      return t.weakPassword;
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
            <span className="status-divider" />
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
                    <button className="primary-light" disabled={sendRunning} onClick={addFiles}>{t.addFiles}</button>
                    <button className="secondary" disabled={sendRunning} onClick={addFolder}>{t.addFolder}</button>
                  </div>
                  <div className="drop-zone">
                    <div className="path-list">
                      {sharePaths.length === 0 ? (
                        <p className="drop-hint">{t.dropHint}</p>
                      ) : sharePaths.map((path) => (
                        <div className="path-row" key={path}>
                          <span>{path}</span>
                          <button disabled={sendRunning} onClick={() => removeSharePath(path)} aria-label={`${t.remove} ${path}`}>{t.remove}</button>
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
              <label>{mode === 'send' ? t.senderPassphrase : t.passphrase}</label>
              <div className="inline password-line">
                <input
                  type={passwordVisible ? 'text' : 'password'}
                  value={activePassword}
                  onChange={(event) => mode === 'send' ? setSendPassword(event.target.value) : setReceivePassword(event.target.value)}
                  placeholder={t.passPlaceholder}
                  disabled={currentRunning}
                />
                {mode === 'send' ? (
                  <>
                    <button className="secondary" disabled={!sendPassword} onClick={copyPassword}>{t.copy}</button>
                    {!sendRunning && <button className="secondary" onClick={generatePassword}>{t.generate}</button>}
                    {!sendRunning && <button className="secondary" disabled={scanBusy} onClick={startScreenScan}>{t.scan}</button>}
                    <button className="secondary" disabled={!sendPassword} onClick={showPasswordQr}>{t.qr}</button>
                  </>
                ) : (
                  <>
                    {!receiveRunning && <button className="secondary" onClick={pastePassword}>{t.paste}</button>}
                    {!receiveRunning && <button className="secondary" onClick={generateReceivePassword}>{t.generate}</button>}
                    {!receiveRunning && <button className="secondary" disabled={scanBusy} onClick={startScreenScan}>{t.scan}</button>}
                    <button className="secondary" disabled={!receivePassword} onClick={showPasswordQr}>{t.qr}</button>
                  </>
                )}
              </div>
              {mode === 'send' ? (
                <div className="field-hint">
                  <p>{t.senderPasswordHint}</p>
                </div>
              ) : (
                <div className="field-hint">
                  <p>{t.receiverPasswordHint}</p>
                </div>
              )}
            </div>

            <label className="check">
              <input type="checkbox" checked={useUDP} onChange={(event) => setUseUDP(event.target.checked)} />
              <span>{t.useUDP}</span>
            </label>

            <div className="primary-actions">
              {currentRunning ? (
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
                  <div className="compact-switch" aria-label={t.downloadMode}>
                    <button className={downloadMode === 'resume' ? 'active' : ''} disabled={status.downloading} onClick={() => setDownloadMode('resume')}>{t.resumeDownload}</button>
                    <button className={downloadMode === 'overwrite' ? 'active' : ''} disabled={status.downloading} onClick={() => setDownloadMode('overwrite')}>{t.overwriteDownload}</button>
                  </div>
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
                    {' - '}{t.completed} {downloadProgress.doneFiles || 0}/{downloadProgress.totalFiles || 0} {t.files}
                    {(downloadProgress.skippedFiles || 0) > 0 && ` - ${t.skipped} ${downloadProgress.skippedFiles}`}
                    {(downloadProgress.resumedFiles || 0) > 0 && ` - ${t.resumed} ${downloadProgress.resumedFiles}`}
                    {(downloadProgress.failedFiles || 0) > 0 && ` - ${t.failed} ${downloadProgress.failedFiles}`}
                    {(downloadProgress.totalDirs || 0) > 0 && ` - ${t.dir} ${downloadProgress.doneDirs || 0}/${downloadProgress.totalDirs}`}
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
              <Metric label={t.p2pStatus} value={(mode === 'receive' ? receiveP2PReport?.status : latestReport(p2pSessions)?.status) || (currentRunning ? 'starting' : 'idle')} />
              <Metric label={t.peer} value={(mode === 'receive' ? receiveP2PReport?.peer : latestReport(p2pSessions)?.peer) || '-'} />
              <Metric label={t.network} value={(mode === 'receive' ? receiveP2PReport?.network : latestReport(p2pSessions)?.network) || '-'} />
              <Metric label={t.connectionRoute} value={routeLabel((mode === 'receive' ? receiveP2PReport?.mode : latestReport(p2pSessions)?.mode) || '', t)} />
              <Metric label={t.speed} value={formatRate(activeSpeed)} />
            </section>
            <section className="log-pane">
            <div className="log-header">
              <h3>{t.activity}</h3>
              <div className="button-row">
                <button className="ghost" disabled={logs.length === 0} onClick={copyLogs}>{t.copyLogs}</button>
                <button className="ghost" onClick={() => setLogs([])}>{t.clear}</button>
              </div>
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
      {qrDataUrl && (
        <div className="qr-backdrop" role="presentation" onClick={closePasswordQr}>
          <section className="qr-dialog" role="dialog" aria-modal="true" aria-label={t.qr} onClick={(event) => event.stopPropagation()}>
            <h2>{t.qr}</h2>
            <img src={qrDataUrl} alt={t.qr} />
            <div className="qr-password">{qrPassword}</div>
            <button className="primary" onClick={closePasswordQr}>{t.close}</button>
          </section>
        </div>
      )}
      {scanImage && (
        <div className="qr-backdrop" role="presentation" onClick={closeScreenScan}>
          <section className="scan-dialog" role="dialog" aria-modal="true" aria-label={t.scan} onClick={(event) => event.stopPropagation()}>
            <h2>{t.scanTitle}</h2>
            <p className="scan-hint">{t.scanHint}</p>
            <div
              className="scan-stage"
              onPointerDown={scanPointerDown}
              onPointerMove={scanPointerMove}
              onPointerUp={scanPointerUp}
            >
              <img ref={scanImgRef} src={scanImage} alt={t.scan} draggable={false} />
              {scanRect && (
                <div
                  className="scan-selection"
                  style={{left: scanRect.x, top: scanRect.y, width: scanRect.w, height: scanRect.h}}
                />
              )}
            </div>
            {scanError && <div className="scan-error">{scanError}</div>}
            <div className="scan-actions">
              <button className="secondary" disabled={scanBusy} onClick={startScreenScan}>{t.scanAgain}</button>
              <button className="secondary" disabled={scanBusy} onClick={decodeWholeScan}>{t.scanWhole}</button>
              <button className="primary" onClick={closeScreenScan}>{t.close}</button>
            </div>
          </section>
        </div>
      )}
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

function latestReport(reports: P2PReport[]) {
  return reports.reduce<P2PReport | null>((latest, report) => {
    if (!latest || report.timestamp > latest.timestamp) {
      return report;
    }
    return latest;
  }, null);
}

function routeLabel(modeValue: string, t: typeof text.zh) {
  const clean = modeValue.trim().toLowerCase();
  if (clean === 'p2p') {
    return t.directRoute;
  }
  if (clean === 'relay') {
    return t.relayRoute;
  }
  return '-';
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

