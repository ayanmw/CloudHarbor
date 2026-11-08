<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {cloudMutationRejection} from './cloud-mutation-rejection';
import CloudDirectoryPicker from './CloudDirectoryPicker.vue';
import CloudBatchPreview from './CloudBatchPreview.vue';
import {vModal} from './modal';
import type {CloudFileEntry,CloudActionResult} from './cloud-files-types';
import {validCloudDirectory} from './cloud-files-route';
const props=defineProps<{operation:'move'|'copy'|'delete';items:CloudFileEntry[];directory:string;observedAt:string;minimized:boolean;afterSuccess:()=>Promise<void>}>(),emit=defineEmits<{close:[];minimize:[];busy:[boolean,string?,boolean?];auxiliaryResult:[CloudActionResult];result:[CloudActionResult];authenticationRequired:[]}>();
const auxiliaryBusy=ref(false);
function directoryBusy(value:boolean,label?:string,confirmed=false){auxiliaryBusy.value=value;if(value)ack.value=false;emit('busy',value,label,confirmed);}
function directoryUnknown(value:CloudActionResult){consumed.value=true;stopRequested.value=true;auxiliaryBusy.value=false;choosing.value=false;message.value=value.message;emit('result',value);emit('busy',false);}
const previewReady=ref(false);
const choosing=ref(false),panel=ref<HTMLElement|null>(null);
// Reopening a minimized drawer must restore its keyboard focus and Escape handling.
watch(()=>props.minimized,value=>{if(!value)queueMicrotask(()=>panel.value?.querySelector<HTMLElement>('button:not(:disabled),a[href],input:not(:disabled)')?.focus())});
const snapshot=props.items.map(entry=>({...entry})),operation=props.operation,sourceDirectory=props.directory,observedAt=props.observedAt;
const rows=ref(snapshot.map(entry=>({entry,state:'not_sent',operationID:'',message:''}))),ack=ref(false),pending=ref(false),consumed=ref(false),stopRequested=ref(false),message=ref(''),destination=ref(sourceDirectory);
watch(previewReady,value=>{if(!value)ack.value=false;},{flush:'sync'});
watch(destination,()=>{previewReady.value=false;ack.value=false;},{flush:'sync'});
const labels:Record<string,string>={not_sent:'未发送',sending:'正在执行',confirmed:'已确认',failed:'失败',unknown:'结果未知'};
let active=true,generation=0;
function target(entry:CloudFileEntry,parent=destination.value){return operation==='delete'?'':(parent==='/'?'':parent)+'/'+entry.name;}
const valid=computed(()=>snapshot.length>0&&snapshot.length<=100&&new Set(snapshot.map(i=>i.fs_id)).size===snapshot.length&&(operation==='delete'||validCloudDirectory(destination.value)&&snapshot.every(i=>target(i)!==i.path&&(!i.directory||!(destination.value===i.path||destination.value.startsWith(i.path+'/'))))));
function picked(path:string){destination.value=path;ack.value=false;choosing.value=false;queueMicrotask(()=>{const start=panel.value?.querySelector<HTMLButtonElement>('[data-testid=batch-preview-start]');if(start&&!start.disabled)start.focus();else panel.value?.querySelector<HTMLInputElement>('[data-testid=cloud-batch-destination]')?.focus();});}
function cancelPicker(){choosing.value=false;queueMicrotask(()=>panel.value?.querySelector<HTMLElement>('[data-testid=cloud-batch-pick-directory]')?.focus());}
function close(){if(!pending.value&&!auxiliaryBusy.value)emit('close');}
function expected(entry:CloudFileEntry){return {fs_id:entry.fs_id,path:entry.path,server_filename:entry.name,size:entry.provider_size,isdir:entry.directory?1:0,server_mtime:entry.server_mtime,md5:entry.md5||''};}
async function start(){
 if(!active||auxiliaryBusy.value||pending.value||consumed.value||!ack.value||!valid.value||choosing.value||(operation!=='delete'&&!previewReady.value))return;const request=++generation,fixedDestination=destination.value;pending.value=true;consumed.value=true;message.value='';emit('busy',true);
 try{for(const row of rows.value){if(!active||request!==generation||stopRequested.value)break;row.state='sending';const fixedTarget=target(row.entry,fixedDestination);
  try{const body:any={operation,expected:expected(row.entry)};if(operation!=='delete')body.destination_directory=fixedDestination;
   const response=await fetch('/api/cloud/mutations?identity_format=string',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(!active||request!==generation)return;
   if(response.status===401){row.state='failed';row.message='管理员登录已失效，本批停止';pending.value=false;emit('busy',false);emit('authenticationRequired');return;}
   if(response.status>=500||response.status===408)throw Error('请求未取得可信回执');
   const data=await response.json().catch(e=>{if(response.status===400)return null;throw e});if(!active||request!==generation)return;const rejection=cloudMutationRejection(response.status,data);
   if(rejection){row.state='failed';row.message=rejection;message.value='本批停止，后续项目未发送。';emit('result',{status:'failed',message:rejection,source:row.entry.path,target:fixedTarget});try{await props.afterSuccess();if(!active||request!==generation)return;}catch{if(!active||request!==generation)return;row.message+=' 目录刷新失败，请手动刷新。';}break;}
   if(data?.outcome!==undefined)throw Error('拒绝回执无效');
   if(response.status===400){row.state='failed';row.message='输入或身份被拒绝，请刷新后重新选择';message.value='本批停止，后续项目未发送。';break;}
   const result=response.ok?data:data?.operation;
   if(!result||!['confirmed','failed','unknown'].includes(result.status)||typeof result.operation_id!=='string'||!/^[a-f0-9]{32}$/.test(result.operation_id)||result.object_id!==row.entry.fs_id||(result.source_path||'')!==row.entry.path||(result.destination_path||'')!==fixedTarget)throw Error('回执与固定清单不一致');
   row.state=result.status;row.operationID=result.operation_id;row.message=typeof result.warning==='string'?result.warning:'';
   emit('result',{status:row.state,operation_id:row.operationID,message:row.message||'批量操作 · '+labels[row.state],source:row.entry.path,target:fixedTarget});
   if(row.state!=='confirmed'){message.value='本批停止，后续项目未发送；结果未知时不要重复提交。';break;}
   try{await props.afterSuccess();if(!active||request!==generation)return;}catch{message.value='该项已确认，但目录刷新未确认成功；本批停止，后续项目未发送。';break;}
  }catch{if(!active||request!==generation)return;row.state='unknown';row.message='结果未知，可能已执行，请查看操作记录，不要重复提交';message.value='本批停止，后续项目未发送。';emit('result',{status:'unknown',message:row.message,source:row.entry.path,target:fixedTarget});break;}
 }
 if(active&&request===generation&&!message.value)message.value=stopRequested.value?'已停止后续项目；当前已发送项的结果见下表。':'本批所有已选项目均已确认。';
 }finally{if(request===generation){pending.value=false;emit('busy',false);
 // The initiating control disappears after consumption; keep keyboard focus in the result drawer.
 queueMicrotask(()=>{if(active&&request===generation&&!props.minimized)panel.value?.querySelector<HTMLElement>('button:not(:disabled)')?.focus()});}}
}
function leave(){active=false;generation++;const row=rows.value.find(row=>row.state==='sending');if(row){row.state='unknown';row.message='离页时尚未取得确认，请核对操作记录';emit('result',{status:'unknown',message:row.message,source:row.entry.path,target:target(row.entry)});}pending.value=false;if(consumed.value)stopRequested.value=true;previewReady.value=false;ack.value=false;message.value=consumed.value?'已离页，本批不再自动发送后续项目。':'已离页，请重新在线核对后确认。';emit('busy',false);}
function restore(event:PageTransitionEvent){if(event.persisted){active=true;if(!consumed.value)stopRequested.value=false;}}
onMounted(()=>{window.addEventListener('pagehide',leave);window.addEventListener('pageshow',restore)});onUnmounted(()=>{active=false;generation++;window.removeEventListener('pagehide',leave);window.removeEventListener('pageshow',restore)});
</script>
<template><div v-show="!minimized" class="modal-backdrop"><section ref="panel" class="detail-drawer cloud-batch-drawer" role="dialog" aria-modal="true" aria-labelledby="cloud-batch-title" v-modal="close"><header class="page-header"><h2 id="cloud-batch-title">{{operation==='delete'?'批量删除':operation==='copy'?'批量复制':'批量移动'}} · 固定 {{snapshot.length}} 项</h2><button class="button secondary" :disabled="auxiliaryBusy||pending" @click="close">关闭</button></header><p class="notice">仅处理确认时固定的清单，逐项在线核对身份；每项确认后刷新列表。关页停止后续，已发送但未确认项保持未知，不自动重发。删除目录包含其内容。复制保留来源，同名目标拒绝覆盖。</p><p class="muted">来源：{{sourceDirectory}} · 列表观察 UTC：{{observedAt||'未记录'}}</p><label v-if="operation!=='delete'">目标网盘目录<input data-testid="cloud-batch-destination" v-model="destination" :disabled="auxiliaryBusy||pending||consumed" @input="ack=false"/><button class="button secondary" type="button" data-testid="cloud-batch-pick-directory" :disabled="auxiliaryBusy||pending||consumed" @click="choosing=true;ack=false">浏览选择目录</button></label><CloudDirectoryPicker v-if="choosing&&!consumed" :initial="destination" :after-create="afterSuccess" @busy="directoryBusy" @confirmed="emit('auxiliaryResult',$event)" @unknown="directoryUnknown" :forbidden="snapshot.filter(i=>i.directory).map(i=>i.path)" @select="picked" @close="cancelPicker" @authentication-required="emit('authenticationRequired')"/><CloudBatchPreview v-if="operation!=='delete'&&!consumed" :items="snapshot" :operation="operation" :destination="destination" :disabled="auxiliaryBusy||pending||choosing||!valid" @ready="previewReady=$event" @authentication-required="emit('authenticationRequired')"/><div class="event-table-scroll"><table><thead><tr><th>项目 / 来源</th><th v-if="operation!=='delete'">固定目标</th><th>状态</th><th>结果 / 记录</th></tr></thead><tbody><tr v-for="row in rows" :key="row.entry.fs_id"><td>{{row.entry.name}}<small class="cell-sub">{{row.entry.path}}</small></td><td v-if="operation!=='delete'">{{target(row.entry)}}</td><td>{{labels[row.state]}}</td><td>{{row.message}}<a v-if="row.operationID" class="event-detail-link" :href="'/operations?operation_id='+row.operationID">查看操作记录</a></td></tr></tbody></table></div><template v-if="!consumed"><label><input type="checkbox" data-testid="cloud-batch-ack" v-model="ack" :disabled="operation!=='delete'&&!previewReady"/>已核对这 {{snapshot.length}} 项及{{operation==='delete'?'目录内全部内容的删除范围':'目标目录'}}</label><p v-if="!valid" class="notice">目标无效、未变化，或包含项目自身/子目录，不能执行。</p><button class="button" :class="operation==='delete'?'danger-button':'primary'" data-testid="cloud-batch-confirm" :disabled="!ack||!valid||pending||choosing||(operation!=='delete'&&!previewReady)" @click="start">确认{{operation==='delete'?'删除':operation==='copy'?'复制':'移动'}}这 {{snapshot.length}} 项</button></template><div v-if="pending||auxiliaryBusy" class="toolbar"><button v-if="pending" class="button secondary" :disabled="stopRequested" @click="stopRequested=true">停止后续项目</button><button class="button secondary" @click="emit('minimize')">收起并继续浏览</button></div><p v-if="message" role="status">{{message}}</p><p><a class="button secondary" href="/operations">操作记录中心</a></p></section></div></template>
<style scoped>.cloud-batch-drawer{width:min(900px,100vw)}.cloud-batch-drawer td{white-space:normal;overflow-wrap:anywhere}.cloud-batch-drawer input:not([type=checkbox]){width:100%;border:1px solid #ccd5e5;border-radius:6px;padding:9px}.cloud-batch-drawer>label{display:block;margin:16px 0}</style>
