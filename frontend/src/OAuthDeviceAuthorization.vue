<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {oauthStatusLabel,safeBaiduAuthorizationURL,validateOAuthDeviceCode,validateOAuthStatus} from './oauth-types';
const props=defineProps<{configured:boolean;disabled:boolean;blocked?:boolean}>(),emit=defineEmits<{authenticationRequired:[];busy:[value:boolean];settled:[]}>();
type DeviceView={userCode:string;verificationURL:string;qrURL:string;receivedAt:string;deadline:number;interval:number};
const view=ref<DeviceView|null>(null),phase=ref(''),message=ref(''),error=ref(''),active=ref(true),clock=ref(Date.now());
const expired=computed(()=>!!view.value&&clock.value>=view.value.deadline);
let deviceCode='',generation=0,controller:AbortController|undefined,timer:number|undefined;
function stopTimer(){if(timer!==undefined)window.clearInterval(timer);timer=undefined;}
function invalidate(text=''){generation++;controller?.abort();deviceCode='';view.value=null;stopTimer();phase.value='';emit('busy',false);message.value=text;error.value='';}
function updateClock(){clock.value=Date.now();if(expired.value){stopTimer();if(phase.value!=='polling'){deviceCode='';message.value='本机估算有效期已到期，请重新获取授权码；有效性以服务端校验为准。';}}}
async function post(url:string,body:unknown,signal:AbortSignal,request:number){
 const response=await fetch(url,{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),signal});if(request!==generation)return null;
 if(response.status===401){emit('authenticationRequired');throw Error('管理员登录已失效，请重新登录。');}
 if(!response.ok)throw Error(await response.text()||'授权请求失败');
 return response.json();
}
async function getCode(){
 if(!active.value||props.disabled||props.blocked||!props.configured||phase.value==='getting')return;
 invalidate();const request=++generation,startedAt=Date.now();controller=new AbortController();phase.value='getting';emit('busy',true);message.value='正在获取授权码…';
 try{
  const data=await post('/api/oauth/device-code',{},controller.signal,request);if(request!==generation)return;
  const value=validateOAuthDeviceCode(data);deviceCode=value.device_code;
  const verificationURL=safeBaiduAuthorizationURL(value.verification_url),qrURL=safeBaiduAuthorizationURL(value.qrcode_url);
  view.value={userCode:value.user_code,verificationURL,qrURL,receivedAt:new Date().toISOString(),deadline:startedAt+value.expires_in*1000,interval:value.interval};
  message.value=verificationURL&&qrURL?'请使用需要下载的百度账号完成授权，再点击验证。':'部分授权链接缺失或未通过校验，请核对后重新获取授权码。';
  updateClock();if(!expired.value)timer=window.setInterval(updateClock,500);
 }catch(e:any){if(request===generation){deviceCode='';view.value=null;message.value='';error.value=e.message||'获取授权码失败';}}
 finally{if(request===generation){phase.value='';emit('busy',false);}}
}
async function verify(){
 if(!active.value||props.disabled||props.blocked||!props.configured||phase.value||!view.value||!deviceCode)return;
 updateClock();if(expired.value)return;
 const request=++generation,code=deviceCode;controller=new AbortController();phase.value='polling';emit('busy',true);error.value='';message.value='等待百度授权并验证账号，轮询间隔由服务端控制…';
 try{
  const data=await post('/api/oauth/token',{device_code:code},controller.signal,request);if(request!==generation)return;
  const result=validateOAuthStatus(data);deviceCode='';view.value=null;stopTimer();
  message.value=result.ready?'授权已验证并保存，请以上方最新账号状态为准。':`验证请求已返回，账号仍需核对：${oauthStatusLabel(result)}。`;
 }catch(e:any){if(request===generation){deviceCode='';view.value=null;stopTimer();message.value='';error.value=(e.message||'验证等待中断')+'。请刷新账号状态核对结果，重新等待前需获取新码。';}}
 finally{if(request===generation){phase.value='';emit('busy',false);emit('settled');}}
}
function cancel(){if(phase.value!=='polling')return;invalidate('已停止本次等待，请刷新账号状态核对；这不代表撤销已经完成的授权。');emit('settled');}
function leave(){active.value=false;invalidate();}
function resume(event:PageTransitionEvent){if(event.persisted)active.value=true;}
watch(()=>[props.configured,props.disabled],()=>{if(!props.configured||props.disabled)invalidate(view.value||phase.value?'配置或登录状态已变化，请重新核对后获取授权码。':'');});
onMounted(()=>{window.addEventListener('pagehide',leave);window.addEventListener('pageshow',resume);});
onUnmounted(()=>{leave();window.removeEventListener('pagehide',leave);window.removeEventListener('pageshow',resume);});
</script>
<template>
 <section class="event-results" data-testid="oauth-device" aria-label="设备码授权">
  <h2>百度账号授权</h2><p class="muted">获取授权码后，在百度页面完成授权，再返回这里验证。同一下载目录绑定一个百度账号。</p>
  <p v-if="!configured" class="notice">应用配置尚未核对，请先查看上方配置状态。</p>
  <div class="oauth-action-row"><button class="button primary" data-testid="oauth-device-get" :disabled="!active||disabled||blocked||!configured||phase==='getting'" @click="getCode">{{phase==='getting'?'正在获取…':view?'重新获取授权码':'获取授权码'}}</button><button v-if="phase==='polling'" class="button secondary" data-testid="oauth-device-cancel" @click="cancel">停止本次等待</button></div>
  <p v-if="message" class="notice" role="status" data-testid="oauth-device-message">{{message}}</p><p v-if="error" class="notice danger" role="alert" data-testid="oauth-device-error">{{error}}</p>
  <div v-if="view" class="oauth-device-view">
   <p class="muted">百度页面需要填写的授权码</p><code class="oauth-user-code" data-testid="oauth-user-code">{{view.userCode}}</code>
   <img v-if="view.qrURL&&!expired" data-testid="oauth-device-qr" class="oauth-device-qr" :src="view.qrURL" alt="百度授权二维码"/>
   <p><a v-if="view.verificationURL&&!expired" class="oauth-authorization-link" data-testid="oauth-device-link" :href="view.verificationURL" target="_blank" rel="noopener noreferrer">打开百度授权页面 ↗</a></p>
   <p class="muted">本机接收 UTC：{{view.receivedAt}}<br/>本机估算截止 UTC：{{new Date(view.deadline).toISOString()}} · 有效性以服务端校验为准。轮询间隔由服务端控制（当前 {{view.interval}} 秒）。</p>
   <button class="button primary" data-testid="oauth-device-verify" :disabled="!active||disabled||blocked||!configured||!!phase||expired" @click="verify">{{phase==='polling'?'正在等待验证…':'已完成百度授权，开始验证'}}</button>
  </div>
 </section>
</template>
