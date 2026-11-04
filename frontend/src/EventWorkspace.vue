<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { Search, RefreshCw, Download, AlertCircle, X } from 'lucide-vue-next';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import { vModal } from './modal';
import { emptyEventFilters, readEventRoute, validateEventRoute, eventRouteURL, eventRouteParams, validEventID, type EventRoute } from './event-route';
const kindLabels: Record<string,string> = {
 authentication_wait_entered:'等待百度授权', cloud_file_observed:'扫描发现网盘文件', download_attempt_finished:'下载尝试结束', download_attempt_started:'下载尝试开始', download_destination_recorded:'保存下载目标', job_completed:'任务完成', job_registered:'任务登记', job_state_saved:'任务状态保存', legacy_import:'旧队列导入', source_operation_sending:'准备处理网盘源文件', source_identity_changed:'网盘源文件身份变化', source_operation_confirmed:'网盘源文件操作已确认', source_operation_retryable_failure:'网盘源文件操作可重试失败', source_operation_unknown:'网盘源文件操作结果未知', startup_recovery:'启动恢复', status_transition:'任务状态变化', task_cancelled:'取消任务', task_cloud_identity_updated:'网盘文件身份更新', task_paused:'暂停任务', task_recovered_after_scan:'扫描后恢复任务', task_resumed:'继续任务', task_retry_requested:'手动请求重试', task_source_deleted_remotely:'网盘源文件已删除', task_source_missing:'扫描未发现网盘源文件',
};
type Event = {sequence:string;job_id:string;kind:string;recorded_at_utc:string;occurred_at_utc?:string;cloud_observed_at_utc?:string;response_observed_at_utc?:string;attempt_id?:string;operation_id?:string;run_id?:string;evidence?:unknown};
type Page = {events:Event[];total_matches:number;as_of_sequence:string;has_more:boolean;next_before_sequence:string|null;kind_counts:{kind:string;count:number}[]};
const initial=readEventRoute(location.search,Object.keys(kindLabels));
const route=ref<EventRoute>(initial.route),draft=ref({...initial.route.filters}),page=ref<Page|null>(null),loading=ref(false),error=ref(''),inputError=ref(initial.error);
const displayedRoute=ref<EventRoute|null>(null), cursors=ref<string[]>(initial.route.before?['',initial.route.before]:['']), pageIndex=ref(initial.route.before?1:0);
const selected=ref<Event|null>(null);
const exporting=ref(false),exportMessage=ref(''),exportError=ref('');
let exportGeneration=0,exportController:AbortController|undefined;
let generation=0, controller:AbortController|undefined;
function writeRoute(mode:'push'|'replace') {const url=eventRouteURL(route.value);if(location.pathname+location.search+location.hash!==url)history[mode==='push'?'pushState':'replaceState']({},'',url);}
writeRoute('replace');
async function load(index=pageIndex.value) {
 if(inputError.value)return;
 cancelExport();
 selected.value=null;
 const request=++generation; controller?.abort(); controller=new AbortController(); loading.value=true;
 const requested={...route.value,filters:{...route.value.filters}};
 const query=eventRouteParams(requested);query.set('limit','20');
 try {
  const response=await fetch('/api/task-events?'+query,{signal:controller.signal,credentials:'same-origin'});
  if(response.status===401){location.assign('/login?next='+encodeURIComponent(location.pathname+location.search));throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'本机事件暂不可读');
  const result=await response.json();
  if(request!==generation)return;
  if(!Array.isArray(result.events)||typeof result.as_of_sequence!=='string'||!validEventID(result.as_of_sequence,true))throw Error('事件响应格式无效，请刷新重试');
  if(requested.asOf&&result.as_of_sequence!==requested.asOf)throw Error('事件快照已变化，请显式刷新记录。');
  const next=result.next_before_sequence;
  if(typeof result.has_more!=='boolean'||(result.has_more&&(typeof next!=='string'||!validEventID(next)||BigInt(next)>BigInt(result.as_of_sequence)||(requested.before&&BigInt(next)>=BigInt(requested.before))))||(!result.has_more&&next!==null))throw Error('事件分页边界无效，请刷新重试');
  if(result.events.length>20||!result.events.every((event:Event)=>typeof event.sequence==='string'&&validEventID(event.sequence)&&BigInt(event.sequence)<=BigInt(result.as_of_sequence)&&(!requested.before||BigInt(event.sequence)<BigInt(requested.before))&&typeof event.job_id==='string'&&validEventID(event.job_id)))throw Error('事件身份或页边界无效，请刷新重试');
  route.value={...requested,asOf:result.as_of_sequence};writeRoute('replace');
  displayedRoute.value={...route.value,filters:{...route.value.filters}};
  pageIndex.value=index;cursors.value[index]=requested.before;
  page.value=result;error.value='';
 }catch(e:any){if(request===generation&&e.name!=='AbortError')error.value=e.message;}
 finally{if(request===generation)loading.value=false;}
}
function submit(){const next={filters:{...draft.value},asOf:'',before:''};inputError.value=validateEventRoute(next,Object.keys(kindLabels));if(inputError.value)return;route.value=next;cursors.value=[''];writeRoute('push');load(0);}
function refresh(){const next={filters:{...route.value.filters},asOf:'',before:''};inputError.value=validateEventRoute(next,Object.keys(kindLabels));if(inputError.value)return;route.value=next;cursors.value=[''];writeRoute('replace');load(0);}
function canPage(){return !loading.value&&!inputError.value&&displayedRoute.value&&JSON.stringify(displayedRoute.value.filters)===JSON.stringify(route.value.filters)&&displayedRoute.value.asOf===route.value.asOf;}
function navigatePage(delta:number){
 if(!canPage()||!displayedRoute.value||!page.value)return;
 const index=pageIndex.value+delta;
 const before=delta>0?page.value.next_before_sequence:cursors.value[index];
 if(index<0||before===null||before===undefined)return;
 route.value={...displayedRoute.value,before};writeRoute('push');load(index);
}
function clear(){draft.value=emptyEventFilters();submit();}
function selectKind(kind:string){draft.value={...route.value.filters,kind};submit();}
function filterCorrelation(key:'task_id'|'attempt_id'|'operation_id') {
 const event=selected.value;if(!event||!displayedRoute.value)return;
 const value=key==='task_id'?event.job_id:event[key];if(!value)return;
 draft.value={...displayedRoute.value.filters,task_id:'',attempt_id:'',operation_id:'',[key]:value};
 selected.value=null;submit();
}
function cancelExport(){exportGeneration++;exportController?.abort();exporting.value=false;exportMessage.value='';exportError.value='';}
async function exportEvidence(){
 if(exporting.value||loading.value||error.value||inputError.value||!displayedRoute.value)return;
 const request=++exportGeneration;exportController=new AbortController();exporting.value=true;exportError.value='';exportMessage.value='正在导出当前快照的脱敏证据…';
 const query=eventRouteParams({...displayedRoute.value,before:''});
 try{
  const response=await fetch('/api/task-events/export?'+query,{signal:exportController.signal,credentials:'same-origin'});
  if(request!==exportGeneration)return;
  if(response.status===401){location.assign('/login?next='+encodeURIComponent(location.pathname+location.search));throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'导出请求失败');
  const blob=await response.blob();if(request!==exportGeneration)return;
  const disposition=response.headers.get('Content-Disposition')||'',match=disposition.match(/filename="(task-events-global-[A-Za-z0-9._-]+\.json)"/i);
  const objectURL=URL.createObjectURL(blob),link=document.createElement('a');link.href=objectURL;link.download=match?match[1]:'task-events-global-evidence.json';document.body.append(link);link.click();link.remove();window.setTimeout(()=>URL.revokeObjectURL(objectURL),60000);
  exportMessage.value='当前快照证据已开始下载。';
 }catch(e:any){if(request===exportGeneration&&e.name!=='AbortError'){exportMessage.value='';exportError.value='导出失败：'+e.message;}}
 finally{if(request===exportGeneration)exporting.value=false;}
}
function pop(){cancelExport();selected.value=null;const parsed=readEventRoute(location.search,Object.keys(kindLabels));generation++;controller?.abort();loading.value=false;route.value=parsed.route;draft.value={...parsed.route.filters};inputError.value=parsed.error;cursors.value=parsed.route.before?['',parsed.route.before]:[''];writeRoute('replace');load(parsed.route.before?1:0);}
onMounted(()=>{document.title='任务事件 · NAS Downloader';load();window.addEventListener('popstate',pop);});onUnmounted(()=>{generation++;controller?.abort();cancelExport();window.removeEventListener('popstate',pop);});
</script>

<template>
 <div class="workspace event-workspace">
  <WorkspaceSidebar active="/task-events" @error="error=$event"/>
  <main class="main" :inert="selected?true:undefined">
   <header class="page-header"><div><span class="eyebrow">ACTIVITY</span><h1>任务事件</h1><p class="muted">追踪下载任务的状态变化，按记录时间核对本机证据。</p></div><button class="button secondary" :disabled="loading" @click="refresh"><RefreshCw :size="16"/>刷新记录</button></header>
   <form class="event-filter-panel" @submit.prevent="submit">
    <div class="event-filter-row"><label>任务 ID<input name="task_id" v-model="draft.task_id" inputmode="numeric" placeholder="全部任务"/></label><label>事件类型<select name="kind" v-model="draft.kind"><option value="">全部类型</option><option v-for="(label,kind) in kindLabels" :key="kind" :value="kind">{{label}}</option></select></label><button class="button primary" :disabled="loading"><Search :size="16"/>筛选</button><button class="button secondary" type="button" :disabled="loading" @click="clear">清除</button></div>
    <details class="event-advanced"><summary>高级筛选 · 关联 ID 与记录 UTC 范围</summary><div class="event-filter-row">
     <label>远端操作 ID<input name="operation_id" v-model="draft.operation_id" maxlength="128"/></label><label>下载尝试 ID<input name="attempt_id" v-model="draft.attempt_id" maxlength="128"/></label>
     <label>记录 UTC 起始日<input name="from_utc" type="date" v-model="draft.from_utc"/></label><label>记录 UTC 截止日<input name="through_utc" type="date" v-model="draft.through_utc"/></label>
     <label>记录 UTC 精确起点<input name="recorded_from_utc" v-model="draft.recorded_from_utc" placeholder="2026-11-03T00:00:00.000001Z"/></label><label>记录 UTC 精确截止（不含）<input name="recorded_before_utc" v-model="draft.recorded_before_utc" placeholder="RFC3339 UTC"/></label>
    </div><p class="muted">日期范围与精确范围二选一。精确时间按原字符串提交，保留小数秒精度。</p></details>
   </form>
   <div class="event-export-tools"><button class="button secondary" data-testid="event-export" :disabled="exporting||loading||!!error||!!inputError||!displayedRoute" @click="exportEvidence"><Download :size="16"/>{{exporting?'正在导出…':'导出当前筛选快照'}}</button><span class="muted" role="status" aria-live="polite">{{exportMessage}}</span><span v-if="exportError" class="notice danger" role="alert">{{exportError}}</span></div>
   <div v-if="inputError" class="notice danger" role="alert"><AlertCircle :size="18"/>{{inputError}}</div>
   <div v-if="error" class="notice danger" role="alert"><AlertCircle :size="18"/>{{error}}<span v-if="page">保留上次读取的记录，尚未按当前条件重新核对。</span></div>
   <section class="event-results">
    <div class="event-result-summary" role="status" aria-live="polite">{{loading?'正在读取本机事件…':page?`匹配 ${page.total_matches} 条 · 本页 ${page.events.length} 条`:'尚未读取记录'}}<span v-if="page" class="muted">快照序列 {{page.as_of_sequence}}</span></div>
    <div v-if="page?.kind_counts?.length" class="event-kind-counts" aria-label="事件类型命中数"><button v-for="item in page.kind_counts" :key="item.kind" type="button" :disabled="loading" @click="selectKind(item.kind)">{{kindLabels[item.kind]||item.kind}} <b>{{item.count}}</b></button></div>
    <div class="event-table-scroll"><table><thead><tr><th>事件类型</th><th>任务 ID</th><th>本机记录 UTC</th><th>事件序列</th></tr></thead><tbody><tr v-for="event in page?.events||[]" :key="event.sequence"><td><button class="event-detail-link" data-testid="event-details" :disabled="loading" @click="selected=event">{{kindLabels[event.kind]||event.kind}}</button></td><td>{{event.job_id}}</td><td>{{event.recorded_at_utc}}</td><td>{{event.sequence}}</td></tr></tbody></table></div>
    <p v-if="page&&!page.events.length&&!loading&&!error" class="event-empty">没有事件记录。可以调整筛选条件后再试。</p>
    <div class="drawer-actions"><button class="button secondary" data-testid="event-prev" :disabled="!canPage()||pageIndex===0" @click="navigatePage(-1)">{{pageIndex===1?'返回当前快照首页':'较新记录'}}</button><span>当前浏览位置 {{pageIndex+1}}</span><button class="button secondary" data-testid="event-next" :disabled="!canPage()||!page?.has_more" @click="navigatePage(1)">更早记录</button></div>
    <p class="muted">本页只读本机记录，不调用百度 API。记录时间与云端发生、观察时间是不同证据。</p>
   </section>
  </main>
  <div v-if="selected" class="modal-backdrop" @click.self="selected=null">
   <section class="detail-drawer" role="dialog" aria-modal="true" aria-labelledby="event-detail-title" v-modal="()=>selected=null">
    <header><h2 id="event-detail-title">事件详情</h2><button class="icon-button" aria-label="关闭事件详情" @click="selected=null"><X/></button></header>
    <h3>{{kindLabels[selected.kind]||selected.kind}}</h3>
    <p class="muted">已读取快照中的本机记录，不代表当前网盘或本地文件已重新核对。</p>
    <dl class="event-detail-fields">
     <dt>事件序列</dt><dd>{{selected.sequence}}</dd><dt>任务 ID</dt><dd>{{selected.job_id}}</dd>
     <dt>本机记录 UTC</dt><dd>{{selected.recorded_at_utc||'未记录'}}</dd>
     <template v-if="selected.occurred_at_utc"><dt>事件发生 UTC</dt><dd>{{selected.occurred_at_utc}}</dd></template>
     <template v-if="selected.cloud_observed_at_utc"><dt>云端观察 UTC</dt><dd>{{selected.cloud_observed_at_utc}}</dd></template>
     <template v-if="selected.response_observed_at_utc"><dt>响应观察 UTC</dt><dd>{{selected.response_observed_at_utc}}</dd></template>
     <template v-if="selected.attempt_id"><dt>下载尝试 ID</dt><dd>{{selected.attempt_id}}</dd></template>
     <template v-if="selected.operation_id"><dt>远端操作 ID</dt><dd>{{selected.operation_id}}</dd></template>
     <template v-if="selected.run_id"><dt>运行 ID</dt><dd>{{selected.run_id}}</dd></template>
    </dl>
    <div class="drawer-actions"><button class="button secondary" @click="filterCorrelation('task_id')">同任务</button><button v-if="selected.attempt_id" class="button secondary" data-testid="filter-attempt" @click="filterCorrelation('attempt_id')">同下载尝试</button><button v-if="selected.operation_id" class="button secondary" @click="filterCorrelation('operation_id')">同远端操作</button></div>
    <p class="muted">切换关联维度时清除其他关联 ID，保留事件类型和时间条件。</p>
    <details class="event-evidence" :key="selected.sequence"><summary>查看脱敏证据</summary><pre v-if="selected.evidence">{{JSON.stringify(selected.evidence,null,2)}}</pre><p v-else class="muted">未记录额外证据</p></details>
   </section>
  </div>
 </div>
</template>
