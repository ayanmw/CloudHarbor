<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {historyAPIBase,historyRunID,historyUTC,type HistorySource} from './cloud-history-types';
import {historyFilterQuery,type HistoryFilters} from './cloud-history-route';
const props=defineProps<{source:HistorySource;run?:string;filters?:HistoryFilters;disabled:boolean}>(),emit=defineEmits<{authenticationRequired:[]}>();
const loading=ref(false),result=ref(''),active=ref(true);
const supported=computed(()=>!!props.run||props.source!=='scan');
const url=computed(()=>{const base=historyAPIBase(props.source)+'/runs/';if(props.run)return base+encodeURIComponent(props.run)+'/export';const query=props.filters?historyFilterQuery(props.filters):new URLSearchParams();return base+'export'+(query.size?'?'+query:'');});
const limit=computed(()=>props.run?(props.source==='refresh'?100:10000):(props.source==='refresh'?5000:10000));
let generation=0,controller:AbortController|undefined;
function reset(){generation++;controller?.abort();loading.value=false;result.value='';}
watch(url,reset,{flush:'sync'});watch(()=>props.disabled,value=>{if(value)reset();},{flush:'sync'});
function stop(){active.value=false;reset();}
function resume(event:PageTransitionEvent){if(event.persisted)active.value=true;}
async function download(){
 if(!active.value||props.disabled||loading.value||!supported.value)return;
 const request=++generation,run=props.run,source=props.source,filters=props.filters?historyFilterQuery({...props.filters}):new URLSearchParams(),cap=limit.value;controller=new AbortController();loading.value=true;result.value='';
 try{const response=await fetch(url.value,{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){stop();emit('authenticationRequired');return;}
  const raw=await response.text();if(request!==generation)return;if(!response.ok)throw Error(raw||'导出失败，未生成文件');
  const data=JSON.parse(raw);let valid=historyUTC(data?.exported_at_utc);
  if(run){valid=valid&&data?.run?.run_id===run&&Array.isArray(data.events)&&Number.isSafeInteger(data.total_events)&&data.total_events>=0&&data.total_events<=cap&&data.events.length===data.total_events&&data.events.every((event:any)=>event.run_id===run);}
  else{valid=valid&&data?.filter&&['status','started_from_utc','started_through_utc'].every(key=>(data.filter[key]||'')===(filters.get(key)||''))&&Array.isArray(data.runs)&&Number.isSafeInteger(data.total_runs)&&data.total_runs>=0&&data.total_runs<=cap&&data.runs.length===data.total_runs&&data.runs.every((row:any)=>historyRunID(row.run_id));}
  if(!valid)throw Error('导出内容与所选运行或筛选条件不一致，未生成文件');
  // Parse only to check scope; preserve original evidence bytes and int64 precision.
  const link=document.createElement('a'),blobURL=URL.createObjectURL(new Blob([raw],{type:'application/json'}));link.href=blobURL;link.download=run?'cloud-'+source+'-'+run+'.json':'cloud-'+source+'-history.json';document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(blobURL),1000);result.value='已请求下载导出文件。';
 }catch(e:any){if(request===generation)result.value=e.message||'导出失败';}finally{if(request===generation)loading.value=false;}
}
onMounted(()=>{window.addEventListener('pagehide',stop);window.addEventListener('pageshow',resume);});onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',resume);});
</script>
<template><section data-testid="history-export-panel"><button v-if="supported" class="button secondary" data-testid="history-export-start" :disabled="disabled||loading||!active" @click="download">{{loading?'正在导出…':run?'导出此轮完整证据':'导出已应用筛选的摘要'}}</button><p class="muted">{{supported?(run?'完整证据最多 '+limit+' 条事件。':'按已应用条件导出，最多 '+limit+' 条摘要。')+'超限不生成部分文件。':'全盘扫描暂仅支持从运行详情导出单轮证据。'}}</p><p v-if="result" role="status">{{result}}</p></section></template>
