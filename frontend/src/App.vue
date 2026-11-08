<script setup lang="ts">
import {formatBytes} from './format-bytes';
import { computed, onMounted, onUnmounted, ref } from "vue";
import { vModal } from "./modal";
import WorkspaceSidebar from "./WorkspaceSidebar.vue";
import DownloadSettings from "./DownloadSettings.vue";
import DownloadStateGuidance from "./DownloadStateGuidance.vue";
import { readDownloadRoute, downloadRouteURL, type DownloadRoute } from "./download-route";
import {
  Folder,
  Files,
  Settings,
  Search,
  ChevronRight,
  ArrowLeft,
  RefreshCw,
  Pause,
  Play,
  MoreHorizontal,
  CheckCircle2,
  AlertCircle,
  X,
  Copy,
} from "lucide-vue-next";
type Task = {
  file_evidence?: { observed_at_utc: string; observation_source: string; state_at_utc?: string; interrupted: boolean; historical: boolean; result?: { kind: string; sha256: string; recorded_at_utc: string } | null };
  download_history?: { source: string; reason?: string; summary?: { files: number; folders: number; size: number; completed: number; manifest: { published_at_utc?: string } } };
	transfer_statistics?: { active_ns: number; network_bytes: number; partial?: boolean; recorded_at_utc?: string } | null;
  id: string;
  name: string;
  path: string;
  status: string;
  local_path: string;
  destination?: { path: string; attempt_id?: string; recorded_at_utc: string };
  error?: string;
  size: number;
  progress: number;
  speed: number;
  files: number;
  folders: number;
  completed: number;
  active: number;
  mode: string;
  can_pause: boolean;
  can_resume: boolean;
  can_cancel: boolean;
  can_retry: boolean;
  can_clear?: boolean;
  clear_token?: string;
  prepared: boolean;
};
type Workspace = {
  manifest_id?: string;
  items: Task[];
  root: Task | null;
  total: number;
  offset: number;
  limit: number;
  counts: Record<string, number>;
  root_count: number;
};
const labels: Record<string, string> = {
  downloading: "下载中",
  pending: "等待下载",
  retry_wait: "等待重试",
  paused: "已暂停",
  done: "已完成",
  failed: "下载失败",
  auth_wait: "等待授权",
  source_unknown: "网盘后处理待核对",
  cancelled: "已取消",
};
const tabs = [
  ["", "全部文件"],
  ["downloading", "下载中"],
  ["pending", "等待下载"],
  ["done", "已完成"],
  ["failed", "下载失败"],
  ["paused", "已暂停"],
];
const settingsOpen = ref(false);
const initialRoute = readDownloadRoute(location.search);
const rootID = ref(initialRoute.task);
const manifestID = ref(initialRoute.manifest);
const manifestConflict = ref(false);
const data = ref<Workspace | null>(null),
  health = ref<any>(null),
  status = ref(initialRoute.status),
  search = ref(initialRoute.q),
  appliedSearch = ref(initialRoute.q),
  offset = ref(initialRoute.offset),
  loading = ref(true),
  error = ref(""),
  inputError = ref(""),
  actionError = ref(""),
  busy = ref(""),
  dialog = ref<Task | null>(null),
  events = ref<any[]>([]),
  eventError = ref("");
const copyMessage = ref("");
const actionMessage = ref("");
const eventLoading = ref(false), eventPage = ref(0), eventNext = ref<string | null>(null);
const eventCursors = ref<(string | null)[]>([null]);
let eventGeneration = 0;
const confirmation = ref<{ task: Task; kind: "cancel" | "retry" | "clear" } | null>(null);
let workspaceActive=true;
let refreshInFlight = false;
let timer: ReturnType<typeof setInterval> | undefined,
  controller: AbortController | undefined,
  generation = 0;
let displayedRoute: DownloadRoute | undefined;
function routeState(): DownloadRoute { return { task: rootID.value, status: status.value, q: appliedSearch.value, offset: offset.value, manifest: manifestID.value }; }
function applyRoute(route: DownloadRoute) { inputError.value = ""; rootID.value = route.task; status.value = route.status; search.value = route.q; appliedSearch.value = route.q; offset.value = route.offset; manifestID.value = route.manifest; }
function writeRoute(mode: "push" | "replace", route = routeState()) {
  const url = downloadRouteURL(route);
  if (location.pathname + location.search + location.hash === url) return;
  if (mode === "push") history.pushState({}, "", url); else history.replaceState({}, "", url);
}
writeRoute("replace");
function completionText(t: Task): string {
  if (t.status === "source_unknown")
    return t.mode === "delete"
      ? "本地阶段已结束 · 网盘删除结果待核对"
      : "本地阶段已结束 · 网盘改名结果待核对";
  if (t.files) return `${t.completed} / ${t.files} 个${t.download_history?.source === "current_attempt" ? "已记录" : "已发现"}文件`;
  if (t.prepared) return "空文件夹";
  return t.status === "done" ? "历史文件明细未记录" : "正在等待目录内容";
}
function historyHint(t: Task) {
  const h = t.download_history;
  if (!h || h.source === "unrecorded" || !h.summary) return "尚无已保存清单；旧明细不按本地文件反推。";
  const at = h.summary.manifest.published_at_utc || "未记录";
  if (h.source === "previous_attempt") return `${h.reason === "root_identity_changed" ? "原身份对应的历史清单" : "上一轮清单"}：${h.summary.files} 个文件、${h.summary.folders} 个文件夹，已记录完成 ${h.summary.completed} 个；发布于 ${at}（UTC）。仅供历史核对，不计入本轮。`;
  return `本次已保存清单 · 发布于 ${at}（UTC）。完成数来自本地证据，不代表当前网盘或本地文件已重新核对。`;
}
const monitorLabel = computed(() => {
  const h = health.value;
  if (!h) return "正在连接监控";
  if (!h.authentication?.ready) return "等待百度授权";
  if (h.queue_monitor_retry_remaining_seconds > 0) return "目录监控等待重试";
  if (h.scan_history_available === false) return "监控记录暂不可读";
  if (h.scan_error) return "目录扫描异常";
  return h.scan_in_progress ? "正在检查网盘目录" : "目录监控正常";
});
const root = computed(() => data.value?.root),
  title = computed(() =>
    rootID.value ? root.value?.name || "任务详情" : "下载任务",
  );
const bytes = formatBytes;
// Browser acceptance found direct files repeated their name as the path subtitle.
function taskPathLabel(task: Task) {
  const relative = rootID.value && root.value && task.path.startsWith(root.value.path + "/")
    ? task.path.slice(root.value.path.length + 1) : task.path;
  return rootID.value && root.value && task.path.startsWith(root.value.path + "/") && relative === task.name ? "" : relative;
}

// Browser review found historical roots were incorrectly described as new empty queues.
const emptyState = computed(() => {
  if (appliedSearch.value || status.value) return {title: rootID.value ? "没有匹配的文件" : "没有匹配的任务", text: "请调整已应用的搜索或状态筛选，当前空结果不代表整个任务或目录为空。"};
  if (!rootID.value) return {title: "这里还没有下载任务", text: "添加网盘项目到监控目录后，下载会自动开始。"};
  const task = root.value;
  if (task && ["done", "source_unknown"].includes(task.status) && !task.prepared && !task.files) return {title: "历史文件明细未记录", text: "根任务记录仍保留；不根据当前本地文件反推历史清单，请结合任务事件核对。"};
  if (task?.prepared && !task.files) return {title: "清单中没有文件", text: "当前记录仅包含文件夹，文件夹不单独展示为下载条目。"};
  return {title: "当前没有可展示的文件", text: "请结合任务状态和清单依据核对；文件夹仅统计数量，空列表本身不能证明网盘目录为空。"};
});
const downloadDuration = (t: Task) => {
  const stats = t.transfer_statistics;
  if (!stats) return t.status === "pending" ? "尚未开始" : "历史统计未记录";
  const total = Math.floor(stats.active_ns / 1e9);
  if (total < 1) return stats.active_ns > 0 ? "不足1秒" : "0秒";
  const h = Math.floor(total / 3600), m = Math.floor(total / 60) % 60, sec = total % 60;
  return (h ? `${h}小时` : "") + (h || m ? `${m}分` : "") + `${String(sec).padStart(m || h ? 2 : 1, "0")}秒`;
};
const averageSpeed = (t: Task) => {
  const s = t.transfer_statistics;
  return s && s.active_ns > 0 ? bytes(s.network_bytes / (s.active_ns / 1e9)) + "/s" : "—";
};
const statisticsHint = "仅累计文件数据传输期间；并发时间不重复，暂停、离线、校验及网盘后处理不计入。平均速度按实际接收字节计算，包含重传，不含已有本地文件。异常退出可能丢失最后未保存的增量。";
const percent = (t: Task) =>
  t.status === "done"
    ? 100
    : t.size > 0
      ? Math.min(100, (t.progress / t.size) * 100)
      : 0;
async function api(url: string, options: RequestInit = {}) {
  const r = await fetch(url, options);
  if (r.status === 401) {
    location.assign(
      "/login?next=" + encodeURIComponent(location.pathname + location.search),
    );
    throw Error("请重新登录");
  }
  if (!r.ok) {
    const text = await r.text();
    let body;
    if (r.status === 409) { try { body = JSON.parse(text); } catch {} }
    if (body?.code === "manifest_changed") throw Object.assign(Error(body.message || "下载清单已更新，请重新载入第一页。"), { code: "manifest_changed" });
    throw Error(text || "请求失败");
  }
  return r.json();
}
async function load(quiet = false) {
  if (manifestConflict.value) return;
  if (quiet && refreshInFlight) return;
  refreshInFlight = true;
  const current = ++generation;
  controller?.abort();
  controller = new AbortController();
  if (!quiet) loading.value = true;
  try {
    const requestedRoute = routeState();
    const q = new URLSearchParams({
      root: requestedRoute.task,
      status: requestedRoute.status,
      q: requestedRoute.q,
      offset: String(requestedRoute.offset),
      limit: "50",
    });
    if (rootID.value && manifestID.value) q.set("manifest", manifestID.value);
    const [workspace, h] = await Promise.all([
      api("/api/task-workspace?" + q, { signal: controller.signal }),
      api("/api/health", { signal: controller.signal }),
    ]);
    if (current !== generation) return;
    manifestID.value = rootID.value && /^[a-f0-9]{32}$/.test(workspace.manifest_id || "") ? workspace.manifest_id : "";
    manifestConflict.value = false;
    const serverOffset = Number.isSafeInteger(workspace.offset) && workspace.offset >= 0 ? workspace.offset : requestedRoute.offset;
    const pageOffset = workspace.total > 0 && serverOffset >= workspace.total ? Math.floor((workspace.total - 1) / 50) * 50 : serverOffset;
    offset.value = pageOffset;
    if (pageOffset !== serverOffset) {
      writeRoute("replace", { ...requestedRoute, offset: pageOffset, manifest: manifestID.value });
      load();
      return;
    }
    displayedRoute = { ...requestedRoute, offset: pageOffset, manifest: manifestID.value };
    writeRoute("replace", displayedRoute);
    data.value = workspace;
    health.value = h;
    error.value = "";
  } catch (e: any) {
    if (e.name !== "AbortError" && current === generation) {
      error.value = e.message;
      manifestConflict.value = e.code === "manifest_changed";
      if (manifestConflict.value && displayedRoute) { applyRoute(displayedRoute); writeRoute("replace"); }
    }
  } finally {
    if (current === generation) {
      loading.value = false;
      refreshInFlight = false;
    }
  }
}
function reloadCurrent() {
  manifestID.value = "";
  manifestConflict.value = false;
  offset.value = 0;
  writeRoute("replace");
  load();
}
function open(t: Task) {
  inputError.value = "";
  manifestID.value = "";
  manifestConflict.value = false;
  data.value = null;
  rootID.value = t.id;
  status.value = "";
  offset.value = 0;
  search.value = "";
  appliedSearch.value = "";
  displayedRoute = undefined;
  writeRoute("push");
  load();
}
function back() {
  inputError.value = "";
  manifestID.value = "";
  manifestConflict.value = false;
  data.value = null;
  rootID.value = "";
  status.value = "";
  search.value = "";
  appliedSearch.value = "";
  offset.value = 0;
  displayedRoute = undefined;
  writeRoute("push");
  load();
}
function pop() {
  manifestID.value = "";
  manifestConflict.value = false;
  data.value = null;
  displayedRoute = undefined;
  applyRoute(readDownloadRoute(location.search));
  writeRoute("replace");
  load();
}
function filter(value: string) {
  if (manifestConflict.value) return;
  status.value = value;
  offset.value = 0;
  writeRoute("push");
  load();
}
function submitSearch() {
  if (manifestConflict.value) return;
  if (Array.from(search.value.trim()).length > 128) { inputError.value = "搜索条件最多 128 个字符"; return; }
  inputError.value = "";
  appliedSearch.value = search.value.trim();
  offset.value = 0;
  writeRoute("push");
  load();
}
function clearSearch() { search.value = ""; submitSearch(); }
function changePage(delta: number) {
  if (manifestConflict.value || loading.value) return;
  offset.value = Math.max(0, offset.value + delta);
  writeRoute("push");
  load();
}
async function action(t: Task, kind: string) {
  if (busy.value || error.value || health.value?.queue_healthy === false) return;
  if (kind === "cancel" || kind === "retry" || kind === "clear") {
    confirmation.value = { task: t, kind };
    return;
  }
  await executeAction(t, kind);
}
function closeConfirmation() { if (!busy.value) confirmation.value = null; }
async function confirmAction() {
  const selected = confirmation.value;
  if (!selected || busy.value || error.value || health.value?.queue_healthy === false) return;
  await executeAction(selected.task, selected.kind);
  if (confirmation.value === selected) confirmation.value = null;
}
async function executeAction(t: Task, kind: string) {
  if (busy.value) return;
  busy.value = t.id;
  actionError.value = "";
  actionMessage.value = "";
  try {
    await api(`/api/tasks/${encodeURIComponent(t.id)}/${kind}`, {
      method: "POST",
      ...(kind === "clear" ? {headers:{"Content-Type":"application/json"},body:JSON.stringify({acknowledge_reset:true,clear_token:t.clear_token})}:{}),
    });
    if(kind === "clear"&&!workspaceActive)return;
    const label = { pause: "暂停任务", resume: "恢复任务", cancel: "取消任务", retry: "重试任务", clear:"清理记录" }[kind] || "任务操作";
    actionMessage.value = `${label}「${t.name}」请求已接受。`;
    if (dialog.value === t) dialog.value = null;
    if(kind === "clear") { eventGeneration++;events.value=[];eventLoading.value=false;generation++;controller?.abort();refreshInFlight=false;manifestConflict.value=false;displayedRoute=undefined;data.value=null; if(rootID.value===t.id){rootID.value="";manifestID.value="";offset.value=0;status.value="";search.value="";appliedSearch.value="";writeRoute("replace");} }
    await load(true);
  } catch (e: any) {
    actionError.value = e.message;
  } finally {
    busy.value = "";
  }
}
async function details(t: Task) {
  copyMessage.value = "";
  dialog.value = t;
  events.value = [];
  eventError.value = "";
  eventGeneration++;
  eventLoading.value = false;
  eventPage.value = 0;
  eventNext.value = null;
  eventCursors.value = [null];
  if (t.file_evidence) return;
  await loadEventPage(0, null);
}
async function loadEventPage(index: number, cursor: string | null) {
  const target = dialog.value;
  if (!target || target.file_evidence || eventLoading.value) return;
  const request = ++eventGeneration;
  eventLoading.value = true;
  try {
    const query = new URLSearchParams({ limit: "20", sequence_format: "string" });
    if (cursor) query.set("before_sequence", cursor);
    const r = await api(
      `/api/tasks/${encodeURIComponent(target.id)}/events?${query}`,
    );
    if (request !== eventGeneration || dialog.value !== target) return;
    const next = r.next_before_sequence ?? null;
    if (next !== null && (typeof next !== "string" || !/^[1-9][0-9]{0,18}$/.test(next) || (next.length === 19 && next > "9223372036854775807"))) throw Error("事件游标无效，请重新打开任务信息。");
    events.value = Array.isArray(r.events) ? r.events : [];
    eventNext.value = next;
    eventPage.value = index;
    eventCursors.value[index] = cursor;
    eventError.value = "";
  } catch (e: any) {
    if (request === eventGeneration && dialog.value === target) eventError.value = e.message;
  } finally {
    if (request === eventGeneration && dialog.value === target) eventLoading.value = false;
  }
}
async function copyPath(value: string, label: string) {
  if (!value) return;
  const target = dialog.value;
  try {
    await navigator.clipboard.writeText(value);
    if (dialog.value === target) copyMessage.value = label + "已复制";
  } catch {
    if (dialog.value === target) copyMessage.value = "复制失败，请手动选择并复制路径。";
  }
}
function eventUTC(value: unknown): string {
  if (!value) return "未记录";
  if (typeof value !== "string" || !Number.isFinite(Date.parse(value))) return "时间格式无效";
  return new Date(value).toISOString();
}
async function toggleQueue() {
  if (busy.value || error.value || !health.value || health.value.queue_healthy === false) return;
  busy.value = "queue";
  actionError.value = "";
  actionMessage.value = "";
  const paused = health.value.queue_pause?.paused;
  try {
    await api(
      "/api/queue/" + (paused ? "resume" : "pause"),
      { method: "POST" },
    );
    actionMessage.value = `${paused ? "恢复" : "暂停"}队列请求已接受。`;
    await load(true);
  } catch (e: any) {
    actionError.value = e.message;
  } finally {
    busy.value = "";
  }
}

onMounted(() => {
  load();
  timer = setInterval(() => {
    if (!loading.value && !document.hidden && !busy.value) load(true);
  }, 2500);
  window.addEventListener("popstate", pop);
});
onUnmounted(() => {
  workspaceActive=false;
  clearInterval(timer);
  controller?.abort();
  window.removeEventListener("popstate", pop);
});
</script>

<template>
  <div class="workspace">
    <WorkspaceSidebar active="/" @error="error=$event"><template #status><span class="connection-dot" :class="{offline:!!error&&!manifestConflict}"></span>{{error&&!manifestConflict?'读取异常':health?.authentication?.ready?'百度账号可用':'等待百度授权'}}</template></WorkspaceSidebar>
    <main class="main">
      <div class="content" :class="{ 'task-detail': !!rootID }">
        <div class="page-heading">
          <div>
            <button v-if="rootID" class="back-link" @click="back">
              <ArrowLeft :size="15" />返回任务总览
            </button>
            <div class="eyebrow">
              {{ rootID ? "TASK DETAILS" : "DOWNLOADS" }}
            </div>
            <h1>{{ title }}</h1>
            <p>
              {{
                rootID
                  ? "查看文件传输进度，管理本次下载任务。"
                  : "所有下载任务，清晰掌握每一次传输。"
              }}
            </p>
          </div>
          <div class="heading-actions">
            <button class="button secondary" @click="settingsOpen = true">
              <Settings :size="16" />下载设置</button
            ><button
              class="button secondary"
              :disabled="!!busy || !!error || health?.queue_healthy === false"
              @click="reloadCurrent()"
            >
              <RefreshCw :size="16" />刷新</button
            ><a class="button primary" href="/intake" aria-label="添加网盘项目"
              ><Download :size="16" />添加任务</a
            >
          </div>
        </div>
        <div v-if="error" class="notice danger" role="alert">
          <AlertCircle :size="18" /><span>{{ error }}</span
          ><button @click="manifestConflict ? reloadCurrent() : load()">{{ manifestConflict ? "重新载入清单" : "重新连接" }}</button>
        </div>
        <div v-if="inputError" class="notice danger" role="alert">
          <AlertCircle :size="18" /><span>{{ inputError }}</span>
        </div>
        <div v-if="actionError" class="notice danger" role="alert">
          <AlertCircle :size="18" /><span>操作未确认成功：{{ actionError }}</span>
          <button @click="actionError = ''">关闭提示</button>
        </div>
        <div v-if="actionMessage" class="notice" role="status" aria-live="polite" data-testid="action-feedback">
          <CheckCircle2 :size="18" /><span>{{ actionMessage }}{{ error ? '当前状态读取未成功，请刷新核对，不要重复提交。' : '请以当前任务状态为准。' }}</span>
          <button @click="actionMessage = ''">关闭提示</button>
        </div>
        <template v-if="root"
          ><section class="detail-summary">
            <div class="summary-icon"><Folder :size="27" /></div>
            <div class="summary-main">
              <span class="badge" :class="root.status">{{
                root.status === "downloading" && !root.prepared
                  ? "准备目录"
                  : labels[root.status] || root.status
              }}</span>
              <h2>{{ root.name }}</h2>
              <p class="truncate" :title="root.local_path">
                保存至 {{ root.local_path || (root.status === "downloading" && !root.prepared ? "正在准备本地目录" : "未记录本地路径") }}
              </p>
              <div class="summary-meta">
                <span :title="statisticsHint">下载用时 {{ downloadDuration(root) }}</span>
                <span :title="statisticsHint">平均 {{ averageSpeed(root) }}{{ root.transfer_statistics?.partial ? "（部分记录）" : "" }}</span>
                <span>{{ completionText(root) }}</span
                ><span v-if="root.files || root.prepared"
                  >{{ root.folders }} 个文件夹</span
                ><span>{{
                  root.mode === "delete"
                    ? "完成后删除网盘项"
                    : "完成后保留并标记"
                }}</span>
              </div>
            </div>
            <div class="summary-actions">
              <button
                v-if="root.can_pause"
                class="button secondary"
                :disabled="!!busy || !!error || health?.queue_healthy === false"
                @click="action(root, 'pause')"
              >
                <Pause :size="16" />暂停任务</button
              ><button
                v-if="root.can_resume"
                class="button primary"
                :disabled="!!busy || !!error || health?.queue_healthy === false"
                @click="action(root, 'resume')"
              >
                <Play :size="16" />继续下载</button
              ><button
                class="icon-button"
                aria-label="查看任务信息"
                @click="details(root)"
              >
                <MoreHorizontal />
              </button>
            </div>
          </section>
          <DownloadStateGuidance :task="root" :stale="!!error" />
          <p v-if="root.download_history" class="discovery-note history-provenance">{{ historyHint(root) }}</p>
          <p
            v-if="
              (root.status === 'done' || root.status === 'source_unknown') &&
              !root.prepared &&
              !root.files
            "
            class="discovery-note"
          >
            {{
              root.status === "source_unknown"
                ? completionText(root) + "；历史子文件明细未记录。"
                : "历史文件明细未记录，任务完成记录仍保留。"
            }}
          </p>
          <div v-else class="metrics">
            <div>
              <span>文件传输</span
              ><strong>{{ root.active }}<small> 个进行中</small></strong>
            </div>
            <div>
              <span>已完成文件</span
              ><strong
                >{{ root.completed
                }}<small> / {{ root.files }} 个{{ root.download_history?.source === "current_attempt" ? "已记录" : "已发现" }}</small></strong
              >
            </div>
            <div>
              <span>已传输数据</span
              ><strong
                >{{ bytes(root.progress)
                }}<small> / {{ bytes(root.size) }} {{ root.download_history?.source === "current_attempt" ? "已记录" : "已发现" }}</small></strong
              >
            </div>
            <div>
              <span>当前速度</span
              ><strong>{{
                root.speed ? bytes(root.speed) + "/s" : "—"
              }}</strong>
            </div>
          </div>
          <p
            v-if="root.status !== 'done' && root.status !== 'source_unknown'"
            class="discovery-note"
          >
            {{ root.download_history?.source === "current_attempt" ? "本轮清单已完整保存。文件夹已预先创建，不单独占用下载行。" : "目录内容在遍历时逐步发现，当前文件数与容量可能继续增加。文件夹会在文件传输前创建，不单独占用下载行。" }}
          </p>
        </template>
        <div v-else class="overview-strip">
          <div>
            <span class="soft-icon"><Files :size="20" /></span>
            <div>
              <span>下载任务</span
              ><strong>{{ data?.root_count ?? "—" }}</strong>
            </div>
          </div>
          <p>
            <span class="connection-dot"></span
            >{{ health?.queue_pause?.paused ? "队列已暂停" : "下载队列运行中"
            }}<span class="muted"> · {{ monitorLabel }}</span>
          </p>
          <button
            class="button secondary"
            :disabled="
              !!busy || !!error || !health || health.queue_healthy === false
            "
            @click="toggleQueue"
          >
            <component
              :is="health?.queue_pause?.paused ? Play : Pause"
              :size="15"
            />{{ health?.queue_pause?.paused ? "继续队列" : "暂停队列" }}
          </button>
        </div>
        <section class="table-panel">
          <div class="table-toolbar">
            <div class="tabs" role="tablist" aria-label="任务状态">
              <button
                v-for="[value, label] in rootID
                  ? tabs
                  : [
                      ['', '全部任务'],
                      ['downloading', '下载中'],
                      ['pending', '等待下载'],
                      ['done', '已完成'],
                      ['failed', '下载失败'],
                      ['paused', '已暂停'],
                    ]"
                :key="value"
                role="tab"
                :aria-selected="status === value"
                :class="{ selected: status === value }"
                @click="filter(value)"
              >
                {{ label
                }}<span v-if="rootID && value && data?.counts[value]">{{
                  data.counts[value]
                }}</span>
              </button>
            </div>
            <select
              class="status-select"
              aria-label="更多任务状态"
              :value="
                [
                  'retry_wait',
                  'auth_wait',
                  'source_unknown',
                  'cancelled',
                ].includes(status)
                  ? status
                  : ''
              "
              @change="filter(($event.target as HTMLSelectElement).value)"
            >
              <option value="">更多状态</option>
              <option value="retry_wait">等待重试</option>
              <option value="auth_wait">等待授权</option>
              <option value="source_unknown">网盘后处理待核对</option>
              <option value="cancelled">已取消</option>
            </select>
            <form
              class="search"
              @submit.prevent="submitSearch()"
            >
              <Search :size="16" /><input
                v-model="search"
                :placeholder="rootID ? '搜索文件名或路径，回车应用' : '搜索下载任务，回车应用'"
                aria-label="搜索文件名或路径"
              /><button
                v-if="search"
                type="button"
                class="icon-button"
                aria-label="清空搜索"
                @click="clearSearch()"
              >
                <X :size="14" />
              </button>
            </form>
          </div>
          <div class="table-scroll" role="region" :aria-label="rootID ? '任务文件列表' : '下载任务列表'" tabindex="0">
            <table class="download-task-table" :class="rootID ? 'download-files-table' : 'download-overview-table'">
              <!-- Real browser review found the two new statistics columns squeezed
                   to zero by the old five-column widths. Allocate every column. -->
              <colgroup v-if="!rootID"><col style="width:27%"/><col style="width:13%"/><col style="width:23%"/><col style="width:8%"/><col style="width:12%"/><col style="width:10%"/><col style="width:7%"/></colgroup>
              <colgroup v-else><col style="width:40%"/><col style="width:13%"/><col style="width:25%"/><col style="width:13%"/><col style="width:9%"/></colgroup>
              <thead>
                <tr>
                  <th class="name-column">
                    {{ rootID ? "文件名称" : "任务名称" }}
                  </th>
                  <th>状态</th>
                  <th>{{ rootID ? "传输进度" : "完成情况" }}</th>
                  <th>速度</th><th v-if="!rootID" :title="statisticsHint">下载用时</th><th v-if="!rootID" :title="statisticsHint">平均速度</th>
                  <th class="actions-column">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="t in data?.items || []" :key="t.id">
                  <td>
                    <button
                      class="file-name"
                      :title="t.path"
                      @click="rootID ? details(t) : open(t)"
                    >
                      <span class="file-icon" :class="{ folder: !rootID }"
                        ><component
                          :is="rootID ? Files : Folder"
                          :size="21" /></span
                      ><span class="name-text"
                        ><strong :title="t.name">{{ t.name }}<span v-if="t.file_evidence?.historical" class="badge">历史</span></strong
                        ><small v-if="taskPathLabel(t)" :title="t.path">{{ taskPathLabel(t) }}</small></span
                      ><ChevronRight
                        v-if="!rootID"
                        class="row-arrow"
                        :size="16"
                      />
                    </button>
                  </td>
                  <td>
                    <span class="badge" :class="t.status">{{
                      t.status === "downloading" && !t.prepared
                        ? "准备目录"
                        : labels[t.status] || t.status
                    }}</span>
                    <small v-if="t.file_evidence?.interrupted" class="muted"> · 上次中断</small>
                  </td>
                  <td>
                    <template v-if="rootID"
                      ><div class="file-progress">
                        <span>{{
                          t.status === "done"
                            ? bytes(t.size)
                            : bytes(t.progress) + " / " + bytes(t.size)
                        }}</span
                        ><small>{{ percent(t).toFixed(1) }}%</small>
                      </div>
                      <div
                        class="progress-track"
                        role="progressbar"
                        :aria-label="t.name + ' 下载进度'"
                        :aria-valuenow="percent(t)"
                        :aria-valuemin="0"
                        :aria-valuemax="100"
                      >
                        <span
                          :style="{ width: percent(t) + '%' }"
                          :class="{ complete: t.status === 'done' }"
                        ></span></div></template
                    ><template v-else
                      ><span>{{ completionText(t) }}</span
                      ><small
                        v-if="
                          (t.files || t.folders) &&
                          (t.status !== 'source_unknown' || t.files > 0)
                        "
                        class="cell-sub"
                        >{{ t.folders }} 个文件夹<span v-if="t.progress">
                          · {{ bytes(t.progress) }} 已传输</span
                        ></small
                      ></template
                    >
                  </td>
                  <td class="speed">
                    {{ t.speed > 0 ? bytes(t.speed) + "/s" : "—" }}
                  </td>
                  <td v-if="!rootID" :title="statisticsHint">{{ downloadDuration(t) }}<small v-if="t.transfer_statistics?.partial" class="cell-sub">部分记录</small></td>
                  <td v-if="!rootID" class="speed" :title="statisticsHint">{{ averageSpeed(t) }}</td>
                  <td>
                    <div class="row-actions">
                      <button
                        v-if="t.can_pause"
                        class="icon-button"
                        :disabled="
                          !!busy || !!error || health?.queue_healthy === false
                        "
                        aria-label="暂停任务"
                        @click="action(t, 'pause')"
                      >
                        <Pause :size="16" /></button
                      ><button
                        v-if="t.can_resume"
                        class="icon-button"
                        :disabled="
                          !!busy || !!error || health?.queue_healthy === false
                        "
                        aria-label="继续任务"
                        @click="action(t, 'resume')"
                      >
                        <Play :size="16" /></button
                      ><button
                        class="icon-button"
                        aria-label="查看详情"
                        @click="rootID ? details(t) : open(t)"
                      >
                        <component
                          :is="rootID ? MoreHorizontal : ChevronRight"
                          :size="18"
                        />
                      </button>
                    </div>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-if="loading && !data" class="empty-state">
            <RefreshCw class="spin" :size="28" />
            <h3>正在读取下载任务</h3>
            <p>请稍候，正在连接本地服务。</p>
          </div>
          <div v-else-if="!data?.items.length" class="empty-state">
            <CheckCircle2 :size="36" />
            <h3>
              {{ emptyState.title }}
            </h3>
            <p>
              {{ emptyState.text }}
            </p>
            <a
              v-if="!rootID && !status && !search"
              class="button primary"
              href="/intake"
              aria-label="添加网盘项目"
              >添加第一个任务</a
            >
          </div>
          <footer class="table-footer">
            <span
              >{{ data?.total ?? 0 }} {{ rootID ? "个文件" : "个任务"
              }}<span v-if="data?.total">
                · 第 {{ Math.floor(offset / 50) + 1 }} /
                {{ Math.max(1, Math.ceil(data.total / 50)) }} 页</span
              ></span
            >
            <div>
              <button
                class="button secondary compact"
                :disabled="offset === 0 || loading || manifestConflict"
                @click="changePage(-50)"
              >
                上一页</button
              ><button
                class="button secondary compact"
                :disabled="!data || offset + 50 >= data.total || loading || manifestConflict"
                @click="changePage(50)"
              >
                下一页
              </button>
            </div>
          </footer>
        </section>
        <p class="page-footnote">
          {{ health?.version || "NAS Downloader"
          }}<span>自动刷新 · 本地安全连接</span>
        </p>
      </div>
    </main>
    <DownloadSettings v-if="settingsOpen" @close="settingsOpen = false" />
    <div v-if="dialog" class="modal-backdrop" :inert="confirmation ? true : undefined" :aria-hidden="confirmation ? 'true' : undefined" @click.self="dialog = null">
      <section
        class="detail-drawer"
        v-modal="() => (dialog = null)"
        role="dialog"
        aria-modal="true"
        aria-labelledby="drawer-title"
      >
        <header>
          <h2 id="drawer-title">任务信息</h2>
          <button
            class="icon-button"
            aria-label="关闭详情"
            @click="dialog = null"
          >
            <X />
          </button>
        </header>
        <h3>{{ dialog.name }}</h3>
        <span class="badge" :class="dialog.status">{{
          labels[dialog.status]
        }}</span>
        <dl>
          <dt>网盘路径</dt>
          <dd class="path-value"><span>{{ dialog.path }}</span><button class="icon-button path-copy" aria-label="复制网盘路径" title="复制网盘路径" :disabled="!dialog.path" @click="copyPath(dialog.path, '网盘路径')"><Copy :size="15" /></button></dd>
          <dt>本地路径</dt>
          <dd class="path-value"><span>{{ dialog.local_path || "—" }}</span><button class="icon-button path-copy" aria-label="复制本地目标" title="复制记录中的本地目标，不检查当前文件是否存在" :disabled="!dialog.local_path" @click="copyPath(dialog.local_path, '本地目标')"><Copy :size="15" /></button></dd>
          <template v-if="dialog.destination">
            <dt>目标记录时间（UTC）</dt>
            <dd>{{ dialog.destination.recorded_at_utc }}</dd>
            <dt>路径依据</dt>
            <dd>本次尝试的历史保存目标；不代表文件当前仍在此处。</dd>
          </template>
          <template v-if="dialog.download_history">
            <dt>清单依据</dt>
            <dd>{{ historyHint(dialog) }}</dd>
          </template>
          <template v-if="dialog.file_evidence">
            <dt>清单记录时间（UTC）</dt><dd>{{ dialog.file_evidence.observed_at_utc }}</dd>
            <dt>条目来源</dt><dd>{{ dialog.file_evidence.observation_source === 'directory_listing' ? '本轮目录列表' : '根任务快照，未经本轮重新在线核对' }}</dd>
            <dt v-if="dialog.file_evidence.state_at_utc">执行状态记录（UTC）</dt><dd v-if="dialog.file_evidence.state_at_utc">{{ dialog.file_evidence.state_at_utc }}</dd>
            <dt>本地完成依据</dt>
            <dd>{{ dialog.file_evidence.result ? (dialog.file_evidence.result.kind === 'local_verified' ? '本地文件完整校验通过' : '最终文件已发布并同步') : (dialog.file_evidence.interrupted ? '中断后尚未完成，等待重新处理' : '尚无本地完成证据') }}</dd>
            <template v-if="dialog.file_evidence.result">
              <dt>完成依据记录时间（UTC）</dt><dd>{{ dialog.file_evidence.result.recorded_at_utc }}</dd>
              <dt>本地 SHA256</dt><dd>{{ dialog.file_evidence.result.sha256 }}</dd>
            </template>
          </template>
          <dt>当前状态</dt>
          <dd>{{ dialog.error || labels[dialog.status] }}</dd>
          <dt>容量</dt>
          <dd>
            {{
              dialog.files
                ? `${dialog.files} 个已发现文件，${dialog.folders} 个文件夹`
                : bytes(dialog.size)
            }}
          </dd>
        </dl>
        <p class="copy-feedback" role="status" aria-live="polite">{{ copyMessage }}</p>
        <p v-if="actionError" class="notice danger" role="alert">操作未确认成功：{{ actionError }}</p>
        <div class="drawer-actions">
          <button
            v-if="dialog.can_retry"
            class="button primary"
            :disabled="!!busy || !!error || health?.queue_healthy === false"
            @click="action(dialog, 'retry')"
          >
            重试任务</button
          ><button
            v-if="dialog.can_cancel"
            class="button danger-button"
            :disabled="!!busy || !!error || health?.queue_healthy === false"
            @click="action(dialog, 'cancel')"
          >
            取消任务
          </button>
        </div>
        <button v-if="dialog.can_clear&amp;&amp;dialog.clear_token" class="button danger-button" data-testid="clear-download-record" :disabled="!!busy||!!error||health?.queue_healthy===false" @click="action(dialog,'clear')">清理记录</button>
        <template v-if="!dialog.file_evidence">
        <h4>最近任务事件</h4>
        <p v-if="eventError" class="notice danger">{{ eventError }}</p>
        <p v-else-if="eventLoading" class="muted">正在读取事件…</p>
        <p v-else-if="!events.length" class="muted">暂无事件记录</p>
        <ol class="timeline">
          <li v-for="(e, i) in events" :key="`${dialog.id}:${typeof e.sequence === 'string' ? e.sequence : `${eventPage}:${i}`}`">
            <strong>{{ e.event_type || e.kind || e.event }}</strong
            ><time>本机记录 UTC：{{ eventUTC(e.recorded_at_utc) }}</time>
            <time v-if="e.occurred_at_utc">事件发生 UTC：{{ eventUTC(e.occurred_at_utc) }}</time>
            <time v-if="e.cloud_observed_at_utc">云端观察 UTC：{{ eventUTC(e.cloud_observed_at_utc) }}</time>
            <time v-if="e.response_observed_at_utc">响应观察 UTC：{{ eventUTC(e.response_observed_at_utc) }}</time>
            <details class="event-evidence">
              <summary>关联与证据</summary>
              <dl>
                <dt>事件序列</dt><dd>{{ typeof e.sequence === 'string' ? e.sequence : '未提供精确序列' }}</dd>
                <template v-if="e.attempt_id"><dt>下载尝试 ID</dt><dd>{{ e.attempt_id }}</dd></template>
                <template v-if="e.operation_id"><dt>远端操作 ID</dt><dd>{{ e.operation_id }}</dd></template>
                <template v-if="e.run_id"><dt>运行 ID</dt><dd>{{ e.run_id }}</dd></template>
              </dl>
              <pre v-if="e.evidence">{{ JSON.stringify(e.evidence, null, 2) }}</pre>
              <p v-else class="muted">未记录额外证据</p>
            </details>
          </li>
        </ol>
        <div class="drawer-actions">
          <button class="button secondary" data-testid="newer-events" :disabled="eventLoading || eventPage === 0" @click="loadEventPage(eventPage - 1, eventCursors[eventPage - 1] ?? null)">较新记录</button>
          <span>第 {{ eventPage + 1 }} 页</span>
          <button class="button secondary" data-testid="older-events" :disabled="eventLoading || !eventNext" @click="loadEventPage(eventPage + 1, eventNext)">更早记录</button>
        </div>
        <a href="/task-events" aria-label="任务事件" class="text-link"
          >前往完整任务事件记录 <ChevronRight :size="14"
        /></a>
        </template>
        <p v-else class="muted">文件证据仅表示记录时的状态，不代表当前文件已重新核对。</p>
      </section>
    </div>
    <div v-if="confirmation" class="modal-backdrop action-confirm-backdrop" @click.self="closeConfirmation">
      <section class="action-confirm" role="dialog" aria-modal="true" aria-labelledby="action-confirm-title" v-modal="closeConfirmation">
        <h2 id="action-confirm-title">{{ confirmation.kind === 'clear' ? '清理下载记录' : confirmation.kind === 'cancel' ? '取消任务' : '重试任务' }}</h2>
        <h3>{{ confirmation.task.name }}</h3>
        <p class="confirmation-path">{{ confirmation.task.path }}</p>
        <p>{{ confirmation.kind === 'clear' ? '永久清理此任务的下载事件、文件清单、统计和历史隔离信息，无法恢复。本地文件和网盘内容保留；若项目仍在监控目录，会按当前策略作为新任务重新处理，包括可能的网盘改名或删除。' : confirmation.kind === 'cancel' ? '停止此任务的后续调度。已发送的网盘操作不能撤回，本地已下载文件会保留。' : '按此任务既定策略重新尝试，由程序重新核对当前状态；不会解除结果未知的隔离。' }}</p>
        <p v-if="error" class="notice danger" role="alert">当前状态读取异常，请返回并重新核对。</p>
        <div class="drawer-actions">
          <button class="button secondary" :disabled="!!busy" @click="closeConfirmation">返回</button>
          <button class="button primary" :disabled="!!busy || !!error || health?.queue_healthy === false" @click="confirmAction">{{ busy ? '正在提交…' : confirmation.kind === 'clear' ? '确认清理记录并重置隔离' : confirmation.kind === 'cancel' ? '确认取消' : '确认重试' }}</button>
        </div>
      </section>
    </div>
  </div>
</template>
