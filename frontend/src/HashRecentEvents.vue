<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from "vue";
import {
  historyCount,
  historyUTC,
  historyRunID,
  historyTimeText,
  historyStatuses,
  historyPhases,
} from "./cloud-history-types";
const props = defineProps<{ disabled: boolean }>(),
  emit = defineEmits<{ authenticationRequired: [] }>();
type Row = {
  run_id: string;
  sequence: string;
  status: string;
  phase: string;
  recorded_at_utc: string;
  evidence_json: string;
};
const rows = ref<Row[]>([]),
  opened = ref(false),
  fresh = ref(false),
  busy = ref(false),
  error = ref(""),
  observed = ref(""),
  before = ref("0"),
  next = ref("0"),
  more = ref(false),
  stack = ref<string[]>([]);
let active = true,
  epoch = 0,
  controller: AbortController | undefined;
function invalidate() {
  epoch++;
  controller?.abort();
  busy.value = false;
  fresh.value = false;
}
function hide() {
  active = false;
  invalidate();
}
function restore(e: PageTransitionEvent) {
  if (e.persisted) {
    active = true;
    error.value = "页面已恢复，保留历史结果；请重新读取最近事件。";
  }
}
function current(id: number) {
  return active && !props.disabled && id === epoch;
}
async function load(
  cursor = "0",
  direction: "reset" | "next" | "previous" = "reset",
) {
  if (!active || props.disabled || busy.value) return;
  const id = ++epoch;
  controller = new AbortController();
  busy.value = true;
  fresh.value = false;
  error.value = "";
  const query = new URLSearchParams({ identity_format: "string", limit: "20" });
  if (cursor !== "0") query.set("before_sequence", cursor);
  try {
    const r = await fetch("/api/cloud-cleanup/duplicate-scan/events?" + query, {
      credentials: "same-origin",
      signal: controller.signal,
    });
    if (!current(id)) return;
    if (r.status === 401) {
      invalidate();
      emit("authenticationRequired");
      return;
    }
    if (!r.ok) throw Error("跨运行事件暂时不可读");
    const d = await r.json();
    if (!current(id)) return;
    if (
      !d ||
      d.run_id !== "" ||
      !Array.isArray(d.rows) ||
      d.rows.length > 20 ||
      !historyCount(d.next_before_sequence) ||
      typeof d.has_more !== "boolean"
    )
      throw Error("跨运行事件分页响应无效");
    let previous = cursor === "0" ? 9223372036854775808n : BigInt(cursor);
    for (const row of d.rows) {
      if (
        !row ||
        !historyCount(row.sequence, false) ||
        BigInt(row.sequence) >= previous ||
        typeof row.run_id !== "string" ||
        row.run_id.length > 256 ||
        /[\u0000-\u001f\u007f]/.test(row.run_id) ||
        typeof row.status !== "string" ||
        typeof row.phase !== "string" ||
        !historyUTC(row.recorded_at_utc) ||
        typeof row.evidence_json !== "string"
      )
        throw Error("跨运行事件身份或原文证据无效");
      previous = BigInt(row.sequence);
    }
    if (
      (d.has_more &&
        (!d.rows.length ||
          d.next_before_sequence !== d.rows.at(-1).sequence)) ||
      (!d.has_more && d.next_before_sequence !== "0")
    )
      throw Error("跨运行事件游标未推进");
    if (direction === "next") stack.value.push(before.value);
    else if (direction === "previous") stack.value.pop();
    else stack.value = [];
    rows.value = d.rows;
    before.value = cursor;
    next.value = d.next_before_sequence;
    more.value = d.has_more;
    opened.value = true;
    fresh.value = true;
    observed.value = new Date().toISOString();
  } catch (e: any) {
    if (current(id))
      error.value = e.message + "；已显示结果仅供历史查看，请重新读取。";
  } finally {
    if (current(id)) busy.value = false;
  }
}
watch(
  () => props.disabled,
  (v) => {
    if (v) invalidate();
  },
);
onMounted(() => {
  window.addEventListener("pagehide", hide);
  window.addEventListener("pageshow", restore);
});
onUnmounted(() => {
  hide();
  window.removeEventListener("pagehide", hide);
  window.removeEventListener("pageshow", restore);
});
</script>
<template>
  <section
    class="event-results hash-recent-events"
    data-testid="hash-recent-events"
  >
    <header class="page-header">
      <div>
        <h2>跨运行最近事件</h2>
        <p class="muted">
          只读本地哈希扫描事件，可包含旧运行标识。此处不限定某轮，也不证明扫描器当前仍在运行。
        </p>
      </div>
      <button
        class="button secondary"
        data-testid="hash-recent-load"
        :disabled="disabled || busy"
        @click="load()"
      >
        {{ opened ? "重新读取最近事件" : "查看跨运行最近事件" }}
      </button>
    </header>
    <p v-if="error" class="notice danger" role="alert">{{ error }}</p>
    <p v-if="busy" role="status">正在读取本地事件…</p>
    <template v-if="opened"
      ><p class="muted">
        本页游标 {{ before }} · 本页 {{ rows.length }} 条{{
          fresh ? "" : " · 历史结果"
        }}
        · 本机读取 UTC {{ observed }}
      </p>
      <div class="event-table-scroll">
        <table>
          <thead>
            <tr>
              <th>序号 / 记录 UTC</th>
              <th>所属运行</th>
              <th>状态 / 阶段</th>
              <th>脱敏原文</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="row.sequence">
              <td>
                {{ row.sequence }}<br />{{
                  historyTimeText(row.recorded_at_utc)
                }}
              </td>
              <td>
                {{ row.run_id || "历史未记录" }}<br /><a
                  v-if="historyRunID(row.run_id)"
                  class="button secondary compact"
                  :href="'/cloud/history?source=hash&amp;run=' + row.run_id"
                  >按此标识查看运行</a
                ><span v-else class="muted">旧标识，仅保留原值</span>
              </td>
              <td>
                {{ historyStatuses[row.status] || row.status }}<br />{{
                  historyPhases[row.phase] || row.phase
                }}
              </td>
              <td>
                <details>
                  <summary>展开原始 JSON</summary>
                  <pre>{{ row.evidence_json || "历史未记录" }}</pre>
                </details>
              </td>
            </tr>
            <tr v-if="!rows.length">
              <td colspan="4">暂无读取到的本地事件，不代表从未执行过扫描。</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="path-impact-pagination">
        <button
          class="button secondary compact"
          data-testid="hash-recent-previous"
          :disabled="disabled || busy || !fresh || !stack.length"
          @click="load(stack[stack.length - 1] || '0', 'previous')"
        >
          较新一页</button
        ><button
          class="button secondary compact"
          data-testid="hash-recent-next"
          :disabled="disabled || busy || !fresh || !more"
          @click="load(next, 'next')"
        >
          更早一页
        </button>
      </div></template
    >
  </section>
</template>
<style scoped>
table {
  width: 100%;
}
td {
  max-width: 340px;
  min-width: 130px;
  vertical-align: top;
  overflow-wrap: anywhere;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-width: 340px;
}
summary {
  cursor: pointer;
}
</style>
