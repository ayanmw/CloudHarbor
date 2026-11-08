<script setup lang="ts">
import {onUnmounted,ref} from 'vue';
import {enumerateUploadDirectory,type DirectorySelection,type UploadDirectoryHandle} from './upload-directory-selection';
const props=defineProps<{disabled:boolean}>(),emit=defineEmits<{selection:[DirectorySelection];busy:[boolean]}>();
const picker=(window as Window&{showDirectoryPicker?:(options:{mode:'read'})=>Promise<UploadDirectoryHandle>}).showDirectoryPicker;
const available=typeof picker==='function'&&window.isSecureContext===true,busy=ref(false),status=ref(''),error=ref('');
let generation=0,controller:AbortController|undefined;
function cancel(){generation++;controller?.abort();busy.value=false;status.value='目录检查已取消；未提交清单。';emit('busy',false);}
async function choose(){
 if(props.disabled||busy.value||!available)return;
 const current=++generation;controller=new AbortController();const signal=controller.signal;busy.value=true;error.value='';status.value='等待选择只读目录…';emit('busy',true);
 try{const root=await picker!.call(window,{mode:'read'});if(current!==generation)return;const selection=await enumerateUploadDirectory(root,signal,(files,directories)=>{if(current===generation)status.value=`正在检查：${files} 个文件 · ${directories} 个目录`;});if(current!==generation)return;status.value='目录检查完成；请核对清单与目标后提交。';emit('selection',selection);}
 catch(e:any){if(current!==generation)return;if(e?.name==='AbortError')status.value='已取消目录选择；未提交清单。';else{error.value=e?.message||'目录读取失败';status.value='目录检查未完成，未提交残缺清单；原选择保持不变。';}}
 finally{if(current===generation){busy.value=false;emit('busy',false);}}
}
function leave(){generation++;controller?.abort();busy.value=false;emit('busy',false);}
window.addEventListener('pagehide',leave);window.addEventListener('popstate',leave);
onUnmounted(()=>{leave();window.removeEventListener('pagehide',leave);window.removeEventListener('popstate',leave);});
</script>
<template><section data-testid="upload-directory-selection"><button type="button" class="button secondary" data-testid="upload-complete-directory" :disabled="disabled||busy||!available" @click="choose">选择完整目录（含空目录）</button><button v-if="busy" type="button" class="button secondary" data-testid="upload-enumeration-cancel" @click="cancel">取消检查</button><p v-if="!available" class="muted">完整目录选择需浏览器支持并通过 HTTPS 或可信本机地址访问；仍可使用上方普通目录上传，但不包含空目录。</p><p role="status">{{status}}</p><p v-if="error" role="alert" class="notice danger">{{error}}</p></section></template>
