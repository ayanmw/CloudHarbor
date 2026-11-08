<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {Cloud,Folder,RefreshCw} from 'lucide-vue-next';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import PathChangeImpact from './PathChangeImpact.vue';
import LocalMigrationStatus from './LocalMigrationStatus.vue';
import {validateManagedPathSettings,queueTargetError,type ManagedPathSettings,type PathImpact} from './path-settings-types';
import {vModal} from './modal';
const settings=ref<ManagedPathSettings|null>(null),loading=ref(false),error=ref(''),sidebarError=ref(''),observedAt=ref(''),authenticationRequired=ref(false),active=ref(true);
const draftQueue=ref(''),draftLocal=ref(''),migrate=ref(false),impact=ref<PathImpact|null>(null),saving=ref(false),ack=ref(false),result=ref(''),unknown=ref(false);
const migrationRefresh=ref(0);
function sourceLabel(source:string|undefined,editable:boolean){return source==='environment'?'环境配置（.env 或启动环境）':source==='saved'?'已保存设置（SQLite）':source==='startup'?'启动默认值（未使用已保存值）':editable?'服务端未提供来源':'环境配置（由只读状态确认）';}
const receiptID=ref(''),submittedAt=ref('');
// Migration polling can finish during POST; do not invalidate its receipt generation.
function migrationChanged(){if(!saving.value)load();}
type Confirmation={queue_dir:string;local_download_dir:string;migrate_files:boolean;queue_change_preview_digest:string;expected_paths:{queue_dir:string;local_download_dir:string}};
const confirmation=ref<Confirmation|null>(null);
const unresolved=ref<Confirmation|null>(null),reconcileResult=ref('');
async function reconcile(){
 if(!unknown.value||saving.value||loading.value)return;
 reconcileResult.value='';await load();migrationRefresh.value++;
 if(error.value||!active.value||!settings.value)return;
 const s=settings.value,u=unresolved.value;
 reconcileResult.value='已读回当前配置：网盘 '+s.queue_dir+'；本地 '+s.local_download_dir+'。'+(u&&s.queue_dir===u.queue_dir&&s.local_download_dir===u.local_download_dir?'当前路径与提交值相同；':'当前路径与提交值不同；')+'这不证明本次请求完成或文件迁移完整。请结合迁移 ID、时间与阶段核对。';
}
const blocked=computed(()=>loading.value||saving.value||!!error.value||authenticationRequired.value||!active.value||!settings.value||settings.value.queue_busy||unknown.value);
const canReview=computed(()=>{const s=settings.value;if(!s||blocked.value||queueTargetError(draftQueue.value)||!draftLocal.value.trim())return false;const cloud=draftQueue.value!==s.queue_dir,local=draftLocal.value!==s.local_download_dir;return (cloud||local)&&(!cloud||(s.queue_editable&&impact.value?.target_queue_dir===draftQueue.value))&&(!local||s.local_download_editable);});
function closeConfirmation(){if(!saving.value){confirmation.value=null;ack.value=false;}}
watch([draftQueue,draftLocal,migrate,settings,impact],()=>{closeConfirmation();},{flush:'sync'});
function review(){if(!canReview.value||!settings.value)return;ack.value=false;confirmation.value={queue_dir:draftQueue.value,local_download_dir:draftLocal.value,migrate_files:migrate.value,queue_change_preview_digest:draftQueue.value===settings.value.queue_dir?'':impact.value!.digest,expected_paths:{queue_dir:settings.value.queue_dir,local_download_dir:settings.value.local_download_dir}};}
async function save(){
 if(!confirmation.value||!ack.value||blocked.value)return;
 const body=confirmation.value,request=generation;unresolved.value=body;receiptID.value='';submittedAt.value=new Date().toISOString();reconcileResult.value='';saving.value=true;confirmation.value=null;ack.value=false;result.value='';
 try{
  const response=await fetch('/api/settings/paths',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
  if(request!==generation)return;
  if(response.status===401){requireLogin();return;}
  // A proxy/server error may arrive after a write; only explicit client rejections are definitive.
  if(response.status>=500||response.status===408)throw Error('服务端未提供确定的保存结果');
  if(!response.ok){result.value=response.status===409?'配置或任务状态已变化，保存被拒绝；请刷新配置并重新预览确认。':'保存被拒绝，请刷新配置后重新确认。';impact.value=null;await load();return;}
  const value=await response.json();if(request!==generation)return;
  const paths=response.status===202?value.paths:value;
  // A successful HTTP response still needs usable paths before it can confirm a save.
  if(!paths||typeof paths.queue_dir!=='string'||queueTargetError(paths.queue_dir)||typeof paths.local_download_dir!=='string'||!paths.local_download_dir.trim()||/[\u0000-\u001f\u007f-\u009f]/u.test(paths.local_download_dir)||(response.status!==200&&response.status!==202))throw Error('保存响应无效');
  if(response.status===202){if(!value.migration||typeof value.migration.id!=='string'||!/^[a-f0-9]{32}$/.test(value.migration.id))throw Error('迁移凭据缺失');receiptID.value=value.migration.id;}
  result.value=response.status===202?'服务端已受理本地目录迁移，尚未完成；请查看迁移状态。':'服务端已确认保存路径配置。';impact.value=null;await load();
 }catch{if(request===generation){unknown.value=true;impact.value=null;result.value='保存结果未知，可能已生效；不会自动重发。请刷新配置，并查看迁移记录核对。';}}
 finally{saving.value=false;migrationRefresh.value++;}
}
let generation=0,controller:AbortController|undefined;
function stop(){if(saving.value)unknown.value=true;confirmation.value=null;active.value=false;generation++;controller?.abort();loading.value=false;}
function requireLogin(){if(authenticationRequired.value)return;authenticationRequired.value=true;error.value='管理员登录已失效，请重新登录。';stop();location.assign('/login?next=%2Fsettings%2Fpaths');}
async function load(){
 if(loading.value||authenticationRequired.value||!active.value)return;
 const request=++generation;confirmation.value=null;controller=new AbortController();loading.value=true;
 try{
  const response=await fetch('/api/settings/paths',{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){requireLogin();return;}
  if(!response.ok)throw Error(await response.text()||'路径配置暂不可读');
  const data=await response.json();if(request!==generation)return;
  const value=validateManagedPathSettings(data);if(!settings.value||draftQueue.value===settings.value.queue_dir)draftQueue.value=value.queue_dir;
  if(!settings.value||draftLocal.value===settings.value.local_download_dir)draftLocal.value=value.local_download_dir;
  settings.value=value;observedAt.value=new Date().toISOString();error.value='';
 }catch(e:any){if(request===generation)error.value=e.message||'路径配置暂不可读';}
 finally{if(request===generation)loading.value=false;}
}
function resume(event:PageTransitionEvent){if(event.persisted&&!authenticationRequired.value){active.value=true;load();}}
onMounted(()=>{document.title='路径与存储 · NAS Downloader';window.addEventListener('pagehide',stop);window.addEventListener('pageshow',resume);load();});
onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',resume);});
</script>
<template>
 <div class="workspace event-workspace path-settings-workspace">
  <WorkspaceSidebar active="/settings/paths" @error="sidebarError=$event"/>
  <main class="main">
   <header class="page-header"><div><p class="eyebrow">自动下载设置</p><h1>路径与存储</h1><p class="muted">分别核对网盘监控目录与本地文件保存位置。</p></div><button class="button secondary" data-testid="path-refresh" :disabled="loading||saving||authenticationRequired||!active" @click="load"><RefreshCw :size="16"/>刷新配置</button></header>
   <p v-if="sidebarError" class="notice danger" role="alert">{{sidebarError}}</p>
   <p v-if="error" class="notice danger" data-testid="path-read-error" role="alert">{{error}}<span v-if="settings"> · 下方为上次读取结果，不能视作当前已重新核对。</span></p>
   <p v-if="loading" class="muted" role="status">正在读取路径配置…</p>
   <section v-if="settings" class="event-results" aria-label="当前路径总览">
    <p v-if="settings.queue_busy" class="notice" data-testid="path-queue-block" role="status">{{settings.queue_busy_reason}}<span v-if="error">（上次读取状态）</span></p>
    <h2>当前路径</h2><dl class="path-settings-overview"><div><dt><Cloud :size="18"/>网盘自动监控目录</dt><dd data-testid="path-current-queue">{{settings.queue_dir}}</dd><p class="muted" data-testid="path-queue-source">来源：{{sourceLabel(settings.queue_source,settings.queue_editable)}}</p><p class="muted" data-testid="path-queue-policy">{{settings.queue_editable?'未由环境锁定；保存仍需通过运行状态检查':'由 .env 或启动环境配置，网页只读'}}</p></div><div><dt><Folder :size="18"/>本地下载目录</dt><dd data-testid="path-current-local">{{settings.local_download_dir}}</dd><p class="muted" data-testid="path-local-source">来源：{{sourceLabel(settings.local_download_source,settings.local_download_editable)}}</p><p class="muted" data-testid="path-local-policy">{{settings.local_download_editable?'未由环境锁定；保存仍需通过运行状态检查':'由 .env 或启动环境配置，网页只读'}}</p></div></dl>
    <p class="muted">生效优先级：.env / 启动环境明确配置 → 已保存设置 → 启动默认值。来源表示当前生效依据，不表示最后修改人或迁移完成。</p><p class="muted">本机读回 UTC：{{observedAt}}。这里显示服务端返回的当前配置，不代表已重新检查远端目录或本地磁盘内容。</p>
   </section>
   <section v-if="settings" class="event-results" aria-label="路径变更草稿">
    <h2>路径变更草稿</h2><p class="muted">网盘目录变更需先预览影响。本地目标以服务端规范化结果为准，建议填写绝对路径。</p>
    <label class="path-target-label">目标网盘监控目录<input v-model="draftQueue" data-testid="path-target-queue" :readonly="!settings.queue_editable" :disabled="blocked" autocomplete="off"/></label>
    <PathChangeImpact :current-queue="settings.queue_dir" :target-queue="draftQueue" :disabled="blocked||!settings.queue_editable" @ready="impact=$event" @authentication-required="requireLogin"/>
    <label class="path-target-label">目标本地下载目录<input v-model="draftLocal" data-testid="path-target-local" :readonly="!settings.local_download_editable" :disabled="blocked" autocomplete="off"/></label>
    <label><input v-model="migrate" type="checkbox" :disabled="blocked||!settings.local_download_editable||draftLocal===settings.local_download_dir"/>迁移原本地目录内的全部文件到目标目录</label>
    <p class="muted">不勾选时仅变更后续下载位置。迁移完成前不会把目标当成当前目录；迁移失败需核对占用原因后重试。</p>
    <button class="button primary" data-testid="path-save-review" :disabled="!canReview" @click="review">核对并保存</button>
    <p v-if="saving" role="status">正在提交，请勿重复操作…</p><p v-if="result" class="notice" data-testid="path-save-result" role="status">{{result}}</p>
    <div v-if="unknown" class="notice"><p>当前保存结果仍待核对，本页暂不允许再次提交。</p><p v-if="unresolved">待核对目标：网盘 {{unresolved.queue_dir}}；本地 {{unresolved.local_download_dir}}；{{unresolved.migrate_files?'包含文件迁移':'仅变更配置'}}。</p><button class="button secondary" data-testid="path-reconcile" :disabled="loading||saving||authenticationRequired||!active" @click="reconcile">读取当前配置与迁移状态</button><p v-if="reconcileResult" data-testid="path-reconcile-result">{{reconcileResult}}</p><a href="/cloud#migration-history">查看完整迁移记录</a></div>
   </section>
   <p v-if="submittedAt" class="muted">本页提交 UTC：{{submittedAt}}</p><p v-if="receiptID" class="notice" data-testid="path-save-receipt">本次迁移受理回执 ID：{{receiptID}}。受理不代表迁移完成。</p>
   <LocalMigrationStatus :refresh-key="migrationRefresh" :receipt-id="receiptID" @authentication-required="requireLogin" @changed="migrationChanged"/>
   <div v-if="confirmation" class="modal-backdrop" @click.self="closeConfirmation"><section class="detail-drawer" role="dialog" aria-modal="true" aria-labelledby="path-confirm-title" v-modal="closeConfirmation">
    <h2 id="path-confirm-title">确认路径变更</h2><dl class="path-settings-overview"><div><dt>当前网盘监控目录</dt><dd>{{confirmation.expected_paths.queue_dir}}</dd><dt>目标网盘监控目录</dt><dd>{{confirmation.queue_dir}}</dd></div><div><dt>当前本地目录</dt><dd>{{confirmation.expected_paths.local_download_dir}}</dd><dt>提交的本地目标</dt><dd>{{confirmation.local_download_dir}}</dd></div></dl>
    <p>{{confirmation.migrate_files?'迁移原目录全部内容；完成后清理原目录。':'不迁移原本地文件，仅修改配置。'}}</p><p class="muted">保存时服务端重新检查运行状态、当前路径和影响快照；有冲突将拒绝。</p>
    <label><input v-model="ack" type="checkbox" data-testid="path-save-ack"/>我已核对来源、目标及迁移选择</label><div class="toolbar"><button class="button secondary" data-testid="path-save-cancel" @click="closeConfirmation">取消</button><button class="button primary" data-testid="path-save-submit" :disabled="!ack||blocked" @click="save">确认保存</button></div>
   </section></div>
  </main>
 </div>
</template>
