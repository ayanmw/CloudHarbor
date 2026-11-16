<script setup lang="ts">
import {onMounted,ref} from 'vue';
import {validCloudDirectory,validCloudWorkspaceReturn} from './cloud-files-route';
import CloudToolsNav from './CloudToolsNav.vue';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import DuplicatePreview from './DuplicatePreview.vue';
import DuplicateScanStatus from './DuplicateScanStatus.vue';
import EmptyDirectoryPreview from './EmptyDirectoryPreview.vue';
import CleanupBatch from './CleanupBatch.vue';
import type {CleanupCandidate} from './cleanup-types';
const params=new URLSearchParams(location.search);
const scope=params.getAll('directory').length===1&&validCloudDirectory(params.get('directory')||'')?params.get('directory')!:'/';
const returnFiles=params.getAll('return_files').length===1?validCloudWorkspaceReturn(params.get('return_files')||''):'';
const duplicateSelection=ref<CleanupCandidate[]>([]),emptySelection=ref<CleanupCandidate[]>([]);
const locked=ref(false),duplicatePreview=ref<InstanceType<typeof DuplicatePreview>>(),emptyPreview=ref<InstanceType<typeof EmptyDirectoryPreview>>();
async function reconcile(){const results=await Promise.allSettled([duplicatePreview.value?.refreshAfterMutation(),emptyPreview.value?.refreshAfterMutation()]);return results.every(r=>r.status==='fulfilled'&&r.value===true);}
const hashSummary=ref('读取中');
const auth=ref(false),sidebarError=ref('');
function requireLogin(){if(auth.value)return;auth.value=true;location.assign('/login?next=%2Fcloud%2Fcleanup');}
onMounted(()=>{document.title='网盘清理 · CloudHarbor';});
</script>
<template><div class="workspace event-workspace cleanup-workspace"><WorkspaceSidebar active="/cloud/cleanup" @error="sidebarError=$event"/><main class="main"><header class="page-header"><div><p class="eyebrow">网盘缓存与内容核对</p><h1>重复文件与空目录</h1><p class="muted">先扫描、预览，再确认远端操作。候选来自本地缓存，实际删除以服务端重新核对为准。</p></div></header><CloudToolsNav active="/cloud/cleanup" :directory="scope" :return-files="returnFiles"/><p v-if="sidebarError" role="alert" class="notice danger">{{sidebarError}}</p><fieldset class="cleanup-preview-fieldset" :disabled="locked"><DuplicatePreview ref="duplicatePreview" :disabled="auth" @selection="duplicateSelection=$event" @authentication-required="requireLogin"/><EmptyDirectoryPreview ref="emptyPreview" :disabled="auth" @selection="emptySelection=$event" @authentication-required="requireLogin"/><details class="cleanup-maintenance"><summary>内容校验扫描（全盘维护）<span class="muted" role="status"> · {{hashSummary}}</span></summary><p class="muted">需要补充文件内容校验时再启动全盘扫描；不会改变上方已选择的目录范围。</p><DuplicateScanStatus @status="hashSummary=$event" :disabled="auth" @authentication-required="requireLogin"/></details></fieldset><p data-testid="cleanup-selected-count">已选择 {{duplicateSelection.length+emptySelection.length}} 项，执行前需确认固定清单。</p><CleanupBatch :candidates="[...duplicateSelection,...emptySelection]" :disabled="auth" :reconcile="reconcile" @locked="locked=$event" @authentication-required="requireLogin"/></main></div></template>
<style scoped>.cleanup-maintenance{margin:16px 0}.cleanup-maintenance>summary{cursor:pointer;border:1px solid #dce3ed;border-radius:8px;padding:10px 12px;background:#fff;font-weight:600}.cleanup-workspace :deep(.muted){overflow-wrap:anywhere;min-width:0}.cleanup-workspace :deep(.cleanup-group){border-top:1px solid #e7eaf0;margin-top:16px;padding-top:8px}.cleanup-preview-fieldset{border:0;padding:0;margin:0;min-width:0}.cleanup-workspace :deep(th){width:auto}</style>
