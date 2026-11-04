<script setup lang="ts">
import {formatBytes} from './format-bytes';
import {computed,onUnmounted,ref,watch} from 'vue';
import {vModal} from './modal';
import {validCloudDirectory} from './cloud-files-route';
import type {CloudFileAction,CloudActionResult} from './cloud-files-types';
const props=defineProps<{action:CloudFileAction;minimized:boolean}>(),emit=defineEmits<{close:[];minimize:[];busy:[boolean];result:[CloudActionResult];authenticationRequired:[]}>();
const names={mkdir:'新建文件夹',rename:'重命名',move:'移动项目',delete:'删除网盘项目'};
const name=ref(props.action.operation==='rename'?props.action.item!.name:''),destination=ref(props.action.directory),ack=ref(false),pending=ref(false),consumed=ref(false),error=ref('');
const target=computed(()=>{const a=props.action;if(a.operation==='delete')return '删除此项目'+(a.item?.directory?'及其全部内容':'');const parent=a.operation==='rename'?a.item!.path.slice(0,a.item!.path.lastIndexOf('/'))||'/':destination.value;const leaf=a.operation==='move'?(name.value||a.item!.name):name.value;return (parent==='/'?'':parent)+'/'+leaf;});
const validName=(value:string)=>!!value.trim()&&!['.','..'].includes(value)&&!/[\\/\u0000-\u001f\u007f]/.test(value)&&new Blob([value]).size<=255;
const valid=computed(()=>{const op=props.action.operation;if(op==='delete')return true;if(op==='move')return validCloudDirectory(destination.value)&&(!name.value||validName(name.value))&&target.value!==props.action.item!.path;return validName(name.value)&&(op!=='rename'||target.value!==props.action.item!.path);});
watch([name,destination],()=>{ack.value=false;error.value='';});
let active=true;
function close(){if(!pending.value)emit('close');}
async function submit(){
 if(!active||pending.value||consumed.value||!ack.value||!valid.value)return;
 const a=props.action,item=a.item,source=item?.path||a.directory,fixedTarget=target.value;
 const body:any={operation:a.operation};if(item)body.expected={fs_id:item.fs_id,path:item.path,server_filename:item.name,size:item.provider_size,isdir:item.directory?1:0,server_mtime:item.server_mtime,md5:item.md5||''};
 if(a.operation==='move'||a.operation==='mkdir')body.destination_directory=destination.value;
 if(a.operation!=='delete'&&name.value)body.new_name=name.value;
 pending.value=true;consumed.value=true;emit('busy',true);
 try{const response=await fetch('/api/cloud/mutations?identity_format=string',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(!active)return;
  if(response.status===401){emit('authenticationRequired');return;}
  if(response.status===400){error.value='输入或身份格式被拒绝，请关闭后刷新目录并重新确认。';return;}
  if(response.status>=500||response.status===408)throw Error('结果未知');
  const data=await response.json();if(!active)return;const result=response.ok?data:data.operation;
  if(!result||typeof result.operation_id!=='string'||!/^[a-f0-9]{32}$/.test(result.operation_id)||!['confirmed','failed','unknown'].includes(result.status))throw Error('未获得确定回执');
  if(typeof result.object_id!=='string'||!/^(0|[1-9][0-9]*)$/.test(result.object_id)||result.object_id.length>19||BigInt(result.object_id)>9223372036854775807n||(item&&result.object_id!==item.fs_id)||(result.source_path||'')!==(item?.path||'')||(result.destination_path||'')!==(a.operation==='delete'?'':fixedTarget))throw Error('操作回执与确认对象不一致');
  emit('result',{status:result.status,operation_id:result.operation_id,message:result.warning||data.message||(result.status==='confirmed'?'网盘操作已确认。':'请到操作记录核对，勿重复提交。'),source,target:fixedTarget});emit('close');
 }catch{if(active){emit('result',{status:'unknown',message:'操作结果未知，可能已执行；请查看操作记录，不要重复提交。',source,target:fixedTarget});emit('close');}}
 finally{pending.value=false;emit('busy',false);}
}
onUnmounted(()=>{active=false;});
</script>
<template><div v-show="!minimized" class="modal-backdrop" @click.self="close"><section class="detail-drawer" role="dialog" aria-modal="true" aria-labelledby="cloud-action-title" v-modal="close"><header class="page-header"><h2 id="cloud-action-title">{{names[action.operation]}}</h2><button class="button secondary" data-testid="cloud-action-cancel" :disabled="pending" @click="close">取消</button></header><p class="notice">保存前服务端会暂停下载队列并重新核对远端身份；结果未知时不会自动重发。</p><dl class="path-settings-overview"><div><dt>来源 / 当前目录</dt><dd>{{action.item?.path||action.directory}}</dd><template v-if="action.item"><dt>精确文件 ID</dt><dd>{{action.item.fs_id}}</dd><dt>网盘原始大小</dt><dd>{{formatBytes(action.item.provider_size)}}</dd></template></div><div><dt>操作目标</dt><dd>{{target}}</dd><dt>列表观察 UTC</dt><dd>{{action.observedAt||'未知'}}</dd></div></dl><label v-if="action.operation==='mkdir'||action.operation==='move'" class="path-target-label">目标目录<input data-testid="cloud-action-destination" v-model="destination" :disabled="pending||consumed"/></label><label v-if="action.operation!=='delete'" class="path-target-label">{{action.operation==='move'?'目标名称（空则保留原名）':'名称'}}<input v-model="name" data-testid="cloud-action-name" :disabled="pending||consumed"/></label><label><input v-model="ack" data-testid="cloud-action-ack" type="checkbox" :disabled="pending||consumed"/>我已核对来源、目标及{{action.operation==='delete'?'删除范围':'操作内容'}}</label><p v-if="error" class="notice danger" role="alert">{{error}}</p><button class="button primary" data-testid="cloud-action-submit" :disabled="pending||consumed||!ack||!valid" @click="submit">{{pending?'正在核对并执行…':'确认执行'}}</button><button v-if="pending" class="button secondary" data-testid="cloud-action-minimize" @click="emit('minimize')">收起并继续浏览</button><p><a href="/operations">查看操作记录</a></p></section></div></template>
