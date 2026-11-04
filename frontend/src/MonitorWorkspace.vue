<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue';
import {RefreshCw,Search,AlertCircle} from 'lucide-vue-next';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import MonitorRunDetails from './MonitorRunDetails.vue';
import {validEventID as validSequence} from './event-route';
import {emptyMonitorFilters as empty,readMonitorRoute,validateMonitorRoute,monitorRouteURL} from './monitor-route';
type Run={sequence:string;run_id:string;status:string;monitor_path:string;started_at_utc:string;finished_at_utc?:string;cloud_observed_at_utc?:string;recorded_at_utc:string;error_code?:string};
const labels:Record<string,string>={running:'正在运行',success:'成功',failed:'失败',interrupted:'进程中断（未判定成功）'};
const initial=readMonitorRoute(location.search);
const authenticationRequired=ref(false);
const draft=ref({...initial.route.filters}),applied=ref({...initial.route.filters}),runs=ref<Run[]|null>(null),loading=ref(false),error=ref(''),inputError=ref(initial.error);
const cursor=ref(initial.route.before),requestedCursor=ref(initial.route.before),selectedRunID=ref(initial.route.run),nextCursor=ref<string|null>(null),cursors=ref(initial.route.before?['',initial.route.before]:['']),pageIndex=ref(initial.route.before?1:0),displayedFilters=ref<ReturnType<typeof empty>|null>(null);
const showDetails=computed(()=>!!selectedRunID.value&&!loading.value&&!inputError.value&&!authenticationRequired.value&&(runs.value!==null||!!error.value));
let generation=0,controller:AbortController|undefined;
function writeRoute(mode:'push'|'replace') {const url=monitorRouteURL({filters:applied.value,before:requestedCursor.value,run:selectedRunID.value});if(location.pathname+location.search+location.hash!==url)history[mode==='push'?'pushState':'replaceState']({},'',url);}
writeRoute('replace');
async function load(before=cursor.value,index=pageIndex.value){
 if(inputError.value)return;
 const request=++generation;controller?.abort();controller=new AbortController();loading.value=true;
 const requested={...applied.value};
 const query=new URLSearchParams({limit:'20',sequence_format:'string'});for(const [key,value] of Object.entries(requested))if(value)query.set(key,value);if(before)query.set('before_sequence',before);
 try{
  const response=await fetch('/api/monitor-scans?'+query,{signal:controller.signal,credentials:'same-origin'});
  if(request!==generation)return;
  if(response.status===401){authenticationRequired.value=true;location.assign('/login?next='+encodeURIComponent(location.pathname+location.search));throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'监控历史暂不可读');
  const result=await response.json();if(request!==generation)return;
  if(!Array.isArray(result.runs)||result.runs.length>20||!result.runs.every((run:Run)=>typeof run.sequence==='string'&&validSequence(run.sequence)&&(!before||BigInt(run.sequence)<BigInt(before))&&/^[a-f0-9]{32}$/.test(run.run_id)&&!!labels[run.status]))throw Error('监控历史响应格式无效');
  const next=result.next_before_sequence;
  if(typeof result.has_more!=='boolean'||(result.has_more&&(typeof next!=='string'||!validSequence(next)||(before&&BigInt(next)>=BigInt(before))))||(!result.has_more&&next!==null))throw Error('监控历史分页边界无效');
  runs.value=result.runs;error.value='';authenticationRequired.value=false;
  nextCursor.value=next;cursor.value=before;pageIndex.value=index;displayedFilters.value=requested;
  if(index===0)cursors.value=[''];cursors.value[index]=before;
  requestedCursor.value=before;writeRoute('replace');
 }catch(e:any){if(request===generation&&e.name!=='AbortError')error.value=e.message;}
 finally{if(request===generation)loading.value=false;}
}
function submit(){const next={filters:{...draft.value},before:'',run:''};inputError.value=validateMonitorRoute(next);if(inputError.value)return;applied.value=next.filters;requestedCursor.value='';selectedRunID.value='';writeRoute('push');load('',0);}
function clear(){draft.value=empty();submit();}
function refresh(){inputError.value=validateMonitorRoute({filters:applied.value,before:'',run:''});if(inputError.value)return;requestedCursor.value='';selectedRunID.value='';writeRoute('replace');load('',0);}
function canPage(){return !loading.value&&!inputError.value&&displayedFilters.value&&JSON.stringify(displayedFilters.value)===JSON.stringify(applied.value);}
function openRun(run:Run){selectedRunID.value=run.run_id;writeRoute('push');}
function closeRun(){selectedRunID.value='';writeRoute('push');}
function navigatePage(delta:number){if(!canPage())return;const index=pageIndex.value+delta,before=delta>0?nextCursor.value:cursors.value[index];if(index<0||before===null||before===undefined)return;requestedCursor.value=before;selectedRunID.value='';writeRoute('push');load(before,index);}
function pop(){generation++;controller?.abort();loading.value=false;const parsed=readMonitorRoute(location.search);applied.value=parsed.route.filters;draft.value={...parsed.route.filters};requestedCursor.value=parsed.route.before;selectedRunID.value=parsed.route.run;inputError.value=parsed.error;cursors.value=parsed.route.before?['',parsed.route.before]:[''];writeRoute('replace');load(parsed.route.before,parsed.route.before?1:0);}
onMounted(()=>{document.title='监控历史 · NAS Downloader';load();window.addEventListener('popstate',pop);});onUnmounted(()=>{generation++;controller?.abort();window.removeEventListener('popstate',pop);});
</script>
<template>
 <div class="workspace event-workspace monitor-workspace">
  <WorkspaceSidebar active="/monitor-history" @error="error=$event"/>
  <main class="main" :inert="showDetails?true:undefined">
   <header class="page-header"><div><span class="eyebrow">MONITORING</span><h1>监控历史</h1><p class="muted">核对每轮自动巡检与本机记录，中断不会被当作成功。</p></div><button class="button secondary" :disabled="loading" @click="refresh"><RefreshCw :size="16"/>刷新记录</button></header>
   <form class="event-filter-panel" @submit.prevent="submit"><div class="event-filter-row"><label>扫描状态<select name="status" v-model="draft.status"><option value="">全部状态</option><option v-for="(label,status) in labels" :key="status" :value="status">{{label}}</option></select></label><label>开始日期 UTC<input type="date" name="started_from_utc" v-model="draft.started_from_utc"/></label><label>结束日期 UTC<input type="date" name="started_through_utc" v-model="draft.started_through_utc"/></label><button class="button primary" :disabled="loading"><Search :size="16"/>筛选</button><button class="button secondary" type="button" :disabled="loading" @click="clear">清除</button></div></form>
   <div v-if="inputError" class="notice danger" role="alert"><AlertCircle :size="18"/>{{inputError}}</div>
   <div v-if="error" class="notice danger" role="alert"><AlertCircle :size="18"/>{{error}}<span v-if="runs">保留上次读取记录，尚未按当前条件重新核对。</span></div>
   <section class="event-results"><div class="event-result-summary" role="status" aria-live="polite">{{loading?'正在读取监控历史…':runs?`本页 ${runs.length} 轮巡检`:'尚未读取记录'}}</div>
    <div class="event-table-scroll monitor-table"><table><thead><tr><th>状态</th><th>监控目录 / 运行 ID</th><th>本机开始 UTC</th><th>本机结束 UTC</th><th>云端响应观察 UTC</th></tr></thead><tbody><tr v-for="run in runs||[]" :key="run.run_id"><td>{{labels[run.status]}}<small v-if="run.error_code" class="cell-sub">{{run.error_code}}</small></td><td><button class="event-detail-link" data-testid="monitor-details" :disabled="loading" @click="openRun(run)">{{run.monitor_path}}</button><small class="cell-sub">{{run.run_id}}</small></td><td>{{run.started_at_utc}}</td><td>{{run.finished_at_utc||'未记录'}}</td><td>{{run.cloud_observed_at_utc||'未记录'}}</td></tr></tbody></table></div>
    <p v-if="runs&&!runs.length&&!loading&&!error" class="event-empty">没有扫描记录。可以调整筛选条件后再试。</p><div class="drawer-actions"><button class="button secondary" data-testid="monitor-prev" :disabled="!canPage()||pageIndex===0" @click="navigatePage(-1)">较新记录</button><span>浏览位置 {{pageIndex+1}}</span><button class="button secondary" data-testid="monitor-next" :disabled="!canPage()||!nextCursor" @click="navigatePage(1)">更早记录</button></div><p class="muted">这里只读取本机 SQLite，不发起百度请求。结束时间缺失不代表巡检成功。</p>
   </section>
  </main>
  <MonitorRunDetails v-if="showDetails" :run-i-d="selectedRunID" :run="runs?.find(run=>run.run_id===selectedRunID)||null" @close="closeRun"/>
 </div>
</template>
