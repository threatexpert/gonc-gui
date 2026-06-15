export namespace main {
	
	export class AppStatus {
	    running: boolean;
	    goncPath: string;
	
	    static createFrom(source: any = {}) {
	        return new AppStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.goncPath = source["goncPath"];
	    }
	}
	export class TransferRequest {
	    mode: string;
	    password: string;
	    sharePaths: string[];
	    saveDir: string;
	    goncPath: string;
	    downloadSubPath: string;
	    noCompress: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TransferRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.password = source["password"];
	        this.sharePaths = source["sharePaths"];
	        this.saveDir = source["saveDir"];
	        this.goncPath = source["goncPath"];
	        this.downloadSubPath = source["downloadSubPath"];
	        this.noCompress = source["noCompress"];
	    }
	}

}

