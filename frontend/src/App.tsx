import {useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent} from 'react';
import QRCode from 'qrcode';
import type {KeyboardEvent as ReactKeyboardEvent} from 'react';
import {prepareZXingModule, readBarcodes, type ReaderOptions} from 'zxing-wasm/reader';
import zxingReaderWasmUrl from 'zxing-wasm/reader/zxing_reader.wasm?url';
import appIconUrl from './assets/images/appicon.png';
import './App.css';
import {TransferInlineQr} from './TransferInlineQr';
import {
  fileTransferReportBelongsToRun,
  inlineQrShouldMask,
  isFileTransferMode,
  latchSuccessfulConnection,
  maskAfterPassphraseUpdate,
  transferStartGate,
} from './inlineQrState';
import {vpnprofile} from '../wailsjs/go/models';
import {
  CaptureScreen,
  CheckReceivedFiles,
  CheckForUpdate,
  ClearTaskbarProgress,
  GeneratePassword,
  IsAdministrator,
  LoadVPNProfiles,
  OpenSaveDir,
  RemoteFiles,
  RevealReceivedFile,
  SaveVPNProfiles,
  SelectFiles,
  SelectFolder,
  StartHTTPDownload,
  StartTransfer,
  Status,
  StartupSharePaths,
  StopHTTPDownload,
  StopTransfer,
  SetTaskbarProgress,
  UpdateSharePaths
} from '../wailsjs/go/main/App';
import {BrowserOpenURL, ClipboardGetText, Environment, EventsOff, EventsOn, OnFileDrop, OnFileDropOff, InitializeNotifications, IsNotificationAvailable, RequestNotificationAuthorization, SendNotification, WindowMinimise, WindowSetAlwaysOnTop, WindowShow, WindowUnminimise} from '../wailsjs/runtime/runtime';

type Mode = 'send' | 'receive' | 'vpnServer' | 'vpnClient';
type Lang = 'zh' | 'en';
type DownloadMode = 'resume' | 'overwrite';

type UpdateErrorCode = 'network' | 'manifest' | 'platform';

type UpdateState =
  | {kind: 'idle'}
  | {kind: 'checking'}
  | {kind: 'current'; latestVersion: string}
  | {kind: 'available'; latestVersion: string; downloadUrl: string}
  | {kind: 'error'; code: UpdateErrorCode};

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
  clientRunId?: number;
};

type DownloadEvent = {
  type: string;
  level: string;
  message: string;
  time: string;
  clientTaskId?: number;
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

type ReceivedLocalState = {
  saveDir: string;
  size: number;
};

type VPNProfile = {
  name: string;
  passphrase: string;
  useUdp: boolean;
  routeIpv6: boolean;
  dnsServers: string;
  routeCidrs: string;
  linkConfig: string;
  mtu: number;
  routeMetric: number;
  blockDnsLeak: boolean;
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
const goncSourceUrl = 'https://github.com/threatexpert/gonc';
const guiSourceUrl = 'https://github.com/threatexpert/gonc-gui';
const vpnProfileQrType = 'gonc.vpn.profile';
const defaultVpnDNS = '8.8.8.8\n2001:4860:4860::8888';
const defaultVpnRoutes = '0.0.0.0/1\n128.0.0.0/1\n::/0';
const privateLanRoutes = '10.0.0.0/8\n172.16.0.0/12\n192.168.0.0/16';
const defaultVpnMTU = 1400;
const defaultVpnRouteMetric = 1;
const scanDecodeTargetSize = 480;
const scanTileSizes = [960, 640, 420];
const scanTileOverlap = 0.35;
const zxingReaderOptions: ReaderOptions = {
  formats: ['QRCode'],
  tryHarder: true,
  tryRotate: true,
  tryInvert: true,
  tryDownscale: true,
  tryDenoise: true,
  maxNumberOfSymbols: 1,
  textMode: 'Plain',
};

prepareZXingModule({
  overrides: {
    locateFile: (path: string, prefix: string) => path.endsWith('.wasm') ? zxingReaderWasmUrl : prefix + path,
  },
});

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
    receiveCurrentDir: '接收当前目录',
    connectedReceivers: '已连接',
    connectingReceivers: '正在建立',
    connections: '连接',
    establishing: '建立中',
    negotiatingConnection: '建立安全连接中',
    waitingConnection: '在线',
    waitingPeer: '等待对端',
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
    lanRoute: '局域网',
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
    scanCaptureTimeout: '截图已取消或超时，请重试。',
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
    openFolder: '打开目录',
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
    mtu: 'MTU',
    mtuPlaceholder: '默认 1400',
    mtuHint: '留空或无效值使用默认 1400，建议范围 576-9000。',
    routeMetric: 'Windows 路由跃点数',
    routeMetricPlaceholder: '默认 1',
    routeMetricHint: '仅 Windows VPN 使用。留空或无效值使用默认 1。',
    dnsLeakProtection: '防 DNS 泄漏',
    dnsLeakProtectionHint: '仅 Windows VPN 使用。连接期间阻止系统 DNS Client 服务用非 VPN 本地地址访问 TCP/UDP 53，停止 VPN 后会删除规则。',
    remoteFiles: '对方分享的文件',
    selectAll: '全选',
    invertSelection: '反选',
    refresh: '刷新',
    refreshing: '刷新中',
    stopDownload: '停止下载',
    downloadSelected: '下载选中',
    downloadMode: '下载方式',
    resumeDownload: '续传',
    overwriteDownload: '覆盖',
    revealReceivedFileWindows: '在文件资源管理器中显示',
    revealReceivedFileMac: '在访达中显示',
    revealReceivedFileOther: '在文件管理器中显示',
    localFileUnavailable: '本地文件不可用：',
    noSelection: '请先勾选要下载的文件或目录。',
    downloadFailed: '下载失败：',
    noList: '尚未读取目录',
    remoteListAutoLoadFailed: '自动读取文件列表失败：',
    files: '个文件',
    folders: '个目录',
    selected: '已选',
    excluded: '排除',
    completed: '完成',
    skipped: '跳过',
    resumed: '续传',
    failed: '失败',
    listHint: '连接建立后会自动读取对方分享的文件目录；可点击目录进入，并勾选需要下载的项。',
    activity: '活动日志',
    diagnostics: '状态和日志',
    clear: '清空',
    close: '关闭',
    about: '关于',
    aboutTitle: '开源项目',
    aboutDescription: 'Gonc 是开源项目。你可以在 GitHub 查看源代码、反馈问题和关注更新。',
    checkForUpdates: '检查更新',
    checkingForUpdates: '正在检查…',
    upToDate: '已是最新版本',
    updateAvailable: '发现新版本',
    goToDownload: '前往下载',
    updateNetworkError: '无法检查更新，请检查网络后重试。',
    updateManifestError: '更新信息无效，请稍后重试。',
    updatePlatformError: '当前平台暂不提供更新。',
    logHint: '传输开始后日志会显示在这里。',
    file: '文件',
    dir: '目录',
    modifiedTime: '修改时间',
    vpnTunnelPausedTitle: 'VPN 隧道已断开',
    vpnTunnelPausedBody: '正在等待隧道重连，系统路由已临时暂停。',
    vpnTunnelRestoredTitle: 'VPN 隧道已恢复',
    vpnTunnelRestoredBody: '隧道已重新连接，系统路由已恢复。',
    vpnStartedTitle: 'VPN 已开启',
    vpnStartedBody: '系统 VPN 路由和 DNS 已配置完成。',
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
    receiveCurrentDir: 'Receive Current Folder',
    connectedReceivers: 'Connected',
    connectingReceivers: 'Establishing',
    connections: 'Connections',
    establishing: 'Establishing',
    negotiatingConnection: 'Negotiating secure connection',
    waitingConnection: 'Online',
    waitingPeer: 'Waiting for peer',
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
    lanRoute: 'LAN',
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
    scanCaptureTimeout: 'Screenshot was cancelled or timed out. Try again.',
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
    openFolder: 'Open Folder',
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
    mtu: 'MTU',
    mtuPlaceholder: 'Default 1400',
    mtuHint: 'Blank or invalid values use the default 1400. Recommended range: 576-9000.',
    routeMetric: 'Windows route metric',
    routeMetricPlaceholder: 'Default 1',
    routeMetricHint: 'Windows VPN only. Blank or invalid values use the default 1.',
    dnsLeakProtection: 'DNS leak protection',
    dnsLeakProtectionHint: 'Windows VPN only. Blocks the system DNS Client service from accessing TCP/UDP 53 from non-VPN local addresses and removes the rules after VPN stops.',
    remoteFiles: 'Peer Shared Files',
    selectAll: 'Select All',
    invertSelection: 'Invert',
    refresh: 'Refresh',
    refreshing: 'Refreshing',
    stopDownload: 'Stop Download',
    downloadSelected: 'Download Selected',
    downloadMode: 'Download Mode',
    resumeDownload: 'Resume',
    overwriteDownload: 'Overwrite',
    revealReceivedFileWindows: 'Show in File Explorer',
    revealReceivedFileMac: 'Show in Finder',
    revealReceivedFileOther: 'Show in file manager',
    localFileUnavailable: 'Local file unavailable:',
    noSelection: 'Select files or folders to download first.',
    downloadFailed: 'Download failed:',
    noList: 'No list loaded',
    remoteListAutoLoadFailed: 'Automatic file list load failed:',
    files: 'files',
    folders: 'folders',
    selected: 'selected',
    excluded: 'excluded',
    completed: 'Done',
    skipped: 'Skipped',
    resumed: 'Resumed',
    failed: 'Failed',
    listHint: 'After the connection is ready, shared files from the peer are loaded automatically. Click folders to browse and tick items to download.',
    activity: 'Activity',
    diagnostics: 'Status and Logs',
    clear: 'Clear',
    close: 'Close',
    about: 'About',
    aboutTitle: 'Open source project',
    aboutDescription: 'Gonc is developed as an open source project. You can inspect the source code, report issues, and follow updates on GitHub.',
    checkForUpdates: 'Check for updates',
    checkingForUpdates: 'Checking…',
    upToDate: 'Up to date',
    updateAvailable: 'Update available',
    goToDownload: 'Go to download',
    updateNetworkError: 'Unable to check for updates. Check your network and try again.',
    updateManifestError: 'Update information is invalid. Try again later.',
    updatePlatformError: 'Updates are unavailable for this platform.',
    logHint: 'Logs will appear here after a transfer starts.',
    file: 'FILE',
    dir: 'DIR',
    modifiedTime: 'Modified',
    vpnTunnelPausedTitle: 'VPN tunnel disconnected',
    vpnTunnelPausedBody: 'Waiting for the tunnel to reconnect. System routes are paused temporarily.',
    vpnTunnelRestoredTitle: 'VPN tunnel restored',
    vpnTunnelRestoredBody: 'The tunnel is connected again and system routes are restored.',
    vpnStartedTitle: 'VPN is on',
    vpnStartedBody: 'System VPN routes and DNS are ready.',
    shareUpdateFailed: 'Failed to update shared list.',
    weakPassword: 'Passphrase is too weak. Use at least 8 characters with both letters and digits.',
  }
};

function detectLang(): Lang {
  const lang = navigator.language.toLowerCase();
  return lang === 'zh-cn' || lang.startsWith('zh-hans') ? 'zh' : 'en';
}

async function decodeQrFromImageRegion(img: HTMLImageElement, sx: number, sy: number, sw: number, sh: number): Promise<string | null> {
  const source = cropImageRegion(img, sx, sy, sw, sh);
  const attempts: HTMLCanvasElement[] = [source, withWhiteBorder(source)];
  const smallestSide = Math.min(source.width, source.height);
  if (smallestSide > 0 && smallestSide < scanDecodeTargetSize) {
    const scale = Math.min(5, Math.ceil(scanDecodeTargetSize / smallestSide));
    const scaled = scaleCanvas(source, scale);
    attempts.push(scaled, withWhiteBorder(scaled));
  }
  attempts.push(...scanTileCanvases(source));

  for (const canvas of attempts) {
    const decoded = await decodeQrCanvas(canvas);
    if (decoded) {
      return decoded;
    }
  }
  return null;
}

function cropImageRegion(img: HTMLImageElement, sx: number, sy: number, sw: number, sh: number) {
  const pad = Math.max(16, Math.round(Math.min(sw, sh) * 0.1));
  const x = Math.max(0, Math.floor(sx - pad));
  const y = Math.max(0, Math.floor(sy - pad));
  const right = Math.min(img.naturalWidth, Math.ceil(sx + sw + pad));
  const bottom = Math.min(img.naturalHeight, Math.ceil(sy + sh + pad));
  const width = Math.max(1, right - x);
  const height = Math.max(1, bottom - y);
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext('2d');
  if (!ctx) {
    throw new Error('canvas is unavailable');
  }
  ctx.drawImage(img, x, y, width, height, 0, 0, width, height);
  return canvas;
}

async function decodeQrCanvas(canvas: HTMLCanvasElement): Promise<string | null> {
  const ctx = canvas.getContext('2d', {willReadFrequently: true});
  if (!ctx) {
    throw new Error('canvas is unavailable');
  }
  const imageData = ctx.getImageData(0, 0, canvas.width, canvas.height);
  try {
    const results = await readBarcodes(imageData, zxingReaderOptions);
    const result = results.find(item => item.isValid && item.text);
    return result?.text || null;
  } catch {
    return null;
  }
}

function scanTileCanvases(source: HTMLCanvasElement) {
  if (Math.max(source.width, source.height) <= scanTileSizes[0]) {
    return [];
  }
  const tiles: HTMLCanvasElement[] = [];
  const seen = new Set<string>();
  for (const requestedSize of scanTileSizes) {
    const tileWidth = Math.min(requestedSize, source.width);
    const tileHeight = Math.min(requestedSize, source.height);
    if (tileWidth < 120 || tileHeight < 120) {
      continue;
    }
    const stepX = Math.max(80, Math.round(tileWidth * (1 - scanTileOverlap)));
    const stepY = Math.max(80, Math.round(tileHeight * (1 - scanTileOverlap)));
    for (const y of scanStarts(source.height, tileHeight, stepY)) {
      for (const x of scanStarts(source.width, tileWidth, stepX)) {
        const key = `${x},${y},${tileWidth},${tileHeight}`;
        if (seen.has(key)) {
          continue;
        }
        seen.add(key);
        tiles.push(withWhiteBorder(copyCanvasRegion(source, x, y, tileWidth, tileHeight)));
      }
    }
  }
  return tiles;
}

function scanStarts(total: number, size: number, step: number) {
  if (size >= total) {
    return [0];
  }
  const starts: number[] = [];
  for (let value = 0; value + size < total; value += step) {
    starts.push(value);
  }
  starts.push(total - size);
  return starts;
}

function copyCanvasRegion(source: HTMLCanvasElement, sx: number, sy: number, sw: number, sh: number) {
  const canvas = document.createElement('canvas');
  canvas.width = sw;
  canvas.height = sh;
  const ctx = canvas.getContext('2d');
  if (!ctx) {
    throw new Error('canvas is unavailable');
  }
  ctx.drawImage(source, sx, sy, sw, sh, 0, 0, sw, sh);
  return canvas;
}

function withWhiteBorder(source: HTMLCanvasElement) {
  const margin = Math.max(16, Math.round(Math.min(source.width, source.height) * 0.1));
  const canvas = document.createElement('canvas');
  canvas.width = source.width + margin * 2;
  canvas.height = source.height + margin * 2;
  const ctx = canvas.getContext('2d');
  if (!ctx) {
    throw new Error('canvas is unavailable');
  }
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.drawImage(source, margin, margin);
  return canvas;
}

function scaleCanvas(source: HTMLCanvasElement, scale: number) {
  const canvas = document.createElement('canvas');
  canvas.width = source.width * scale;
  canvas.height = source.height * scale;
  const ctx = canvas.getContext('2d');
  if (!ctx) {
    throw new Error('canvas is unavailable');
  }
  ctx.imageSmoothingEnabled = false;
  ctx.drawImage(source, 0, 0, canvas.width, canvas.height);
  return canvas;
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
  const [vpnClientMTU, setVpnClientMTU] = useState(String(defaultVpnMTU));
  const [vpnClientRouteMetric, setVpnClientRouteMetric] = useState(String(defaultVpnRouteMetric));
  const [vpnClientBlockDNSLeak, setVpnClientBlockDNSLeak] = useState(false);
  const [vpnClientExtraArgs, setVpnClientExtraArgs] = useState('');
  const [runtimePlatform, setRuntimePlatform] = useState(() => navigator.platform.toLowerCase().includes('win') ? 'windows' : '');
  const [vpnProfiles, setVpnProfiles] = useState<VPNProfile[]>([]);
  const [selectedVpnProfile, setSelectedVpnProfile] = useState(0);
  const [status, setStatus] = useState<AppStatus>({running: false, sendRunning: false, receiveRunning: false, vpnServerRunning: false, vpnClientRunning: false, localHTTPUrl: '', downloading: false, defaultSaveDir: ''});
  const [logs, setLogs] = useState<LogEvent[]>([]);
  const [error, setError] = useState('');
  const [startPending, setStartPending] = useState(false);
  const [receiveP2PReport, setReceiveP2PReport] = useState<P2PReport | null>(null);
  const [sendP2PReports, setSendP2PReports] = useState<Record<string, P2PReport>>({});
  const [sendQrHasConnected, setSendQrHasConnected] = useState(false);
  const [receiveQrHasConnected, setReceiveQrHasConnected] = useState(false);
  const previousSendQrPassphrase = useRef(sendPassword);
  const previousReceiveQrPassphrase = useRef(receivePassword);
  const [vpnServerP2PReports, setVpnServerP2PReports] = useState<Record<string, P2PReport>>({});
  const [vpnClientP2PReport, setVpnClientP2PReport] = useState<P2PReport | null>(null);
  const [remoteList, setRemoteList] = useState<RemoteList | null>(null);
  const [remoteListLoading, setRemoteListLoading] = useState(false);
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(new Set());
  const [excludedPaths, setExcludedPaths] = useState<Set<string>>(new Set());
  const [downloadError, setDownloadError] = useState('');
  const [downloadProgress, setDownloadProgress] = useState<DownloadEvent | null>(null);
  const [downloadMode, setDownloadMode] = useState<DownloadMode>('resume');
  const [receivedLocalFiles, setReceivedLocalFiles] = useState<Map<string, ReceivedLocalState>>(new Map());
  const [receivedDownloadActiveState, setReceivedDownloadActiveState] = useState(false);
  const [receivedCompletionRefreshPending, setReceivedCompletionRefreshPending] = useState(false);
  const [sendTraffic, setSendTraffic] = useState<LogEvent | null>(null);
  const [receiveTraffic, setReceiveTraffic] = useState<LogEvent | null>(null);
  const [vpnServerTraffic, setVpnServerTraffic] = useState<LogEvent | null>(null);
  const [vpnClientTraffic, setVpnClientTraffic] = useState<LogEvent | null>(null);
  const [vpnClientPeerIPv6, setVpnClientPeerIPv6] = useState('disabled');
  const [vpnClientSocks5Endpoint, setVpnClientSocks5Endpoint] = useState('');
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
  const [aboutOpen, setAboutOpen] = useState(false);
  const [updateState, setUpdateState] = useState<UpdateState>({kind: 'idle'});
  const aboutButtonRef = useRef<HTMLButtonElement | null>(null);
  const aboutDialogRef = useRef<HTMLElement | null>(null);
  const scanImgRef = useRef<HTMLImageElement | null>(null);
  const scanDragStart = useRef<{x: number; y: number} | null>(null);
  const scanBusyRef = useRef(false);
  const scanImageRef = useRef('');
  const scanCaptureSeq = useRef(0);
  const notificationsReady = useRef(false);
  const vpnDisconnectNotified = useRef(false);
  const vpnStartedNotified = useRef(false);
  const vpnTunnelWasConnected = useRef(false);
  const vpnStopRequested = useRef(false);
  const receivedCheckGeneration = useRef(0);
  const receivedSaveDir = useRef(saveDir);
  const receivedVisibleEntries = useRef<VisibleEntry[]>([]);
  const receivedDownloadActive = useRef(false);
  const receivedCompletionRefreshPendingRef = useRef(false);
  const receivedDownloadTaskId = useRef(0);
  const receivedTerminalOwner = useRef<number | null>(null);
  const receivedTerminalRefreshPromise = useRef<{taskId: number; promise: Promise<void>} | null>(null);
  const transferRunSequence = useRef(0);
  const startPendingRef = useRef(false);
  const activeSendTransferRun = useRef(0);
  const activeReceiveTransferRun = useRef(0);
  const receivedStatusDownloadingRef = useRef(status.downloading);
  const receivedLocalFilesRef = useRef(receivedLocalFiles);
  const [nowTick, setNowTick] = useState(Date.now());
  const passwordTimer = useRef<number | null>(null);
  const activePassword = mode === 'send' ? sendPassword : (mode === 'receive' ? receivePassword : (mode === 'vpnServer' ? vpnServerPassword : vpnClientPassword));

  useEffect(() => {
    if (!aboutOpen) {
      return;
    }
    const dialog = aboutDialogRef.current;
    if (!dialog) {
      return;
    }
    const activeElement = document.activeElement;
    if (updateState.kind === 'checking') {
      if (!dialog.contains(activeElement) || (activeElement instanceof HTMLButtonElement && activeElement.disabled)) {
        dialog.focus();
      }
      return;
    }
    if (!dialog.contains(activeElement)) {
      (dialog.querySelector<HTMLElement>('button:not(:disabled)') ?? dialog).focus();
    }
  }, [aboutOpen, updateState.kind]);

  const remoteFiles = useMemo(() => remoteListFiles(remoteList), [remoteList]);
  const visibleEntries = useMemo(() => safeShallowEntries(remoteFiles, currentRemotePath), [remoteFiles, currentRemotePath]);
  receivedSaveDir.current = saveDir;
  receivedVisibleEntries.current = visibleEntries;
  receivedLocalFilesRef.current = receivedLocalFiles;
  receivedStatusDownloadingRef.current = status.downloading;
  const currentRemoteBreadcrumbs = useMemo(() => remoteBreadcrumbs(currentRemotePath), [currentRemotePath]);
  const selectedRemoteBytes = useMemo(() => selectedRemoteSize(remoteFiles, selectedPaths, excludedPaths), [remoteFiles, selectedPaths, excludedPaths]);
  const activeSpeed = Math.max(
    freshSpeed(downloadProgress?.time, downloadProgress?.bytesPerSecond, nowTick),
    freshSpeed(receiveTraffic?.time, receiveTraffic?.inBps, nowTick),
    freshSpeed(receiveTraffic?.time, receiveTraffic?.outBps, nowTick)
  );
  const sendRunning = status.sendRunning;
  const receiveRunning = status.receiveRunning;
  const vpnServerRunning = status.vpnServerRunning;
  const vpnClientRunning = status.vpnClientRunning;
  const isWindows = runtimePlatform === 'windows';
  const currentRunning = mode === 'send' ? sendRunning : (mode === 'receive' ? receiveRunning : (mode === 'vpnServer' ? vpnServerRunning : vpnClientRunning));
  const canStart = !startPending && !currentRunning && activePassword.trim().length > 0 && (mode !== 'send' || sharePaths.length > 0);
  const receivedActionsUnavailable = receivedDownloadActiveState || status.downloading || receivedCompletionRefreshPending;
  const canDownload = Boolean(mode === 'receive' && status.localHTTPUrl && saveDir && selectedPaths.size > 0 && !receivedActionsUnavailable);
  const canDownloadAll = Boolean(mode === 'receive' && status.localHTTPUrl && saveDir && remoteList && visibleEntries.length > 0 && !receivedActionsUnavailable);
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
  const revealFileLabel = runtimePlatform === 'windows'
    ? t.revealReceivedFileWindows
    : (runtimePlatform === 'darwin' ? t.revealReceivedFileMac : t.revealReceivedFileOther);

  useEffect(() => {
    const previous = previousSendQrPassphrase.current;
    setSendQrHasConnected((masked) => maskAfterPassphraseUpdate(
      masked, sendRunning, previous, sendPassword,
    ));
    previousSendQrPassphrase.current = sendPassword;
  }, [sendPassword, sendRunning]);

  useEffect(() => {
    const previous = previousReceiveQrPassphrase.current;
    setReceiveQrHasConnected((masked) => maskAfterPassphraseUpdate(
      masked, receiveRunning, previous, receivePassword,
    ));
    previousReceiveQrPassphrase.current = receivePassword;
  }, [receivePassword, receiveRunning]);

  useEffect(() => {
    if (!remoteList || !saveDir || status.downloading || receivedDownloadActive.current || receivedCompletionRefreshPendingRef.current) {
      return;
    }
    refreshVisibleReceivedFiles(visibleEntries, saveDir).catch(() => undefined);
  }, [remoteList, visibleEntries, saveDir]);

  useEffect(() => {
    const timer = window.setInterval(() => setNowTick(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    scanBusyRef.current = scanBusy;
  }, [scanBusy]);

  useEffect(() => {
    scanImageRef.current = scanImage;
  }, [scanImage]);

  useEffect(() => {
    const recoverPendingCapture = () => {
      if (document.visibilityState === 'hidden') {
        return;
      }
      window.setTimeout(() => {
        if (!scanBusyRef.current || scanImageRef.current) {
          return;
        }
        scanCaptureSeq.current += 1;
        restoreAppWindowAfterCapture();
        setScanBusy(false);
        setError(t.scanCaptureTimeout);
      }, 800);
    };
    window.addEventListener('focus', recoverPendingCapture);
    document.addEventListener('visibilitychange', recoverPendingCapture);
    return () => {
      window.removeEventListener('focus', recoverPendingCapture);
      document.removeEventListener('visibilitychange', recoverPendingCapture);
    };
  }, [t.scanCaptureTimeout]);

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
    StartupSharePaths()
      .then((paths) => {
        if (cancelled || !paths || paths.length === 0) {
          return;
        }
        setMode('send');
        appendSharePaths(paths);
      })
      .catch(() => undefined);
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    Environment()
      .then((env) => {
        if (!cancelled) {
          setRuntimePlatform(String(env.platform || '').toLowerCase());
        }
      })
      .catch(() => {
        if (!cancelled && navigator.platform.toLowerCase().includes('win')) {
          setRuntimePlatform('windows');
        }
      });
    return () => {
      cancelled = true;
    };
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
        return;
      }
      if (event.type === 'socks5' && event.mode === 'vpnClient') {
        setVpnClientSocks5Endpoint(event.localUrl || event.message.replace(/^SOCKS5 endpoint is ready:\s*/i, ''));
      }
      if (event.mode === 'vpnClient') {
        if (event.message.includes('pausing system VPN routes') || event.message.includes('pausing Windows VPN routes')) {
          if (!vpnStopRequested.current) {
            notifyVpnTunnelPaused();
          }
        } else if (event.message.includes('System VPN started') || event.message.includes('System VPN routes restored') || event.message.includes('Windows VPN started') || event.message.includes('Windows VPN routes restored')) {
          notifyVpnStarted();
          notifyVpnTunnelRestored();
          vpnTunnelWasConnected.current = true;
          vpnStopRequested.current = false;
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
        if (!fileTransferReportBelongsToRun(report.clientRunId ?? 0, activeSendTransferRun.current)) {
          return;
        }
        setSendQrHasConnected((current) => latchSuccessfulConnection(current, report.status));
        setSendP2PReports((current) => ({...current, [p2pSessionKey(report)]: report}));
      } else if (report.side === 'receive') {
        if (!fileTransferReportBelongsToRun(report.clientRunId ?? 0, activeReceiveTransferRun.current)) {
          return;
        }
        setReceiveQrHasConnected((current) => latchSuccessfulConnection(current, report.status));
        setReceiveP2PReport(report);
      } else if (report.side === 'vpnServer') {
        setVpnServerP2PReports((current) => ({...current, [p2pSessionKey(report)]: report}));
      } else if (report.side === 'vpnClient') {
        setVpnClientP2PReport(report);
        handleVpnClientTunnelReport(report.status);
      }
    });
    EventsOn('download:event', (event: DownloadEvent) => {
      const taskId = event.clientTaskId;
      if (!taskId || taskId <= 0 || taskId !== receivedDownloadTaskId.current) {
        return;
      }
      const terminal = event.type === 'status' && (event.message.includes('download complete') || event.message.includes('download finished') || event.level === 'error');
      if (event.type === 'progress') {
        setDownloadProgress(event);
        SetTaskbarProgress(event.doneBytes || 0, event.totalBytes || 0).catch(() => undefined);
      } else {
        setLogs((current) => [...current.slice(-399), event]);
      }
      if (event.type === 'status') {
        if (terminal) {
          if (event.level === 'error') {
            setDownloadError(`${t.downloadFailed} ${localizeError(event.message)}`);
          }
          ClearTaskbarProgress().catch(() => undefined);
          refreshReceivedFilesAfterDownload(taskId).catch(() => undefined);
        } else {
          refreshStatus();
        }
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
      receivedStatusDownloadingRef.current = next.downloading;
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
      if (!receivedSaveDir.current && next.defaultSaveDir) {
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

  async function refreshVisibleReceivedFiles(entries: VisibleEntry[], root = saveDir) {
    const generation = ++receivedCheckGeneration.current;
    const files = entries.filter((entry) => !entry.is_dir);
    const checked = await CheckReceivedFiles(root, files as any);
    if (generation !== receivedCheckGeneration.current || receivedDownloadActive.current) {
      return;
    }
    const paths = new Set(files.map((file) => normalizeRemotePath(file.path)));
    setReceivedLocalFiles((current) => {
      if (generation !== receivedCheckGeneration.current || receivedDownloadActive.current) {
        return current;
      }
      const next = new Map(current);
      paths.forEach((path) => next.delete(path));
      checked.forEach((state) => {
        const path = normalizeRemotePath(state.remotePath);
        const remote = files.find((file) => normalizeRemotePath(file.path) === path);
        if (state.available && remote) {
          next.set(path, {saveDir: root, size: remote.size});
        }
      });
      receivedLocalFilesRef.current = next;
      return next;
    });
  }

  async function revealReceivedFile(file: VisibleEntry) {
    if (receivedDownloadImperativeBusy()) {
      return;
    }
    const path = normalizeRemotePath(file.path);
    const local = receivedLocalFiles.get(path);
    if (!local) {
      return;
    }
    const generation = receivedCheckGeneration.current;
    let unavailableError = '';
    try {
      const result = await RevealReceivedFile(local.saveDir, file as any);
      if (!result.error) {
        return;
      }
      if (!result.unavailable) {
        setDownloadError(localizeError(result.error));
        return;
      }
      unavailableError = result.error;
    } catch (err) {
      setDownloadError(localizeError(String(err)));
      return;
    }
    if (generation !== receivedCheckGeneration.current || receivedLocalFilesRef.current.get(path) !== local) {
      return;
    }
    setReceivedLocalFiles((current) => {
      if (generation !== receivedCheckGeneration.current || current.get(path) !== local) {
        return current;
      }
      const next = new Map(current);
      next.delete(path);
      receivedLocalFilesRef.current = next;
      return next;
    });
    setDownloadError(`${t.localFileUnavailable} ${localizeError(unavailableError)}`);
  }

  function receivedDownloadImperativeBusy() {
    return receivedDownloadActive.current || receivedCompletionRefreshPendingRef.current || receivedStatusDownloadingRef.current;
  }

  function refreshReceivedFilesAfterDownload(taskId: number): Promise<void> {
    if (taskId !== receivedDownloadTaskId.current) {
      return Promise.resolve();
    }
    const existing = receivedTerminalRefreshPromise.current;
    if (existing?.taskId === taskId) {
      return existing.promise;
    }
    if (receivedTerminalOwner.current !== null) {
      return Promise.resolve();
    }
    receivedTerminalOwner.current = taskId;
    receivedCompletionRefreshPendingRef.current = true;
    setReceivedCompletionRefreshPending(true);
    receivedDownloadActive.current = false;
    setReceivedDownloadActiveState(false);
    const promise = (async () => {
      try {
        await refreshStatus();
        await refreshVisibleReceivedFiles(receivedVisibleEntries.current, receivedSaveDir.current);
      } finally {
        if (taskId === receivedDownloadTaskId.current && receivedTerminalOwner.current === taskId) {
          receivedCompletionRefreshPendingRef.current = false;
          setReceivedCompletionRefreshPending(false);
        }
      }
    })();
    receivedTerminalRefreshPromise.current = {taskId, promise};
    return promise;
  }

  function beginReceivedDownloadTask(): number | null {
    if (receivedDownloadImperativeBusy()) {
      return null;
    }
    receivedCheckGeneration.current += 1;
    receivedDownloadTaskId.current += 1;
    const taskId = receivedDownloadTaskId.current;
    receivedDownloadActive.current = true;
    setReceivedDownloadActiveState(true);
    receivedCompletionRefreshPendingRef.current = false;
    setReceivedCompletionRefreshPending(false);
    receivedTerminalOwner.current = null;
    receivedTerminalRefreshPromise.current = null;
    return taskId;
  }

  function abandonReceivedDownloadTask(taskId: number) {
    if (taskId !== receivedDownloadTaskId.current || receivedTerminalOwner.current !== null) {
      return;
    }
    receivedDownloadActive.current = false;
    setReceivedDownloadActiveState(false);
    receivedCompletionRefreshPendingRef.current = false;
    setReceivedCompletionRefreshPending(false);
  }

  function resetReceivedDownloadStateForNewConnection() {
    receivedCheckGeneration.current += 1;
    receivedDownloadTaskId.current += 1;
    receivedDownloadActive.current = false;
    setReceivedDownloadActiveState(false);
    receivedCompletionRefreshPendingRef.current = false;
    setReceivedCompletionRefreshPending(false);
    receivedTerminalOwner.current = null;
    receivedTerminalRefreshPromise.current = null;
    const empty = new Map<string, ReceivedLocalState>();
    receivedLocalFilesRef.current = empty;
    setReceivedLocalFiles(empty);
  }

  async function openSaveDir() {
    setError('');
    try {
      const opened = await OpenSaveDir(saveDir);
      if (opened) {
        setSaveDir(opened);
      }
    } catch (err) {
      setError(localizeError(String(err)));
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
    const captureSeq = scanCaptureSeq.current + 1;
    scanCaptureSeq.current = captureSeq;
    setError('');
    setScanError('');
    setScanRect(null);
    setScanPurpose(purpose);
    setScanBusy(true);
    try {
      prepareAppWindowForCapture();
      const dataUrl = await captureScreenWithTimeout(12000, t.scanCaptureTimeout);
      if (scanCaptureSeq.current !== captureSeq) {
        return;
      }
      setScanImage(dataUrl);
    } catch (err) {
      if (scanCaptureSeq.current === captureSeq) {
        setError(localizeError(String(err)));
      }
    } finally {
      if (scanCaptureSeq.current === captureSeq) {
        restoreAppWindowAfterCapture();
        setScanBusy(false);
      }
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
      const result = await decodeQrFromImageRegion(img, sx, sy, sw, sh);
      if (result) {
        const decoded = result.trim();
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
    const gate = transferStartGate(startPendingRef.current);
    if (!gate.accepted) {
      return;
    }
    startPendingRef.current = gate.pending;
    setStartPending(gate.pending);
    setError('');
    const passphrase = activePassword.trim();
    const clientRunId = isFileTransferMode(mode) ? ++transferRunSequence.current : 0;
    let transferStarted = false;
    try {
      if (mode === 'send') {
        activeSendTransferRun.current = clientRunId;
        setSendQrHasConnected(false);
        setSendP2PReports({});
        setSendTraffic(null);
      } else if (mode === 'receive') {
        activeReceiveTransferRun.current = clientRunId;
        setReceiveQrHasConnected(false);
        setReceiveP2PReport(null);
        setRemoteList(null);
        setRemoteListLoading(false);
        setSelectedPaths(new Set());
        setExcludedPaths(new Set());
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
        setVpnClientSocks5Endpoint('');
        vpnDisconnectNotified.current = false;
        vpnStartedNotified.current = false;
        vpnTunnelWasConnected.current = false;
        vpnStopRequested.current = false;
      }
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
        mtu: normalizeVpnMTU(vpnClientMTU),
        routeMetric: normalizeRouteMetric(vpnClientRouteMetric),
        blockDnsLeak: isWindows && vpnClientBlockDNSLeak,
        enableIpv6: vpnClientEnableIPv6,
        tunnelOnly: vpnClientTunnelOnly,
        extraArgs: mode === 'vpnClient' ? vpnClientExtraArgs : vpnServerExtraArgs,
        ...(isFileTransferMode(mode) ? {clientRunId} : {}),
      });
      transferStarted = true;
      if (mode === 'receive') {
        resetReceivedDownloadStateForNewConnection();
      } else if (mode === 'vpnServer') {
        setVpnServerAdvanced(false);
      } else if (mode === 'vpnClient') {
        setVpnClientAdvanced(false);
      }
      await refreshStatus();
    } catch (err) {
      if (!transferStarted && mode === 'send' && activeSendTransferRun.current === clientRunId) {
        activeSendTransferRun.current = 0;
      } else if (!transferStarted && mode === 'receive' && activeReceiveTransferRun.current === clientRunId) {
        activeReceiveTransferRun.current = 0;
      }
      setError(localizeError(String(err)));
    } finally {
      startPendingRef.current = false;
      setStartPending(false);
    }
  }

  async function stop() {
    setError('');
    if (mode === 'vpnClient') {
      vpnStopRequested.current = true;
    }
    const receiveTaskId = receivedDownloadTaskId.current;
    const receiveDownloadWasBusy = mode === 'receive' && receivedDownloadImperativeBusy();
    try {
      let stopFailure: unknown = null;
      try {
        await StopTransfer(mode);
      } catch (err) {
        stopFailure = err;
      }
      if (receiveDownloadWasBusy) {
        await refreshReceivedFilesAfterDownload(receiveTaskId);
      }
      if (stopFailure) {
        throw stopFailure;
      }
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
          setVpnClientSocks5Endpoint('');
          vpnDisconnectNotified.current = false;
          vpnStartedNotified.current = false;
          vpnTunnelWasConnected.current = false;
        }
      }
      await refreshStatus();
    } catch (err) {
      setError(localizeError(String(err)));
    } finally {
      if (mode === 'vpnClient') {
        window.setTimeout(() => {
          vpnStopRequested.current = false;
        }, 1000);
      }
    }
  }

  async function loadRemoteFiles(path = currentRemotePath, silent = false, clearSelection = false) {
    if (!silent) {
      setError('');
      setDownloadError('');
    }
    if (clearSelection) {
      setSelectedPaths(new Set());
      setExcludedPaths(new Set());
    }
    setRemoteListLoading(true);
    try {
      const normalized = normalizeRemotePath(path);
      const list = await RemoteFiles(normalized);
      setCurrentRemotePath(normalized);
      setRemoteList((current) => {
        try {
          return mergeRemoteList(current, list, normalized);
        } catch (mergeError) {
          appendLog('status', 'warn', `文件列表解析失败：${localizeError(String(mergeError))}`);
          return current || recalculateRemoteList('', []);
        }
      });
    } catch (err) {
      if (!silent) {
        setDownloadError(localizeError(String(err)));
      } else {
        appendLog('status', 'warn', `${t.remoteListAutoLoadFailed} ${localizeError(String(err))}`);
      }
    } finally {
      setRemoteListLoading(false);
    }
  }

  function browseRemotePath(path: string) {
    setError('');
    setDownloadError('');
    setCurrentRemotePath(normalizeRemotePath(path));
  }

  function applyVpnProfile(profile: VPNProfile) {
    setVpnClientPassword(profile.passphrase || '');
    setVpnClientUseUDP(Boolean(profile.useUdp));
    setVpnClientEnableIPv6(Boolean(profile.routeIpv6));
    setVpnClientTunnelOnly(Boolean(profile.tunnelOnly));
    setVpnClientDNSServers(profile.dnsServers || defaultVpnDNS);
    setVpnClientRouteCIDRs(profile.routeCidrs || defaultVpnRoutes);
    setVpnClientLinkConfig(profile.linkConfig || '');
    setVpnClientMTU(String(normalizeVpnMTU(profile.mtu)));
    setVpnClientRouteMetric(String(normalizeRouteMetric(profile.routeMetric)));
    setVpnClientBlockDNSLeak(Boolean(profile.blockDnsLeak));
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
      case 'mtu':
        setVpnClientMTU(String(value));
        break;
      case 'routeMetric':
        setVpnClientRouteMetric(String(value));
        break;
      case 'blockDnsLeak':
        setVpnClientBlockDNSLeak(Boolean(value));
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
    setDownloadError('');
    if (selectedPaths.size === 0) {
      setDownloadError(t.noSelection);
      return;
    }
    const taskId = beginReceivedDownloadTask();
    if (taskId === null) {
      return;
    }
    try {
      await StartHTTPDownload(saveDir, '/', Array.from(selectedPaths), Array.from(excludedPaths), remoteFiles as any, downloadMode === 'resume', taskId);
      await refreshStatus();
    } catch (err) {
      abandonReceivedDownloadTask(taskId);
      setDownloadError(`${t.downloadFailed} ${localizeError(String(err))}`);
    }
  }

  async function startDownloadAll() {
    setError('');
    setDownloadError('');
    const taskId = beginReceivedDownloadTask();
    if (taskId === null) {
      return;
    }
    try {
      await StartHTTPDownload(saveDir, currentRemotePath, [], [], [], downloadMode === 'resume', taskId);
      await refreshStatus();
    } catch (err) {
      abandonReceivedDownloadTask(taskId);
      setDownloadError(`${t.downloadFailed} ${localizeError(String(err))}`);
    }
  }

  async function stopDownload() {
    setError('');
    const taskId = receivedDownloadTaskId.current;
    try {
      await StopHTTPDownload();
      await ClearTaskbarProgress().catch((err) => {
        setError(localizeError(String(err)));
      });
      await refreshReceivedFilesAfterDownload(taskId);
    } catch (err) {
      setError(localizeError(String(err)));
    }
  }

  function notifyVpnTunnelPaused() {
    if (!vpnTunnelWasConnected.current || vpnDisconnectNotified.current || !notificationsReady.current) {
      return;
    }
    vpnDisconnectNotified.current = true;
    SendNotification({
      id: `vpn-tunnel-paused-${Date.now()}`,
      title: t.vpnTunnelPausedTitle,
      body: t.vpnTunnelPausedBody,
    }).catch(() => undefined);
  }

  function notifyVpnStarted() {
    if (vpnStartedNotified.current || !notificationsReady.current) {
      return;
    }
    vpnStartedNotified.current = true;
    SendNotification({
      id: `vpn-started-${Date.now()}`,
      title: t.vpnStartedTitle,
      body: t.vpnStartedBody,
    }).catch(() => undefined);
  }

  function notifyVpnTunnelRestored() {
    if (!vpnDisconnectNotified.current || !notificationsReady.current) {
      vpnDisconnectNotified.current = false;
      return;
    }
    vpnDisconnectNotified.current = false;
    SendNotification({
      id: `vpn-tunnel-restored-${Date.now()}`,
      title: t.vpnTunnelRestoredTitle,
      body: t.vpnTunnelRestoredBody,
    }).catch(() => undefined);
  }

  function handleVpnClientTunnelReport(status: string) {
    if (vpnTunnelIsConnected(status)) {
      vpnTunnelWasConnected.current = true;
      vpnStopRequested.current = false;
      if (vpnClientTunnelOnly) {
        notifyVpnTunnelRestored();
      }
      return;
    }
    if (vpnTunnelNeedsNotification(status) && !vpnStopRequested.current) {
      notifyVpnTunnelPaused();
    }
  }

  function restoreAppWindowAfterCapture() {
    try {
      WindowShow();
      WindowUnminimise();
      WindowSetAlwaysOnTop(true);
      window.setTimeout(() => WindowSetAlwaysOnTop(false), 80);
      window.setTimeout(() => {
        WindowShow();
        WindowUnminimise();
        WindowSetAlwaysOnTop(true);
        window.setTimeout(() => WindowSetAlwaysOnTop(false), 80);
      }, 300);
    } catch {
      // Best-effort UI recovery for Linux screenshot portal cancellation.
    }
  }

  function prepareAppWindowForCapture() {
    try {
      WindowMinimise();
    } catch {
      // The backend also moves the window before capture.
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
    const normalized = normalizeRemotePath(path);
    const nextSelected = new Set(selectedPaths);
    const nextExcluded = new Set(excludedPaths);
    if (isEffectivelySelected(normalized, nextSelected, nextExcluded)) {
      nextSelected.delete(normalized);
      if (hasSelectedParent(normalized, nextSelected)) {
        nextExcluded.add(normalized);
      } else {
        nextExcluded.delete(normalized);
      }
    } else {
      nextExcluded.delete(normalized);
      nextSelected.add(normalized);
    }
    setSelectedPaths(nextSelected);
    setExcludedPaths(nextExcluded);
  }

  function selectVisibleRemoteFiles() {
    if (!visibleEntries.length) {
      return;
    }
    const nextSelected = new Set(selectedPaths);
    const nextExcluded = new Set(excludedPaths);
    visibleEntries.forEach((file) => {
      const path = normalizeRemotePath(file.path);
      nextExcluded.delete(path);
      nextSelected.add(path);
    });
    setSelectedPaths(nextSelected);
    setExcludedPaths(nextExcluded);
  }

  function invertVisibleRemoteFiles() {
    if (!visibleEntries.length) {
      return;
    }
    const nextSelected = new Set(selectedPaths);
    const nextExcluded = new Set(excludedPaths);
    visibleEntries.forEach((file) => {
      const path = normalizeRemotePath(file.path);
      if (isEffectivelySelected(path, nextSelected, nextExcluded)) {
        nextSelected.delete(path);
        if (hasSelectedParent(path, nextSelected)) {
          nextExcluded.add(path);
        } else {
          nextExcluded.delete(path);
        }
      } else {
        nextExcluded.delete(path);
        nextSelected.add(path);
      }
    });
    setSelectedPaths(nextSelected);
    setExcludedPaths(nextExcluded);
  }

  function localizeError(message: string) {
    if (message.includes('password is too weak')) {
      return t.weakPassword;
    }
    return message;
  }

  function openAbout() {
    setUpdateState({kind: 'idle'});
    setAboutOpen(true);
  }

  function closeAbout() {
    if (updateState.kind !== 'checking') {
      setAboutOpen(false);
      aboutButtonRef.current?.focus();
    }
  }

  function handleAboutKeyDown(event: ReactKeyboardEvent<HTMLElement>) {
    if (event.key === 'Escape') {
      if (updateState.kind !== 'checking') {
        event.preventDefault();
        closeAbout();
      }
      return;
    }
    if (event.key !== 'Tab') {
      return;
    }

    const dialog = aboutDialogRef.current;
    if (!dialog) {
      return;
    }
    const focusable = Array.from(dialog.querySelectorAll<HTMLElement>('button:not(:disabled), [href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'));
    if (focusable.length === 0) {
      event.preventDefault();
      return;
    }
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && (document.activeElement === dialog || document.activeElement === first || !dialog.contains(document.activeElement))) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && (document.activeElement === dialog || document.activeElement === last)) {
      event.preventDefault();
      first.focus();
    }
  }

  async function checkForUpdates() {
    setUpdateState({kind: 'checking'});
    try {
      const result = await CheckForUpdate(appVersion);
      if (result.updateAvailable && result.downloadUrl) {
        setUpdateState({kind: 'available', latestVersion: result.latestVersion, downloadUrl: result.downloadUrl});
      } else {
        setUpdateState({kind: 'current', latestVersion: result.latestVersion});
      }
    } catch (error) {
      const message = String(error);
      setUpdateState({
        kind: 'error',
        code: message.includes('update_unsupported_platform')
          ? 'platform'
          : message.includes('update_invalid_manifest')
            ? 'manifest'
            : 'network',
      });
    }
  }

  const passphraseField = (
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
          </>
        ) : mode === 'receive' ? (
          <>
            {!receiveRunning && <button className="secondary" onClick={generateReceivePassword}>{t.generate}</button>}
            {!receiveRunning && <button className="secondary" onClick={pastePassword}>{t.paste}</button>}
            {!receiveRunning && <button className="secondary" disabled={scanBusy} onClick={() => startScreenScan()}>{t.scan}</button>}
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
  );

  return (
    <main className="shell">
      <section className="workspace">
        <header className="app-header">
          <div className="brand">
            <img className="brand-mark" src={appIconUrl} alt="" aria-hidden="true" />
            <div>
              <div className="brand-title">
                <h1>{t.brand}</h1>
                <span>{appVersion}</span>
                <button ref={aboutButtonRef} className="about-entry" onClick={openAbout}>{t.about}</button>
              </div>
              <p>{t.subtitle}</p>
            </div>
          </div>
          <div className="header-actions">
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
          </div>
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
                    <button className="secondary" onClick={openSaveDir}>{t.openFolder}</button>
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

            {isFileTransferMode(mode) ? (
              <div className="transfer-setup-grid">
                <div className="transfer-setup-fields">
                  {passphraseField}
                  <label className="check">
                    <input
                      type="checkbox"
                      checked={useUDP}
                      disabled={currentRunning}
                      onChange={(event) => setUseUDP(event.target.checked)}
                    />
                    <span>{t.useUDP}</span>
                  </label>
                </div>
                <div className="transfer-inline-qr-column">
                  <TransferInlineQr
                    passphrase={activePassword}
                    masked={inlineQrShouldMask(mode, sendQrHasConnected, receiveQrHasConnected)}
                    onActivate={showPasswordQr}
                    onError={(message) => setError(localizeError(message))}
                  />
                </div>
              </div>
            ) : passphraseField}

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
                      <label>{t.mtu}</label>
                      <input
                        type="number"
                        min="576"
                        max="9000"
                        step="1"
                        value={vpnClientMTU}
                        disabled={vpnClientRunning || vpnClientTunnelOnly}
                        onChange={(event) => {
                          const value = event.target.value;
                          setVpnClientMTU(value);
                          updateCurrentVpnProfile({mtu: normalizeVpnMTU(value)});
                        }}
                        placeholder={t.mtuPlaceholder}
                      />
                      <div className="field-hint"><p>{t.mtuHint}</p></div>
                    </div>
                    {isWindows && (
                      <div className="field">
                        <label>{t.routeMetric}</label>
                        <input
                          type="number"
                          min="1"
                          max="9999"
                          step="1"
                          value={vpnClientRouteMetric}
                          disabled={vpnClientRunning || vpnClientTunnelOnly}
                          onChange={(event) => {
                            const value = event.target.value;
                            setVpnClientRouteMetric(value);
                            updateCurrentVpnProfile({routeMetric: normalizeRouteMetric(value)});
                          }}
                          placeholder={t.routeMetricPlaceholder}
                        />
                        <div className="field-hint"><p>{t.routeMetricHint}</p></div>
                      </div>
                    )}
                    {isWindows && (
                      <div className="field inline-field">
                        <label>
                          <input
                            type="checkbox"
                            checked={vpnClientBlockDNSLeak}
                            disabled={vpnClientRunning || vpnClientTunnelOnly}
                            onChange={(event) => {
                              const checked = event.target.checked;
                              setVpnClientBlockDNSLeak(checked);
                              updateCurrentVpnProfile({blockDnsLeak: checked});
                            }}
                          />
                          <span>{t.dnsLeakProtection}</span>
                        </label>
                        <div className="field-hint"><p>{t.dnsLeakProtectionHint}</p></div>
                      </div>
                    )}
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
                  <div className="compact-switch" aria-label={t.downloadMode}>
                    <button className={downloadMode === 'resume' ? 'active' : ''} disabled={status.downloading} onClick={() => setDownloadMode('resume')}>{t.resumeDownload}</button>
                    <button className={downloadMode === 'overwrite' ? 'active' : ''} disabled={status.downloading} onClick={() => setDownloadMode('overwrite')}>{t.overwriteDownload}</button>
                  </div>
                  {status.downloading ? (
                    <button className="danger" onClick={stopDownload}>{t.stopDownload}</button>
                  ) : (
                    <>
                      <button className="primary" disabled={!canDownloadAll} onClick={startDownloadAll}>
                        {currentRemotePath === '/' ? t.receiveAll : t.receiveCurrentDir}
                      </button>
                      <button className="secondary" disabled={!canDownload} onClick={startDownload}>{t.downloadSelected}</button>
                    </>
                  )}
                </div>
              </div>
              <div className="remote-summary">
                <div className="remote-breadcrumb" aria-label={t.currentDir}>
                  {currentRemoteBreadcrumbs.map((part, index) => (
                    <button
                      className={index === currentRemoteBreadcrumbs.length - 1 ? 'active' : ''}
                      disabled={index === currentRemoteBreadcrumbs.length - 1 || remoteListLoading}
                      key={part.path}
                      onClick={() => browseRemotePath(part.path)}
                    >
                      {part.name}
                    </button>
                  ))}
                </div>
                <div className="remote-tools">
                  <span>{remoteList ? `${visibleEntries.filter((item) => !item.is_dir).length} ${t.files}` : t.noList}</span>
                  <span>{remoteList ? `${visibleEntries.filter((item) => item.is_dir).length} ${t.folders}` : '-'}</span>
                  <span>
                    {selectedPaths.size} {t.selected}
                    {excludedPaths.size > 0 ? ` · ${excludedPaths.size} ${t.excluded}` : ''}
                    {selectedRemoteBytes > 0 ? ` · ${formatBytes(selectedRemoteBytes)}` : ''}
                  </span>
                  <button className="quiet-action" disabled={!remoteList || visibleEntries.length === 0 || status.downloading} onClick={selectVisibleRemoteFiles}>{t.selectAll}</button>
                  <button className="quiet-action" disabled={!remoteList || visibleEntries.length === 0 || status.downloading} onClick={invertVisibleRemoteFiles}>{t.invertSelection}</button>
                  <button className="quiet-action" disabled={!status.localHTTPUrl || remoteListLoading} onClick={() => loadRemoteFiles(currentRemotePath, false, true)}>{t.refresh}</button>
                </div>
              </div>
              {downloadError && <div className="remote-error">{downloadError}</div>}
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
                    {visibleEntries.map((file) => {
                      const received = file.is_dir ? undefined : receivedLocalFiles.get(normalizeRemotePath(file.path));
                      const local = received?.size === file.size ? received : undefined;
                      return (
                        <div className="remote-row" key={file.path}>
                          <input
                            type="checkbox"
                            checked={isEffectivelySelected(file.path, selectedPaths, excludedPaths)}
                            onChange={() => toggleSelected(file.path)}
                          />
                          <span className={`type-icon ${file.is_dir ? 'folder' : `file ${local ? 'local-available' : ''}`}`} aria-label={file.is_dir ? t.dir : t.file} title={file.is_dir ? t.dir : t.file}>
                            {local && <span className="local-available-dot" aria-hidden="true" />}
                          </span>
                          {file.is_dir ? (
                            <button className="folder-link" onClick={() => browseRemotePath(file.path)}>{file.name}</button>
                          ) : (
                            <strong className={local ? 'local-filename' : ''}>{file.name}</strong>
                          )}
                          <em>{formatModTime(file.mod_time)}</em>
                          <em>{file.is_dir ? '' : formatBytes(file.size)}</em>
                          {local && (
                            <button className="reveal-received-file" aria-label={revealFileLabel} title={revealFileLabel} disabled={receivedActionsUnavailable} onClick={() => revealReceivedFile(file)}>
                              <span className="reveal-folder-icon" aria-hidden="true" />
                            </button>
                          )}
                        </div>
                      );
                    })}
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
              {mode === 'vpnClient' && <Metric label={t.linkConfig} value={vpnClientSocks5Endpoint || '-'} />}
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
      {aboutOpen && (
        <div className="qr-backdrop" role="presentation" onClick={closeAbout}>
          <section ref={aboutDialogRef} className="about-dialog" role="dialog" aria-modal="true" aria-label={t.aboutTitle} tabIndex={-1} onKeyDown={handleAboutKeyDown} onClick={(event) => event.stopPropagation()}>
            <img className="about-mark" src={appIconUrl} alt="" aria-hidden="true" />
            <h2>{t.aboutTitle}</h2>
            <div className="about-version">{t.brand} {appVersion}</div>
            <p>{t.aboutDescription}</p>
            <button className="about-link" onClick={() => BrowserOpenURL(goncSourceUrl)}>{goncSourceUrl}</button>
            <button className="about-link" onClick={() => BrowserOpenURL(guiSourceUrl)}>{guiSourceUrl}</button>
            <div className="about-update" aria-live="polite">
              {updateState.kind === 'current' && <p>{t.upToDate} ({updateState.latestVersion})</p>}
              {updateState.kind === 'available' && <p>{t.updateAvailable}: {updateState.latestVersion}</p>}
              {updateState.kind === 'error' && (
                <p className="about-error">
                  {updateState.code === 'platform' ? t.updatePlatformError : updateState.code === 'manifest' ? t.updateManifestError : t.updateNetworkError}
                </p>
              )}
            </div>
            <div className="about-actions">
              <button className="secondary" disabled={updateState.kind === 'checking'} onClick={checkForUpdates}>
                {updateState.kind === 'checking' ? t.checkingForUpdates : t.checkForUpdates}
              </button>
              {updateState.kind === 'available' && (
                <button className="primary" onClick={() => BrowserOpenURL(updateState.downloadUrl)}>{t.goToDownload}</button>
              )}
              <button className="primary" disabled={updateState.kind === 'checking'} onClick={closeAbout}>{t.close}</button>
            </div>
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
    mtu: defaultVpnMTU,
    routeMetric: defaultVpnRouteMetric,
    blockDnsLeak: false,
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
    mtu: normalizeVpnMTU(profile.mtu),
    routeMetric: normalizeRouteMetric(profile.routeMetric),
    blockDnsLeak: Boolean(profile.blockDnsLeak),
    extraArgs: String(profile.extraArgs || '').trim(),
    tunnelOnly: Boolean(profile.tunnelOnly),
  };
}

function normalizeVpnMTU(value: unknown): number {
  const mtu = typeof value === 'number' ? value : Number(String(value || '').trim());
  if (!Number.isFinite(mtu) || mtu < 576 || mtu > 9000) {
    return defaultVpnMTU;
  }
  return Math.round(mtu);
}

function normalizeRouteMetric(value: unknown): number {
  const metric = typeof value === 'number' ? value : Number(String(value || '').trim());
  if (!Number.isFinite(metric) || metric < 1 || metric > 9999) {
    return defaultVpnRouteMetric;
  }
  return Math.round(metric);
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
  if (clean === 'lan') {
    return t.lanRoute;
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

function captureScreenWithTimeout(timeoutMs: number, timeoutMessage: string) {
  let timer = 0;
  return Promise.race([
    CaptureScreen(),
    new Promise<string>((_resolve, reject) => {
      timer = window.setTimeout(() => reject(new Error(timeoutMessage)), timeoutMs);
    }),
  ]).finally(() => window.clearTimeout(timer));
}

function vpnTunnelIsConnected(status: string) {
  return normalizeP2PStatus(status) === 'connected';
}

function vpnTunnelNeedsNotification(status: string) {
  const normalized = normalizeP2PStatus(status);
  if (['wait', 'waiting', 'idle', 'ready', 'connecting', 'negotiating', 'reconnecting', 'disconnected', 'disconnect', 'closed', 'stopped'].includes(normalized)) {
    return true;
  }
  return normalized.includes('fail') ||
    normalized.includes('error') ||
    normalized.includes('lost') ||
    normalized.includes('timeout');
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
    return {label: t.waitingPeer, tone: 'waiting'};
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

function safeShallowEntries(files: RemoteFile[], currentPath: string): VisibleEntry[] {
  try {
    return shallowEntries(files, currentPath);
  } catch {
    return [];
  }
}

function shallowEntries(files: RemoteFile[], currentPath: string): VisibleEntry[] {
  const current = normalizeRemotePath(currentPath);
  const byPath = new Map<string, VisibleEntry>();
  for (const item of files || []) {
    const file = normalizeRemoteFile(item);
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

function mergeRemoteList(current: RemoteList | null, fresh: RemoteList, refreshPath: string): RemoteList {
  const freshFiles = remoteListFiles(fresh);
  if (!current || normalizeRemotePath(refreshPath) === '/') {
    return recalculateRemoteList(fresh?.serverUrl || current?.serverUrl || '', freshFiles);
  }
  const target = normalizeRemotePath(refreshPath);
  const byPath = new Map<string, RemoteFile>();
  for (const item of remoteListFiles(current)) {
    const file = normalizeRemoteFile(item);
    const path = normalizeRemotePath(file.path);
    if (path === target || path.startsWith(`${target}/`)) {
      continue;
    }
    byPath.set(path, {...file, path});
  }
  for (const item of freshFiles) {
    const file = normalizeRemoteFile(item);
    const path = normalizeRemotePath(file.path);
    byPath.set(path, {...file, path});
  }
  return recalculateRemoteList(fresh?.serverUrl || current.serverUrl, Array.from(byPath.values()));
}

function recalculateRemoteList(serverUrl: string, files: RemoteFile[]): RemoteList {
  let fileCount = 0;
  let dirCount = 0;
  let totalSize = 0;
  const normalizedFiles = (files || []).map(normalizeRemoteFile);
  for (const file of normalizedFiles) {
    if (file.is_dir) {
      dirCount += 1;
    } else {
      fileCount += 1;
      totalSize += file.size || 0;
    }
  }
  return {serverUrl, files: normalizedFiles, fileCount, dirCount, totalSize};
}

function remoteListFiles(list: RemoteList | null | undefined): RemoteFile[] {
  const files = (list as any)?.files ?? (list as any)?.Files;
  return Array.isArray(files) ? files : [];
}

function normalizeRemoteFile(file: RemoteFile): RemoteFile {
  const item = (file || {}) as any;
  const path = normalizeRemotePath(item.path ?? item.Path ?? '/');
  return {
    name: String(item.name ?? item.Name ?? remoteBaseName(path)),
    is_dir: Boolean(item.is_dir ?? item.IsDir),
    mod_time: item.mod_time ?? item.ModTime ?? '',
    size: Number(item.size ?? item.Size ?? 0),
    path,
  };
}

function remoteBaseName(path: string) {
  const normalized = normalizeRemotePath(path);
  if (normalized === '/') {
    return '/';
  }
  return normalized.slice(normalized.lastIndexOf('/') + 1);
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

function remoteBreadcrumbs(value: string) {
  const normalized = normalizeRemotePath(value);
  const parts = normalized === '/' ? [] : normalized.replace(/^\//, '').split('/');
  const breadcrumbs = [{name: '/', path: '/'}];
  let current = '';
  for (const part of parts) {
    current = `${current}/${part}`;
    breadcrumbs.push({name: part, path: normalizeRemotePath(current)});
  }
  return breadcrumbs;
}

function selectedRemoteSize(files: RemoteFile[], selectedPaths: Set<string>, excludedPaths: Set<string>) {
  if (files.length === 0 || selectedPaths.size === 0) {
    return 0;
  }
  const counted = new Set<string>();
  let total = 0;
  for (const file of files) {
    if (file.is_dir) {
      continue;
    }
    const filePath = normalizeRemotePath(file.path);
    if (!isEffectivelySelected(filePath, selectedPaths, excludedPaths) || counted.has(filePath)) {
      continue;
    }
    counted.add(filePath);
    total += file.size || 0;
  }
  return total;
}

function isEffectivelySelected(path: string, selectedPaths: Set<string>, excludedPaths: Set<string>) {
  const normalized = normalizeRemotePath(path);
  const selectedRank = longestRemoteRuleMatch(normalized, selectedPaths);
  if (selectedRank < 0) {
    return false;
  }
  const excludedRank = longestRemoteRuleMatch(normalized, excludedPaths);
  return excludedRank < 0 || selectedRank > excludedRank;
}

function hasSelectedParent(path: string, selectedPaths: Set<string>) {
  const parent = parentPath(path);
  return parent !== path && longestRemoteRuleMatch(parent, selectedPaths) >= 0;
}

function longestRemoteRuleMatch(path: string, rules: Set<string>) {
  const normalized = normalizeRemotePath(path).replace(/\/+$/, '') || '/';
  let best = -1;
  rules.forEach((rule) => {
    const item = normalizeRemotePath(rule).replace(/\/+$/, '') || '/';
    if (item === '/') {
      best = Math.max(best, 0);
      return;
    }
    if (normalized === item || normalized.startsWith(`${item}/`)) {
      best = Math.max(best, item.split('/').filter(Boolean).length + 1);
    }
  });
  return best;
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

