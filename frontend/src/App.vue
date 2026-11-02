<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { vModal } from "./modal";
import DownloadSettings from "./DownloadSettings.vue";
import {
  Download,
  Folder,
  Files,
  Cloud,
  Settings,
  History,
  Search,
  ChevronRight,
  ArrowLeft,
  RefreshCw,
  Pause,
  Play,
  MoreHorizontal,
  CheckCircle2,
  AlertCircle,
  LogOut,
  X,
  Activity,
} from "lucide-vue-next";
type Task = {
	transfer_statistics?: { active_ns: number; network_bytes: number; partial?: boolean; recorded_at_utc?: string } | null;
  id: string;
  name: string;
  path: string;
  status: string;
  local_path: string;
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
  prepared: boolean;
};
type Workspace = {
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
const rootID = ref(new URLSearchParams(location.search).get("task") || "");
const data = ref<Workspace | null>(null),
  health = ref<any>(null),
  status = ref(""),
  search = ref(""),
  offset = ref(0),
  loading = ref(true),
  error = ref(""),
  busy = ref(""),
  dialog = ref<Task | null>(null),
  events = ref<any[]>([]),
  eventError = ref("");
let refreshInFlight = false;
let timer: ReturnType<typeof setInterval> | undefined,
  controller: AbortController | undefined,
  generation = 0;
function completionText(t: Task): string {
  if (t.status === "source_unknown")
    return t.mode === "delete"
      ? "本地阶段已结束 · 网盘删除结果待核对"
      : "本地阶段已结束 · 网盘改名结果待核对";
  if (t.files) return `${t.completed} / ${t.files} 个已发现文件`;
  if (t.prepared) return "空文件夹";
  return t.status === "done" ? "历史文件明细未记录" : "正在等待目录内容";
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
const bytes = (n: number) => {
  if (!Number.isFinite(n) || n <= 0) return "0 B";
  const u = ["B", "KB", "MB", "GB", "TB"],
    i = Math.min(4, Math.floor(Math.log(n) / Math.log(1024)));
  return `${(n / 1024 ** i).toFixed(i ? 2 : 0)} ${u[i]}`;
};
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
  if (!r.ok) throw Error((await r.text()) || "请求失败");
  return r.json();
}
async function load(quiet = false) {
  if (quiet && refreshInFlight) return;
  refreshInFlight = true;
  const current = ++generation;
  controller?.abort();
  controller = new AbortController();
  if (!quiet) loading.value = true;
  try {
    const q = new URLSearchParams({
      root: rootID.value,
      status: status.value,
      q: search.value,
      offset: String(offset.value),
      limit: "50",
    });
    const [workspace, h] = await Promise.all([
      api("/api/task-workspace?" + q, { signal: controller.signal }),
      api("/api/health", { signal: controller.signal }),
    ]);
    if (current !== generation) return;
    data.value = workspace;
    health.value = h;
    error.value = "";
  } catch (e: any) {
    if (e.name !== "AbortError" && current === generation)
      error.value = e.message;
  } finally {
    if (current === generation) {
      loading.value = false;
      refreshInFlight = false;
    }
  }
}
function open(t: Task) {
  data.value = null;
  rootID.value = t.id;
  status.value = "";
  offset.value = 0;
  search.value = "";
  history.pushState({}, "", `/?task=${encodeURIComponent(t.id)}`);
  load();
}
function back() {
  data.value = null;
  rootID.value = "";
  status.value = "";
  search.value = "";
  offset.value = 0;
  history.pushState({}, "", "/");
  load();
}
function pop() {
  data.value = null;
  rootID.value = new URLSearchParams(location.search).get("task") || "";
  offset.value = 0;
  load();
}
function filter(value: string) {
  status.value = value;
  offset.value = 0;
  load();
}
async function action(t: Task, kind: string) {
  if (
    (kind === "cancel" || kind === "retry") &&
    !confirm(`${kind === "cancel" ? "取消" : "重试"}任务「${t.name}」？`)
  )
    return;
  busy.value = t.id;
  try {
    await api(`/api/tasks/${encodeURIComponent(t.id)}/${kind}`, {
      method: "POST",
    });
    await load(true);
  } catch (e: any) {
    error.value = e.message;
  } finally {
    busy.value = "";
  }
}
async function details(t: Task) {
  dialog.value = t;
  events.value = [];
  eventError.value = "";
  try {
    const r = await api(
      `/api/tasks/${encodeURIComponent(t.id)}/events?limit=20`,
    );
    if (dialog.value?.id === t.id) events.value = r.events || [];
  } catch (e: any) {
    eventError.value = e.message;
  }
}
async function toggleQueue() {
  busy.value = "queue";
  try {
    await api(
      "/api/queue/" + (health.value?.queue_pause?.paused ? "resume" : "pause"),
      { method: "POST" },
    );
    await load(true);
  } catch (e: any) {
    error.value = e.message;
  } finally {
    busy.value = "";
  }
}
async function logout() {
  await fetch("/api/logout", { method: "POST" });
  location.assign("/login");
}
onMounted(() => {
  load();
  timer = setInterval(() => {
    if (!loading.value && !document.hidden && !busy.value) load(true);
  }, 2500);
  window.addEventListener("popstate", pop);
});
onUnmounted(() => {
  clearInterval(timer);
  controller?.abort();
  window.removeEventListener("popstate", pop);
});
</script>

<template>
  <div class="workspace">
    <aside class="sidebar">
      <a class="brand" href="/" aria-label="下载任务"
        ><span class="brand-icon"><Download :size="23" /></span
        ><span>NAS Downloader<small>百度网盘自动下载</small></span></a
      >
      <div class="nav-label">工作空间</div>
      <nav>
        <a class="active" href="/" aria-label="下载任务"
          ><Download :size="19" />下载任务</a
        ><a href="/cloud" aria-label="网盘文件"><Cloud :size="19" />网盘文件</a
        ><a href="/intake" aria-label="添加网盘项目"
          ><Files :size="19" />添加网盘项目</a
        >
      </nav>
      <div class="nav-label">管理</div>
      <nav>
        <a href="/operations" aria-label="操作记录"
          ><History :size="19" />操作记录</a
        ><a href="/monitor-history" aria-label="监控历史"
          ><Activity :size="19" />监控历史</a
        ><a href="/task-events" aria-label="任务事件"
          ><History :size="19" />任务事件</a
        ><a href="/oauth" aria-label="账号与授权"
          ><Settings :size="19" />账号与授权</a
        >
      </nav>
      <div class="sidebar-bottom">
        <span class="connection-dot" :class="{ offline: !!error }"></span
        >{{ error ? "连接异常" : "本地服务已连接"
        }}<button class="icon-button" aria-label="退出登录" @click="logout">
          <LogOut :size="17" />
        </button>
      </div>
    </aside>
    <main class="main">
      <header class="topbar">
        <span
          >工作空间 <ChevronRight :size="14" /> 下载任务
          <template v-if="rootID"
            ><ChevronRight :size="14" /> 详情</template
          ></span
        ><span class="account"
          ><span
            class="connection-dot"
            :class="{ offline: health && !health.authentication?.ready }"
          ></span
          >{{
            health?.authentication?.ready ? "百度账号已连接" : "等待百度授权"
          }}</span
        >
      </header>
      <div class="content">
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
              @click="load()"
            >
              <RefreshCw :size="16" />刷新</button
            ><a class="button primary" href="/intake" aria-label="添加网盘项目"
              ><Download :size="16" />添加任务</a
            >
          </div>
        </div>
        <div v-if="error" class="notice danger" role="alert">
          <AlertCircle :size="18" /><span>{{ error }}</span
          ><button @click="load()">重新连接</button>
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
                保存至 {{ root.local_path || "正在准备本地目录" }}
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
                }}<small> / {{ root.files }} 个已发现</small></strong
              >
            </div>
            <div>
              <span>已传输数据</span
              ><strong
                >{{ bytes(root.progress)
                }}<small> / {{ bytes(root.size) }} 已发现</small></strong
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
            目录内容在遍历时逐步发现，当前文件数与容量可能继续增加。文件夹会在文件传输前创建，不单独占用下载行。
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
              @submit.prevent="
                offset = 0;
                load();
              "
            >
              <Search :size="16" /><input
                v-model="search"
                :placeholder="rootID ? '搜索文件名或路径' : '搜索下载任务'"
                aria-label="搜索文件名或路径"
              /><button
                v-if="search"
                type="button"
                class="icon-button"
                aria-label="清空搜索"
                @click="
                  search = '';
                  offset = 0;
                  load();
                "
              >
                <X :size="14" />
              </button>
            </form>
          </div>
          <div class="table-scroll">
            <table>
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
                      @click="rootID ? details(t) : open(t)"
                    >
                      <span class="file-icon" :class="{ folder: !rootID }"
                        ><component
                          :is="rootID ? Files : Folder"
                          :size="21" /></span
                      ><span class="name-text"
                        ><strong :title="t.name">{{ t.name }}</strong
                        ><small :title="t.path">{{
                          rootID && root
                            ? t.path.slice(root.path.length + 1)
                            : t.path
                        }}</small></span
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
              {{ search || status ? "没有匹配的任务" : "这里还没有下载任务" }}
            </h3>
            <p>
              {{
                rootID
                  ? "已发现的文件会显示在这里，文件夹仅统计数量。"
                  : "添加网盘项目到监控目录后，下载会自动开始。"
              }}
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
                :disabled="offset === 0 || loading"
                @click="
                  offset = Math.max(0, offset - 50);
                  load();
                "
              >
                上一页</button
              ><button
                class="button secondary compact"
                :disabled="!data || offset + 50 >= data.total || loading"
                @click="
                  offset += 50;
                  load();
                "
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
    <div v-if="dialog" class="modal-backdrop" @click.self="dialog = null">
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
          <dd>{{ dialog.path }}</dd>
          <dt>本地路径</dt>
          <dd>{{ dialog.local_path || "—" }}</dd>
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
        <h4>最近任务事件</h4>
        <p v-if="eventError" class="notice danger">{{ eventError }}</p>
        <p v-else-if="!events.length" class="muted">暂无事件记录</p>
        <ol class="timeline">
          <li v-for="(e, i) in events" :key="i">
            <strong>{{ e.event_type || e.kind || e.event }}</strong
            ><time>{{ e.recorded_at_utc }}</time>
          </li>
        </ol>
        <a href="/task-events" aria-label="任务事件" class="text-link"
          >前往完整任务事件记录 <ChevronRight :size="14"
        /></a>
      </section>
    </div>
  </div>
</template>
