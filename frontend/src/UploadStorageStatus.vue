<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {HardDrive} from 'lucide-vue-next';
import {operationUTC} from './operation-route';
import {formatBytes} from './format-bytes';
const props=defineProps<{disabled:boolean;selectedBytes:string}>(),emit=defineEmits<{authenticationRequired:[]}>();
type Snapshot={directory:string;staging_bytes:string;staging_files:string;partial:boolean;disappeared_files:string;scan_started_at_utc:string;scan_finished_at_utc:string;available_bytes:string|null;available_error:string;available_checked_at_utc:string};
const opened=ref(false),loading=ref(false),fresh=ref(false),error=ref(''),snapshot=ref<Snapshot|null>(null);
let generation=0,active=true,controller:AbortController|undefined;
const count=(x:unknown):x is string=>typeof x==='string'&&/^(0|[1-9][0-9]*)$/.test(x)&&x.length<=19&&BigInt(x)<=9223372036854775807n;
const utc=(x:unknown)=>typeof x==='string'&&!!operationUTC(x);
function valid(value:any):value is Snapshot{return value&&typeof value.directory==='string'&&!!value.directory&&['staging_bytes','staging_files','disappeared_files'].every(k=>count(value[k]))&&typeof value.partial==='boolean'&&value.partial===(value.disappeared_files!=='0')&&['scan_started_at_utc','scan_finished_at_utc','available_checked_at_utc'].every(k=>utc(value[k]))&&typeof value.available_error==='string'&&(value.available_bytes===null?!!value.available_error:count(value.available_bytes)&&!value.available_error);}
const shortage=computed(()=>fresh.value&&!loading.value&&snapshot.value?.available_bytes!==null&&snapshot.value?.available_bytes!==undefined&&/^(0|[1-9][0-9]*)$/.test(props.selectedBytes)&&BigInt(props.selectedBytes)>BigInt(snapshot.value.available_bytes));
function close(){generation++;controller?.abort();opened.value=false;loading.value=false;fresh.value=false;}
function toggle(){if(opened.value){close();return}if(props.disabled||!active)return;opened.value=true;load();}
async function load(){
 if(!opened.value||loading.value||props.disabled||!active)return;const request=++generation;controller?.abort();controller=new AbortController();loading.value=true;fresh.value=false;
 const current=()=>active&&opened.value&&!props.disabled&&request===generation;
 try{const response=await fetch('/api/uploads/storage',{credentials:'same-origin',signal:controller.signal});if(!current())return;if(response.status===401){close();emit('authenticationRequired');return}if(!response.ok)throw Error('暂存空间读取失败，请稍后重新核对。');const value=await response.json();if(!current())return;if(!valid(value))throw Error('暂存空间响应无效。');snapshot.value=value;error.value='';fresh.value=true;}
 catch(e:any){if(current())error.value=e.message||'暂存空间读取失败。';}
 finally{if(current())loading.value=false;}
}
function leave(){active=false;close();}
function resume(event:PageTransitionEvent){if(event.persisted)active=true;}
watch(()=>props.disabled,value=>{if(value)close()});
onMounted(()=>{window.addEventListener('pagehide',leave);window.addEventListener('pageshow',resume);window.addEventListener('popstate',close)});
onUnmounted(()=>{leave();window.removeEventListener('pagehide',leave);window.removeEventListener('pageshow',resume);window.removeEventListener('popstate',close)});
</script>
<template><section class="upload-storage-status"><button class="button secondary" data-testid="upload-storage-open" :disabled="disabled" :aria-expanded="opened" @click="toggle"><HardDrive :size="16"/>上传暂存空间</button><div v-if="opened" class="event-results" data-testid="upload-storage-panel"><header class="page-header"><h2>暂存文件与磁盘容量</h2><button class="button secondary compact" data-testid="upload-storage-refresh" :disabled="loading||disabled" @click="load">{{loading?'正在读取…':'刷新空间快照'}}</button></header><p v-if="error" class="notice danger" role="alert">{{error}}</p><p v-if="snapshot&&!fresh" class="notice" role="status">下方为上次空间快照，当前容量尚待核对。</p><p>当前所选文件：{{formatBytes(selectedBytes)}}<small class="cell-sub">不是整个批次的空间需求；补传实际接收量可能更少，目录和数据库另有开销。</small></p><template v-if="snapshot"><div class="event-table-scroll"><table><thead><tr><th>暂存逻辑大小</th><th>暂存文件数</th><th>所在卷可用空间</th></tr></thead><tbody><tr><td :title="snapshot.staging_bytes+' B'">{{formatBytes(snapshot.staging_bytes)}}</td><td>{{snapshot.staging_files}}</td><td>{{snapshot.available_bytes===null?'未知':formatBytes(snapshot.available_bytes)}}<small v-if="snapshot.available_error" class="cell-sub">{{snapshot.available_error}}</small></td></tr></tbody></table></div><p v-if="shortage" class="notice danger" role="status" data-testid="upload-storage-shortage">本次选择量超过所观察的可用空间，建议先释放空间或减少选择；不要把旧快照当作已预留空间。</p><p v-if="snapshot.partial" class="notice">统计期间有 {{snapshot.disappeared_files}} 个文件消失，可能正在接收或清理；已知部分不代表完整暂存量。</p><dl><dt>暂存路径</dt><dd>{{snapshot.directory}}</dd><dt>文件统计区间 UTC</dt><dd>{{snapshot.scan_started_at_utc}} 至 {{snapshot.scan_finished_at_utc}}</dd><dt>容量读取 UTC</dt><dd>{{snapshot.available_checked_at_utc}}</dd></dl></template><p class="muted">仅统计本工具顶层暂存文件的逻辑大小，不含数据库，不等于磁盘实际分配。统计与容量读取不是原子快照；可用空间未预留，其他任务可能同时占用，实际写入仍可能失败。这里只读，不执行清理。</p></div></section></template>
<style scoped>
.upload-storage-status{margin:12px 0}.upload-storage-status>.event-results{margin-top:10px;min-width:0}.upload-storage-status dl{display:grid;grid-template-columns:130px minmax(0,1fr);gap:8px;font-size:13px}.upload-storage-status dt,.upload-storage-status dd{margin:0;overflow-wrap:anywhere}.upload-storage-status table{min-width:400px}.upload-storage-status .page-header{flex-wrap:wrap;gap:10px}.upload-storage-status h2{font-size:18px}@media(max-width:540px){.upload-storage-status dl{grid-template-columns:minmax(0,1fr)}.upload-storage-status dd{margin-bottom:8px}}
</style>
