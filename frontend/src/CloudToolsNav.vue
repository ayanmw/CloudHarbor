<script setup lang="ts">
import {computed} from 'vue';
import {Folder,Database} from 'lucide-vue-next';
import {validCloudDirectory,validCloudWorkspaceReturn} from './cloud-files-route';
const props=defineProps<{active:string;directory?:string;returnFiles?:string}>();
// The sidebar owns full page navigation; keep only context-sensitive tools here.
const directory=computed(()=>props.directory&&validCloudDirectory(props.directory)?props.directory:'/');
const cacheHref=computed(()=>'/cloud/cache?directory='+encodeURIComponent(directory.value));
const filesHref=computed(()=>validCloudWorkspaceReturn(props.returnFiles||'')||(directory.value==='/'?'/cloud/files':'/cloud/files?dir='+encodeURIComponent(directory.value)));
</script>
<template><section class="cloud-tools-group"><div class="cloud-tools-nav" role="group" aria-label="网盘上下文操作"><a v-if="active==='/cloud/files'" class="button secondary" data-testid="cloud-directory-tools" :href="cacheHref" :title="'打开 '+directory+' 的缓存工具'"><Database :size="15" aria-hidden="true"/>目录缓存工具</a><a v-else class="button secondary" data-testid="cloud-return-files" :href="filesHref" :title="'返回 '+directory"><Folder :size="15" aria-hidden="true"/>返回网盘文件</a><slot/></div></section></template>

<style scoped>
.cloud-tools-group{margin:8px 0 12px;min-width:0}.cloud-tools-nav{display:flex;align-items:center;flex-wrap:wrap;gap:8px;min-width:0}.cloud-tools-nav .button{border:1px solid var(--border,#dce3ed)}
</style>
