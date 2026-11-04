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
async function render(overrides = {}, fileOverrides = {}, options = {}) {
  const currentRoot = { ...root, ...overrides };
  const dom = new JSDOM(html, {
    url: options.url || "http://localhost/",
    runScripts: "outside-only",
    pretendToBeVisual: true,
  });
  let fail = false;
  const requests = [];
  const methods = [];
  let refresh;
  dom.window.setInterval = (fn) => { refresh = fn; return 1; };
  dom.window.clearInterval = () => {};
  dom.window.fetch = async (url, init = {}) => {
    requests.push(String(url));
    methods.push(init.method || 'GET');
    const custom = await options.respond?.(String(url), init);
    if (custom) return custom;
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
            : files.map((file) => ({ ...file, ...fileOverrides }))
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
  options.beforeMount?.(dom.window);
  dom.window.eval(bundle);
  await tick();
  await tick();
  return {
    dom,
    requests,
    methods,
    setFailure: () => {
      fail = true;
    },
    poll: () => refresh?.(),
  };
}

test("download workspace exposes the independent path settings navigation", async () => {
  const { dom } = await render();
  try {
    const link = dom.window.document.querySelector('.app-header a[data-module="settings"]');
    assert.ok(link);
    assert.equal(link.getAttribute('href'), '/settings/paths');
    assert.match(link.textContent, /设置/);
  } finally { dom.window.close(); }
});

test("local search validation stays visible across polling without changing connection or task eligibility", async () => {
  const { dom, requests, poll } = await render();
  try {
    const doc = dom.window.document, input = doc.querySelector('form.search input');
    input.value = '词'.repeat(129);
    input.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
    const before = requests.length;
    doc.querySelector('form.search').dispatchEvent(new dom.window.Event('submit', { bubbles: true, cancelable: true }));
    await tick();
    assert.equal(requests.length, before, 'invalid search must not issue requests');
    assert.match(doc.querySelector('[role=alert]').textContent, /最多 128/);
    assert.doesNotMatch(doc.body.textContent, /连接异常/);
    assert.equal(doc.querySelector('tbody .row-actions button').disabled, false);
    poll(); await tick(); await tick();
    assert.match(doc.querySelector('[role=alert]').textContent, /最多 128/);
    assert.ok(requests.slice(before).filter(u => u.includes('task-workspace')).every(u => !new URL(u, 'http://localhost').searchParams.get('q')));
    input.value = '课程'; input.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
    doc.querySelector('form.search').dispatchEvent(new dom.window.Event('submit', { bubbles: true, cancelable: true }));
    await tick(); await tick();
    assert.doesNotMatch(doc.body.textContent, /最多 128/);
  } finally { dom.window.close(); }
});

test("a rejected queue action has its own feedback and does not report a read connection failure", async () => {
  const { dom, methods, poll } = await render({}, {}, { respond: async (_url, init) => init.method === 'POST' ? { ok: false, status: 409, text: async () => '队列状态已变化' } : null });
  try {
    const doc = dom.window.document;
    [...doc.querySelectorAll('button')].find(b => b.textContent.trim() === '暂停队列').click();
    await tick(); await tick();
    assert.match(doc.querySelector('[role=alert]').textContent, /操作未确认成功.*队列状态已变化/);
    assert.doesNotMatch(doc.body.textContent, /连接异常|读取异常/);
    poll(); await tick(); await tick();
    assert.match(doc.querySelector('[role=alert]').textContent, /队列状态已变化/);
    assert.equal(methods.filter(m => m === 'POST').length, 1);
  } finally { dom.window.close(); }
});

test("retry uses a page confirmation, cancel does not post and repeated confirmation posts only once", async () => {
  let nativeCalls = 0, releasePost;
  const { dom, methods } = await render({ status: 'failed', can_retry: true }, {}, {
    beforeMount: w => { w.confirm = () => { nativeCalls++; return false; }; },
    respond: async (_url, init) => init.method === 'POST' ? new Promise(resolve => { releasePost = () => resolve({ ok: true, json: async () => ({}) }); }) : null,
  });
  try {
    const doc = dom.window.document;
    doc.querySelector('[aria-label="查看详情"]').click(); await tick(); await tick();
    doc.querySelector('[aria-label="查看任务信息"]').click(); await tick();
    [...doc.querySelectorAll('button')].find(b => b.textContent.trim() === '重试任务').click(); await tick();
    assert.equal(nativeCalls, 0);
    let confirm = doc.querySelector('[aria-labelledby="action-confirm-title"]');
    assert.ok(confirm); assert.match(confirm.textContent, /课程目录/); assert.match(confirm.textContent, /queue\/课程目录/);
    [...confirm.querySelectorAll('button')].find(b => b.textContent.trim() === '返回').click(); await tick();
    assert.equal(methods.filter(m => m === 'POST').length, 0);
    [...doc.querySelectorAll('button')].find(b => b.textContent.trim() === '重试任务').click(); await tick();
    confirm = doc.querySelector('[aria-labelledby="action-confirm-title"]');
    const submit = [...confirm.querySelectorAll('button')].find(b => b.textContent.trim() === '确认重试');
    submit.click(); submit.click(); await tick();
    assert.equal(methods.filter(m => m === 'POST').length, 1);
    assert.equal(submit.disabled, true);
    releasePost(); await tick(); await tick();
    assert.equal(doc.querySelector('[aria-labelledby="action-confirm-title"]'), null);
    assert.equal(doc.querySelector('.detail-drawer'), null, 'accepted action closes the stale task snapshot');
    assert.match(doc.querySelector('[data-testid="action-feedback"]').textContent,/请求已接受/);
  } finally { releasePost?.(); dom.window.close(); }
});

test("cancel confirmation explains its limits and Escape closes it without posting", async () => {
  const { dom, methods } = await render();
  try {
    const doc = dom.window.document;
    doc.querySelector('[aria-label="查看详情"]').click(); await tick(); await tick();
    doc.querySelector('[aria-label="查看任务信息"]').click(); await tick();
    [...doc.querySelectorAll('button')].find(b => b.textContent.trim() === '取消任务').click(); await tick();
    const panel = doc.querySelector('[aria-labelledby="action-confirm-title"]');
    assert.match(panel.textContent, /已发送的网盘操作不能撤回/);
    assert.equal(doc.querySelector('.detail-drawer').parentElement.hasAttribute('inert'), true);
    panel.dispatchEvent(new dom.window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); await tick();
    assert.equal(doc.querySelector('[aria-labelledby="action-confirm-title"]'), null);
    assert.equal(doc.querySelector('.detail-drawer').parentElement.hasAttribute('inert'), false);
    assert.equal(methods.filter(m => m === 'POST').length, 0);
  } finally { dom.window.close(); }
});

test("an accepted task action remains distinct from a failed status read and is never automatically reposted", async () => {
  let accepted = false;
  const { dom, methods, poll } = await render({}, {}, { respond: async (_url, init) => {
    if (init.method === 'POST') { accepted = true; return { ok: true, json: async () => ({}) }; }
    if (accepted) return { ok: false, status: 503, text: async () => '状态暂不可读' };
  } });
  try {
    const doc = dom.window.document;
    doc.querySelector('[aria-label="暂停任务"]').click(); await tick(); await tick();
    const feedback = doc.querySelector('[data-testid="action-feedback"]');
    assert.ok(feedback); assert.match(feedback.textContent, /暂停任务.*请求已接受/);
    assert.match(feedback.textContent, /读取.*失败|未成功/);
    assert.doesNotMatch(doc.body.textContent, /操作未确认成功/);
    poll(); await tick(); await tick();
    assert.equal(methods.filter(m => m === 'POST').length, 1);
    assert.match(feedback.textContent, /请求已接受/);
    accepted = false; poll(); await tick(); await tick();
    assert.match(feedback.textContent, /请求已接受/);
    assert.doesNotMatch(feedback.textContent, /读取.*失败|未成功/);
  } finally { dom.window.close(); }
});

test("task events page with exact string cursors and retain the current page on a failed older read", async () => {
  let fail = false;
  const { dom, requests } = await render({}, {}, { respond: async url => {
    if (!url.includes('/events?')) return;
    if (fail) return { ok: false, status: 503, text: async () => '事件暂不可读' };
    const older = new URL(url,'http://localhost').searchParams.get('before_sequence');
    return { ok: true, json: async () => ({events:[{sequence:older?'9007199254740992':'9007199254740993',kind:older?'older-event':'latest-event'}],next_before_sequence:older?null:'9007199254740993'}) };
  } });
  try {
    const doc=dom.window.document;
    doc.querySelector('[aria-label="查看详情"]').click(); await tick(); await tick();
    doc.querySelector('[aria-label="查看任务信息"]').click(); await tick(); await tick();
    assert.match(doc.querySelector('.timeline').textContent,/latest-event/);
    doc.querySelector('.timeline details').open = true;
    let older=doc.querySelector('[data-testid="older-events"]'); assert.ok(older);
    fail=true; older.click(); await tick(); await tick();
    assert.match(doc.querySelector('.timeline').textContent,/latest-event/);
    fail=false; older.click(); await tick(); await tick();
    assert.match(doc.querySelector('.timeline').textContent,/older-event/);
    assert.equal(doc.querySelector('.timeline details').open, false, 'a different event starts collapsed');
    assert.doesNotMatch(doc.querySelector('.timeline').textContent,/latest-event/);
    assert.ok(requests.some(u=>u.includes('before_sequence=9007199254740993') && u.includes('sequence_format=string')));
    assert.equal(doc.querySelector('[data-testid="older-events"]').disabled,true);
    doc.querySelector('[data-testid="newer-events"]').click(); await tick(); await tick();
    assert.match(doc.querySelector('.timeline').textContent,/latest-event/);
  } finally {dom.window.close();}
});

test("a late event response cannot overwrite a reopened task panel", async () => {
  let finishOld, calls=0;
  const {dom} = await render({}, {}, {respond: async url => {
    if (!url.includes('/events?')) return;
    if (++calls===1) return new Promise(resolve=>{finishOld=()=>resolve({ok:true,json:async()=>({events:[{kind:'stale-event'}]})});});
    return {ok:true,json:async()=>({events:[{kind:'fresh-event'}],next_before_sequence:null})};
  }});
  try {
    const doc=dom.window.document;
    doc.querySelector('[aria-label="查看详情"]').click(); await tick(); await tick();
    doc.querySelector('[aria-label="查看任务信息"]').click(); await tick();
    doc.querySelector('[aria-label="关闭详情"]').click(); await tick();
    doc.querySelector('[aria-label="查看任务信息"]').click(); await tick(); await tick();
    assert.match(doc.querySelector('.timeline').textContent,/fresh-event/);
    finishOld(); await tick(); await tick();
    assert.match(doc.querySelector('.timeline').textContent,/fresh-event/);
    assert.doesNotMatch(doc.querySelector('.timeline').textContent,/stale-event/);
  } finally {finishOld?.();dom.window.close();}
});

test("event evidence distinguishes UTC clocks and escapes evidence without inventing missing times", async () => {
  const {dom} = await render({}, {}, {respond: async url => url.includes('/events?') ? {ok:true,json:async()=>({events:[{sequence:'9007199254740993',kind:'download_completed',recorded_at_utc:'2026-11-03T10:00:00+08:00',occurred_at_utc:'invalid-time',response_observed_at_utc:'2026-11-03T02:00:01Z',attempt_id:'attempt-test',operation_id:'op-test',evidence:{path:'<img src=x onerror=alert(1)>'}}],next_before_sequence:null})}:null});
  try {
    const doc=dom.window.document;
    doc.querySelector('[aria-label="查看详情"]').click(); await tick(); await tick();
    doc.querySelector('[aria-label="查看任务信息"]').click(); await tick(); await tick();
    const timeline=doc.querySelector('.timeline');
    assert.match(timeline.textContent,/本机记录 UTC.*2026-11-03T02:00:00.000Z/s);
    assert.match(timeline.textContent,/事件发生 UTC.*时间格式无效/s);
    assert.match(timeline.textContent,/响应观察 UTC.*2026-11-03T02:00:01.000Z/s);
    assert.doesNotMatch(timeline.textContent,/云端观察 UTC/);
    assert.equal(timeline.querySelector('details').open,false);
    assert.match(timeline.textContent,/9007199254740993/);
    assert.match(timeline.textContent,/attempt-test/);
    assert.match(timeline.querySelector('pre').textContent,/<img/);
    assert.equal(timeline.querySelector('img'),null);
  } finally {dom.window.close();}
});

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

test("task properties distinguish a recorded historical destination from present file existence", async () => {
  const { dom } = await render({ destination: { path: root.local_path, attempt_id: "attempt-1", recorded_at_utc: "2026-11-02T14:15:16Z" } });
  try {
    const doc = dom.window.document;
    doc.querySelector('[aria-label="查看详情"]').click();
    await tick();
    await tick();
    doc.querySelector('[aria-label="查看任务信息"]').click();
    await tick();
    assert.match(doc.querySelector('.modal-backdrop').textContent, /2026-11-02T14:15:16Z/);
    assert.match(doc.querySelector('.modal-backdrop').textContent, /不代表文件当前仍在此处/);
  } finally { dom.window.close(); }
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

test("saved root counts show provenance and previous attempts remain explicitly historical", async () => {
  for (const source of ["current_attempt", "previous_attempt", "unrecorded"]) {
    const { dom } = await render({
      prepared: source === "current_attempt",
      files: source === "current_attempt" ? 3 : 0,
      completed: source === "current_attempt" ? 1 : 0,
      download_history: { source, summary: source === "unrecorded" ? undefined : { files: 3, completed: 1, folders: 4, size: 1000, manifest: { published_at_utc: "2026-11-02T15:16:17Z" } } },
    });
    try {
      const doc = dom.window.document;
      doc.querySelector('.file-name').click();
      await tick(); await tick();
      const note = doc.querySelector('.history-provenance');
      assert.ok(note, "missing provenance");
      if (source === "current_attempt") {
        assert.match(note.textContent, /本次已保存清单/);
        assert.match(note.textContent, /2026-11-02T15:16:17Z/);
        assert.match(note.textContent, /不代表当前网盘或本地文件已重新核对/);
      } else if (source === "previous_attempt") {
        assert.match(note.textContent, /上一轮/);
        assert.match(note.textContent, /3 个文件/);
        assert.match(note.textContent, /不计入本轮/);
      } else {
        assert.match(note.textContent, /尚无已保存清单/);
        assert.doesNotMatch(note.textContent, /空文件夹/);
      }
    } finally { dom.window.close(); }
  }
});

test("file details show interrupted and completion evidence without requesting nonexistent child job events", async () => {
  for (const verified of [false, true]) {
    const { dom, requests } = await render({}, { status: verified ? "done" : "pending", file_evidence: { observed_at_utc: "2026-11-02T01:02:03Z", observation_source: "directory_listing", interrupted: !verified, historical: false, result: verified ? { kind: "local_verified", sha256: "a".repeat(64), recorded_at_utc: "2026-11-02T04:05:06Z" } : null } });
    try {
      const doc = dom.window.document;
      doc.querySelector('.file-name').click(); await tick(); await tick();
      if (!verified) assert.match(doc.querySelector('tbody').textContent, /上次中断/);
      doc.querySelector('tbody .file-name').click(); await tick(); await tick();
      const drawer = doc.querySelector('.modal-backdrop');
      assert.match(drawer.textContent, /2026-11-02T01:02:03Z/);
      assert.match(drawer.textContent, /本轮目录列表/);
      if (verified) {
        assert.match(drawer.textContent, /本地文件完整校验通过/);
        assert.match(drawer.textContent, /2026-11-02T04:05:06Z/);
        assert.match(drawer.textContent, new RegExp("a".repeat(64)));
      } else assert.match(drawer.textContent, /中断后尚未完成/);
      assert.ok(!requests.some(url => url.includes('/api/tasks/2/events')));
    } finally { dom.window.close(); }
  }
});

test("manifest change preserves old rows and requires explicit reload before fetching the new first page", async () => {
  const oldID = "a".repeat(32), newID = "b".repeat(32);
  let changed = false;
  const { dom, requests, poll } = await render({}, {}, { respond: async (url) => {
    const u = new URL(url, "http://localhost");
    if (u.pathname !== "/api/task-workspace" || !u.searchParams.get("root")) return;
    if (changed && u.searchParams.get("manifest") === oldID) return { ok: false, status: 409, text: async () => JSON.stringify({ code: "manifest_changed", message: "下载清单已更新，请重新载入第一页。" }) };
    return { ok: true, status: 200, json: async () => ({ root: { ...root, name: changed ? "新轮次目录" : "旧轮次目录" }, items: [{ ...files[1], name: changed ? "新页面.bin" : "旧页面.bin" }], total: 120, offset: Number(u.searchParams.get("offset")), limit: 50, counts: { pending: 120 }, root_count: 1, manifest_id: changed ? newID : oldID }) };
  } });
  try {
    const doc = dom.window.document;
    doc.querySelector('.file-name').click(); await tick(); await tick();
    changed = true;
    [...doc.querySelectorAll('button')].find(b => b.textContent.trim() === "下一页").click(); await tick(); await tick();
    assert.ok(requests.some(url => new URL(url, "http://localhost").searchParams.get('manifest') === oldID));
    assert.match(doc.querySelector('[role=alert]').textContent, /清单已更新/);
    assert.match(doc.querySelector('tbody').textContent, /旧页面.bin/);
    assert.match(doc.querySelector('.table-footer').textContent, /第 1 \/ 3 页/);
    assert.equal(doc.querySelector('.summary-actions button').disabled, true);
    const before = requests.length; poll(); await tick(); await tick();
    assert.equal(requests.length, before, "conflict triggered automatic retry");
    [...doc.querySelectorAll('button')].find(b => b.textContent.trim() === "重新载入清单").click(); await tick(); await tick();
    assert.match(doc.querySelector('tbody').textContent, /新页面.bin/);
    const last = new URL(requests.filter(url => url.includes('/api/task-workspace')).at(-1), "http://localhost");
    assert.equal(last.searchParams.get('offset'), '0');
    assert.ok(!last.searchParams.get('manifest'));
  } finally { dom.window.close(); }
});

test("a late previous request cannot replace the manifest selected after navigating back", async () => {
  const oldID = "c".repeat(32), newID = "d".repeat(32);
  let resolveOld, detailRequests = 0;
  const response = (id, name) => ({ ok: true, status: 200, json: async () => ({ root, items: [{ ...files[1], name }], total: 120, offset: 0, limit: 50, counts: { pending: 120 }, root_count: 1, manifest_id: id }) });
  const { dom, requests } = await render({}, {}, { respond: async url => {
    const u = new URL(url, "http://localhost");
    if (u.pathname !== '/api/task-workspace' || !u.searchParams.get('root')) return;
    detailRequests++;
    if (detailRequests === 1) return new Promise(resolve => { resolveOld = resolve; });
    return response(newID, "当前轮次.bin");
  } });
  try {
    const doc = dom.window.document;
    doc.querySelector('.file-name').click(); await tick(); await tick();
    [...doc.querySelectorAll('button')].find(b => b.textContent.includes('返回任务总览')).click(); await tick(); await tick();
    doc.querySelector('.file-name').click(); await tick(); await tick();
    resolveOld(response(oldID, "旧请求.bin")); await tick(); await tick();
    assert.match(doc.querySelector('tbody').textContent, /当前轮次.bin/);
    assert.doesNotMatch(doc.querySelector('tbody').textContent, /旧请求.bin/);
    [...doc.querySelectorAll('button')].find(b => b.textContent.trim() === '下一页').click(); await tick(); await tick();
    const last = new URL(requests.filter(url => url.includes('/api/task-workspace')).at(-1), 'http://localhost');
    assert.equal(last.searchParams.get('manifest'), newID);
  } finally { dom.window.close(); }
});

test("download URL restores exact task manifest search and page while dropping action-like parameters", async () => {
  const id = '9007199254740993', manifest = 'a'.repeat(32), query = 'Ä课程😀%_&+';
  const params = new URLSearchParams({ task: id, status: 'done', q: query, offset: '50', manifest, action: 'delete', 'admin-key': 'must-not-forward' });
  const { dom, requests, methods } = await render({}, {}, { url: 'http://localhost/?' + params, respond: async url => {
    const u = new URL(url, 'http://localhost'); if (u.pathname !== '/api/task-workspace') return;
    return { ok: true, status: 200, json: async () => ({ root, items: [files[2]], total: 120, offset: Number(u.searchParams.get('offset')), limit: 50, counts: { done: 120 }, root_count: 1, manifest_id: manifest }) };
  } });
  try {
    const request = new URL(requests.find(url => url.includes('/api/task-workspace')), 'http://localhost');
    assert.equal(request.searchParams.get('root'), id); assert.equal(request.searchParams.get('status'), 'done');
    assert.equal(request.searchParams.get('q'), query); assert.equal(request.searchParams.get('offset'), '50'); assert.equal(request.searchParams.get('manifest'), manifest);
    assert.ok(!dom.window.location.search.includes('admin-key') && !dom.window.location.search.includes('action'));
    assert.ok(!requests.some(url => url.includes('must-not-forward'))); assert.ok(methods.every(method => method === 'GET'));
    assert.equal(dom.window.document.querySelector('[aria-label="搜索文件名或路径"]').value, query);
  } finally { dom.window.close(); }
});

test("download URL rejects invalid identifiers and restores filters through browser history without writes", async () => {
  const invalid = await render({}, {}, { url: 'http://localhost/?task=9223372036854775808&status=bad&q=' + 'x'.repeat(129) + '&offset=-1&manifest=' + 'a'.repeat(32) + '&action=cancel' });
  try {
    const u = new URL(invalid.requests.find(url => url.includes('/api/task-workspace')), 'http://localhost');
    assert.equal(u.searchParams.get('root'), ''); assert.equal(u.searchParams.get('status'), ''); assert.equal(u.searchParams.get('q'), ''); assert.equal(u.searchParams.get('offset'), '0');
    assert.equal(invalid.dom.window.location.search, '');
  } finally { invalid.dom.window.close(); }
  const { dom, requests, methods } = await render({}, {}, { url: 'http://localhost/?status=paused&q=课程', respond: async url => {
    const u = new URL(url, 'http://localhost'); if (u.pathname !== '/api/task-workspace') return;
    return { ok: true, status: 200, json: async () => ({ items: [root], root: null, total: 120, offset: Number(u.searchParams.get('offset')), limit: 50, root_count: 120, counts: {} }) };
  } });
  const traverse = async direction => {
    const popped = new Promise((resolve, reject) => { const timeout = setTimeout(() => reject(Error('no popstate')), 2000); dom.window.addEventListener('popstate', () => { clearTimeout(timeout); resolve(); }, { once: true }); });
    dom.window.history[direction](); await popped; await tick(); await tick();
  };
  try {
    const doc = dom.window.document;
    [...doc.querySelectorAll('[role=tab]')].find(t => t.textContent.startsWith('已完成')).click(); await tick(); await tick();
    assert.equal(new URL(dom.window.location.href).searchParams.get('status'), 'done');
    [...doc.querySelectorAll('button')].find(b => b.textContent.trim() === '下一页').click(); await tick(); await tick();
    assert.equal(new URL(dom.window.location.href).searchParams.get('offset'), '50');
    await traverse('back'); assert.equal(new URL(requests.filter(url => url.includes('/api/task-workspace')).at(-1), 'http://localhost').searchParams.get('offset'), '0');
    await traverse('back'); assert.equal(new URL(requests.filter(url => url.includes('/api/task-workspace')).at(-1), 'http://localhost').searchParams.get('status'), 'paused');
    await traverse('forward'); assert.equal(new URL(dom.window.location.href).searchParams.get('status'), 'done');
    assert.ok(methods.every(method => method === 'GET'));
  } finally { dom.window.close(); }
});

test("file panel copies exact observed paths and reports clipboard failure without inventing a local target", async () => {
  for (const [localPath, denied] of [['D:/Recorded/文件.bin', false], ['', false], ['D:/Recorded/文件.bin', true]]) {
    const written = []; let alerts = 0;
    const { dom } = await render({}, { local_path: localPath, file_evidence: { observed_at_utc: '2026-11-02T01:02:03Z', observation_source: 'directory_listing', interrupted: false, historical: !localPath } }, { beforeMount: window => {
      Object.defineProperty(window.navigator, 'clipboard', { configurable: true, value: { writeText: async value => { if (denied) throw Error('denied'); written.push(value); } } });
      window.alert = () => { alerts++; };
    } });
    try {
      const doc = dom.window.document;
      doc.querySelector('.file-name').click(); await tick(); await tick();
      doc.querySelector('tbody .file-name').click(); await tick(); await tick();
      const cloud = doc.querySelector('[aria-label="复制网盘路径"]'), local = doc.querySelector('[aria-label="复制本地目标"]');
      assert.ok(cloud && local);
      cloud.click(); await tick(); await tick();
      assert.match(doc.querySelector('[role=status]').textContent, denied ? /复制失败/ : /网盘路径已复制/);
      if (!denied) assert.equal(written[0], files[0].path);
      assert.equal(local.disabled, !localPath);
      const before = written.length; local.click(); await tick(); await tick();
      if (!localPath) assert.equal(written.length, before);
      else if (!denied) { assert.equal(written.at(-1), localPath); assert.match(doc.querySelector('[role=status]').textContent, /本地目标已复制/); }
      assert.equal(alerts, 0);
    } finally { dom.window.close(); }
  }
});

test("visible background polling preserves the submitted search while a new query is still a draft", async () => {
  const { dom, requests, poll } = await render({}, {}, { url: 'http://localhost/?q=old&offset=50', respond: async url => {
    const u = new URL(url, 'http://localhost'); if (u.pathname !== '/api/task-workspace') return;
    return { ok: true, status: 200, json: async () => ({ root: null, items: [root], total: 120, offset: Number(u.searchParams.get('offset')), limit: 50, counts: {}, root_count: 120 }) };
  } });
  try {
    assert.equal(dom.window.document.hidden, false);
    const input = dom.window.document.querySelector('[aria-label="搜索文件名或路径"]');
    input.value = 'new'; input.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
    const historyLength = dom.window.history.length;
    poll(); await tick(); await tick();
    const last = new URL(requests.filter(url => url.includes('/api/task-workspace')).at(-1), 'http://localhost');
    assert.equal(last.searchParams.get('q'), 'old'); assert.equal(last.searchParams.get('offset'), '50');
    assert.equal(new URL(dom.window.location.href).searchParams.get('q'), 'old'); assert.equal(input.value, 'new'); assert.equal(dom.window.history.length, historyLength);
    input.form.dispatchEvent(new dom.window.Event('submit', { bubbles: true, cancelable: true })); await tick(); await tick();
    const submitted = new URL(requests.filter(url => url.includes('/api/task-workspace')).at(-1), 'http://localhost');
    assert.equal(submitted.searchParams.get('q'), 'new'); assert.equal(submitted.searchParams.get('offset'), '0'); assert.equal(dom.window.history.length, historyLength + 1);
  } finally { dom.window.close(); }
});

test("returning to the overview clears the detail search that had been submitted", async () => {
  const { dom, requests } = await render({}, {}, { url: 'http://localhost/?task=9007199254740993&q=old' });
  try {
    [...dom.window.document.querySelectorAll('button')].find(b => b.textContent.includes('返回任务总览')).click(); await tick(); await tick();
    const last = new URL(requests.filter(url => url.includes('/api/task-workspace')).at(-1), 'http://localhost');
    assert.equal(last.searchParams.get('root'), ''); assert.equal(last.searchParams.get('q'), ''); assert.equal(dom.window.location.search, '');
  } finally { dom.window.close(); }
});

test("download route bounds queries by Unicode code points instead of UTF-16 units", async () => {
  for (const length of [128, 129]) {
    const query = '😀'.repeat(length), { dom, requests } = await render({}, {}, { url: 'http://localhost/?q=' + encodeURIComponent(query) });
    try {
      const sent = new URL(requests.find(url => url.includes('/api/task-workspace')), 'http://localhost').searchParams.get('q');
      assert.equal(sent, length === 128 ? query : '');
    } finally { dom.window.close(); }
  }
});

test("out-of-range download pages recover by replacing the URL and do not keep unknown fragments", async () => {
  const { dom, requests, methods } = await render({}, {}, { url: 'http://localhost/?offset=999&action=delete#admin-key=discard', respond: async url => {
    const u = new URL(url, 'http://localhost'); if (u.pathname !== '/api/task-workspace') return;
    return { ok: true, status: 200, json: async () => ({ root: null, items: [root], total: 55, offset: Math.min(Number(u.searchParams.get('offset')), 55), limit: 50, counts: {}, root_count: 55 }) };
  } });
  try {
    await tick(); await tick();
    const pages = requests.filter(url => url.includes('/api/task-workspace')).map(url => new URL(url, 'http://localhost').searchParams.get('offset'));
    assert.deepEqual(pages, ['999', '50']);
    assert.equal(new URL(dom.window.location.href).searchParams.get('offset'), '50');
    assert.equal(dom.window.location.hash, ''); assert.equal(dom.window.history.length, 1); assert.ok(methods.every(method => method === 'GET'));
  } finally { dom.window.close(); }
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
    assert.match(doc.querySelector("tbody").textContent, /100 KiB\/s/);
    doc.querySelector(".file-name").click();
    await tick(); await tick();
    assert.match(doc.querySelector(".summary-meta").textContent, /下载用时.*1分05秒/);
  } finally { dom.window.close(); }
  const legacy = await render();
  try { assert.match(legacy.dom.window.document.querySelector("tbody").textContent, /历史统计未记录/); }
  finally { legacy.dom.window.close(); }
});
