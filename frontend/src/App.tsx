import {useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent} from 'react';
import QRCode from 'qrcode';
import jsQR from 'jsqr';
import './App.css';
import {vpnprofile} from '../wailsjs/go/models';
import {
  CaptureScreen,
  ClearTaskbarProgress,
  GeneratePassword,
  IsAdministrator,
  LoadVPNProfiles,
  RemoteFiles,
  SaveVPNProfiles,
  SelectFiles,
  SelectFolder,
  StartHTTPDownload,
  StartTransfer,
  Status,
  StopHTTPDownload,
  StopTransfer,
  SetTaskbarProgress,
  UpdateSharePaths
} from '../wailsjs/go/main/App';
import {ClipboardGetText, EventsOff, EventsOn, OnFileDrop, OnFileDropOff, InitializeNotifications, IsNotificationAvailable, RequestNotificationAuthorization, SendNotification} from '../wailsjs/runtime/runtime';

type Mode = 'send' | 'receive' | 'vpnServer' | 'vpnClient';
type Lang = 'zh' | 'en';
type DownloadMode = 'resume' | 'overwrite';

type LogEvent = {
  type: string;
  level: string;
  message: string;
  time: string;
  mode?: string;
  inBytes?: number;
  outBytes?: number;
  localUrl?: string;
  inBps?: number;
  outBps?: number;
  peerIpv6?: string;
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
  vpnServerRunning: boolean;
  vpnClientRunning: boolean;
  localHTTPUrl: string;
  downloading: boolean;
  defaultSaveDir: string;
};

type VisibleEntry = RemoteFile & {
  synthetic?: boolean;
};

type VPNProfile = {
  name: string;
  passphrase: string;
  useUdp: boolean;
  routeIpv6: boolean;
  dnsServers: string;
  routeCidrs: string;
  linkConfig: string;
  extraArgs: string;
  tunnelOnly: boolean;
};

type VPNProfileStore = {
  version: number;
  selected: number;
  profiles: VPNProfile[];
};

type StatusTone = 'idle' | 'waiting' | 'connecting' | 'connected' | 'error';
type ConnectionStatus = {
  label: string;
  tone: StatusTone;
};

const appVersion = __APP_VERSION__;
const vpnProfileQrType = 'gonc.vpn.profile';
const defaultVpnDNS = '8.8.8.8\n2001:4860:4860::8888';
const defaultVpnRoutes = '0.0.0.0/1\n128.0.0.0/1\n::/0';
const privateLanRoutes = '10.0.0.0/8\n172.16.0.0/12\n192.168.0.0/16';

const text = {
  zh: {
    brand: 'Gonc',
    subtitle: '点对点安全传输工具',
    send: '发送文件',
    receive: '接收文件',
    vpnServer: 'VPN 服务端',
    vpnClient: 'VPN 客户端',
    running: '运行中',
    idle: '空闲',
    sender: '发送方',
    receiver: '接收方',
    sendTitle: '发送文件',
    receiveTitle: '接收文件',
    vpnServerTitle: 'VPN 服务端',
    stop: '停止',
    start: '开始',
    startShare: '开始分享',
    startReceive: '连接对方',
    startVpnServer: '启动 VPN 服务端',
    startVpnClient: '连接 VPN',
    vpnConnectAdminPrompt: '将请求管理员权限',
    receiveAll: '接收全部',
    connectedReceivers: '已连接',
    connectingReceivers: '正在建立',
    connections: '连接',
    establishing: '建立中',
    negotiatingConnection: '建立安全连接中',
    waitingConnection: '等待连接',
    newConnection: '有新连接',
    connectionSuccess: '连接成功',
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
    sentTotal: '累计发送',
    passphrase: '口令',
    senderPassphrase: '口令（已为你生成高强度随机口令,建议直接使用。口令是连接安全的唯一凭据,请通过安全渠道分享给接收方）',
    passPlaceholder: '两端使用相同口令',
    senderPasswordHint: '口令相同即可连接，谁生成谁扫码都行。口令仅需分享给接收方。双方用口令哈希在公共 MQTT 服务器碰头,网络地址以口令 AES 加密后交换——该服务器看不到口令也解不出地址。随后建立点对点直连,数据不经中转;连接基于口令完成双向认证与密钥协商,TLS 加密、无需 CA 证书,杜绝中间人窃听篡改。',
    receiverPasswordHint: '口令相同即可连接，谁生成谁扫码都行。口令用于发现双方网络地址。双方用口令哈希在公共 MQTT 服务器碰头,网络地址以口令 AES 加密后交换——该服务器看不到口令也解不出地址。随后建立点对点直连,数据不经中转;连接基于口令完成双向认证与密钥协商,TLS 加密、无需 CA 证书,杜绝中间人窃听篡改。',
    vpnServerPasswordHint: '作为 linkagent 服务端运行，支持多个客户端同时连接。口令相同即可连接，建议随机生成并通过安全渠道分享给 VPN 客户端。',
    vpnClientPasswordHint: '连接远端 linkagent VPN 服务端。系统 VPN 需要管理员权限，会在连接时弹出 UAC 授权。',
    vpnProfile: '配置',
    vpnProfileDefaultName: '默认配置',
    vpnProfileNewName: '新配置',
    vpnProfileNew: '新增',
    vpnProfileDelete: '删除',
    vpnProfileImport: '截图导入',
    vpnProfileExport: '导出二维码',
    vpnProfileName: '名称',
    vpnProfileQr: '配置二维码',
    vpnProfileQrHint: '二维码包含完整 VPN 配置和口令。',
    vpnProfileInvalid: '这不是有效的 Gonc VPN 配置二维码。',
    vpnProfileImported: '已导入 VPN 配置',
    generate: '随机',
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
    advancedSettings: '高级设置',
    hideAdvancedSettings: '收起高级设置',
    upstreamProxy: '上游代理节点',
    upstreamProxyPlaceholder: '例如 socks5://127.0.0.1:1080',
    upstreamProxyHint: '为空则直接使用本机网络出口；填写后代理客户端流量从该上游节点出口。',
    dnsForward: 'DNS 转发',
    dnsForwardPlaceholder: '例如 8.8.8.8:53',
    dnsForwardHint: '为空则不改写 DNS；填写后客户端 UDP:53 会转为 TCP DNS 转发到该服务器。如果设置的上游代理不支持 UDP，这里必须填写，否则客户端将无法 DNS。',
    extraArgs: '额外 gonc 参数',
    extraArgsPlaceholder: '例如 -x socks5://host:port',
    extraArgsHint: '追加到外层 gonc 命令，适合临时使用高级参数。',
    routeIpv6: 'Route IPv6',
    peerIpv6: '对端 IPv6 出口',
    peerIpv6Disabled: '未启用',
    peerIpv6Waiting: '等待 P2P',
    peerIpv6Checking: '检测中',
    peerIpv6Available: '可用',
    peerIpv6Unavailable: '不可用',
    tunnelOnly: '仅 SOCKS5 隧道',
    tunnelOnlyHint: '只建立本地 SOCKS5 隧道，不修改系统路由。',
    vpnDnsServers: 'DNS',
    vpnDnsServersPlaceholder: '每行一个 DNS 服务器；留空会使用 Google DNS。',
    routeCidrs: '路由 CIDR',
    routeCidrsPlaceholder: '留空为全局：0.0.0.0/1 和 128.0.0.0/1',
    routeFillGlobal: '全局路由',
    routeFillPrivate: '常见内网',
    linkConfig: 'SOCKS5 入口',
    linkConfigPlaceholder: '留空自动选择本地端口',
    remoteFiles: '对方分享的文件',
    refresh: '刷新',
    refreshing: '刷新中',
    stopDownload: '停止下载',
    downloadSelected: '下载选中',
    downloadMode: '下载方式',
    resumeDownload: '续传',
    overwriteDownload: '覆盖',
    noSelection: '请先勾选要下载的文件或目录。',
    noList: '尚未读取目录',
    remoteListAutoLoadFailed: '自动读取文件列表失败：',
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
    modifiedTime: '修改时间',
    vpnTunnelPausedTitle: 'VPN 隧道已断开',
    vpnTunnelPausedBody: '正在等待隧道重连，系统路由已临时暂停。',
    shareUpdateFailed: '更新分享列表失败。',
    weakPassword: '口令强度不足。请使用至少 8 位，并同时包含字母和数字的口令。',
  },
  en: {
    brand: 'Gonc',
    subtitle: 'Secure peer-to-peer transfer tool',
    send: 'Send Files',
    receive: 'Receive Files',
    vpnServer: 'VPN Server',
    vpnClient: 'VPN Client',
    running: 'Running',
    idle: 'Idle',
    sender: 'Sender',
    receiver: 'Receiver',
    sendTitle: 'Send files',
    receiveTitle: 'Receive files',
    vpnServerTitle: 'VPN Server',
    stop: 'Stop',
    start: 'Start',
    startShare: 'Start Sharing',
    startReceive: 'Connect',
    startVpnServer: 'Start VPN Server',
    startVpnClient: 'Connect VPN',
    vpnConnectAdminPrompt: 'Administrator permission required',
    receiveAll: 'Receive All',
    connectedReceivers: 'Connected',
    connectingReceivers: 'Establishing',
    connections: 'Connections',
    establishing: 'Establishing',
    negotiatingConnection: 'Negotiating secure connection',
    waitingConnection: 'Waiting',
    newConnection: 'New Connection',
    connectionSuccess: 'Connection established',
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
    sentTotal: 'Sent total',
    passphrase: 'Passphrase',
    senderPassphrase: 'Passphrase (a high-strength random passphrase has been generated for you; using it directly is recommended. This is the only credential for connection security, so share it with the receiver through a secure channel)',
    passPlaceholder: 'Same passphrase on both sides',
    senderPasswordHint: 'The same passphrase is all you need to connect; either side can generate it or scan the QR. Share the passphrase only with the receiver. Both sides meet on the public MQTT server using a passphrase hash, and exchange network addresses encrypted with passphrase-derived AES, so the server cannot see the passphrase or decrypt the addresses. A direct peer-to-peer connection is then established; data is not relayed. The connection uses the passphrase for mutual authentication and key negotiation, with TLS encryption and no CA certificate required, preventing man-in-the-middle eavesdropping or tampering.',
    receiverPasswordHint: 'The same passphrase is all you need to connect; either side can generate it or scan the QR. The passphrase is used to discover each side\'s network address. Both sides meet on the public MQTT server using a passphrase hash, and exchange network addresses encrypted with passphrase-derived AES, so the server cannot see the passphrase or decrypt the addresses. A direct peer-to-peer connection is then established; data is not relayed. The connection uses the passphrase for mutual authentication and key negotiation, with TLS encryption and no CA certificate required, preventing man-in-the-middle eavesdropping or tampering.',
    vpnServerPasswordHint: 'Run as a linkagent server and allow multiple VPN clients to connect. Use the same passphrase on the client; generating a random one and sharing it securely is recommended.',
    vpnClientPasswordHint: 'Connect to a remote linkagent VPN server. System VPN requires administrator permission and will show a UAC prompt when connecting.',
    vpnProfile: 'Profile',
    vpnProfileDefaultName: 'Default',
    vpnProfileNewName: 'New profile',
    vpnProfileNew: 'New',
    vpnProfileDelete: 'Delete',
    vpnProfileImport: 'Screenshot Import',
    vpnProfileExport: 'Export QR',
    vpnProfileName: 'Name',
    vpnProfileQr: 'Profile QR',
    vpnProfileQrHint: 'The QR code contains the full VPN profile and passphrase.',
    vpnProfileInvalid: 'This is not a valid Gonc VPN profile QR code.',
    vpnProfileImported: 'Imported VPN profile',
    generate: 'Random',
    copy: 'Copy',
    copyLogs: 'Copy Logs',
    qr: 'QR',
    scan: 'Screenshot Scan',
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
    advancedSettings: 'Advanced Settings',
    hideAdvancedSettings: 'Hide Advanced Settings',
    upstreamProxy: 'Upstream Proxy',
    upstreamProxyPlaceholder: 'e.g. socks5://127.0.0.1:1080',
    upstreamProxyHint: 'Blank uses this machine as the network exit. Set one to route client traffic through the upstream proxy.',
    dnsForward: 'DNS Forwarding',
    dnsForwardPlaceholder: 'e.g. 8.8.8.8:53',
    dnsForwardHint: 'Blank leaves DNS unchanged. Set one to forward client UDP:53 as TCP DNS to this server. If the upstream proxy does not support UDP, this must be set or clients will not be able to resolve DNS.',
    extraArgs: 'Extra gonc Args',
    extraArgsPlaceholder: 'e.g. -x socks5://host:port',
    extraArgsHint: 'Appended to the outer gonc command for temporary advanced options.',
    routeIpv6: 'Route IPv6',
    peerIpv6: 'Peer IPv6 exit',
    peerIpv6Disabled: 'Not enabled',
    peerIpv6Waiting: 'Waiting for P2P',
    peerIpv6Checking: 'Checking',
    peerIpv6Available: 'Available',
    peerIpv6Unavailable: 'Unavailable',
    tunnelOnly: 'Tunnel only',
    tunnelOnlyHint: 'Only create the local SOCKS5 tunnel without changing system routes.',
    vpnDnsServers: 'DNS',
    vpnDnsServersPlaceholder: 'One DNS server per line. Leave blank to use Google DNS.',
    routeCidrs: 'Route CIDR',
    routeCidrsPlaceholder: 'Blank means global: 0.0.0.0/1 and 128.0.0.0/1',
    routeFillGlobal: 'Global routes',
    routeFillPrivate: 'Private LANs',
    linkConfig: 'SOCKS5 Entry',
    linkConfigPlaceholder: 'Blank picks a local port automatically',
    remoteFiles: 'Peer Shared Files',
    refresh: 'Refresh',
    refreshing: 'Refreshing',
    stopDownload: 'Stop Download',
    downloadSelected: 'Download Selected',
    downloadMode: 'Download Mode',
    resumeDownload: 'Resume',
    overwriteDownload: 'Overwrite',
    noSelection: 'Select files or folders to download first.',
    noList: 'No list loaded',
    remoteListAutoLoadFailed: 'Automatic file list load failed:',
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
    modifiedTime: 'Modified',
    vpnTunnelPausedTitle: 'VPN tunnel disconnected',
    vpnTunnelPausedBody: 'Waiting for the tunnel to reconnect. System routes are paused temporarily.',
    shareUpdateFailed: 'Failed to update shared list.',
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
  const [vpnServerPassword, setVpnServerPassword] = useState('');
  const [vpnClientPassword, setVpnClientPassword] = useState('');
  const [sharePaths, setSharePaths] = useState<string[]>([]);
  const [saveDir, setSaveDir] = useState('');
  const [currentRemotePath, setCurrentRemotePath] = useState('/');
  const [useUDP, setUseUDP] = useState(false);
  const [vpnServerUseUDP, setVpnServerUseUDP] = useState(false);
  const [vpnServerAdvanced, setVpnServerAdvanced] = useState(false);
  const [vpnServerUpstream, setVpnServerUpstream] = useState('');
  const [vpnServerDNSForward, setVpnServerDNSForward] = useState('');
  const [vpnServerExtraArgs, setVpnServerExtraArgs] = useState('');
  const [vpnClientUseUDP, setVpnClientUseUDP] = useState(false);
  const [vpnClientAdvanced, setVpnClientAdvanced] = useState(false);
  const [vpnClientEnableIPv6, setVpnClientEnableIPv6] = useState(false);
  const [vpnClientTunnelOnly, setVpnClientTunnelOnly] = useState(false);
  const [vpnClientDNSServers, setVpnClientDNSServers] = useState('');
  const [vpnClientRouteCIDRs, setVpnClientRouteCIDRs] = useState('');
  const [vpnClientLinkConfig, setVpnClientLinkConfig] = useState('');
  const [vpnClientExtraArgs, setVpnClientExtraArgs] = useState('');
  const [vpnProfiles, setVpnProfiles] = useState<VPNProfile[]>([]);
  const [selectedVpnProfile, setSelectedVpnProfile] = useState(0);
  const [status, setStatus] = useState<AppStatus>({running: false, sendRunning: false, receiveRunning: false, vpnServerRunning: false, vpnClientRunning: false, localHTTPUrl: '', downloading: false, defaultSaveDir: ''});
  const [logs, setLogs] = useState<LogEvent[]>([]);
  const [error, setError] = useState('');
  const [receiveP2PReport, setReceiveP2PReport] = useState<P2PReport | null>(null);
  const [sendP2PReports, setSendP2PReports] = useState<Record<string, P2PReport>>({});
  const [vpnServerP2PReports, setVpnServerP2PReports] = useState<Record<string, P2PReport>>({});
  const [vpnClientP2PReport, setVpnClientP2PReport] = useState<P2PReport | null>(null);
  const [remoteList, setRemoteList] = useState<RemoteList | null>(null);
  const [remoteListLoading, setRemoteListLoading] = useState(false);
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [downloadProgress, setDownloadProgress] = useState<DownloadEvent | null>(null);
  const [downloadMode, setDownloadMode] = useState<DownloadMode>('resume');
  const [sendTraffic, setSendTraffic] = useState<LogEvent | null>(null);
  const [receiveTraffic, setReceiveTraffic] = useState<LogEvent | null>(null);
  const [vpnServerTraffic, setVpnServerTraffic] = useState<LogEvent | null>(null);
  const [vpnClientTraffic, setVpnClientTraffic] = useState<LogEvent | null>(null);
  const [vpnClientPeerIPv6, setVpnClientPeerIPv6] = useState('disabled');
  const [passwordVisible, setPasswordVisible] = useState(false);
  const [qrDataUrl, setQrDataUrl] = useState('');
  const [qrPassword, setQrPassword] = useState('');
  const [qrTitle, setQrTitle] = useState('');
  const [qrHint, setQrHint] = useState('');
  const [scanImage, setScanImage] = useState('');
  const [scanPurpose, setScanPurpose] = useState<'password' | 'vpnProfile'>('password');
  const [scanBusy, setScanBusy] = useState(false);
  const [scanError, setScanError] = useState('');
  const [scanRect, setScanRect] = useState<{x: number; y: number; w: number; h: number} | null>(null);
  const [isAdministrator, setIsAdministrator] = useState(false);
  const scanImgRef = useRef<HTMLImageElement | null>(null);
  const scanDragStart = useRef<{x: number; y: number} | null>(null);
  const notificationsReady = useRef(false);
  const vpnDisconnectNotified = useRef(false);
  const [nowTick, setNowTick] = useState(Date.now());
  const passwordTimer = useRef<number | null>(null);
  const activePassword = mode === 'send' ? sendPassword : (mode === 'receive' ? receivePassword : (mode === 'vpnServer' ? vpnServerPassword : vpnClientPassword));

  const visibleEntries = useMemo(() => shallowEntries(remoteList?.files || [], currentRemotePath), [remoteList, currentRemotePath]);
  const activeSpeed = Math.max(
    freshSpeed(downloadProgress?.time, downloadProgress?.bytesPerSecond, nowTick),
    freshSpeed(receiveTraffic?.time, receiveTraffic?.inBps, nowTick),
    freshSpeed(receiveTraffic?.time, receiveTraffic?.outBps, nowTick)
  );
  const sendRunning = status.sendRunning;
  const receiveRunning = status.receiveRunning;
  const vpnServerRunning = status.vpnServerRunning;
  const vpnClientRunning = status.vpnClientRunning;
  const currentRunning = mode === 'send' ? sendRunning : (mode === 'receive' ? receiveRunning : (mode === 'vpnServer' ? vpnServerRunning : vpnClientRunning));
  const canStart = !currentRunning && activePassword.trim().length > 0 && (mode !== 'send' || sharePaths.length > 0);
  const canDownload = Boolean(mode === 'receive' && status.localHTTPUrl && saveDir && selectedPaths.size > 0 && !status.downloading);
  const canDownloadAll = Boolean(mode === 'receive' && status.localHTTPUrl && saveDir && remoteList && visibleEntries.length > 0 && !status.downloading);
  const primaryLabel = mode === 'send' ? t.startShare : (mode === 'receive' ? t.startReceive : (mode === 'vpnServer' ? t.startVpnServer : t.startVpnClient));
  const showVpnAdminPrompt = mode === 'vpnClient' && !vpnClientTunnelOnly && !isAdministrator;
  const p2pSessions = useMemo(() => Object.values(sendP2PReports), [sendP2PReports]);
  const vpnServerSessions = useMemo(() => Object.values(vpnServerP2PReports), [vpnServerP2PReports]);
  const latestSendReport = latestReport(p2pSessions);
  const latestVpnServerReport = latestReport(vpnServerSessions);
  const connectedCount = p2pSessions.filter((report) => report.topic && report.status === 'connected').length;
  const sendStatus = multiClientActivityStatus(latestSendReport, p2pSessions, sendRunning, t);
  const transferSpeed = mode === 'send'
    ? freshSpeed(sendTraffic?.time, sendTraffic?.outBps, nowTick)
    : (mode === 'vpnServer' ? freshSpeed(vpnServerTraffic?.time, vpnServerTraffic?.outBps, nowTick) : (mode === 'vpnClient' ? Math.max(freshSpeed(vpnClientTraffic?.time, vpnClientTraffic?.inBps, nowTick), freshSpeed(vpnClientTraffic?.time, vpnClientTraffic?.outBps, nowTick)) : activeSpeed));
  const sendTotalBytes = sendTraffic?.outBytes || 0;
  const receiveStatus = receiveConnectionStatus(receiveP2PReport, receiveRunning, Boolean(status.localHTTPUrl), t);
  const vpnClientStatus = receiveConnectionStatus(vpnClientP2PReport, vpnClientRunning, false, t);
  const vpnServerConnectedCount = vpnServerSessions.filter((report) => report.topic && report.status === 'connected').length;
  const vpnServerStatus = multiClientActivityStatus(latestVpnServerReport, vpnServerSessions, vpnServerRunning, t);
  const statusTone = mode === 'receive' ? receiveStatus.tone : (mode === 'send' ? sendStatus.tone : (mode === 'vpnServer' ? vpnServerStatus.tone : vpnClientStatus.tone));
  const activeP2PReport = mode === 'receive'
    ? receiveP2PReport
    : (mode === 'vpnServer' ? latestVpnServerReport : (mode === 'vpnClient' ? vpnClientP2PReport : latestSendReport));

  useEffect(() => {
    const timer = window.setInterval(() => setNowTick(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    let cancelled = false;
    Promise.all([GeneratePassword(), GeneratePassword()])
      .then(([sendValue, vpnServerValue]) => {
        if (!cancelled) {
          setSendPassword((current) => current || sendValue);
          setVpnServerPassword((current) => current || vpnServerValue);
        }
      })
      .catch((err) => setError(localizeError(String(err))));
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    LoadVPNProfiles()
      .then((store: VPNProfileStore) => {
        const profiles = normalizeVpnProfiles(store.profiles, t);
        const selected = clampIndex(store.selected, profiles.length);
        setVpnProfiles(profiles);
        setSelectedVpnProfile(selected);
        applyVpnProfile(profiles[selected]);
      })
      .catch((err) => {
        setError(localizeError(String(err)));
        const fallback = [defaultVpnProfile(t.vpnProfileDefaultName)];
        setVpnProfiles(fallback);
        setSelectedVpnProfile(0);
        applyVpnProfile(fallback[0]);
      });
  }, []);

  useEffect(() => {
    IsAdministrator()
      .then(setIsAdministrator)
      .catch(() => setIsAdministrator(false));
  }, []);

  useEffect(() => {
    let cancelled = false;
    InitializeNotifications()
      .then(() => IsNotificationAvailable())
      .then(async (available) => {
        if (!available || cancelled) {
          return;
        }
        notificationsReady.current = await RequestNotificationAuthorization().catch(() => true);
      })
      .catch(() => {
        notificationsReady.current = false;
      });
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
        } else if (event.mode === 'vpnServer') {
          setVpnServerTraffic(event);
        } else if (event.mode === 'vpnClient') {
          setVpnClientTraffic(event);
        }
        return;
      }
      if (event.type === 'peer_ipv6' && event.mode === 'vpnClient') {
        setVpnClientPeerIPv6(event.peerIpv6 || event.message || '');
      }
      if (event.mode === 'vpnClient') {
        if (event.message.includes('pausing Windows VPN routes')) {
          notifyVpnTunnelPaused();
        } else if (event.message.includes('Windows VPN started') || event.message.includes('Windows VPN routes restored')) {
          vpnDisconnectNotified.current = false;
        }
      }
      setLogs((current) => [...current.slice(-399), event]);
      if (event.mode === 'receive' && event.localUrl) {
        setStatus((current) => ({...current, localHTTPUrl: event.localUrl || current.localHTTPUrl}));
        window.setTimeout(() => loadRemoteFiles('/', true), 700);
      }
      if (event.type === 'status' || event.type === 'local_http') {
        if (event.message.includes('stopped') || event.message.includes('finished')) {
          if (event.mode === 'receive') {
            setReceiveP2PReport((current) => current ? {...current, status: event.message.includes('stopped') ? 'stopped' : 'finished'} : null);
          } else if (event.mode === 'vpnServer') {
            setVpnServerP2PReports((current) => {
              const next: Record<string, P2PReport> = {};
              for (const [key, value] of Object.entries(current)) {
                next[key] = {...value, status: event.message.includes('stopped') ? 'stopped' : 'finished'};
              }
              return next;
            });
          } else if (event.mode === 'vpnClient') {
            setVpnClientP2PReport((current) => current ? {...current, status: event.message.includes('stopped') ? 'stopped' : 'finished'} : null);
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
      } else if (report.side === 'vpnServer') {
        setVpnServerP2PReports((current) => ({...current, [p2pSessionKey(report)]: report}));
      } else if (report.side === 'vpnClient') {
        setVpnClientP2PReport(report);
      }
    });
    EventsOn('download:event', (event: DownloadEvent) => {
      if (event.type === 'progress') {
        setDownloadProgress(event);
        SetTaskbarProgress(event.doneBytes || 0, event.totalBytes || 0).catch(() => undefined);
      } else {
        setLogs((current) => [...current.slice(-399), event]);
      }
      if (event.type === 'status') {
        if (event.message.includes('download complete') || event.message.includes('download finished') || event.level === 'error') {
          ClearTaskbarProgress().catch(() => undefined);
        }
        refreshStatus();
      }
    });
    OnFileDrop((_x, _y, paths) => {
      if (mode === 'send') {
        appendSharePaths(paths);
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
  }, [mode]);

  useEffect(() => {
    if (!sendRunning) {
      return;
    }
    if (sharePaths.length === 0) {
      return;
    }
    UpdateSharePaths(sharePaths).catch((err) => {
      setError(`${t.shareUpdateFailed} ${localizeError(String(err))}`);
    });
  }, [sendRunning, sharePaths, t.shareUpdateFailed]);

  async function refreshStatus() {
    try {
      const next = await Status();
      setStatus({
        running: next.running,
        sendRunning: next.sendRunning,
        receiveRunning: next.receiveRunning,
        vpnServerRunning: next.vpnServerRunning,
        vpnClientRunning: next.vpnClientRunning,
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

  async function generateVpnServerPassword() {
    setError('');
    setVpnServerPassword(await GeneratePassword());
    revealPasswordTemporarily();
  }

  async function generateVpnClientPassword() {
    setError('');
    const value = await GeneratePassword();
    setVpnProfileField('passphrase', value);
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
      if (mode === 'vpnServer') {
        setVpnServerPassword(value.trim());
      } else if (mode === 'vpnClient') {
        setVpnProfileField('passphrase', value.trim());
      } else {
        setReceivePassword(value.trim());
      }
      revealPasswordTemporarily();
    } catch (err) {
      try {
        const value = await navigator.clipboard.readText();
        if (mode === 'vpnServer') {
          setVpnServerPassword(value.trim());
        } else if (mode === 'vpnClient') {
          setVpnProfileField('passphrase', value.trim());
        } else {
          setReceivePassword(value.trim());
        }
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
      setQrTitle(t.qr);
      setQrHint('');
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
    setQrTitle('');
    setQrHint('');
  }

  async function showVpnProfileQr() {
    const profile = normalizeVpnProfile(vpnProfiles[selectedVpnProfile] || currentVpnProfileFromState(t), t);
    const payload = JSON.stringify({type: vpnProfileQrType, profile});
    setError('');
    try {
      setQrPassword(profile.name || t.vpnProfileDefaultName);
      setQrTitle(t.vpnProfileQr);
      setQrHint(t.vpnProfileQrHint);
      setQrDataUrl(await QRCode.toDataURL(payload, {
        width: 280,
        margin: 2,
        color: {
          dark: '#101826',
          light: '#ffffff',
        },
      }));
      updateCurrentVpnProfile(profile);
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  async function startScreenScan(purpose: 'password' | 'vpnProfile' = 'password') {
    setError('');
    setScanError('');
    setScanRect(null);
    setScanPurpose(purpose);
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
        if (scanPurpose === 'vpnProfile') {
          if (!importVpnProfileFromQr(decoded)) {
            return;
          }
        } else if (mode === 'send') {
          setSendPassword(decoded);
        } else if (mode === 'vpnServer') {
          setVpnServerPassword(decoded);
        } else if (mode === 'vpnClient') {
          setVpnProfileField('passphrase', decoded);
        } else {
          setReceivePassword(decoded);
        }
        revealPasswordTemporarily();
        closeScreenScan();
        if (scanPurpose !== 'vpnProfile') {
          appendLog('status', 'info', t.scanSuccess);
        }
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
    } else if (mode === 'receive') {
      setReceiveP2PReport(null);
      setRemoteList(null);
      setRemoteListLoading(false);
      setSelectedPaths(new Set());
      setDownloadProgress(null);
      ClearTaskbarProgress().catch(() => undefined);
      setReceiveTraffic(null);
      setCurrentRemotePath('/');
    } else if (mode === 'vpnServer') {
      setVpnServerP2PReports({});
      setVpnServerTraffic(null);
    } else {
      setVpnClientP2PReport(null);
      setVpnClientTraffic(null);
      setVpnClientPeerIPv6(vpnClientEnableIPv6 && !vpnClientTunnelOnly ? 'waiting' : 'disabled');
      vpnDisconnectNotified.current = false;
    }
    try {
      await StartTransfer({
        mode,
        password: passphrase,
        sharePaths,
        saveDir,
        downloadSubPath: currentRemotePath,
        useUDP: mode === 'vpnServer' ? vpnServerUseUDP : (mode === 'vpnClient' ? vpnClientUseUDP : useUDP),
        upstream: vpnServerUpstream,
        dnsForward: vpnServerDNSForward,
        dnsServers: vpnClientDNSServers,
        routeCidrs: vpnClientRouteCIDRs,
        linkConfig: vpnClientLinkConfig,
        enableIpv6: vpnClientEnableIPv6,
        tunnelOnly: vpnClientTunnelOnly,
        extraArgs: mode === 'vpnClient' ? vpnClientExtraArgs : vpnServerExtraArgs
      });
      if (mode === 'vpnServer') {
        setVpnServerAdvanced(false);
      } else if (mode === 'vpnClient') {
        setVpnClientAdvanced(false);
      }
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
      } else if (mode === 'receive') {
        setReceiveP2PReport((current) => current ? {...current, status: 'stopped'} : null);
        setReceiveTraffic(null);
        setRemoteListLoading(false);
        ClearTaskbarProgress().catch(() => undefined);
      } else {
        if (mode === 'vpnServer') {
          setVpnServerP2PReports((current) => {
            const next: Record<string, P2PReport> = {};
            for (const [key, value] of Object.entries(current)) {
              next[key] = {...value, status: 'stopped'};
            }
            return next;
          });
          setVpnServerTraffic(null);
        } else {
          setVpnClientP2PReport((current) => current ? {...current, status: 'stopped'} : null);
          setVpnClientTraffic(null);
        }
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
    setRemoteListLoading(true);
    try {
      const normalized = normalizeRemotePath(path);
      const list = await RemoteFiles(normalized);
      setCurrentRemotePath(normalized);
      setSelectedPaths(new Set());
      setRemoteList(list);
    } catch (err) {
      if (!silent) {
        setError(localizeError(String(err)));
      } else {
        appendLog('status', 'warn', `${t.remoteListAutoLoadFailed} ${localizeError(String(err))}`);
      }
    } finally {
      setRemoteListLoading(false);
    }
  }

  function applyVpnProfile(profile: VPNProfile) {
    setVpnClientPassword(profile.passphrase || '');
    setVpnClientUseUDP(Boolean(profile.useUdp));
    setVpnClientEnableIPv6(Boolean(profile.routeIpv6));
    setVpnClientTunnelOnly(Boolean(profile.tunnelOnly));
    setVpnClientDNSServers(profile.dnsServers || defaultVpnDNS);
    setVpnClientRouteCIDRs(profile.routeCidrs || defaultVpnRoutes);
    setVpnClientLinkConfig(profile.linkConfig || '');
    setVpnClientExtraArgs(profile.extraArgs || '');
  }

  function persistVpnProfiles(nextProfiles: VPNProfile[], selected: number) {
    SaveVPNProfiles(vpnprofile.Store.createFrom({version: 1, selected, profiles: nextProfiles}))
      .catch((err: unknown) => setError(localizeError(String(err))));
  }

  function updateCurrentVpnProfile(patch: Partial<VPNProfile>) {
    setVpnProfiles((current) => {
      const base = normalizeVpnProfiles(current, t);
      const selected = clampIndex(selectedVpnProfile, base.length);
      const next = base.map((profile, index) => index === selected ? normalizeVpnProfile({...profile, ...patch}, t) : profile);
      persistVpnProfiles(next, selected);
      return next;
    });
  }

  function selectVpnProfile(index: number) {
    const selected = clampIndex(index, vpnProfiles.length);
    setSelectedVpnProfile(selected);
    if (vpnProfiles[selected]) {
      applyVpnProfile(vpnProfiles[selected]);
      persistVpnProfiles(vpnProfiles, selected);
    }
  }

  function setVpnProfileField<K extends keyof VPNProfile>(key: K, value: VPNProfile[K]) {
    updateCurrentVpnProfile({[key]: value} as Partial<VPNProfile>);
    if (key === 'name') {
      return;
    }
    switch (key) {
      case 'passphrase':
        setVpnClientPassword(String(value));
        break;
      case 'useUdp':
        setVpnClientUseUDP(Boolean(value));
        break;
      case 'routeIpv6':
        setVpnClientEnableIPv6(Boolean(value));
        break;
      case 'tunnelOnly':
        setVpnClientTunnelOnly(Boolean(value));
        break;
      case 'dnsServers':
        setVpnClientDNSServers(String(value));
        break;
      case 'routeCidrs':
        setVpnClientRouteCIDRs(String(value));
        break;
      case 'linkConfig':
        setVpnClientLinkConfig(String(value));
        break;
      case 'extraArgs':
        setVpnClientExtraArgs(String(value));
        break;
    }
  }

  function addVpnProfile() {
    const nextProfile = defaultVpnProfile(uniqueVpnProfileName(vpnProfiles, t.vpnProfileNewName));
    const next = [...normalizeVpnProfiles(vpnProfiles, t), nextProfile];
    const selected = next.length - 1;
    setVpnProfiles(next);
    setSelectedVpnProfile(selected);
    applyVpnProfile(nextProfile);
    persistVpnProfiles(next, selected);
  }

  function deleteVpnProfile() {
    const current = normalizeVpnProfiles(vpnProfiles, t);
    let next = current.filter((_profile, index) => index !== selectedVpnProfile);
    if (next.length === 0) {
      next = [defaultVpnProfile(t.vpnProfileDefaultName)];
    }
    const selected = clampIndex(selectedVpnProfile, next.length);
    setVpnProfiles(next);
    setSelectedVpnProfile(selected);
    applyVpnProfile(next[selected]);
    persistVpnProfiles(next, selected);
  }

  function importVpnProfileFromQr(value: string) {
    try {
      const parsed = JSON.parse(value);
      if (!parsed || parsed.type !== vpnProfileQrType || !parsed.profile) {
        throw new Error(t.vpnProfileInvalid);
      }
      const imported = normalizeVpnProfile(parsed.profile as VPNProfile, t);
      imported.name = uniqueVpnProfileName(vpnProfiles, imported.name || t.vpnProfileDefaultName);
      const next = [...normalizeVpnProfiles(vpnProfiles, t), imported];
      const selected = next.length - 1;
      setVpnProfiles(next);
      setSelectedVpnProfile(selected);
      applyVpnProfile(imported);
      persistVpnProfiles(next, selected);
      appendLog('status', 'info', `${t.vpnProfileImported}: ${imported.name}`);
      return true;
    } catch {
      setScanError(t.vpnProfileInvalid);
      return false;
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
      await ClearTaskbarProgress();
      await refreshStatus();
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  function notifyVpnTunnelPaused() {
    if (vpnDisconnectNotified.current || !notificationsReady.current) {
      return;
    }
    vpnDisconnectNotified.current = true;
    SendNotification({
      id: `vpn-tunnel-paused-${Date.now()}`,
      title: t.vpnTunnelPausedTitle,
      body: t.vpnTunnelPausedBody,
    }).catch(() => undefined);
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
        {(mode === 'send' || mode === 'vpnServer' || mode === 'vpnClient') && (
          <div className={`status-block ${statusTone}`}>
            <span className={`dot ${statusTone}`} />
            <span>{mode === 'send' ? sendStatus.label : (mode === 'vpnServer' ? vpnServerStatus.label : vpnClientStatus.label)}</span>
            {mode !== 'vpnClient' && (
              <>
                <span className="status-divider" />
                <span>{t.connectedShort} {mode === 'send' ? connectedCount : vpnServerConnectedCount}</span>
              </>
            )}
            <span className="status-divider" />
            <span>{formatRate(transferSpeed)}</span>
          </div>
        )}
        </header>

        <div className="mode-switch" role="tablist" aria-label="Transfer mode">
          <button className={mode === 'send' ? 'active' : ''} onClick={() => setMode('send')}>{t.send}</button>
          <button className={mode === 'receive' ? 'active' : ''} onClick={() => setMode('receive')}>{t.receive}</button>
          <button className={mode === 'vpnClient' ? 'active' : ''} onClick={() => setMode('vpnClient')}>{t.vpnClient}</button>
          <button className={mode === 'vpnServer' ? 'active' : ''} onClick={() => setMode('vpnServer')}>{t.vpnServer}</button>
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
                    <button className="primary-light" onClick={addFiles}>{t.addFiles}</button>
                    <button className="secondary" onClick={addFolder}>{t.addFolder}</button>
                  </div>
                  <div className="drop-zone">
                    <div className="path-list">
                      {sharePaths.length === 0 ? (
                        <p className="drop-hint">{t.dropHint}</p>
                      ) : sharePaths.map((path) => (
                        <div className="path-row" key={path}>
                          <span>{path}</span>
                          <button disabled={sendRunning && sharePaths.length <= 1} onClick={() => removeSharePath(path)} aria-label={`${t.remove} ${path}`}>{t.remove}</button>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              </>
            ) : mode === 'receive' ? (
              <>
                <div className="field">
                  <label>{t.saveDir}</label>
                  <div className="inline">
                    <input value={saveDir} onChange={(event) => setSaveDir(event.target.value)} placeholder={t.savePlaceholder} />
                    <button className="secondary" onClick={chooseSaveDir}>{t.choose}</button>
                  </div>
                </div>
              </>
            ) : null}

            {mode === 'vpnClient' && (
              <>
                <div className="field">
                  <label>{t.vpnProfile}</label>
                  <div className="inline profile-line">
                    <select
                      value={selectedVpnProfile}
                      disabled={vpnClientRunning}
                      onChange={(event) => selectVpnProfile(Number(event.target.value))}
                    >
                      {normalizeVpnProfiles(vpnProfiles, t).map((profile, index) => (
                        <option value={index} key={`${profile.name}-${index}`}>{profile.name}</option>
                      ))}
                    </select>
                    <button className="secondary" disabled={vpnClientRunning} onClick={addVpnProfile}>{t.vpnProfileNew}</button>
                    <button className="secondary" disabled={vpnClientRunning} onClick={() => startScreenScan('vpnProfile')}>{t.vpnProfileImport}</button>
                    <button className="secondary" disabled={vpnProfiles.length === 0} onClick={showVpnProfileQr}>{t.vpnProfileExport}</button>
                    <button className="secondary" disabled={vpnClientRunning} onClick={deleteVpnProfile}>{t.vpnProfileDelete}</button>
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
                  onChange={(event) => {
                    if (mode === 'send') {
                      setSendPassword(event.target.value);
                    } else if (mode === 'receive') {
                      setReceivePassword(event.target.value);
                    } else if (mode === 'vpnServer') {
                      setVpnServerPassword(event.target.value);
                    } else {
                      setVpnProfileField('passphrase', event.target.value);
                    }
                  }}
                  placeholder={t.passPlaceholder}
                  disabled={currentRunning}
                />
                {mode === 'send' ? (
                  <>
                    {!sendRunning && <button className="secondary" onClick={generatePassword}>{t.generate}</button>}
                    <button className="secondary" disabled={!sendPassword} onClick={copyPassword}>{t.copy}</button>
                    {!sendRunning && <button className="secondary" disabled={scanBusy} onClick={() => startScreenScan()}>{t.scan}</button>}
                    <button className="secondary" disabled={!sendPassword} onClick={showPasswordQr}>{t.qr}</button>
                  </>
                ) : mode === 'receive' ? (
                  <>
                    {!receiveRunning && <button className="secondary" onClick={generateReceivePassword}>{t.generate}</button>}
                    {!receiveRunning && <button className="secondary" onClick={pastePassword}>{t.paste}</button>}
                    {!receiveRunning && <button className="secondary" disabled={scanBusy} onClick={() => startScreenScan()}>{t.scan}</button>}
                    <button className="secondary" disabled={!receivePassword} onClick={showPasswordQr}>{t.qr}</button>
                  </>
                ) : mode === 'vpnServer' ? (
                  <>
                    {!vpnServerRunning && <button className="secondary" onClick={generateVpnServerPassword}>{t.generate}</button>}
                    {!vpnServerRunning && <button className="secondary" onClick={pastePassword}>{t.paste}</button>}
                    <button className="secondary" disabled={!vpnServerPassword} onClick={copyPassword}>{t.copy}</button>
                    {!vpnServerRunning && <button className="secondary" disabled={scanBusy} onClick={() => startScreenScan()}>{t.scan}</button>}
                    <button className="secondary" disabled={!vpnServerPassword} onClick={showPasswordQr}>{t.qr}</button>
                  </>
                ) : (
                  <>
                    {!vpnClientRunning && <button className="secondary" onClick={generateVpnClientPassword}>{t.generate}</button>}
                    {!vpnClientRunning && <button className="secondary" onClick={pastePassword}>{t.paste}</button>}
                    <button className="secondary" disabled={!vpnClientPassword} onClick={copyPassword}>{t.copy}</button>
                    {!vpnClientRunning && <button className="secondary" disabled={scanBusy} onClick={() => startScreenScan()}>{t.scan}</button>}
                    <button className="secondary" disabled={!vpnClientPassword} onClick={showPasswordQr}>{t.qr}</button>
                  </>
                )}
              </div>
              {mode === 'send' ? (
                <div className="field-hint">
                  <p>{t.senderPasswordHint}</p>
                </div>
              ) : mode === 'receive' ? (
                <div className="field-hint">
                  <p>{t.receiverPasswordHint}</p>
                </div>
              ) : mode === 'vpnServer' ? (
                <div className="field-hint">
                  <p>{t.vpnServerPasswordHint}</p>
                </div>
              ) : (
                <div className="field-hint">
                  <p>{t.vpnClientPasswordHint}</p>
                </div>
              )}
            </div>

            {(mode === 'send' || mode === 'receive') && (
              <label className="check">
                <input
                  type="checkbox"
                  checked={useUDP}
                  disabled={currentRunning}
                  onChange={(event) => setUseUDP(event.target.checked)}
                />
                <span>{t.useUDP}</span>
              </label>
            )}

            {mode === 'vpnClient' && (
              <section className="advanced-panel">
                <button className="secondary advanced-toggle" disabled={vpnClientRunning} onClick={() => setVpnClientAdvanced((value) => !value)}>
                  {vpnClientAdvanced ? t.hideAdvancedSettings : t.advancedSettings}
                </button>
                {vpnClientAdvanced && (
                  <div className="advanced-fields">
                    <div className="field">
                      <label>{t.vpnProfileName}</label>
                      <input
                        value={vpnProfiles[selectedVpnProfile]?.name || ''}
                        disabled={vpnClientRunning}
                        onChange={(event) => setVpnProfileField('name', event.target.value)}
                      />
                    </div>
                    <label className="check">
                      <input
                        type="checkbox"
                        checked={vpnClientUseUDP}
                        disabled={vpnClientRunning}
                        onChange={(event) => setVpnProfileField('useUdp', event.target.checked)}
                      />
                      <span>{t.useUDP}</span>
                    </label>
                    <label className="check">
                      <input
                        type="checkbox"
                        checked={vpnClientEnableIPv6}
                        disabled={vpnClientRunning || vpnClientTunnelOnly}
                        onChange={(event) => setVpnProfileField('routeIpv6', event.target.checked)}
                      />
                      <span>{t.routeIpv6}</span>
                    </label>
                    <div className="field">
                      <label className="check">
                        <input
                          type="checkbox"
                          checked={vpnClientTunnelOnly}
                          disabled={vpnClientRunning}
                          onChange={(event) => setVpnProfileField('tunnelOnly', event.target.checked)}
                        />
                        <span>{t.tunnelOnly}</span>
                      </label>
                      <div className="field-hint"><p>{t.tunnelOnlyHint}</p></div>
                    </div>
                    <div className="field">
                      <label>{t.linkConfig}</label>
                      <input
                        value={vpnClientLinkConfig}
                        disabled={vpnClientRunning}
                        onChange={(event) => setVpnProfileField('linkConfig', event.target.value)}
                        placeholder={t.linkConfigPlaceholder}
                      />
                    </div>
                    <div className="field">
                      <label>{t.vpnDnsServers}</label>
                      <textarea
                        value={vpnClientDNSServers}
                        disabled={vpnClientRunning || vpnClientTunnelOnly}
                        onChange={(event) => setVpnProfileField('dnsServers', event.target.value)}
                        placeholder={t.vpnDnsServersPlaceholder}
                      />
                    </div>
                    <div className="field">
                      <label>{t.routeCidrs}</label>
                      <textarea
                        value={vpnClientRouteCIDRs}
                        disabled={vpnClientRunning || vpnClientTunnelOnly}
                        onChange={(event) => setVpnProfileField('routeCidrs', event.target.value)}
                        placeholder={t.routeCidrsPlaceholder}
                      />
                      <div className="quiet-actions">
                        <button
                          type="button"
                          className="quiet-action"
                          disabled={vpnClientRunning || vpnClientTunnelOnly}
                          onClick={() => setVpnProfileField('routeCidrs', defaultVpnRoutes)}
                        >
                          {t.routeFillGlobal}
                        </button>
                        <button
                          type="button"
                          className="quiet-action"
                          disabled={vpnClientRunning || vpnClientTunnelOnly}
                          onClick={() => setVpnProfileField('routeCidrs', privateLanRoutes)}
                        >
                          {t.routeFillPrivate}
                        </button>
                      </div>
                    </div>
                    <div className="field">
                      <label>{t.extraArgs}</label>
                      <input
                        value={vpnClientExtraArgs}
                        disabled={vpnClientRunning}
                        onChange={(event) => setVpnProfileField('extraArgs', event.target.value)}
                        placeholder={t.extraArgsPlaceholder}
                      />
                      <div className="field-hint"><p>{t.extraArgsHint}</p></div>
                    </div>
                  </div>
                )}
              </section>
            )}

            {mode === 'vpnServer' && (
              <section className="advanced-panel">
                <button className="secondary advanced-toggle" disabled={vpnServerRunning} onClick={() => setVpnServerAdvanced((value) => !value)}>
                  {vpnServerAdvanced ? t.hideAdvancedSettings : t.advancedSettings}
                </button>
                {vpnServerAdvanced && (
                  <div className="advanced-fields">
                    <label className="check">
                      <input
                        type="checkbox"
                        checked={vpnServerUseUDP}
                        disabled={vpnServerRunning}
                        onChange={(event) => setVpnServerUseUDP(event.target.checked)}
                      />
                      <span>{t.useUDP}</span>
                    </label>
                    <div className="field">
                      <label>{t.upstreamProxy}</label>
                      <input
                        value={vpnServerUpstream}
                        disabled={vpnServerRunning}
                        onChange={(event) => setVpnServerUpstream(event.target.value)}
                        placeholder={t.upstreamProxyPlaceholder}
                      />
                      <div className="field-hint"><p>{t.upstreamProxyHint}</p></div>
                    </div>
                    <div className="field">
                      <label>{t.dnsForward}</label>
                      <input
                        value={vpnServerDNSForward}
                        disabled={vpnServerRunning}
                        onChange={(event) => setVpnServerDNSForward(event.target.value)}
                        placeholder={t.dnsForwardPlaceholder}
                      />
                      <div className="field-hint"><p>{t.dnsForwardHint}</p></div>
                    </div>
                    <div className="field">
                      <label>{t.extraArgs}</label>
                      <input
                        value={vpnServerExtraArgs}
                        disabled={vpnServerRunning}
                        onChange={(event) => setVpnServerExtraArgs(event.target.value)}
                        placeholder={t.extraArgsPlaceholder}
                      />
                      <div className="field-hint"><p>{t.extraArgsHint}</p></div>
                    </div>
                  </div>
                )}
              </section>
            )}

            <div className="primary-actions">
              {currentRunning ? (
                <button className="danger big-action" onClick={stop}>{t.stop}</button>
              ) : (
                <button className="primary big-action" disabled={!canStart} onClick={start}>
                  <span>{primaryLabel}</span>
                  {showVpnAdminPrompt && <span className="button-subtext">{t.vpnConnectAdminPrompt}</span>}
                </button>
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
                  <button className="secondary refresh-button" disabled={!status.localHTTPUrl || remoteListLoading} onClick={() => loadRemoteFiles()}>
                    {remoteListLoading && <span className="spinner-dot" aria-hidden="true" />}
                    {remoteListLoading ? t.refreshing : t.refresh}
                  </button>
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
                {remoteListLoading && (
                  <div className="remote-loading">
                    <span className="spinner-dot" aria-hidden="true" />
                    <span>{t.refreshing}</span>
                  </div>
                )}
                {!remoteList ? (
                  <p className="muted">{t.listHint}</p>
                ) : (
                  <>
                    {currentRemotePath !== '/' && (
                      <div className="remote-row nav-row">
                        <span>{t.dir}</span>
                        <button className="folder-link" onClick={() => loadRemoteFiles(parentPath(currentRemotePath))}>{t.parent}</button>
                        <em>-</em>
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
                        <em>{formatModTime(file.mod_time)}</em>
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
              <Metric label={t.p2pStatus} value={activeP2PReport?.status || (currentRunning ? 'starting' : 'idle')} />
              <Metric label={t.peer} value={activeP2PReport?.peer || '-'} />
              <Metric label={t.network} value={activeP2PReport?.network || '-'} />
              <Metric label={t.connectionRoute} value={routeLabel(activeP2PReport?.mode || '', t)} />
              <Metric label={t.speed} value={formatRate(transferSpeed)} />
              {mode === 'send' && <Metric label={t.sentTotal} value={formatBytes(sendTotalBytes)} />}
              {mode === 'vpnClient' && <Metric label={t.peerIpv6} value={peerIpv6Label(vpnClientPeerIPv6, t)} />}
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
            <h2>{qrTitle || t.qr}</h2>
            <img src={qrDataUrl} alt={t.qr} />
            <div className="qr-password">{qrPassword}</div>
            {qrHint && <p className="field-hint">{qrHint}</p>}
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
              <button className="secondary" disabled={scanBusy} onClick={() => startScreenScan(scanPurpose)}>{t.scanAgain}</button>
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

function defaultVpnProfile(name: string): VPNProfile {
  return {
    name: name || 'Default',
    passphrase: '',
    useUdp: false,
    routeIpv6: false,
    dnsServers: defaultVpnDNS,
    routeCidrs: defaultVpnRoutes,
    linkConfig: '',
    extraArgs: '',
    tunnelOnly: false,
  };
}

function currentVpnProfileFromState(t: typeof text.zh): VPNProfile {
  return defaultVpnProfile(t.vpnProfileDefaultName);
}

function normalizeVpnProfile(profile: Partial<VPNProfile>, t: typeof text.zh): VPNProfile {
  return {
    name: String(profile.name || t.vpnProfileDefaultName).trim() || t.vpnProfileDefaultName,
    passphrase: String(profile.passphrase || '').trim(),
    useUdp: Boolean(profile.useUdp),
    routeIpv6: Boolean(profile.routeIpv6),
    dnsServers: normalizeLines(String(profile.dnsServers || defaultVpnDNS)) || defaultVpnDNS,
    routeCidrs: normalizeLines(String(profile.routeCidrs || defaultVpnRoutes)) || defaultVpnRoutes,
    linkConfig: String(profile.linkConfig || '').trim(),
    extraArgs: String(profile.extraArgs || '').trim(),
    tunnelOnly: Boolean(profile.tunnelOnly),
  };
}

function normalizeVpnProfiles(profiles: VPNProfile[] | undefined, t: typeof text.zh) {
  const source = profiles && profiles.length > 0 ? profiles : [defaultVpnProfile(t.vpnProfileDefaultName)];
  return source.map((profile) => normalizeVpnProfile(profile, t));
}

function normalizeLines(value: string) {
  return value.replace(/\r\n/g, '\n').split('\n').map((line) => line.trim()).filter(Boolean).join('\n');
}

function clampIndex(index: number, length: number) {
  if (length <= 0) {
    return 0;
  }
  return Math.max(0, Math.min(index || 0, length - 1));
}

function uniqueVpnProfileName(profiles: VPNProfile[], base: string) {
  const cleanBase = (base || 'Profile').trim() || 'Profile';
  const existing = new Set(profiles.map((profile) => (profile.name || '').trim()));
  if (!existing.has(cleanBase)) {
    return cleanBase;
  }
  for (let index = 2; index < 1000; index++) {
    const candidate = `${cleanBase} ${index}`;
    if (!existing.has(candidate)) {
      return candidate;
    }
  }
  return `${cleanBase} ${Date.now()}`;
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

function peerIpv6Label(value: string, t: typeof text.zh) {
  switch ((value || '').trim().toLowerCase()) {
    case 'disabled':
      return t.peerIpv6Disabled;
    case 'waiting':
      return t.peerIpv6Waiting;
    case 'checking':
      return t.peerIpv6Checking;
    case 'available':
      return t.peerIpv6Available;
    case 'unavailable':
      return t.peerIpv6Unavailable;
    default:
      return value || '-';
  }
}

function normalizeP2PStatus(status: string) {
  return status.trim().toLowerCase();
}

function singleConnectionStatus(status: string, running: boolean, t: typeof text.zh): ConnectionStatus {
  if (!running) {
    return {label: t.idle, tone: 'idle'};
  }
  const reportStatus = normalizeP2PStatus(status);
  if (reportStatus.startsWith('error:') || reportStatus.startsWith('failed') || reportStatus === 'error') {
    return {label: t.connectionFailed, tone: 'error'};
  }
  if (reportStatus === 'disconnected' || reportStatus === 'stopped' || reportStatus === 'finished') {
    return {label: t.disconnected, tone: 'idle'};
  }
  if (reportStatus === 'connecting') {
    return {label: t.establishing, tone: 'connecting'};
  }
  if (reportStatus === 'negotiating') {
    return {label: t.negotiatingConnection, tone: 'connecting'};
  }
  if (reportStatus === 'connected') {
    return {label: t.connectedShort, tone: 'connected'};
  }
  if (reportStatus === 'wait' || reportStatus === 'waiting' || reportStatus === 'ready' || reportStatus === 'idle') {
    return {label: t.waitingConnection, tone: 'waiting'};
  }
  return {label: t.establishing, tone: 'connecting'};
}

function receiveConnectionStatus(report: P2PReport | null, running: boolean, localHTTPReady: boolean, t: typeof text.zh): ConnectionStatus {
  if (localHTTPReady && running && !report?.status) {
    return {label: t.connectedShort, tone: 'connected'};
  }
  return singleConnectionStatus(report?.status || '', running, t);
}

function multiClientActivityStatus(latest: P2PReport | null, reports: P2PReport[], running: boolean, t: typeof text.zh): ConnectionStatus {
  if (!running) {
    return {label: t.idle, tone: 'idle'};
  }
  const latestStatus = normalizeP2PStatus(latest?.status || '');
  if (latestStatus) {
    if (latestStatus.startsWith('error:') || latestStatus.startsWith('failed') || latestStatus === 'error') {
      return {label: t.connectionFailed, tone: 'error'};
    }
    if (latestStatus === 'connecting') {
      return {label: t.newConnection, tone: 'connecting'};
    }
    if (latestStatus === 'negotiating') {
      return {label: t.negotiatingConnection, tone: 'connecting'};
    }
    if (latestStatus === 'connected') {
      return {label: t.connectionSuccess, tone: 'connected'};
    }
    if (['wait', 'waiting', 'ready', 'idle', 'starting', 'preparing', 'disconnected', 'stopped', 'finished'].includes(latestStatus)) {
      return {label: t.waitingConnection, tone: 'waiting'};
    }
    return {label: t.establishing, tone: 'connecting'};
  }

  const statuses = reports
    .filter((report) => report.topic)
    .map((report) => normalizeP2PStatus(report.status));
  if (statuses.some((status) => status.startsWith('error:') || status.startsWith('failed') || status === 'error')) {
    return {label: t.connectionFailed, tone: 'error'};
  }
  if (statuses.includes('negotiating')) {
    return {label: t.negotiatingConnection, tone: 'connecting'};
  }
  if (statuses.includes('connecting')) {
    return {label: t.newConnection, tone: 'connecting'};
  }
  if (statuses.some((status) => status && !['wait', 'waiting', 'ready', 'idle', 'disconnected', 'stopped', 'finished'].includes(status))) {
    return {label: t.establishing, tone: 'connecting'};
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

function formatModTime(value: string) {
  if (!value) {
    return '-';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '-';
  }
  return date.toLocaleString(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
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

