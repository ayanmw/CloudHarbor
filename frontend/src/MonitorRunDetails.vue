<script setup lang="ts">
import {onUnmounted,ref,watch} from 'vue';
import {X} from 'lucide-vue-next';
import {vModal} from './modal';
import {validEventID as validSequence} from './event-route';
type Run={run_id:string;status:string;monitor_path:string;started_at_utc:string;finished_at_utc?:string;cloud_observed_at_utc?:string;recorded_at_utc:string;error_code?:string};
type MonitorEvent={sequence:string;run_id:string;kind:string;recorded_at_utc:string;cloud_observed_at_utc?:string;error_code?:string};
const props=defineProps<{runID:string;run:Run|null}>(),emit=defineEmits<{close:[]}>();
const labels:Record<string,string>={running:'正在运行',success:'成功',failed:'失败',interrupted:'进程中断（未判定成功）'};
// Read-only UI review found raw storage codes in the timeline. Translate the
// known producer codes, retaining exact codes in evidence and unknown values.
const eventLabels:Record<string,string>={started:'开始巡检',cloud_listed:'已读取网盘目录',completed:'本轮巡检完成',failed:'本轮巡检失败',interrupted:'进程中断（未判定成功）'};
const events=ref<MonitorEvent[]>([]),loading=ref(false),error=ref(''),next=ref<string|null>(null),pageIndex=ref(0),cursors=ref(['']);
const exporting=ref(false),exportError=ref(''),exportMessage=ref('');
const authenticationRequired=ref(false);
let exportGeneration=0,exportController:AbortController|undefined;
let generation=0,controller:AbortController|undefined;
async function loadEvents(before='',index=0){
 if(loading.value||authenticationRequired.value)return;
 const request=++generation,id=props.runID;controller?.abort();controller=new AbortController();loading.value=true;
 const query=new URLSearchParams({run_id:id,limit:'20',sequence_format:'string'});if(before)query.set('before_sequence',before);
 try{
  const response=await fetch('/api/monitor-scans/events?'+query,{credentials:'same-origin',signal:controller.signal});if(request!==generation||id!==props.runID)return;
  if(response.status===401){authenticationRequired.value=true;location.assign('/login?next='+encodeURIComponent(location.pathname+location.search));throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'事件暂不可读');
  const data=await response.json();if(request!==generation||id!==props.runID)return;
  if(!Array.isArray(data.events)||data.events.length>20||!data.events.every((event:MonitorEvent)=>event.run_id===id&&typeof event.sequence==='string'&&validSequence(event.sequence)&&(!before||BigInt(event.sequence)<BigInt(before))))throw Error('扫描事件身份或分页边界无效');
  const boundary=data.next_before_sequence;
  if(typeof data.has_more!=='boolean'||(data.has_more&&(typeof boundary!=='string'||!validSequence(boundary)||(before&&BigInt(boundary)>=BigInt(before))))||(!data.has_more&&boundary!==null))throw Error('扫描事件游标无效');
  events.value=data.events;next.value=boundary;pageIndex.value=index;cursors.value[index]=before;error.value='';
 }catch(e:any){if(request===generation&&id===props.runID&&e.name!=='AbortError')error.value=e.message;}
 finally{if(request===generation&&id===props.runID)loading.value=false;}
}
function navigate(delta:number){const index=pageIndex.value+delta,before=delta>0?next.value:cursors.value[index];if(index<0||before===null||before===undefined)return;loadEvents(before,index);}
function cancelExport(){exportGeneration++;exportController?.abort();exporting.value=false;exportError.value='';exportMessage.value='';}
async function exportRun(){
 if(exporting.value||authenticationRequired.value)return;
 const request=++exportGeneration,id=props.runID;exportController=new AbortController();exporting.value=true;exportError.value='';exportMessage.value='正在读取本轮脱敏证据…';
 try{
  const response=await fetch('/api/monitor-scans/'+encodeURIComponent(id)+'/export',{signal:exportController.signal,credentials:'same-origin'});if(request!==exportGeneration||id!==props.runID)return;
  if(response.status===401){authenticationRequired.value=true;location.assign('/login?next='+encodeURIComponent(location.pathname+location.search));throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'导出请求失败');
  const blob=await response.blob();if(request!==exportGeneration||id!==props.runID)return;
  const objectURL=URL.createObjectURL(blob),link=document.createElement('a');link.href=objectURL;link.download='queue-monitor-scan-'+id+'.json';document.body.append(link);link.click();link.remove();window.setTimeout(()=>URL.revokeObjectURL(objectURL),60000);exportMessage.value='本轮证据已开始下载。';
 }catch(e:any){if(request===exportGeneration&&id===props.runID&&e.name!=='AbortError'){exportMessage.value='';exportError.value='导出失败：'+e.message;}}
 finally{if(request===exportGeneration&&id===props.runID)exporting.value=false;}
}
watch(()=>props.runID,()=>{generation++;controller?.abort();cancelExport();authenticationRequired.value=false;events.value=[];error.value='';loading.value=false;next.value=null;pageIndex.value=0;cursors.value=[''];loadEvents();},{immediate:true});
onUnmounted(()=>{generation++;controller?.abort();cancelExport();});
</script>
<template>
 <div class="modal-backdrop" @click.self="emit('close')"><section class="detail-drawer" role="dialog" aria-modal="true" aria-labelledby="monitor-detail-title" v-modal="()=>emit('close')">
  <header><h2 id="monitor-detail-title">扫描详情</h2><button class="icon-button" aria-label="关闭扫描详情" @click="emit('close')"><X/></button></header>
  <h3>{{run?labels[run.status]:'扫描轮次'}}</h3>
  <dl class="event-detail-fields"><dt>运行 ID</dt><dd>{{runID}}</dd><template v-if="run"><dt>监控目录</dt><dd>{{run.monitor_path}}</dd><dt>本机开始 UTC</dt><dd>{{run.started_at_utc}}</dd><dt>本机结束 UTC</dt><dd>{{run.finished_at_utc||'未记录'}}</dd><dt>本机记录 UTC</dt><dd>{{run.recorded_at_utc||'未记录'}}</dd><dt>云端响应观察 UTC</dt><dd>{{run.cloud_observed_at_utc||'未记录'}}</dd><template v-if="run.error_code"><dt>错误分类</dt><dd>{{run.error_code}}</dd></template></template></dl>
  <p v-if="!run" class="notice">该轮次不在当前列表页，未读取目录和起止元数据；下方只展示此运行 ID 的事件。</p>
  <p class="muted">中断和缺失结束时间不代表成功。时间保留记录原值，事件不证明当前网盘已重新核对。</p>
  <button class="button secondary" data-testid="monitor-export" :disabled="exporting||authenticationRequired" @click="exportRun">{{exporting?'正在导出…':'导出本轮 JSON'}}</button><p role="status" aria-live="polite">{{exportMessage}}</p><p v-if="exportError" role="alert" class="notice danger">{{exportError}}</p>
  <h4>本轮事件</h4><p v-if="error" class="notice danger" role="alert">{{error}} · 保留上次成功读取的事件</p><p v-if="loading" class="muted">正在读取事件…</p>
  <p v-if="!loading&&!error&&!events.length" class="muted">没有已记录事件</p>
  <ol class="timeline"><li v-for="event in events" :key="runID+':'+event.sequence"><strong>{{Object.hasOwn(eventLabels,event.kind)?eventLabels[event.kind]:event.kind}}</strong><time>本机记录 UTC：{{event.recorded_at_utc||'未记录'}}</time><time v-if="event.cloud_observed_at_utc">云端响应观察 UTC：{{event.cloud_observed_at_utc}}</time><details class="event-evidence"><summary>查看事件证据</summary><dl class="event-detail-fields"><dt>事件代码</dt><dd>{{event.kind}}</dd><dt>事件序列</dt><dd>{{event.sequence}}</dd><dt>错误分类</dt><dd>{{event.error_code||'未记录'}}</dd></dl></details></li></ol>
  <div class="drawer-actions"><button class="button secondary" :disabled="loading||authenticationRequired||pageIndex===0" @click="navigate(-1)">较新事件</button><span>第 {{pageIndex+1}} 页</span><button class="button secondary" data-testid="monitor-event-next" :disabled="loading||authenticationRequired||!next" @click="navigate(1)">更早事件</button></div>
 </section></div>
</template>
