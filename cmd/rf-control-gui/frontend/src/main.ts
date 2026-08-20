import './style.css';

import {
  ConfigureCW,
  ConfigureSweep,
  Connect,
  Disconnect,
  Discover,
  GetStatus,
  LoadTuningProfile,
  PreviewNetwork,
  SaveTuningProfile,
  SetIPAddress,
  Version,
} from '../wailsjs/go/main/App';
import { gui as GoModels } from '../wailsjs/go/models';

type Endpoint = { kind: 'usb' | 'ethernet'; address: string; port: number };
type DiscoveredDevice = {
  id: string;
  name: string;
  boardType: string;
  serial: string;
  firmware: string;
  ipAddress: string;
  macAddress: string;
  connections: Endpoint[];
};
type DiscoveryResult = { devices: DiscoveredDevice[]; warnings: string[]; timedOut: boolean };
type NetworkConfig = {
  ipAddress: string;
  gateway: string;
  subnet: string;
  hostname: string;
  macAddress: string;
  serial: string;
  firmware: string;
  boardId: string;
};
type DeviceStatus = {
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
  rfEnabled: boolean;
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
};
type Snapshot = {
  connected: boolean;
  endpoint: Endpoint;
  network: NetworkConfig;
  status: DeviceStatus;
  customerControl: boolean;
};
type NetworkPlan = { ipAddress: string; gateway: string; subnet: string };
type BarracudaTuningProfile = {
  mode: 'cw' | 'sweep';
  if_frequency_mhz?: number;
  start_if_mhz?: number;
  stop_if_mhz?: number;
  sweep_time?: string;
  attenuation_db: number;
  clock: 'internal' | 'external';
  rf_enabled: boolean;
};
type TuningProfile = { barracuda: BarracudaTuningProfile };
type Tab = 'control' | 'status' | 'network';

const scanTimeoutMs = 5000;
const nominalMaximumOutputDbm = -25;

const state = {
  appVersion: 'dev',
  discovery: { devices: [], warnings: [], timedOut: false } as DiscoveryResult,
  snapshot: null as Snapshot | null,
  tab: 'control' as Tab,
  busy: false,
  scanning: false,
  scanMessage: 'Press Scan for devices to begin',
  scanKind: 'idle' as 'idle' | 'working' | 'success' | 'empty' | 'error',
  notice: '' as string,
  noticeKind: 'info' as 'info' | 'success' | 'error',
  control: {
    mode: 'cw' as 'cw' | 'sweep',
    cwMHz: 400,
    startMHz: 50,
    stopMHz: 1500,
    sweepTime: '10s',
    outputPowerDbm: nominalMaximumOutputDbm,
    clock: 'internal' as 'internal' | 'external',
    rfEnabled: true,
  },
  networkInput: '',
  networkPlan: null as NetworkPlan | null,
  networkError: '',
};

const app = document.querySelector<HTMLDivElement>('#app')!;

const escapeHTML = (value: unknown): string => String(value ?? '')
  .replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;')
  .replaceAll('"', '&quot;').replaceAll("'", '&#039;');

const errorText = (error: unknown): string => {
  if (error instanceof Error) return error.message;
  if (typeof error === 'string') return error;
  return JSON.stringify(error);
};

const normalizeDiscoveryResult = (result: DiscoveryResult): DiscoveryResult => ({
  devices: Array.isArray(result?.devices) ? result.devices : [],
  warnings: Array.isArray(result?.warnings) ? result.warnings : [],
  timedOut: Boolean(result?.timedOut),
});

const endpointLabel = (endpoint: Endpoint): string => endpoint.kind === 'usb'
  ? `USB-C · ${endpoint.address}`
  : `Ethernet · ${endpoint.address}:${endpoint.port || 5000}`;

const connectionMatches = (left: Endpoint, right: Endpoint): boolean =>
  left.kind === right.kind && left.address === right.address && (left.port || 5000) === (right.port || 5000);

const lockChip = (label: string, applicable: boolean, locked: boolean): string => {
  if (!applicable) return '';
  const kind = locked ? 'good' : 'bad';
  const text = locked ? 'Locked' : 'Not locked';
  return `<span class="chip ${kind}"><i></i>${escapeHTML(label)}: ${text}</span>`;
};

const statusStrip = (status: DeviceStatus): string => `
  <div class="status-strip">
    <span class="chip good"><i></i>Connected</span>
    ${status.barracuda ? `<span class="chip ${status.rfEnabled ? 'good' : 'bad'}"><i></i>RF: ${status.rfEnabled ? 'On' : 'Off'}</span>` : ''}
    ${lockChip('Signal', status.signalLockApplicable, status.signalLocked)}
    ${lockChip('External reference', status.referenceLockApplicable, status.referenceLocked)}
    ${status.temperatureAvailable ? `<span class="chip neutral">${status.temperatureC.toFixed(1)} °C${status.temperatureBootSample ? ' boot sample' : ''}</span>` : ''}
  </div>`;

function renderSidebar(): string {
  const devices = state.discovery.devices.map((device) => `
    <article class="device-card">
      <div class="device-title">
        <span class="device-icon">RF</span>
        <div>
          <strong>${escapeHTML(device.name || boardName(device.boardType))}</strong>
          <small>${escapeHTML(boardName(device.boardType))}${device.serial ? ` · ${escapeHTML(device.serial)}` : ''}</small>
        </div>
      </div>
      <div class="device-meta">
        ${device.ipAddress ? `<span>${escapeHTML(device.ipAddress)}</span>` : ''}
        ${device.firmware ? `<span>FW ${escapeHTML(device.firmware)}</span>` : ''}
      </div>
      <div class="connection-list">
        ${device.connections.map((endpoint, endpointIndex) => {
          const selected = state.snapshot && connectionMatches(state.snapshot.endpoint, endpoint);
          return `<button class="connection ${selected ? 'selected' : ''}" data-connect="${escapeHTML(device.id)}" data-endpoint="${endpointIndex}" ${state.busy ? 'disabled' : ''}>
            <span>${escapeHTML(endpointLabel(endpoint))}</span><b>${selected ? 'Active' : 'Connect'}</b>
          </button>`;
        }).join('')}
      </div>
    </article>`).join('');

  const warnings = state.discovery.warnings.map((warning) => `<p class="sidebar-warning">${escapeHTML(warning)}</p>`).join('');
  return `
    <aside class="sidebar">
      <div class="brand"><img class="brand-logo" src="/logo.svg" alt="Ocupoint"><div><strong>OCUPOINT</strong><span>RF CONTROL</span></div></div>
      <div class="sidebar-heading"><span>Devices</span><button class="refresh-button ${state.scanning ? 'scanning' : ''}" id="refresh-devices" title="Refresh devices" ${state.busy ? 'disabled' : ''}><span class="refresh-icon">↻</span><span>${state.scanning ? 'Scanning…' : 'Refresh'}</span></button></div>
      <div class="scan-feedback ${state.scanKind}" role="status" aria-live="polite"><span></span>${escapeHTML(state.scanMessage)}</div>
      <div class="devices">${devices || '<div class="empty-small">No devices discovered</div>'}</div>
      ${warnings}
      <form class="manual-connect" id="manual-connect">
        <label for="manual-address">Connect directly</label>
        <div><input id="manual-address" placeholder="COM5 or 192.168.0.246" required><button ${state.busy ? 'disabled' : ''}>Connect</button></div>
        <small>Enter a Windows COM port or device IP address.</small>
      </form>
      <div class="sidebar-footer"><span class="app-version">Version ${escapeHTML(state.appVersion)}</span></div>
    </aside>`;
}

function renderEmpty(): string {
  return `
    <main class="empty-workspace">
      <div class="signal-art" aria-hidden="true"><span></span><span></span><span></span><span></span></div>
      <p class="eyebrow">Hardware control</p>
      <h1>Select an RF device</h1>
      <p>Enter a known Ethernet address, or scan for a USB-C or Ethernet device.</p>
      <button class="primary" id="empty-refresh" ${state.busy ? 'disabled' : ''}>${state.scanning ? 'Scanning USB and Ethernet…' : 'Scan for devices'}</button>
    </main>`;
}

function renderHeader(snapshot: Snapshot): string {
  const network = snapshot.network;
  const status = snapshot.status;
  return `
    <header class="workspace-header">
      <div>
        <p class="eyebrow">${escapeHTML(endpointLabel(snapshot.endpoint))}</p>
        <h1>${escapeHTML(network.hostname || status.boardLabel)}</h1>
        <p>${escapeHTML(status.boardLabel)}${network.serial ? ` · S/N ${escapeHTML(network.serial)}` : ''}${network.firmware ? ` · Firmware ${escapeHTML(network.firmware)}` : ''}</p>
      </div>
      <button class="secondary" id="disconnect-device" ${state.busy ? 'disabled' : ''}>Disconnect</button>
    </header>
    ${statusStrip(status)}`;
}

function renderTabs(snapshot: Snapshot): string {
  const tabs: Array<[Tab, string]> = snapshot.customerControl
    ? [['control', 'RF Control'], ['status', 'Status'], ['network', 'Network']]
    : [['status', 'Status'], ['network', 'Network']];
  if (!snapshot.customerControl && state.tab === 'control') state.tab = 'status';
  return `<nav class="tabs">${tabs.map(([id, label]) => `<button data-tab="${id}" class="${state.tab === id ? 'active' : ''}">${label}</button>`).join('')}</nav>`;
}

function renderControl(snapshot: Snapshot): string {
  const control = state.control;
  const rfPending = control.rfEnabled !== snapshot.status.rfEnabled;
  return `
    <section class="content control-layout">
      <div class="control-card">
      <div class="section-intro"><h2>Mode</h2><label class="rf-slider-control ${control.rfEnabled ? 'on' : 'off'} ${rfPending ? 'pending' : ''}"><span><b>RF ${control.rfEnabled ? 'ON' : 'OFF'}</b><small>${rfPending ? 'Change pending' : 'Applied state'}</small></span><input id="rf-enabled" type="checkbox" ${control.rfEnabled ? 'checked' : ''} ${state.busy ? 'disabled' : ''}><i aria-hidden="true"></i></label></div>
        <div class="segment" role="group" aria-label="RF mode"><button data-mode="cw" class="${control.mode === 'cw' ? 'active' : ''}">CW</button><button data-mode="sweep" class="${control.mode === 'sweep' ? 'active' : ''}">Sweep</button></div>
        <form id="rf-form">
          ${control.mode === 'cw' ? `
            <div class="field"><label for="cw-frequency">RF frequency</label><div class="input-unit"><input id="cw-frequency" type="number" min="50" max="1500" step="1" value="${control.cwMHz}" required><span>MHz</span></div><small>Allowed range: 50–1500 MHz RF</small></div>` : `
            <div class="field-row">
              <div class="field"><label for="sweep-start">Start frequency</label><div class="input-unit"><input id="sweep-start" type="number" min="50" max="1499" step="1" value="${control.startMHz}" required><span>MHz</span></div></div>
              <div class="field"><label for="sweep-stop">Stop frequency</label><div class="input-unit"><input id="sweep-stop" type="number" min="51" max="1500" step="1" value="${control.stopMHz}" required><span>MHz</span></div></div>
              <div class="field"><label for="sweep-time">Sweep time</label><input id="sweep-time" value="${escapeHTML(control.sweepTime)}" placeholder="10s" required><small>Examples: 10s, 20ms, 35us</small></div>
            </div>`}
          <div class="field-row output-settings">
            <div class="field"><label for="output-power">Output power</label><div class="input-unit"><input id="output-power" type="number" min="-56" max="-25" step="0.25" value="${control.outputPowerDbm}" required><span>dBm</span></div><small>−25 to −56 dBm in 0.25 dB steps; nominal −25 dBm at maximum output.</small></div>
            <fieldset class="field"><legend>Clock source</legend><div class="radio-row"><label><input type="radio" name="clock" value="internal" ${control.clock === 'internal' ? 'checked' : ''}>Internal</label><label><input type="radio" name="clock" value="external" ${control.clock === 'external' ? 'checked' : ''}>External reference</label></div><small>External mode must lock before RF is enabled.</small></fieldset>
          </div>
          <div class="form-actions"><button class="primary large" type="submit" ${state.busy ? 'disabled' : ''}>${state.busy ? 'Applying…' : 'Apply'}</button><div class="profile-actions"><button class="secondary" type="button" id="load-tuning" ${state.busy ? 'disabled' : ''}>Load</button><button class="secondary export-profile" type="button" id="export-tuning" ${state.busy ? 'disabled' : ''}>Export</button></div><small>Export saves every setting currently shown as CLI-ready JSON. Loading a profile does not tune hardware until Apply.</small></div>
        </form>
      </div>
      <aside class="live-panel"><p class="eyebrow">Live state</p><h3>${snapshot.status.mode ? snapshot.status.mode.toUpperCase() : 'Not configured'}</h3>${renderBarracudaFacts(snapshot.status)}</aside>
    </section>`;
}

function renderBarracudaFacts(status: DeviceStatus): string {
  const frequency = status.ifFrequencyMHz ? `${status.ifFrequencyMHz}${status.sweepStopIfMHz ? `–${status.sweepStopIfMHz}` : ''} MHz RF` : 'Unavailable';
  return `<dl class="facts">
    <div><dt>Frequency</dt><dd>${escapeHTML(frequency)}</dd></div>
    ${status.sweepTime ? `<div><dt>Sweep time</dt><dd>${escapeHTML(status.sweepTime)}</dd></div>` : ''}
    <div><dt>Clock</dt><dd>${escapeHTML(status.clock || 'internal')}</dd></div>
    <div><dt>Nominal output</dt><dd>${status.outputEstimateAvailable ? `${status.nominalOutputDbm.toFixed(2)} dBm` : 'Unavailable'}</dd></div>
    <div><dt>Signal lock</dt><dd class="${status.signalLocked ? 'text-good' : 'text-bad'}">${status.signalLocked ? 'Locked' : 'Not locked'}</dd></div>
    ${status.referenceLockApplicable ? `<div><dt>Reference lock</dt><dd class="${status.referenceLocked ? 'text-good' : 'text-bad'}">${status.referenceLocked ? 'Locked' : 'Not locked'}</dd></div>` : ''}
  </dl>`;
}

function renderStatus(snapshot: Snapshot): string {
  const status = snapshot.status;
  const boardSpecific = status.barracuda ? renderBarracudaFacts(status) : renderGenericFacts(status);
  return `
    <section class="content">
      <div class="section-intro"><div><p class="eyebrow">Read-only telemetry</p><h2>Device status</h2></div><button class="secondary" id="refresh-status" ${state.busy ? 'disabled' : ''}>Refresh now</button></div>
      <div class="status-grid">
        <article class="panel"><h3>${escapeHTML(status.boardLabel)}</h3>${boardSpecific}</article>
        <article class="panel"><h3>Device identity</h3><dl class="facts">${identityFacts(snapshot.network)}</dl></article>
      </div>
      ${!status.barracuda ? '<div class="callout">This release provides discovery, status, and network configuration for this board. Product-specific output controls remain available through the CLI.</div>' : ''}
    </section>`;
}

function renderGenericFacts(status: DeviceStatus): string {
  if (status.boardType === 'rf_switch') {
    return `<dl class="facts"><div><dt>RF switch channel</dt><dd>${status.rfSwitchChannel === 0 ? 'Off / isolated' : status.rfSwitchChannel}</dd></div></dl>`;
  }
  if (status.boardType === 'whalepod' || status.boardType === 'whalepod_automation') {
    return `<dl class="facts">
      <div><dt>Channels</dt><dd>${status.channelsEnabled ? 'Enabled' : 'Disabled'}</dd></div>
      <div><dt>Frontend attenuation</dt><dd>${status.attenuationDb.toFixed(0)} dB</dd></div>
      <div><dt>Calibration</dt><dd>${status.calibrationEnabled ? 'Enabled' : 'Disabled'}</dd></div>
      <div><dt>Calibration source</dt><dd>${status.calSourceInternal ? 'Internal' : 'External'}</dd></div>
      <div><dt>Calibration attenuation</dt><dd>${status.calAttenuationDb} dB</dd></div>
    </dl>`;
  }
  return `<dl class="facts">
    <div><dt>Channels</dt><dd>${status.channelsEnabled ? 'Enabled' : 'Disabled'}</dd></div>
    <div><dt>Attenuation</dt><dd>${status.attenuationDb.toFixed(0)} dB</dd></div>
    <div><dt>LO frequency</dt><dd>${status.loFrequencyMHz || 'Unavailable'}${status.loFrequencyMHz ? ' MHz' : ''}</dd></div>
    <div><dt>RF switch</dt><dd>${escapeHTML(cleanEnum(status.rfSwitch))}</dd></div>
    <div><dt>Mixer switch</dt><dd>${escapeHTML(cleanEnum(status.mixerSwitch))}</dd></div>
    <div><dt>IF switch</dt><dd>${escapeHTML(cleanEnum(status.ifSwitch))}</dd></div>
  </dl>`;
}

function identityFacts(network: NetworkConfig): string {
  const rows: Array<[string, string]> = [
    ['Serial number', network.serial || 'Not set'], ['Firmware', network.firmware || 'Unavailable'],
    ['MAC address', network.macAddress || 'Unavailable'], ['Board ID', network.boardId || 'Unavailable'],
  ];
  return rows.map(([key, value]) => `<div><dt>${key}</dt><dd>${escapeHTML(value)}</dd></div>`).join('');
}

function renderNetwork(snapshot: Snapshot): string {
  const network = snapshot.network;
  const plan = state.networkPlan;
  return `
    <section class="content network-layout">
      <div class="section-intro"><div><p class="eyebrow">Static network</p><h2>Network configuration</h2></div><span class="connection-note">Connected via ${escapeHTML(snapshot.endpoint.kind === 'usb' ? 'USB-C' : 'Ethernet')}</span></div>
      ${snapshot.endpoint.kind !== 'usb' ? '<div class="callout warning">USB-C is recommended when changing the IP address. An Ethernet change will intentionally end this connection.</div>' : ''}
      <div class="network-grid">
        <article class="panel"><h3>Current settings</h3><dl class="facts">
          <div><dt>IP address</dt><dd>${escapeHTML(network.ipAddress || 'Unavailable')}</dd></div>
          <div><dt>Gateway</dt><dd>${escapeHTML(network.gateway || 'Unavailable')}</dd></div>
          <div><dt>Subnet</dt><dd>${escapeHTML(network.subnet || 'Unavailable')}</dd></div>
          <div><dt>Hostname</dt><dd>${escapeHTML(network.hostname || 'Not set')}</dd></div>
        </dl></article>
        <article class="panel edit-network"><h3>Assign a new address</h3><p>Enter only the device IP. The customer network plan uses a /24 subnet and gateway at <code>.1</code>.</p>
          <div class="field"><label for="new-ip">New device IP</label><input id="new-ip" inputmode="decimal" value="${escapeHTML(state.networkInput)}" placeholder="192.168.50.25"></div>
          ${state.networkError ? `<p class="validation-error">${escapeHTML(state.networkError)}</p>` : ''}
          <dl class="network-preview">
            <div><dt>Device IP</dt><dd>${escapeHTML(plan?.ipAddress || '—')}</dd></div>
            <div><dt>Gateway</dt><dd>${escapeHTML(plan?.gateway || '—')}</dd></div>
            <div><dt>Subnet</dt><dd>${escapeHTML(plan?.subnet || '—')}</dd></div>
          </dl>
          <button class="primary large" id="apply-ip" ${!plan || state.busy ? 'disabled' : ''}>Apply and reboot</button>
          <small>Hostname, MAC address, and serial number are preserved.</small>
        </article>
      </div>
    </section>`;
}

function renderWorkspace(snapshot: Snapshot): string {
  let body = '';
  switch (state.tab) {
    case 'control': body = renderControl(snapshot); break;
    case 'network': body = renderNetwork(snapshot); break;
    default: body = renderStatus(snapshot);
  }
  return `<main class="workspace">${renderHeader(snapshot)}${renderTabs(snapshot)}${body}</main>`;
}

function render(): void {
  app.innerHTML = `
    <div class="shell">${renderSidebar()}${state.snapshot ? renderWorkspace(state.snapshot) : renderEmpty()}</div>
    ${state.notice ? `<div class="toast ${state.noticeKind}">${escapeHTML(state.notice)}</div>` : ''}
    ${state.busy ? '<div class="busy-line"></div>' : ''}`;
  bindEvents();
}

function bindEvents(): void {
  document.querySelector('#refresh-devices')?.addEventListener('click', () => void discoverDevices());
  document.querySelector('#empty-refresh')?.addEventListener('click', () => void discoverDevices());
  document.querySelector('#disconnect-device')?.addEventListener('click', () => void disconnectDevice());
  document.querySelector('#refresh-status')?.addEventListener('click', () => void refreshStatus(true));

  document.querySelectorAll<HTMLElement>('[data-tab]').forEach((button) => button.addEventListener('click', () => {
    state.tab = button.dataset.tab as Tab;
    render();
  }));
  document.querySelectorAll<HTMLElement>('[data-mode]').forEach((button) => button.addEventListener('click', () => {
    state.control.mode = button.dataset.mode as 'cw' | 'sweep';
    render();
  }));
  document.querySelectorAll<HTMLButtonElement>('[data-connect]').forEach((button) => button.addEventListener('click', () => {
    const device = state.discovery.devices.find((item) => item.id === button.dataset.connect);
    const endpoint = device?.connections[Number(button.dataset.endpoint)];
    if (endpoint) void connectDevice(endpoint);
  }));

  document.querySelector<HTMLFormElement>('#manual-connect')?.addEventListener('submit', (event) => {
    event.preventDefault();
    const address = (document.querySelector<HTMLInputElement>('#manual-address')?.value || '').trim();
    if (!address) return;
    const usb = /^COM\d+$/i.test(address);
    void connectDevice({ kind: usb ? 'usb' : 'ethernet', address: usb ? address.toUpperCase() : address, port: usb ? 0 : 5000 });
  });

  document.querySelector<HTMLFormElement>('#rf-form')?.addEventListener('submit', (event) => {
    event.preventDefault();
    readControlInputs();
    void applyRFControl();
  });
  document.querySelector<HTMLInputElement>('#rf-enabled')?.addEventListener('change', (event) => {
    state.control.rfEnabled = (event.target as HTMLInputElement).checked;
    render();
  });
  document.querySelector('#load-tuning')?.addEventListener('click', () => void loadTuningProfile());
  document.querySelector('#export-tuning')?.addEventListener('click', () => void exportTuningProfile());
  document.querySelectorAll<HTMLInputElement>('input[name="clock"]').forEach((input) => input.addEventListener('change', () => {
    state.control.clock = input.value as 'internal' | 'external';
  }));

  let previewTimer = 0;
  document.querySelector<HTMLInputElement>('#new-ip')?.addEventListener('input', (event) => {
    state.networkInput = (event.target as HTMLInputElement).value.trim();
    window.clearTimeout(previewTimer);
    previewTimer = window.setTimeout(() => void updateNetworkPreview(), 180);
  });
  document.querySelector('#apply-ip')?.addEventListener('click', () => void applyIPAddress());
}

function readControlInputs(): void {
  const numberValue = (selector: string, fallback: number): number => {
    const input = document.querySelector<HTMLInputElement>(selector);
    return input ? Number(input.value) : fallback;
  };
  state.control.outputPowerDbm = numberValue('#output-power', state.control.outputPowerDbm);
  state.control.rfEnabled = document.querySelector<HTMLInputElement>('#rf-enabled')?.checked ?? state.control.rfEnabled;
  if (state.control.mode === 'cw') {
    state.control.cwMHz = numberValue('#cw-frequency', state.control.cwMHz);
  } else {
    state.control.startMHz = numberValue('#sweep-start', state.control.startMHz);
    state.control.stopMHz = numberValue('#sweep-stop', state.control.stopMHz);
    state.control.sweepTime = document.querySelector<HTMLInputElement>('#sweep-time')?.value.trim() || state.control.sweepTime;
  }
}

async function withAction<T>(message: string, action: () => Promise<T>): Promise<T | null> {
  if (state.busy) return null;
  state.busy = true;
  state.notice = '';
  render();
  try {
    const result = await action();
    state.notice = message;
    state.noticeKind = 'success';
    window.setTimeout(clearNotice, 4500);
    return result;
  } catch (error) {
    state.notice = errorText(error);
    state.noticeKind = 'error';
    window.setTimeout(clearNotice, 7000);
    return null;
  } finally {
    state.busy = false;
    render();
  }
}

function clearNotice(): void {
  if (!state.notice) return;
  state.notice = '';
  render();
}

async function discoverWithTimeout(): Promise<DiscoveryResult> {
  let timeoutHandle = 0;
  try {
    return await Promise.race([
      Discover() as Promise<DiscoveryResult>,
      new Promise<never>((_resolve, reject) => {
        timeoutHandle = window.setTimeout(() => {
          reject(new Error('Scan timed out after 5 seconds. USB control requires a COM port; a device using the WinUSB driver cannot be opened as a serial control port.'));
        }, scanTimeoutMs);
      }),
    ]);
  } finally {
    window.clearTimeout(timeoutHandle);
  }
}

async function discoverDevices(): Promise<void> {
  if (state.busy) return;
  state.busy = true;
  state.scanning = true;
  state.scanKind = 'working';
  state.scanMessage = 'Scanning USB and Ethernet…';
  state.notice = '';
  render();
  try {
    const result = normalizeDiscoveryResult(await discoverWithTimeout());
    state.discovery = result;
    const count = result.devices.length;
    const completed = new Date().toLocaleTimeString([], {
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
    const message = result.timedOut
      ? 'Scan timed out after 5 seconds. Enter the COM port below to connect directly.'
      : count === 0
      ? `No devices found · scan completed at ${completed}. USB requires a COM port, not WinUSB.`
      : `${count} device${count === 1 ? '' : 's'} found · scan completed at ${completed}`;
    state.scanMessage = message;
    state.scanKind = result.timedOut ? 'error' : count === 0 ? 'empty' : 'success';
    state.notice = message;
    state.noticeKind = result.timedOut ? 'error' : count === 0 ? 'info' : 'success';
    window.setTimeout(clearNotice, 4500);
  } catch (error) {
    const detail = errorText(error);
    const message = detail.startsWith('Scan timed out') ? detail : `Device scan failed: ${detail}`;
    state.scanMessage = message;
    state.scanKind = 'error';
    state.notice = message;
    state.noticeKind = 'error';
    window.setTimeout(clearNotice, 7000);
  } finally {
    state.scanning = false;
    state.busy = false;
    render();
  }
}

async function connectDevice(endpoint: Endpoint): Promise<void> {
  const result = await withAction(`Connected over ${endpoint.kind === 'usb' ? 'USB-C' : 'Ethernet'}`, () => Connect(endpoint) as Promise<Snapshot>);
  if (!result) return;
  state.snapshot = result;
  state.tab = result.customerControl ? 'control' : 'status';
  state.networkInput = '';
  state.networkPlan = null;
  state.networkError = '';
  state.control.rfEnabled = result.status.rfEnabled;
  render();
}

async function disconnectDevice(): Promise<void> {
  await withAction('Device disconnected', async () => { await Disconnect(); });
  state.snapshot = null;
  state.tab = 'control';
  render();
}

async function refreshStatus(showNotice: boolean): Promise<void> {
  if (!state.snapshot || state.busy) return;
  try {
    const result = await GetStatus() as Snapshot;
    state.snapshot = result;
    if (showNotice) {
      state.notice = 'Status refreshed';
      state.noticeKind = 'success';
      window.setTimeout(clearNotice, 2500);
    }
    if (showNotice || state.tab === 'status') render();
  } catch (error) {
    if (showNotice) {
      state.notice = errorText(error);
      state.noticeKind = 'error';
      render();
    }
  }
}

async function applyRFControl(): Promise<void> {
  const control = state.control;
  const attenuation = nominalMaximumOutputDbm - control.outputPowerDbm;
  const message = `${control.mode === 'cw' ? 'CW' : 'Sweep'} configuration applied · RF ${control.rfEnabled ? 'on' : 'off'}`;
  const result = control.mode === 'cw'
    ? await withAction(message, () => ConfigureCW({ frequencyMHz: control.cwMHz, attenuation, clock: control.clock, rfEnabled: control.rfEnabled }) as Promise<Snapshot>)
    : await withAction(message, () => ConfigureSweep({ startMHz: control.startMHz, stopMHz: control.stopMHz, sweepTime: control.sweepTime, attenuation, clock: control.clock, rfEnabled: control.rfEnabled }) as Promise<Snapshot>);
  if (result) {
    state.snapshot = result;
    state.control.rfEnabled = result.status.rfEnabled;
  }
  render();
}

function tuningProfileFromControl(): TuningProfile {
  const control = state.control;
  const barracuda: BarracudaTuningProfile = {
    mode: control.mode,
    attenuation_db: nominalMaximumOutputDbm - control.outputPowerDbm,
    clock: control.clock,
    rf_enabled: control.rfEnabled,
  };
  if (control.mode === 'cw') {
    barracuda.if_frequency_mhz = control.cwMHz;
  } else {
    barracuda.start_if_mhz = control.startMHz;
    barracuda.stop_if_mhz = control.stopMHz;
    barracuda.sweep_time = control.sweepTime;
  }
  return { barracuda };
}

async function exportTuningProfile(): Promise<void> {
  if (state.busy) return;
  readControlInputs();
  state.busy = true;
  state.notice = '';
  render();
  try {
    const path = await SaveTuningProfile(new GoModels.TuningProfile(tuningProfileFromControl())) as string;
    if (path) {
      state.notice = 'Current configuration exported as CLI-ready JSON';
      state.noticeKind = 'success';
      window.setTimeout(clearNotice, 3500);
    }
  } catch (error) {
    state.notice = errorText(error);
    state.noticeKind = 'error';
    window.setTimeout(clearNotice, 7000);
  } finally {
    state.busy = false;
    render();
  }
}

async function loadTuningProfile(): Promise<void> {
  if (state.busy) return;
  state.busy = true;
  state.notice = '';
  render();
  try {
    const profile = await LoadTuningProfile() as TuningProfile;
    const config = profile?.barracuda;
    if (!config) return;
    state.control.mode = config.mode;
    state.control.outputPowerDbm = nominalMaximumOutputDbm - config.attenuation_db;
    state.control.clock = config.clock;
    state.control.rfEnabled = config.rf_enabled;
    if (config.mode === 'cw') {
      state.control.cwMHz = config.if_frequency_mhz ?? state.control.cwMHz;
    } else {
      state.control.startMHz = config.start_if_mhz ?? state.control.startMHz;
      state.control.stopMHz = config.stop_if_mhz ?? state.control.stopMHz;
      state.control.sweepTime = config.sweep_time || state.control.sweepTime;
    }
    state.notice = 'Tuning JSON loaded · press Apply to tune the hardware';
    state.noticeKind = 'success';
    window.setTimeout(clearNotice, 5000);
  } catch (error) {
    state.notice = errorText(error);
    state.noticeKind = 'error';
    window.setTimeout(clearNotice, 7000);
  } finally {
    state.busy = false;
    render();
  }
}

async function updateNetworkPreview(): Promise<void> {
  const refocus = document.activeElement?.id === 'new-ip';
  if (!state.networkInput) {
    state.networkPlan = null;
    state.networkError = '';
    renderNetworkPreview(refocus);
    return;
  }
  try {
    state.networkPlan = await PreviewNetwork(state.networkInput) as NetworkPlan;
    state.networkError = '';
  } catch (error) {
    state.networkPlan = null;
    state.networkError = errorText(error);
  }
  renderNetworkPreview(refocus);
}

function renderNetworkPreview(refocus: boolean): void {
  render();
  if (!refocus) return;
  const input = document.querySelector<HTMLInputElement>('#new-ip');
  input?.focus();
  input?.setSelectionRange(input.value.length, input.value.length);
}

async function applyIPAddress(): Promise<void> {
  if (!state.networkPlan || !state.networkInput) return;
  const plan = state.networkPlan;
  const confirmed = window.confirm(`Change the device IP to ${plan.ipAddress}?\n\nGateway: ${plan.gateway}\nSubnet: ${plan.subnet}\n\nThe device will reboot and disconnect.`);
  if (!confirmed) return;
  const result = await withAction(`Network settings applied. Device is rebooting at ${plan.ipAddress}.`, () => SetIPAddress(state.networkInput));
  if (!result) return;
  state.snapshot = null;
  state.networkInput = '';
  state.networkPlan = null;
  state.scanMessage = `Device moved to ${plan.ipAddress} · press Scan to rediscover it`;
  state.scanKind = 'idle';
  render();
}

function boardName(board: string): string {
  const names: Record<string, string> = {
    barracuda: 'Barracuda', whalepod: 'Whalepod', whalepod_automation: 'Whalepod Automation',
    straps: 'STRAPS', bc: 'Black Canyon', rf_switch: 'RF Switch',
  };
  return names[board] || board || 'Unknown device';
}

function cleanEnum(value: string): string {
  return value.replace(/^(RF_SWITCH_OPTION_|MIXER_SWITCH_OPTION_|IF_SWITCH_OPTION_)/, '').replaceAll('_', ' ').toLowerCase();
}

async function loadAppVersion(): Promise<void> {
  try {
    state.appVersion = await Version() as string || 'dev';
    render();
  } catch {
    // Keep the source-build fallback when metadata is unavailable.
  }
}

render();
void loadAppVersion();
window.setInterval(() => void refreshStatus(false), 3000);
