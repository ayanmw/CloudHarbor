<script setup lang="ts">
import {X} from 'lucide-vue-next';
import {vModal} from './modal';
import {downloadRouteURL} from './download-route';
import {monitorRouteURL,emptyMonitorFilters} from './monitor-route';
import {intakeWaitReasons,intakeDownloadStates,intakeStates,intakeErrors,intakeLabel,type IntakeRecord} from './intake-types';
defineProps<{record:IntakeRecord;busy:boolean;disabled:boolean;feedback:string}>();
const emit=defineEmits<{close:[];recheck:[record:IntakeRecord]}>();
</script>
<template>
 <div class="modal-backdrop" @click.self="emit('close')"><section class="detail-drawer intake-detail-drawer" role="dialog" aria-modal="true" aria-labelledby="intake-record-title" v-modal="()=>emit('close')">
  <header><h2 id="intake-record-title">入队详情</h2><button class="icon-button" aria-label="关闭入队详情" @click="emit('close')"><X/></button></header>
  <p class="notice">这是读取时的历史快照，不能作为新的移动确认。结果未知持续隔离，不重发、不提供人工放行。</p>
  <button v-if="record.items.some(item=>item.state==='unknown'||item.state==='sending')" class="button secondary" data-testid="intake-recheck" :disabled="busy||disabled" @click="emit('recheck',record)">{{busy?'正在处理…':'重新核对可确认的项目'}}</button>
  <p v-if="feedback" class="notice" role="status">{{feedback}}</p>
  <dl class="event-detail-fields intake-detail-summary"><div><dt>批次 ID</dt><dd>{{record.id}}</dd></div><div><dt>记录状态</dt><dd>{{intakeLabel(record)}}</dd></div><div><dt>目标队列</dt><dd>{{record.queue_dir}}</dd></div><div><dt>固定策略</dt><dd>{{record.download_mode==='delete'?'下载完成后删除网盘项':'下载完成后保留并标记 .可删除'}}</dd></div><div><dt>创建时间</dt><dd>{{record.created}}</dd></div><div><dt>预检有效期至</dt><dd>{{record.expires}}</dd></div></dl>
  <details class="intake-evidence-notes"><summary>查看时间与证据说明</summary><p class="muted">时间按记录原时区显示，不推算完成时间或当前云端状态。队列登记只说明本地根任务已登记，不代表监控已发现、文件已下载或当前正在执行。监控观察表示当时列表中核对到该项目，不保证整轮扫描成功或当前仍在线。</p></details>
  <div class="event-table-scroll intake-detail-table"><table><thead><tr><th>源项目 / 身份</th><th>目标</th><th>结果 / 依据</th><th>关联下载任务</th></tr></thead><tbody><tr v-for="item in record.items" :key="item.source.path"><td data-label="源项目 / 身份">{{item.source.path}}<small class="cell-sub">对象 ID：{{item.source_id}}</small></td><td data-label="目标">{{item.target}}</td><td data-label="结果 / 依据">{{intakeStates[item.state]}}<small v-if="item.error" class="cell-sub">{{intakeErrors[item.error]||item.error}}</small><small v-if="typeof item.async_task_id==='string'" class="cell-sub">异步任务 ID：{{item.async_task_id}}</small><small v-if="item.sync_confirmed===true" class="cell-sub">已记录同步确认；本机收到回执：{{item.sync_confirmed_at_utc||'历史时间未记录'}}</small><small v-if="item.state==='moved'" class="cell-sub">本地队列登记核对：{{item.queue_registered_at_utc||'历史时间未记录'}}</small></td><td data-label="关联下载任务"><template v-if="item.download_task"><a class="button secondary compact" :href="downloadRouteURL({task:item.download_task.task_id,status:'',q:'',offset:0,manifest:''})">查看下载任务</a><small class="cell-sub">{{intakeDownloadStates[item.download_task.status]}}</small><small v-if="item.download_task.wait_reason" class="cell-sub">{{intakeWaitReasons[item.download_task.wait_reason]}}</small><small class="cell-sub">状态读取于：{{item.download_task.observed_at_utc}}</small><div v-if="item.download_task.monitor_observation"><a class="button secondary compact" :href="monitorRouteURL({filters:emptyMonitorFilters(),before:'',run:item.download_task.monitor_observation.run_id})">查看监控观察</a><small class="cell-sub">首次核对到：{{item.download_task.monitor_observation.observed_at_utc}}</small><small class="cell-sub">当时路径：{{item.download_task.monitor_observation.path}}</small></div><small v-else class="cell-sub">未记录监控观察；可到监控历史核对。</small></template><span v-else class="muted">{{item.state==='moved'?'未找到有批次来源证据的任务；可到下载列表核对':'尚无已确认的任务关联'}}</span></td></tr></tbody></table></div>
 </section></div>
</template>
