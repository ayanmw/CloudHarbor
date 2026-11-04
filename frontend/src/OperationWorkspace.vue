<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue';
import {RefreshCw,Search,AlertCircle} from 'lucide-vue-next';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import OperationDetails from './OperationDetails.vue';
import {validEventID} from './event-route';
import {operationKinds as kinds,operationStatuses as statuses,validateOperationFilters,readOperationRoute,operationParams,operationRouteURL,validOperationID,validOperationCursor,olderOperationKey,operationUTC,type OperationCursor,type OperationFilters} from './operation-route';
type Operation={operation_id:string;object_id?:string;kind:string;status:string;source_path:string;destination_path:string;created_at_utc:string;response_observed_at_utc?:string};
const initial=readOperationRoute(location.search);
const authenticationRequired=ref(false);
const draft=ref({...initial.route.filters}),applied=ref({...initial.route.filters}),operations=ref<Operation[]|null>(null),total=ref(0),loading=ref(false),error=ref('');
const inputError=ref(initial.error),navigationError=ref(initial.error),cursor=ref<OperationCursor|null>(initial.route.before),requestedCursor=ref<OperationCursor|null>(initial.route.before),selectedOperationID=ref(initial.route.operation),nextCursor=ref<OperationCursor|null>(null),pageIndex=ref(initial.route.before?1:0),cursors=ref<(OperationCursor|null)[]>(initial.route.before?[null,initial.route.before]:[null]),displayedFilters=ref<OperationFilters|null>(null);
const showDetails=computed(()=>!!selectedOperationID.value&&!loading.value&&!navigationError.value&&!authenticationRequired.value&&(operations.value!==null||!!error.value));
const exporting=ref(false),exportMessage=ref(''),exportError=ref('');
let exportGeneration=0,exportController:AbortController|undefined;
let generation=0,controller:AbortController|undefined;
function writeRoute(mode:'push'|'replace'){const url=operationRouteURL({filters:applied.value,before:requestedCursor.value,operation:selectedOperationID.value});if(location.pathname+location.search+location.hash!==url)history[mode==='push'?'pushState':'replaceState']({},'',url);}
writeRoute('replace');
async function load(before=cursor.value,index=pageIndex.value){
 if(navigationError.value||authenticationRequired.value)return;
 cancelExport();
 const request=++generation;controller?.abort();controller=new AbortController();loading.value=true;
 const requested={...applied.value};const query=operationParams({filters:requested,before,operation:''});query.set('limit','20');query.set('identity_format','string');
 try{
  const response=await fetch('/api/cloud/operations?'+query,{signal:controller.signal,credentials:'same-origin'});if(request!==generation)return;
  if(response.status===401){authenticationRequired.value=true;location.assign('/login?next='+encodeURIComponent(location.pathname+location.search));throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'操作历史暂不可读');
  const result=await response.json();if(request!==generation)return;
  if(!Array.isArray(result.operations)||result.operations.length>20||!result.operations.every((op:Operation,i:number,rows:Operation[])=>validOperationID(op.operation_id)&&validOperationCursor(op)&&Object.keys(kinds).includes(op.kind)&&Object.keys(statuses).includes(op.status)&&(!before||olderOperationKey(op,before))&&(!i||olderOperationKey(op,rows[i-1])))||!Number.isSafeInteger(result.total)||result.total<result.operations.length)throw Error('操作历史身份或排序无效');
  const next=result.next_cursor,last=result.operations.at(-1);
  if(next!==null&&(!validOperationCursor(next)||!last||next.operation_id!==last.operation_id||operationUTC(next.created_at_utc)!==operationUTC(last.created_at_utc)||(before&&!olderOperationKey(next,before))))throw Error('操作历史组合游标无效');
  operations.value=result.operations;total.value=result.total;error.value='';authenticationRequired.value=false;
  cursor.value=before;requestedCursor.value=before;nextCursor.value=next;pageIndex.value=index;displayedFilters.value=requested;if(index===0)cursors.value=[null];cursors.value[index]=before;writeRoute('replace');
 }catch(e:any){if(request===generation&&e.name!=='AbortError')error.value=e.message;}
 finally{if(request===generation)loading.value=false;}
}
function submit(){const next={...draft.value,q:draft.value.q.trim()};inputError.value=validateOperationFilters(next);if(inputError.value)return;navigationError.value='';applied.value=next;requestedCursor.value=null;selectedOperationID.value='';writeRoute('push');load(null,0);}
function clear(){draft.value={status:'',kind:'',q:'',from:'',to:''};submit();}
function objectIdentity(value:unknown){return typeof value==='string'&&validEventID(value,true)?value==='0'?'未记录有效对象ID（0）':value:'未提供精确对象ID';}
function openOperation(operation:Operation){if(loading.value||authenticationRequired.value)return;cancelExport();selectedOperationID.value=operation.operation_id;writeRoute('push');}
function closeOperation(){selectedOperationID.value='';writeRoute('push');}
function refresh(){navigationError.value=validateOperationFilters(applied.value);if(navigationError.value){inputError.value=navigationError.value;return;}requestedCursor.value=null;selectedOperationID.value='';writeRoute('replace');load(null,0);}
function canPage(){return !loading.value&&!authenticationRequired.value&&!inputError.value&&!navigationError.value&&displayedFilters.value&&JSON.stringify(displayedFilters.value)===JSON.stringify(applied.value);}
function navigate(delta:number){if(!canPage())return;const index=pageIndex.value+delta,before=delta>0?nextCursor.value:cursors.value[index];if(index<0||before===undefined||(delta>0&&!before))return;requestedCursor.value=before;selectedOperationID.value='';writeRoute('push');load(before,index);}
function cancelExport(){exportGeneration++;exportController?.abort();exporting.value=false;exportMessage.value='';exportError.value='';}
async function exportSummary(){
 if(exporting.value||!canPage()||error.value)return;
 const request=++exportGeneration;exportController=new AbortController();exporting.value=true;exportError.value='';exportMessage.value='正在导出已提交筛选的实时摘要…';
 const query=operationParams({filters:{...applied.value},before:null,operation:''});
 try{
  const response=await fetch('/api/cloud/operations/export?'+query,{signal:exportController.signal,credentials:'same-origin'});if(request!==exportGeneration)return;
  if(response.status===401){authenticationRequired.value=true;location.assign('/login?next='+encodeURIComponent(location.pathname+location.search));throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'摘要导出失败');const blob=await response.blob();if(request!==exportGeneration)return;
  const match=(response.headers.get('Content-Disposition')||'').match(/filename="(remote-operations-[A-Za-z0-9._-]+\.json)"/i),objectURL=URL.createObjectURL(blob),link=document.createElement('a');link.href=objectURL;link.download=match?match[1]:'remote-operations-evidence.json';document.body.append(link);link.click();link.remove();window.setTimeout(()=>URL.revokeObjectURL(objectURL),60000);exportMessage.value='筛选摘要已开始下载（导出时的实时记录）。';
 }catch(e:any){if(request===exportGeneration&&e.name!=='AbortError'){exportMessage.value='';exportError.value='导出失败：'+e.message;}}
 finally{if(request===exportGeneration)exporting.value=false;}
}
function pop(){cancelExport();generation++;controller?.abort();loading.value=false;const parsed=readOperationRoute(location.search);applied.value=parsed.route.filters;draft.value={...parsed.route.filters};requestedCursor.value=parsed.route.before;selectedOperationID.value=parsed.route.operation;inputError.value=parsed.error;navigationError.value=parsed.error;cursors.value=parsed.route.before?[null,parsed.route.before]:[null];writeRoute('replace');load(parsed.route.before,parsed.route.before?1:0);}
onMounted(()=>{document.title='操作历史 · NAS Downloader';load();window.addEventListener('popstate',pop);});onUnmounted(()=>{generation++;controller?.abort();cancelExport();window.removeEventListener('popstate',pop);});
</script>
<template>
 <div class="workspace event-workspace operation-workspace">
  <WorkspaceSidebar active="/operations" @error="error=$event"/>
  <main class="main" :inert="showDetails?true:undefined">
   <header class="page-header"><div><span class="eyebrow">OPERATIONS</span><h1>操作历史</h1><p class="muted">只读核对本机记录的远端操作，不发起百度请求。</p></div><button class="button secondary" :disabled="loading" @click="refresh"><RefreshCw :size="16"/>刷新记录</button></header>
   <form class="event-filter-panel" @submit.prevent="submit"><div class="event-filter-row">
    <label>关键字<input name="q" v-model="draft.q" placeholder="操作 ID、文件 ID 或网盘路径（最多128字符）"/></label>
    <label>结果状态<select name="status" v-model="draft.status"><option value="">全部状态</option><option v-for="(label,status) in statuses" :key="status" :value="status">{{label}}</option></select></label>
    <label>操作类型<select name="kind" v-model="draft.kind"><option value="">全部类型</option><option v-for="(label,kind) in kinds" :key="kind" :value="kind">{{label}}</option></select></label>
    <button class="button primary" :disabled="loading"><Search :size="16"/>筛选</button><button class="button secondary" type="button" :disabled="loading" @click="clear">清除</button>
   </div><details class="event-advanced"><summary>高级筛选 · 创建时间范围</summary><div class="event-filter-row"><label>创建起点 UTC（含）<input name="from" v-model="draft.from" placeholder="2026-11-03T00:00:00Z"/></label><label>创建终点 UTC（含）<input name="to" v-model="draft.to" placeholder="RFC3339 UTC"/></label></div><p class="muted">起止均包含，支持最多9位小数；使用Z结尾UTC，按输入原值查询。</p></details></form>
   <div class="notice">结果未知的操作持续隔离；此页不提供重发、放行或删除记录。</div>
   <div class="event-export-tools"><button class="button secondary" data-testid="operation-summary-export" :disabled="exporting||!canPage()||!!error" @click="exportSummary">{{exporting?'正在导出…':'导出筛选摘要 JSON'}}</button><span role="status" aria-live="polite">{{exportMessage}}</span><span v-if="exportError" class="notice danger" role="alert">{{exportError}}</span></div>
   <div v-if="inputError" class="notice danger" role="alert"><AlertCircle :size="18"/>{{inputError}}</div>
   <div v-if="error" class="notice danger" role="alert"><AlertCircle :size="18"/>{{error}}<span v-if="operations">保留上次读取记录，尚未按当前条件重新核对。</span></div>
   <section class="event-results"><div class="event-result-summary" role="status" aria-live="polite">{{loading?'正在读取操作历史…':operations?`实时匹配 ${total} 笔 · 本页 ${operations.length} 笔`:'尚未读取记录'}}</div>
    <div class="event-table-scroll"><table><thead><tr><th>操作 / 结果</th><th>来源与目标</th><th>创建 UTC</th><th>响应观察 UTC</th></tr></thead><tbody><tr v-for="operation in operations||[]" :key="operation.operation_id"><td><button class="event-detail-link" data-testid="operation-details" :disabled="loading" @click="openOperation(operation)">{{kinds[operation.kind]}}</button><small class="cell-sub">{{statuses[operation.status]}}</small></td><td>{{operation.source_path||'未记录来源'}}<small class="cell-sub">{{operation.destination_path||'无目标路径'}}</small><small class="cell-sub">操作 ID：{{operation.operation_id}}</small><small class="cell-sub">对象 ID：{{objectIdentity(operation.object_id)}}</small></td><td>{{operation.created_at_utc}}</td><td>{{operation.response_observed_at_utc||'未观察到'}}</td></tr></tbody></table></div>
    <p v-if="operations&&!operations.length&&!loading&&!error" class="event-empty">没有操作记录。可以调整筛选条件后再试。</p><p class="muted">结果、路径和时间均为记录时的证据，不代表当前云端内容已重新核对。</p>
    <div class="drawer-actions"><button class="button secondary" data-testid="operation-prev" :disabled="!canPage()||pageIndex===0" @click="navigate(-1)">{{pageIndex===1?'返回筛选首页':'较新记录'}}</button><span>浏览位置 {{pageIndex+1}}</span><button class="button secondary" data-testid="operation-next" :disabled="!canPage()||!nextCursor" @click="navigate(1)">更早记录</button></div>
   </section>
  </main>
  <OperationDetails v-if="showDetails" :operation-id="selectedOperationID" :operation="operations?.find(operation=>operation.operation_id===selectedOperationID)||null" @close="closeOperation"/>
 </div>
</template>
