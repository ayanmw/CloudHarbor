<script setup lang="ts">
import {formatBytes} from './format-bytes';
import {onMounted,onUnmounted,ref,watch} from 'vue';
const props=defineProps<{path:string;disabled:boolean}>(),emit=defineEmits<{authenticationRequired:[]}>();
type Size={path:string;bytes:string;files:string;directories:string;complete:boolean;observed_at_utc?:string;cached_at_utc:string};
const value=ref<Size|null>(null),loading=ref(false),error=ref(''),active=ref(true);
let generation=0,controller:AbortController|undefined;
const count=(item:unknown)=>typeof item==='string'&&/^(0|[1-9][0-9]*)$/.test(item)&&item.length<=19&&BigInt(item)<=9223372036854775807n;
const utc=(item:unknown)=>typeof item==='string'&&/Z$/.test(item)&&Number.isFinite(Date.parse(item));
function stop(){active.value=false;generation++;controller?.abort();loading.value=false;value.value=null;error.value='';}
watch(()=>props.disabled,disabled=>{if(disabled){generation++;controller?.abort();loading.value=false;value.value=null;error.value='';}});
async function load(){
 if(!active.value||props.disabled||loading.value)return;const request=++generation;controller=new AbortController();loading.value=true;
 try{const response=await fetch('/api/cloud-inventory/size?'+new URLSearchParams({dir:props.path,identity_format:'string'}),{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){stop();emit('authenticationRequired');return;}
  if(!response.ok)throw Error(await response.text()||'目录大小统计失败');const data=await response.json();if(request!==generation)return;
  if(!data||data.path!==props.path||!['bytes','files','directories'].every(key=>count(data[key]))||typeof data.complete!=='boolean'||!utc(data.cached_at_utc)||(data.observed_at_utc!==undefined&&!utc(data.observed_at_utc)))throw Error('目录大小响应无效');
  value.value=data;error.value='';
 }catch(e:any){if(request===generation)error.value=e.message||'目录大小统计失败';}
 finally{if(request===generation)loading.value=false;}
}
function resume(event:PageTransitionEvent){if(event.persisted)active.value=true;}
onMounted(()=>{window.addEventListener('pagehide',stop);window.addEventListener('pageshow',resume);});
onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',resume);});
</script>
<template><section class="event-results" aria-label="本地缓存目录大小"><button class="button secondary" data-testid="cloud-size-refresh" :disabled="loading||disabled||!active" @click="load">{{loading?'正在统计…':'重算本地缓存大小'}}</button><p class="muted">只统计本地已缓存的 {{path}} 及子目录文件；不会扫描远端。更新网盘列表缓存请使用高级管理中的目录刷新。</p><p v-if="error" class="notice danger" role="alert">{{error}}<span v-if="value"> · 下方为上次统计。</span></p><div v-if="value" data-testid="cloud-size-result"><p><strong :title="value.bytes+' B'">{{formatBytes(value.bytes)}}</strong> · {{value.files}} 个文件 · {{value.directories}} 个目录 · {{value.complete?'本地缓存范围完整，仍可能过时':'缓存不完整，大小仅为已知部分'}}</p><p class="muted">本地计算 UTC：{{value.cached_at_utc}}<template v-if="value.observed_at_utc&&!value.observed_at_utc.startsWith('0001-')"><br>远端观察 UTC：{{value.observed_at_utc}}</template></p></div></section></template>
