<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {cloudMutationRejection} from './cloud-mutation-rejection';
import type {CloudActionResult} from './cloud-files-types';
const props=defineProps<{directory:string;afterSuccess:(path:string)=>Promise<void>}>(),emit=defineEmits<{close:[];busy:[boolean,string?,boolean?];confirmed:[CloudActionResult];unknown:[CloudActionResult];authenticationRequired:[]}>();
const panel=ref<HTMLElement|null>(null),nameInput=ref<HTMLInputElement|null>(null);
const focusResult=()=>queueMicrotask(()=>panel.value?.querySelector<HTMLElement>('button:not(:disabled)')?.focus());
const parent=props.directory,name=ref(''),ack=ref(false),pending=ref(false),consumed=ref(false),status=ref(''),message=ref(''),operationID=ref('');
const target=computed(()=>(parent==='/'?'':parent)+'/'+name.value),valid=computed(()=>!!name.value.trim()&&!['.','..'].includes(name.value)&&!/[\\/\u0000-\u001f\u007f]/.test(name.value)&&new Blob([name.value]).size<=255);
watch(name,()=>ack.value=false);let active=true,generation=0;
function uncertain(){status.value='unknown';message.value='新建目录结果未知，外层项目尚未发送；请核对操作记录，不要重发。';emit('unknown',{status:'unknown',operation_id:operationID.value||undefined,message:message.value,source:parent,target:target.value});}
async function refreshTarget(request:number){try{await props.afterSuccess(target.value);if(active&&request===generation)message.value='新建已确认，请选择此目录后重新核对外层操作。';}catch{if(active&&request===generation)message.value='新建已确认，但目录刷新未完成；可重新读取，勿重复新建。';}}
async function submit(){
 if(!active||pending.value||consumed.value||!ack.value||!valid.value)return;const request=++generation,fixedTarget=target.value;pending.value=true;consumed.value=true;emit('busy',true,'新建目录 '+fixedTarget);
 try{const response=await fetch('/api/cloud/mutations?identity_format=string',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({operation:'mkdir',destination_directory:parent,new_name:name.value})});if(!active||request!==generation)return;
  // An explicit auth rejection must release busy before the owner leaves for login.
  if(response.status===401){status.value='failed';pending.value=false;emit('busy',false);emit('authenticationRequired');return;}if(response.status>=500||response.status===408)throw Error('未取得回执');
  const data=await response.json().catch(e=>{if(response.status===400)return null;throw e});if(!active||request!==generation)return;
  const rejection=cloudMutationRejection(response.status,data);if(rejection){status.value='failed';message.value=rejection;return;}if(data?.outcome!==undefined)throw Error('拒绝回执无效');if(response.status===400){status.value='failed';message.value='名称或路径被拒绝，本次未执行。请关闭表单并重新核对。';return;}
  const result=response.ok?data:data?.operation;
  if(!result||typeof result.operation_id!=='string'||!/^[a-f0-9]{32}$/.test(result.operation_id)||result.object_id!=='0'||(result.source_path||'')!==''||result.destination_path!==fixedTarget||!['confirmed','failed','unknown'].includes(result.status))throw Error('回执不匹配');
  operationID.value=result.operation_id;status.value=result.status;
  if(result.status==='unknown'){uncertain();return;}if(result.status==='failed'){message.value=result.warning||'新建失败，请核对目标后重新选择。';return;}
  message.value='新建已确认，正在读取新目录…';emit('busy',true,'读取已新建目录 '+fixedTarget,true);emit('confirmed',{status:'confirmed',operation_id:operationID.value,message:'新建目录已确认；外层项目尚未发送。',source:parent,target:fixedTarget});await refreshTarget(request);
 }catch{if(active&&request===generation)uncertain();}finally{if(active&&request===generation){pending.value=false;emit('busy',false);focusResult();}}
}
async function recheck(){if(!active||pending.value||status.value!=='confirmed')return;const request=++generation;pending.value=true;emit('busy',true,'读取新目录 '+target.value,true);await refreshTarget(request);if(active&&request===generation){pending.value=false;emit('busy',false);focusResult();}}
function leave(){if(!active)return;active=false;generation++;if(pending.value&&!status.value)uncertain();pending.value=false;emit('busy',false);focusResult();}
onMounted(()=>{nameInput.value?.focus();window.addEventListener('pagehide',leave)});onUnmounted(()=>{leave();window.removeEventListener('pagehide',leave)});
</script>
<template><section ref="panel" class="picker-create" data-testid="cloud-picker-create" aria-label="新建目标目录"><header><strong>新建目标目录</strong><button type="button" class="button secondary" :disabled="pending" @click="emit('close')">返回目录浏览</button></header><p class="muted">这是独立的新建操作，不会自动执行外层操作；同名目录拒绝覆盖。</p><label>目录名称<input ref="nameInput" data-testid="cloud-picker-create-name" v-model="name" :disabled="pending||consumed"/></label><p>目标：{{target}}</p><label v-if="!consumed"><input type="checkbox" data-testid="cloud-picker-create-ack" v-model="ack"/>已核对新建位置和名称</label><button v-if="!consumed" type="button" class="button primary" data-testid="cloud-picker-create-submit" :disabled="pending||!ack||!valid" @click="submit">确认新建目录</button><p v-if="message" role="status">{{message}}</p><button v-if="status==='confirmed'" type="button" class="button secondary" :disabled="pending" @click="recheck">重新读取新目录</button><a v-if="operationID" class="button secondary" :href="'/operations?operation_id='+operationID">查看新建操作记录</a></section></template>
<style scoped>.picker-create{border:1px solid #cbd8ed;border-radius:8px;background:white;margin:12px 0;padding:12px}.picker-create header{display:flex;justify-content:space-between;align-items:center;gap:8px}.picker-create input:not([type=checkbox]){display:block;box-sizing:border-box;width:100%;padding:8px;border:1px solid #cbd5e5;border-radius:6px}.picker-create p{overflow-wrap:anywhere}.picker-create .button{margin:4px 6px 4px 0}</style>
