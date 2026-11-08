<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from "vue";
import {
  reasons,
  parsePage,
  comparePaths,
  type Conditions,
  type Page,
  type Row,
} from "./incomplete-cache";
const props = defineProps<{ disabled: boolean }>(),
  emit = defineEmits<{ authenticationRequired: [] }>();
const draftReason = ref(""),
  draftQuery = ref(""),
  page = ref<Page>(),
  conditions = ref<Conditions>({ reason: "", query: "" }),
  cursors = ref([""]),
  index = ref(0),
  busy = ref(false),
  exporting = ref(false),
  fresh = ref(false),
  error = ref(""),
  progress = ref("");
let alive = true,
  epoch = 0,
  controller: AbortController | undefined;
function cancel() {
  epoch++;
  controller?.abort();
  busy.value = false;
  exporting.value = false;
  fresh.value = false;
}
function hide() {
  alive = false;
  cancel();
}
function show(e: PageTransitionEvent) {
  if (e.persisted) {
    alive = true;
    error.value = "页面恢复：旧结果仅供历史查看，请重新读取。";
  }
}
function current(id: number) {
  return alive && !props.disabled && id === epoch;
}
async function read(
  c: Conditions,
  before: string,
  limit: number,
  id: number,
  generation = "",
  total = "",
) {
  const q = new URLSearchParams({
    identity_format: "string",
    reason: c.reason,
    q: c.query,
    before_path: before,
    limit: String(limit),
  });
  if (generation) q.set("cache_generation", generation);
  const r = await fetch("/api/cloud-inventory/incomplete-directories?" + q, {
    credentials: "same-origin",
    signal: controller!.signal,
  });
  if (!current(id)) throw Error("已取消");
  if (r.status === 401) {
    cancel();
    emit("authenticationRequired");
    throw Error("请重新登录");
  }
  if (r.status === 409) throw Error("缓存代次变化，请重新读取");
  if (!r.ok) throw Error("缓存清单读取失败");
  const d = await r.json();
  if (!current(id)) throw Error("已取消");
  return parsePage(d, c, before, limit, generation, total);
}
async function load(apply = false, target = 0) {
  if (!alive || props.disabled || busy.value) return;
  const id = ++epoch;
  controller = new AbortController();
  busy.value = true;
  error.value = "";
  fresh.value = false;
  const c = apply
    ? { reason: draftReason.value, query: draftQuery.value.trim() }
    : { ...conditions.value };
  try {
    const result = await read(
      c,
      target ? cursors.value[target]! : "",
      25,
      id,
      target ? page.value?.cache_generation : "",
      target ? page.value?.total : "",
    );
    if (!current(id)) return;
    page.value = result;
    conditions.value = c;
    index.value = target;
    if (!target) cursors.value = [""];
    fresh.value = true;
  } catch (e: any) {
    if (current(id)) error.value = e.message + "；已显示结果仅供历史查看。";
  } finally {
    if (current(id)) busy.value = false;
  }
}
function next() {
  if (page.value?.has_more) {
    cursors.value[index.value + 1] = page.value.next_before_path;
    load(false, index.value + 1);
  }
}
function save(entries: Row[], scope: string, snapshot: Page) {
  const blob = new Blob(
    [
      JSON.stringify(
        {
          source: "local_cache",
          scope,
          conditions: { ...conditions.value },
          cache_generation: snapshot.cache_generation,
          total: snapshot.total,
          exported_count: String(entries.length),
          exported_at_utc: new Date().toISOString(),
          observed_at_utc: snapshot.observed_at_utc,
          entries,
        },
        null,
        2,
      ),
    ],
    { type: "application/json" },
  );
  const url = URL.createObjectURL(blob),
    a = document.createElement("a");
  a.href = url;
  a.download = "cache-incomplete-" + scope + ".json";
  a.click();
  window.setTimeout(() => URL.revokeObjectURL(url), 0);
}
async function exportRows(all = false) {
  if (!fresh.value || !page.value || busy.value || props.disabled) return;
  const snapshot = page.value;
  if (!all) {
    save(snapshot.entries, "page", snapshot);
    return;
  }
  if (BigInt(snapshot.total) > 10000n) {
    error.value = "完整导出最多 10000 条，请缩小筛选范围。";
    return;
  }
  const id = ++epoch;
  controller = new AbortController();
  busy.value = true;
  exporting.value = true;
  error.value = "";
  let before = "",
    rows: Row[] = [];
  try {
    while (true) {
      const part = await read(
        { ...conditions.value },
        before,
        100,
        id,
        snapshot.cache_generation,
        snapshot.total,
      );
      if (
        part.entries.length &&
        rows.length &&
        comparePaths(part.entries[0]!.path, rows[rows.length - 1]!.path) <= 0
      )
        throw Error("导出路径重复或倒序");
      rows.push(...part.entries);
      progress.value = String(rows.length) + " / " + snapshot.total;
      if (rows.length > 10000 || BigInt(rows.length) > BigInt(snapshot.total))
        throw Error("导出数量变化");
      if (!part.has_more) break;
      before = part.next_before_path;
    }
    if (BigInt(rows.length) !== BigInt(snapshot.total))
      throw Error("导出期间清单数量变化");
    if (current(id)) save(rows, "filtered", snapshot);
  } catch (e: any) {
    if (current(id)) {
      error.value = e.message + "；未生成导出文件，请重新读取。";
      fresh.value = false;
    }
  } finally {
    if (current(id)) {
      busy.value = false;
      exporting.value = false;
    }
  }
}
watch(
  () => props.disabled,
  (v) => {
    if (v) cancel();
  },
);
onMounted(() => {
  window.addEventListener("pagehide", hide);
  window.addEventListener("pageshow", show);
});
onUnmounted(() => {
  hide();
  window.removeEventListener("pagehide", hide);
  window.removeEventListener("pageshow", show);
});
</script>
<template>
  <section class="event-results" data-testid="cache-incomplete">
    <header class="page-header">
      <div>
        <h2>缓存覆盖不完整路径</h2>
        <p class="muted">
          只读本地缓存；查看和导出不会启动扫描。缺少缓存不代表远端文件不存在。完整导出最多 10000 项，逐页核对代次与数量，不是远端原子快照。
        </p>
      </div>
      <button
        class="button secondary"
        data-testid="incomplete-load"
        :disabled="disabled || busy"
        @click="load()"
      >
        {{ page ? "重新读取清单" : "查看不完整路径" }}
      </button>
    </header>
    <form class="cache-controls" @submit.prevent="load(true)">
      <label
        >原因
        <select v-model="draftReason" :disabled="disabled || busy">
          <option value="">全部原因</option>
          <option v-for="(label, key) in reasons" :key="key" :value="key">
            {{ label }}
          </option>
        </select></label
      ><label
        >路径包含
        <input
          v-model="draftQuery"
          maxlength="128"
          :disabled="disabled || busy"
          placeholder="输入路径关键字" /></label
      ><button class="button secondary" :disabled="disabled || busy">
        应用筛选并读取
      </button>
    </form>
    <p v-if="error" role="alert" class="notice danger">{{ error }}</p>
    <p v-if="busy" role="status">
      {{ exporting ? "正在导出 " + progress : "正在读取本地清单…" }}
      <button
        v-if="exporting"
        class="button secondary compact"
        @click="
          cancel();
          error = '已取消导出，请重新读取。';
        "
      >
        取消导出
      </button>
    </p>
    <template v-if="page"
      ><p class="muted">
        已显示条件：{{ reasons[conditions.reason] || "全部原因" }} ·
        {{ conditions.query || "全部路径" }} · 共 {{ page.total }} 项 · 缓存代次
        {{ page.cache_generation }}{{ fresh ? "" : " · 历史结果" }}
      </p>
      <p class="muted">
        本机读取 UTC：{{
          page.observed_at_utc
        }}。下方为缓存观察记录，不是远端实时证明；跳转后读取当前缓存。
      </p>
      <div class="event-table-scroll">
        <table>
          <thead>
            <tr>
              <th>目录路径</th>
              <th>原因 / 状态</th>
              <th>缓存观察 UTC / 关联 ID</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in page.entries" :key="row.path">
              <td>{{ row.path }}</td>
              <td>
                {{ reasons[row.reason] }} / {{ row.status }}<br />{{
                  row.cached_directory ? "有目录缓存" : "缺少目录缓存"
                }}
              </td>
              <td>
                {{ row.observed_at_utc || "未记录"
                }}<br v-if="row.last_run_id" />{{ row.last_run_id || "" }}
              </td>
              <td>
                <a
                  v-if="row.cached_directory"
                  class="button secondary compact"
                  :href="
                    '/cloud/files?source=cache&amp;dir=' +
                    encodeURIComponent(row.path)
                  "
                  >查看缓存</a
                >
                <a
                  class="button secondary compact"
                  :href="
                    '/cloud/cache?directory=' + encodeURIComponent(row.path)
                  "
                  >准备刷新</a
                >
              </td>
            </tr>
            <tr v-if="!page.entries.length">
              <td colspan="4">
                当前筛选没有不完整路径；这不等于已验证远端完整。
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="incomplete-actions">
        <button
          class="button secondary compact"
          :disabled="disabled || busy || !fresh || index === 0"
          @click="load(false, index - 1)"
        >
          上一页</button
        ><span>第 {{ index + 1 }} 页 · 本页 {{ page.entries.length }} 项</span
        ><button
          class="button secondary compact"
          :disabled="disabled || busy || !fresh || !page.has_more"
          @click="next"
        >
          下一页</button
        ><button
          class="button secondary compact"
          data-testid="incomplete-export"
          :disabled="disabled || busy || !fresh"
          @click="exportRows()"
        >
          导出本页 JSON</button
        ><button
          class="button secondary compact"
          :disabled="disabled || busy || !fresh"
          @click="exportRows(true)"
        >
          导出全部筛选结果
        </button>
      </div></template
    >
  </section>
</template>
<style scoped>
.incomplete-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
  margin-top: 12px;
}
td {
  vertical-align: top;
  overflow-wrap: anywhere;
}
td:first-child {
  min-width: 180px;
  max-width: 420px;
}
td:nth-child(3) {
  max-width: 300px;
}
table {
  width: 100%;
}
select,
input {
  max-width: 100%;
}
</style>
