import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const root = new URL("../../internal/webui/", import.meta.url);
const tick = () => new Promise(resolve => setImmediate(resolve));

async function render() {
  const html = await readFile(new URL("index_cloud.html", root), "utf8");
  const actions = await readFile(new URL("res/cloud_actions.js", root), "utf8");
  const dom = new JSDOM(html, { url: "http://localhost/cloud", runScripts: "outside-only" });
  const win = dom.window, doc = win.document, posts = [];
  let scenario = "ok", item = { id: "42", fs_id: 42, path: "/report.txt", name: "report.txt", size: 3, directory: false, server_mtime: 123 };
  win.setInterval = () => 0;
  win.prompt = win.confirm = () => { throw new Error("native confirmation must not be used for directory CRUD"); };
  win.HTMLDialogElement.prototype.showModal = function () { this.open = true; };
  win.HTMLDialogElement.prototype.close = function () { this.open = false; this.dispatchEvent(new win.Event("close")); };
  win.fetch = async (input, options = {}) => {
    if (options.method === "POST") {
      const body = JSON.parse(options.body); posts.push(body);
      if (scenario === "network") throw new Error("connection lost after sending request");
      if (scenario === "validation") return { ok: false, status: 400, text: async () => "名称不符合要求" };
      if (scenario === "unknown") return { ok: true, json: async () => ({ status: "unknown", operation_id: "a".repeat(32) }) };
      if (body.operation === "rename") item = { ...item, name: body.new_name, path: "/" + body.new_name };
      return { ok: true, json: async () => ({ status: "confirmed", operation_id: "a".repeat(32) }) };
    }
    return { ok: true, json: async () => ({ directory: "/", entries: [item], next_offset: null, metadata: { status: "complete" }, authentication: { ready: true } }) };
  };
  for (const file of ["shared.js", "cloud_browse_route.js", "cloud_properties.js", "cloud_progress.js", "cloud_scan_limits.js"]) win.eval(await readFile(new URL("res/" + file, root), "utf8"));
  win.eval(actions);
  for (const match of html.matchAll(/<script>([\s\S]*?)<\/script>/g)) win.eval(match[1]);
  win.eval(await readFile(new URL("res/workspace-legacy.js", root), "utf8"));
  await tick(); await tick();
  return { dom, win, doc, posts, setScenario(value) { scenario = value; } };
}

async function submit(p) {
  p.doc.querySelector("#cloud-action-form").dispatchEvent(new p.win.Event("submit", { cancelable: true }));
  await tick(); await tick();
}

test("rename dialog retains invalid input and sends the original expected identity on confirmation", async () => {
  const p = await render();
  try {
    p.doc.querySelector('[data-rename="42"]').click();
    assert.equal(p.doc.querySelector('#cloud-action-dialog').open, true);
    p.doc.querySelector('#cloud-action-name').value = '新名称.txt';
    p.setScenario('validation'); await submit(p);
    assert.equal(p.doc.querySelector('#cloud-action-name').value, '新名称.txt');
    assert.match(p.doc.querySelector('#cloud-action-message').textContent, /名称不符合要求/);
    assert.equal(p.doc.querySelector('#cloud-action-submit').disabled, false);
    p.setScenario('ok'); await submit(p);
    assert.equal(p.doc.querySelector('#cloud-action-dialog').open, false);
    assert.equal(p.posts[1].operation, 'rename');
    assert.equal(p.posts[1].expected.path, '/report.txt');
    assert.equal(p.posts[1].expected.fs_id, 42);
    assert.equal(p.posts[1].new_name, '新名称.txt');
    assert.match(p.doc.querySelector('#entries').textContent, /新名称.txt/);
  } finally { p.dom.window.close(); }
});

test("delete requires explicit confirmation and an unknown outcome cannot be blindly resubmitted", async () => {
  const p = await render();
  try {
    p.doc.querySelector('[data-delete="42"]').click();
    await submit(p); assert.equal(p.posts.length, 0);
    p.doc.querySelector('#cloud-action-delete-confirm').click();
    p.setScenario('unknown'); await submit(p);
    assert.equal(p.posts.length, 1);
    assert.equal(p.doc.querySelector('#cloud-action-submit').disabled, true);
    assert.match(p.doc.querySelector('#cloud-action-message').textContent, /核对/);
    assert.ok(p.doc.querySelector('#cloud-action-dialog a[href="/operations"]'));
    await submit(p); assert.equal(p.posts.length, 1);
  } finally { p.dom.window.close(); }
});

test("mkdir cancel/invalid names do not send writes and an ambiguous network failure blocks resubmit", async () => {
  const p = await render();
  try {
    p.doc.querySelector('#mkdir').click();
    p.doc.querySelector('#cloud-action-cancel').click(); assert.equal(p.posts.length, 0);
    p.doc.querySelector('#mkdir').click();
    p.doc.querySelector('#cloud-action-name').value = '../bad'; await submit(p);
    assert.equal(p.posts.length, 0);
    p.doc.querySelector('#cloud-action-name').value = '测试目录';
    p.setScenario('network'); await submit(p);
    assert.equal(p.posts.length, 1); assert.equal(p.posts[0].destination_directory, '/');
    assert.equal(p.doc.querySelector('#cloud-action-name').value, '测试目录');
    assert.equal(p.doc.querySelector('#cloud-action-submit').disabled, true);
    await submit(p); assert.equal(p.posts.length, 1);
  } finally { p.dom.window.close(); }
});
