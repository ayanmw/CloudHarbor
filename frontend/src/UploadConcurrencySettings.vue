<script setup lang="ts">
import {onMounted,onUnmounted,ref,watch} from 'vue';
const props=defineProps<{disabled:boolean}>(),emit=defineEmits<{authenticationRequired:[]}>();
type Snapshot={limit:number;maximum:number;admitted_files:number;finishing_files:number;admission_error:string;updated_at_utc:string;observed_at_utc:string};
const expanded=ref(false),busy=ref(false),data=ref<Snapshot|null>(null),draft=ref(1),error=ref(''),feedback=ref(''),fresh=ref(false),unconfirmed=ref(false);
let active=true,generation=0,writing=false,controller:AbortController|undefined;
function utc(value:unknown){return typeof value==='string'&&value.endsWith('Z')&&Number.isFinite(Date.parse(value));}
function valid(value:any):value is Snapshot{return value&&(value.limit===1||value.limit===2)&&value.maximum===2&&['admitted_files','finishing_files'].every(key=>Number.isSafeInteger(value[key])&&value[key]>=0&&value[key]<=2)&&value.admitted_files+value.finishing_files<=2&&typeof value.admission_error==='string'&&(value.updated_at_utc===''||utc(value.updated_at_utc))&&utc(value.observed_at_utc);}
function invalidate(){if(writing)unconfirmed.value=true;generation++;controller?.abort();writing=false;busy.value=false;fresh.value=false;}
function close(){expanded.value=false;invalidate();queueMicrotask(()=>document.querySelector<HTMLElement>('[data-testid=upload-concurrency-toggle]')?.focus());}
function toggle(){if(!active||props.disabled)return;if(expanded.value){close();return}expanded.value=true;void read();}
async function request(save:boolean){
 if(!active||props.disabled||busy.value||!expanded.value||(save&&(!fresh.value||!data.value||![1,2].includes(draft.value))))return;
 const id=++generation,requested=draft.value,current=()=>active&&expanded.value&&id===generation;controller?.abort();controller=new AbortController();busy.value=true;fresh.value=false;writing=save;error.value='';feedback.value='';
 try{const response=await fetch('/api/uploads/settings/concurrency',{method:save?'POST':'GET',credentials:'same-origin',signal:controller.signal,...(save?{headers:{'Content-Type':'application/json'},body:JSON.stringify({limit:requested})}:{})});if(!current())return;
  if(response.status===401){emit('authenticationRequired');return}
  if(!response.ok)throw Error(await response.text()||'并发设置请求失败');
  const value=await response.json();if(!current())return;
  if(!valid(value)||(save&&value.limit!==requested))throw Error('并发设置响应无效或已被其他页面改变');
  data.value=value;draft.value=value.limit;fresh.value=true;
  if(save){unconfirmed.value=false;feedback.value='已保存；后续准入使用新上限，降低并发不会中断在途请求。待核对任务和已停止的执行器不会因此恢复。'}else feedback.value='已读取当前配置和运行快照。';
 }catch(e:any){if(current()){if(save)unconfirmed.value=true;error.value=(save?'保存结果未确认：':'读取失败：')+(e.message||'请求中断')+'。请刷新核对，不会自动重发。';}}
 finally{if(current()){writing=false;busy.value=false;}}
}
function read(){return request(false)}
function leave(){active=false;expanded.value=false;invalidate();}
function restore(event:PageTransitionEvent){if(event.persisted)active=true;}
function pop(){leave();active=true;}
watch(()=>props.disabled,value=>{if(value){expanded.value=false;invalidate();}});
onMounted(()=>{window.addEventListener('pagehide',leave);window.addEventListener('pageshow',restore);window.addEventListener('popstate',pop)});
onUnmounted(()=>{leave();window.removeEventListener('pagehide',leave);window.removeEventListener('pageshow',restore);window.removeEventListener('popstate',pop)});
</script>
<template><section class="upload-concurrency"><button type="button" class="button secondary" data-testid="upload-concurrency-toggle" :aria-expanded="expanded" :disabled="disabled" @click="toggle">{{expanded?'收起上传并发设置':'上传并发与运行快照'}}</button><div v-if="expanded" data-testid="upload-concurrency-settings" class="event-results"><h2>上传并发</h2><p class="muted">默认 1，最多 2 个文件；目录与远端提交仍排队。降低上限不会中断在途请求，只影响后续准入；并发增加不保证带宽提高。</p><p v-if="unconfirmed" class="notice">上次保存响应未确认。下方快照只代表本次读取，不证明原请求已结束；请核对后操作，不会自动重发。</p><p v-if="error" class="notice danger" role="alert">{{error}}</p><p v-if="feedback" role="status">{{feedback}}</p><template v-if="data"><p>{{fresh?'当前读取':'前次快照'}}：当前设置 {{data.limit}} · 已准入 {{data.admitted_files}} · 回执收尾 {{data.finishing_files}}</p><p class="muted">观察 UTC {{data.observed_at_utc}} · {{data.updated_at_utc?'配置保存 UTC '+data.updated_at_utc:'使用默认配置'}}。数量来自本机当前尝试所有权，未返回不等于一直传输字节；此处手动刷新。</p><p v-if="data.admission_error" role="alert" class="notice danger">执行器已停止新准入：{{data.admission_error}}。在途回执仍会保存，请检查日志后重启核对。</p></template><div class="toolbar"><label>并行文件上限 <select v-model.number="draft" :disabled="disabled||busy||!data"><option :value="1">1 个文件 · 默认</option><option :value="2">2 个文件</option></select></label><button type="button" class="button primary" data-testid="upload-concurrency-save" :disabled="disabled||busy||!fresh||!data" @click="request(true)">保存并发设置</button><button type="button" class="button secondary" data-testid="upload-concurrency-refresh" :disabled="disabled||busy" @click="read">刷新配置与快照</button></div><p v-if="busy" role="status">正在处理并发设置请求…</p></div></section></template>
<style scoped>.upload-concurrency{margin-bottom:16px}.upload-concurrency .event-results{margin-top:12px}.upload-concurrency label{display:flex;align-items:center;gap:8px}.upload-concurrency select{border:1px solid var(--line);border-radius:6px;padding:8px}</style>
