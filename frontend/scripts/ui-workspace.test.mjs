import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const html = await readFile(
  new URL("../../internal/webui/app/index.html", import.meta.url),
  "utf8",
);
const asset = html.match(/src="\/app\/(assets\/[^\"]+\.js)"/)[1];
const bundle = await readFile(
  new URL("../../internal/webui/app/" + asset, import.meta.url),
  "utf8",
);
const tick = () => new Promise((resolve) => setImmediate(resolve));
const root = {
  id: "9007199254740993",
  name: "课程目录",
  path: "/queue/课程目录",
  local_path: "D:/Downloads/课程目录",
  status: "downloading",
  size: 1000,
  progress: 500,
  speed: 100,
  files: 3,
  folders: 4,
  completed: 1,
  active: 1,
  prepared: true,
  mode: "keep",
  can_pause: true,
  can_cancel: true,
};
const files = [
  {
    ...root,
    id: "2",
    name: "下载中.bin",
    path: root.path + "/下载中.bin",
    status: "downloading",
    size: 600,
    progress: 300,
    can_pause: false,
  },
  {
    ...root,
    id: "3",
    name: "待下载.bin",
    path: root.path + "/待下载.bin",
    status: "pending",
    size: 200,
    progress: 0,
    can_pause: false,
  },
  {
    ...root,
    id: "4",
    name: "已完成.bin",
    path: root.path + "/已完成.bin",
    status: "done",
    size: 200,
    progress: 200,
    can_pause: false,
  },
];
async function render(overrides = {}) {
  const currentRoot = { ...root, ...overrides };
  const dom = new JSDOM(html, {
    url: "http://localhost/",
    runScripts: "outside-only",
  });
  let fail = false;
  const requests = [];
  dom.window.setInterval = () => 1;
  dom.window.clearInterval = () => {};
  dom.window.fetch = async (url) => {
    requests.push(String(url));
    if (fail)
      return { ok: false, status: 503, text: async () => "本地服务暂不可用" };
    const u = new URL(url, "http://localhost");
    let data;
    if (u.pathname === "/api/health")
      data = {
        authentication: { ready: true },
        queue_pause: { paused: false },
        queue_healthy: true,
      };
    else {
      const detail = !!u.searchParams.get("root"),
        status = u.searchParams.get("status");
      const all = detail
          ? currentRoot.status === "source_unknown"
            ? []
            : files
          : [currentRoot],
        items = status ? all.filter((t) => t.status === status) : all;
      data = {
        items,
        root: detail ? currentRoot : null,
        total: items.length,
        root_count: 1,
        counts: { downloading: 1, pending: 1, done: 1 },
      };
    }
    return { ok: true, status: 200, json: async () => data };
  };
  dom.window.eval(bundle);
  await tick();
  await tick();
  return {
    dom,
    requests,
    setFailure: () => {
      fail = true;
    },
  };
}

test("built Vue workspace opens a root detail, shows only file rows and filters completed files", async () => {
  const { dom, requests } = await render();
  try {
    const doc = dom.window.document;
    assert.equal(doc.querySelectorAll("tbody tr").length, 1);
    doc.querySelector(".file-name").click();
    await tick();
    await tick();
    assert.ok(requests.some((u) => u.includes("root=9007199254740993")));
    assert.equal(doc.querySelectorAll("tbody tr").length, 3);
    assert.match(doc.querySelector("tbody").textContent, /下载中.bin/);
    assert.doesNotMatch(doc.querySelector("tbody").textContent, /4 个文件夹/);
    const done = [...doc.querySelectorAll("[role=tab]")].find((t) =>
      t.textContent.startsWith("已完成"),
    );
    done.click();
    await tick();
    await tick();
    assert.equal(doc.querySelectorAll("tbody tr").length, 1);
    assert.match(doc.querySelector("tbody").textContent, /已完成.bin/);
    assert.ok(requests.some((u) => u.includes("status=done")));
  } finally {
    dom.window.close();
  }
});

test("failed refresh preserves visible data and disables queue and task mutation controls", async () => {
  const { dom, setFailure } = await render();
  try {
    setFailure();
    const doc = dom.window.document;
    [...doc.querySelectorAll("button")]
      .find((b) => b.textContent === "刷新")
      .click();
    await tick();
    await tick();
    assert.match(
      doc.querySelector("[role=alert]").textContent,
      /本地服务暂不可用/,
    );
    assert.equal(doc.querySelector("tbody .row-actions button").disabled, true);
    assert.equal(
      [...doc.querySelectorAll("button")].find(
        (b) => b.textContent === "暂停队列",
      ).disabled,
      true,
    );
    assert.equal(doc.querySelectorAll("tbody tr").length, 1);
  } finally {
    dom.window.close();
  }
});

test("unknown source operation is shown as postprocessing, not waiting for directory download", async () => {
  const { dom } = await render({
    status: "source_unknown",
    mode: "delete",
    files: 0,
    folders: 1,
    prepared: false,
    active: 0,
    speed: 0,
    can_pause: false,
    can_cancel: false,
  });
  try {
    const doc = dom.window.document;
    const table = doc.querySelector("tbody");
    assert.match(table.textContent, /网盘删除结果待核对/);
    assert.doesNotMatch(table.textContent, /正在等待目录内容|1 个文件夹/);
    doc.querySelector(".file-name").click();
    await tick();
    await tick();
    assert.match(
      doc.querySelector(".discovery-note").textContent,
      /本地阶段已结束/,
    );
    assert.equal(doc.querySelector(".metrics"), null);
  } finally {
    dom.window.close();
  }
});

test("root statistics show measured download duration and average without inventing historical values", async () => {
  const { dom } = await render({ transfer_statistics: { active_ns: 65000000000, network_bytes: 6656000 } });
  try {
    const doc = dom.window.document;
    assert.match(doc.querySelector("tbody").textContent, /1分05秒/);
    assert.match(doc.querySelector("tbody").textContent, /100.00 KB\/s/);
    doc.querySelector(".file-name").click();
    await tick(); await tick();
    assert.match(doc.querySelector(".summary-meta").textContent, /下载用时.*1分05秒/);
  } finally { dom.window.close(); }
  const legacy = await render();
  try { assert.match(legacy.dom.window.document.querySelector("tbody").textContent, /历史统计未记录/); }
  finally { legacy.dom.window.close(); }
});
