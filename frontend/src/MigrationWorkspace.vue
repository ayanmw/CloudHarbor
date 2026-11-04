<script setup lang="ts">
import {onMounted,onUnmounted,ref} from 'vue';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import MigrationDetails from './MigrationDetails.vue';
import MigrationExport from './MigrationExport.vue';
import type {Migration} from './migration-types';
import {readMigrationRoute,validateMigrationRoute,migrationRouteURL,type MigrationRoute} from './migration-route';
const selected=ref<Migration|null>(null),selectedObserved=ref('');
function openDetails(item:Migration){if(loading.value||error.value||inputError.value||auth.value)return;selected.value={...item};selectedObserved.value=observed.value;}
const entries=ref<Migration[]>([]),total=ref(0),loading=ref(false),error=ref(''),sidebarError=ref(''),observed=ref(''),ready=ref(false),auth=ref(false);
const initial=readMigrationRoute(location.search),route=ref<MigrationRoute>(initial.route),draft=ref({...initial.route.filters}),inputError=ref(initial.error),nextOffset=ref<number|null>(null);
function navigate(next:MigrationRoute){const invalid=validateMigrationRoute(next);if(invalid){inputError.value=invalid;return;}generation++;controller?.abort();loading.value=false;route.value=next;inputError.value='';history.pushState(null,'',migrationRouteURL(next));load();}
function submit(){navigate({filters:{...draft.value,q:draft.value.q.trim()},offset:0});}
function page(offset:number){navigate({filters:{...route.value.filters},offset});}
function restore(){generation++;controller?.abort();loading.value=false;const value=readMigrationRoute(location.search);route.value=value.route;draft.value={...value.route.filters};inputError.value=value.error;if(!value.error)load();}
const statuses:Record<string,string>={running:'迁移中',failed:'失败',completed:'完成'};
const phases:Record<string,string>={copying:'复制文件',verifying:'核验内容',retiring_source:'隔离原目录',source_retired:'原目录已隔离',deleting_source:'清理原目录',complete:'完成'};
let generation=0,active=true,controller:AbortController|undefined;
function requireLogin(){auth.value=true;stop();error.value='管理员登录已失效，请重新登录。';location.assign('/login?next=%2Fsettings%2Fmigrations');}
function stop(){selected.value=null;active=false;generation++;controller?.abort();loading.value=false;}
async function load(recover=true){
 if(loading.value||!active||auth.value||inputError.value)return;selected.value=null;const request=++generation;controller=new AbortController();loading.value=true;const requested=route.value.offset,params=new URLSearchParams({offset:String(requested),limit:'20',counter_format:'string'});for(const [key,value] of Object.entries(route.value.filters))if(value)params.set(key,value);
 try{const response=await fetch('/api/settings/local-migrations?'+params,{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){auth.value=true;stop();error.value='管理员登录已失效，请重新登录。';location.assign('/login?next=%2Fsettings%2Fmigrations');return;}
  if(!response.ok)throw Error(await response.text()||'迁移历史读取失败');
  const data=await response.json();if(request!==generation)return;
  if(!data||!Array.isArray(data.entries)||!Number.isSafeInteger(data.total)||data.total<0||data.offset!==requested||data.limit!==20||data.entries.length>20||data.entries.length>data.total||!data.entries.every((item:any)=>item&&typeof item.id==='string'&&/^[a-f0-9]{32}$/.test(item.id)&&Object.hasOwn(statuses,item.status)&&Object.hasOwn(phases,item.phase)&&['total_files','processed_files','total_bytes','processed_bytes'].every(key=>typeof item[key]==='string'&&/^(0|[1-9][0-9]*)$/.test(item[key])&&item[key].length<=19&&BigInt(item[key])<=9223372036854775807n)&&['source_path','target_path'].every(key=>typeof item[key]==='string'&&item[key])&&['started_at_utc','updated_at_utc'].every(key=>typeof item[key]==='string'&&/Z$/.test(item[key])&&Number.isFinite(Date.parse(item[key])))))throw Error('迁移历史响应无效');
  if(data.next_offset!==null&&(!Number.isSafeInteger(data.next_offset)||data.next_offset!==requested+20||data.next_offset>=data.total||data.entries.length!==20))throw Error('迁移分页响应无效');
  const expectedLength=Math.min(20,Math.max(0,data.total-requested)),expectedNext=data.total-requested>20?requested+20:null;
  if(data.entries.length!==expectedLength||data.next_offset!==expectedNext||new Set(data.entries.map((item:Migration)=>item.id)).size!==data.entries.length)throw Error('迁移分页响应无效');
  if(!data.entries.every((item:Migration)=>(item.last_error===undefined||typeof item.last_error==='string')&&(item.finished_at_utc===undefined||(typeof item.finished_at_utc==='string'&&/Z$/.test(item.finished_at_utc)&&Number.isFinite(Date.parse(item.finished_at_utc))))))throw Error('迁移时间或错误响应无效');
  if(requested>0&&requested>=data.total&&data.entries.length===0){
   if(!recover)throw Error('迁移记录持续变化，请重新查询。');
   route.value={filters:{...route.value.filters},offset:data.total?Math.floor((data.total-1)/20)*20:0};history.replaceState(null,'',migrationRouteURL(route.value));loading.value=false;await load(false);return;
  }
  entries.value=data.entries;total.value=data.total;nextOffset.value=data.next_offset;observed.value=new Date().toISOString();ready.value=true;error.value='';
 }catch(e:any){if(request===generation)error.value=e.message||'迁移历史读取失败';}
 finally{if(request===generation)loading.value=false;}
}
function resume(event:PageTransitionEvent){if(event.persisted&&!auth.value){active=true;load();}}
onMounted(()=>{document.title='迁移历史 · NAS Downloader';window.addEventListener('pagehide',stop);window.addEventListener('pageshow',resume);window.addEventListener('popstate',restore);load();});
onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',resume);window.removeEventListener('popstate',restore);});
</script>
<template>
 <div class="workspace event-workspace migration-workspace"><WorkspaceSidebar active="/settings/migrations" @error="sidebarError=$event"/><main class="main">
  <header class="page-header"><div><p class="eyebrow">本地存储记录</p><h1>迁移历史</h1><p class="muted">核对过去的目录迁移，不代表当前磁盘已重新检查。</p></div><button class="button secondary" :disabled="loading||auth" @click="load()">刷新记录</button></header>
  <p><a href="/settings/paths">返回路径与存储</a></p><p v-if="sidebarError" class="notice danger" role="alert">{{sidebarError}}</p>
  <form class="event-filter-panel event-filter-row" data-testid="migration-filter" @submit.prevent="submit"><label>状态<select v-model="draft.status"><option value="">全部</option><option value="running">迁移中</option><option value="failed">失败</option><option value="completed">完成</option></select></label><label>路径关键词<input v-model="draft.q" data-testid="migration-query"/></label><label>开始日期 UTC（含）<input v-model="draft.started_from_utc" type="date"/></label><label>结束日期 UTC（含）<input v-model="draft.started_through_utc" type="date"/></label><button class="button primary" :disabled="loading||auth">查询</button></form>
  <p v-if="inputError" class="notice danger" role="alert">{{inputError}}</p>
  <MigrationExport :filters="route.filters" :disabled="loading||auth||!!inputError||!!error||!ready" @authentication-required="requireLogin"/>
  <section class="event-results" data-testid="migration-history-results" aria-label="迁移历史列表">
   <p v-if="error" class="notice danger" role="alert">{{error}}<span v-if="ready"> · 下方为上次读取结果。</span></p><p v-if="loading" role="status">正在读取迁移历史…</p>
   <p v-if="ready" class="muted">共 {{total}} 条记录 · 本页 {{entries.length}} 条。实时偏移分页，期间新增记录可能改变页边界。</p>
   <div class="event-table-scroll"><table data-testid="migration-history-table"><thead><tr><th>状态 / 阶段</th><th>原本地目录</th><th>目标本地目录</th><th>文件 已处理 / 总量</th><th>字节 已处理 / 总量</th><th>开始 UTC</th><th>更新 UTC</th><th>操作</th></tr></thead><tbody><tr v-for="item in entries" :key="item.id"><td>{{statuses[item.status]}} · {{phases[item.phase]}}</td><td>{{item.source_path}}</td><td>{{item.target_path}}</td><td>{{item.processed_files}} / {{item.total_files}}</td><td>{{item.processed_bytes}} / {{item.total_bytes}}</td><td>{{item.started_at_utc}}</td><td>{{item.updated_at_utc}}</td><td><button class="button secondary" data-testid="migration-details-open" :disabled="loading||!!error||!!inputError||auth" @click="openDetails(item)">查看详情</button></td></tr><tr v-if="ready&&!entries.length"><td colspan="8">暂无迁移记录</td></tr></tbody></table></div>
   <p v-if="observed" class="muted">本机读取 UTC：{{observed}}</p>
   <div class="toolbar"><button class="button secondary" data-testid="migration-prev" :disabled="loading||auth||!!error||!!inputError||route.offset===0" @click="page(route.offset-20)">上一页</button><button class="button secondary" data-testid="migration-next" :disabled="loading||auth||!!error||!!inputError||nextOffset===null" @click="page(nextOffset!)">下一页</button></div>
  </section>
  <MigrationDetails v-if="selected" :record="selected" :observed-at="selectedObserved" @close="selected=null" @authentication-required="requireLogin"/>
 </main></div>
</template>
