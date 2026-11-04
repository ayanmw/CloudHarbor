<script setup lang="ts">
import {formatBytes} from './format-bytes';
import {onMounted,onUnmounted,ref,watch} from 'vue';
import {validCloudDirectory} from './cloud-files-route';
import type {CleanupEntry as Entry,CleanupCandidate} from './cleanup-types';
const props=defineProps<{disabled:boolean}>(),emit=defineEmits<{authenticationRequired:[];selection:[CleanupCandidate[]]}>();
type Group={sha256:string;size:string;keep:Entry;delete:Entry[]};
const groups=ref<Group[]>([]),total=ref('0'),deleteCount=ref('0'),offset=ref('0'),next=ref<string|null>(null),loading=ref(false),loaded=ref(false),error=ref(''),observed=ref('');
const retained=ref<Record<string,string>>({}),selected=ref<Record<string,boolean>>({}),shown=ref<Record<string,number>>({});
const key=(group:Group)=>group.sha256+':'+group.size;
const entries=(group:Group)=>[group.keep,...group.delete];
const visible=(group:Group)=>entries(group).slice(0,shown.value[key(group)]||20);
function clearSelection(){selected.value={};emit('selection',[]);}
function publish(){const out:CleanupCandidate[]=[];if(!error.value&&!loading.value){for(const group of groups.value){const keep=entries(group).find(e=>e.fs_id===(retained.value[key(group)]||group.keep.fs_id))!;for(const entry of entries(group)){if(selected.value[entry.fs_id]&&entry.fs_id!==keep.fs_id)out.push({kind:'duplicate',entry,retained:keep,sha256:group.sha256,observedAt:observed.value});}}}emit('selection',out);}
function keepCopy(group:Group,id:string){retained.value[key(group)]=id;delete selected.value[id];publish();}
function toggle(entry:Entry,event:Event){selected.value[entry.fs_id]=(event.target as HTMLInputElement).checked;publish();}
function selectVisible(){for(const group of groups.value){const keep=retained.value[key(group)]||group.keep.fs_id;for(const entry of visible(group))if(entry.fs_id!==keep)selected.value[entry.fs_id]=true;}publish();}
const exact=(x:unknown)=>typeof x==='string'&&/^(0|[1-9][0-9]*)$/.test(x)&&x.length<=19&&BigInt(x)<=9223372036854775807n;
const validEntry=(e:any,size:string)=>e&&exact(e.fs_id)&&e.fs_id!=='0'&&e.directory===false&&exact(e.size)&&e.size===size&&exact(e.provider_mtime)&&typeof e.path==='string'&&validCloudDirectory(e.path)&&typeof e.name==='string'&&typeof e.content_verified_at_utc==='string'&&e.content_verified_at_utc.endsWith('Z')&&Number.isFinite(Date.parse(e.content_verified_at_utc));
let active=true,generation=0,controller:AbortController|undefined;
function stop(){clearSelection();active=false;generation++;controller?.abort();loading.value=false;}
async function load(at='0'){
 if(!active||props.disabled||loading.value)return;clearSelection();const request=++generation;controller=new AbortController();loading.value=true;error.value='';
 try{const response=await fetch('/api/cloud-cleanup/duplicates?identity_format=string&limit=10&offset='+at,{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;if(response.status===401){stop();emit('authenticationRequired');return;}if(!response.ok)throw Error(await response.text()||'重复候选暂不可读');const data=await response.json();if(request!==generation)return;
 if(!data||!Array.isArray(data.groups)||data.groups.length>10||!exact(data.total)||!exact(data.delete_count)||data.offset!==at||data.limit!==10||(data.next_offset!==null&&(!exact(data.next_offset)||BigInt(data.next_offset)<=BigInt(at))))throw Error('重复候选分页无效');
 for(const group of data.groups){if(!group||typeof group.sha256!=='string'||!/^[a-f0-9]{64}$/.test(group.sha256)||!exact(group.size)||!validEntry(group.keep,group.size)||!Array.isArray(group.delete)||!group.delete.length||!group.delete.every((e:any)=>validEntry(e,group.size)))throw Error('重复组身份或校验证据无效');const ids=[group.keep,...group.delete].map(e=>e.fs_id);if(new Set(ids).size!==ids.length)throw Error('重复组中出现相同文件身份');}
 retained.value={};shown.value={};groups.value=data.groups;total.value=data.total;deleteCount.value=data.delete_count;offset.value=at;next.value=data.next_offset;loaded.value=true;observed.value=new Date().toISOString();
 }catch(e:any){if(request===generation)error.value=e.message||'重复候选读取失败';}finally{if(request===generation)loading.value=false;}
}
function restore(event:PageTransitionEvent){if(event.persisted&&!props.disabled){active=true;if(loaded.value)load();}}
watch(()=>props.disabled,value=>{if(value)stop();});
onMounted(()=>{window.addEventListener('pagehide',stop);window.addEventListener('pageshow',restore);});onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',restore);});
async function refreshAfterMutation(){if(!loaded.value)return active&&!props.disabled;generation++;controller?.abort();loading.value=false;await load();return active&&!props.disabled&&!error.value;}
defineExpose({refreshAfterMutation});
</script>
<template><section class="event-results"><header class="page-header"><div><h2>已校验的重复文件</h2><p class="muted">读取本地缓存与内容 SHA-256。大小相同或网盘 MD5 相同不会直接作为删除凭据。</p></div><button class="button secondary" data-testid="cleanup-preview" :disabled="disabled||loading" @click="load()">{{loaded?'刷新预览':'读取重复预览'}}</button></header><p v-if="loading" role="status">正在读取本地候选…</p><p v-if="error" class="notice danger" role="alert">{{error}}<span v-if="loaded"> · 下方保留上次读取结果，尚未重新确认。</span></p><template v-if="loaded"><p>重复组 {{total}} · 默认多余副本 {{deleteCount}} · 当前组偏移 {{offset}}</p><p class="muted">保留选择仅对当前预览生效；刷新或翻页会清空选择，实际执行前还需核对远端。</p><button class="button secondary" data-testid="duplicate-select-visible" :disabled="disabled||loading||!!error||!groups.length" @click="selectVisible">选择当前可见多余副本</button><button class="button secondary" :disabled="disabled||loading" @click="clearSelection">清空选择</button><p v-if="!groups.length">暂无已校验重复组；这不证明全盘没有重复内容。</p><article v-for="group in groups" :key="key(group)" class="cleanup-group"><p class="muted">SHA-256：{{group.sha256}} · 单份 {{formatBytes(group.size)}}</p><div class="event-table-scroll"><table><thead><tr><th>保留此份</th><th>待删除</th><th>文件 / 网盘路径</th><th>大小</th><th>内容校验 UTC</th></tr></thead><tbody><tr v-for="entry in visible(group)" :key="entry.fs_id"><td><input type="radio" data-testid="duplicate-keep" :name="'keep-'+key(group)" :value="entry.fs_id" :aria-label="'保留 '+entry.path" :checked="entry.fs_id===(retained[key(group)]||group.keep.fs_id)" :disabled="disabled||loading||!!error" @change="keepCopy(group,entry.fs_id)"/></td><td><input type="checkbox" data-testid="duplicate-delete" :aria-label="'选择删除 '+entry.path" :checked="!!selected[entry.fs_id]" :disabled="disabled||loading||!!error||entry.fs_id===(retained[key(group)]||group.keep.fs_id)" @change="toggle(entry,$event)"/></td><td>{{entry.name}}<br><span class="muted">{{entry.path}}</span></td><td>{{formatBytes(entry.size)}}</td><td>{{entry.content_verified_at_utc}}</td></tr></tbody></table></div><button v-if="visible(group).length<group.delete.length+1" class="button secondary" :disabled="disabled||loading" @click="shown[key(group)]=visible(group).length+20">再显示 20 份副本</button><p class="muted">本组 {{group.delete.length+1}} 份，当前显示 {{visible(group).length}} 份；批量选择只包含当前可见项。</p></article><button class="button secondary" :disabled="disabled||loading||offset==='0'" @click="load()">回到首批</button><button v-if="next" class="button secondary" :disabled="disabled||loading" @click="load(next)">下一批重复组</button></template><p v-if="observed" class="muted">本机读取 UTC：{{observed}}</p></section></template>

<style scoped>th:nth-child(1),th:nth-child(2){width:58px}</style>
