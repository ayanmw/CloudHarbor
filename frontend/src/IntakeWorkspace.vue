<script setup lang="ts">
import {formatBytes} from "./format-bytes";
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {Folder,File,ArrowUp,RefreshCw} from 'lucide-vue-next';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import IntakePreview from './IntakePreview.vue';
import IntakeHistory from './IntakeHistory.vue';
import IntakeRecordDetails from './IntakeRecordDetails.vue';
import {validCloudDirectory} from './cloud-files-route';
import {validateIntake,resultMessage,type IntakeRecord} from './intake-types';

type Entry={id:string;path:string;name:string;directory:boolean;size:number;can_enqueue:boolean};
type Listing={directory:string;queue_dir:string;entries:Entry[];offset:number;total:number|null;next_offset?:number|null};
const pathParameters=new URLSearchParams(location.search).getAll('path');
const selectionRouteError=pathParameters.length>20?'下载预检每批最多20项':pathParameters.some(path=>!validCloudDirectory(path))?'下载路径无效':new Set(pathParameters).size!==pathParameters.length?'下载路径重复，请重新选择':'';
const confirmationPaths=selectionRouteError?[]:pathParameters;
const confirmationPath=pathParameters.length?(confirmationPaths[0]||'/'):'';
const historyOnly=location.pathname==='/intake-history';
const draftDirectory=ref('/'),requestedDirectory=ref('/'),listing=ref<Listing|null>(null),loading=ref(false),error=ref(''),inputError=ref(''),authenticationRequired=ref(false);
const selectedText=ref('');
const selectedRecord=ref<IntakeRecord|null>(null);
const historyRevision=ref(0),actionRecordID=ref('');
const auth=ref<{ready:boolean;state:string;updated_at?:string}|null>(null),authLoading=ref(false),authError=ref('');
const authReady=computed(()=>auth.value?.ready===true&&!authError.value&&!authenticationRequired.value);
const authText=computed(()=>authError.value||(!auth.value?'正在读取账号状态…':auth.value.ready?'账号可用':({unconfigured:'尚未授权',verification_required:'等待账号验证',account_mismatch:'账号不匹配',reauthorization_required:'需要重新授权',token_rejected:'凭证失效，等待续期',storage_error:'凭证保存失败',closed:'服务已停止'} as Record<string,string>)[auth.value.state]||'账号暂不可用'));
let authGeneration=0,authController:AbortController|undefined;
const preview=ref<IntakeRecord|null>(null),action=ref(''),actionError=ref(''),selectionNotice=ref(''),result=ref(''),clock=ref(Date.now());
const selectionLocked=computed(()=>(!!action.value&&action.value!=='preview')||authenticationRequired.value);
let actionGeneration=0,actionController:AbortController|undefined,timer:ReturnType<typeof setInterval>|undefined;
const selectedPaths=computed(()=>[...new Set(selectedText.value.split(/\r?\n/).filter(path=>path.trim()!==''))]);
const selectionError=computed(()=>selectedPaths.value.length>20?'每批最多选择 20 个不同项目':selectedPaths.value.some(path=>!validPath(path))?'每项须为以 / 开头的绝对网盘路径':'');
let generation=0,controller:AbortController|undefined;
function validPath(value:unknown):value is string{return typeof value==='string'&&value.startsWith('/')&&!value.includes('\0');}
function validate(value:Listing,directory:string,offset:number){
 if(!value||value.directory!==directory||value.offset!==offset||!validPath(value.queue_dir)||!Array.isArray(value.entries)||value.entries.length>100||!(value.total===null||(Number.isSafeInteger(value.total)&&value.total>=0)))throw Error('目录响应无效，请重新读取');
 if(value.next_offset!=null&&(!Number.isSafeInteger(value.next_offset)||value.next_offset<=offset))throw Error('目录分页响应无效');
 if(value.entries.some(e=>!e||typeof e.id!=='string'||!validPath(e.path)||typeof e.name!=='string'||typeof e.directory!=='boolean'||typeof e.can_enqueue!=='boolean'||typeof e.size!=='number'||!Number.isFinite(e.size)||e.size<0))throw Error('目录条目响应无效');
 return value;
}
async function load(directory:string,offset=0){
 if(authenticationRequired.value)return;
 if(!validPath(directory)){inputError.value='请输入以 / 开头的绝对网盘路径';return;}
 inputError.value='';requestedDirectory.value=directory;draftDirectory.value=directory;loading.value=true;error.value='';
 const request=++generation;controller?.abort();controller=new AbortController();
 try{
  const response=await fetch('/api/cloud?'+new URLSearchParams({dir:directory,offset:String(offset),limit:'100'}),{signal:controller.signal,credentials:'same-origin'});
  if(request!==generation)return;
  if(response.status===401){requireLogin();throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'网盘目录暂不可读');
  const value=await response.json();if(request!==generation)return;
  listing.value=validate(value,directory,offset);
 }catch(e:any){if(request===generation)error.value=e.message||'目录读取失败';}
 finally{if(request===generation)loading.value=false;}
}
function parent(){const path=requestedDirectory.value.replace(/\/+$/,'');return path.slice(0,path.lastIndexOf('/'))||'/';}
// Capacity audit found this legacy browser still labelled binary units as KB/TB.
const bytes = formatBytes;
function canPage(){return !!listing.value&&!loading.value&&!error.value&&!authenticationRequired.value&&listing.value.directory===requestedDirectory.value;}
function setPaths(paths:string[]){selectedText.value=paths.join('\n');}
function removePath(path:string){setPaths(selectedPaths.value.filter(value=>value!==path));}
function toggle(entry:Entry){
 if(selectionLocked.value||!canPage()||!entry.can_enqueue)return;
 if(selectedPaths.value.includes(entry.path))removePath(entry.path);
 else if(selectedPaths.value.length<20)setPaths([...selectedPaths.value,entry.path]);
}
function discardPreview(){
 if(preview.value||action.value==='preview')selectionNotice.value='选择已变化，请重新预检。';
 preview.value=null;
 if(action.value==='preview'){actionGeneration++;actionController?.abort();action.value='';}
}
function requireLogin(){if(authenticationRequired.value)return;authenticationRequired.value=true;discardPreview();selectedRecord.value=null;location.assign('/login?next=%2Fintake');}
async function loadAuth(){
 if(authLoading.value||authenticationRequired.value)return;
 const request=++authGeneration;authController=new AbortController();authLoading.value=true;
 try{
  const response=await fetch('/api/oauth/status',{credentials:'same-origin',signal:authController.signal});if(request!==authGeneration)return;
  if(response.status===401){requireLogin();throw Error('请重新登录');}
  if(!response.ok)throw Error('账号状态暂不可读');
  const value=await response.json();if(request!==authGeneration)return;
  if(!value||typeof value.ready!=='boolean'||typeof value.state!=='string')throw Error('账号状态响应无效');
  if(!value.ready||(auth.value?.updated_at&&value.updated_at!==auth.value.updated_at)){discardPreview();selectionNotice.value='账号状态已变化，请核对账号后重新预检。';}
  auth.value=value;authError.value='';
 }catch(e:any){if(request===authGeneration){authError.value=e.message||'账号状态读取失败';discardPreview();}}
 finally{if(request===authGeneration)authLoading.value=false;}
}
watch(selectedText,discardPreview,{flush:'sync'});
async function requestRecord(url:string,body:unknown,signal:AbortSignal){
 const response=await fetch(url,{method:'POST',headers:{'Content-Type':'application/json'},credentials:'same-origin',signal,body:JSON.stringify(body)});
 if(response.status===401){requireLogin();throw Error('请重新登录');}
 if(!response.ok)throw Error(await response.text()||'入队请求失败');
 return validateIntake(await response.json());
}
async function createPreview(){
 if(action.value||!authReady.value||selectionError.value||!selectedPaths.value.length)return;
 const paths=[...selectedPaths.value],request=++actionGeneration;actionController?.abort();actionController=new AbortController();action.value='preview';preview.value=null;actionError.value='';selectionNotice.value='';result.value='';actionRecordID.value='';
 try{
  const record=await requestRecord('/api/intakes/preview',{paths},actionController.signal);if(request!==actionGeneration)return;
  if(record.state!=='preview'||record.items.length!==paths.length||record.items.some((item,index)=>item.source.path!==paths[index]||item.state!=='ready'))throw Error('预检项目与当前选择不匹配，请重新预检');
  clock.value=Date.now();preview.value=record;
 }catch(e:any){if(request===actionGeneration)actionError.value=e.message||'预检失败';}
 finally{if(request===actionGeneration)action.value='';}
}
async function commit(){
 if(action.value||!authReady.value||!preview.value||Date.now()>=Date.parse(preview.value.expires))return;
 const record=preview.value,request=++actionGeneration;preview.value=null;action.value='commit';actionRecordID.value=record.id;actionError.value='';result.value='正在提交移动，请勿重复操作。';actionController=new AbortController();
 try{
  const response=await requestRecord('/api/intakes/'+record.id+'/commit',{},actionController.signal);if(request!==actionGeneration)return;
  if(response.id!==record.id)throw Error('提交响应批次不匹配');
  result.value=resultMessage(response);
 }catch(e:any){if(request===actionGeneration)result.value=(e.message||'提交中断')+'。请核对入队记录，本次不会自动重发移动。';}
 finally{if(request===actionGeneration){action.value='';historyRevision.value++;load(requestedDirectory.value);}}
}
async function recheck(record:IntakeRecord){
 if(action.value||!authReady.value||!record.items.some(item=>item.state==='unknown'||item.state==='sending'))return;
 discardPreview();action.value='recheck';actionRecordID.value=record.id;const request=++actionGeneration;actionController=new AbortController();result.value='正在核对可确认项目，不会重新发出移动请求…';
 try{
  const response=await requestRecord('/api/intakes/'+record.id+'/recheck',{},actionController.signal);if(request!==actionGeneration)return;
  if(response.id!==record.id)throw Error('核对响应批次不匹配');
  result.value=resultMessage(response);if(selectedRecord.value?.id===record.id)selectedRecord.value=response;
 }catch(e:any){if(request===actionGeneration)result.value=(e.message||'核对失败')+'。记录仍需核对，本次不会重发移动。';}
 finally{if(request===actionGeneration){action.value='';historyRevision.value++;load(requestedDirectory.value);}}
}
function consumeHandoff(){
 const prefix='#nas-intake=';if(!location.hash.startsWith(prefix))return;
 const encoded=location.hash.slice(prefix.length);
 try{history.replaceState(null,'',location.pathname+location.search);}catch{selectionNotice.value='无法清除外部输入，请关闭此页并在管理页直接选择。';return;}
 try{
  if(encoded.length>60000)throw Error();const value=JSON.parse(decodeURIComponent(encoded));
  if(!value||Array.isArray(value)||Object.keys(value).sort().join(',')!=='paths,version'||value.version!==1||!Array.isArray(value.paths)||!value.paths.length||value.paths.length>20)throw Error();
  for(const path of value.paths)if(typeof path!=='string'||!path.length||path.length>2048||!path.startsWith('/')||path==='/'||/[\\\u0000-\u001f\u007f-\u009f]/u.test(path)||path.slice(1).split('/').some((part:string)=>!part||part==='.'||part==='..'))throw Error();
  if(new Set(value.paths).size!==value.paths.length)throw Error();
  selectedText.value=value.paths.join('\n');selectionNotice.value='已接收外部路径，尚未预检或移动。请核对本服务当前授权的百度账号及路径，再手动预检和确认。';
 }catch{selectionNotice.value='外部选择数据无效，未导入；请在本页直接选择项目。';}
}
consumeHandoff();
onMounted(()=>{document.title=(historyOnly?'入队记录':'下载预检')+' · NAS Downloader';loadAuth();if(confirmationPath)selectedText.value=confirmationPaths.join('\n');else if(!historyOnly)load('/');timer=setInterval(()=>clock.value=Date.now(),500);});
onUnmounted(()=>{generation++;controller?.abort();actionGeneration++;actionController?.abort();authGeneration++;authController?.abort();clearInterval(timer);});
</script>

<template>
 <div class="workspace event-workspace intake-workspace">
  <WorkspaceSidebar :active="historyOnly?'/intake-history':'/cloud/files'" :inert="!!selectedRecord" @error="error=$event"/>
  <main class="main" :inert="!!selectedRecord">
   <header class="page-header"><div><p class="eyebrow">网盘 → 自动下载队列</p><h1>{{historyOnly?'入队记录':confirmationPath?'下载预检':'添加网盘项目'}}</h1><p class="muted">{{historyOnly?'只读查看预检、入队及核对记录；不会自动重发移动。':'选择网盘项目，核对后移动到自动监控目录，由后台检测下载。'}}</p></div><a class="button secondary" href="/cloud/files">返回网盘文件</a><button v-if="!historyOnly&&!confirmationPath" class="button secondary" :disabled="loading||authenticationRequired" @click="load(requestedDirectory)"><RefreshCw :size="16"/>刷新目录</button></header>
   <p v-if="selectionRouteError" class="notice danger" role="alert">{{selectionRouteError}}</p><section class="intake-account-status"><p data-testid="intake-auth" role="status">{{authText}}</p><button class="button secondary" data-testid="intake-auth-refresh" :disabled="authLoading||authenticationRequired" @click="loadAuth">刷新账号状态</button><a href="/oauth">账号与授权</a></section>
   <section v-if="!historyOnly&&!confirmationPath" class="event-results intake-browser" data-testid="intake-browser" aria-label="网盘目录浏览">
    <form class="intake-directory-toolbar" data-testid="intake-directory-form" @submit.prevent="load(draftDirectory)">
     <button class="button secondary" type="button" data-testid="intake-up" :disabled="requestedDirectory==='/'||authenticationRequired" @click="load(parent())"><ArrowUp :size="16"/>上一级</button>
     <label class="intake-path-input">网盘路径<input v-model="draftDirectory" name="directory" autocomplete="off" placeholder="/" :disabled="authenticationRequired"/></label><button class="button secondary" :disabled="authenticationRequired">打开目录</button>
    </form>
    <p data-testid="intake-path" class="intake-current-path">{{requestedDirectory}}</p>
    <p v-if="inputError" class="notice danger" role="alert">{{inputError}}</p>
    <p v-if="error" class="notice danger" role="alert">{{error}}<span v-if="listing"> · 下方保留上次读取结果，尚未重新核对。</span></p>
    <div class="event-result-summary" role="status" aria-live="polite">{{loading?'正在读取目录…':listing?`${listing.total===null?'总数未知':`共 ${listing.total} 项`} · 本页 ${listing.entries.length} 项`:'尚未读取目录'}}</div>
    <div class="event-table-scroll"><table><thead><tr><th>选择</th><th>名称</th><th>类型</th><th>大小</th></tr></thead><tbody>
     <tr v-for="entry in listing?.entries||[]" :key="entry.path"><td><input type="checkbox" data-testid="intake-select" :aria-label="`选择 ${entry.name}`" :checked="selectedPaths.includes(entry.path)" :disabled="selectionLocked||!canPage()||!entry.can_enqueue||(!selectedPaths.includes(entry.path)&&selectedPaths.length>=20)" @change="toggle(entry)"/></td><td><button v-if="entry.directory" class="event-detail-link intake-entry-name" data-testid="intake-open" :disabled="!canPage()" @click="load(entry.path)"><Folder :size="17"/>{{entry.name}}</button><span v-else class="intake-entry-name"><File :size="17"/>{{entry.name}}</span><small v-if="!entry.can_enqueue" class="cell-sub">不可选：可能已在队列、历史或隔离范围内</small></td><td>{{entry.directory?'文件夹':'文件'}}</td><td data-testid="intake-size">{{entry.directory?'—':bytes(entry.size)}}</td></tr>
    </tbody></table></div>
    <p v-if="listing&&!listing.entries.length&&!loading&&!error" class="event-empty">此目录暂无项目。可以返回上一级或打开其他目录。</p>
    <div class="intake-pagination"><button class="button secondary" data-testid="intake-prev" :disabled="!canPage()||!listing?.offset" @click="load(listing!.directory,Math.max(0,listing!.offset-100))">上一页</button><span>{{listing?`从第 ${listing.offset+1} 项开始`:'尚未读取'}}</span><button class="button secondary" data-testid="intake-next" :disabled="!canPage()||listing?.next_offset==null" @click="load(listing!.directory,listing!.next_offset!)">下一页</button></div>
    <p v-if="listing" class="muted">自动监控目录：{{listing.queue_dir}} · 目录大小未统计时不展示为 0 B。</p>
   </section>
   <section v-if="!historyOnly" class="event-results intake-selection" aria-label="已选网盘项目">
    <div class="intake-pagination"><h2>已选项目 <small>{{selectedPaths.length}} / 20</small></h2><button class="button secondary" data-testid="intake-clear" :disabled="!selectedPaths.length||selectionLocked" @click="setPaths([])">清空选择</button></div>
    <p class="muted">可以跨目录选择，也可手工输入绝对网盘路径，每行一项。这里保存的是路径选择，预检时仍须在线核对身份与冲突。</p>
    <label>已选路径<textarea v-model="selectedText" name="paths" rows="4" class="intake-paths" :disabled="selectionLocked" placeholder="/我的文件/课程目录"/></label>
    <p v-if="selectionError" data-testid="intake-selection-error" class="notice danger" role="alert">{{selectionError}}</p>
    <ul class="intake-selected-list"><li v-for="path in selectedPaths" :key="path" data-testid="intake-selected-item"><span>{{path}}</span><button class="button secondary" data-testid="intake-remove" :aria-label="`移除 ${path}`" :disabled="selectionLocked" @click="removePath(path)">移除</button></li></ul>
    <p v-if="!selectedPaths.length" class="muted">尚未选择项目。</p>
    <p v-if="selectionNotice" data-testid="intake-selection-notice" class="notice" role="status">{{selectionNotice}}</p>
    <button class="button primary" data-testid="intake-preview" :disabled="!!action||!authReady||!!selectionError||!selectedPaths.length" @click="createPreview">{{action==='preview'?'正在预检…':'预检已选项目'}}</button>
    <p v-if="actionError" data-testid="intake-action-error" class="notice danger" role="alert">{{actionError}}</p>
    <p v-if="result" data-testid="intake-result" class="notice" role="status">{{result}}</p>
   </section>
  <p v-if="!historyOnly"><a href="/intake-history">查看入队记录</a></p><IntakePreview v-if="preview" :key="preview.id" :record="preview" :now="clock" :busy="!!action" :disabled="!authReady" @confirm="commit"/>
  <IntakeHistory v-if="historyOnly||!confirmationPath" :revision="historyRevision" :disabled="authenticationRequired" @select="selectedRecord=$event" @authentication-required="requireLogin"/>
  </main>
  <IntakeRecordDetails v-if="selectedRecord" :key="selectedRecord.id" :record="selectedRecord" :busy="!!action" :disabled="!authReady" :feedback="actionRecordID===selectedRecord.id?result:''" @recheck="recheck" @close="selectedRecord=null"/>
 </div>
</template>
