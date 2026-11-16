<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue';
import {ArrowUp,ChevronRight,Download,File,FileText,FileCode,FileImage,FileVideo,FileAudio,FileArchive,Folder,HardDrive,RefreshCw,Search} from 'lucide-vue-next';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import {formatBytes} from './format-bytes';

type Config={root:string;revision:string;saved_at_utc:string};
type Entry={name:string;path:string;id:string;directory:boolean;size:string;modified_at_utc:string;revision:string};
type Listing={path:string;entries:Entry[];total:number;offset:number;limit:number;excluded:number;observed_at_utc:string;root_revision:string;stale:boolean;error?:string;error_at_utc?:string};
const config=ref<Config|null>(null),list=ref<Listing|null>(null),loading=ref(false),saving=ref(false),fresh=ref(false),error=ref(''),sidebarError=ref(''),rootDraft=ref(''),path=ref(''),query=ref(''),queryDraft=ref(''),offset=ref(0),auth=ref(false);
let active=true,generation=0,controller:AbortController|undefined;
const crumbs=computed(()=>{let value='';return path.value.split('/').filter(Boolean).map(name=>{value=value?value+'/'+name:name;return {name,path:value}})});
const canDownload=computed(()=>fresh.value&&!loading.value&&!saving.value&&!auth.value&&!list.value?.stale);
const pageNumber=computed(()=>Math.floor(offset.value/50)+1);
function readRoute(){const q=new URL(location.href).searchParams;path.value=q.get('path')||'';query.value=q.get('query')||'';queryDraft.value=query.value;const value=q.get('offset')||'0';offset.value=/^(0|[1-9][0-9]*)$/.test(value)&&Number.isSafeInteger(Number(value))?Number(value):0;}
function route(){const u=new URL('/library',location.origin);if(path.value)u.searchParams.set('path',path.value);if(query.value)u.searchParams.set('query',query.value);if(offset.value)u.searchParams.set('offset',String(offset.value));history.pushState(null,'',u.pathname+u.search);}
function begin(){controller?.abort();controller=new AbortController();const id=++generation;loading.value=true;fresh.value=false;error.value='';auth.value=false;return {id,signal:controller.signal};}
function current(id:number){return active&&id===generation;}
async function readEntries(id:number,signal:AbortSignal){
 const u=new URL('/api/library/entries',location.origin);u.searchParams.set('path',path.value);u.searchParams.set('query',query.value);u.searchParams.set('offset',String(offset.value));u.searchParams.set('limit','50');
 const response=await fetch(u.pathname+u.search,{signal,credentials:'same-origin'});
 if(!current(id))return;if(response.status===401){auth.value=true;throw Error('管理员登录已失效，请重新登录。');}
 const body=await response.json();if(!current(id))return;
 if(!body||!Array.isArray(body.entries)||!Number.isSafeInteger(body.total)||body.total<0||typeof body.observed_at_utc!=='string')throw Error('目录响应格式无效，请重试。');
 if(body.root_revision!==config.value?.revision)throw Error('文件库配置已变化，请重新读取配置。');
 list.value=body;fresh.value=response.ok&&!body.stale;
 if(!response.ok||body.stale)error.value=body.error||'目录读取失败，请核对文件库是否在线。';
}
async function load(){if(!active||!config.value?.root)return;const {id,signal}=begin();try{await readEntries(id,signal)}catch(e:any){if(current(id)&&e.name!=='AbortError')error.value=e.message||'目录读取失败。'}finally{if(current(id))loading.value=false}}
async function loadConfig(){
 if(!active)return;const {id,signal}=begin();
 try{const response=await fetch('/api/library/config',{signal,credentials:'same-origin'});if(!current(id))return;if(response.status===401){auth.value=true;throw Error('管理员登录已失效，请重新登录。');}if(!response.ok)throw Error('无法读取文件库配置。');const body=await response.json();if(!current(id))return;if(typeof body.root!=='string'||typeof body.revision!=='string')throw Error('配置响应格式无效。');config.value=body;rootDraft.value=body.root;if(body.root)await readEntries(id,signal);else list.value=null;
 }catch(e:any){if(current(id)&&e.name!=='AbortError')error.value=e.message||'文件库读取失败。'}finally{if(current(id))loading.value=false}
}
async function saveRoot(){
 if(saving.value||loading.value||!config.value)return;saving.value=true;fresh.value=false;error.value='';const id=++generation;
 try{const response=await fetch('/api/library/config',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify({root:rootDraft.value,expected_revision:config.value.revision})});if(!current(id))return;if(!response.ok)throw Error((await response.text())||'文件库保存失败。');await response.json();if(!current(id))return;path.value='';query.value='';queryDraft.value='';offset.value=0;list.value=null;route();await loadConfig();
 }catch(e:any){if(current(id))error.value=e.message||'文件库保存失败。'}finally{saving.value=false}
}
function navigate(next:string){if(saving.value)return;path.value=next;query.value='';queryDraft.value='';offset.value=0;route();void load();}
function search(){query.value=queryDraft.value;offset.value=0;route();void load();}
function page(delta:number){offset.value=Math.max(0,offset.value+delta*50);route();void load();}
function downloadURL(entry:Entry){const q=new URLSearchParams({path:entry.path,revision:entry.revision});return '/api/library/content?'+q.toString();}
function closeSettings(event:KeyboardEvent){const details=event.currentTarget as HTMLDetailsElement;details.open=false;details.querySelector('summary')?.focus();}
function date(value:string){const d=new Date(value);return Number.isNaN(d.getTime())?'—':d.toLocaleString();}
function fileIcon(name:string){const ext=name.split('.').pop()?.toLowerCase()||'';if(['png','jpg','jpeg','gif','webp','svg','bmp','heic'].includes(ext))return FileImage;if(['mp4','mkv','avi','mov','webm'].includes(ext))return FileVideo;if(['mp3','flac','wav','m4a'].includes(ext))return FileAudio;if(['zip','rar','7z','tar','gz'].includes(ext))return FileArchive;if(['js','ts','py','go','json','html','css','cs','sh'].includes(ext))return FileCode;if(['txt','md','pdf','doc','docx','xls','xlsx','ppt','pptx'].includes(ext))return FileText;return File;}
function pop(){readRoute();void load();}
function hide(){active=false;generation++;controller?.abort();loading.value=false;fresh.value=false;}
function restore(event:PageTransitionEvent){if(event.persisted){active=true;readRoute();void loadConfig();}}
onMounted(()=>{document.title='本站文件 · 泊云 CloudHarbor';readRoute();window.addEventListener('popstate',pop);window.addEventListener('pagehide',hide);window.addEventListener('pageshow',restore);void loadConfig();});
onUnmounted(()=>{hide();window.removeEventListener('popstate',pop);window.removeEventListener('pagehide',hide);window.removeEventListener('pageshow',restore);});
</script>

<template>
 <div class="workspace event-workspace local-library-workspace">
  <WorkspaceSidebar active="/library" @error="sidebarError=$event"/>
  <main class="main">
   <header class="page-header"><div><p class="eyebrow">CloudHarbor · 本地存储</p><h1><HardDrive :size="26"/> 本站文件</h1><p class="muted">只读文件库，独立于百度授权。此页面不会修改或删除本地文件。</p></div><button class="button secondary" data-testid="library-refresh" :disabled="loading||saving||auth" @click="loadConfig"><RefreshCw :size="16"/> {{loading?'正在读取…':'刷新'}}</button></header>
   <p v-if="sidebarError" class="notice danger" role="alert">{{sidebarError}}</p>
   <p v-if="error" class="notice danger" role="alert">{{error}}</p>
   <a v-if="auth" class="button secondary" href="/login?next=%2Flibrary">重新登录</a>
   <section v-if="config&&!config.root" class="card library-config-panel" data-testid="library-config-panel"><h2>选择本站文件库</h2><p class="muted">输入此服务器上已有目录的绝对路径；不会迁移文件，也不会默认公开下载目录。</p><form @submit.prevent="saveRoot"><label for="library-root">文件库根目录</label><input id="library-root" v-model="rootDraft" data-testid="library-root-input" placeholder="D:\Shared 或 /mnt/storage" required :disabled="saving||auth"/><button type="submit" class="button primary" :disabled="saving||auth">{{saving?'正在保存…':'启用只读文件库'}}</button></form></section>
   <template v-else-if="config?.root">
    <details class="library-root-settings" @keydown.esc.stop="closeSettings"><summary>文件库设置 <span class="muted library-root-value">{{config.root}}</span></summary><p class="muted">修改时须结束本站浏览或下载请求。凭据目录和程序私有状态目录不能发布。</p><form @submit.prevent="saveRoot"><label for="library-root-change">根目录</label><input id="library-root-change" v-model="rootDraft" data-testid="library-root-input" required :disabled="saving||loading||auth"/><button class="button secondary" type="submit" :disabled="saving||loading||auth">保存根目录</button></form></details>
    <section class="card library-browser">
     <nav class="library-address" aria-label="本站路径"><button class="button secondary" data-testid="library-up" aria-label="向上一级" :disabled="!path||saving" @click="navigate(path.split('/').slice(0,-1).join('/'))"><ArrowUp :size="16"/></button><button class="library-crumb" @click="navigate('')">文件库</button><template v-for="crumb in crumbs" :key="crumb.path"><ChevronRight :size="14"/><button class="library-crumb" @click="navigate(crumb.path)">{{crumb.name}}</button></template></nav>
     <form class="library-search" @submit.prevent="search"><label class="sr-only" for="library-search">当前目录文件名</label><input id="library-search" v-model="queryDraft" placeholder="搜索当前目录文件名" maxlength="256" :disabled="saving||auth"/><button class="button secondary" type="submit" :disabled="saving||auth"><Search :size="16"/> 搜索</button></form>
     <p v-if="list&&!fresh" class="notice" role="status">{{loading?'正在刷新，下方暂为历史观察。':'下方为历史观察，当前目录尚未重新核对。'}} 历史条目不可直接下载。</p>
     <div v-if="list?.entries.length" class="event-table-scroll"><table data-testid="library-table" class="library-table"><thead><tr><th>名称</th><th>大小</th><th>修改时间</th><th>操作</th></tr></thead><tbody><tr v-for="entry in list.entries" :key="entry.path"><td><button v-if="entry.directory" class="library-directory" :disabled="!canDownload" @click="navigate(entry.path)"><Folder :size="18"/><span>{{entry.name}}</span></button><span v-else class="library-file"><component :is="fileIcon(entry.name)" :size="17"/><span>{{entry.name}}</span></span></td><td>{{entry.directory?'—':formatBytes(entry.size)}}</td><td>{{date(entry.modified_at_utc)}}</td><td><template v-if="!entry.directory"><a v-if="canDownload" class="button secondary compact" data-testid="library-download" :href="downloadURL(entry)" download><Download :size="14"/> 下载</a><button v-else class="button secondary compact" data-testid="library-download" disabled>下载</button></template><span v-else class="muted">文件夹</span></td></tr></tbody></table></div>
     <p v-else class="library-empty" role="status">{{loading?'正在读取目录…':list?.stale?'当前没有可用的历史条目，请恢复目录后刷新。':query?'当前目录没有匹配的文件。':'此目录为空。'}}</p>
     <footer class="library-list-footer"><span>第 {{pageNumber}} 页 · {{list?.total??0}} 项<span v-if="list?.excluded"> · {{list.excluded}} 项私有路径、链接或不支持的项目未发布</span></span><div><button class="button secondary compact" :disabled="!offset||loading||saving" @click="page(-1)">上一页</button><button class="button secondary compact" :disabled="!list||offset+50>=list.total||loading||saving" @click="page(1)">下一页</button></div></footer>
     <p v-if="list?.observed_at_utc" class="muted library-observation">{{list.stale?'最后成功观察':'目录观察'}}：{{date(list.observed_at_utc)}}<span v-if="list.error_at_utc"> · 失败：{{date(list.error_at_utc)}}</span></p>
    </section>
    <p class="muted">下载前会计算内容校验，大文件可能需要准备时间；由浏览器接收文件，不占用网页内存。下载期间请避免其他程序原地修改文件。</p>
   </template>
   <p v-else-if="loading" role="status">正在读取本站文件库配置…</p>
  </main>
 </div>
</template>

<style scoped>
.library-config-panel,.library-browser{background:#fff;border:1px solid var(--border,#e4e9f0);border-radius:10px;padding:16px}.local-library-workspace input{padding:9px 12px;border:1px solid var(--border,#d7dfeb);border-radius:7px;background:#fff}.sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}.local-library-workspace h1{display:flex;align-items:center;gap:10px}.library-config-panel form,.library-root-settings form{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:12px}.library-config-panel input,.library-root-settings input{flex:1;min-width:220px}.library-root-settings{padding:12px 16px;background:var(--surface,#fff);border:1px solid var(--border,#e4e9f0);border-radius:10px;margin-bottom:16px}.library-root-settings summary{cursor:pointer;font-weight:600}.library-root-value{margin-left:12px;font-size:13px;font-weight:400;overflow-wrap:anywhere}.library-address{display:flex;align-items:center;gap:6px;flex-wrap:wrap;min-height:40px}.library-crumb,.library-directory{border:0;background:transparent;color:var(--primary,#2563eb);cursor:pointer;padding:4px;border-radius:5px;text-align:left}.library-crumb:hover,.library-directory:hover{background:#eef4ff}.library-search{display:flex;gap:8px;margin:12px 0}.library-search input{flex:1;min-width:0}.library-table{width:100%;min-width:620px}.library-table td,.library-table th{padding:10px 12px;text-align:left;border-bottom:1px solid var(--border,#e4e9f0)}.library-table th:first-child{width:50%}.library-table th:nth-child(2){width:110px}.library-table td:first-child{max-width:440px;overflow-wrap:anywhere}.library-table th:last-child,.library-table td:last-child{position:sticky;right:0;background:#fff;box-shadow:-1px 0 0 var(--border,#e4e9f0)}.library-table th:last-child{background:#f7f9fc;width:100px}.library-file,.library-directory{display:flex;align-items:center;gap:9px}.library-file svg,.library-directory svg{flex:none}.library-directory:disabled{color:var(--muted,#758398);cursor:not-allowed}.library-empty{padding:40px 16px;text-align:center;background:#f7f9fc;border-radius:8px}.library-list-footer{display:flex;gap:12px;justify-content:space-between;align-items:center;flex-wrap:wrap;margin-top:12px;font-size:13px}.library-list-footer>div{display:flex;gap:8px}.library-observation{font-size:12px;margin:10px 0 0}.local-library-workspace .main{min-width:0}.library-browser{min-width:0}@media(max-width:640px){.library-root-value{display:none}.library-config-panel input,.library-root-settings input{min-width:0;width:100%;flex-basis:100%}.library-table{min-width:560px}}
</style>
