export namespace httpdownload {
	
	export class FileInfo {
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
	    mode: string;
	    password: string;
	    sharePaths: string[];
	    saveDir: string;
	    downloadSubPath: string;
	    useUDP: boolean;
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
	        this.mode = source["mode"];
	        this.password = source["password"];
	        this.sharePaths = source["sharePaths"];
	        this.saveDir = source["saveDir"];
	        this.downloadSubPath = source["downloadSubPath"];
	        this.useUDP = source["useUDP"];
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

export namespace vpnprofile {
	
	export class Profile {
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
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.passphrase = source["passphrase"];
	        this.useUdp = source["useUdp"];
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

