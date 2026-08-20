import './style.css';

import {
  ConfigureCW,
  ConfigureSweep,
  Connect,
  Disconnect,
  Discover,
  GetStatus,
  PreviewNetwork,
  SetIPAddress,
  SetMaximumAttenuation,
} from '../wailsjs/go/main/App';

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
type DiscoveryResult = { devices: DiscoveredDevice[]; warnings: string[] };
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
type Tab = 'bringup' | 'control' | 'status' | 'network';

const state = {
  discovery: { devices: [], warnings: [] } as DiscoveryResult,
  snapshot: null as Snapshot | null,
  tab: 'bringup' as Tab,
  busy: false,
  scanning: false,
  scanMessage: 'Waiting for device scan',
  scanKind: 'idle' as 'idle' | 'working' | 'success' | 'empty' | 'error',
  notice: '' as string,
  noticeKind: 'info' as 'info' | 'success' | 'error',
  control: {
    mode: 'cw' as 'cw' | 'sweep',
    cwMHz: 400,
    startMHz: 50,
    stopMHz: 1500,
    sweepTime: '10s',
    attenuation: 0,
    clock: 'internal' as 'internal' | 'external',
  },
  bringup: {} as Record<string, 'pass' | 'failed'>,
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
    ${lockChip('Signal', status.signalLockApplicable, status.signalLocked)}
    ${lockChip('10 MHz reference', status.referenceLockApplicable, status.referenceLocked)}
    ${status.maximumAttenuation ? '<span class="chip caution"><i></i>Maximum attenuation</span>' : ''}
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
      <div class="brand"><div class="mark">O</div><div><strong>OCUPOINT</strong><span>RF CONTROL</span></div></div>
      <div class="sidebar-heading"><span>Devices</span><button class="refresh-button ${state.scanning ? 'scanning' : ''}" id="refresh-devices" title="Refresh devices" ${state.busy ? 'disabled' : ''}><span class="refresh-icon">↻</span><span>${state.scanning ? 'Scanning…' : 'Refresh'}</span></button></div>
      <div class="scan-feedback ${state.scanKind}" role="status" aria-live="polite"><span></span>${escapeHTML(state.scanMessage)}</div>
      <div class="devices">${devices || '<div class="empty-small">No devices discovered</div>'}</div>
      ${warnings}
      <form class="manual-connect" id="manual-connect">
        <label for="manual-ip">Connect by IP</label>
        <div><input id="manual-ip" inputmode="decimal" placeholder="192.168.50.25" required><button ${state.busy ? 'disabled' : ''}>Connect</button></div>
        <small>Use this when broadcast discovery is blocked.</small>
      </form>
      <div class="sidebar-footer"><span>Local control only</span><span>CLI fallback included</span></div>
    </aside>`;
}

function renderEmpty(): string {
  return `
    <main class="empty-workspace">
      <div class="signal-art" aria-hidden="true"><span></span><span></span><span></span><span></span></div>
      <p class="eyebrow">Hardware control</p>
      <h1>Select an RF device</h1>
      <p>Connect over USB-C for first bring-up, or select an Ethernet endpoint for normal operation.</p>
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
    ? [['bringup', 'First Bring-Up'], ['control', 'RF Control'], ['status', 'Status'], ['network', 'Network']]
    : [['status', 'Status'], ['network', 'Network']];
  if (!snapshot.customerControl && (state.tab === 'bringup' || state.tab === 'control')) state.tab = 'status';
  return `<nav class="tabs">${tabs.map(([id, label]) => `<button data-tab="${id}" class="${state.tab === id ? 'active' : ''}">${label}</button>`).join('')}</nav>`;
}

function resultBadge(id: string): string {
  const result = state.bringup[id];
  if (!result) return '<span class="step-state">Not run</span>';
  return result === 'pass' ? '<span class="step-state pass">Software check passed</span>' : '<span class="step-state fail">Software check failed</span>';
}

function renderBringup(snapshot: Snapshot): string {
  const refPassed = snapshot.status.clock === 'external' && snapshot.status.referenceLocked && snapshot.status.signalLocked;
  return `
    <section class="content bringup">
      <div class="section-intro"><div><p class="eyebrow">Guided acceptance</p><h2>Barracuda first bring-up</h2></div><p>Verify the RF path over USB-C before moving control to Ethernet.</p></div>
      ${snapshot.endpoint.kind !== 'usb' ? '<div class="callout warning">First bring-up should be performed over USB-C. This device is currently connected over Ethernet.</div>' : ''}
      <div class="step-list">
        <article class="step"><span class="step-number">01</span><div class="step-body"><div class="step-heading"><h3>Connect the hardware</h3><span class="step-state manual">Operator check</span></div><p>Connect Ethernet, an RF SMA measurement cable to Port A or B, USB-C, and then the 12 V supply.</p><ul><li>Measurement instrument is configured for a 50–1500 MHz IF.</li><li>External 10 MHz reference is available for the final test.</li></ul></div></article>
        <article class="step"><span class="step-number">02</span><div class="step-body"><div class="step-heading"><h3>Verify a CW tone</h3>${resultBadge('cw')}</div><p>Configure 400 MHz IF, internal clock, and 0 dB attenuation.</p><div class="step-actions"><button class="primary" id="bringup-cw" ${state.busy ? 'disabled' : ''}>Run CW test</button><span>Expected: signal locked and approximately −25 dBm at operating temperature.</span></div></div></article>
        <article class="step"><span class="step-number">03</span><div class="step-body"><div class="step-heading"><h3>Verify a continuous sweep</h3>${resultBadge('sweep')}</div><p>Sweep 50–1500 MHz IF over 10 seconds using the internal clock.</p><div class="step-actions"><button class="primary" id="bringup-sweep" ${state.busy ? 'disabled' : ''}>Run sweep test</button><span>Confirm the requested sweep on the measurement instrument.</span></div></div></article>
        <article class="step"><span class="step-number">04</span><div class="step-body"><div class="step-heading"><h3>Verify the external 10 MHz reference</h3>${resultBadge('reference')}</div><p>Connect and enable the external reference before running this check.</p><div class="step-actions"><button class="primary" id="bringup-reference" ${state.busy ? 'disabled' : ''}>Test external reference</button><span>${refPassed ? 'Reference and signal are locked.' : 'RF remains at maximum attenuation if lock validation fails.'}</span></div></div></article>
        <article class="step"><span class="step-number">05</span><div class="step-body"><div class="step-heading"><h3>Move control to Ethernet</h3><span class="step-state">Final step</span></div><p>Review or update the static IP, allow the unit to reboot, and reconnect at the new Ethernet address.</p><div class="step-actions"><button class="secondary" data-tab="network">Open network settings</button></div></div></article>
      </div>
    </section>`;
}

function renderControl(snapshot: Snapshot): string {
  const control = state.control;
  const estimate = -25 - Number(control.attenuation || 0);
  return `
    <section class="content control-layout">
      <div class="control-card">
        <div class="section-intro"><div><p class="eyebrow">Customer controls</p><h2>RF output</h2></div><span class="power-readout"><small>Estimated output</small><strong id="power-estimate">${estimate.toFixed(2)} dBm</strong></span></div>
        <div class="segment" role="group" aria-label="RF mode"><button data-mode="cw" class="${control.mode === 'cw' ? 'active' : ''}">CW</button><button data-mode="sweep" class="${control.mode === 'sweep' ? 'active' : ''}">Sweep</button></div>
        <form id="rf-form">
          ${control.mode === 'cw' ? `
            <div class="field"><label for="cw-frequency">IF frequency</label><div class="input-unit"><input id="cw-frequency" type="number" min="50" max="1500" step="1" value="${control.cwMHz}" required><span>MHz</span></div><small>Allowed range: 50–1500 MHz IF</small></div>` : `
            <div class="field-row">
              <div class="field"><label for="sweep-start">Start IF</label><div class="input-unit"><input id="sweep-start" type="number" min="50" max="1499" step="1" value="${control.startMHz}" required><span>MHz</span></div></div>
              <div class="field"><label for="sweep-stop">Stop IF</label><div class="input-unit"><input id="sweep-stop" type="number" min="51" max="1500" step="1" value="${control.stopMHz}" required><span>MHz</span></div></div>
              <div class="field"><label for="sweep-time">Sweep time</label><input id="sweep-time" value="${escapeHTML(control.sweepTime)}" placeholder="10s" required><small>Examples: 10s, 20ms, 35us</small></div>
            </div>`}
          <div class="field-row output-settings">
            <div class="field"><label for="attenuation">Digital attenuation</label><div class="input-unit"><input id="attenuation" type="number" min="0" max="31.75" step="0.25" value="${control.attenuation}" required><span>dB</span></div><small>0–31.75 dB in 0.25 dB steps</small></div>
            <fieldset class="field"><legend>Clock source</legend><div class="radio-row"><label><input type="radio" name="clock" value="internal" ${control.clock === 'internal' ? 'checked' : ''}>Internal</label><label><input type="radio" name="clock" value="external" ${control.clock === 'external' ? 'checked' : ''}>External 10 MHz</label></div><small>External mode must lock before RF is enabled.</small></fieldset>
          </div>
          <div class="form-actions"><button class="primary large" type="submit" ${state.busy ? 'disabled' : ''}>${state.busy ? 'Applying…' : `Apply ${control.mode.toUpperCase()}`}</button><button class="danger-quiet" type="button" id="max-attenuation" ${state.busy ? 'disabled' : ''}>Set maximum attenuation</button></div>
        </form>
      </div>
      <aside class="live-panel"><p class="eyebrow">Live state</p><h3>${snapshot.status.mode ? snapshot.status.mode.toUpperCase() : 'Not configured'}</h3>${renderBarracudaFacts(snapshot.status)}</aside>
    </section>`;
}

function renderBarracudaFacts(status: DeviceStatus): string {
  const frequency = status.ifFrequencyMHz ? `${status.ifFrequencyMHz}${status.sweepStopIfMHz ? `–${status.sweepStopIfMHz}` : ''} MHz IF` : 'Unavailable';
  return `<dl class="facts">
    <div><dt>Frequency</dt><dd>${escapeHTML(frequency)}</dd></div>
    ${status.sweepTime ? `<div><dt>Sweep time</dt><dd>${escapeHTML(status.sweepTime)}</dd></div>` : ''}
    <div><dt>Clock</dt><dd>${escapeHTML(status.clock || 'internal')}</dd></div>
    <div><dt>Attenuation</dt><dd>${status.attenuationDb.toFixed(2)} dB</dd></div>
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
    case 'bringup': body = renderBringup(snapshot); break;
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
    const address = (document.querySelector<HTMLInputElement>('#manual-ip')?.value || '').trim();
    if (address) void connectDevice({ kind: 'ethernet', address, port: 5000 });
  });

  document.querySelector('#bringup-cw')?.addEventListener('click', () => void bringupCW());
  document.querySelector('#bringup-sweep')?.addEventListener('click', () => void bringupSweep());
  document.querySelector('#bringup-reference')?.addEventListener('click', () => void bringupReference());
  document.querySelector('#max-attenuation')?.addEventListener('click', () => void maximumAttenuation());

  document.querySelector<HTMLFormElement>('#rf-form')?.addEventListener('submit', (event) => {
    event.preventDefault();
    readControlInputs();
    void applyRFControl();
  });
  document.querySelector<HTMLInputElement>('#attenuation')?.addEventListener('input', (event) => {
    const value = Number((event.target as HTMLInputElement).value);
    state.control.attenuation = value;
    const estimate = document.querySelector('#power-estimate');
    if (estimate && Number.isFinite(value)) estimate.textContent = `${(-25 - value).toFixed(2)} dBm`;
  });
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
  state.control.attenuation = numberValue('#attenuation', state.control.attenuation);
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

async function discoverDevices(): Promise<void> {
  if (state.busy) return;
  state.busy = true;
  state.scanning = true;
  state.scanKind = 'working';
  state.scanMessage = 'Scanning USB and Ethernet…';
  state.notice = '';
  render();
  try {
    const result = await Discover() as DiscoveryResult;
    state.discovery = result;
    const count = result.devices.length;
    const completed = new Date().toLocaleTimeString([], {
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
    const message = count === 0
      ? `No devices found · scan completed at ${completed}`
      : `${count} device${count === 1 ? '' : 's'} found · scan completed at ${completed}`;
    state.scanMessage = message;
    state.scanKind = count === 0 ? 'empty' : 'success';
    state.notice = message;
    state.noticeKind = count === 0 ? 'info' : 'success';
    window.setTimeout(clearNotice, 4500);
  } catch (error) {
    const message = `Device scan failed: ${errorText(error)}`;
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
  state.tab = result.customerControl ? 'bringup' : 'status';
  state.networkInput = '';
  state.networkPlan = null;
  state.networkError = '';
  render();
  void discoverDevices();
}

async function disconnectDevice(): Promise<void> {
  await withAction('Device disconnected', async () => { await Disconnect(); });
  state.snapshot = null;
  state.tab = 'bringup';
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
    if (showNotice || state.tab === 'status' || state.tab === 'bringup') render();
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
  const result = control.mode === 'cw'
    ? await withAction('CW configuration applied', () => ConfigureCW({ frequencyMHz: control.cwMHz, attenuation: control.attenuation, clock: control.clock }) as Promise<Snapshot>)
    : await withAction('Sweep configuration applied', () => ConfigureSweep({ startMHz: control.startMHz, stopMHz: control.stopMHz, sweepTime: control.sweepTime, attenuation: control.attenuation, clock: control.clock }) as Promise<Snapshot>);
  if (result) state.snapshot = result;
  render();
}

async function maximumAttenuation(): Promise<void> {
  const result = await withAction('Maximum attenuation applied (31.75 dB)', () => SetMaximumAttenuation() as Promise<Snapshot>);
  if (result) {
    state.snapshot = result;
    state.control.attenuation = 31.75;
  }
  render();
}

async function bringupCW(): Promise<void> {
  const result = await withAction('CW test configured', () => ConfigureCW({ frequencyMHz: 400, attenuation: 0, clock: 'internal' }) as Promise<Snapshot>);
  state.bringup.cw = result?.status.signalLocked ? 'pass' : 'failed';
  if (result) state.snapshot = result;
  render();
}

async function bringupSweep(): Promise<void> {
  const result = await withAction('Sweep test configured', () => ConfigureSweep({ startMHz: 50, stopMHz: 1500, sweepTime: '10s', attenuation: 0, clock: 'internal' }) as Promise<Snapshot>);
  state.bringup.sweep = result?.status.signalLocked ? 'pass' : 'failed';
  if (result) state.snapshot = result;
  render();
}

async function bringupReference(): Promise<void> {
  const result = await withAction('External reference verified', () => ConfigureCW({ frequencyMHz: 400, attenuation: 0, clock: 'external' }) as Promise<Snapshot>);
  state.bringup.reference = result?.status.referenceLocked && result.status.signalLocked ? 'pass' : 'failed';
  if (result) state.snapshot = result;
  render();
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
  render();
  window.setTimeout(() => void discoverDevices(), 4500);
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

render();
void discoverDevices();
window.setInterval(() => void refreshStatus(false), 3000);
