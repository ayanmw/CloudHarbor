<script setup lang="ts">
import {computed,onMounted,onUnmounted,reactive,ref,watch} from 'vue';
import {validateOAuthAppConfig,type OAuthAppConfig} from './oauth-types';
const props=defineProps<{disabled:boolean;blocked?:boolean}>(),emit=defineEmits<{authenticationRequired:[];configured:[value:boolean];busy:[value:boolean];saved:[]}>();
const config=ref<OAuthAppConfig|null>(null),loading=ref(false),writing=ref(false),error=ref(''),inputError=ref(''),message=ref('');
const keys=['app_id','app_key','secret_key','sign_key'] as const;
const labels={app_id:'AppID',app_key:'AppKey',secret_key:'SecretKey',sign_key:'SignKey'};
const fields=reactive({app_id:'',app_key:'',secret_key:'',sign_key:''});
const active=ref(true),observedAt=ref('');
const editable=computed(()=>active.value&&config.value?.editable===true&&!config.value.configured&&!loading.value&&!writing.value&&!error.value&&!props.disabled&&!props.blocked);
const sourceLabel=computed(()=>config.value?.source==='env'?'进程环境变量 / .env（优先使用）':config.value?.source==='queue'?'当前下载目录的 queue 配置':'尚未配置来源');
let readGeneration=0,writeGeneration=0,readController:AbortController|undefined,writeController:AbortController|undefined;
function clearPlaintext(){for(const key of keys)fields[key]='';}
async function readConfig(){
 if(loading.value||props.disabled||props.blocked||!active.value)return null;
 if(!writing.value)message.value='';
 const request=++readGeneration;readController=new AbortController();loading.value=true;inputError.value='';emit('configured',false);
 try{
  const response=await fetch('/api/app-config',{credentials:'same-origin',signal:readController.signal});if(request!==readGeneration)return null;
  if(response.status===401){emit('authenticationRequired');throw Error('管理员登录已失效，请重新登录。');}
  if(!response.ok)throw Error(await response.text()||'应用配置状态暂不可读');
  const data=await response.json();if(request!==readGeneration)return null;
  const value=validateOAuthAppConfig(data);config.value=value;observedAt.value=new Date().toISOString();error.value='';emit('configured',value.configured);if(!value.editable)clearPlaintext();return value;
 }catch(e:any){if(request===readGeneration){error.value=e.message||'应用配置状态暂不可读';clearPlaintext();}return null;}
 finally{if(request===readGeneration)loading.value=false;}
}
async function save(){
 if(!editable.value)return;
 const body=Object.fromEntries(keys.map(key=>[key,fields[key].trim()]));
 if(keys.some(key=>!body[key]||body[key].length>4096)){inputError.value='请完整填写四项应用凭证，每项最多 4096 个字符。';return;}
 inputError.value='';message.value='正在保存应用凭证…';writing.value=true;emit('configured',false);
 const request=++writeGeneration;writeController=new AbortController();let accepted=false,responseError='';
 try{
  const response=await fetch('/api/app-config',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),signal:writeController.signal});if(request!==writeGeneration)return;
  if(response.status===401){emit('authenticationRequired');throw Error('管理员登录已失效，请重新登录。');}
  if(!response.ok)throw Error(await response.text()||'应用配置保存失败');accepted=true;
 }catch(e:any){if(request!==writeGeneration)return;responseError=e.message||'保存连接中断';}
 finally{if(request===writeGeneration)clearPlaintext();}
 if(request!==writeGeneration)return;
 const current=await readConfig();if(request!==writeGeneration)return;
 if(accepted&&current?.configured){message.value='应用凭证已保存并立即启用；明文已清除，网页只读。';emit('saved');}
 else if(accepted)message.value='保存请求已成功返回，但读回状态未确认生效。请先刷新配置状态，不要重复提交。';
 else message.value=`保存响应未确认（${responseError}）。${current?.configured?'已读回现有配置，不再允许网页保存。':current?.editable?'服务器当前报告配置为空，请核对后重新填写。':'请先核对配置状态。'}系统不会自动重发凭证。`;
 writing.value=false;
}
function stop(){active.value=false;readGeneration++;writeGeneration++;readController?.abort();writeController?.abort();loading.value=false;writing.value=false;clearPlaintext();emit('configured',false);}
function resume(event:PageTransitionEvent){if(event.persisted&&!props.disabled){active.value=true;readConfig();}}
watch(()=>props.disabled,disabled=>{if(disabled)stop();});
watch(()=>loading.value||writing.value,value=>emit('busy',value),{immediate:true,flush:'sync'});
onMounted(()=>{window.addEventListener('pagehide',stop);window.addEventListener('pageshow',resume);readConfig();});
onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',resume);});
</script>
<template>
 <section class="event-results" data-testid="oauth-app-config" aria-label="百度应用凭证">
  <div class="oauth-card-heading"><h2>百度网盘应用凭证</h2><button class="button secondary" data-testid="oauth-config-refresh" :disabled="loading||writing||disabled||blocked" @click="readConfig">刷新配置状态</button></div>
  <p v-if="loading" role="status">正在读取配置状态…</p>
  <p v-if="error||inputError" class="notice danger" data-testid="oauth-config-error" role="alert">{{inputError||error}}<span v-if="error&&config"> · 下方为上次读取结果，当前配置状态尚未重新核对。</span></p>
  <p v-if="message" class="notice" data-testid="oauth-config-message" role="status">{{message}}</p>
  <p v-if="config?.configured" class="muted">应用凭证已配置，网页不提供覆盖或清空入口。</p>
  <p v-if="config" class="muted" data-testid="oauth-config-source">配置来源：{{sourceLabel}}<small v-if="observedAt" class="cell-sub">本机读回 UTC：{{observedAt}}</small></p>
  <div v-if="config?.configured" class="event-table-scroll oauth-mask-table" data-testid="oauth-config-masks"><table><thead><tr><th>应用凭证</th><th>掩码核对</th></tr></thead><tbody><tr v-for="key in keys" :key="key"><td>{{labels[key]}}</td><td class="oauth-mask-value">{{config[`${key}_mask`]||'未返回掩码'}}</td></tr></tbody></table></div>
  <p v-else-if="config&&!config.editable&&!error" class="notice">已有初始配置锁定填写入口，请从命令行检查 .env 或 queue 配置。</p>
  <form v-if="editable||writing" class="oauth-config-form" data-testid="oauth-config-form" @submit.prevent="save" autocomplete="off">
   <p class="muted">仅在配置为空时填写。保存后立即启用；需要修改时，请通过命令行处理配置并重启。</p>
   <div class="oauth-config-fields"><label v-for="key in keys" :key="key">{{labels[key]}}<input v-model="fields[key]" :name="key" :type="key==='app_id'?'text':'password'" autocomplete="off" maxlength="4096" required :disabled="writing||disabled"/></label></div>
   <button class="button primary" data-testid="oauth-config-save" :disabled="writing||loading||disabled">{{writing?'正在保存并核对…':'保存并立即启用'}}</button>
  </form>
  <details class="oauth-setup-guide" data-testid="oauth-config-guide"><summary>还没有应用凭证？查看申请与授权说明</summary>
   <ol><li>登录<a href="https://yun.baidu.com/union" target="_blank" rel="noopener noreferrer">百度网盘开发者平台</a>，按平台要求创建自己的应用。</li><li>在应用凭证信息中复制 AppID、AppKey、SecretKey、SignKey，填入首次配置表单。当前设备码 OAuth 使用 AppKey 和 SecretKey，其余凭证同时保存供配置核对。</li><li>本工具使用设备码授权，不需要本机 OAuth 授权回调地址。若平台要求必填，请核对应用类型与授权模式，普通管理页不能冒充回调处理地址。</li></ol>
   <p>管理员登录保护本工具，百度授权允许工具访问网盘。同一下载目录绑定一个百度账号；更换账号请使用新的下载目录。</p>
   <p>环境变量 / .env 优先，缺省时读取当前下载目录的 queue 配置。已配置项只显示掩码，需要修改时请停服后从命令行处理配置并重启；网页没有清空或覆盖入口。</p>
  </details>
 </section>
</template>
