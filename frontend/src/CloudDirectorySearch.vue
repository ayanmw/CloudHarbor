<script setup lang="ts">
import {onMounted,onUnmounted,ref,watch} from 'vue';
import {Folder} from 'lucide-vue-next';
import {validCloudDirectory} from './cloud-files-route';
const props=defineProps<{forbidden?:string[];disabled?:boolean}>(),emit=defineEmits<{navigate:[string];close:[];authenticationRequired:[]}>();
const draft=ref(''),query=ref(''),rows=ref<{fs_id:string;path:string;name:string;directory:boolean}[]>([]),loading=ref(false),ready=ref(false),error=ref(''),notice=ref(''),coverage=ref(''),observed=ref(''),cacheGeneration=ref(''),offset=ref(0),total=ref(0),next=ref<number|null>(null),input=ref<HTMLInputElement|null>(null);
let active=true,generation=0,controller:AbortController|undefined;
const integer=(value:unknown)=>typeof value==='string'&&/^(0|[1-9][0-9]*)$/.test(value)&&value.length<=19&&BigInt(value)<=9223372036854775807n;
function forbidden(path:string){return (props.forbidden||[]).some(parent=>path===parent||path.startsWith(parent+'/'));}
function cancel(){generation++;controller?.abort();loading.value=false;}
watch(draft,()=>{cancel();rows.value=[];ready.value=false;error.value='';notice.value='';});
async function search(start=0,recover=true,fixedQuery?:string){
 if(!active||props.disabled||loading.value)return;const term=(fixedQuery??draft.value).trim();if(!term||Array.from(term).length>128){error.value='搜索内容须为1至128个字符。';return;}
 if(start&&!integer(cacheGeneration.value)){error.value='缺少缓存代次，请重新搜索。';return;}
 const request=++generation;controller?.abort();controller=new AbortController();loading.value=true;ready.value=false;rows.value=[];error.value='';query.value=term;
 const q=new URLSearchParams({q:term,directories_only:'1',offset:String(start),limit:'50',identity_format:'string'});if(start)q.set('cache_generation',cacheGeneration.value);
 try{const response=await fetch('/api/cloud-inventory/search?'+q,{credentials:'same-origin',signal:controller.signal});if(!active||request!==generation)return;
  if(response.status===401){cancel();emit('authenticationRequired');return;}
  if(response.status===409){const conflict=await response.json();if(!active||request!==generation)return;if(conflict.code==='cache_generation_changed'&&recover){notice.value='缓存已更新，已重新读取第一页。';loading.value=false;await search(0,false,term);return;}throw Error('缓存持续变化或搜索条件已失效，请重新搜索。');}
  if(!response.ok)throw Error('无法读取缓存目录搜索结果，请稍后重试。');const data=await response.json();if(!active||request!==generation)return;
  if(data?.directories_only!==true||data.query!==term||data.offset!==start||!integer(data.cache_generation)||!data.coverage||typeof data.coverage.status!=='string'||!data.coverage.status||!Number.isSafeInteger(data.total)||data.total<0||!Array.isArray(data.entries)||data.entries.length!==Math.min(50,Math.max(0,data.total-start)))throw Error('缓存目录搜索响应无效。');
  const expectedNext=data.total-start>50?start+50:null;
  if((data.next_offset??null)!==expectedNext||expectedNext!==null&&expectedNext>1000000||data.entries.some((entry:any)=>!entry||!integer(entry.fs_id)||entry.fs_id==='0'||entry.directory!==true||!validCloudDirectory(entry.path)||typeof entry.name!=='string'||!entry.name||entry.path.split('/').at(-1)!==entry.name)||new Set(data.entries.map((i:any)=>i.fs_id)).size!==data.entries.length||new Set(data.entries.map((i:any)=>i.path)).size!==data.entries.length)throw Error('缓存目录搜索分页或身份无效。');
  const timestamp=data.coverage.scan_updated_at_utc??'';if(typeof timestamp!=='string'||timestamp&&(!/Z$/.test(timestamp)||!Number.isFinite(Date.parse(timestamp))))throw Error('缓存观察时间无效。');
  rows.value=data.entries;offset.value=start;total.value=data.total;next.value=expectedNext;cacheGeneration.value=data.cache_generation;coverage.value=data.coverage.status;observed.value=timestamp;ready.value=true;
 }catch(e:any){if(active&&request===generation)error.value=e.message||'搜索失败，请重新搜索。';}finally{if(request===generation)loading.value=false;}
}
function open(path:string){if(active&&!loading.value&&!props.disabled&&ready.value&&!error.value&&!forbidden(path))emit('navigate',path);}
function leave(){active=false;cancel();rows.value=[];ready.value=false;}
function resume(event:PageTransitionEvent){if(event.persisted){active=true;notice.value='页面已恢复，请重新搜索以核对缓存。';}}
watch(()=>props.disabled,value=>{if(value){cancel();rows.value=[];ready.value=false;}});
onMounted(()=>{input.value?.focus();window.addEventListener('pagehide',leave);window.addEventListener('pageshow',resume)});onUnmounted(()=>{leave();window.removeEventListener('pagehide',leave);window.removeEventListener('pageshow',resume)});
</script>
<template><section class="picker-search" data-testid="cloud-directory-search" aria-label="搜索已缓存目录"><header><strong>搜索已缓存目录</strong><button class="button secondary" type="button" @click="emit('close')">返回目录浏览</button></header><p class="muted">只搜索本地已缓存的目录，不会扫描网盘。结果可能过时；打开后仍需在线读取，才能选为目标。</p><form @submit.prevent="search()"><label>目录名称或路径<input ref="input" v-model="draft" data-testid="cloud-picker-search-query" :disabled="disabled"/></label><button class="button primary" data-testid="cloud-picker-search-submit" :disabled="loading||disabled">搜索目录</button></form><p v-if="loading" role="status">正在搜索缓存目录…</p><p v-if="error" role="alert" class="notice danger">{{error}} 上次结果不可使用。</p><p v-if="notice" role="status">{{notice}}</p><p v-if="ready" class="muted">{{coverage==='complete'?'缓存已发布，仍可能过时':'缓存不完整，仅展示已知目录'}} · 代次 {{cacheGeneration}} · {{total}} 项<span v-if="observed"> · 最近扫描 UTC：{{observed}}</span></p><ul><li v-for="item in rows" :key="item.fs_id"><button type="button" class="button secondary" data-testid="cloud-picker-search-result" :disabled="loading||disabled||!!error||forbidden(item.path)" :title="forbidden(item.path)?'不能选择项目自身或子目录':item.path" @click="open(item.path)"><span><Folder :size="15"/> {{item.name}}</span><small>{{item.path}}</small></button></li></ul><p v-if="ready&&!rows.length">缓存中没有匹配目录，不代表网盘中不存在。</p><div v-if="ready" class="toolbar"><button type="button" class="button secondary" :disabled="loading||disabled||!!error||offset===0" @click="search(Math.max(0,offset-50),true,query)">上一页</button><button type="button" class="button secondary" :disabled="loading||disabled||!!error||next===null" @click="search(next!,true,query)">下一页</button></div></section></template>
<style scoped>.picker-search{border-top:1px solid #d6dfed;margin-top:12px;padding-top:12px}.picker-search header,.picker-search form{display:flex;align-items:center;flex-wrap:wrap;gap:8px;justify-content:space-between}.picker-search label{flex:1}.picker-search input{display:block;box-sizing:border-box;width:100%;border:1px solid #cbd5e5;border-radius:6px;padding:8px}.picker-search ul{list-style:none;padding:0;max-height:240px;overflow:auto}.picker-search li{margin:6px 0}.picker-search li button{width:100%;display:flex;align-items:flex-start;flex-direction:column;text-align:left;white-space:normal;overflow-wrap:anywhere}.picker-search small{font-weight:normal;color:#65718a}</style>
