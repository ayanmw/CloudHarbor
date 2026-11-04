<script setup lang="ts">
import {computed,onUnmounted,ref,watch} from 'vue';
import {vModal} from './modal';
import {validCloudDirectory} from './cloud-files-route';
const props=defineProps<{kind:'scan'|'refresh';state:{run_id:string;status:string;worker_active:boolean;root_path?:string;pending_count:string;request_budget:string};disabled:boolean}>();
const emit=defineEmits<{changed:[];authenticationRequired:[]}>();
const directory=ref(new URLSearchParams(location.search).get('directory')||'/'),mode=ref(''),budget=ref('1000'),ack=ref(false),pending=ref(false),message=ref('');
const confirmation=ref<{run:string;method:string;directory:string;mode:string;budget:number}|null>(null);
const resumable=computed(()=>(props.kind==='refresh'?['running','paused'].includes(props.state.status):!['idle','complete'].includes(props.state.status))&&props.state.pending_count!=='0');
const valid=computed(()=>props.kind==='scan'||validCloudDirectory(resumable.value?props.state.root_path||'':directory.value));
let active=true,consumed=false;
// A new checkpoint must not inherit a choice made for another run.
watch(()=>props.state.run_id,()=>{mode.value='';budget.value='1000';});
function prepare(method:string){
 if(props.disabled||pending.value)return;
 const chosen=mode.value||(resumable.value?'':'unlimited');
 if(method==='POST'&&(!valid.value||(chosen==='limited'&&(!/^[1-9][0-9]*$/.test(budget.value)||Number(budget.value)>100000)))){message.value='请输入规范绝对路径；有限请求次数须为 1 至 100000。';return;}
 ack.value=false;consumed=false;message.value='';confirmation.value={run:props.state.run_id,method,directory:props.kind==='refresh'&&resumable.value?props.state.root_path||directory.value:directory.value,mode:chosen,budget:chosen==='limited'?Number(budget.value):0};
}
function close(){if(!pending.value)confirmation.value=null;}
async function submit(){
 const fixed=confirmation.value;if(!fixed||!ack.value||pending.value||consumed||props.disabled)return;
 consumed=true;pending.value=true;
 try{const response=await fetch('/api/cloud-inventory/'+props.kind+(fixed.method==='DELETE'?'?expected_run_id='+encodeURIComponent(fixed.run):''),{method:fixed.method,credentials:'same-origin',headers:{'Content-Type':'application/json'},...(fixed.method==='POST'?{body:JSON.stringify({expected_run_id:fixed.run,request_limit_mode:fixed.mode,request_budget:fixed.budget,...(props.kind==='refresh'?{directory:fixed.directory}:{})})}:{})});if(!active)return;
 if(response.status===401){emit('authenticationRequired');return;}
 if(response.status>=500||response.status===408)throw Error('unknown');
 if(!response.ok){message.value='请求被拒绝：'+(await response.text());return;}
 message.value=fixed.method==='DELETE'?'暂停请求已接收，正在读回检查点；执行器退出前仍可能完成当前请求。':'开始或恢复请求已接收，正在读回实际运行状态。';
 }catch{if(active)message.value='请求结果未知，可能已执行；已停止重复提交，请先刷新状态核对。';}
 finally{pending.value=false;if(active){confirmation.value=null;emit('changed');}}
}
onUnmounted(()=>{active=false;});
</script>
<template><div class="cache-controls"><template v-if="!state.worker_active"><label v-if="kind==='refresh'&&!resumable" class="path-target-label">刷新目录及子目录<input v-model="directory" :disabled="disabled||pending" placeholder="/目录"/></label><label class="path-target-label">请求策略<select v-model="mode" :disabled="disabled||pending"><option value="">{{resumable?'保留检查点原策略':'默认完整扫描'}}</option><option value="unlimited">扫描至完成</option><option value="limited">高级：限制本次新增请求次数</option></select></label><label v-if="mode==='limited'" class="path-target-label">新增请求次数<input v-model="budget" inputmode="numeric" :disabled="disabled||pending"/></label><p v-if="kind==='refresh'&&state.status==='failed'" class="notice">上次目录刷新失败。此次将创建新运行，默认扫描至完成，不恢复旧预算。</p><p v-if="resumable" class="muted">恢复原运行 {{state.run_id}}；原工具上限 {{state.request_budget==='0'?'不限':state.request_budget}}。有限模式追加次数，不重置已用次数。</p><button class="button primary" data-testid="cache-start" :disabled="disabled||pending" @click="prepare('POST')">{{resumable?'恢复扫描':'开始扫描'}}</button></template><button v-else class="button secondary" :disabled="disabled||pending" @click="prepare('DELETE')">暂停扫描</button><p v-if="message" class="notice" role="status">{{message}}</p><div v-if="confirmation" class="modal-backdrop"><section class="detail-drawer" role="dialog" aria-modal="true" aria-labelledby="cache-confirm-title" v-modal="close"><header class="page-header"><h2 id="cache-confirm-title">确认{{confirmation.method==='DELETE'?'暂停':'开始或恢复'}}{{kind==='scan'?'全盘扫描':'目录刷新'}}</h2><button class="button secondary" :disabled="pending" @click="close">取消</button></header><p>绑定运行：{{confirmation.run||'尚无运行记录'}}</p><p>范围：{{kind==='scan'?'/（整个账号）':confirmation.directory}}</p><p v-if="confirmation.method==='POST'">策略：{{confirmation.mode==='limited'?'新增 '+confirmation.budget+' 次请求':confirmation.mode==='unlimited'?'扫描至完成':'保留原检查点策略'}}。工具上限不是百度官方额度。</p><p v-if="kind==='scan'||confirmation.directory==='/'" class="notice">此操作覆盖整个账号的目录树，可能耗时较长。</p><label><input type="checkbox" v-model="ack" :disabled="pending"/>已核对运行、范围与请求策略</label><button class="button primary" data-testid="cache-confirm" :disabled="!ack||pending||disabled" @click="submit">{{pending?'提交中…':'确认执行'}}</button></section></div></div></template>
