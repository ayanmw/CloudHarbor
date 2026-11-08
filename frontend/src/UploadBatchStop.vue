<script setup lang="ts">
import {computed} from 'vue';
import {vModal} from './modal';
const props=defineProps<{batchId:string;target:string;states:string[];directoryStates?:string[]}>(),emit=defineEmits<{close:[];confirm:[]}>();
const counts=computed(()=>({waiting:props.states.filter(s=>['receiving','queued','paused'].includes(s)).length,active:props.states.filter(s=>['hashing','submitting','uploading','finalizing'].includes(s)).length,unknown:props.states.filter(s=>s==='unknown').length,cancelled:props.states.filter(s=>s==='cancelled').length}));
const directoryCounts=computed(()=>({queued:(props.directoryStates||[]).filter(state=>state==='queued').length,sending:(props.directoryStates||[]).filter(state=>state==='sending').length,unknown:(props.directoryStates||[]).filter(state=>state==='unknown').length}));
let consumed=false;
function confirm(){if(consumed)return;consumed=true;emit('confirm');}
</script>
<template><div class="modal-backdrop" @click.self="emit('close')"><section class="detail-drawer upload-stop-confirmation" role="dialog" aria-modal="true" aria-labelledby="upload-stop-title" data-testid="upload-stop-confirmation" v-modal="()=>emit('close')"><h2 id="upload-stop-title">{{directoryStates?.length?'确认停止本批等待项目':'确认停止本批等待文件'}}</h2><p>批次：{{batchId}}</p><p>网盘目标：{{target}}</p><p>当前读取：可停止 {{counts.waiting}} · 正在处理 {{counts.active}} · 结果未知 {{counts.unknown}} · 已停止 {{counts.cancelled}}</p><p v-if="directoryStates?.length" class="notice">目录：可停止 {{directoryCounts.queued}} · 请求已发送 {{directoryCounts.sending}} · 待核对 {{directoryCounts.unknown}}。仅停止排队中的目录；不会删除远端目录，已发送和未知项保留。</p><p class="notice">停止本批等待本机接收、已排队及已暂停的文件，并清理已停止项的本机暂存。需要再次上传时，可能需重新选择原文件。</p><p>不会强制中断正在处理的文件，不会放行结果未知项，也不会删除网盘文件。活跃浏览器接收期间，服务端会拒绝停止。</p><p class="muted">数量来自最近读取，状态可能变化，以服务端执行时为准。即使没有新的可停止项，也可用此操作重试已停止项的暂存清理。</p><div class="toolbar"><button class="button secondary" data-testid="upload-stop-cancel" @click="emit('close')">返回</button><button class="button danger-button" data-testid="upload-stop-submit" @click="confirm">确认停止并清理暂存</button></div></section></div></template>

<style scoped>.upload-stop-confirmation p{overflow-wrap:anywhere}</style>
