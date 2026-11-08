<script setup lang="ts">
import {computed} from 'vue';
import {validEventID} from './event-route';
const props=defineProps<{task:{id:string;status:string;prepared:boolean;error?:string};stale:boolean}>();
const state=computed(()=>{
 switch(props.task.status){
  case 'source_unknown':return {title:'网盘后处理结果待核对',text:'本地阶段已结束，但网盘改名或删除缺少确定结果。本次后处理以任务事件为准；其他网盘操作记录不保证包含本任务。不要重复发送后处理操作。',link:'/operations',label:'其他网盘操作记录'};
  case 'auth_wait':return {title:'正在等待百度授权',text:'请先核对账号授权，再查看任务事件确认是否恢复；授权可用不等于该任务已开始传输。',link:'/oauth',label:'检查账号与授权'};
  case 'retry_wait':return {title:'正在等待重试',text:'任务处于重试等待状态。请结合服务记录与事件时间核对原因；此处没有倒计时证据，不推测下一次执行时间。'};
  case 'failed':return {title:'下载失败',text:'请核对下面的服务记录和任务事件，再决定如何处理。可否重试仍以任务操作区提供的能力为准。'};
  case 'paused':return {title:'任务已暂停',text:'该任务当前记录为暂停；继续操作和结果请在任务操作区及事件中核对。'};
  case 'pending':return {title:'正在等待后台调度',text:'等待状态不代表正在传输。可查看监控历史及任务事件，核对目录发现和后续调度。',link:'/monitor-history',label:'查看目录监控历史'};
  case 'downloading':if(!props.task.prepared)return {title:'正在准备目录内容',text:'目录准备尚未完成，不能将当前文件数当作完整清单，也不能按零字节认定为空目录。请结合监控历史和任务事件核对。',link:'/monitor-history',label:'查看目录监控历史'};
 }
 return null;
});
const eventsURL=computed(()=>validEventID(props.task.id)?'/task-events?task_id='+encodeURIComponent(props.task.id):'/task-events');
</script>
<template><section v-if="state" class="download-state-guidance" data-testid="download-state-guidance" aria-label="下载状态说明"><h3>{{state.title}}</h3><p v-if="stale" class="notice">下方是上次读取的任务状态，当前状态尚未重新确认。</p><p>{{state.text}}</p><p v-if="task.error" class="download-state-reason"><strong>服务记录的说明：</strong>{{task.error}}</p><nav aria-label="下载状态核对入口"><!-- Persisted source postprocessing is recorded in task events, separately from file-management operations. --><a class="button compact" :class="task.status==='source_unknown'?'primary':'secondary'" data-testid="download-state-events" :href="eventsURL">查看任务事件</a><a v-if="state.link" class="button secondary compact" :data-testid="state.link==='/operations'?'download-state-operations':undefined" :href="state.link">{{state.label}}</a></nav><p class="muted">说明来自任务接口返回的记录；具体发生时间请在事件中核对 UTC，不代表已重新验证网盘或本地文件。</p></section></template>
<style scoped>
.download-state-guidance{margin:16px 0;padding:16px 20px;border:1px solid #cbd5e1;border-radius:10px;background:#f8fafc;overflow-wrap:anywhere}.download-state-guidance h3{margin:0 0 8px;font-size:16px}.download-state-guidance p{line-height:1.6}.download-state-guidance nav{display:flex;flex-wrap:wrap;gap:8px}.download-state-reason{white-space:pre-wrap}.download-state-guidance .muted{margin-bottom:0;font-size:12px}
</style>
