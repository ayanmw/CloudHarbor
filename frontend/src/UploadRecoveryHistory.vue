<script setup lang="ts">
import {onMounted,onUnmounted,ref,watch} from 'vue';
import {formatBytes} from './format-bytes';
import {uploadNames} from './upload-progress';
const props=defineProps<{batchId:string;disabled:boolean}>(),emit=defineEmits<{authenticationRequired:[]}>();
type Recovery={sequence:string;item_id:string;path:string;previous_state:string;previous_updated_at_utc:string;state:string;confirmed_part_bytes:string;recorded_at_utc:string};
const expanded=ref(false),records=ref<Recovery[]|null>(null),error=ref(''),loading=ref(false),next=ref<string|null>(null),page=ref(0),trail=ref<(string|null)[]>([null]);
let active=true,generation=0,controller:AbortController|undefined;
const counter=(value:unknown,zero=false):value is string=>typeof value==='string'&&(zero?/^(0|[1-9][0-9]*)$/:/^[1-9][0-9]*$/).test(value)&&(value.length<19||(value.length===19&&value<='9223372036854775807'));
const timestamp=(value:unknown)=>typeof value==='string'&&/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)&&Number.isFinite(Date.parse(value));
function invalidate(){generation++;controller?.abort();loading.value=false;}
async function load(before:string|null=null,index=0){
 if(!active||props.disabled||!expanded.value)return;
 const request=++generation,batch=props.batchId;controller?.abort();controller=new AbortController();loading.value=true;error.value='';
 const query=new URLSearchParams({limit:'50'});if(before)query.set('before_sequence',before);
 try{
  const response=await fetch('/api/uploads/'+batch+'/recovery?'+query,{credentials:'same-origin',signal:controller.signal});if(!active||request!==generation)return;
  if(response.status===401){emit('authenticationRequired');throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'恢复历史不可读');
  const data=await response.json();if(!active||request!==generation)return;
  if(data.batch_id!==batch||!Array.isArray(data.records)||data.records.length>50)throw Error('恢复历史批次响应无效');
  let previous=before;
  for(const r of data.records){
   if(!r||!counter(r.sequence)||typeof r.item_id!=='string'||!/^[a-f0-9]{32}$/.test(r.item_id)||typeof r.path!=='string'||!r.path||!['hashing','uploading','submitting','finalizing'].includes(r.previous_state)||!(['submitting','finalizing'].includes(r.previous_state)?r.state==='unknown':['queued','paused'].includes(r.state))||!counter(r.confirmed_part_bytes,true)||!timestamp(r.previous_updated_at_utc)||!timestamp(r.recorded_at_utc)||(previous!==null&&BigInt(r.sequence)>=BigInt(previous)))throw Error('恢复历史证据响应无效');
   previous=r.sequence;
  }
  if(!(data.next_before_sequence===null||(counter(data.next_before_sequence)&&data.records.length===50&&data.next_before_sequence===data.records.at(-1).sequence)))throw Error('恢复历史分页响应无效');
  records.value=data.records;next.value=data.next_before_sequence;page.value=index;trail.value=index===0?[null]:[...trail.value.slice(0,index),before];
 }catch(e:any){if(active&&request===generation)error.value=e.message||'恢复历史不可读';}
 finally{if(active&&request===generation)loading.value=false;}
}
function toggle(){expanded.value=!expanded.value;if(expanded.value)load();else invalidate();}
watch(()=>props.batchId,()=>{invalidate();expanded.value=false;records.value=null;error.value='';next.value=null;page.value=0;trail.value=[null];});
watch(()=>props.disabled,value=>{if(value)invalidate();});
function suspend(){active=false;invalidate();}
function restore(event:PageTransitionEvent){if(event.persisted)active=true;}
onMounted(()=>{window.addEventListener('pagehide',suspend);window.addEventListener('pageshow',restore);});
onUnmounted(()=>{suspend();window.removeEventListener('pagehide',suspend);window.removeEventListener('pageshow',restore);});
</script>
<template>
 <section class="upload-recovery-history" data-testid="upload-recovery-history">
  <button class="button secondary" data-testid="upload-recovery-toggle" :aria-expanded="expanded" :disabled="disabled" @click="toggle">{{expanded?'收起重启恢复记录':'查看重启恢复记录'}}</button>
  <template v-if="expanded"><p class="muted">这里只展示已保存的历史恢复证据，不代表当前执行状态。重新排队只表示等待处理，是否恢复会话请查看文件详情；提交结果未知仍隔离。</p>
   <button class="button secondary compact" :disabled="loading||disabled" @click="load()">刷新恢复记录</button>
   <p v-if="loading" role="status">正在读取恢复历史…</p><p v-if="error" class="notice danger" role="alert">{{error}}；保留上次结果，翻页暂不可用。</p>
   <p v-if="records?.length===0" class="muted">本批未记录重启恢复事件；旧记录不会补造。</p>
   <div v-if="records?.length" class="event-table-scroll"><table><thead><tr><th>文件 / 事件</th><th>重启前阶段 / 原记录 UTC</th><th>恢复处理 / UTC</th><th>历史分片确认量</th></tr></thead><tbody><tr v-for="record in records" :key="record.sequence"><td>{{record.path}}<small class="cell-sub">事件 {{record.sequence}} · {{record.item_id}}</small></td><td>{{uploadNames[record.previous_state]}}<small class="cell-sub">{{record.previous_updated_at_utc}}</small></td><td>{{record.state==='paused'?'保留暂停请求；已暂停':record.state==='queued'?'重新排队；未证明分片会话恢复':'结果待核对；未自动重发'}}<small class="cell-sub">{{record.recorded_at_utc}}</small></td><td>{{formatBytes(record.confirmed_part_bytes)}}</td></tr></tbody></table></div>
   <p>恢复历史第 {{page+1}} 页</p><button class="button secondary compact" :disabled="loading||disabled||!!error||page===0" @click="load(trail[page-1],page-1)">较新恢复记录</button><button class="button secondary compact" :disabled="loading||disabled||!!error||!next" @click="load(next,page+1)">更早恢复记录</button>
  </template>
 </section>
</template>
