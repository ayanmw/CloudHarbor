<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from "vue";
import {
  reasonLabels,
  historyUTC,
  type HistoryRun,
} from "./cloud-history-types";
type ScanRun = Extract<HistoryRun, { source: "scan" }>;
type Snapshot = { run: ScanRun; read_at_utc: string; context: string };
type Comparison = {
  status: string;
  reason_status: string;
  incomplete_directories_delta: string | null;
  reason_deltas: Record<string, string> | null;
};
const props = defineProps<{ disabled: boolean }>();
const selected = ref<Snapshot[]>([]),
  result = ref<{
    earlier: Snapshot;
    later: Snapshot;
    comparison: Comparison;
  }>(),
  error = ref("");
let active = true;
function contains(id: string) {
  return selected.value.some((s) => s.run.run_id === id);
}
function canSelect(id: string) {
  return contains(id) || selected.value.length < 2;
}
function clear() {
  selected.value = [];
  result.value = undefined;
  error.value = "";
}
function remove(id: string) {
  selected.value = selected.value.filter((s) => s.run.run_id !== id);
  result.value = undefined;
  error.value = "";
}
function select(run: HistoryRun, context: string, observedAt: string) {
  if (!active || props.disabled || run.source !== "scan") return;
  if (contains(run.run_id)) {
    remove(run.run_id);
    return;
  }
  if (!canSelect(run.run_id)) return;
  selected.value.push({
    run: JSON.parse(JSON.stringify(run)),
    read_at_utc: observedAt,
    context,
  });
  result.value = undefined;
  error.value = "";
}
function compare() {
  if (!active || props.disabled || selected.value.length !== 2) return;
  const ordered = [...selected.value].sort((a, b) =>
      BigInt(a.run.sequence) < BigInt(b.run.sequence) ? -1 : 1,
    ),
    earlier = ordered[0]!,
    later = ordered[1]!;
  const c: Comparison = {
    status: "comparable",
    reason_status: "reasons_unrecorded",
    incomplete_directories_delta: null,
    reason_deltas: null,
  };
  result.value = { earlier, later, comparison: c };
  if (earlier.run.sequence === later.run.sequence) {
    c.status = "sequence_conflict";
    return;
  }
  if ([earlier, later].some((s) => s.run.status !== "complete")) {
    c.status = "runs_must_be_complete";
    return;
  }
  if (
    [earlier, later].some(
      (s) =>
        !s.run.coverage_status ||
        s.run.coverage_incomplete_directories === null ||
        !historyUTC(s.run.coverage_observed_at_utc) ||
        s.run.coverage_observed_at_utc!.startsWith("0001-"),
    )
  ) {
    c.status = "coverage_snapshot_unrecorded";
    return;
  }
  c.incomplete_directories_delta = (
    BigInt(later.run.coverage_incomplete_directories!) -
    BigInt(earlier.run.coverage_incomplete_directories!)
  ).toString();
  if (!earlier.run.coverage_reasons || !later.run.coverage_reasons) return;
  c.reason_status = "comparable";
  c.reason_deltas = {};
  for (const key of Object.keys(reasonLabels) as (keyof typeof reasonLabels)[])
    c.reason_deltas[key] = (
      BigInt(later.run.coverage_reasons[key]) -
      BigInt(earlier.run.coverage_reasons[key])
    ).toString();
}
const statuses: Record<string, string> = {
  comparable: "覆盖总数可比较",
  runs_must_be_complete: "两轮均需已完成，当前不计算差值",
  coverage_snapshot_unrecorded: "缺少历史覆盖快照或观察时间，当前不计算差值",
  sequence_conflict: "历史序号冲突，当前不计算差值",
};
function exportResult() {
  if (!active || props.disabled || !result.value) return;
  let url = "";
  try {
    const blob = new Blob(
      [
        JSON.stringify(
          {
            format_version: 1,
            scope: "selected_full_scan_coverage_comparison",
            exported_at_utc: new Date().toISOString(),
            ...result.value,
          },
          null,
          2,
        ),
      ],
      { type: "application/json" },
    );
    url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "selected-scan-coverage-comparison.json";
    a.click();
    error.value = "";
  } catch {
    error.value = "比较JSON导出失败，请重试。";
  } finally {
    if (url) window.setTimeout(() => URL.revokeObjectURL(url), 0);
  }
}
function hide() {
  active = false;
  clear();
}
function show(e: PageTransitionEvent) {
  if (e.persisted) active = true;
}
watch(
  () => props.disabled,
  (v) => {
    if (v) clear();
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
defineExpose({ select, contains, canSelect });
</script>
<template>
  <section class="scan-comparison" data-testid="scan-compare-panel">
    <header class="page-header">
      <div>
        <h3>比较两轮覆盖统计</h3>
        <p class="muted">
          已选 {{ selected.length }} / 2
          轮。使用下方运行表的“加入比较”，可跨分页和筛选选择。
        </p>
      </div>
      <div class="comparison-actions">
        <button
          class="button secondary compact"
          data-testid="scan-compare-start"
          :disabled="disabled || selected.length !== 2"
          @click="compare"
        >
          比较所选两轮</button
        ><button
          class="button secondary compact"
          :disabled="disabled || !selected.length"
          @click="clear"
        >
          清空比较选择</button
        ><button
          class="button secondary compact"
          data-testid="scan-compare-export"
          :disabled="disabled || !result"
          @click="exportResult"
        >
          导出比较 JSON
        </button>
      </div>
    </header>
    <p class="muted">
      比较的是选中时保存的本地历史快照，刷新列表不会替换已选快照。按运行序号确定先后，差值为后轮减前轮；不是文件差异或当前网盘状态，各原因可能重叠，不能相加。离开页面会清除选择。
    </p>
    <ul v-if="selected.length">
      <li v-for="s in selected" :key="s.run.run_id">
        <code>{{ s.run.run_id }}</code> · 序号 {{ s.run.sequence }} · 读取 UTC
        {{ s.read_at_utc }}
        <button
          class="button secondary compact"
          :disabled="disabled"
          @click="remove(s.run.run_id)"
        >
          移除此轮
        </button>
      </li>
    </ul>
    <p v-if="error" class="notice danger" role="alert">{{ error }}</p>
    <div v-if="result" data-testid="scan-compare-result">
      <p class="notice">
        {{ statuses[result.comparison.status] }}。{{
          result.comparison.reason_status === "comparable"
            ? "原因明细可比较"
            : "原因证据不足，不计算原因差值。"
        }}
      </p>
      <div class="event-table-scroll">
        <table>
          <thead>
            <tr>
              <th>历史证据</th>
              <th>前轮</th>
              <th>后轮</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <th>运行 ID</th>
              <td>{{ result.earlier.run.run_id }}</td>
              <td>{{ result.later.run.run_id }}</td>
            </tr>
            <tr>
              <th>序号 / 完成状态</th>
              <td>
                {{ result.earlier.run.sequence }} /
                {{ result.earlier.run.status }}
              </td>
              <td>
                {{ result.later.run.sequence }} / {{ result.later.run.status }}
              </td>
            </tr>
            <tr>
              <th>覆盖状态 / 代次</th>
              <td>
                {{ result.earlier.run.coverage_status || "未记录" }} /
                {{ result.earlier.run.coverage_generation ?? "未记录" }}
              </td>
              <td>
                {{ result.later.run.coverage_status || "未记录" }} /
                {{ result.later.run.coverage_generation ?? "未记录" }}
              </td>
            </tr>
            <tr>
              <th>覆盖观察 UTC</th>
              <td>
                {{ result.earlier.run.coverage_observed_at_utc || "未记录" }}
              </td>
              <td>
                {{ result.later.run.coverage_observed_at_utc || "未记录" }}
              </td>
            </tr>
            <tr>
              <th>读取条件</th>
              <td>{{ result.earlier.context }}</td>
              <td>{{ result.later.context }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="event-table-scroll">
        <table>
          <thead>
            <tr>
              <th>统计项</th>
              <th>前轮</th>
              <th>后轮</th>
              <th>差值（后 − 前）</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <th>未完整目录</th>
              <td>
                {{
                  result.earlier.run.coverage_incomplete_directories ?? "未记录"
                }}
              </td>
              <td>
                {{
                  result.later.run.coverage_incomplete_directories ?? "未记录"
                }}
              </td>
              <td>
                {{
                  result.comparison.incomplete_directories_delta ?? "不可计算"
                }}
              </td>
            </tr>
            <tr v-for="(label, key) in reasonLabels" :key="key">
              <th>{{ label }}</th>
              <td>
                {{ result.earlier.run.coverage_reasons?.[key] ?? "未记录" }}
              </td>
              <td>
                {{ result.later.run.coverage_reasons?.[key] ?? "未记录" }}
              </td>
              <td>
                {{ result.comparison.reason_deltas?.[key] ?? "不可计算" }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>
<style scoped>
.scan-comparison {
  border: 1px solid #dfe3ed;
  border-radius: 12px;
  padding: 16px;
  margin: 16px 0;
  min-width: 0;
}
.comparison-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}
table {
  width: 100%;
}
td,
th,
li {
  overflow-wrap: anywhere;
  vertical-align: top;
}
td {
  max-width: 320px;
  min-width: 150px;
}
li {
  margin: 8px 0;
}
</style>
