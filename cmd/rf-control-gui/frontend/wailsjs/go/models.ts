export namespace gui {

	export class CWRequest {
	    frequencyMHz: number;
	    attenuation: number;
	    clock: string;

	    static createFrom(source: any = {}) {
	        return new CWRequest(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.frequencyMHz = source["frequencyMHz"];
	        this.attenuation = source["attenuation"];
	        this.clock = source["clock"];
	    }
	}
	export class DeviceStatus {
	    boardType: string;
	    boardLabel: string;
	    barracuda: boolean;
	    mode: string;
	    ifFrequencyMHz: number;
	    sweepStopIfMHz: number;
	    sweepTime: string;
	    clock: string;
	    referenceLockApplicable: boolean;
	    referenceLocked: boolean;
	    signalLockApplicable: boolean;
	    signalLocked: boolean;
	    attenuationDb: number;
	    maximumAttenuation: boolean;
	    outputEstimateAvailable: boolean;
	    nominalOutputDbm: number;
	    temperatureAvailable: boolean;
	    temperatureC: number;
	    temperatureBootSample: boolean;
	    channelsEnabled: boolean;
	    calibrationEnabled: boolean;
	    calSourceInternal: boolean;
	    calAttenuationDb: number;
	    loFrequencyMHz: number;
	    rfSwitch: string;
	    mixerSwitch: string;
	    ifSwitch: string;
	    rfSwitchChannel: number;

	    static createFrom(source: any = {}) {
	        return new DeviceStatus(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.boardType = source["boardType"];
	        this.boardLabel = source["boardLabel"];
	        this.barracuda = source["barracuda"];
	        this.mode = source["mode"];
	        this.ifFrequencyMHz = source["ifFrequencyMHz"];
	        this.sweepStopIfMHz = source["sweepStopIfMHz"];
	        this.sweepTime = source["sweepTime"];
	        this.clock = source["clock"];
	        this.referenceLockApplicable = source["referenceLockApplicable"];
	        this.referenceLocked = source["referenceLocked"];
	        this.signalLockApplicable = source["signalLockApplicable"];
	        this.signalLocked = source["signalLocked"];
	        this.attenuationDb = source["attenuationDb"];
	        this.maximumAttenuation = source["maximumAttenuation"];
	        this.outputEstimateAvailable = source["outputEstimateAvailable"];
	        this.nominalOutputDbm = source["nominalOutputDbm"];
	        this.temperatureAvailable = source["temperatureAvailable"];
	        this.temperatureC = source["temperatureC"];
	        this.temperatureBootSample = source["temperatureBootSample"];
	        this.channelsEnabled = source["channelsEnabled"];
	        this.calibrationEnabled = source["calibrationEnabled"];
	        this.calSourceInternal = source["calSourceInternal"];
	        this.calAttenuationDb = source["calAttenuationDb"];
	        this.loFrequencyMHz = source["loFrequencyMHz"];
	        this.rfSwitch = source["rfSwitch"];
	        this.mixerSwitch = source["mixerSwitch"];
	        this.ifSwitch = source["ifSwitch"];
	        this.rfSwitchChannel = source["rfSwitchChannel"];
	    }
	}
	export class NetworkConfig {
	    ipAddress: string;
	    gateway: string;
	    subnet: string;
	    hostname: string;
	    macAddress: string;
	    serial: string;
	    firmware: string;
	    boardId: string;

	    static createFrom(source: any = {}) {
	        return new NetworkConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ipAddress = source["ipAddress"];
	        this.gateway = source["gateway"];
	        this.subnet = source["subnet"];
	        this.hostname = source["hostname"];
	        this.macAddress = source["macAddress"];
	        this.serial = source["serial"];
	        this.firmware = source["firmware"];
	        this.boardId = source["boardId"];
	    }
	}
	export class Endpoint {
	    kind: string;
	    address: string;
	    port: number;

	    static createFrom(source: any = {}) {
	        return new Endpoint(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.address = source["address"];
	        this.port = source["port"];
	    }
	}
	export class DeviceSnapshot {
	    connected: boolean;
	    endpoint: Endpoint;
	    network: NetworkConfig;
	    status: DeviceStatus;
	    customerControl: boolean;

	    static createFrom(source: any = {}) {
	        return new DeviceSnapshot(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.endpoint = this.convertValues(source["endpoint"], Endpoint);
	        this.network = this.convertValues(source["network"], NetworkConfig);
	        this.status = this.convertValues(source["status"], DeviceStatus);
	        this.customerControl = source["customerControl"];
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

	export class DiscoveredDevice {
	    id: string;
	    name: string;
	    boardType: string;
	    serial: string;
	    firmware: string;
	    ipAddress: string;
	    macAddress: string;
	    connections: Endpoint[];

	    static createFrom(source: any = {}) {
	        return new DiscoveredDevice(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.boardType = source["boardType"];
	        this.serial = source["serial"];
	        this.firmware = source["firmware"];
	        this.ipAddress = source["ipAddress"];
	        this.macAddress = source["macAddress"];
	        this.connections = this.convertValues(source["connections"], Endpoint);
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
	export class DiscoveryResult {
	    devices: DiscoveredDevice[];
	    warnings: string[];

	    static createFrom(source: any = {}) {
	        return new DiscoveryResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.devices = this.convertValues(source["devices"], DiscoveredDevice);
	        this.warnings = source["warnings"];
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

	export class NetworkPlan {
	    ipAddress: string;
	    gateway: string;
	    subnet: string;

	    static createFrom(source: any = {}) {
	        return new NetworkPlan(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ipAddress = source["ipAddress"];
	        this.gateway = source["gateway"];
	        this.subnet = source["subnet"];
	    }
	}
	export class NetworkChangeResult {
	    plan: NetworkPlan;
	    rebooting: boolean;

	    static createFrom(source: any = {}) {
	        return new NetworkChangeResult(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.plan = this.convertValues(source["plan"], NetworkPlan);
	        this.rebooting = source["rebooting"];
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


	export class SweepRequest {
	    startMHz: number;
	    stopMHz: number;
	    sweepTime: string;
	    attenuation: number;
	    clock: string;

	    static createFrom(source: any = {}) {
	        return new SweepRequest(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.startMHz = source["startMHz"];
	        this.stopMHz = source["stopMHz"];
	        this.sweepTime = source["sweepTime"];
	        this.attenuation = source["attenuation"];
	        this.clock = source["clock"];
	    }
	}

}
