import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import vm from 'node:vm';
import ts from 'typescript';

// Exercise the real frontend handlers with a small DOM and Wails boundary.
const source = readFileSync(new URL('../src/main.ts', import.meta.url), 'utf8');
function frontend(api = {}) {
  const elements = new Map();
  const element = (selector, value = '') => {
    const handlers = {};
    const node = { value, handlers, innerHTML: '', removed: false,
      addEventListener(name, fn) { handlers[name] = fn; },
      remove() { this.removed = true; } };
    elements.set(selector, node);
    return node;
  };
  element('#app');
  const context = vm.createContext({
    exports: {}, Error,
    require: (name) => name.endsWith('.css') ? {} : api,
    document: { querySelector: (selector) => elements.get(selector) ?? null,
      querySelectorAll: () => [] },
    window: { setTimeout() {}, setInterval() {} },
  });
  const script = ts.transpileModule(source + `
    let renderCount = 0;
    render = () => { renderCount++; };
    Object.assign(exports, { state, bindEvents, clearNotice, applyRFControl, getRenderCount: () => renderCount });
  `, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
  vm.runInContext(script, context);
  return { ...context.exports, element };
}

test('RF edits are retained before Apply or a navigation redraw', () => {
  const ui = frontend();
  const form = ui.element('#rf-form');
  ui.element('#cw-frequency', '725');
  ui.element('#output-power', '-37');
  ui.bindEvents();
  form.handlers.input();
  assert.equal(ui.state.control.cwMHz, 725);
  assert.equal(ui.state.control.outputPowerDbm, -37);
});

test('notice expiry removes only the notice and preserves the form', () => {
  const ui = frontend();
  const form = ui.element('#rf-form');
  const toast = ui.element('.toast');
  ui.state.notice = 'Applied';
  ui.clearNotice();
  assert.equal(ui.state.notice, '');
  assert.equal(toast.removed, true);
  assert.equal(form.removed, false);
  assert.equal(ui.getRenderCount(), 0);
});

test('failed Apply refreshes telemetry while preserving the lock error', async () => {
  const live = { status: { attenuationDb: 0, signalLocked: false } };
  let statusCalls = 0;
  const ui = frontend({
    ConfigureCW: async () => { throw new Error('ADF4159 did not lock'); },
    GetStatus: async () => { statusCalls++; return live; },
  });
  ui.state.snapshot = { status: { attenuationDb: 31.75 } };
  await ui.applyRFControl();
  assert.equal(statusCalls, 1);
  assert.equal(ui.state.snapshot, live);
  assert.match(ui.state.notice, /ADF4159 did not lock/);
  assert.equal(ui.state.noticeKind, 'error');
});
