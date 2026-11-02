import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const shell = await readFile(
  new URL("../../internal/webui/res/workspace-legacy.js", import.meta.url),
  "utf8",
);
const tick = () => new Promise((resolve) => setImmediate(resolve));

test("cloud browser keeps a compact size column and navigation in empty directories", async () => {
  const html = await readFile(new URL("../../internal/webui/index_cloud.html", import.meta.url), "utf8");
  const shared = await readFile(new URL("../../internal/webui/res/shared.js", import.meta.url), "utf8");
  const dom = new JSDOM(html, { url: "http://localhost/cloud", runScripts: "outside-only" });
  const requests = [];
  let failListing = false;
  try {
    const win = dom.window, doc = win.document;
    win.setInterval = () => 0;
    win.fetch = async (url, options = {}) => {
      assert.ok(!options.method || options.method === "GET", "browsing must not mutate remote files");
      const parsed = new URL(url, win.location.href); requests.push(parsed);
      if (failListing && parsed.pathname === "/api/cloud") return { ok: false, status: 503, text: async () => "目录读取失败" };
      const dir = parsed.searchParams.get("dir") || "/";
      const entries = dir === "/" ? [
        { id: "1", name: "子目录", path: "/子目录", directory: true, size: 2048, size_complete: true },
        { id: "2", name: "<img src=x>.txt", path: "/<img src=x>.txt", directory: false, size: 4096 },
      ] : [];
      return { ok: true, json: async () => ({ directory: dir, entries, next_offset: null, metadata: { status: "complete" }, coverage: { status: "complete" }, authentication: { ready: true } }) };
    };
    win.eval(shared);
    for (const match of html.matchAll(/<script>([\s\S]*?)<\/script>/g)) win.eval(match[1]);
    win.eval(shell);
    await tick(); await tick();
    assert.deepEqual([...doc.querySelectorAll("#entries th")].map(n => n.textContent), ["名称", "大小", "操作"]);
    const rows = doc.querySelectorAll("#entries tbody tr");
    assert.equal(rows.length, 2);
    assert.doesNotMatch(rows[0].cells[0].textContent, /KiB/);
    assert.match(rows[0].cells[1].textContent, /2.*KiB/);
    assert.equal(doc.querySelector("#entries img"), null);
    assert.equal(doc.querySelector("#browser-cache-tools").open, false);
    assert.equal(doc.querySelector("#up").disabled, true);
    doc.querySelector('#entries [data-open="/子目录"]').click();
    await tick(); await tick();
    assert.equal(doc.querySelector("#directory").value, "/子目录");
    assert.equal(doc.querySelector("#up").disabled, false);
    assert.match(doc.querySelector("#browse-breadcrumbs").textContent, /全部文件.*子目录/);
    assert.equal(doc.querySelectorAll("#entries th").length, 3);
    assert.match(doc.querySelector("#entries tbody").textContent, /此目录为空/);
    assert.ok(doc.querySelector("#empty-cleanup-card").classList.contains("workspace-section-hidden"));
    assert.ok(doc.querySelector("#duplicate-cleanup-card").classList.contains("workspace-section-hidden"));
    doc.querySelector("#up").click(); await tick(); await tick();
    assert.equal(doc.querySelector("#directory").value, "/");
    assert.equal(doc.querySelectorAll("#entries tbody tr").length, 2);
    doc.querySelector("#browse-refresh").click(); await tick(); await tick();
    assert.ok(requests.filter(u => u.pathname === "/api/cloud").length >= 4);
    failListing = true;
    doc.querySelector("#browse-refresh").click(); await tick(); await tick();
    assert.equal(doc.querySelectorAll("#entries th").length, 3);
    assert.match(doc.querySelector("#message").textContent, /目录读取失败/);
    assert.doesNotMatch(doc.querySelector("#entries tbody").textContent, /此目录为空/);
    const source = doc.querySelector("#browse-source");
    source.value = "cache"; source.dispatchEvent(new win.Event("change"));
    await tick(); await tick();
    doc.querySelector('#entries [data-open="/子目录"]').click();
    await tick(); await tick();
    doc.querySelector('#browse-breadcrumbs [data-browse-directory="/"]').click();
    await tick(); await tick();
    assert.equal(source.value, "cache");
    assert.equal(doc.querySelector("#directory").value, "/");
    assert.equal(doc.querySelectorAll("#entries [data-delete]").length, 0);
  } finally { dom.window.close(); }
});

test("cloud shell defaults to files, preserves actions and transforms live entries to a table", async () => {
  const dom = new JSDOM(
    `<main><nav>old links</nav><h1>网盘管理</h1><section class="card"><h2>目录缓存刷新历史</h2><div id="history"><div class="entry"><span>Run 1<pre>{"safe":true}</pre></span><span><button data-action="export">导出</button></span></div></div></section><section class="card"><h2>自动下载路径</h2><form id="path-form"><input id="queue-path"></form></section><section class="card"><h2>目录浏览、管理与大小</h2><form id="browse-form"></form><div id="entries"><div class="entry" data-id="42"><span>文件</span><span>1 MB</span><span><button data-action="rename">重命名</button></span></div></div></section></main>`,
    { url: "http://localhost/cloud", runScripts: "outside-only" },
  );
  try {
    let clicks = 0;
    dom.window.document
      .querySelector("#entries")
      .addEventListener("click", (e) => {
        if (e.target.dataset.action === "rename") clicks++;
      });
    dom.window.eval(shell);
    await tick();
    const doc = dom.window.document;
    assert.equal(doc.querySelectorAll(".workspace-sidebar").length, 1);
    assert.equal(
      doc.querySelector('[data-workspace-group="files"]').hidden,
      false,
    );
    assert.equal(
      doc.querySelector('[data-workspace-group="history"]').hidden,
      true,
    );
    assert.ok(doc.querySelector('#entries table tbody tr[data-id="42"]'));
    doc.querySelector('[data-action="rename"]').click();
    assert.equal(clicks, 1);
    assert.ok(doc.querySelector("#browse-form"));
    assert.ok(doc.querySelector("#path-form"));
    doc.querySelector('[data-group="settings"]').click();
    assert.equal(
      doc.querySelector('[data-workspace-group="settings"]').hidden,
      false,
    );
    assert.ok(doc.querySelector("#history table details pre"));
    doc.querySelector("#entries").innerHTML =
      '<div class="entry"><span>第二项</span><span>2 MB</span><span>操作</span></div>';
    await tick();
    assert.equal(
      doc.querySelector("#entries tbody tr td").textContent,
      "第二项",
    );
  } finally {
    dom.window.close();
  }
});

test("management records use semantic tables and keep evidence collapsed", async () => {
  const dom = new JSDOM(
    '<main><h1>任务事件</h1><div id="records"><div class="entry"><span>事件<pre>安全证据</pre></span><span>UTC</span></div></div></main>',
    { url: "http://localhost/task-events", runScripts: "outside-only" },
  );
  try {
    dom.window.eval(shell);
    await tick();
    const doc = dom.window.document;
    assert.equal(doc.querySelectorAll("#records table tbody tr").length, 1);
    assert.equal(doc.querySelector("#records details").open, false);
    assert.equal(
      doc.querySelector("#records details pre").textContent,
      "安全证据",
    );
  } finally {
    dom.window.close();
  }
});
