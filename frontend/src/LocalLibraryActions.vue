<script setup lang="ts">
import {computed,nextTick,onMounted,onUnmounted,ref,watch} from 'vue';
import {FolderPlus,Upload,FolderUp,History,Trash2,X,RotateCcw} from 'lucide-vue-next';
import {formatBytes} from './format-bytes';
type Item={name:string;path:string;revision:string;directory:boolean};
type Request={id:string;kind:string;root_revision:string;path?:string;revision?:string;target?:string;parent_identity?:string;trash_id?:string;size?:string};
type Operation={id:string;request:Request;root_identity:string;root_path?:string;item_path?:string;status:string;error?:string;started_at_utc:string;finished_at_utc?:string;recovered_at_utc?:string;bytes:string;sha256?:string;restored_by?:string};
type UploadItem={file:File;relative:string;status:string;loaded:number;error:string};
const props=defineProps<{rootRevision:string;rootIdentity:string;path:string;parentIdentity:string;enabled:boolean;readable:boolean;refresh:()=>Promise<void>}>();
const emit=defineEmits<{busy:[value:boolean];blocked:[value:boolean]}>();
const busy=ref(false),notice=ref(''),panel=ref(''),mode=ref(''),selected=ref<Item|null>(null),name=ref(''),destination=ref(''),restoreItem=ref<Operation|null>(null);
const fileInput=ref<HTMLInputElement>(),directoryInput=ref<HTMLInputElement>(),toolbar=ref<HTMLDivElement>();
const records=ref<Operation[]>([]),historyOffset=ref(0),historyLoading=ref(false),historyError=ref('');
const batchTarget=ref('');
const uploads=ref<UploadItem[]>([]),stop=ref(false),pending=ref<string[]>([]);
const canWrite=computed(()=>props.enabled&&!busy.value&&!pending.value.length);
const trashRecords=computed(()=>records.value.filter(o=>o.request.kind==='trash'&&o.status==='succeeded'&&!o.restored_by&&o.root_identity===props.rootIdentity));
let active=true,xhr:XMLHttpRequest|undefined,returnFocus:HTMLElement|null=null,historyGeneration=0;
const pendingKey='cloudharbor.library.pending.v1';
// getRandomValues also works on an ordinary LAN HTTP origin. randomUUID requires a secure context.
function id(){const bytes=crypto.getRandomValues(new Uint8Array(16));return Array.from(bytes,b=>b.toString(16).padStart(2,'0')).join('');}
function readPending(){try{const v=JSON.parse(sessionStorage.getItem(pendingKey)||'[]');return Array.isArray(v)?v.filter(x=>typeof x==='string'&&/^[a-f0-9]{32}$/.test(x)).slice(0,1000):[]}catch{return []}}
function remember(value:string,keep:boolean){const current=readPending().filter(x=>x!==value);if(keep)current.push(value);sessionStorage.setItem(pendingKey,JSON.stringify(current));if(active){pending.value=current;emit('blocked',!!current.length)}}
function setBusy(v:boolean){busy.value=v;emit('busy',v)}
function join(a:string,b:string){return a&&b?a+'/'+b:a||b}
function message(e:any){return e?.message||'请求失败，请核对操作记录。'}
function label(kind:string){return ({mkdir:'新建目录',rename:'重命名',move:'移动',trash:'移入回收站',restore:'恢复',upload:'上传',manage:'管理项目'} as Record<string,string>)[kind]||kind}
function status(value:string){return ({succeeded:'成功',failed:'失败',prepared:'正在核对',unknown:'结果未知'} as Record<string,string>)[value]||value}
function contentURL(item:Item){return '/api/library/content?'+new URLSearchParams({path:item.path,revision:item.revision})}
function date(v:string){return v?new Date(v).toLocaleString():'—'}
async function receipt(value:string){const r=await fetch('/api/library/operations?id='+encodeURIComponent(value),{credentials:'same-origin'});if(!r.ok)throw Error(r.status===404?'暂未取得完成凭据，请保留操作ID继续查询，不要重发。':'操作记录读取失败。');const op=await r.json();if(op.id!==value||!['succeeded','failed','unknown','prepared'].includes(op.status))throw Error('操作记录格式无效。');return op as Operation}
function accept(op:Operation){if(op.status==='succeeded'||op.status==='failed')remember(op.id,false);if(op.status!=='succeeded')throw Error(op.error||'操作结果尚未确认，请查看记录，不要重发。');return op}
async function execute(req:Request){
 remember(req.id,true);let r:Response;
 try{r=await fetch('/api/library/operations',{method:'POST',credentials:'same-origin',headers:{'Content-Type':'application/json'},body:JSON.stringify(req)});}catch(e){if(!active)throw e;return accept(await receipt(req.id))}
 let body:any;try{body=await r.json()}catch{if(!active)throw Error('页面已离开，保留操作ID');return accept(await receipt(req.id))}
 if(body.id!==req.id)throw Error('操作响应身份不符，请查询原操作ID。');
 if(body.admitted===false){remember(req.id,false);throw Error(body.error||'操作未执行。')}
 if(!['succeeded','failed','unknown','prepared'].includes(body.status))throw Error('操作响应缺少有效凭据，请查询原ID。');return accept(body)
}
function focusBack(){void nextTick(()=>{if(returnFocus?.isConnected)returnFocus.focus();else toolbar.value?.querySelector('button')?.focus()})}
function close(){if(busy.value)return;mode.value='';selected.value=null;restoreItem.value=null;focusBack()}
function dialogKeydown(event:KeyboardEvent){
 if(event.key==='Escape'){event.preventDefault();event.stopPropagation();close();return}
 if(event.key!=='Tab')return;
 const scope=event.currentTarget as HTMLElement;
 const fields=Array.from(scope.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),a[href]'));
 const first=fields[0],last=fields[fields.length-1];
 if(event.shiftKey&&document.activeElement===first){event.preventDefault();last?.focus()}
 else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first?.focus()}
}
function open(kind:string,item?:Item,op?:Operation){if(kind!=='manage'&&!canWrite.value){notice.value='请先刷新当前目录并核对未决操作，再进行文件管理。';return};// Preserve the original row trigger when switching from the compact menu to a form.
if(mode.value!=='manage'||kind==='manage')returnFocus=document.activeElement as HTMLElement;mode.value=kind;selected.value=item||null;restoreItem.value=op||null;name.value=kind==='rename'?item?.name||'':'';destination.value=props.path;notice.value='';void nextTick(()=>{const dialog=document.querySelector<HTMLElement>('.library-operation-dialog');(dialog?.querySelector<HTMLElement>('input')||dialog?.querySelector<HTMLElement>('[data-primary]')||dialog?.querySelector<HTMLElement>('button'))?.focus()})}
defineExpose({open});
async function parent(path:string){
 const r=await fetch('/api/library/entries?'+new URLSearchParams({path,limit:'1'}),{credentials:'same-origin'});
 const b=await r.json();
 if(!r.ok||b.stale||b.root_revision!==props.rootRevision||typeof b.directory_identity!=='string'||!b.directory_identity){throw Object.assign(Error(b.error||'目标目录无法核对，请先确认路径存在。'),{status:r.status})}
 return b.directory_identity as string;
}

async function submit(){
 if(!canWrite.value||!mode.value)return;const kind=mode.value,item=selected.value;const req:Request={id:'',kind,root_revision:props.rootRevision};
 try{req.id=id();
  if(kind==='mkdir'||kind==='rename'){if(!name.value||/[\\/]/.test(name.value))throw Error('请输入单个目录或文件名称，不能含路径分隔符。');req.target=join(props.path,name.value);req.parent_identity=props.parentIdentity}
  if(kind==='rename'||kind==='move'||kind==='trash'){if(!item)throw Error('尚未选择项目');req.path=item.path;req.revision=item.revision}
  setBusy(true);
  if(kind==='move'){const dest=destination.value.trim().replace(/^\/+|\/+$/g,'');req.target=join(dest,item!.name);req.parent_identity=await parent(dest)}
  if(kind==='restore')req.trash_id=restoreItem.value?.id;
  await execute(req);if(!active)return;mode.value='';notice.value=label(kind)+'成功。';await props.refresh();if(panel.value==='history'||panel.value==='trash')void loadHistory();
 }catch(e){if(active)notice.value=message(e)}finally{if(active){setBusy(false);if(!mode.value)focusBack()}}
}
async function loadHistory(){const gen=++historyGeneration;historyLoading.value=true;historyError.value='';try{const r=await fetch('/api/library/operations?'+new URLSearchParams({offset:String(historyOffset.value),limit:'50'}),{credentials:'same-origin'});if(!r.ok)throw Error('操作记录读取失败，保留上次结果。');const b=await r.json();if(!Array.isArray(b.items))throw Error('操作记录格式无效');if(active&&gen===historyGeneration)records.value=b.items;}catch(e){if(active&&gen===historyGeneration)historyError.value=message(e)}finally{if(active&&gen===historyGeneration)historyLoading.value=false}}
function show(value:string){panel.value=panel.value===value?'':value;if(panel.value==='history'||panel.value==='trash'){historyOffset.value=0;void loadHistory()}}
async function checkPending(value:string){try{const op=await receipt(value);accept(op);notice.value='原操作已完成，列表已刷新。';await props.refresh()}catch(e){notice.value=message(e)}}
function choose(event:Event){const input=event.target as HTMLInputElement;const files=Array.from(input.files||[]);input.value='';if(!files.length)return;if(files.length>1000){notice.value='每批最多1000个文件，请缩小范围。';return}batchTarget.value=props.path;uploads.value=files.map(file=>({file,relative:file.webkitRelativePath||file.name,status:'待上传',loaded:0,error:''}));panel.value='uploads';notice.value='已选择文件，点击开始上传后执行。目录选择不包含空目录。'}
async function ensureDirectories(relative:string,base:string,cache:Map<string,string>,rootRevision:string){
 let p=base;
 for(const part of relative.split('/').filter(Boolean)){
  if(stop.value||!active)throw Error('已停止后续目录准备');
  const next=join(p,part);
  if(!cache.has(next)){
   let found:string;
   try{found=await parent(next)}catch(e:any){
    if(e.status!==404)throw e;
    if(stop.value||!active)throw Error('已停止后续目录准备');
    await execute({id:id(),kind:'mkdir',root_revision:rootRevision,target:next,parent_identity:cache.get(p)!});
    await props.refresh();
    found=await parent(next);
   }
   cache.set(next,found);
  }
  p=next;
 }
 return cache.get(p)!;
}

function sendFile(item:UploadItem,req:Request){return new Promise<Operation>((resolve,reject)=>{const upload=new XMLHttpRequest();xhr=upload;const q=new URLSearchParams({id:req.id,root_revision:req.root_revision,target:req.target!,parent_identity:req.parent_identity!,size:String(item.file.size)});upload.open('POST','/api/library/upload?'+q);upload.withCredentials=true;upload.setRequestHeader('Content-Type','application/octet-stream');upload.upload.onprogress=e=>{if(active){item.loaded=e.loaded;item.status=e.loaded>=item.file.size?'校验并发布…':'上传中'}};upload.onload=()=>{try{const op=JSON.parse(upload.responseText);if(op.id!==req.id)throw Error('上传响应身份不符');if(op.admitted===false){remember(req.id,false);reject(Error(op.error));return}resolve(accept(op))}catch(e){reject(e)}};upload.onerror=()=>reject(Error('上传连接中断'));upload.onabort=()=>reject(Error('上传已停止，请查询原操作ID确认结果'));upload.send(item.file)})}
async function startUpload(){if(!canWrite.value||!uploads.value.some(x=>x.status==='待上传'))return;setBusy(true);stop.value=false;const base=props.path,revision=props.rootRevision;const cache=new Map([[base,props.parentIdentity]]);
 try{for(const item of uploads.value){if(stop.value||!active)break;if(item.status!=='待上传')continue;let uploadID='';try{if(item.file.size>50*1024**3)throw Error('单文件最大50GiB');const parts=item.relative.split('/');if(parts.some(p=>!p||p==='.'||p==='..'||/[\\:]/.test(p)))throw Error('文件路径无效');const filename=parts.pop()!;item.status='准备目录…';const pid=await ensureDirectories(parts.join('/'),base,cache,revision);if(stop.value||!active)break;const req:Request={id:id(),kind:'upload',root_revision:revision,target:join(join(base,parts.join('/')),filename),parent_identity:pid};uploadID=req.id;remember(req.id,true);await sendFile(item,req);item.loaded=item.file.size;item.status='成功';await props.refresh();}catch(e){if(!active)break;if(stop.value&&!uploadID&&!pending.value.length){item.status='未执行';item.error='已停止，未发送文件';break}if(uploadID&&readPending().includes(uploadID)){try{accept(await receipt(uploadID));item.status='成功';item.loaded=item.file.size;await props.refresh();continue}catch(check){e=check}}item.status=pending.value.length?'待核对':'失败';item.error=message(e);if(pending.value.length){stop.value=true;notice.value='存在未决操作，已停止后续上传，请查询原操作ID。'}}}}
 finally{if(active){for(const item of uploads.value)if(item.status==='待上传'||item.status==='准备目录…')item.status='未执行';setBusy(false);xhr=undefined}}
}
function stopUpload(){stop.value=true;xhr?.abort()}
watch(()=>[props.rootRevision,props.path],()=>{if(busy.value)stopUpload();else{close();if(uploads.value.some(x=>x.status==='待上传')){for(const item of uploads.value)if(item.status==='待上传')item.status='未执行';notice.value='浏览目录已变化，原批次未执行，请在目标目录重新选择文件。'}}});
onMounted(()=>{pending.value=readPending();emit('blocked',!!pending.value.length)});
onUnmounted(()=>{active=false;historyGeneration++;stop.value=true;xhr?.abort();emit('busy',false)});
</script>

<template>
 <div ref="toolbar" class="library-management-toolbar">
  <button class="button secondary compact" data-testid="library-upload-files" :disabled="!canWrite" @click="fileInput?.click()"><Upload :size="15"/>上传文件</button>
  <button class="button secondary compact" :disabled="!canWrite" @click="directoryInput?.click()"><FolderUp :size="15"/>上传目录</button>
  <button class="button secondary compact" data-testid="library-mkdir" :disabled="!canWrite" @click="open('mkdir')"><FolderPlus :size="15"/>新建目录</button>
  <span class="toolbar-spacer"/>
  <button class="button secondary compact" :aria-pressed="panel==='trash'" :disabled="busy" @click="show('trash')"><Trash2 :size="15"/>回收站</button>
  <button class="button secondary compact" :aria-pressed="panel==='history'" :disabled="busy" @click="show('history')"><History :size="15"/>操作记录</button>
  <button v-if="uploads.length" class="button secondary compact" :aria-pressed="panel==='uploads'" @click="show('uploads')">上传 {{uploads.filter(x=>x.status==='成功').length}}/{{uploads.length}}</button>
  <input ref="fileInput" data-testid="library-file-input" type="file" multiple hidden @change="choose"/><input ref="directoryInput" type="file" multiple webkitdirectory hidden @change="choose"/>
 </div>
 <p v-if="notice" class="notice" role="status">{{notice}}</p>
 <div v-if="pending.length" class="notice danger" role="alert"><strong>有 {{pending.length}} 项操作尚未取得完成凭据，已暂停新的写操作。</strong><p>只查询原操作ID，不自动重发。进程中断的未知结果需要核对本机文件与操作记录。</p><div v-for="value in pending" :key="value" class="library-pending"><code>{{value}}</code><button class="button secondary compact" :disabled="busy" @click="checkPending(value)">查询结果</button></div></div>
 <section v-if="panel==='uploads'" class="library-work-panel"><header><h2>上传批次</h2><div><button class="button primary compact" :disabled="!canWrite||!uploads.some(x=>x.status==='待上传')" @click="startUpload">开始上传</button><button class="button secondary compact" :disabled="!busy" @click="stopUpload">停止上传</button></div></header><p class="muted">批次目标：/{{batchTarget}} · 同名不覆盖 · 每批最多1000文件 · 单文件50GiB · 串行上传，浏览器关闭后不自动续传。批次进度仅本页展示，刷新后请在操作记录查看已提交项目。</p><div class="event-table-scroll"><table class="library-job-table"><thead><tr><th>文件</th><th>大小</th><th>进度</th><th>状态</th></tr></thead><tbody><tr v-for="item in uploads" :key="item.relative"><td>{{item.relative}}<small v-if="item.error" class="danger-text">{{item.error}}</small></td><td>{{formatBytes(item.file.size)}}</td><td><progress :value="item.loaded" :max="item.file.size||1" :aria-label="item.relative+' 上传进度'"/> {{formatBytes(item.loaded)}}</td><td>{{item.status}}</td></tr></tbody></table></div></section>
 <section v-if="panel==='history'||panel==='trash'" class="library-work-panel"><header><h2>{{panel==='trash'?'回收站':'操作记录'}}</h2><button class="button secondary compact" :disabled="historyLoading" @click="loadHistory">刷新记录</button></header><p class="muted">{{panel==='trash'?'记录中的回收项目，恢复前会核对实际身份；原路径被占用时不覆盖。本版不永久清空。':'本地文件库操作证据，时间区分开始、结束与重启发现；历史记录不代表当前文件仍存在。'}}</p><p v-if="historyError" class="notice danger" role="alert">{{historyError}}</p><div class="event-table-scroll"><table class="library-job-table"><thead><tr><th>项目／路径</th><th>操作</th><th>状态</th><th>时间与证据</th><th>操作</th></tr></thead><tbody><tr v-for="op in panel==='trash'?trashRecords:records" :key="op.id"><td>{{op.item_path||op.request.path||op.request.target||op.request.trash_id}}<small v-if="op.request.path&&op.request.target">→ {{op.request.target}}</small></td><td>{{label(op.request.kind)}}</td><td>{{status(op.status)}}<small v-if="op.restored_by">已恢复</small></td><td><details><summary>{{date(op.started_at_utc)}}</summary><p>开始：{{date(op.started_at_utc)}}<br/>结束：{{date(op.finished_at_utc||'')}}<br v-if="op.recovered_at_utc"/><span v-if="op.recovered_at_utc">重启核对：{{date(op.recovered_at_utc)}}</span></p><code>{{op.id}}</code><p v-if="op.root_path">文件库：{{op.root_path}}</p><p v-if="op.error">{{op.error}}</p><p v-if="op.sha256" class="library-hash">SHA256 {{op.sha256}}</p></details></td><td><button v-if="op.request.kind==='trash'&&op.status==='succeeded'&&!op.restored_by" class="button secondary compact" :disabled="!canWrite||op.root_identity!==rootIdentity" @click="open('restore',undefined,op)"><RotateCcw :size="14"/>恢复</button><span v-else>—</span></td></tr></tbody></table></div><p v-if="!(panel==='trash'?trashRecords:records).length" class="muted">{{historyLoading?'正在读取…':'本页暂无对应记录。'}}</p><footer><span>按操作记录分页 · 从第 {{historyOffset+1}} 条起</span><div><button class="button secondary compact" :disabled="!historyOffset||historyLoading" @click="historyOffset-=50;loadHistory()">上一页</button><button class="button secondary compact" :disabled="records.length<50||historyLoading" @click="historyOffset+=50;loadHistory()">下一页</button></div></footer></section>
 <div v-if="mode" class="library-dialog-backdrop" @keydown="dialogKeydown"><section class="library-operation-dialog" role="dialog" aria-modal="true" aria-labelledby="library-operation-title"><header><h2 id="library-operation-title">{{label(mode)}}</h2><button class="button secondary compact" aria-label="关闭操作" :disabled="busy" @click="close"><X :size="16"/></button></header><p v-if="selected" class="library-source">{{selected.path}}</p><p v-if="mode==='trash'" class="notice">{{selected?.directory?'目录及其当前全部内容':'文件'}}将移入本站回收站，可恢复；不会操作百度网盘。</p><p v-if="mode==='restore'" class="notice">恢复到 {{restoreItem?.request.path}}，路径被占用时会拒绝，不覆盖已有内容。</p><div v-if="mode==='manage'" class="library-management-menu"><a v-if="selected&&!selected.directory&&readable" class="button secondary" :href="contentURL(selected)" download @click="close">下载文件</a><button class="button secondary" :disabled="!canWrite" @click="open('rename',selected!)">重命名</button><button class="button secondary" :disabled="!canWrite" @click="open('move',selected!)">移动到…</button><button class="button secondary" :disabled="!canWrite" @click="open('trash',selected!)">移入回收站</button></div><form v-else @submit.prevent="submit"><template v-if="mode==='mkdir'||mode==='rename'"><label for="library-operation-name">{{mode==='mkdir'?'目录名称':'新名称'}}</label><input id="library-operation-name" v-model="name" maxlength="255" required :disabled="busy"/></template><template v-if="mode==='move'"><label for="library-operation-destination">目标目录（相对本站文件库，留空为根目录）</label><input id="library-operation-destination" v-model="destination" :disabled="busy" placeholder="例如：资料/归档"/><p class="muted">保留原名称，仅在同一文件库内移动。目标必须已存在。</p></template><p v-if="notice" class="notice" role="alert">{{notice}}</p><footer><button type="button" class="button secondary" data-testid="library-operation-cancel" :disabled="busy" @click="close">取消</button><button data-primary type="submit" class="button primary" data-testid="library-operation-confirm" :disabled="!canWrite">{{busy?'正在执行…':label(mode)}}</button></footer></form></section></div>
</template>
<style scoped>
.library-management-toolbar{display:flex;gap:8px;align-items:center;flex-wrap:wrap;margin:0 0 14px}.toolbar-spacer{flex:1}.library-management-toolbar .button{border:1px solid #dce3ee}.library-work-panel{border:1px solid #dfe5ee;border-radius:10px;background:#fff;padding:16px;margin:14px 0}.library-work-panel header,.library-operation-dialog header{display:flex;justify-content:space-between;align-items:center;gap:12px}.library-work-panel h2,.library-operation-dialog h2{margin:0;font-size:17px}.library-work-panel header>div,.library-work-panel footer>div{display:flex;gap:8px}.library-job-table{width:100%;min-width:650px;font-size:13px}.library-job-table th,.library-job-table td{text-align:left;padding:9px;border-bottom:1px solid #e5eaf2;vertical-align:top}.library-job-table td:first-child{max-width:320px;overflow-wrap:anywhere}.library-job-table small{display:block;margin-top:4px;overflow-wrap:anywhere}.library-job-table summary{cursor:pointer}.library-work-panel footer{display:flex;justify-content:space-between;gap:8px;align-items:center;margin-top:12px;font-size:12px}.library-job-table progress{width:100px}.danger-text{color:#b91c1c}.library-pending{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:8px}.library-dialog-backdrop{position:fixed;inset:0;background:#15243b66;z-index:1000;display:flex;align-items:center;justify-content:center;padding:20px}.library-operation-dialog{width:min(520px,100%);max-height:90vh;overflow:auto;background:white;border:1px solid #dce3ee;border-radius:12px;padding:22px;box-shadow:0 20px 60px #15243b33}.library-operation-dialog label{display:block;margin:16px 0 8px}.library-operation-dialog input{width:100%;box-sizing:border-box;border:1px solid #d2dce9;border-radius:7px;padding:10px}.library-operation-dialog footer{display:flex;justify-content:flex-end;gap:10px;margin-top:22px}.library-management-menu{display:grid;gap:10px;margin-top:16px}.library-work-panel .event-table-scroll{max-height:320px;overflow:auto}.library-source,.library-hash{overflow-wrap:anywhere}.library-source{background:#f6f8fc;border-radius:6px;padding:10px;font-size:13px}.library-hash{font-size:11px} @media(max-width:640px){.toolbar-spacer{display:none}.library-management-toolbar{gap:6px}.library-management-toolbar .button{font-size:12px;padding:7px}.library-operation-dialog{padding:16px}.library-work-panel footer{flex-wrap:wrap}}
</style>
