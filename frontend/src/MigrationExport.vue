<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import type {MigrationFilters} from './migration-route';
const props=defineProps<{id?:string;filters?:MigrationFilters;disabled?:boolean}>(),emit=defineEmits<{authenticationRequired:[]}>();
const loading=ref(false),result=ref(''),active=ref(true);
const url=computed(()=>{if(props.id)return '/api/settings/local-migrations/'+encodeURIComponent(props.id)+'/export';const params=new URLSearchParams();for(const [key,value] of Object.entries(props.filters||{}))if(value)params.set(key,value);return '/api/settings/local-migrations/export'+(params.size?'?'+params:'');});
let generation=0,controller:AbortController|undefined;
function reset(){generation++;controller?.abort();loading.value=false;result.value='';}
watch(url,reset,{flush:'sync'});
watch(()=>props.disabled,value=>{if(value)reset();},{flush:'sync'});
async function download(){
 if(!active.value||loading.value||props.disabled)return;const request=++generation,id=props.id,filters={...props.filters};controller=new AbortController();loading.value=true;result.value='';
 try{const response=await fetch(url.value,{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){stop();emit('authenticationRequired');return;}
  if(!response.ok)throw Error(await response.text()||'导出失败');const text=await response.text();if(request!==generation)return;
  const data=JSON.parse(text);
  if(id?data?.migration?.id!==id:!data?.filter||!Object.entries(filters).every(([key,value])=>(data.filter[key==='q'?'query':key]||'')===value)||!Number.isSafeInteger(data.total)||data.total<0||data.total>5000||!Array.isArray(data.migrations)||data.migrations.length!==data.total)throw Error('导出内容与所选记录或筛选条件不一致');
  // Preserve raw evidence integers; JSON.parse above checks identity only.
  const link=document.createElement('a'),blobURL=URL.createObjectURL(new Blob([text],{type:'application/json'}));link.href=blobURL;link.download=id?'local-migration-'+id+'.json':'local-migration-history.json';document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(blobURL),1000);result.value='导出已开始下载。';
 }catch(e:any){if(request===generation)result.value=e.message||'导出失败';}
 finally{if(request===generation)loading.value=false;}
}
function stop(){active.value=false;reset();}
function resume(event:PageTransitionEvent){if(event.persisted)active.value=true;}
onMounted(()=>{window.addEventListener('pagehide',stop);window.addEventListener('pageshow',resume);});
onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',resume);});
</script>
<template><section data-testid="migration-export-panel"><button class="button secondary" data-testid="migration-export-start" :disabled="loading||disabled||!active" @click="download">{{loading?'正在导出…':id?'导出此记录证据':'导出已提交筛选的摘要'}}</button><p class="muted">{{id?'完整证据最多100条事件，超限不生成部分文件。':'按已提交条件导出，最多5000条摘要，超限不生成部分文件。'}}</p><p v-if="result" data-testid="migration-export-result" role="status">{{result}}</p></section></template>
