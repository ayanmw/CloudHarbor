import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";

const root = new URL("../../internal/webui/", import.meta.url);
const tick = () => new Promise(resolve => setImmediate(resolve));

async function render(search, conflict = false) {
  const html = await readFile(new URL("index_cloud.html", root), "utf8");
  const shared = await readFile(new URL("res/shared.js", root), "utf8");
  const route = await readFile(new URL("res/cloud_browse_route.js", root), "utf8");
  const shell = await readFile(new URL("res/workspace-legacy.js", root), "utf8");
  const dom = new JSDOM(html, { url: "http://localhost/cloud" + search, runScripts: "outside-only" });
  const requests = [];
  dom.window.setInterval = () => 0;
  dom.window.fetch = async (input, options = {}) => {
    assert.ok(!options.method || options.method === "GET", "restoring a URL must be read-only");
    const url = new URL(input, dom.window.location.href); requests.push(url);
    if (conflict && url.pathname === "/api/cloud-inventory" && url.searchParams.get("offset") === "100") {
      conflict = false;
      return { ok: false, status: 409, text: async () => JSON.stringify({ code: "cache_generation_changed", error: "缓存版本变化" }) };
    }
    return { ok: true, json: async () => ({ directory: url.searchParams.get("dir") || "/", entries: [], total: 200, next_offset: null, cache_generation: 7, metadata: { status: "complete" }, coverage: { status: "complete" }, authentication: { ready: true } }) };
  };
  dom.window.eval(shared); dom.window.eval(route);
  dom.window.eval(await readFile(new URL("res/cloud_properties.js", root), "utf8"));
  dom.window.eval(await readFile(new URL("res/cloud_actions.js", root), "utf8"));
  dom.window.eval(await readFile(new URL("res/cloud_progress.js", root), "utf8"));
  dom.window.eval(await readFile(new URL("res/cloud_scan_limits.js", root), "utf8"));
  for (const match of html.matchAll(/<script>([\s\S]*?)<\/script>/g)) dom.window.eval(match[1]);
  dom.window.eval(shell);
  await tick(); await tick();
  return { dom, requests };
}

test("cloud URL restores cached search/page and browser back/forward without writes", async () => {
  const initial = new URLSearchParams({ dir: "/资料/课程", source: "cache", q: "合同", offset: "100", ui_tab: "files" });
  const { dom, requests } = await render("?" + initial);
  try {
    const win = dom.window, doc = win.document;
    assert.equal(doc.querySelector("#directory").value, "/资料/课程");
    assert.equal(doc.querySelector("#cache-search-query").value, "合同");
    assert.ok(requests.some(u => u.pathname === "/api/cloud-inventory/search" && u.searchParams.get("q") === "合同" && u.searchParams.get("offset") === "100"));
    doc.querySelector("#directory").value = "/另一个目录";
    doc.querySelector("#browse-form").dispatchEvent(new win.Event("submit", { cancelable: true }));
    await tick(); await tick();
    assert.equal(new URL(win.location.href).searchParams.get("dir"), "/另一个目录");
    assert.equal(new URL(win.location.href).searchParams.has("q"), false);
    const back = new Promise(resolve => win.addEventListener("popstate", resolve, { once: true }));
    win.history.back(); await back; await tick(); await tick();
    assert.equal(doc.querySelector("#directory").value, "/资料/课程");
    assert.equal(doc.querySelector("#cache-search-query").value, "合同");
    assert.equal(doc.querySelector("#browse-source").value, "cache");
    const forward = new Promise(resolve => win.addEventListener("popstate", resolve, { once: true }));
    win.history.forward(); await forward; await tick(); await tick();
    assert.equal(doc.querySelector("#directory").value, "/另一个目录");
    assert.equal(win.history.length, 2, "popstate must not push a fresh history entry");
    assert.equal(requests.some(u => u.pathname === "/api/cloud"), false);
  } finally { dom.window.close(); }
});

test("cache generation recovery replaces the route instead of appending a broken page", async () => {
  const { dom, requests } = await render("?dir=%2Fcached&source=cache&offset=100", true);
  try {
    assert.ok(requests.some(u => u.pathname === "/api/cloud-inventory" && u.searchParams.get("offset") === "0"));
    assert.equal(new URL(dom.window.location.href).searchParams.has("offset"), false);
    assert.equal(dom.window.history.length, 1);
    assert.match(dom.window.document.querySelector("#message").textContent, /缓存已更新/);
  } finally { dom.window.close(); }
});

test("cloud route rejects invalid browsing values and never forwards action-like parameters", async () => {
  const { dom, requests } = await render("?dir=..%2Fbad&source=other&offset=-1&q=test&request_budget=1000");
  try {
    const state = dom.window.CloudBrowseRoute.read(dom.window.location.search);
    assert.equal(state.directory, "/"); assert.equal(state.source, "live"); assert.equal(state.offset, 0); assert.equal(state.query, "");
    assert.ok(requests.some(u => u.pathname === "/api/cloud" && u.searchParams.get("dir") === "/"));
    assert.equal(requests.some(u => u.searchParams.has("request_budget")), false);
    const encoded = new URLSearchParams({ dir: '/课程 & 资料/"片名"', source: "cache" });
    assert.equal(dom.window.CloudBrowseRoute.read("?" + encoded).directory, '/课程 & 资料/"片名"');
    const emoji = '📁'.repeat(128);
    assert.equal(dom.window.CloudBrowseRoute.read('?' + new URLSearchParams({source:'cache',q:emoji})).query,emoji);
    assert.equal(dom.window.CloudBrowseRoute.read('?' + new URLSearchParams({source:'cache',q:emoji+'📁'})).query,'');
  } finally { dom.window.close(); }
});
