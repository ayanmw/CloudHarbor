<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref} from 'vue';
import {RefreshCw} from 'lucide-vue-next';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import OAuthAppConfig from './OAuthAppConfig.vue';
import OAuthDeviceAuthorization from './OAuthDeviceAuthorization.vue';
import {oauthStatusLabel,validateOAuthStatus,type OAuthStatus} from './oauth-types';
const status=ref<OAuthStatus|null>(null),loading=ref(false),error=ref(''),authenticationRequired=ref(false);
const appConfigured=ref(false),configBusy=ref(false),deviceBusy=ref(false),refreshing=ref(false),active=ref(true),renewMessage=ref(''),renewError=ref('');
const canRenew=computed(()=>active.value&&!authenticationRequired.value&&!loading.value&&!error.value&&status.value?.can_refresh===true&&appConfigured.value&&!configBusy.value&&!deviceBusy.value&&!refreshing.value);
let generation=0,controller:AbortController|undefined,timer:number|undefined,renewGeneration=0,renewController:AbortController|undefined;
function stopStatus(){active.value=false;generation++;controller?.abort();if(timer!==undefined)window.clearInterval(timer);timer=undefined;loading.value=false;}
function leave(){stopStatus();renewGeneration++;renewController?.abort();refreshing.value=false;renewMessage.value='';renewError.value='';}
function requireLogin(){if(authenticationRequired.value)return;authenticationRequired.value=true;error.value='管理员登录已失效，请重新登录。';leave();location.assign('/login?next=%2Foauth');}
async function loadStatus(){
 if(loading.value||authenticationRequired.value||!active.value||refreshing.value)return;
 const request=++generation;controller=new AbortController();loading.value=true;
 try{
  const response=await fetch('/api/oauth/status',{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){requireLogin();error.value='管理员登录已失效，请重新登录。';return;}
  if(!response.ok)throw Error(await response.text()||'账号状态暂不可读');
  const value=await response.json();if(request!==generation)return;
  status.value=validateOAuthStatus(value);error.value='';
 }catch(e:any){if(request===generation)error.value=e.message||'账号状态暂不可读';}
 finally{if(request===generation)loading.value=false;}
}
function startStatus(){if(authenticationRequired.value)return;active.value=true;if(timer===undefined)timer=window.setInterval(loadStatus,5000);loadStatus();}
function reconcileStatus(){generation++;controller?.abort();loading.value=false;loadStatus();}
async function renew(){
 if(!canRenew.value)return;
 const request=++renewGeneration;renewController=new AbortController();refreshing.value=true;renewMessage.value='正在续期并保存凭证…';renewError.value='';
 try{
  const response=await fetch('/api/oauth/refresh',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:'{}',signal:renewController.signal});if(request!==renewGeneration)return;
  if(response.status===401){requireLogin();return;}
  if(!response.ok)throw Error(await response.text()||'续期请求失败');
  const data=await response.json();if(request!==renewGeneration)return;const result=validateOAuthStatus(data);
  renewMessage.value=result.ready?'续期请求已完成，请以上方重新读取的账号状态为准。':`续期请求已返回，账号仍需核对：${oauthStatusLabel(result)}。`;
 }catch(e:any){if(request===renewGeneration){renewMessage.value='';renewError.value=(e.message||'续期连接中断')+'。请刷新账号状态核对，系统不会自动重试续期。';}}
 finally{if(request===renewGeneration){refreshing.value=false;reconcileStatus();}}
}
function onPageShow(event:PageTransitionEvent){if(event.persisted)startStatus();}
onMounted(()=>{document.title='账号与授权 · CloudHarbor';window.addEventListener('pagehide',leave);window.addEventListener('pageshow',onPageShow);startStatus();});
onUnmounted(()=>{leave();window.removeEventListener('pagehide',leave);window.removeEventListener('pageshow',onPageShow);});
</script>
<template>
 <div class="workspace event-workspace oauth-workspace">
  <WorkspaceSidebar active="/oauth" @error="error=$event"/>
  <main class="main">
   <header class="page-header"><div><p class="eyebrow">账号设置</p><h1>账号与授权</h1><p class="muted">查看本工具当前账号状态、应用配置与授权信息。</p></div><button class="button secondary" data-testid="oauth-status-refresh" :disabled="loading||authenticationRequired||refreshing" @click="loadStatus"><RefreshCw :size="16"/>刷新状态</button></header>
   <section class="event-results" aria-label="账号状态总览">
    <h2>账号状态</h2><p data-testid="oauth-status" class="oauth-state" role="status" aria-live="polite">{{error?'无法读取账号状态':loading?'正在读取账号状态…':status?oauthStatusLabel(status):'尚未读取账号状态'}}</p>
    <p v-if="error" class="notice danger" data-testid="oauth-read-error" role="alert">{{error}}<span v-if="status"> · 下方为上次读取证据，不能视作当前已重新核对。</span></p>
    <dl class="oauth-status-grid"><div><dt>凭证到期时间</dt><dd data-testid="oauth-expiry">{{status?.expires_at||'未知，不推算到期时间'}}</dd></div><div><dt>自动续期</dt><dd data-testid="oauth-renewal">{{status?(status.can_refresh?(status.next_attempt?`等待重试：${status.next_attempt}`:'已配置'):'未配置或需要重新授权'):'未知，尚未读取'}}</dd></div><div><dt>账号绑定</dt><dd>{{status?(status.account_bound?'当前目录已绑定账号':'尚未绑定账号'):'未知'}}</dd></div><div><dt>状态更新时间</dt><dd data-testid="oauth-updated">{{status?.updated_at||'未读取'}}</dd></div></dl>
    <p class="muted">时间保留服务端原时区与小数秒，不代表此刻重新验证了网盘内容。状态每 5 秒读取一次。</p>
    <p data-testid="oauth-provider-error" class="muted">最近状态错误：{{status?.last_error||'—'}}</p>
    <div class="oauth-action-row"><button class="button secondary" data-testid="oauth-renew" :disabled="!canRenew" @click="renew">{{refreshing?'正在续期…':'手动续期凭证'}}</button><span class="muted">账号状态和应用配置核对后，按服务端续期能力启用。</span></div>
    <p v-if="renewMessage" class="notice" role="status" data-testid="oauth-renew-message">{{renewMessage}}</p><p v-if="renewError" class="notice danger" role="alert" data-testid="oauth-renew-error">{{renewError}}</p>
   </section>
   <OAuthAppConfig :disabled="authenticationRequired" :blocked="deviceBusy||refreshing" @authentication-required="requireLogin" @busy="configBusy=$event" @configured="appConfigured=$event" @saved="reconcileStatus"/>
   <OAuthDeviceAuthorization :configured="appConfigured" :disabled="authenticationRequired" :blocked="configBusy||refreshing" @authentication-required="requireLogin" @busy="deviceBusy=$event" @settled="reconcileStatus"/>
  </main>
 </div>
</template>
