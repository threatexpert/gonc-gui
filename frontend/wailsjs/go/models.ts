export namespace appupdate {
	
	export class Result {
	    currentVersion: string;
	    latestVersion: string;
	    updateAvailable: boolean;
	    downloadUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.updateAvailable = source["updateAvailable"];
	        this.downloadUrl = source["downloadUrl"];
	    }
	}

}

export namespace httpdownload {
	
	export class FileInfo {
	    link_target?: string;
	    name: string;
	    is_dir: boolean;
	    // Go type: time
	    mod_time: any;
	    size: number;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.link_target = source["link_target"];
	        this.name = source["name"];
	        this.is_dir = source["is_dir"];
	        this.mod_time = this.convertValues(source["mod_time"], null);
	        this.size = source["size"];
	        this.path = source["path"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AppStatus {
	    running: boolean;
	    sendRunning: boolean;
	    receiveRunning: boolean;
	    vpnServerRunning: boolean;
	    vpnClientRunning: boolean;
	    localHTTPUrl: string;
	    downloading: boolean;
	    defaultSaveDir: string;
	
	    static createFrom(source: any = {}) {
	        return new AppStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.sendRunning = source["sendRunning"];
	        this.receiveRunning = source["receiveRunning"];
	        this.vpnServerRunning = source["vpnServerRunning"];
	        this.vpnClientRunning = source["vpnClientRunning"];
	        this.localHTTPUrl = source["localHTTPUrl"];
	        this.downloading = source["downloading"];
	        this.defaultSaveDir = source["defaultSaveDir"];
	    }
	}
	export class ReceivedFileActionResult {
	    unavailable: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ReceivedFileActionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.unavailable = source["unavailable"];
	        this.error = source["error"];
	    }
	}
	export class ReceivedFileState {
	    remotePath: string;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ReceivedFileState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.remotePath = source["remotePath"];
	        this.available = source["available"];
	    }
	}
	export class RemoteListResponse {
	    serverUrl: string;
	    files: httpdownload.FileInfo[];
	    fileCount: number;
	    dirCount: number;
	    totalSize: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteListResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverUrl = source["serverUrl"];
	        this.files = this.convertValues(source["files"], httpdownload.FileInfo);
	        this.fileCount = source["fileCount"];
	        this.dirCount = source["dirCount"];
	        this.totalSize = source["totalSize"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TransferRequest {
	    disconnectProtection: boolean;
	    reconnectDns: string;
	    mode: string;
	    clientRunId?: number;
	    password: string;
	    sharePaths: string[];
	    saveDir: string;
	    downloadSubPath: string;
	    useUDP: boolean;
	    p2pWithLan: boolean;
	    upstream: string;
	    dnsForward: string;
	    dnsServers: string;
	    routeCidrs: string;
	    linkConfig: string;
	    mtu: number;
	    routeMetric: number;
	    blockDnsLeak: boolean;
	    enableIpv6: boolean;
	    tunnelOnly: boolean;
	    extraArgs: string;
	
	    static createFrom(source: any = {}) {
	        return new TransferRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.disconnectProtection = source["disconnectProtection"];
	        this.reconnectDns = source["reconnectDns"];
	        this.mode = source["mode"];
	        this.clientRunId = source["clientRunId"];
	        this.password = source["password"];
	        this.sharePaths = source["sharePaths"];
	        this.saveDir = source["saveDir"];
	        this.downloadSubPath = source["downloadSubPath"];
	        this.useUDP = source["useUDP"];
	        this.p2pWithLan = source["p2pWithLan"];
	        this.upstream = source["upstream"];
	        this.dnsForward = source["dnsForward"];
	        this.dnsServers = source["dnsServers"];
	        this.routeCidrs = source["routeCidrs"];
	        this.linkConfig = source["linkConfig"];
	        this.mtu = source["mtu"];
	        this.routeMetric = source["routeMetric"];
	        this.blockDnsLeak = source["blockDnsLeak"];
	        this.enableIpv6 = source["enableIpv6"];
	        this.tunnelOnly = source["tunnelOnly"];
	        this.extraArgs = source["extraArgs"];
	    }
	}

}

export namespace receivecompare {

	export class ActiveReceive {
	    id: number;
	    path: string;
	    checking: boolean;
	    doneBytes: number;
	    totalBytes: number;

	    static createFrom(source: any = {}) {
	        return new ActiveReceive(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.path = source["path"];
	        this.checking = source["checking"];
	        this.doneBytes = source["doneBytes"];
	        this.totalBytes = source["totalBytes"];
	    }
	}
	export class Item {
	    linkTarget?: string;
	    preserveLink?: boolean;
	    id: number;
	    path: string;
	    size: number;
	    localSize?: number;
	    remoteSize?: number;
	    modified: string;
	    localModified: string;
	    remoteModified: string;
	    selected: boolean;
	    outcome: string;
	    error: string;

	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.linkTarget = source["linkTarget"];
	        this.preserveLink = source["preserveLink"];
	        this.id = source["id"];
	        this.path = source["path"];
	        this.size = source["size"];
	        this.localSize = source["localSize"];
	        this.remoteSize = source["remoteSize"];
	        this.modified = source["modified"];
	        this.localModified = source["localModified"];
	        this.remoteModified = source["remoteModified"];
	        this.selected = source["selected"];
	        this.outcome = source["outcome"];
	        this.error = source["error"];
	    }
	}
	export class Snapshot {
	    batchTotal: number;
	    batchFinished: number;
	    activeReceives: ActiveReceive[];
	    id: string;
	    root: string;
	    scope: string;
	    kind: string;
	    phase: string;
	    error: string;
	    remote: number;
	    scanned: number;
	    candidates: number;
	    bytes: number;
	    unchecked: number;
	    warnings: string[];
	    matched: number;
	    selected: number;
	    selectedBytes: number;
	    selectedLocalOnly: number;
	    selectedLocalOnlyBytes: number;
	    selectedReceive: number;
	    selectedReceiveBytes: number;
	    deleted: number;
	    received: number;
	    skipped: number;
	    failed: number;
	    deleteMode: string;
	    page: number;
	    items: Item[];

	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.batchTotal = source["batchTotal"];
	        this.batchFinished = source["batchFinished"];
	        this.activeReceives = this.convertValues(source["activeReceives"], ActiveReceive);
	        this.id = source["id"];
	        this.root = source["root"];
	        this.scope = source["scope"];
	        this.kind = source["kind"];
	        this.phase = source["phase"];
	        this.error = source["error"];
	        this.remote = source["remote"];
	        this.scanned = source["scanned"];
	        this.candidates = source["candidates"];
	        this.bytes = source["bytes"];
	        this.unchecked = source["unchecked"];
	        this.warnings = source["warnings"];
	        this.matched = source["matched"];
	        this.selected = source["selected"];
	        this.selectedBytes = source["selectedBytes"];
	        this.selectedLocalOnly = source["selectedLocalOnly"];
	        this.selectedLocalOnlyBytes = source["selectedLocalOnlyBytes"];
	        this.selectedReceive = source["selectedReceive"];
	        this.selectedReceiveBytes = source["selectedReceiveBytes"];
	        this.deleted = source["deleted"];
	        this.received = source["received"];
	        this.skipped = source["skipped"];
	        this.failed = source["failed"];
	        this.deleteMode = source["deleteMode"];
	        this.page = source["page"];
	        this.items = this.convertValues(source["items"], Item);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace sharecontent {
	
	export class ClipboardResult {
	    Paths: string[];
	    Kind: string;
	
	    static createFrom(source: any = {}) {
	        return new ClipboardResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Paths = source["Paths"];
	        this.Kind = source["Kind"];
	    }
	}

}

export namespace vpnprofile {
	
	export class Profile {
	    disconnectProtection: boolean;
	    reconnectDns: string;
	    name: string;
	    passphrase: string;
	    useUdp: boolean;
	    p2pWithLan: boolean;
	    routeIpv6: boolean;
	    dnsServers: string;
	    routeCidrs: string;
	    linkConfig: string;
	    mtu: number;
	    routeMetric: number;
	    blockDnsLeak: boolean;
	    extraArgs: string;
	    tunnelOnly: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.disconnectProtection = source["disconnectProtection"];
	        this.reconnectDns = source["reconnectDns"];
	        this.name = source["name"];
	        this.passphrase = source["passphrase"];
	        this.useUdp = source["useUdp"];
	        this.p2pWithLan = source["p2pWithLan"];
	        this.routeIpv6 = source["routeIpv6"];
	        this.dnsServers = source["dnsServers"];
	        this.routeCidrs = source["routeCidrs"];
	        this.linkConfig = source["linkConfig"];
	        this.mtu = source["mtu"];
	        this.routeMetric = source["routeMetric"];
	        this.blockDnsLeak = source["blockDnsLeak"];
	        this.extraArgs = source["extraArgs"];
	        this.tunnelOnly = source["tunnelOnly"];
	    }
	}
	export class Store {
	    version: number;
	    selected: number;
	    profiles: Profile[];
	
	    static createFrom(source: any = {}) {
	        return new Store(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.selected = source["selected"];
	        this.profiles = this.convertValues(source["profiles"], Profile);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

