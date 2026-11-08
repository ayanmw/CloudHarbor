<script setup lang="ts">
import {onMounted,onUnmounted,ref,watch} from 'vue';
import {ListTodo} from 'lucide-vue-next';
import {vModal} from './modal';
import {backgroundSources,backgroundSnapshot,type BackgroundSnapshot,type BackgroundSource} from './background-task-status';
const props=defineProps<{disabled?:boolean}>();
type Row={snapshot:BackgroundSnapshot|null;observed:string;error:string};
const emptyRows=()=>Object.fromEntries(backgroundSources.map(source=>[source.id,{snapshot:null,observed:'',error:''}])) as Record<BackgroundSource,Row>;
const opened=ref(false),loading=ref(false),login=ref(false),rows=ref(emptyRows());
let generation=0,controller:AbortController|undefined;
function close(){generation++;controller?.abort();opened.value=false;loading.value=false;}
function open(){if(props.disabled)return;rows.value=emptyRows();login.value=false;opened.value=true;load();}
async function load(){
 if(!opened.value||props.disabled||loading.value||login.value)return;
 const request=++generation;controller?.abort();const abort=new AbortController();controller=abort;loading.value=true;
 const current=()=>opened.value&&!props.disabled&&request===generation;
 await Promise.all(backgroundSources.map(async source=>{
  try{const response=await fetch(source.url,{credentials:'same-origin',signal:abort.signal});if(!current())return;
   if(response.status===401){login.value=true;generation++;abort.abort();loading.value=false;return;}
   if(!response.ok)throw Error('读取失败，请稍后刷新或进入详情核对。');
   const value=await response.json();if(!current())return;const snapshot=backgroundSnapshot(source.id,value);rows.value[source.id]={snapshot,observed:new Date().toISOString(),error:''};
  }catch(e:any){if(current())rows.value[source.id].error=e.message||'读取失败，请进入详情核对。';}
 }));
 if(current())loading.value=false;
}
watch(()=>props.disabled,value=>{if(value)close();});
onMounted(()=>{window.addEventListener('pagehide',close);window.addEventListener('popstate',close)});
onUnmounted(()=>{close();window.removeEventListener('pagehide',close);window.removeEventListener('popstate',close)});
</script>
<template><button class="header-icon-button" data-testid="background-tasks-open" type="button" aria-label="后台任务" title="后台任务" :aria-expanded="opened" :disabled="disabled" @click="open"><ListTodo :size="18"/><span>任务</span></button><Teleport to="body"><div v-if="opened" class="modal-backdrop" @click.self="close"><section class="detail-drawer background-tasks-panel" role="dialog" aria-modal="true" aria-labelledby="background-tasks-title" data-testid="background-tasks-panel" v-modal="close"><header class="page-header"><div><h2 id="background-tasks-title">后台任务快照</h2><p class="muted">按需读取，不自动刷新；关闭面板不会停止后台任务。</p></div><button class="button secondary" data-testid="background-tasks-close" @click="close">关闭</button></header><p v-if="login" class="notice danger" role="alert">管理员会话失效，请重新登录。下方仅保留此前快照。 <a class="button secondary compact" href="/login">前往登录</a></p><button class="button secondary" data-testid="background-tasks-refresh" :disabled="loading||login" @click="load">{{loading?'正在读取…':'刷新快照'}}</button><div class="event-table-scroll"><table><thead><tr><th>任务 / 详情</th><th>状态快照</th><th>观察时间 UTC</th></tr></thead><tbody><tr v-for="source in backgroundSources" :key="source.id" :data-background-source="source.id"><td><strong>{{source.label}}</strong><a class="button secondary compact" :href="rows[source.id].snapshot?.href||source.href">查看详情</a></td><td><p v-if="rows[source.id].error" class="notice danger" role="alert">{{rows[source.id].error}}</p><template v-if="rows[source.id].snapshot"><strong v-if="rows[source.id].error||loading||login" class="snapshot-old">上次快照（待重新核对）</strong><p>{{rows[source.id].snapshot!.summary}}</p><small class="muted">{{rows[source.id].snapshot!.detail}}</small></template><span v-else>{{loading&&!login?'等待读取':'尚无可用快照'}}</span></td><td><template v-if="rows[source.id].observed">本机读取：<time>{{rows[source.id].observed}}</time><template v-if="rows[source.id].snapshot!.serverUTC">{{rows[source.id].snapshot!.serverLabel||'服务端记录'}}：<time>{{rows[source.id].snapshot!.serverUTC}}</time></template></template><span v-else>—</span></td></tr></tbody></table></div><p class="muted">各来源独立读取，非同一时刻的全局事务快照。清理仅展示最近批次，不能据此推定其他批次已完成；未知结果请进入详情核对。</p></section></div></Teleport></template>
<style scoped>
.background-tasks-panel{width:min(980px,100vw);min-width:0}.background-tasks-panel table{min-width:680px}.background-tasks-panel th,.background-tasks-panel td{white-space:normal;overflow-wrap:anywhere;vertical-align:top}.background-tasks-panel td:first-child{min-width:130px}.background-tasks-panel td:first-child a{display:flex;margin-top:8px;width:max-content;border:1px solid #cbd5e5}.background-tasks-panel td p{margin:4px 0 8px}.background-tasks-panel time{display:block;font-size:12px;margin-bottom:8px}.background-tasks-panel .snapshot-old{display:block;color:#8b631d;font-size:12px}.background-tasks-panel .event-table-scroll{margin-top:14px}.background-tasks-panel .page-header{align-items:flex-start}@media(max-width:540px){.background-tasks-panel{padding:18px 14px}.background-tasks-panel .page-header h2{font-size:20px}}
</style>
