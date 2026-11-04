<script setup lang="ts">
import {onUnmounted,ref,watch} from 'vue';
import {X} from 'lucide-vue-next';
import {vModal} from './modal';
import {validDay,validEventID} from './event-route';
import {operationStatuses as statuses,operationKinds as kinds,operationUTC} from './operation-route';
type Operation={operation_id:string;object_id?:string;kind:string;status:string;source_path:string;destination_path:string;created_at_utc:string;response_observed_at_utc?:string};
type EvidenceEvent={sequence:string;status:string;recorded_at_utc:string;response_observed_at_utc?:string;evidence_json:string};
const props=defineProps<{operationId:string;operation:Operation|null}>(),emit=defineEmits<{close:[]}>();
const empty=()=>({status:'',recorded_from_utc:'',recorded_through_utc:''});
const draft=ref(empty()),applied=ref(empty()),events=ref<EvidenceEvent[]>([]),loading=ref(false),error=ref(''),inputError=ref(''),authenticationRequired=ref(false),next=ref<string|null>(null),pageIndex=ref(0),cursors=ref<(string|null)[]>([null]);
const displayedFilters=ref<ReturnType<typeof empty>|null>(null);
const exporting=ref(''),exportMessage=ref(''),exportError=ref('');
let exportGeneration=0,exportController:AbortController|undefined;
let generation=0,controller:AbortController|undefined;
function followingDay(value:string){const [year,month,day]=value.split('-').map(Number),format=(y:number,m:number,d:number)=>`${String(y).padStart(4,'0')}-${String(m).padStart(2,'0')}-${String(d).padStart(2,'0')}`;const candidate=format(year,month,day+1);return validDay(candidate)?candidate:month<12?format(year,month+1,1):format(year+1,1,1);}
function validate(){const f=draft.value;if(f.status&&!Object.keys(statuses).includes(f.status))return '事件状态无效。';for(const value of [f.recorded_from_utc,f.recorded_through_utc])if(value&&(!validDay(value)||!operationUTC(value+'T00:00:00Z')))return '事件UTC日期无效或超出存储范围。';if(f.recorded_through_utc&&!operationUTC(followingDay(f.recorded_through_utc)+'T00:00:00Z'))return '事件截止日期超出存储范围。';if(f.recorded_from_utc&&f.recorded_through_utc&&f.recorded_from_utc>f.recorded_through_utc)return '事件起始日不能晚于结束日。';return '';}
async function loadEvents(before:string|null=null,index=0){
 if(loading.value||authenticationRequired.value)return;
 const request=++generation,id=props.operationId;controller?.abort();controller=new AbortController();loading.value=true;
 const query=new URLSearchParams({limit:'20',identity_format:'string'});for(const [key,value] of Object.entries(applied.value))if(value)query.set(key,value);if(before)query.set('before_sequence',before);
 try{
  const response=await fetch('/api/cloud/operations/'+encodeURIComponent(id)+'/events?'+query,{signal:controller.signal,credentials:'same-origin'});if(request!==generation||id!==props.operationId)return;
  if(response.status===401){authenticationRequired.value=true;location.assign('/login?next='+encodeURIComponent(location.pathname+location.search));throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'操作事件暂不可读');const data=await response.json();if(request!==generation||id!==props.operationId)return;
  if(!Array.isArray(data.events)||data.events.length>20||!data.events.every((event:EvidenceEvent,i:number,rows:EvidenceEvent[])=>typeof event.sequence==='string'&&validEventID(event.sequence)&&Object.keys(statuses).includes(event.status)&&typeof event.evidence_json==='string'&&(!before||BigInt(event.sequence)<BigInt(before))&&(!i||BigInt(event.sequence)<BigInt(rows[i-1].sequence))))throw Error('操作事件身份或排序无效');
  const boundary=data.next_before_sequence,last=data.events.at(-1);if(boundary!==null&&(typeof boundary!=='string'||!validEventID(boundary)||!last||last.sequence!==boundary||(before&&BigInt(boundary)>=BigInt(before))))throw Error('操作事件游标无效');
  events.value=data.events;displayedFilters.value={...applied.value};next.value=boundary;pageIndex.value=index;if(index===0)cursors.value=[null];cursors.value[index]=before;error.value='';
 }catch(e:any){if(request===generation&&id===props.operationId&&e.name!=='AbortError')error.value=e.message;}
 finally{if(request===generation&&id===props.operationId)loading.value=false;}
}
function submit(){if(loading.value||authenticationRequired.value)return;inputError.value=validate();if(inputError.value)return;cancelExport();applied.value={...draft.value};loadEvents(null,0);}
function clear(){draft.value=empty();submit();}
function canPage(){return !loading.value&&!authenticationRequired.value&&!inputError.value&&displayedFilters.value&&JSON.stringify(displayedFilters.value)===JSON.stringify(applied.value);}
function navigate(delta:number){if(!canPage())return;const index=pageIndex.value+delta,before=delta>0?next.value:cursors.value[index];if(index<0||before===undefined||(delta>0&&!before))return;loadEvents(before,index);}
function localSummary(text:string){
 // Only known string fields become prose; numeric evidence is displayed from
 // the untouched JSON text below, never re-serialized from this parsed object.
 let value:any;try{value=JSON.parse(text);}catch{return '';}
 const labels:Record<string,string>={local_cache_recovery_complete:'本地缓存恢复完成：只确认受影响目录已标为 stale，不表示任务队列已同步。',local_reconciliation_complete:'本地同步完成：缓存处理、队列身份处理和监控目录核对均已完成。',confirmed_cache_recovery:'已确认操作的缓存恢复：重启后将受影响目录标为 stale。',unknown_cache_recovery:'unknown 操作缓存恢复：重启后重试目录失效，unknown 隔离仍保留。',unknown_result_persistence_failed:'远端结果未知：unknown 状态未能写入 SQLite，来源已在当前进程隔离；启动恢复会重试。'};
 if(!value||typeof value!=='object')return '';let summary=typeof value.event==='string'&&Object.hasOwn(labels,value.event)?labels[value.event]:'';if(value.event==='cache_refresh_failed')summary='本地缓存增量更新失败'+(typeof value.reason==='string'?'：'+value.reason:'。');if(Array.isArray(value.invalidated_cache_paths)){const paths=value.invalidated_cache_paths.filter((path:unknown)=>typeof path==='string');if(paths.length)summary+='\n影响缓存路径：'+paths.join('、');}return summary;
}
function objectID(value:unknown){return typeof value==='string'&&validEventID(value,true)?value==='0'?'未记录有效身份（0）':value:'未提供精确对象ID';}
function cancelExport(){exportGeneration++;exportController?.abort();exporting.value='';exportMessage.value='';exportError.value='';}
async function exportEvidence(filtered:boolean){
 if(exporting.value||authenticationRequired.value||(filtered&&(!canPage()||error.value)))return;
 const id=props.operationId,request=++exportGeneration;exportController=new AbortController();exporting.value=filtered?'filtered':'all';exportError.value='';exportMessage.value='正在读取脱敏证据…';
 const query=new URLSearchParams();if(filtered)for(const [key,value] of Object.entries(applied.value))if(value)query.set(key,value);
 const endpoint='/api/cloud/operations/'+encodeURIComponent(id)+(filtered?'/events/export':'/export')+(query.size?'?'+query:'');
 try{
  const response=await fetch(endpoint,{signal:exportController.signal,credentials:'same-origin'});if(request!==exportGeneration||id!==props.operationId)return;
  if(response.status===401){authenticationRequired.value=true;location.assign('/login?next='+encodeURIComponent(location.pathname+location.search));throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'证据导出失败');const blob=await response.blob();if(request!==exportGeneration||id!==props.operationId)return;
  const objectURL=URL.createObjectURL(blob),link=document.createElement('a');link.href=objectURL;link.download=(filtered?'remote-operation-events-':'remote-operation-')+id+'.json';document.body.append(link);link.click();link.remove();window.setTimeout(()=>URL.revokeObjectURL(objectURL),60000);exportMessage.value=(filtered?'筛选事件证据':'整笔操作证据')+'已开始下载。';
 }catch(e:any){if(request===exportGeneration&&id===props.operationId&&e.name!=='AbortError'){exportMessage.value='';exportError.value='导出失败：'+e.message;}}
 finally{if(request===exportGeneration&&id===props.operationId)exporting.value='';}
}
watch(()=>props.operationId,()=>{generation++;controller?.abort();cancelExport();loading.value=false;authenticationRequired.value=false;draft.value=empty();applied.value=empty();displayedFilters.value=null;events.value=[];error.value='';inputError.value='';next.value=null;pageIndex.value=0;cursors.value=[null];loadEvents();},{immediate:true});
onUnmounted(()=>{generation++;controller?.abort();cancelExport();});
</script>
<template>
 <div class="modal-backdrop" @click.self="emit('close')"><section class="detail-drawer" role="dialog" aria-modal="true" aria-labelledby="operation-detail-title" v-modal="()=>emit('close')">
  <header><h2 id="operation-detail-title">操作详情</h2><button class="icon-button" aria-label="关闭操作详情" @click="emit('close')"><X/></button></header>
  <h3>{{operation?kinds[operation.kind]:'历史操作'}}</h3><p class="notice">结果未知持续隔离；此页不重发、不放行。记录不证明当前云端内容已重新核对。</p>
  <dl class="event-detail-fields"><dt>操作 ID</dt><dd>{{operationId}}</dd><template v-if="operation"><dt>记录结果</dt><dd>{{statuses[operation.status]}}</dd><dt>对象 ID</dt><dd>{{objectID(operation.object_id)}}</dd><dt>来源</dt><dd>{{operation.source_path||'未记录'}}</dd><dt>目标</dt><dd>{{operation.destination_path||'无目标'}}</dd><dt>创建 UTC</dt><dd>{{operation.created_at_utc}}</dd><dt>响应观察 UTC</dt><dd>{{operation.response_observed_at_utc||'未观察到'}}</dd></template></dl>
  <p v-if="!operation" class="muted">该操作不在当前列表页，未读取摘要元数据；只展示此操作的事件，不借用其他操作字段。</p>
  <button class="button secondary" data-testid="operation-full-export" :disabled="!!exporting||authenticationRequired" @click="exportEvidence(false)">导出整笔证据 JSON</button><p role="status" aria-live="polite">{{exportMessage}}</p><p v-if="exportError" role="alert" class="notice danger">{{exportError}}</p>
  <h4>事件筛选</h4><form class="event-filter-panel" @submit.prevent="submit"><div class="event-filter-row"><label>事件状态<select name="event_status" v-model="draft.status"><option value="">全部状态</option><option v-for="(label,status) in statuses" :key="status" :value="status">{{label}}</option></select></label><label>记录 UTC 起始日<input type="date" v-model="draft.recorded_from_utc"/></label><label>记录 UTC 结束日<input type="date" v-model="draft.recorded_through_utc"/></label><button class="button primary" :disabled="loading||authenticationRequired">筛选事件</button><button class="button secondary" type="button" :disabled="loading||authenticationRequired" @click="clear">清除</button></div></form>
  <p v-if="inputError" class="notice danger" role="alert">{{inputError}}</p><p v-if="error" class="notice danger" role="alert">{{error}} · 保留上次读取的事件</p><p v-if="loading" class="muted">正在读取事件…</p><p v-if="!loading&&!error&&!events.length" class="muted">当前条件没有已记录事件</p>
  <button class="button secondary" data-testid="operation-filtered-export" :disabled="!!exporting||!canPage()||!!error" @click="exportEvidence(true)">导出筛选事件证据</button>
  <ol class="timeline"><li v-for="event in events" :key="operationId+':'+event.sequence"><strong>{{statuses[event.status]}} · #{{event.sequence}}</strong><time>本机记录 UTC：{{event.recorded_at_utc}}</time><time v-if="event.response_observed_at_utc">响应观察 UTC：{{event.response_observed_at_utc}}</time><p v-if="localSummary(event.evidence_json)" class="muted">{{localSummary(event.evidence_json)}}</p><details class="event-evidence"><summary>查看精确脱敏证据</summary><pre>{{event.evidence_json}}</pre></details></li></ol>
  <div class="drawer-actions"><button class="button secondary" :disabled="!canPage()||pageIndex===0" @click="navigate(-1)">较新事件</button><span>第 {{pageIndex+1}} 页</span><button class="button secondary" data-testid="operation-event-next" :disabled="!canPage()||!next" @click="navigate(1)">更早事件</button></div>
 </section></div>
</template>
