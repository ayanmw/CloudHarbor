<script setup lang="ts">
import {onMounted,onUnmounted,ref,watch} from 'vue';
import {formatBytes} from './format-bytes';
import type {Resolution} from './sync-resolution';
const props=defineProps<{decision:Resolution;disabled?:boolean}>(),emit=defineEmits<{authenticationRequired:[]}>();
type Step={action:string;path:string;from?:string;local?:{id:string;directory:boolean;size:string};remote?:{id:string;directory:boolean;size:string};reason?:string};
const labels:Record<string,string>={local_archive:'归档本地旧项',remote_archive:'归档网盘旧项',local_move:'保留本地副本',local_mkdir:'建立本地目录',remote_mkdir:'建立网盘目录',download:'下载到本地',upload:'上传到网盘'};
const rows=ref<Step[]>([]),offset=ref(0),loaded=ref(false),busy=ref(false),fresh=ref(false),more=ref(false),error=ref('');
let active=true,generation=0,controller:AbortController|undefined;
const relative=(p:unknown):p is string=>typeof p==='string'&&!!p&&!/[\\:\x00]/.test(p)&&p.split('/').every(x=>!!x&&x!=='.'&&x!=='..');
const within=(p:string,root:string)=>p===root||p.startsWith(root+'/');
function parse(data:any,at:number):Step[]{
 const review=props.decision.subtree_review!,root=review.root,copy=props.decision.local_copy_path,total=BigInt(review.plan_step_count!);
 const inScope=(p:unknown)=>relative(p)&&(within(p,root)||!!copy&&within(p,copy));
 const expected=BigInt(at)>=total?0:Number(total-BigInt(at)>100n?100n:total-BigInt(at));
 if(!data||data.decision_id!==props.decision.id||data.version!=='subtree-steps-v1'||data.root!==root||data.total!==review.plan_step_count||data.offset!==at||data.limit!==100||data.has_more!==(BigInt(at+expected)<total)||!Array.isArray(data.steps)||data.steps.length!==expected)throw Error('有序步骤分页身份或范围无效');
 for(const step of data.steps){
  if(!step||!Object.hasOwn(labels,step.action)||!inScope(step.path)||step.reason!==undefined&&typeof step.reason!=='string'||step.from!==undefined&&!inScope(step.from))throw Error('步骤操作或路径无效');
  if(props.decision.source_config.mode==='pull'&&['remote_archive','remote_mkdir','upload'].includes(step.action))throw Error('单向同步步骤不得写入网盘');
  const source=['local_archive','local_move','remote_mkdir','upload'].includes(step.action)?step.local:step.remote;
  if(!source||typeof source.id!=='string'||!source.id||typeof source.directory!=='boolean'||typeof source.size!=='string'||!/^(0|[1-9][0-9]*)$/.test(source.size)||source.path!==(step.action==='local_move'?step.from:step.path))throw Error('步骤来源证据无效');
  if(step.action==='local_move'&&(!step.from||step.path!==copy||step.from!==root)||step.action.endsWith('_mkdir')&&!source.directory||['download','upload'].includes(step.action)&&source.directory)throw Error('步骤类型或副本映射无效');
 }
 return data.steps;
}
function invalidate(){generation++;controller?.abort();busy.value=false}
async function load(at=offset.value){
 if(!active||busy.value||props.disabled||!Number.isSafeInteger(at)||at<0)return;
 const n=++generation;controller=new AbortController();busy.value=true;fresh.value=false;
 try{const response=await fetch('/api/sync/resolutions/'+props.decision.id+'/subtree-plan?offset='+at,{credentials:'same-origin',signal:controller.signal});if(!active||n!==generation)return;if(response.status===401){stop();emit('authenticationRequired');return}if(!response.ok)throw Error((await response.text()).slice(0,1000)||'步骤暂不可读');const data=await response.json();if(!active||n!==generation)return;const next=parse(data,at);rows.value=next;offset.value=at;more.value=data.has_more;loaded.value=true;fresh.value=true;error.value=''}catch(e){if(active&&n===generation)error.value=e instanceof Error?e.message:String(e)}finally{if(active&&n===generation)busy.value=false}
}
function reset(){invalidate();rows.value=[];offset.value=0;loaded.value=false;fresh.value=false;more.value=false;error.value=''}
function stop(){active=false;invalidate();fresh.value=false}
function restore(){active=true}
watch(()=>props.decision.id,reset);watch(()=>props.disabled,v=>{if(v){invalidate();fresh.value=false}});
onMounted(()=>{window.addEventListener('pagehide',stop);window.addEventListener('pageshow',restore)});onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',restore)});
</script>
<template><section class="subtree-plan" data-testid="subtree-plan"><header><h3>固定处理步骤</h3><button class="button secondary compact" data-testid="subtree-plan-read" :disabled="busy||disabled" @click="load()">{{busy?'读取中…':loaded?'重新读取本页步骤':'查看有序步骤'}}</button></header><p class="muted">共 {{decision.subtree_review?.plan_step_count}} 步，按表内顺序规划。归档保留旧内容；具体归档位置由执行回执记录。需要完整重核并明确确认后才会执行。</p><p v-if="error" class="notice danger" role="alert">{{error}}<span v-if="loaded"> · 保留上次读取的固定页。</span></p><div v-if="loaded" class="table-wrap"><table><thead><tr><th>顺序</th><th>处理</th><th>项目 / 来源路径</th><th>原项证据</th></tr></thead><tbody><tr v-for="(step,i) in rows" :key="offset+i"><td>{{offset+i+1}}</td><td>{{labels[step.action]}}</td><td>{{step.path}}<small v-if="step.from">来源 {{step.from}}</small><small>{{step.reason}}</small></td><td><template v-if="step.local||step.remote">{{(step.local||step.remote)!.directory?'文件夹':formatBytes((step.local||step.remote)!.size)}}<small>ID {{(step.local||step.remote)!.id}}</small></template></td></tr></tbody></table><footer><button class="button secondary compact" :disabled="busy||disabled||!fresh||offset===0" @click="load(Math.max(0,offset-100))">上一页步骤</button><span>第 {{Math.floor(offset/100)+1}} 页 · 每页最多 100 步</span><button class="button secondary compact" data-testid="subtree-plan-next" :disabled="busy||disabled||!fresh||!more" @click="load(offset+100)">下一页步骤</button></footer></div></section></template>
<style scoped>.subtree-plan{border-top:1px solid #dce5ef;margin-top:16px;padding-top:12px}header,footer{display:flex;gap:10px;align-items:center;justify-content:space-between;flex-wrap:wrap}.table-wrap{overflow:auto}table{width:100%;border-collapse:collapse;font-size:12px;text-align:left}th,td{padding:8px 10px;border-bottom:1px solid #e4eaf2;vertical-align:top;overflow-wrap:anywhere}th{background:#f5f7fb;white-space:nowrap}small{display:block;color:#65748b;margin-top:4px}footer{margin-top:10px}</style>
