<script setup lang="ts">
import {computed} from 'vue';
import {CircleCheck,TriangleAlert,CircleX} from 'lucide-vue-next';
import {validOperationID} from './operation-route';
import type {CloudActionResult} from './cloud-files-types';
const props=defineProps<{result:CloudActionResult}>();
const status=computed(()=>props.result.status==='confirmed'?'confirmed':props.result.status==='failed'?'failed':'unknown');
const label=computed(()=>({confirmed:'已确认',failed:'失败',unknown:'结果未知'})[status.value]);
const operationID=computed(()=>validOperationID(props.result.operation_id)?props.result.operation_id:'');
const recordURL=computed(()=>operationID.value?'/operations?operation_id='+operationID.value:'/operations');
</script>
<template><section class="event-results cloud-action-feedback" :class="'feedback-'+status" data-testid="cloud-action-result" :role="status==='unknown'?'alert':'status'" aria-atomic="true"><header class="feedback-heading"><div><component :is="status==='confirmed'?CircleCheck:status==='failed'?CircleX:TriangleAlert" :size="18" aria-hidden="true"/><strong>最近操作 · {{label}}</strong></div><a class="button secondary compact" :href="recordURL">{{operationID?'查看此项操作记录':'查看操作记录中心'}}</a></header><p class="feedback-message">{{result.message}}</p><p v-if="status==='unknown'" class="feedback-guidance">请求可能已在网盘执行，新建等写操作暂不可用；先核对记录，勿重复提交。此提示不会解除未知操作的隔离。</p><dl class="feedback-paths"><template v-if="result.source"><dt>来源 / 当时目录</dt><dd>{{result.source}}</dd></template><template v-if="result.target"><dt>操作目标</dt><dd>{{result.target}}</dd></template></dl><details v-if="operationID"><summary>操作记录 ID</summary><code>{{operationID}}</code></details><p v-else class="muted">未取得有效操作记录 ID，请在记录中心按路径和时间核对。</p><small class="muted">仅显示本页最近一项反馈；完整历史见操作记录。列表刷新不代表操作结果已重新核对。</small></section></template>
<style scoped>
.cloud-action-feedback{border-left:3px solid #71829c;margin:12px 0;padding:14px 16px;min-width:0}.feedback-confirmed{border-left-color:#218768}.feedback-failed{border-left-color:#bc4848}.feedback-unknown{border-left-color:#b17a19}.feedback-heading,.feedback-heading>div{display:flex;align-items:center;gap:8px}.feedback-heading{justify-content:space-between;flex-wrap:wrap}.feedback-heading .button{border:1px solid #cbd5e5}.feedback-message{margin:10px 0}.feedback-guidance{font-size:13px;color:#765318}.feedback-paths{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:6px 14px;margin:10px 0;font-size:13px}.feedback-paths dt,.feedback-paths dd{margin:0;overflow-wrap:anywhere}.feedback-paths dt{color:#617089}.cloud-action-feedback details{margin:8px 0}.cloud-action-feedback summary{cursor:pointer}.cloud-action-feedback code{overflow-wrap:anywhere}.cloud-action-feedback>small{display:block;margin-top:10px}@media(max-width:540px){.feedback-paths{grid-template-columns:minmax(0,1fr);gap:4px}.feedback-paths dt:not(:first-child){margin-top:6px}.feedback-heading{align-items:flex-start}.cloud-action-feedback{padding:12px}}
</style>
