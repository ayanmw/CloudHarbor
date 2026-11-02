import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const shell = await readFile(
  new URL("../../internal/webui/res/workspace-legacy.js", import.meta.url),
  "utf8",
);
const tick = () => new Promise((resolve) => setImmediate(resolve));

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
