<script setup lang="ts">
import {computed,nextTick,onMounted,onUnmounted,ref,watch} from 'vue';
import {Folder,File,FileText,FileCode,FileImage,FileVideo,FileAudio,FileArchive,Download,FolderPlus,Upload,RefreshCw,MoreHorizontal,ArrowLeft,ArrowRight,ArrowUp,Pencil,ChevronRight} from 'lucide-vue-next';
import {formatBytes} from './format-bytes';
import {validDisplaySizeListing,type CloudDisplaySizeInfo} from './cloud-display-size';
import CloudToolsNav from './CloudToolsNav.vue';
import CloudActionFeedback from './CloudActionFeedback.vue';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import CloudCacheStatus from './CloudCacheStatus.vue';
import CloudDirectorySize from './CloudDirectorySize.vue';
import CloudFileBatch from './CloudFileBatch.vue';
import CloudFileActions from './CloudFileActions.vue';
import CloudFileInfo from './CloudFileInfo.vue';
import type {CloudFileEntry as Entry,CloudFileAction,CloudActionResult,CloudFileInfoSnapshot} from './cloud-files-types';
import {readCloudFilesRoute,cloudFilesRouteURL,validCloudSearchReturn,validCloudDirectory as validPath,emptyCloudSearchExtra,validCloudSearchExtra,cloudSearchUTCInput,cloudSearchUTCSeconds,type CloudSearchLocation,type CloudSearchExtra,type CloudFilesRoute} from './cloud-files-route';
const fileInfo=ref<CloudFileInfoSnapshot|null>(null);
function showFileInfo(item:Entry){if(loading.value||auth.value||error.value||!ready.value||actionBusy.value||batchBusy.value||(action.value&&!actionMinimized.value)||(batch.value&&!batchMinimized.value))return;closeRowMore();fileInfo.value={item:{...item},source:loadedSource.value==='live'?(displaySizeInfo.value?'snapshot':'live'):'cache',observedAt:observed.value,cacheGeneration:displaySizeInfo.value?.cache_generation||cacheGeneration.value};}
function closeBrowseDetails(event:KeyboardEvent){const panel=event.currentTarget as HTMLDetailsElement;panel.open=false;panel.querySelector('summary')?.focus();}
const cachePanel=ref(false),returnNotice=ref('');
const addressEditing=ref(false),navigationCursor=ref(0),navigationEnd=ref(0),navigationMoving=ref(false);
const navigationSession=Array.from(crypto.getRandomValues(new Uint8Array(8)),n=>n.toString(16).padStart(2,'0')).join('');
function markedHistoryState(index:number){return {...(history.state&&typeof history.state==='object'?history.state:{}),nasCloudPosition:{session:navigationSession,index}};}
function syncNavigation(){
 const marker=history.state?.nasCloudPosition;navigationMoving.value=false;
 if(marker?.session===navigationSession&&Number.isSafeInteger(marker.index)&&marker.index>=0&&marker.index<=navigationEnd.value){navigationCursor.value=marker.index;return;}
 navigationCursor.value=0;navigationEnd.value=0;history.replaceState(markedHistoryState(0),'',location.pathname+location.search);
}
function positionKey(url:string){const parsed=readCloudFilesRoute(new URL(url,location.origin).search);if(parsed.error)return url;const r=parsed.route;return JSON.stringify([r.directory,r.source,r.query,r.scope||'/',r.kind||'all',r.extra||emptyCloudSearchExtra()]);}
function writeCloudURL(url:string,replace:boolean){
 // Sort, paging and refresh update this location instead of adding visits.
 if(replace||positionKey(url)===positionKey(location.pathname+location.search)){history.replaceState(markedHistoryState(navigationCursor.value),'',url);return;}
 const next=navigationCursor.value+1;history.pushState(markedHistoryState(next),'',url);navigationCursor.value=next;navigationEnd.value=next;
}
function navigateHistory(direction:number){if(navigationMoving.value||loading.value||auth.value||(direction<0?navigationCursor.value===0:navigationCursor.value>=navigationEnd.value))return;navigationMoving.value=true;history.go(direction);}
function cancelDirectoryRead(){
 if(!active||!loading.value)return;generation++;controller?.abort();controller=undefined;cleanupPending=false;loading.value=false;navigationMoving.value=false;addressEditing.value=false;draft.value=directory.value;
 error.value=ready.value?'本次目录读取已取消；保留的是上次成功读取的列表，请刷新后再操作。':'目录读取已取消，尚无已确认的列表，请重新打开目录。';
 const request=generation;nextTick(()=>{if(active&&request===generation&&!auth.value)document.querySelector<HTMLElement>('[data-testid=cloud-address-edit]')?.focus();});
}
async function editAddress(){if(!active||loading.value||auth.value)return;addressEditing.value=true;draft.value=directory.value;const request=generation;await nextTick();if(active&&request===generation&&addressEditing.value&&!auth.value)document.querySelector<HTMLInputElement>('[data-testid=cloud-files-path]')?.focus();}
function cancelAddressEdit(){draft.value=directory.value;addressEditing.value=false;const request=generation;nextTick(()=>{if(active&&request===generation&&!addressEditing.value&&!auth.value)document.querySelector<HTMLElement>('[data-testid=cloud-address-edit]')?.focus();});}

let windowWasBlurred=false;
function windowBlur(){windowWasBlurred=true;}
function hasUnsubmittedBrowseDraft(){
 return addressEditing.value||draft.value!==directory.value||queryDraft.value!==activeSearch.value||extensionDraft.value!==activeExtra.value.extensions||modifiedFromDraft.value!==cloudSearchUTCInput(activeExtra.value.modifiedFrom)||modifiedBeforeDraft.value!==cloudSearchUTCInput(activeExtra.value.modifiedBefore)||searchKind.value!==activeKind.value||(searchScope.value==='current'?searchScopePath.value:'/')!==activeScope.value;
}
function windowFocus(){
 if(!windowWasBlurred||!active||auth.value)return;windowWasBlurred=false;
 // Other tabs can change remote content. Preserve an in-progress selection or dialog.
 if(loading.value||Object.keys(selected.value).length||action.value||batch.value||fileInfo.value||hasUnsubmittedBrowseDraft()){returnNotice.value='离开页面期间网盘内容可能变化；已保留当前操作，请在合适时刷新列表。';return;}
 restoreRoute(true);
}
// Poll local durable receipts, never the provider, to detect background cleanup.
let cleanupRevision='',cleanupPending=false,cleanupTimer:number|undefined,cleanupController:AbortController|undefined,cleanupObservation=0;
const cleanupSyncError=ref('');
function stopCleanupObservation(){cleanupObservation++;if(cleanupTimer!==undefined)window.clearTimeout(cleanupTimer);cleanupTimer=undefined;cleanupController?.abort();cleanupController=undefined;}
function scheduleCleanupObservation(){if(!active||auth.value||document.hidden||cleanupTimer!==undefined||cleanupController)return;cleanupTimer=window.setTimeout(()=>{cleanupTimer=undefined;void observeCleanup();},2000);}
function applyCleanupChange(){
 if(!cleanupPending||!active||auth.value||!ready.value)return;
 if(loading.value||!!error.value||Object.keys(selected.value).length||action.value||batch.value||fileInfo.value||hasUnsubmittedBrowseDraft()){returnNotice.value='后台清理结果已变化；已保留当前操作，请刷新列表核对网盘内容。';return;}
 cleanupPending=false;restoreRoute();
}
async function observeCleanup(){
 if(!active||auth.value||document.hidden)return;
 const epoch=cleanupObservation;cleanupController=new AbortController();
 try{const response=await fetch('/api/cloud/cleanup-revision',{credentials:'same-origin',signal:cleanupController.signal});if(epoch!==cleanupObservation||!active)return;if(response.status===401){requireLogin();return;}if(!response.ok)throw Error('read failed');const value=await response.json();if(epoch!==cleanupObservation||!active)return;
 if(typeof value?.revision!=='string'||!/^[a-f0-9]{64}$/.test(value.revision)||typeof value.observed_at_utc!=='string'||!Number.isFinite(Date.parse(value.observed_at_utc)))throw Error('invalid receipt');
 cleanupSyncError.value='';if(value.revision!==cleanupRevision){cleanupRevision=value.revision;cleanupPending=true;applyCleanupChange();}
 }catch{if(epoch===cleanupObservation&&active)cleanupSyncError.value='暂时无法观察后台清理变化，列表可能尚未更新；可手动刷新列表。';}
 finally{if(epoch===cleanupObservation){cleanupController=undefined;scheduleCleanupObservation();}}
}
function cleanupVisibility(){if(document.hidden)stopCleanupObservation();else scheduleCleanupObservation();}
// Six permanent row actions crowded the table. Keep mutations in one inline
// disclosure, preserving the existing confirmation and identity checks.
const rowMore=ref<string|null>(null);
let rowMoreTrigger:HTMLButtonElement|null=null;
function closeRowMore(restoreFocus=false){const trigger=rowMoreTrigger;rowMore.value=null;rowMoreTrigger=null;if(restoreFocus&&trigger?.isConnected)trigger.focus();}
function toggleRowMore(item:Entry,event:Event){if(rowMore.value===item.fs_id){closeRowMore(true);return;}rowMore.value=item.fs_id;rowMoreTrigger=event.currentTarget as HTMLButtonElement;}
function rowMoreAction(operation:'rename'|'move'|'copy'|'delete',item:Entry){closeRowMore(true);chooseAction(operation,item);}
function dismissRowMoreOutside(event:Event){if(rowMore.value&&event.target instanceof Node&&!rowMoreTrigger?.closest('.cloud-row-more')?.contains(event.target))closeRowMore();}
function dismissRowMoreKey(event:KeyboardEvent){if(rowMore.value&&event.key==='Escape'){event.preventDefault();event.stopPropagation();closeRowMore(true);}}

const densityKey='nas-downloader.cloud-list-density.v1';
function readDensity():'compact'|'comfortable'{try{return localStorage.getItem(densityKey)==='comfortable'?'comfortable':'compact';}catch{return 'compact';}}
const density=ref(readDensity()),densityNotice=ref('');
function changeDensity(){try{localStorage.setItem(densityKey,density.value);densityNotice.value='';}catch{densityNotice.value='当前页面已应用；浏览器未保存显示偏好，下次打开可能恢复原显示方式。';}}
const mixedKey='nas-downloader.cloud-mixed-order.v1';
function readMixed(){try{return localStorage.getItem(mixedKey)!=='0';}catch{return true;}}
const mixed=ref(readMixed()),loadedMixed=ref(true),mixedNotice=ref('');
function changeMixed(){if(loading.value||auth.value||actionBusy.value||batchBusy.value)return;try{localStorage.setItem(mixedKey,mixed.value?'1':'0');mixedNotice.value='';}catch{mixedNotice.value='已选择当前混排方式；浏览器未保存偏好。';}open(directory.value);}
const sort=ref('name-asc');
const loadedSort=ref('');
type SortField='name'|'size'|'time';
function columnSort(field:SortField){if(loadedSource.value!=='live'||activeSearch.value||!loadedSort.value.startsWith(field+'-'))return undefined;return loadedSort.value.endsWith('-desc')?'descending':'ascending';}
function sortColumn(field:SortField){
 if(loading.value||auth.value||actionBusy.value||batchBusy.value||!ready.value||loadedSource.value!=='live'||activeSearch.value)return;
 const [current,direction]=loadedSort.value.split('-');
 sort.value=field+'-'+(current===field?(direction==='desc'?'asc':'desc'):field==='time'?'desc':'asc');changeSort();
}
function changeSort(){if(loading.value||auth.value||actionBusy.value||batchBusy.value)return;open(directory.value);}
const selected=ref<Record<string,Entry>>({}),batchBusy=ref(false),batchMinimized=ref(false),batch=ref<{key:number;operation:'move'|'copy'|'delete';items:Entry[];directory:string;observedAt:string}|null>(null);
let selectionAnchor=-1,batchKey=0,requestedPath='/',requestedQuery='';
let requestedScope='/',requestedKind:SearchKind='all';
let requestedExtra=emptyCloudSearchExtra();
const selectedEntries=computed(()=>entries.value.filter(entry=>selected.value[entry.fs_id]).map(entry=>selected.value[entry.fs_id]));
const copyPathBusy=ref(false),copyPathNotice=ref(''),copyPathFallback=ref('');
function resetCopyPaths(){copyPathNotice.value='';copyPathFallback.value='';}
function copyPathIdentity(){return JSON.stringify(selectedEntries.value.map(item=>[item.fs_id,item.path]));}
async function copySelectedPaths(){
 if(selectionDisabled.value||copyPathBusy.value||!selectedEntries.value.length)return;
 const paths=selectedEntries.value.map(item=>item.path),identity=copyPathIdentity(),request=generation;
 copyPathBusy.value=true;resetCopyPaths();
 const current=()=>active&&request===generation&&copyPathIdentity()===identity;
 try{if(!navigator.clipboard?.writeText)throw Error('clipboard unavailable');await navigator.clipboard.writeText(paths.join('\n'));if(current())copyPathNotice.value='已复制 '+paths.length+' 项网盘路径。';}
 catch{if(current()){copyPathNotice.value='浏览器未允许自动复制，请在下方选择文本后手动复制。';copyPathFallback.value=paths.join('\n');}}
 finally{copyPathBusy.value=false;}
}
const selectionDisabled=computed(()=>loading.value||auth.value||!!error.value||!ready.value||batchBusy.value||actionBusy.value);
// Real-account UI review caught enabled links beside a deferred-refresh warning.
// Keep the selection, but wait for an explicit successful refresh before leaving.
const cleanupDirectoryHref=computed(()=>selectionDisabled.value||!!returnNotice.value||action.value||batch.value||fileInfo.value?'':'/cloud/cleanup?'+new URLSearchParams({directory:directory.value,return_files:location.pathname+location.search}));
const selectedComparisonRoots=computed<[string,string]|null>(()=>{
 const chosen=selectedEntries.value;if(selectionDisabled.value||!!returnNotice.value||action.value||batch.value||fileInfo.value||chosen.length!==2||chosen.some(entry=>!entry.directory||!validPath(entry.path)))return null;
 const [left,right]=chosen.map(entry=>entry.path).sort();
 return left===right||left==='/'||right.startsWith(left+'/')?null:[left,right];
});
const comparisonUnavailableReason=computed(()=>{
 if(returnNotice.value)return '列表可能已变化，请先刷新确认后再比较目录。';
 if(selectionDisabled.value||action.value||batch.value||fileInfo.value)return '请先完成当前操作并刷新确认列表。';
 if(selectedEntries.value.length!==2)return '请选择恰好两个目录。';
 if(selectedEntries.value.some(entry=>!entry.directory))return '比较仅支持目录，请取消文件选择。';
 return selectedComparisonRoots.value?'':'请选择有效、不同且互不包含的两个目录。';
});
const comparisonHref=computed(()=>selectedComparisonRoots.value?'/directory-analysis?'+new URLSearchParams({left:selectedComparisonRoots.value[0],right:selectedComparisonRoots.value[1],return_files:location.pathname+location.search}):'');
function guardWorkspace(event:Event){if(!(event.currentTarget as HTMLAnchorElement).getAttribute('href'))event.preventDefault();}

// Links need the same state boundary as buttons; removing href also prevents
// context-menu/new-tab navigation from a stale observation.
const downloadDisabledReason=computed(()=>{
 if(auth.value)return '管理员登录已失效，请重新登录。';
 if(loading.value||!ready.value)return '正在读取网盘列表，请等待读取完成。';
 if(error.value)return '列表读取失败，请先刷新并核对当前网盘内容。';
 if(actionBusy.value||batchBusy.value)return '当前网盘操作尚未结束，请等待结果。';
 if(actionResult.value?.status==='unknown')return '上次网盘操作结果待核对，请先查看操作记录。';
 return '';
});
function guardDownload(event:Event){if(downloadDisabledReason.value)event.preventDefault();}
const downloadURL=computed(()=>{const q=new URLSearchParams();for(const item of selectedEntries.value)q.append('path',item.path);return '/intake?'+q;});
function clearSelection(){selected.value={};selectionAnchor=-1;resetCopyPaths();}
function toggleSelection(entry:Entry,index:number,event:MouseEvent){if(selectionDisabled.value)return;const checked=(event.target as HTMLInputElement).checked,next={...selected.value},start=event.shiftKey&&selectionAnchor>=0?Math.min(selectionAnchor,index):index,end=event.shiftKey&&selectionAnchor>=0?Math.max(selectionAnchor,index):index;for(let i=start;i<=end;i++){const item=entries.value[i];if(checked)next[item.fs_id]={...item};else delete next[item.fs_id]}selected.value=next;selectionAnchor=index;resetCopyPaths();}
function selectPage(event:Event){if(selectionDisabled.value)return;const next:Record<string,Entry>={};if((event.target as HTMLInputElement).checked)for(const entry of entries.value)next[entry.fs_id]={...entry};selected.value=next;selectionAnchor=-1;resetCopyPaths();}
function chooseBatch(operation:'move'|'copy'|'delete'){if(selectionDisabled.value||!selectedEntries.value.length||loadedSource.value!=='live'||actionResult.value?.status==='unknown')return;closeRowMore();batchMinimized.value=false;batch.value={key:++batchKey,operation,items:selectedEntries.value.map(i=>({...i})),directory:directory.value,observedAt:observed.value};}
function closeBatch(){const operation=batch.value?.operation;batch.value=null;queueMicrotask(()=>{const target=document.querySelector<HTMLElement>('[data-testid=cloud-selected-'+operation+']')||document.querySelector<HTMLElement>('[data-testid=cloud-select-page]');target?.focus()});}
async function refreshBatch(){generation++;controller?.abort();loading.value=false;const done=await open(requestedPath,requestedQuery,0,false,true,{scope:requestedScope,kind:requestedKind,extra:requestedExtra});if(done!==true)throw Error('目录刷新未确认');}

const actionBusyConfirmed=ref(false),batchBusyConfirmed=ref(false);
// Modal cleanup runs while list refresh has disabled the original trigger.
// Restore its focus after the fresh rows become interactive, or use the table selector.
// Row mutations are now collapsed; return to their visible More trigger after refresh.
let focusAfterAction='';
function restoreActionFocus(){if(!focusAfterAction||actionBusy.value||loading.value)return;const target=document.querySelector<HTMLElement>(focusAfterAction)||document.querySelector<HTMLElement>('[data-testid=cloud-select-page]:not(:disabled)')||document.querySelector<HTMLElement>('.cloud-header-actions button:not(:disabled)');target?.focus();if(document.activeElement===target)focusAfterAction='';}
function closeAction(){const a=action.value;focusAfterAction=a?.item?'[data-cloud-id="'+a.item.fs_id+'"] [data-testid=cloud-row-more]:not(:disabled)':'[data-testid=cloud-mkdir]:not(:disabled)';action.value=null;queueMicrotask(restoreActionFocus);}
const actionMinimized=ref(false),actionBusyLabel=ref(''),batchBusyLabel=ref('');
function batchBusyChanged(value:boolean,label='',confirmed=false){batchBusy.value=value;batchBusyLabel.value=label;batchBusyConfirmed.value=value&&confirmed;}
const action=ref<CloudFileAction|null>(null),actionBusy=ref(false),actionResult=ref<CloudActionResult|null>(null);
let refreshAfterAction=false;
function chooseAction(operation:CloudFileAction['operation'],item?:Entry){if(loading.value||actionBusy.value||batchBusy.value||auth.value||error.value||actionResult.value?.status==='unknown'||loadedSource.value!=='live'||!ready.value)return;closeRowMore();actionMinimized.value=false;action.value={operation,directory:directory.value,item:item?{...item}:undefined,observedAt:observed.value};}
function actionFinished(value:CloudActionResult){actionResult.value=value;refreshAfterAction=value.status==='confirmed'||value.refresh_required===true;}
function actionBusyChanged(value:boolean,label='',confirmed=false){actionBusy.value=value;actionBusyLabel.value=label;actionBusyConfirmed.value=value&&confirmed;if(!value&&refreshAfterAction&&!loading.value){refreshAfterAction=false;refresh();}if(!value)queueMicrotask(restoreActionFocus);}
function searchCacheReviewURL(){const params=new URLSearchParams({directory:activeScope.value}),saved=validCloudSearchReturn(location.pathname+location.search);if(saved)params.set('return_search',saved);return '/cloud/cache?'+params;}
function inspectLive(item:Entry){const returnSearch=validCloudSearchReturn(location.pathname+location.search);source.value='live';open(item.path.slice(0,item.path.lastIndexOf('/'))||'/', '',0,false,true,{location:returnSearch?{id:item.fs_id,path:item.path,directory:item.directory,returnSearch}:null});}
const searchLocation=ref<CloudSearchLocation|null>(null);
function isLocated(entry:Entry){const target=searchLocation.value;return !!target&&entry.fs_id===target.id&&entry.path===target.path&&entry.directory===target.directory;}
const locationMessage=computed(()=>{
 if(loading.value||error.value)return '实时定位尚待核对；读取失败时下方保留的是历史列表。';
 if(entries.value.some(isLocated))return '已在实时目录本页找到同一身份，已高亮；未自动选择或执行操作。';
 if(entries.value.some(entry=>entry.path===searchLocation.value?.path))return '该路径的身份或类型已变化，请按当前网盘内容核对，不能沿用缓存身份。';
 return '本页未见同一身份；不代表文件已删除，可继续翻页核对或返回搜索。';
});
function returnToSearch(){if(!searchLocation.value||loading.value||auth.value)return;const parsed=readCloudFilesRoute(new URL(searchLocation.value.returnSearch,location.origin).search);if(parsed.error){inputError.value=parsed.error;return;}loadRoute(parsed.route,false);}
const directory=ref('/'),draft=ref('/'),entries=ref<Entry[]>([]),loading=ref(false),error=ref(''),inputError=ref(''),sidebarError=ref(''),ready=ref(false),auth=ref(false),observed=ref('');
const source=ref('live'),loadedSource=ref('live'),cacheStatus=ref(''),cacheGeneration=ref('');
const displaySizeInfo=ref<CloudDisplaySizeInfo|null>(null),readingDisplaySize=ref('');
const queryDraft=ref(''),activeSearch=ref('');
type SearchKind='all'|'file'|'directory';
const searchScope=ref('all'),searchKind=ref<SearchKind>('all'),activeScope=ref('/'),activeKind=ref<SearchKind>('all');
const searchScopePath=ref('/');
const extensionDraft=ref(''),modifiedFromDraft=ref(''),modifiedBeforeDraft=ref(''),extraFiltersOpen=ref(false),activeExtra=ref(emptyCloudSearchExtra());
const extraNotice=computed(()=>[activeExtra.value.extensions?'扩展名：'+activeExtra.value.extensions:'',activeExtra.value.modifiedFrom||activeExtra.value.modifiedBefore?'修改时间 UTC：['+(activeExtra.value.modifiedFrom?cloudSearchUTCInput(activeExtra.value.modifiedFrom)+'Z':'不限')+'，'+(activeExtra.value.modifiedBefore?cloudSearchUTCInput(activeExtra.value.modifiedBefore)+'Z':'不限')+')；未知时间不参与匹配':''].filter(Boolean).join(' · '));
const kindNames={all:'文件与文件夹',file:'仅文件',directory:'仅文件夹'};
const pageOffset=ref(0),nextOffset=ref<number|null>(null),pageNotice=ref('');
const boundaries=new Map<number,string>();
function page(offset:number){open(directory.value,activeSearch.value,offset,true);}
function search(){const query=queryDraft.value.trim();if(!query||Array.from(query).length>128){inputError.value='搜索内容须为1至128个字符。';return;}
 const modifiedFrom=cloudSearchUTCSeconds(modifiedFromDraft.value),modifiedBefore=cloudSearchUTCSeconds(modifiedBeforeDraft.value),extensions=extensionDraft.value.trim()?Array.from(new Set(extensionDraft.value.split(',').map(s=>s.trim().replace(/^\./,'').toLowerCase()))).join(','):'';
 if(modifiedFrom===null||modifiedBefore===null||!validCloudSearchExtra({extensions,modifiedFrom,modifiedBefore},searchKind.value)){inputError.value='扩展名须为最多10个字母/数字后缀，不能与仅文件夹同时使用；UTC修改时间须有效且起点早于终点。';return;}
 open(directory.value,query,0,false,true,{extra:{extensions,modifiedFrom,modifiedBefore}});
}
function refresh(){open(directory.value,loadedSource.value==='cache'?activeSearch.value:'',0,false,true,{scope:activeScope.value,kind:activeKind.value,extra:activeExtra.value});}
const crumbs=computed(()=>[{name:'全部文件',path:'/'},...directory.value.split('/').filter(Boolean).map((name,index,parts)=>({name,path:'/'+parts.slice(0,index+1).join('/')}))]);
let active=true,generation=0,controller:AbortController|undefined;
function stop(){stopCleanupObservation();closeRowMore();if(actionBusy.value&&!actionBusyConfirmed.value||batchBusy.value&&!batchBusyConfirmed.value)actionResult.value={status:'unknown',message:'离页时操作尚未确认，请查看操作记录，不要重复提交。',source:action.value?.item?.path||directory.value,target:''};actionBusy.value=false;action.value=null;fileInfo.value=null;active=false;generation++;controller?.abort();loading.value=false;}
function requireLogin(){auth.value=true;stop();error.value='管理员登录已失效，请重新登录。';location.assign('/login?next=%2Fcloud%2Ffiles');}
type RestoreOptions={source?:'live'|'cache';generation?:string;boundary?:string;sizeSnapshot?:string;mixed?:boolean;notice?:string;replace?:boolean;scope?:string;kind?:SearchKind;location?:CloudSearchLocation|null;extra?:CloudSearchExtra};
async function open(path=directory.value,query='',offset=0,continuation=false,recover=true,restore:RestoreOptions={}){
 if(!active||auth.value||loading.value)return;if(!validPath(path)){inputError.value='请输入规范的绝对网盘目录。';return;}if(!actionBusy.value)action.value=null;fileInfo.value=null;clearSelection();requestedPath=path;requestedQuery=query;
 pageNotice.value=restore.notice||'';returnNotice.value='';inputError.value='';const cleanupAtRequest=cleanupRevision;const [order,direction]=sort.value.split('-'),descending=direction==='desc';const request=++generation,requestedSource=restore.source||(continuation?loadedSource.value:query?'cache':'live');controller=new AbortController();loading.value=true;
 const scope=query?(restore.scope??(continuation?activeScope.value:searchScope.value==='current'?searchScopePath.value:'/')):'/',kind=query?(restore.kind??(continuation?activeKind.value:searchKind.value)):'all';
 const extra=query?(restore.extra??activeExtra.value):emptyCloudSearchExtra();
 const requestedLocation=requestedSource==='live'&&!query?(restore.location!==undefined?restore.location:path===directory.value&&loadedSource.value==='live'?searchLocation.value:null):null;
 const displaySort=requestedSource==='live'&&!query;
 const requestedMixed=restore.mixed??mixed.value;
 const orderLabel=order==='size'?'大小排序':'目录排序';
 const requestedSnapshot=displaySort?(restore.sizeSnapshot||(continuation?displaySizeInfo.value?.snapshot_id:'')||''):'';
 readingDisplaySize.value=displaySort?(requestedSnapshot?'正在读取'+orderLabel+'快照分页，可取消读取…':'正在读取当前目录完整清单并按'+(order==='size'?'大小':order==='time'?'修改时间':'名称')+'排序，可取消读取…'):'';
 // A minimized batch refresh must replay this request, not unsubmitted filters.
 requestedScope=scope;requestedKind=kind;requestedExtra=extra;
 const params=new URLSearchParams({offset:String(offset),limit:query?'50':'100',identity_format:'string'});if(query)params.set('q',query);else params.set('dir',path);if(requestedSource==='live'){params.set('order',order);params.set('desc',descending?'1':'0');params.set('mixed',requestedMixed?'1':'0');}
 if(requestedSnapshot)params.set('snapshot',requestedSnapshot);
 if(query&&(scope!=='/'||kind!=='all')){params.set('scope',scope);params.set('kind',kind);}
 if(query){if(extra.extensions)params.set('extensions',extra.extensions);if(extra.modifiedFrom)params.set('modified_from',extra.modifiedFrom);if(extra.modifiedBefore)params.set('modified_before',extra.modifiedBefore);}
 if(offset&&(continuation||restore.source)){if(requestedSource==='cache')params.set('cache_generation',restore.generation||cacheGeneration.value);else if(restore.boundary||boundaries.has(offset))params.set('expected_boundary',JSON.stringify({fingerprint:restore.boundary||boundaries.get(offset)}));else{loading.value=false;error.value='缺少本页边界，请刷新目录。';return;}}
 try{const response=await fetch((query?'/api/cloud-inventory/search?':requestedSource==='cache'?'/api/cloud-inventory?':displaySort?'/api/cloud-display-size?':'/api/cloud?')+params,{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){auth.value=true;stop();error.value='管理员登录已失效，请重新登录。';location.assign('/login?next=%2Fcloud%2Ffiles');return;}
  if(response.status===409&&displaySort){const conflict=await response.json();if(request!==generation)return;if(conflict.code==='display_size_snapshot_changed'&&recover){loading.value=false;return await open(path,query,0,false,false,{source:'live',replace:true,location:requestedLocation,mixed:requestedMixed,notice:orderLabel+'快照已失效，需要重新读取第一页；以列表读取结果为准，不会重发网盘写入。'});}throw Error(conflict.message||'大小排序快照无法恢复，请刷新目录。');}
  if(response.status===409&&requestedSource==='cache'&&recover){const conflict=await response.json();if(request!==generation)return;if(conflict.code==='cache_generation_changed'){loading.value=false;await open(path,query,0,false,false,{source:'cache',replace:true,scope,kind,extra,notice:'本地缓存已更新，需要重新读取第一页；以列表读取结果为准。'});return;}}
  if(!response.ok)throw Error(await response.text()||'网盘目录读取失败');const value=await response.json();if(request!==generation)return;
  if(requestedSource==='live'&&(!value.order||value.order.field!==order||value.order.descending!==descending))throw Error('目录排序响应与请求不一致');
  if(displaySort&&!validDisplaySizeListing(value,offset,requestedSnapshot,requestedMixed))throw Error(orderLabel+'快照或分页证据无效');
  if(query){if((extra.extensions||extra.modifiedFrom||extra.modifiedBefore||['extensions','modified_from','modified_before'].some(key=>value?.[key]!==undefined))&&(value?.extensions!==extra.extensions||value?.modified_from!==extra.modifiedFrom||value?.modified_before!==extra.modifiedBefore))throw Error('扩展名或修改时间响应与请求不一致');if(value?.query!==query||!value.coverage||typeof value.coverage.status!=='string')throw Error('缓存搜索响应无效');if((scope!=='/'||kind!=='all'||value.scope!==undefined||value.kind!==undefined)&&(value.scope!==scope||value.kind!==kind))throw Error('搜索范围或类型响应与请求不一致');if(!Array.isArray(value.entries)||!value.entries.every((entry:any)=>typeof entry.path==='string'&&(scope==='/'||entry.path.startsWith(scope+'/'))&&(kind==='all'||entry.directory===(kind==='directory'))))throw Error('搜索结果超出范围或类型');value.directory=path;value.metadata={status:value.coverage.status,observed_at_utc:value.coverage.scan_updated_at_utc};}
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
  if(query&&!value.entries.every((entry:Entry)=>(!extra.extensions||!entry.directory&&extra.extensions.split(',').some(ext=>entry.name.toLowerCase().endsWith('.'+ext)))&&(!(extra.modifiedFrom||extra.modifiedBefore)||BigInt(entry.server_mtime)>0n&&(!extra.modifiedFrom||BigInt(entry.server_mtime)>=BigInt(extra.modifiedFrom))&&(!extra.modifiedBefore||BigInt(entry.server_mtime)<BigInt(extra.modifiedBefore)))))throw Error('搜索结果超出扩展名或修改时间条件');
  if(new Set(value.entries.map((entry:Entry)=>entry.id)).size!==value.entries.length||new Set(value.entries.map((entry:Entry)=>entry.fs_id)).size!==value.entries.length)throw Error('目录返回重复身份，请刷新核对。');
  const currentOffset=value.offset,next=value.next_offset??null;
  if(currentOffset!==offset&&!(requestedSource==='live'&&value.boundary_changed===true&&currentOffset===0))throw Error('目录分页位置无效');
  if(next!==null&&(!Number.isSafeInteger(next)||next!==currentOffset+value.entries.length||next<=currentOffset||next>1000000))throw Error('目录分页游标无效');
  if(requestedSource==='live'&&next!==null&&(!value.next_boundary||typeof value.next_boundary.fingerprint!=='string'||!/^[a-f0-9]{64}$/.test(value.next_boundary.fingerprint)))throw Error('下一页边界无效');
  if(!continuation||currentOffset===0)boundaries.clear();
  if(requestedSource==='live'&&next!==null)boundaries.set(next,value.next_boundary.fingerprint);
  pageOffset.value=currentOffset;nextOffset.value=next;if(value.boundary_changed)pageNotice.value='远端目录分页边界已变化，已重新读取第一页。';
  if(restore.boundary&&currentOffset===offset&&currentOffset)boundaries.set(currentOffset,restore.boundary);
  const url=cloudFilesRouteURL({directory:path,source:requestedSource as 'live'|'cache',order:order as 'name'|'size'|'time',descending,query,scope,kind,extra,location:requestedLocation||undefined,offset:currentOffset,generation:value.cache_generation||'',boundary:currentOffset?(restore.boundary||boundaries.get(currentOffset)||''):'',sizeSnapshot:displaySort?value.display_size.snapshot_id:undefined,mixed:displaySort?requestedMixed:undefined});
  writeCloudURL(url,!!restore.replace||!!value.boundary_changed||location.pathname+location.search===url);
  displaySizeInfo.value=displaySort?{...value.display_size}:null;
 if(displaySort)loadedMixed.value=requestedMixed;
  searchLocation.value=requestedLocation;
  activeExtra.value=extra;activeScope.value=scope;activeKind.value=kind;if(!query)searchScopePath.value=path;entries.value=value.entries;directory.value=path;draft.value=path;loadedSource.value=requestedSource;loadedSort.value=requestedSource==='live'?order+'-'+(descending?'desc':'asc'):'';activeSearch.value=query;cacheStatus.value=requestedSource==='cache'?value.metadata.status:'';cacheGeneration.value=requestedSource==='cache'?value.cache_generation:'';observed.value=value.observed_at_utc&&!value.observed_at_utc.startsWith('0001-')?value.observed_at_utc:'';ready.value=true;error.value='';const restoreAddressFocus=addressEditing.value;addressEditing.value=false;if(restoreAddressFocus)queueMicrotask(()=>{if(active&&request===generation&&!auth.value)document.querySelector<HTMLElement>('[data-testid=cloud-address-edit]')?.focus();});if(cleanupRevision===cleanupAtRequest)cleanupPending=false;
  if(requestedLocation)queueMicrotask(()=>{if(active&&request===generation&&!error.value&&entries.value.some(isLocated))document.querySelector<HTMLElement>('.cloud-row-located')?.focus();});
  return true;
 }catch(e:any){if(request===generation)error.value=e.message||'网盘目录读取失败';}
 finally{if(request===generation){loading.value=false;queueMicrotask(restoreActionFocus);if(refreshAfterAction&&!actionBusy.value&&active){refreshAfterAction=false;refresh();}}}
}
function parent(){const parts=directory.value.split('/').filter(Boolean);parts.pop();open('/'+parts.join('/'));}
function size(entry:Entry){if(entry.directory&&!entry.size_complete)return '—';return formatBytes(entry.size);}
// Search rows can include both an ancestor and its descendants; count complete directory coverage once.
const pageSizes=computed(()=>{let files=0n,total=0n,missing=0;const complete=entries.value.filter(entry=>entry.directory&&entry.size_complete);for(const entry of entries.value){if(!entry.directory)files+=BigInt(entry.size);if(complete.some(parent=>parent.path!==entry.path&&entry.path.startsWith(parent.path+'/')))continue;if(!entry.directory||entry.size_complete)total+=BigInt(entry.size);else missing++;}return {files:formatBytes(files),total:formatBytes(total),missing};});
function fileIcon(entry:Entry){if(entry.directory)return Folder;const ext=entry.name.split('.').pop()?.toLowerCase()||'';if(['png','jpg','jpeg','gif','webp','svg','bmp','heic'].includes(ext))return FileImage;if(['mp4','mkv','avi','mov','webm'].includes(ext))return FileVideo;if(['mp3','flac','wav','m4a'].includes(ext))return FileAudio;if(['zip','rar','7z','tar','gz'].includes(ext))return FileArchive;if(['js','ts','py','go','json','html','css','cs','sh'].includes(ext))return FileCode;if(['txt','md','pdf','doc','docx','xls','xlsx','ppt','pptx'].includes(ext))return FileText;return File;}

function mtime(value:string){if(BigInt(value)>8640000000000n)return '—';const date=new Date(Number(value)*1000);return value!=='0'&&Number.isFinite(date.getTime())?date.toISOString():'—';}
function restoreRoute(freshSize:unknown=false){
 syncNavigation();generation++;controller?.abort();loading.value=false;const parsed=readCloudFilesRoute(location.search);if(parsed.error){inputError.value=parsed.error;return;}
 if(freshSize===true&&parsed.route.source==='live'){parsed.route.offset=0;parsed.route.boundary='';parsed.route.sizeSnapshot=undefined;}
 loadRoute(parsed.route,true);
}
function loadRoute(route:CloudFilesRoute,replace:boolean){
 pageNotice.value='';mixed.value=route.mixed??readMixed();
 sort.value=(route.order||'name')+'-'+(route.descending?'desc':'asc');source.value=route.source;draft.value=route.directory;queryDraft.value=route.query;searchScope.value=route.scope&&route.scope!=='/'?'current':'all';searchScopePath.value=route.scope&&route.scope!=='/'?route.scope:route.directory;searchKind.value=route.kind||'all';const extra=route.extra||emptyCloudSearchExtra();extensionDraft.value=extra.extensions;modifiedFromDraft.value=cloudSearchUTCInput(extra.modifiedFrom);modifiedBeforeDraft.value=cloudSearchUTCInput(extra.modifiedBefore);extraFiltersOpen.value=!!(extra.extensions||extra.modifiedFrom||extra.modifiedBefore);
 if(route.offset&&!(route.source==='cache'?route.generation:route.boundary&&route.sizeSnapshot)){route.offset=0;route.sizeSnapshot=undefined;pageNotice.value='分页核对依据缺失，需要重新读取第一页；以列表读取结果为准。';}
 open(route.directory,route.query,route.offset,false,true,{source:route.source,generation:route.generation,boundary:route.boundary,sizeSnapshot:route.sizeSnapshot,mixed:route.mixed,notice:pageNotice.value,replace,scope:route.scope,kind:route.kind,extra:route.extra,location:route.location||null});
}
function resume(event:PageTransitionEvent){if(event.persisted&&!auth.value){active=true;restoreRoute(true);scheduleCleanupObservation();}}
watch([loading,auth,actionBusy,batchBusy,entries],()=>closeRowMore());
watch([loading,ready],()=>{if(!loading.value)applyCleanupChange();});
onMounted(()=>{document.title='网盘文件 · CloudHarbor';document.addEventListener('visibilitychange',cleanupVisibility);scheduleCleanupObservation();window.addEventListener('blur',windowBlur);window.addEventListener('focus',windowFocus);document.addEventListener('pointerdown',dismissRowMoreOutside);document.addEventListener('keydown',dismissRowMoreKey);window.addEventListener('pagehide',stop);window.addEventListener('pageshow',resume);window.addEventListener('popstate',restoreRoute);restoreRoute();});
onUnmounted(()=>{stop();document.removeEventListener('visibilitychange',cleanupVisibility);window.removeEventListener('blur',windowBlur);window.removeEventListener('focus',windowFocus);document.removeEventListener('pointerdown',dismissRowMoreOutside);document.removeEventListener('keydown',dismissRowMoreKey);window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',resume);window.removeEventListener('popstate',restoreRoute);});
</script>
<template>
 <div class="workspace event-workspace cloud-files-workspace"><WorkspaceSidebar active="/cloud/files" @error="sidebarError=$event"/><main class="main">
  <header class="page-header"><div><p class="eyebrow">百度网盘</p><h1>网盘文件</h1><p class="muted">浏览网盘目录，文件大小独立展示。</p></div><div class="cloud-header-actions" role="group" aria-label="当前目录操作"><button class="button secondary" data-testid="cloud-mkdir" :disabled="loading||auth||!!error||!ready||loadedSource!=='live'||actionBusy||batchBusy||actionResult?.status==='unknown'" @click="chooseAction('mkdir')"><FolderPlus :size="16"/>新建文件夹</button><a class="button primary" :href="'/uploads?directory='+encodeURIComponent(directory)"><Upload :size="16"/>上传文件 / 目录</a></div></header>
<p v-if="sidebarError" class="notice danger" role="alert">{{sidebarError}}</p>
  <p v-if="downloadDisabledReason&&ready" class="notice" role="status" data-testid="cloud-download-unavailable">下载预检暂不可用：{{downloadDisabledReason}}</p>
  <p v-if="cleanupSyncError" class="notice" role="status" data-testid="cloud-cleanup-sync-error">{{cleanupSyncError}}</p>
  <p v-if="returnNotice" class="notice" role="status" data-testid="cloud-return-notice">{{returnNotice}}</p>
  <section class="cloud-addressbar" aria-label="网盘目录导航">
   <div class="cloud-history-actions"><button class="button secondary" type="button" data-testid="cloud-files-back" aria-label="后退" :title="navigationCursor===0?'没有本页可返回的位置':'返回上一位置'" :disabled="loading||auth||navigationMoving||navigationCursor===0" @click="navigateHistory(-1)"><ArrowLeft :size="17"/></button><button class="button secondary" type="button" data-testid="cloud-files-forward" aria-label="前进" :title="navigationCursor>=navigationEnd?'没有可前进的位置':'前往下一位置'" :disabled="loading||auth||navigationMoving||navigationCursor>=navigationEnd" @click="navigateHistory(1)"><ArrowRight :size="17"/></button><button class="button secondary" type="button" data-testid="cloud-files-up" aria-label="向上一级" :title="directory==='/'?'已经位于根目录':'打开父目录'" :disabled="loading||auth||directory==='/'" @click="parent"><ArrowUp :size="17"/></button></div>
   <form class="cloud-pathbar" @submit.prevent="open(draft)" @click.self="editAddress">
    <nav v-show="!addressEditing" aria-label="网盘路径" @click.self="editAddress"><template v-for="(crumb,index) in crumbs" :key="crumb.path"><ChevronRight v-if="index" :size="14" aria-hidden="true"/><button type="button" class="event-detail-link" :aria-current="index===crumbs.length-1?'location':undefined" :disabled="loading||auth" @click="open(crumb.path)">{{crumb.name}}</button></template></nav>
    <label v-show="addressEditing" class="cloud-address-input"><input v-model="draft" data-testid="cloud-files-path" aria-label="网盘绝对路径" autocomplete="off" :disabled="loading||auth" @keydown.esc.prevent.stop="cancelAddressEdit"/></label>
    <button v-show="addressEditing" class="button secondary" data-testid="cloud-files-open" :disabled="loading||auth">打开</button><button v-show="!addressEditing" class="button secondary" type="button" data-testid="cloud-address-edit" aria-label="编辑网盘路径" title="输入网盘绝对路径" :disabled="loading||auth" @click="editAddress"><Pencil :size="15"/></button>
   </form><button v-if="loading&&!auth" class="button secondary" type="button" data-testid="cloud-files-cancel-read" @click="cancelDirectoryRead">取消读取</button><button class="button secondary" data-testid="cloud-files-refresh" :disabled="loading||auth" @click="refresh"><RefreshCw :size="16"/>刷新列表</button></section>
  <div class="cloud-browse-options"><details class="cloud-maintenance" @keydown.esc.prevent.stop="closeBrowseDetails"><summary>目录工具</summary>  <CloudToolsNav active="/cloud/files" :directory="directory"><a class="button secondary" data-testid="cloud-directory-cleanup" :href="cleanupDirectoryHref||undefined" :aria-disabled="!cleanupDirectoryHref?'true':undefined" :tabindex="cleanupDirectoryHref?undefined:-1" title="仅预填当前目录，读取预览后再选择整理" @click="guardWorkspace" @auxclick="guardWorkspace">整理当前目录</a><button class="button secondary" data-testid="cloud-cache-status-button" :aria-expanded="cachePanel" @click="cachePanel=!cachePanel">缓存进度与状态</button></CloudToolsNav><section v-if="cachePanel" aria-label="缓存状态"><CloudCacheStatus kind="scan" :disabled="auth" @authentication-required="requireLogin"/><CloudCacheStatus kind="refresh" :disabled="auth" @authentication-required="requireLogin"/></section></details><details class="cloud-view-options" @keydown.esc.prevent.stop="closeBrowseDetails"><summary>视图</summary>  <div class="cloud-display-controls"><label v-if="!activeSearch" class="cloud-sort-control"><span class="cloud-sort-picker">目录排序 <select v-model="sort" data-testid="cloud-files-sort" :disabled="loading||auth||actionBusy||batchBusy||loadedSource!=='live'" @change="changeSort"><option value="name-asc">名称 · 升序</option><option value="name-desc">名称 · 降序</option><option value="size-asc">大小 · 从小到大</option><option value="size-desc">大小 · 从大到小</option><option value="time-desc">修改时间 · 最新优先</option><option value="time-asc">修改时间 · 最早优先</option></select></span><small class="muted"> 大小按当前目录完整清单排序；文件采用网盘大小，目录采用完整缓存汇总。同值名称固定升序；大小未知在所属组内置后。</small></label><label v-if="!activeSearch&amp;&amp;loadedSource==='live'" class="cloud-mixed-control" title="关闭后文件夹在前、文件在后，各组按当前规则排序"><input type="checkbox" v-model="mixed" data-testid="cloud-files-mixed" :disabled="loading||auth||actionBusy||batchBusy" @change="changeMixed"/>文件夹与文件混合排序</label><small v-if="mixedNotice" role="status">{{mixedNotice}}</small><label class="cloud-density-control" title="仅调整本浏览器的列表显示，不改变排序或网盘内容">列表密度 <select v-model="density" data-testid="cloud-files-density" @change="changeDensity"><option value="compact">紧凑 · 更多项目</option><option value="comfortable">舒适 · 更易点击</option></select></label><p v-if="densityNotice" class="muted" role="status">{{densityNotice}}</p></div></details></div>
  <form class="event-filter-panel event-filter-row" @submit.prevent="search"><label>搜索名称或路径<input v-model="queryDraft" data-testid="cloud-cache-query"/></label><details class="cloud-advanced-search" :open="extraFiltersOpen||!!activeSearch" @keydown.esc.prevent.stop="closeBrowseDetails"><summary>高级搜索</summary><div class="cloud-advanced-search-fields"><label>搜索范围<select v-model="searchScope" data-testid="cloud-search-scope" :disabled="loading||auth"><option value="all">全部网盘缓存</option><option value="current">目录及子目录：{{searchScopePath}}</option></select></label><label>对象类型<select v-model="searchKind" data-testid="cloud-search-kind" :disabled="loading||auth"><option value="all">文件与文件夹</option><option value="file">仅文件</option><option value="directory">仅文件夹</option></select></label><details class="cloud-search-extra" :open="extraFiltersOpen" @toggle="extraFiltersOpen=($event.target as HTMLDetailsElement).open"><summary>扩展名与修改时间筛选</summary><div class="cloud-search-extra-grid"><label>扩展名（仅文件）<input v-model="extensionDraft" data-testid="cloud-search-extensions" placeholder="例如 pdf, mp4, 7z" :disabled="loading||auth"/></label><label>修改时间起（UTC，含）<input type="datetime-local" step="1" v-model="modifiedFromDraft" data-testid="cloud-search-from" :disabled="loading||auth"/></label><label>修改时间止（UTC，不含）<input type="datetime-local" step="1" v-model="modifiedBeforeDraft" data-testid="cloud-search-before" :disabled="loading||auth"/></label></div><p class="muted">按缓存中的网盘修改时间筛选，UTC 时区；未知修改时间不参与匹配。改条件后点击搜索才生效。</p></details></div></details><button class="button primary" data-testid="cloud-cache-search" :disabled="loading||auth">搜索</button><button v-if="activeSearch||loadedSource==='cache'" class="button secondary" type="button" :disabled="loading||auth" @click="open(directory)">返回目录</button></form><p v-if="error&&requestedPath!==directory" class="notice" data-testid="cloud-address-failure">未能打开 {{requestedPath}}；<template v-if="ready">下方保留的是 {{directory}} 上次成功读取的列表。</template><template v-else>尚无已确认的目录列表。</template></p><p v-if="inputError" class="notice danger" role="alert">{{inputError}}</p>
  <CloudActionFeedback v-if="actionResult" :result="actionResult"/>
  <small v-if="loadedSort &amp;&amp; (sort!==loadedSort||mixed!==loadedMixed)" class="muted" role="status" data-testid="cloud-sort-pending">{{loading?'正在读取新的排序，当前列表仍为上次结果。':'排序未更新，当前仍为上次结果；请重试。'}}</small>
<section v-if="searchLocation" class="notice cloud-search-location" data-testid="cloud-search-location" aria-label="搜索定位"><strong>{{searchLocation.path}}</strong><p role="status">{{locationMessage}}</p><a class="button secondary" data-testid="cloud-search-return" :href="searchLocation.returnSearch" :aria-disabled="loading||auth" @click.prevent="returnToSearch">返回原搜索结果</a></section><section class="event-results" aria-label="目录列表"><p v-if="error" class="notice danger" role="alert">{{error}}<span v-if="ready"> · 下方为上次成功读取目录 {{directory}} 的历史结果。</span></p><p v-if="loading" role="status">{{readingDisplaySize||'正在读取目录…'}}</p><!-- UE实测范围搜索时全盘前缀会误导；保留服务端状态，不强加范围。 --><p class="muted" data-testid="cloud-files-source-status"><span v-if="activeSearch">{{activeScope==='/'?'全盘缓存搜索':'指定目录及子目录缓存搜索 '+activeScope}}“{{activeSearch}}” · {{kindNames[activeKind]}} · <span v-if="extraNotice">{{extraNotice}} · </span></span>{{loadedSource==='cache'?(activeSearch?'搜索使用本地缓存':'目录使用本地缓存'):displaySizeInfo?'目录排序快照':'实时网盘列表'}}<span v-if="loadedSource==='cache'"> · {{cacheStatus==='complete'?'目录缓存已发布，仍可能过时':'目录缓存不完整或待核对'}} · 代次 {{cacheGeneration}}。缓存无结果不能证明网盘内容不存在。</span><span v-if="observed"> · 服务端观察 UTC：{{observed}}</span></p>
   <details v-if="displaySizeInfo" class="muted" data-testid="cloud-display-size-basis"><summary>排序依据 · {{loadedMixed?'文件夹与文件混合':'文件夹在前，文件在后'}}<span v-if="loadedSort.startsWith('size-')"> · 同值名称升序 · 未知大小置后（{{displaySizeInfo.unknown_directories}} 个目录）</span> · 缓存代次 {{displaySizeInfo.cache_generation}}</summary><p>目录大小来自完整本地缓存汇总，可能过时；刷新列表会重新读取当前目录并重建排序，不会自动扫描子目录。翻页使用同一份快照。</p><p>远端清单读取 UTC：{{displaySizeInfo.remote_read_started_at_utc}} 至 {{displaySizeInfo.remote_observed_at_utc}}；缓存读取 UTC：{{displaySizeInfo.cache_read_at_utc}}。</p><p>快照创建 UTC：{{displaySizeInfo.created_at_utc}}；有效至 UTC：{{displaySizeInfo.expires_at_utc}}；本次服务响应读取 UTC：{{displaySizeInfo.read_at_utc}}。翻页不表示重新读取了网盘；清单读取期间远端仍可能变化。</p></details>
   <div v-if="selectedEntries.length" class="cloud-selection-toolbar" role="region" aria-label="本页批量操作"><strong>已选 {{selectedEntries.length}} 项（仅本页）</strong><a class="button secondary" data-testid="cloud-selected-compare" :href="comparisonHref||undefined" :aria-disabled="!comparisonHref?'true':undefined" :tabindex="comparisonHref?undefined:-1" :title="comparisonUnavailableReason||'只预填目录，由你生成只读报告'" @click="guardWorkspace" @auxclick="guardWorkspace">比较所选目录</a><span v-if="comparisonUnavailableReason" class="muted" data-testid="cloud-compare-reason">{{comparisonUnavailableReason}}</span><button class="button secondary" data-testid="cloud-selected-copy-paths" :disabled="selectionDisabled||copyPathBusy" @click="copySelectedPaths">{{copyPathBusy?'正在复制…':'复制网盘路径'}}</button><span v-if="copyPathNotice" role="status" data-testid="cloud-copy-path-feedback">{{copyPathNotice}}</span><label v-if="copyPathFallback" class="cloud-copy-path-fallback">已选网盘路径（只读，可手动复制）<textarea readonly :value="copyPathFallback" rows="3" /></label><a v-if="selectedEntries.length<=20" class="button primary" data-testid="cloud-selected-download" role="link" :href="downloadDisabledReason?undefined:downloadURL" :aria-disabled="downloadDisabledReason?'true':undefined" :tabindex="downloadDisabledReason?-1:undefined" :title="downloadDisabledReason||'在线预检所选项目'" @click="guardDownload" @auxclick="guardDownload">下载预检</a><span v-else-if="selectedEntries.length>20" class="muted">下载预检每批最多20项，请缩小选择。</span><button class="button secondary" data-testid="cloud-selected-move" :disabled="selectionDisabled||loadedSource!=='live'||actionResult?.status==='unknown'" @click="chooseBatch('move')">移动所选</button><button class="button secondary" data-testid="cloud-selected-copy" :disabled="selectionDisabled||loadedSource!=='live'||actionResult?.status==='unknown'" @click="chooseBatch('copy')">复制所选</button><button class="button secondary" data-testid="cloud-selected-delete" :disabled="selectionDisabled||loadedSource!=='live'||actionResult?.status==='unknown'" @click="chooseBatch('delete')">删除所选</button><button class="button secondary" :disabled="batchBusy" @click="clearSelection">清空选择</button><span v-if="loadedSource!=='live'" class="muted">缓存搜索结果需先到所在目录在线核对，再进行移动/删除。</span></div><div class="event-table-scroll"><table data-testid="cloud-files-table" :class="'cloud-density-'+density"><thead><tr><th class="cloud-selection-cell"><input type="checkbox" data-testid="cloud-select-page" aria-label="选择本页全部项目" :checked="entries.length>0&&selectedEntries.length===entries.length" :indeterminate="selectedEntries.length>0&&selectedEntries.length<entries.length" :disabled="selectionDisabled||!entries.length" @change="selectPage"/></th><th :aria-sort="columnSort('name')"><button v-if="loadedSource==='live' &amp;&amp; !activeSearch" class="cloud-column-sort" data-testid="cloud-header-sort-name" aria-label="按名称排序" :disabled="loading||auth||actionBusy||batchBusy||!ready" @click="sortColumn('name')">名称 <span aria-hidden="true">{{columnSort('name')==='ascending'?'↑':columnSort('name')==='descending'?'↓':'↕'}}</span></button><template v-else>名称</template></th><th>类型</th><th :aria-sort="columnSort('size')"><button v-if="loadedSource==='live' &amp;&amp; !activeSearch" class="cloud-column-sort" data-testid="cloud-header-sort-size" aria-label="按大小排序" title="按显示大小排序；完整缓存目录参与，同值名称升序，未知在组内置后" :disabled="loading||auth||actionBusy||batchBusy||!ready" @click="sortColumn('size')">大小 <span aria-hidden="true">{{columnSort('size')==='ascending'?'↑':columnSort('size')==='descending'?'↓':'↕'}}</span></button><template v-else>大小</template></th><th :aria-sort="columnSort('time')"><button v-if="loadedSource==='live' &amp;&amp; !activeSearch" class="cloud-column-sort" data-testid="cloud-header-sort-time" aria-label="按修改时间排序" :disabled="loading||auth||actionBusy||batchBusy||!ready" @click="sortColumn('time')">远端修改 UTC <span aria-hidden="true">{{columnSort('time')==='ascending'?'↑':columnSort('time')==='descending'?'↓':'↕'}}</span></button><template v-else>远端修改 UTC</template></th><th>操作</th></tr></thead><tbody><tr v-for="(entry,index) in entries" :key="entry.id" :data-cloud-id="entry.fs_id" :class="{'cloud-row-selected':!!selected[entry.fs_id],'cloud-row-located':isLocated(entry)}" :tabindex="isLocated(entry)?-1:undefined"><td class="cloud-selection-cell"><input type="checkbox" data-testid="cloud-select-item" :aria-label="'选择 '+entry.name" :checked="!!selected[entry.fs_id]" :disabled="selectionDisabled" @click="toggleSelection(entry,index,$event)"/></td><td class="cloud-name-cell"><component :is="fileIcon(entry)" :size="18" aria-hidden="true" :class="{'folder-icon':entry.directory}"/><button v-if="entry.directory" class="event-detail-link" :disabled="loading||auth||!!error" @click="open(entry.path)">{{entry.name}}</button><button v-else class="event-detail-link" data-testid="cloud-file-name-info" :aria-label="'查看 '+entry.name+' 的信息'" :disabled="loading||auth||!!error||!ready||actionBusy||batchBusy" @click="showFileInfo(entry)">{{entry.name}}</button><small v-if="activeSearch" class="muted"><br>{{entry.path}}</small></td><td>{{entry.directory?'文件夹':'文件'}}</td><td data-testid="cloud-file-size" :title="entry.directory&&!entry.size_complete?'目录大小未完整缓存':entry.size+' B'">{{size(entry)}}</td><td>{{mtime(entry.server_mtime)}}</td><td class="cloud-row-actions"><button class="button secondary" data-testid="cloud-file-info" :aria-label="'查看 '+entry.name+' 的信息'" :disabled="loading||auth||!!error||!ready||actionBusy||batchBusy" @click="showFileInfo(entry)">信息</button><template v-if="loadedSource==='live'"><a class="button secondary" data-testid="cloud-file-download" role="link" :href="downloadDisabledReason?undefined:'/intake?path='+encodeURIComponent(entry.path)" :aria-disabled="downloadDisabledReason?'true':undefined" :tabindex="downloadDisabledReason?-1:undefined" :title="downloadDisabledReason||'在线预检并移入自动监控目录'" @click="guardDownload" @auxclick="guardDownload" :aria-label="'下载 '+entry.name"><Download :size="13"/>下载</a><div class="cloud-row-more"><button class="button secondary" data-testid="cloud-row-more" :aria-label="entry.name+' 的更多操作'" :aria-expanded="rowMore===entry.fs_id" :aria-controls="'cloud-row-more-'+entry.fs_id" :disabled="loading||auth||!!error||!ready||actionBusy||batchBusy" @click="toggleRowMore(entry,$event)"><MoreHorizontal :size="13" aria-hidden="true"/>更多</button><div v-show="rowMore===entry.fs_id" :id="'cloud-row-more-'+entry.fs_id" class="cloud-row-more-actions" data-testid="cloud-row-more-actions" role="group" :aria-label="entry.name+' 的文件操作'"><button class="button secondary" data-testid="cloud-file-rename" :disabled="loading||auth||!!error||actionBusy||batchBusy||actionResult?.status==='unknown'" @click="rowMoreAction('rename',entry)">改名</button><button class="button secondary" :disabled="loading||auth||!!error||actionBusy||batchBusy||actionResult?.status==='unknown'" data-testid="cloud-file-move" @click="rowMoreAction('move',entry)">移动</button><button class="button secondary" data-testid="cloud-file-copy" :disabled="loading||auth||!!error||actionBusy||batchBusy||actionResult?.status==='unknown'" @click="rowMoreAction('copy',entry)">复制</button><button class="button secondary" :disabled="loading||auth||!!error||actionBusy||batchBusy||actionResult?.status==='unknown'" data-testid="cloud-file-delete" @click="rowMoreAction('delete',entry)">删除</button></div></div></template><button v-else class="button secondary" data-testid="cloud-search-locate" :disabled="loading||auth||!!error" @click="inspectLive(entry)">到实时目录核对</button></td></tr><tr v-if="ready&&!entries.length"><td colspan="6">{{activeSearch?'缓存中未找到匹配项，不代表远端没有这些内容':'目录为空'}}<template v-if="activeSearch"><p class="muted">已显示搜索范围：{{activeScope==='/'?'全部网盘 /':activeScope}}。在缓存页核对范围后再开始刷新。</p><a class="button secondary compact" data-testid="cloud-search-cache-review" :href="searchCacheReviewURL()">检查搜索范围缓存</a></template></td></tr></tbody></table></div>
   <p v-if="pageNotice" class="notice" role="status">{{pageNotice}}</p><p class="muted" data-testid="cloud-page-summary"><template v-if="entries.length">第 {{pageOffset+1}} 项起</template><template v-else>暂无项目</template> · 本页 {{entries.length}} 项 <span data-testid="cloud-page-size"> · 文件 {{pageSizes.files}} · 含目录缓存 {{pageSizes.total}}{{pageSizes.missing?'（已知部分，'+pageSizes.missing+' 个目录未完整缓存）':''}}{{activeSearch?'（已排除完整目录覆盖的子项，仅统计本页结果，不作全盘合计）':''}}</span></p><div class="toolbar"><button class="button secondary" data-testid="cloud-files-prev" :disabled="loading||auth||!!error||pageOffset===0" @click="page(Math.max(0,pageOffset-(activeSearch?50:100)))">上一页</button><button class="button secondary" data-testid="cloud-files-next" :disabled="loading||auth||!!error||nextOffset===null" @click="page(nextOffset!)">下一页</button></div>
  </section>
  <details v-if="!activeSearch" class="cloud-size-tools"><summary>目录大小与缓存统计</summary>  <CloudDirectorySize :key="directory+'|'+loadedSource+'|'+cacheGeneration" :path="directory" :disabled="loading||auth||!ready||!!error" @authentication-required="requireLogin"/></details>

  <p v-if="actionBusy" class="notice" role="status">正在处理：{{actionBusyLabel||action?.item?.path||action?.directory}} · <button class="button secondary" @click="actionMinimized=false">查看当前操作</button></p><p v-if="batch" class="notice">{{batchBusy?(batchBusyLabel||'批量操作进行中'):'本页最近批次'}} · <button class="button secondary" @click="batchMinimized=false">查看批次</button></p><CloudFileBatch v-if="batch" :key="batch.key" :operation="batch.operation" :items="batch.items" :directory="batch.directory" :observed-at="batch.observedAt" :minimized="batchMinimized" :after-success="refreshBatch" @close="closeBatch" @minimize="batchMinimized=true" @busy="batchBusyChanged" @auxiliary-result="actionResult=$event" @result="actionResult=$event" @authentication-required="requireLogin"/><CloudFileInfo v-if="fileInfo" :snapshot="fileInfo" @close="fileInfo=null"/><CloudFileActions v-if="action" :action="action" :after-directory-change="refreshBatch" :minimized="actionMinimized" @minimize="actionMinimized=true" @close="closeAction" @busy="actionBusyChanged" @auxiliary-result="actionResult=$event" @result="actionFinished" @authentication-required="requireLogin"/>
 </main></div>
</template>

<style scoped>
.cloud-browse-options{display:flex;flex-wrap:wrap;align-items:flex-start;gap:8px;margin:8px 0}.cloud-browse-options>.cloud-view-options{margin:0}.cloud-browse-options>details[open]{flex-basis:100%}.cloud-maintenance,.cloud-view-options,.cloud-advanced-search{min-width:0}.cloud-maintenance>summary,.cloud-view-options>summary,.cloud-advanced-search>summary,.cloud-size-tools>summary{cursor:pointer;border:1px solid var(--border,#dce3ed);background:white;border-radius:8px;padding:8px 12px;font-size:13px;font-weight:600;width:fit-content}.cloud-maintenance[open],.cloud-view-options[open]{padding:10px;border:1px solid #dce3ed;border-radius:10px;background:#f8faff}.cloud-view-options{margin:8px 0}.cloud-advanced-search-fields{display:flex;flex-wrap:wrap;gap:12px;margin-top:12px;align-items:end}.cloud-files-workspace .event-filter-row{gap:8px;align-items:end;margin:8px 0}.cloud-files-workspace .event-filter-row>label{flex:1;min-width:180px}.cloud-files-workspace .event-filter-row>details{flex-basis:auto}.cloud-files-workspace .event-filter-row>details[open]{flex-basis:100%;order:1}.cloud-size-tools{margin:14px 0}.cloud-size-tools[open]>summary{margin-bottom:10px}.cloud-sort-control small{max-width:580px}
@media(max-width:760px){.cloud-files-workspace .event-filter-row>label{flex-basis:100%}.cloud-files-workspace .event-filter-row>details{order:1}}
@media(min-width:901px){.cloud-view-options .cloud-sort-picker{display:none}}

.cloud-files-workspace :deep(th){width:auto}.cloud-files-workspace .event-filter-panel{display:inline-flex;vertical-align:bottom;background:transparent;border:0;padding:0;margin:0 16px 12px 0;max-width:100%}.cloud-header-actions{display:flex;gap:8px;flex-wrap:wrap}.cloud-files-workspace .page-header{margin-bottom:12px}.cloud-files-workspace .event-results{padding:14px}.cloud-files-workspace th,.cloud-files-workspace td{padding:8px 10px}.cloud-row-actions{white-space:nowrap}.cloud-row-actions .button{min-height:28px;padding:4px 7px;font-size:12px;border:1px solid #dce2ed;gap:4px;margin-right:5px}.cloud-name-cell>svg{vertical-align:middle;margin-right:8px;color:#7a8ba8}.cloud-name-cell>.folder-icon{color:#e6ac32;fill:#fff2ca}.cloud-files-workspace nav{display:flex;flex-wrap:wrap;gap:4px;margin:12px 0}.cloud-files-workspace nav .event-detail-link{border:1px solid #dce2ed;border-radius:5px;padding:4px 7px;background:white}.cloud-name-cell{max-width:440px;overflow:hidden;text-overflow:ellipsis}.cloud-files-workspace .button.secondary{border:1px solid #dce2ed}
</style>

<style scoped>.cloud-selection-toolbar{display:flex;flex-wrap:wrap;align-items:center;gap:8px;background:#f0f5ff;border:1px solid #cad9f5;border-radius:8px;padding:10px;margin-bottom:10px}.cloud-selection-cell{width:32px!important;text-align:center}.cloud-selection-cell input{accent-color:var(--accent);width:15px;height:15px}.cloud-row-selected{background:#f3f7ff}.cloud-row-located{background:#eef7ff;outline:2px solid var(--accent);outline-offset:-2px}</style>

<style scoped>.cloud-sort-control{display:flex;align-items:center;flex-wrap:wrap;gap:8px;margin:0 0 12px}.cloud-sort-control select{border:1px solid #ccd5e5;border-radius:6px;padding:7px;background:white;color:inherit}.cloud-sort-control small{font-weight:normal}</style>

<style scoped>
/* Filter layout review found repeated scoped additions; keep these rules once. */
.cloud-search-location{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:8px 12px}.cloud-search-location strong{grid-column:1/-1;overflow-wrap:anywhere}.cloud-search-location p{margin:0}.cloud-search-location .button{white-space:nowrap;justify-self:start}@media(max-width:640px){.cloud-search-location{grid-template-columns:minmax(0,1fr)}}.cloud-search-extra{flex:1 0 100%;margin-top:10px}.cloud-search-extra summary{cursor:pointer;width:fit-content;border:1px solid var(--line);border-radius:6px;padding:6px 10px;background:white}.cloud-search-extra-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px;margin-top:10px}.cloud-search-extra-grid input{min-width:0;width:100%}@media(max-width:760px){.cloud-search-extra-grid{grid-template-columns:minmax(0,1fr)}}
</style>

<style scoped>
.cloud-display-controls{display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:8px 16px}.cloud-density-control{display:flex;align-items:center;flex-wrap:wrap;gap:8px;margin:0 0 12px}.cloud-density-control select{border:1px solid #ccd5e5;border-radius:6px;padding:7px;background:white;color:inherit}.cloud-files-workspace .cloud-density-compact th,.cloud-files-workspace .cloud-density-compact td{padding:6px 8px}.cloud-files-workspace .cloud-density-comfortable th,.cloud-files-workspace .cloud-density-comfortable td{padding:12px}.cloud-density-comfortable .cloud-row-actions .button{min-height:36px}
</style>

<style scoped>
.cloud-column-sort{display:inline-flex;align-items:center;gap:6px;min-height:28px;padding:3px 7px;border:1px solid #dce2ed;border-radius:5px;background:white;color:inherit;font:inherit;cursor:pointer}.cloud-column-sort:disabled{cursor:default;opacity:.6}.cloud-column-sort span{color:var(--accent)}
</style>

<style scoped>.cloud-copy-path-fallback{flex:1 0 100%;display:grid;gap:6px}.cloud-copy-path-fallback textarea{width:100%;min-width:0;resize:vertical;font-family:monospace}</style>

<style scoped>
.cloud-row-more{display:inline-block;vertical-align:top}.cloud-row-more-actions{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:5px;margin:6px 5px 0 0;padding:7px;border:1px solid var(--border,#dce2ed);border-radius:7px;background:var(--surface,#fff);white-space:normal}.cloud-row-more-actions .button{margin:0;justify-content:center}.cloud-row-more-actions [data-testid=cloud-file-delete]{color:#b42318;border-color:#e8b7b3}
</style>

<style scoped>.cloud-files-workspace a[aria-disabled="true"]{opacity:.55;cursor:not-allowed}</style>

<style scoped>
.cloud-addressbar{display:flex;align-items:center;gap:8px;margin:12px 0;min-width:0}.cloud-history-actions{display:flex;gap:4px}.cloud-addressbar .button{min-height:34px;padding:6px 9px;flex-shrink:0}.cloud-pathbar{display:flex;align-items:center;gap:4px;flex:1;min-width:0;border:1px solid #ccd5e5;border-radius:7px;background:white;padding:3px}.cloud-pathbar nav{display:flex;align-items:center;flex:1;min-width:0;overflow-x:auto;white-space:nowrap;margin:0}.cloud-pathbar nav button{flex-shrink:0;border:0;padding:5px 7px}.cloud-pathbar nav svg{flex-shrink:0;color:#64748b}.cloud-pathbar [aria-current=location]{font-weight:600;color:#1e293b}.cloud-address-input{flex:1;min-width:0}.cloud-address-input input{width:100%;min-width:0;border:0;padding:5px 8px}.cloud-files-workspace .cloud-pathbar nav{margin:0;flex-wrap:nowrap;gap:1px}.cloud-addressbar .cloud-pathbar .button{border:0}.cloud-addressbar>.button{white-space:nowrap}@media(max-width:640px){.cloud-addressbar{flex-wrap:wrap}.cloud-pathbar{order:3;flex-basis:100%}.cloud-addressbar>.button{margin-left:auto}}
</style>

<style scoped>.cloud-mixed-control{display:inline-flex;align-items:center;gap:6px;font-size:13px;white-space:nowrap}.cloud-mixed-control input{accent-color:var(--accent)}</style>
