<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue';
import {Folder,File,FileText,FileCode,FileImage,FileVideo,FileAudio,FileArchive,Download} from 'lucide-vue-next';
import {formatBytes} from './format-bytes';
import CloudToolsNav from './CloudToolsNav.vue';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import CloudCacheStatus from './CloudCacheStatus.vue';
import CloudDirectorySize from './CloudDirectorySize.vue';
import CloudFileActions from './CloudFileActions.vue';
import type {CloudFileEntry as Entry,CloudFileAction,CloudActionResult} from './cloud-files-types';
import {readCloudFilesRoute,cloudFilesRouteURL,validCloudDirectory as validPath} from './cloud-files-route';
const cachePanel=ref(false);
const actionMinimized=ref(false);
const action=ref<CloudFileAction|null>(null),actionBusy=ref(false),actionResult=ref<CloudActionResult|null>(null);
let refreshAfterAction=false;
function chooseAction(operation:CloudFileAction['operation'],item?:Entry){if(loading.value||actionBusy.value||auth.value||error.value||actionResult.value?.status==='unknown'||loadedSource.value!=='live'||!ready.value)return;actionMinimized.value=false;action.value={operation,directory:directory.value,item:item?{...item}:undefined,observedAt:observed.value};}
function actionFinished(value:CloudActionResult){actionResult.value=value;refreshAfterAction=value.status==='confirmed';}
function actionBusyChanged(value:boolean){actionBusy.value=value;if(!value&&refreshAfterAction&&!loading.value){refreshAfterAction=false;refresh();}}
function inspectLive(item:Entry){source.value='live';open(item.path.slice(0,item.path.lastIndexOf('/'))||'/');}
const directory=ref('/'),draft=ref('/'),entries=ref<Entry[]>([]),loading=ref(false),error=ref(''),inputError=ref(''),sidebarError=ref(''),ready=ref(false),auth=ref(false),observed=ref('');
const source=ref('live'),loadedSource=ref('live'),cacheStatus=ref(''),cacheGeneration=ref('');
const queryDraft=ref(''),activeSearch=ref('');
const pageOffset=ref(0),nextOffset=ref<number|null>(null),pageNotice=ref('');
const boundaries=new Map<number,string>();
function page(offset:number){open(directory.value,activeSearch.value,offset,true);}
function search(){const query=queryDraft.value.trim();if(!query||Array.from(query).length>128){inputError.value='搜索内容须为1至128个字符。';return;}open(directory.value,query);}
function refresh(){open(directory.value,loadedSource.value==='cache'?activeSearch.value:'');}
const crumbs=computed(()=>[{name:'全部文件',path:'/'},...directory.value.split('/').filter(Boolean).map((name,index,parts)=>({name,path:'/'+parts.slice(0,index+1).join('/')}))]);
let active=true,generation=0,controller:AbortController|undefined;
function stop(){if(actionBusy.value)actionResult.value={status:'unknown',message:'离页时操作尚未确认，请查看操作记录，不要重复提交。',source:action.value?.item?.path||directory.value,target:''};actionBusy.value=false;action.value=null;active=false;generation++;controller?.abort();loading.value=false;}
function requireLogin(){auth.value=true;stop();error.value='管理员登录已失效，请重新登录。';location.assign('/login?next=%2Fcloud%2Ffiles');}
type RestoreOptions={source?:'live'|'cache';generation?:string;boundary?:string;replace?:boolean};
async function open(path=directory.value,query='',offset=0,continuation=false,recover=true,restore:RestoreOptions={}){
 if(!active||auth.value||loading.value)return;if(!validPath(path)){inputError.value='请输入规范的绝对网盘目录。';return;}if(!actionBusy.value)action.value=null;
 inputError.value='';const request=++generation,requestedSource=restore.source||(continuation?loadedSource.value:query?'cache':'live');controller=new AbortController();loading.value=true;
 const params=new URLSearchParams({offset:String(offset),limit:query?'50':'100',identity_format:'string'});if(query)params.set('q',query);else params.set('dir',path);
 if(offset&&(continuation||restore.source)){if(requestedSource==='cache')params.set('cache_generation',restore.generation||cacheGeneration.value);else if(restore.boundary||boundaries.has(offset))params.set('expected_boundary',JSON.stringify({fingerprint:restore.boundary||boundaries.get(offset)}));else{loading.value=false;error.value='缺少本页边界，请刷新目录。';return;}}
 try{const response=await fetch((query?'/api/cloud-inventory/search?':requestedSource==='cache'?'/api/cloud-inventory?':'/api/cloud?')+params,{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){auth.value=true;stop();error.value='管理员登录已失效，请重新登录。';location.assign('/login?next=%2Fcloud%2Ffiles');return;}
  if(response.status===409&&requestedSource==='cache'&&recover){const conflict=await response.json();if(request!==generation)return;if(conflict.code==='cache_generation_changed'){pageNotice.value='本地缓存已更新，已重新读取第一页。';loading.value=false;await open(path,query,0,false,false,{source:'cache',replace:true});return;}}
  if(!response.ok)throw Error(await response.text()||'网盘目录读取失败');const value=await response.json();if(request!==generation)return;
  if(query){if(value?.query!==query||!value.coverage||typeof value.coverage.status!=='string')throw Error('缓存搜索响应无效');value.directory=path;value.metadata={status:value.coverage.status,observed_at_utc:value.coverage.scan_updated_at_utc};}
  if(requestedSource==='cache'){
   if(!value?.metadata||typeof value.metadata.status!=='string'||!value.metadata.status||typeof value.cache_generation!=='string'||!/^(0|[1-9][0-9]*)$/.test(value.cache_generation)||value.cache_generation.length>19||BigInt(value.cache_generation)>9223372036854775807n||!Array.isArray(value.entries))throw Error('缓存目录响应无效');
   value.entries=value.entries.map((entry:any)=>({...entry,id:entry.fs_id,provider_size:entry.size,server_mtime:entry.provider_mtime,size:entry.directory&&entry.directory_size_bytes!==undefined?entry.directory_size_bytes:entry.size,size_complete:entry.directory_size_complete}));
   value.observed_at_utc=value.metadata.observed_at_utc||'';
  }
  if(value?.observed_at_utc!==undefined&&value.observed_at_utc!==''&&(typeof value.observed_at_utc!=='string'||!/Z$/.test(value.observed_at_utc)||!Number.isFinite(Date.parse(value.observed_at_utc))))throw Error('目录观察时间无效');
  if(!Array.isArray(value?.entries)||!value.entries.every((entry:any)=>entry&&(entry.size_complete===undefined||typeof entry.size_complete==='boolean')&&typeof entry.path==='string'&&validPath(entry.path)))throw Error('目录完整性或路径响应无效');
  if(requestedSource==='cache'){
   const limit=query?50:100;
   if(!Number.isSafeInteger(value.total)||value.total<0||value.entries.length!==Math.min(limit,Math.max(0,value.total-offset)))throw Error('缓存分页总量无效');
   const expectedNext=value.total-offset>limit?offset+limit:null;if((value.next_offset??null)!==expectedNext)throw Error('缓存分页游标无效');
  }
  if(!value||value.directory!==path||(value.offset!==offset&&!(requestedSource==='live'&&value.boundary_changed===true&&value.offset===0))||!Array.isArray(value.entries)||value.entries.length>100||!value.entries.every((entry:any)=>entry&&typeof entry.id==='string'&&entry.id&&typeof entry.name==='string'&&entry.name&&(query?validPath(entry.path)&&entry.path.split('/').at(-1)===entry.name:entry.path===(path==='/'?'':path)+'/'+entry.name)&&!entry.name.includes('/')&&typeof entry.directory==='boolean'&&['fs_id','size','provider_size','server_mtime'].every(key=>typeof entry[key]==='string'&&/^(0|[1-9][0-9]*)$/.test(entry[key])&&entry[key].length<=19&&BigInt(entry[key])<=9223372036854775807n)&&entry.fs_id!=='0'))throw Error('网盘目录响应无效');
  if(new Set(value.entries.map((entry:Entry)=>entry.id)).size!==value.entries.length||new Set(value.entries.map((entry:Entry)=>entry.fs_id)).size!==value.entries.length)throw Error('目录返回重复身份，请刷新核对。');
  const currentOffset=value.offset,next=value.next_offset??null;
  if(currentOffset!==offset&&!(requestedSource==='live'&&value.boundary_changed===true&&currentOffset===0))throw Error('目录分页位置无效');
  if(next!==null&&(!Number.isSafeInteger(next)||next!==currentOffset+value.entries.length||next<=currentOffset||next>1000000))throw Error('目录分页游标无效');
  if(requestedSource==='live'&&next!==null&&(!value.next_boundary||typeof value.next_boundary.fingerprint!=='string'||!/^[a-f0-9]{64}$/.test(value.next_boundary.fingerprint)))throw Error('下一页边界无效');
  if(!continuation||currentOffset===0)boundaries.clear();
  if(requestedSource==='live'&&next!==null)boundaries.set(next,value.next_boundary.fingerprint);
  pageOffset.value=currentOffset;nextOffset.value=next;if(value.boundary_changed)pageNotice.value='远端目录分页边界已变化，已重新读取第一页。';
  if(restore.boundary&&currentOffset===offset&&currentOffset)boundaries.set(currentOffset,restore.boundary);
  const url=cloudFilesRouteURL({directory:path,source:requestedSource as 'live'|'cache',query,offset:currentOffset,generation:value.cache_generation||'',boundary:currentOffset?(restore.boundary||boundaries.get(currentOffset)||''):''});
  if(restore.replace||value.boundary_changed||location.pathname+location.search===url)history.replaceState(null,'',url);else history.pushState(null,'',url);
  entries.value=value.entries;directory.value=path;draft.value=path;loadedSource.value=requestedSource;activeSearch.value=query;cacheStatus.value=requestedSource==='cache'?value.metadata.status:'';cacheGeneration.value=requestedSource==='cache'?value.cache_generation:'';observed.value=value.observed_at_utc&&!value.observed_at_utc.startsWith('0001-')?value.observed_at_utc:'';ready.value=true;error.value='';
 }catch(e:any){if(request===generation)error.value=e.message||'网盘目录读取失败';}
 finally{if(request===generation){loading.value=false;if(refreshAfterAction&&!actionBusy.value&&active){refreshAfterAction=false;refresh();}}}
}
function parent(){const parts=directory.value.split('/').filter(Boolean);parts.pop();open('/'+parts.join('/'));}
function size(entry:Entry){if(entry.directory&&!entry.size_complete)return '—';return formatBytes(entry.size);}
const pageSizes=computed(()=>{let files=0n,total=0n,missing=0;for(const entry of entries.value){if(!entry.directory){files+=BigInt(entry.size);total+=BigInt(entry.size);}else if(entry.size_complete)total+=BigInt(entry.size);else missing++;}return {files:formatBytes(files),total:formatBytes(total),missing};});
function fileIcon(entry:Entry){if(entry.directory)return Folder;const ext=entry.name.split('.').pop()?.toLowerCase()||'';if(['png','jpg','jpeg','gif','webp','svg','bmp','heic'].includes(ext))return FileImage;if(['mp4','mkv','avi','mov','webm'].includes(ext))return FileVideo;if(['mp3','flac','wav','m4a'].includes(ext))return FileAudio;if(['zip','rar','7z','tar','gz'].includes(ext))return FileArchive;if(['js','ts','py','go','json','html','css','cs','sh'].includes(ext))return FileCode;if(['txt','md','pdf','doc','docx','xls','xlsx','ppt','pptx'].includes(ext))return FileText;return File;}

function mtime(value:string){if(BigInt(value)>8640000000000n)return '—';const date=new Date(Number(value)*1000);return value!=='0'&&Number.isFinite(date.getTime())?date.toISOString():'—';}
function restoreRoute(){
 generation++;controller?.abort();loading.value=false;const parsed=readCloudFilesRoute(location.search);if(parsed.error){inputError.value=parsed.error;return;}
 const route=parsed.route;source.value=route.source;draft.value=route.directory;queryDraft.value=route.query;
 if(route.offset&&!(route.source==='cache'?route.generation:route.boundary)){route.offset=0;pageNotice.value='分页核对依据缺失，已重新读取第一页。';}
 open(route.directory,route.query,route.offset,false,true,{source:route.source,generation:route.generation,boundary:route.boundary,replace:true});
}
function resume(event:PageTransitionEvent){if(event.persisted&&!auth.value){active=true;restoreRoute();}}
onMounted(()=>{document.title='网盘文件 · NAS Downloader';window.addEventListener('pagehide',stop);window.addEventListener('pageshow',resume);window.addEventListener('popstate',restoreRoute);restoreRoute();});
onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',resume);window.removeEventListener('popstate',restoreRoute);});
</script>
<template>
 <div class="workspace event-workspace cloud-files-workspace"><WorkspaceSidebar active="/cloud/files" @error="sidebarError=$event"/><main class="main">
  <header class="page-header"><div><p class="eyebrow">百度网盘</p><h1>网盘文件</h1><p class="muted">浏览网盘目录，文件大小独立展示。</p></div><div class="cloud-header-actions"><a class="button secondary" :href="'/uploads?directory='+encodeURIComponent(directory)">上传文件 / 目录</a><button class="button secondary" :disabled="loading||auth" @click="refresh">刷新列表</button></div></header>
  <CloudToolsNav active="/cloud/files" :directory="directory"/><div class="cloud-local-tools"><button class="button secondary" data-testid="cloud-cache-status-button" :aria-expanded="cachePanel" @click="cachePanel=!cachePanel">缓存进度与状态</button></div><p v-if="sidebarError" class="notice danger" role="alert">{{sidebarError}}</p>
  <section v-if="cachePanel" aria-label="缓存状态"><CloudCacheStatus kind="scan" :disabled="auth" @authentication-required="requireLogin"/><CloudCacheStatus kind="refresh" :disabled="auth" @authentication-required="requireLogin"/></section><form class="event-filter-panel event-filter-row" @submit.prevent="open(draft)"><button class="button secondary" type="button" data-testid="cloud-files-up" :disabled="loading||auth||directory==='/'" @click="parent">上一级</button><label>网盘目录<input v-model="draft" data-testid="cloud-files-path" autocomplete="off"/></label><button class="button primary" data-testid="cloud-files-open" :disabled="loading||auth">打开目录</button></form>
  <form class="event-filter-panel event-filter-row" @submit.prevent="search"><label>搜索全部文件<input v-model="queryDraft" data-testid="cloud-cache-query"/></label><button class="button primary" data-testid="cloud-cache-search" :disabled="loading||auth">搜索</button><button class="button secondary" type="button" :disabled="loading||auth" @click="open(directory)">返回目录</button></form><button class="button secondary" data-testid="cloud-mkdir" :disabled="loading||auth||!!error||!ready||loadedSource!=='live'||actionBusy||actionResult?.status==='unknown'" @click="chooseAction('mkdir')">新建文件夹</button><p v-if="actionResult?.status==='unknown'" class="notice" role="status">上一项操作结果未知，新建等写操作暂不可用。请先到操作记录核对。</p><nav aria-label="网盘路径"><button v-for="crumb in crumbs" :key="crumb.path" class="event-detail-link" :disabled="loading||auth" @click="open(crumb.path)">{{crumb.name}} / </button></nav><p v-if="inputError" class="notice danger" role="alert">{{inputError}}</p>
  <section class="event-results" aria-label="目录列表"><p v-if="error" class="notice danger" role="alert">{{error}}<span v-if="ready"> · 下方为上次成功读取目录 {{directory}} 的历史结果。</span></p><p v-if="loading" role="status">正在读取目录…</p><p class="muted" data-testid="cloud-files-source-status"><span v-if="activeSearch">全盘缓存搜索“{{activeSearch}}” · </span>{{loadedSource==='cache'?'搜索使用本地缓存':'实时网盘列表'}}<span v-if="loadedSource==='cache'"> · {{cacheStatus==='complete'?'目录缓存已发布，仍可能过时':'目录缓存不完整或待核对'}} · 代次 {{cacheGeneration}}。缓存无结果不能证明网盘内容不存在。</span><span v-if="observed"> · 服务端观察 UTC：{{observed}}</span></p>
   <div class="event-table-scroll"><table data-testid="cloud-files-table"><thead><tr><th>名称</th><th>类型</th><th>大小</th><th>远端修改 UTC</th><th>操作</th></tr></thead><tbody><tr v-for="entry in entries" :key="entry.id"><td class="cloud-name-cell"><component :is="fileIcon(entry)" :size="18" aria-hidden="true" :class="{'folder-icon':entry.directory}"/><button v-if="entry.directory" class="event-detail-link" :disabled="loading||auth||!!error" @click="open(entry.path)">{{entry.name}}</button><span v-else>{{entry.name}}</span><small v-if="activeSearch" class="muted"><br>{{entry.path}}</small></td><td>{{entry.directory?'文件夹':'文件'}}</td><td data-testid="cloud-file-size" :title="entry.directory&&!entry.size_complete?'目录大小未完整缓存':entry.size+' B'">{{size(entry)}}</td><td>{{mtime(entry.server_mtime)}}</td><td class="cloud-row-actions"><template v-if="loadedSource==='live'"><a class="button secondary" data-testid="cloud-file-download" :href="'/intake?path='+encodeURIComponent(entry.path)" :aria-label="'下载 '+entry.name"><Download :size="13"/>下载</a><button class="button secondary" data-testid="cloud-file-rename" :disabled="loading||auth||!!error||actionBusy||actionResult?.status==='unknown'" @click="chooseAction('rename',entry)">改名</button><button class="button secondary" :disabled="loading||auth||!!error||actionBusy||actionResult?.status==='unknown'" data-testid="cloud-file-move" @click="chooseAction('move',entry)">移动</button><button class="button secondary" :disabled="loading||auth||!!error||actionBusy||actionResult?.status==='unknown'" data-testid="cloud-file-delete" @click="chooseAction('delete',entry)">删除</button></template><button v-else class="button secondary" :disabled="loading||auth||!!error" @click="inspectLive(entry)">到实时目录核对</button></td></tr><tr v-if="ready&&!entries.length"><td colspan="5">{{activeSearch?'缓存中未找到匹配项，不代表远端没有这些内容':'目录为空'}}</td></tr></tbody></table></div>
   <p v-if="pageNotice" class="notice" role="status">{{pageNotice}}</p><p class="muted">第 {{pageOffset+1}} 项起 · 本页 {{entries.length}} 项 <span data-testid="cloud-page-size"> · 文件 {{pageSizes.files}} · 含目录缓存 {{pageSizes.total}}{{pageSizes.missing?'（已知部分，'+pageSizes.missing+' 个目录未完整缓存）':''}}{{activeSearch?'（搜索结果可能包含父子目录，不作全盘合计）':''}}</span></p><div class="toolbar"><button class="button secondary" data-testid="cloud-files-prev" :disabled="loading||auth||!!error||pageOffset===0" @click="page(Math.max(0,pageOffset-(activeSearch?50:100)))">上一页</button><button class="button secondary" data-testid="cloud-files-next" :disabled="loading||auth||!!error||nextOffset===null" @click="page(nextOffset!)">下一页</button></div>
  </section>
  <CloudDirectorySize v-if="!activeSearch" :key="directory+'|'+loadedSource+'|'+cacheGeneration" :path="directory" :disabled="loading||auth||!ready||!!error" @authentication-required="requireLogin"/>
  <section v-if="actionResult" class="event-results cloud-action-feedback" data-testid="cloud-action-result"><details open><summary>最近操作 · {{actionResult.status==='confirmed'?'已确认':actionResult.status==='failed'?'失败':'结果未知'}}</summary><p>{{actionResult.message}}</p><p>{{actionResult.source}} → {{actionResult.target}}</p><p v-if="actionResult.operation_id">操作 ID：{{actionResult.operation_id}}</p><a :href="actionResult.operation_id?'/operations?operation_id='+actionResult.operation_id:'/operations'">查看操作记录</a></details></section>
  <p v-if="actionBusy" class="notice" role="status">正在处理：{{action?.item?.path||action?.directory}} · <button class="button secondary" @click="actionMinimized=false">查看当前操作</button></p><CloudFileActions v-if="action" :action="action" :minimized="actionMinimized" @minimize="actionMinimized=true" @close="action=null" @busy="actionBusyChanged" @result="actionFinished" @authentication-required="requireLogin"/>
 </main></div>
</template>

<style scoped>
.cloud-files-workspace :deep(th){width:auto}.cloud-files-workspace .event-filter-panel{display:inline-flex;vertical-align:bottom;background:transparent;border:0;padding:0;margin:0 16px 12px 0;max-width:100%}.cloud-header-actions{display:flex;gap:8px;flex-wrap:wrap}.cloud-files-workspace .page-header{margin-bottom:12px}.cloud-files-workspace .event-results{padding:14px}.cloud-files-workspace th,.cloud-files-workspace td{padding:8px 10px}.cloud-row-actions{white-space:nowrap}.cloud-row-actions .button{min-height:28px;padding:4px 7px;font-size:12px;border:1px solid #dce2ed;gap:4px;margin-right:5px}.cloud-name-cell>svg{vertical-align:middle;margin-right:8px;color:#7a8ba8}.cloud-name-cell>.folder-icon{color:#e6ac32;fill:#fff2ca}.cloud-files-workspace nav{display:flex;flex-wrap:wrap;gap:4px;margin:12px 0}.cloud-files-workspace nav .event-detail-link{border:1px solid #dce2ed;border-radius:5px;padding:4px 7px;background:white}.cloud-name-cell{max-width:440px;overflow:hidden;text-overflow:ellipsis}.cloud-files-workspace .button.secondary{border:1px solid #dce2ed}
</style>
