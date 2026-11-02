// Read-only visual fixture. Never reads user credentials or connects to Baidu.
import http from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
const base = fileURLToPath(
  new URL("../../internal/webui/app/", import.meta.url),
);
const root = {
  id: "9007199254740993",
  name: "AI量化交易第1期",
  path: "/NAS_Download_Queue_preview/AI量化交易第1期",
  status: "downloading",
  local_path: "D:\\Downloads\\AI量化交易第1期",
  size: 3865470566,
  progress: 2920577761,
  speed: 1843200,
  files: 278,
  folders: 36,
  completed: 252,
  active: 2,
  prepared: true,
  mode: "keep",
  can_pause: true,
  can_cancel: true,
};
const roots = [
  root,
  {
    ...root,
    id: "2",
    name: "设计资源与参考",
    path: "/NAS_Download_Queue_preview/设计资源与参考",
    status: "pending",
    files: 0,
    folders: 0,
    completed: 0,
    active: 0,
    size: 0,
    progress: 0,
    speed: 0,
  },
  {
    ...root,
    id: "3",
    name: "机器学习基础课程",
    path: "/NAS_Download_Queue_preview/机器学习基础课程",
    status: "done",
    files: 42,
    folders: 8,
    completed: 42,
    active: 0,
    progress: 3865470566,
    speed: 0,
    can_pause: false,
    can_cancel: false,
  },
];
const files = [
  ["csi300_dl_train.pkl", "downloading", 802340864, 755986432],
  ["课程视频 · 第十二讲.mp4", "downloading", 1284532224, 283467841],
  ["课程讲义.pdf", "pending", 8204500, 0],
  ["示例数据集.zip", "pending", 182040500, 0],
  ["01-量化世界的生存法则.pdf", "done", 2453662, 2453662],
  ["开营与学习指南.pdf", "done", 2222981, 2222981],
].map(([name, status, size, progress], i) => ({
  ...root,
  id: String(10 + i),
  name,
  status,
  size,
  progress,
  path: root.path + "/12-论文复现与策略进化/" + name,
  local_path: root.local_path + "\\12-论文复现与策略进化\\" + name,
  can_pause: false,
  can_cancel: false,
  speed: status === "downloading" ? 921600 : 0,
}));
const server = http.createServer(async (req, res) => {
  const u = new URL(req.url, "http://127.0.0.1");
  res.setHeader("Cache-Control", "no-store");
  if (req.method !== "GET") {
    res.writeHead(405);
    res.end("只读界面预览");
    return;
  }
  if (u.pathname.startsWith("/api/")) {
    res.setHeader("Content-Type", "application/json");
    if (u.pathname === "/api/health") {
      res.end(
        JSON.stringify({
          authentication: { ready: true },
          queue_pause: { paused: false },
          queue_healthy: true,
          version: "界面预览 · 模拟数据",
          preview: true,
        }),
      );
      return;
    }
    if (u.pathname === "/api/task-workspace") {
      let rows = u.searchParams.get("root") ? files : roots;
      const status = u.searchParams.get("status"),
        q = u.searchParams.get("q");
      if (status) rows = rows.filter((t) => t.status === status);
      if (q) rows = rows.filter((t) => t.name.includes(q));
      res.end(
        JSON.stringify({
          items: rows,
          root: u.searchParams.get("root") ? root : null,
          total: rows.length,
          root_count: roots.length,
          offset: 0,
          limit: 50,
          counts: { downloading: 2, pending: 2, done: 2 },
        }),
      );
      return;
    }
    if (u.pathname.includes("/events")) {
      res.end(JSON.stringify({ events: [] }));
      return;
    }
    res.writeHead(404);
    res.end("{}");
    return;
  }
  try {
    const relative =
      u.pathname === "/" ? "index.html" : u.pathname.replace(/^\/app\//, "");
    const full = path.resolve(base, relative);
    if (!full.startsWith(path.resolve(base) + path.sep)) {
      res.writeHead(403);
      res.end();
      return;
    }
    const content = await readFile(full);
    res.setHeader(
      "Content-Type",
      full.endsWith(".css")
        ? "text/css"
        : full.endsWith(".js")
          ? "text/javascript"
          : "text/html",
    );
    res.end(content);
  } catch {
    res.writeHead(404);
    res.end();
  }
});
server.listen(18091, "127.0.0.1", () =>
  console.log("Read-only fixture: http://127.0.0.1:18091"),
);
