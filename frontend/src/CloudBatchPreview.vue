<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import type {CloudFileEntry} from './cloud-files-types';
import {parseBatchPreview,previewExpected,previewTarget,previewTime,type PreviewRequest,type PreviewRow,type PreviewMatch,type PreviewNameConflict} from './batch-preview';
const props=defineProps<{items:CloudFileEntry[];operation:'move'|'copy';destination:string;disabled:boolean}>();
const emit=defineEmits<{ready:[boolean];authenticationRequired:[]}>();
type Snapshot={destination:string;started:string;observed:string;rows:PreviewRow[];matches:PreviewMatch[];conflicts:PreviewNameConflict[]};
const loading=ref(false),fresh=ref(false),auth=ref(false),error=ref(''),progress=ref(''),snapshot=ref<Snapshot|null>(null);
let active=true,generation=0,serial=0,controller:AbortController|undefined;
const blocked=computed(()=>!!snapshot.value&&(snapshot.value.rows.some(row=>row.status!=='matched')||snapshot.value.matches.length>0||snapshot.value.conflicts.length>0));
function revoke(){generation++;controller?.abort();loading.value=false;fresh.value=false;emit('ready',false);}
function invalidate(){revoke();snapshot.value=null;error.value='';progress.value='';}
watch([()=>props.destination,()=>props.operation,()=>props.items,()=>props.disabled],invalidate,{flush:'sync'});
function cancel(){revoke();progress.value='核对已取消；未发送文件变更。';}
async function run(){
 if(!active||props.disabled||loading.value||auth.value)return;
 revoke();const requestGeneration=generation,operation=props.operation,destination=props.destination,items=props.items.map(previewExpected);controller=new AbortController();loading.value=true;error.value='';progress.value='正在核对来源身份…';
 let lastObservation='';
 const current=()=>active&&generation===requestGeneration;
 async function read(phase:'sources'|'destination',expected:typeof items,offset=0,boundary=''){
  const body:PreviewRequest={request_id:'preview-'+Date.now()+'-'+(++serial),phase,operation,destination_directory:destination,expected,offset,expected_boundary:boundary};
  const response=await fetch('/api/cloud/mutations/preview',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),signal:controller!.signal});
  if(!current())throw Error('已取消');
  if(response.status===401){auth.value=true;emit('authenticationRequired');throw Error('管理员登录已失效，请重新登录。');}
  if(!response.ok)throw Error('在线核对请求失败（HTTP '+response.status+'）；请检查授权、路径或目录变化后重新核对。');
  const value=await response.json();if(!current())throw Error('已取消');const result=parseBatchPreview(value,body);if(lastObservation&&previewTime(result.started_at_utc)<lastObservation)throw Error('服务观察 UTC 顺序异常，请重新在线核对。');lastObservation=previewTime(result.observed_at_utc);return result;
 }
 try{
  const rows:PreviewRow[]=[],matches=new Map<string,PreviewMatch>();let started='',observed='',offset=0,boundary='',pages=0,checked=0,conflicts:PreviewNameConflict[]|null=null;
  for(let index=0;index<items.length;index+=10){const result=await read('sources',items.slice(index,index+10));if(!current())return;rows.push(...result.rows);started||=result.started_at_utc;observed=result.observed_at_utc;progress.value='来源已核对 '+rows.length+' / '+items.length+' 项；正在读取目标目录…';}
  while(current()){
   const result=await read('destination',items,offset,boundary);if(!current())return;
   if(conflicts!==null&&JSON.stringify(conflicts)!==JSON.stringify(result.batch_conflicts))throw Error('固定目标名称核对结果不一致，请重新预览。');
   conflicts=result.batch_conflicts;for(const match of result.matches)if(!matches.has(match.source_id))matches.set(match.source_id,match);
   checked+=result.checked_count;pages++;observed=result.observed_at_utc;progress.value='来源已核对 '+rows.length+' / '+items.length+' 项；目标已读取 '+checked+' 项 · '+pages+' 页';
   if(result.next_offset===null)break;offset=result.next_offset;boundary=result.next_boundary;
  }
  if(!current())return;
  snapshot.value={destination,started,observed,rows,matches:[...matches.values()],conflicts:conflicts||[]};fresh.value=true;emit('ready',!blocked.value);
 }catch(e:any){if(current()){error.value=e.message||'在线预览未完成；不能确认执行。';fresh.value=false;emit('ready',false);}}
 finally{if(current())loading.value=false;}
}
function leave(){active=false;revoke();progress.value='已离页，确认前需要重新核对。';}
function restore(event:PageTransitionEvent){if(event.persisted&&!auth.value){active=true;revoke();}}
onMounted(()=>{window.addEventListener('pagehide',leave);window.addEventListener('pageshow',restore)});
onUnmounted(()=>{active=false;revoke();window.removeEventListener('pagehide',leave);window.removeEventListener('pageshow',restore)});
const source=(id:string)=>snapshot.value?.rows.find(row=>row.source_id===id);
const match=(id:string)=>snapshot.value?.matches.find(row=>row.source_id===id);
const internalConflict=(id:string)=>snapshot.value?.conflicts.some(pair=>pair.source_id===id||pair.other_source_id===id);
</script>
<template><section class="batch-online-preview" data-testid="batch-preview-panel" aria-label="批量在线冲突预览"><h3>执行前在线核对</h3><p>先核对所选来源，再分页检查目标目录。同名目标拒绝覆盖；预览不是锁，最终执行会再次在线核对。</p><div class="toolbar"><button type="button" class="button secondary" data-testid="batch-preview-start" :disabled="disabled||loading||auth" @click="run">{{loading?'正在核对…':snapshot?'重新在线核对':'开始在线核对'}}</button><button v-if="loading" type="button" class="button secondary" data-testid="batch-preview-cancel" @click="cancel">取消核对</button></div><p v-if="progress" role="status">{{progress}}{{loading?' · 尚未完整，不能执行':''}}</p><p v-if="error" role="alert" class="notice danger">{{error}} 本次没有发送文件变更。</p><template v-if="snapshot"><p v-if="!fresh" class="notice">下方是上次在线核对结果，不能据此确认执行。</p><p v-else class="notice" :class="{danger:blocked}">{{blocked?'发现来源变化或目标冲突，本批不可执行。':'整批核对完成，未发现所选目标冲突；请另行勾选确认。'}}</p><p>核对目标：{{snapshot.destination}}</p><p class="muted">本机观察 UTC：{{snapshot.started}} → {{snapshot.observed}}。这是多次在线观察，不是同一时刻的完整目录快照。</p><details :open="blocked"><summary>查看 {{items.length}} 项核对明细</summary><div class="event-table-scroll"><table><thead><tr><th>项目</th><th>来源身份</th><th>目标名称</th></tr></thead><tbody><tr v-for="item in items" :key="item.fs_id"><td>{{item.name}}<small class="cell-sub">{{item.path}}</small></td><td>{{source(item.fs_id)?.status==='matched'?'观察时一致':'来源身份变化'}}<small v-if="source(item.fs_id)?.status==='changed'" class="cell-sub">观察路径：{{source(item.fs_id)?.current_path}}</small><small class="cell-sub">{{source(item.fs_id)?.observed_at_utc}}</small></td><td>{{internalConflict(item.fs_id)?'本批目标名称冲突':match(item.fs_id)?'线上已有同名项':'未发现同名项'}}<small class="cell-sub">{{previewTarget(snapshot.destination,item.name)}}</small><template v-if="match(item.fs_id)"><small class="cell-sub">同名示例：{{match(item.fs_id)?.conflict_path}}</small><small class="cell-sub">ID {{match(item.fs_id)?.conflict_id}}</small></template></td></tr></tbody></table></div></details></template></section></template>
<style scoped>.batch-online-preview{padding:16px;margin:16px 0;border:1px solid #cbd5e1;border-radius:10px;background:#f8fafc;overflow-wrap:anywhere}.batch-online-preview h3{font-size:16px;margin:0 0 8px}.batch-online-preview p{line-height:1.6}.batch-online-preview td{white-space:normal;overflow-wrap:anywhere}.batch-online-preview summary{cursor:pointer;font-weight:600}.batch-online-preview .muted{font-size:12px}</style>
