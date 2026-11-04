<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {queueTargetError,validatePathImpact,type PathImpact} from './path-settings-types';
const props=defineProps<{currentQueue:string;targetQueue:string;disabled:boolean}>(),emit=defineEmits<{ready:[value:PathImpact|null];authenticationRequired:[]}>();
const snapshot=ref<PathImpact|null>(null),loading=ref(false),error=ref(''),active=ref(true),observedAt=ref('');
const inputError=computed(()=>queueTargetError(props.targetQueue)),changed=computed(()=>props.targetQueue!==props.currentQueue);
const canPage=computed(()=>active.value&&!props.disabled&&!loading.value&&!error.value&&!!snapshot.value);
let generation=0,controller:AbortController|undefined;
function reset(){generation++;controller?.abort();snapshot.value=null;error.value='';loading.value=false;observedAt.value='';emit('ready',null);}
async function load(offset=0,fresh=true){
 if(!active.value||props.disabled||loading.value||inputError.value||!changed.value)return;
 const previous=snapshot.value,request=++generation;controller=new AbortController();loading.value=true;error.value='';emit('ready',null);
 try{
  const query=new URLSearchParams({queue_dir:props.targetQueue,offset:String(offset),limit:'50',identity_format:'string'});
  const response=await fetch('/api/settings/paths?'+query,{signal:controller.signal,credentials:'same-origin'});if(request!==generation)return;
  if(response.status===401){emit('authenticationRequired');throw Error('管理员登录已失效，请重新登录。');}
  if(!response.ok)throw Error(await response.text()||'影响预览暂不可读');
  const data=await response.json();if(request!==generation)return;
  const value=validatePathImpact(data,props.currentQueue,props.targetQueue,offset);
  if(!fresh&&previous){
   if(value.digest!==previous.digest||value.count!==previous.count)throw Error('任务快照已变化，请从第一页重新预览');
   if(value.tasks.length&&previous.tasks.length&&((offset>previous.offset&&BigInt(value.tasks[0].fs_id)<=BigInt(previous.tasks.at(-1)!.fs_id))||(offset<previous.offset&&BigInt(value.tasks.at(-1)!.fs_id)>=BigInt(previous.tasks[0].fs_id))))throw Error('任务分页边界无效，请重新预览');
  }
  snapshot.value=value;observedAt.value=new Date().toISOString();emit('ready',value);
 }catch(e:any){if(request===generation)error.value=e.message||'影响预览读取失败';}
 finally{if(request===generation)loading.value=false;}
}
function leave(){active.value=false;reset();}
function resume(event:PageTransitionEvent){if(event.persisted)active.value=true;}
watch(()=>[props.currentQueue,props.targetQueue,props.disabled],reset,{flush:'sync'});
onMounted(()=>{window.addEventListener('pagehide',leave);window.addEventListener('pageshow',resume);});
onUnmounted(()=>{leave();window.removeEventListener('pagehide',leave);window.removeEventListener('pageshow',resume);});
</script>
<template>
 <div class="path-impact">
  <p v-if="inputError" class="notice danger" data-testid="path-impact-input-error" role="alert">{{inputError}}</p>
  <p v-else-if="!changed" class="muted">监控目录未改变，无需预览变更影响。</p>
  <button class="button secondary" data-testid="path-impact-preview" :disabled="!active||disabled||loading||!!inputError||!changed" @click="load()">{{loading?'正在读取影响…':'预览监控目录变更影响'}}</button>
  <p v-if="error" class="notice danger" data-testid="path-impact-error" role="alert">{{error}}<span v-if="snapshot"> · 保留上次读取的任务，当前预览已失效，不能用于保存确认。</span></p>
  <div v-if="snapshot">
   <p>当前监控：{{snapshot.current_queue_dir}} → 草稿目标：{{snapshot.target_queue_dir}}</p>
   <p class="muted">受影响的未执行根任务 {{snapshot.count}} 项。此预览不是远端身份或移动完成凭据，也不代表整个下载队列空闲。</p>
   <div class="event-table-scroll" data-testid="path-impact-table"><table><thead><tr><th>任务 / 精确身份</th><th>状态</th><th>尝试次数</th><th>固定后处理策略</th></tr></thead><tbody><tr v-for="task in snapshot.tasks" :key="task.fs_id"><td>{{task.server_filename}}<small class="cell-sub">{{task.path}}</small><small class="cell-sub">ID：{{task.fs_id}}</small></td><td>{{task.status==='pending'?'待执行':task.status==='auth_wait'?'等待授权':'等待重试'}}</td><td>{{task.attempts}}</td><td>{{task.download_mode==='delete'?'完成后删除网盘项':'完成后保留并标记 .可删除'}}</td></tr></tbody></table></div>
   <p v-if="!snapshot.count" class="muted">没有会受影响的未执行根任务。</p>
   <div class="path-impact-pagination"><button class="button secondary" data-testid="path-impact-prev" :disabled="!canPage||snapshot.offset===0" @click="load(Math.max(0,snapshot.offset-50),false)">上一页</button><span>{{snapshot.tasks.length?`${snapshot.offset+1}–${snapshot.offset+snapshot.tasks.length} / ${snapshot.count}`:'本页无项目'}}</span><button class="button secondary" data-testid="path-impact-next" :disabled="!canPage||!snapshot.has_more" @click="load(snapshot.offset+50,false)">下一页</button></div>
   <details class="path-impact-evidence"><summary>预览摘要与读取时间</summary><p class="muted">本机读取 UTC：{{observedAt}}</p><code data-testid="path-impact-digest">{{snapshot.digest}}</code></details>
  </div>
 </div>
</template>
