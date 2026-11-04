<script setup lang="ts">
import {X} from 'lucide-vue-next';
import {vModal} from './modal';
import {intakeStates,intakeErrors,intakeLabel,type IntakeRecord} from './intake-types';
defineProps<{record:IntakeRecord;busy:boolean;disabled:boolean;feedback:string}>();
const emit=defineEmits<{close:[];recheck:[record:IntakeRecord]}>();
</script>
<template>
 <div class="modal-backdrop" @click.self="emit('close')"><section class="detail-drawer" role="dialog" aria-modal="true" aria-labelledby="intake-record-title" v-modal="()=>emit('close')">
  <header><h2 id="intake-record-title">入队详情</h2><button class="icon-button" aria-label="关闭入队详情" @click="emit('close')"><X/></button></header>
  <p class="notice">这是读取时的历史快照，不能作为新的移动确认。结果未知持续隔离，不重发、不提供人工放行。</p>
  <button v-if="record.items.some(item=>item.state==='unknown'||item.state==='sending')" class="button secondary" data-testid="intake-recheck" :disabled="busy||disabled" @click="emit('recheck',record)">{{busy?'正在处理…':'重新核对可确认的项目'}}</button>
  <p v-if="feedback" class="notice" role="status">{{feedback}}</p>
  <dl class="event-detail-fields"><dt>批次 ID</dt><dd>{{record.id}}</dd><dt>记录状态</dt><dd>{{intakeLabel(record)}}</dd><dt>目标队列</dt><dd>{{record.queue_dir}}</dd><dt>固定策略</dt><dd>{{record.download_mode==='delete'?'下载完成后删除网盘项':'下载完成后保留并标记 .可删除'}}</dd><dt>创建时间</dt><dd>{{record.created}}</dd><dt>预检有效期至</dt><dd>{{record.expires}}</dd></dl>
  <p class="muted">时间按记录原时区显示，不推算完成时间或当前云端状态。</p>
  <div class="event-table-scroll"><table><thead><tr><th>源项目 / 身份</th><th>目标</th><th>结果 / 依据</th></tr></thead><tbody><tr v-for="item in record.items" :key="item.source.path"><td>{{item.source.path}}<small class="cell-sub">对象 ID：{{item.source_id}}</small></td><td>{{item.target}}</td><td>{{intakeStates[item.state]}}<small v-if="item.error" class="cell-sub">{{intakeErrors[item.error]||item.error}}</small><small v-if="typeof item.async_task_id==='string'" class="cell-sub">异步任务 ID：{{item.async_task_id}}</small><small v-if="item.sync_confirmed===true" class="cell-sub">已记录同步确认</small></td></tr></tbody></table></div>
 </section></div>
</template>
