<script setup lang="ts">
import {onMounted,onUnmounted,ref,watch} from 'vue';
import {vModal} from './modal';
import {validCloudDirectory} from './cloud-files-route';
import type {CleanupCandidate,CleanupEntry} from './cleanup-types';
const props=defineProps<{candidates:CleanupCandidate[];disabled:boolean;reconcile:()=>Promise<boolean>}>(),emit=defineEmits<{locked:[boolean];authenticationRequired:[]}>();
type Result={path:string;status:'confirmed'|'failed'|'unknown';message:string;operation?:string;observedAt:string;authExpired?:boolean};
const fixed=ref<CleanupCandidate[]|null>(null),results=ref<Result[]>([]),ack=ref(false),pending=ref(false),minimized=ref(false),blocked=ref(false),message=ref(''),current=ref(0),stopRequested=ref(false),shown=ref(50);
let active=true,generation=0,consumed=false;
const exact=(x:unknown)=>typeof x==='string'&&/^(0|[1-9][0-9]*)$/.test(x)&&x.length<=19&&BigInt(x)<=9223372036854775807n;
const validEntry=(e:CleanupEntry)=>e&&exact(e.fs_id)&&e.fs_id!=='0'&&exact(e.size)&&exact(e.provider_mtime)&&typeof e.path==='string'&&validCloudDirectory(e.path)&&e.path!=='/'&&typeof e.name==='string';
const expected=(e:CleanupEntry)=>({fs_id:e.fs_id,path:e.path,server_filename:e.name,size:e.size,isdir:e.directory?1:0,server_mtime:e.provider_mtime,md5:typeof e.content_md5==='string'?e.content_md5:''});
function prepare(){
 if(!active||props.disabled||pending.value||blocked.value||!props.candidates.length)return;
 const ids=new Set<string>(),paths=new Set<string>();
 for(const item of props.candidates){if(!validEntry(item.entry)||ids.has(item.entry.fs_id)||paths.has(item.entry.path)||(item.kind==='empty'?item.entry.directory!==true:item.kind!=='duplicate'||item.entry.directory!==false||!item.retained||!validEntry(item.retained)||item.retained.directory!==false||item.retained.fs_id===item.entry.fs_id||item.retained.size!==item.entry.size||!item.sha256||!/^[a-f0-9]{64}$/.test(item.sha256))){message.value='选择包含无效或重复身份，请刷新预览后重新选择。';return;}ids.add(item.entry.fs_id);paths.add(item.entry.path);}
 if(props.candidates.some(item=>item.retained&&(ids.has(item.retained.fs_id)||paths.has(item.retained.path)))){message.value='保留副本同时出现在待删清单中，请重新选择。';return;}
 fixed.value=JSON.parse(JSON.stringify(props.candidates));results.value=[];ack.value=false;pending.value=false;minimized.value=false;consumed=false;stopRequested.value=false;current.value=0;shown.value=50;message.value='';emit('locked',true);
}
function close(){if(!pending.value){fixed.value=null;emit('locked',false);}}
async function execute(item:CleanupCandidate,run:number):Promise<Result>{
 const body:any={operation:'delete',expected:expected(item.entry)};
 if(item.kind==='empty')body.require_empty_directory=true;else{body.expected_content_sha256=item.sha256;body.expected_retained=expected(item.retained!);}
 const response=await fetch('/api/cloud/mutations?identity_format=string',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
 // A late receipt must not redirect or invalidate a restored page.
 if(!active||run!==generation)throw Error('stale response');
 if(response.status===401)return {path:item.entry.path,status:'failed',message:'管理员登录失效，已停止后续项目。',observedAt:new Date().toISOString(),authExpired:true};
 if(response.status>=500||response.status===408)throw Error('unknown');
 if(response.status===400)return {path:item.entry.path,status:'failed',message:'身份或清理参数被拒绝：'+await response.text(),observedAt:new Date().toISOString()};
 const data=await response.json(),value=response.ok?data:data.operation;
 if(!value){if(response.status>=400&&response.status<500)return {path:item.entry.path,status:'failed',message:'远端核对或操作被拒绝，请刷新预览后检查操作记录。',observedAt:new Date().toISOString()};throw Error('missing receipt');}
 if(!/^[a-f0-9]{32}$/.test(value.operation_id)||value.object_id!==item.entry.fs_id||value.source_path!==item.entry.path||(value.destination_path||'')!==''||!['confirmed','failed','unknown'].includes(value.status))throw Error('receipt mismatch');
 return {path:item.entry.path,status:value.status,operation:value.operation_id,message:value.warning||(value.status==='confirmed'?'已确认删除':value.status==='failed'?'操作失败，批次已停止':'操作结果未知，批次已停止'),observedAt:new Date().toISOString()};
}
async function submit(){
 const list=fixed.value;if(!list||!ack.value||consumed||pending.value||blocked.value||props.disabled||!active)return;
 consumed=true;pending.value=true;emit('locked',true);const run=++generation;
 try{for(let index=0;index<list.length;index++){
  if(!active||run!==generation||stopRequested.value||props.disabled)break;current.value=index+1;
  let result:Result;try{result=await execute(list[index],run);}catch{result={path:list[index].entry.path,status:'unknown',message:'请求结果未知，可能已执行；不会重发。',observedAt:new Date().toISOString()};}
  if(!active||run!==generation)break;results.value.push(result);
  if(result.authExpired){message.value=result.message;emit('authenticationRequired');break;}
  if(result.status==='unknown')blocked.value=true;
  let refreshed=false;try{refreshed=await props.reconcile();}catch{refreshed=false;}
  if(!active||run!==generation)break;
  if(result.status!=='confirmed'){message.value='本批次已停止，剩余项目未发送；请按操作记录核对。';break;}
  if(!refreshed||result.message!=='已确认删除'){message.value='远端删除已确认，但本地刷新或后续核对存在问题，剩余项目未发送。';break;}
 }
 if(active&&run===generation&&!message.value)message.value=stopRequested.value?'已停止后续项目；已发送的请求按回执记录。':results.value.length===list.length?'本批次执行结束。':'本批次已停止。';
 }finally{if(run===generation){pending.value=false;minimized.value=true;emit('locked',false);}}
}
function stopForLifecycle(){active=false;generation++;stopRequested.value=true;if(pending.value){
 // Losing an in-flight receipt is a local unknown observation, not a provider failure.
 const item=fixed.value?.[current.value-1];if(item&&!results.value.some(r=>r.path===item.entry.path))results.value.push({path:item.entry.path,status:'unknown',message:'离页时未取得回执，请核对操作记录。',observedAt:new Date().toISOString()});
 blocked.value=true;message.value='离页或登录失效时请求尚未核对，可能已执行；后续项目已停止，请查看操作记录。';pending.value=false;minimized.value=true;}else fixed.value=null;emit('locked',false);}
function restore(event:PageTransitionEvent){if(event.persisted&&!props.disabled)active=true;}
watch(()=>props.disabled,value=>{if(value)stopForLifecycle();});onMounted(()=>{window.addEventListener('pagehide',stopForLifecycle);window.addEventListener('pageshow',restore);});onUnmounted(()=>{stopForLifecycle();window.removeEventListener('pagehide',stopForLifecycle);window.removeEventListener('pageshow',restore);});
</script>
<template><section class="event-results"><header class="page-header"><h2>执行所选清理</h2><button class="button primary" data-testid="cleanup-prepare" :disabled="disabled||pending||blocked||!candidates.length" @click="prepare">核对并清理 {{candidates.length}} 项</button></header><p class="muted">只操作网盘远端。每项由服务端暂停队列并核对身份，重复项重新读取待删和保留副本计算哈希；可能耗时并增加带宽使用。成功后刷新候选。</p><p v-if="blocked" class="notice danger">存在未知结果，本页已禁止再次提交清理。请查看操作记录并重新读取页面核对；不会自动重发。</p><div v-if="pending||results.length||message" data-testid="cleanup-progress"><p role="status">{{pending?'正在执行第 '+current+' 项':'执行反馈'}} · 已确认 {{results.filter(r=>r.status==='confirmed').length}} · 失败 {{results.filter(r=>r.status==='failed').length}} · 未知 {{results.filter(r=>r.status==='unknown').length}}</p><p v-if="message" class="notice">{{message}}</p><button v-if="fixed&&minimized" class="button secondary" @click="minimized=false">展开清单与进度</button><button v-if="pending" class="button secondary" data-testid="cleanup-stop" @click="stopRequested=true">{{stopRequested?'已请求停止后续项目':'停止后续项目'}}</button><div class="event-table-scroll"><table><thead><tr><th>网盘路径</th><th>结果</th><th>本机回执观察 UTC</th><th>证据</th></tr></thead><tbody><tr v-for="result in results" :key="result.path"><td>{{result.path}}</td><td>{{result.message}}</td><td>{{result.observedAt}}</td><td><a v-if="result.operation" :href="'/operations?operation_id='+result.operation">操作记录</a><a v-else href="/operations">查看操作记录</a></td></tr></tbody></table></div></div><div v-if="fixed" v-show="!minimized" class="modal-backdrop"><section class="detail-drawer" role="dialog" aria-modal="true" aria-labelledby="cleanup-batch-title" v-modal="close"><header class="page-header"><h2 id="cleanup-batch-title">确认删除网盘 {{fixed.length}} 项</h2><button class="button secondary" :disabled="pending" @click="close">关闭</button></header><p class="notice danger">将逐项删除下列远端项目。清单已固定，刷新候选不会改变本批次；未知或失败立即停止，未执行项不会自动补发。</p><p>重复文件会重新读取待删和保留副本计算 SHA-256；空目录会重新列出子项，线上已有内容则拒绝删除。</p><div class="event-table-scroll"><table><thead><tr><th>待删除路径</th><th>类型</th><th>保留副本</th></tr></thead><tbody><tr v-for="item in fixed.slice(0,shown)" :key="item.entry.fs_id"><td>{{item.entry.path}}</td><td>{{item.kind==='empty'?'空目录':'重复文件'}}</td><td>{{item.retained?.path||'—'}}</td></tr></tbody></table></div><button v-if="shown<fixed.length" class="button secondary" @click="shown+=50">再显示 50 项</button><label><input type="checkbox" data-testid="cleanup-batch-ack" v-model="ack" :disabled="pending||consumed"/>我已核对删除范围、保留副本与带宽影响</label><button class="button primary" data-testid="cleanup-batch-confirm" :disabled="disabled||pending||consumed||!ack" @click="submit">{{pending?'正在执行…':consumed?'本清单已消费':'确认执行清理'}}</button><button v-if="pending" class="button secondary" @click="minimized=true">收起并继续浏览</button><button v-if="pending" class="button secondary" @click="stopRequested=true">停止后续项目</button><p>已收到 {{results.length}} 项回执，共 {{fixed.length}} 项。</p></section></div></section></template>

<style scoped>
.detail-drawer table{min-width:0;width:100%;table-layout:fixed}
.detail-drawer th,.detail-drawer td{white-space:normal;overflow-wrap:anywhere}
.detail-drawer th:nth-child(2){width:70px}
.detail-drawer>label{display:block;margin:16px 0 12px}
.detail-drawer>button{margin:0 8px 8px 0}
</style>
