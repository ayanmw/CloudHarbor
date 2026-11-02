import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const root = new URL("../../internal/webui/", import.meta.url);
const tick = () => new Promise(resolve => setImmediate(resolve));

test("background scan panel survives tabs/minimising and reuses the existing pause and status polling", async () => {
  const html = await readFile(new URL("index_cloud.html", root), "utf8");
  const progress = await readFile(new URL("res/cloud_progress.js", root), "utf8");
  const dom = new JSDOM(html, { url: "http://localhost/cloud", runScripts: "outside-only" });
  const win = dom.window, doc = win.document, calls = [], timers = [];
  let failed = false, state = { run_id: "run", status: "running", phase: "listing", worker_active: true, pages_processed: 2, items_processed: 125, pending_count: 3, requests_used: 2, request_budget: 0, current_directory: "/docs" };
  try {
    win.setInterval = (callback, delay) => { timers.push({ callback, delay }); return timers.length; };
    win.fetch = async (input, options = {}) => {
      const method = options.method || 'GET'; calls.push({ input, method });
      if (input === '/api/cloud-inventory/scan') {
        if (method === 'DELETE') state = { ...state, status: 'paused', worker_active: false };
        if (method === 'POST') state = { ...state, status: 'running', worker_active: true };
        if (method === 'GET' && failed) throw new Error('state unavailable');
        return { ok: true, json: async () => state };
      }
      return { ok: true, json: async () => ({ directory: '/', entries: [], status: 'idle', next_offset: null, authentication: { ready: true } }) };
    };
    for (const file of ['shared.js','cloud_browse_route.js','cloud_properties.js','cloud_actions.js']) win.eval(await readFile(new URL('res/'+file,root),'utf8'));
    win.eval(progress);
    win.eval(await readFile(new URL("res/cloud_scan_limits.js",root),"utf8"));
    assert.equal(timers.length, 0, 'the panel must not create a second polling loop');
    for (const match of html.matchAll(/<script>([\s\S]*?)<\/script>/g)) win.eval(match[1]);
    win.eval(await readFile(new URL('res/workspace-legacy.js',root),'utf8'));
    await tick(); await tick();
    const panel = doc.querySelector('#cloud-progress');
    assert.equal(panel.hidden, false); assert.match(panel.textContent, /125/);
    assert.equal(panel.querySelector('progress').hasAttribute('value'), false, 'unknown total must not become a percentage');
    const before = calls.length;
    doc.querySelector('#cloud-progress-toggle').click();
    doc.querySelector('[data-group="settings"]').click();
    assert.equal(doc.querySelector('#cloud-progress-body').hidden, true);
    assert.equal(panel.hidden, false); assert.equal(calls.length, before);
    doc.querySelector('#cloud-progress-toggle').click();
    panel.querySelector('[data-progress-action="pause"]').click(); await tick(); await tick();
    assert.equal(calls.filter(c=>c.method==='DELETE').length,1);
    assert.match(panel.textContent,/已暂停/);
    failed = true;
    const poll = timers.find(t=>t.callback.name==='scanStatus');
    await poll.callback(); await tick();
    assert.match(panel.textContent,/状态暂不可读/);
    assert.equal(panel.querySelector('[data-progress-action="resume"]').disabled,true);
    failed = false; await poll.callback(); await tick();
    panel.querySelector('[data-progress-action="resume"]').click(); await tick(); await tick();
    assert.equal(calls.filter(c=>c.method==='POST').length,1);
    assert.match(panel.textContent,/扫描中/);
    win.CloudProgress.update('full',{...state,status:'paused',worker_active:false,request_budget:2,requests_used:2});
    const writes = calls.filter(c=>c.method==='POST').length;
    panel.querySelector('[data-progress-action="resume"]').click(); await tick();
    assert.equal(calls.filter(c=>c.method==='POST').length,writes,'a finite checkpoint must expose its mode choice before continuing');
    assert.equal(doc.querySelector('#scan-details').open,true);
  } finally { dom.window.close(); }
});
