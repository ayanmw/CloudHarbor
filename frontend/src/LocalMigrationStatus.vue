<script setup lang="ts">
import {onMounted,onUnmounted,ref,watch} from 'vue';
const props=defineProps<{refreshKey:number;receiptId:string}>();
const emit=defineEmits<{authenticationRequired:[];changed:[]}>();
type Migration={id:string;status:string;phase:string;source_path:string;target_path:string;processed_files:string;total_files:string;processed_bytes:string;total_bytes:string;started_at_utc:string;updated_at_utc:string;finished_at_utc?:string;last_error?:string};
const record=ref<Migration|null>(null),none=ref(false),loading=ref(false),error=ref(''),observed=ref('');
const exporting=ref(false),exportResult=ref('');
let exportController:AbortController|undefined,exportGeneration=0;
async function exportEvidence(){
 if(!active||exporting.value||loading.value||error.value||!record.value)return;
 const id=record.value.id,request=++exportGeneration;exportController=new AbortController();exporting.value=true;exportResult.value='';
 try{const response=await fetch('/api/settings/local-migrations/'+encodeURIComponent(id)+'/export',{credentials:'same-origin',signal:exportController.signal});
  if(!active||request!==exportGeneration||record.value?.id!==id)return;
  if(response.status===401){stop();emit('authenticationRequired');return;}
  if(!response.ok)throw Error(await response.text()||'迁移证据导出失败');
  const text=await response.text();if(!active||request!==exportGeneration||record.value?.id!==id)return;
  // Keep raw JSON bytes intact: int64 evidence must not be parsed and reserialized as Number.
  if(JSON.parse(text)?.migration?.id!==id)throw Error('导出记录与所选迁移不一致');
  const url=URL.createObjectURL(new Blob([text],{type:'application/json'})),link=document.createElement('a');link.href=url;link.download='local-migration-'+id+'.json';document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),1000);exportResult.value='此记录证据已开始下载。';
 }catch(e:any){if(active&&request===exportGeneration&&record.value?.id===id)exportResult.value=e.message||'迁移证据导出失败';}
 finally{if(request===exportGeneration)exporting.value=false;}
}
const phases:Record<string,string>={copying:'复制文件',verifying:'核验目标内容',retiring_source:'隔离原目录',source_retired:'原目录已隔离',deleting_source:'清理原目录',complete:'完成'};
const statuses:Record<string,string>={running:'迁移中',failed:'迁移失败',completed:'迁移完成'};
let active=true,generation=0,controller:AbortController|undefined,timer:ReturnType<typeof setTimeout>|undefined,refreshPending=false;
function stop(){active=false;refreshPending=false;generation++;exportGeneration++;exportController?.abort();exporting.value=false;controller?.abort();clearTimeout(timer);loading.value=false;}
async function load(){
 if(!active||loading.value)return;clearTimeout(timer);const request=++generation;controller=new AbortController();loading.value=true;
 try{const response=await fetch('/api/settings/local-migration?counter_format=string',{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){stop();emit('authenticationRequired');return;}
  if(!response.ok)throw Error(await response.text()||'迁移状态读取失败');
  const data=await response.json();if(request!==generation)return;
  if(data?.status==='none'){record.value=null;none.value=true;}
  else{
   if(!data||!statuses[data.status]||!phases[data.phase]||typeof data.id!=='string'||!/^[a-f0-9]{32}$/.test(data.id)||!['source_path','target_path'].every(key=>typeof data[key]==='string'&&data[key])||!['processed_files','total_files','processed_bytes','total_bytes'].every(key=>typeof data[key]==='string'&&/^(0|[1-9][0-9]*)$/.test(data[key])&&data[key].length<=19&&BigInt(data[key])<=9223372036854775807n)||!['started_at_utc','updated_at_utc'].every(key=>typeof data[key]==='string'&&/Z$/.test(data[key])&&Number.isFinite(Date.parse(data[key]))))throw Error('迁移状态响应无效或计数超出精确范围');
   const previous=record.value;if(previous?.id!==data.id){exportGeneration++;exportController?.abort();exporting.value=false;exportResult.value='';}record.value=data;none.value=false;if(previous&&(previous.id!==data.id||previous.status!==data.status))emit('changed');
  }
  observed.value=new Date().toISOString();error.value='';
 }catch(e:any){if(request===generation)error.value=(e.message||'迁移状态读取失败')+(record.value?' · 下方为上次读取结果。':'');}
 finally{if(request===generation){loading.value=false;if(active&&refreshPending){refreshPending=false;void load();}else if(active&&record.value?.status==='running'&&!error.value)timer=setTimeout(load,2000);}}
}
function resume(event:PageTransitionEvent){if(event.persisted){active=true;load();}}
// A save receipt can arrive while an older read is pending; queue one fresh observation.
watch(()=>props.refreshKey,()=>{if(loading.value)refreshPending=true;else load();});
onMounted(()=>{window.addEventListener('pagehide',stop);window.addEventListener('pageshow',resume);load();});
onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',resume);});
</script>
<template>
 <section class="event-results" data-testid="path-migration" aria-label="最近本地迁移">
  <header class="page-header"><h2>最近本地迁移</h2><button class="button secondary" data-testid="migration-refresh" :disabled="loading" @click="load">刷新迁移状态</button></header>
  <p class="muted">显示服务端持久化的最近记录，可来自重启前；不证明本页刚才提交已成功。</p>
  <p v-if="props.receiptId" class="notice" data-testid="migration-receipt-match">{{record?.id===props.receiptId?'该记录 ID 与本次受理回执一致；完成情况以记录阶段为准。':'最近读取记录不是本次受理回执对应的迁移，请到完整历史按回执 ID 核对。'}}<span v-if="error">（状态读取失败，当前显示为旧证据）</span></p>
  <p v-if="error" class="notice danger" data-testid="migration-error" role="alert">{{error}}</p><p v-if="loading" role="status">正在读取迁移记录…</p><p v-if="none">尚无本地迁移记录。</p>
  <template v-if="record"><p><strong>{{statuses[record.status]}}</strong> · {{phases[record.phase]}}</p><dl class="path-settings-overview"><div><dt>原本地目录</dt><dd>{{record.source_path}}</dd></div><div><dt>目标本地目录</dt><dd>{{record.target_path}}</dd></div></dl>
   <table class="migration-counts"><thead><tr><th>处理项目</th><th>已处理</th><th>总量</th></tr></thead><tbody><tr><td>文件</td><td>{{record.processed_files}}</td><td>{{record.total_files}}</td></tr><tr><td>字节</td><td>{{record.processed_bytes}}</td><td>{{record.total_bytes}}</td></tr></tbody></table>
   <p class="muted">记录 ID：{{record.id}}<br>开始 UTC：{{record.started_at_utc}}<br>更新 UTC：{{record.updated_at_utc}}<template v-if="record.finished_at_utc&&!record.finished_at_utc.startsWith('0001-')"><br>结束 UTC：{{record.finished_at_utc}}</template></p>
   <p v-if="record.last_error" class="notice danger">{{record.last_error}}</p><p v-if="record.status==='failed'">如有程序占用，请关闭占用程序后，再按原来源和目标重试迁移；不要手动删除尚未核验的文件。</p>
   <button class="button secondary" data-testid="migration-export" :disabled="exporting||loading||!!error" @click="exportEvidence">{{exporting?'正在导出…':'导出此迁移证据'}}</button>
  </template><p v-if="exportResult" data-testid="migration-export-result" role="status">{{exportResult}}</p><p v-if="observed" class="muted">本机读取 UTC：{{observed}}；读取记录不代表重新核验磁盘文件。</p>
  <a href="/settings/migrations">查看迁移历史工作台</a> · <a href="/cloud#migration-history">旧版完整历史</a>
 </section>
</template>
