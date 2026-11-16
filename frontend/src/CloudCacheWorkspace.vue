<script setup lang="ts">
import {ref,onMounted} from 'vue';
import {validCloudDirectory,validCloudSearchReturn} from './cloud-files-route';
import CloudToolsNav from './CloudToolsNav.vue';
import WorkspaceSidebar from './WorkspaceSidebar.vue';
import CloudCacheStatus from './CloudCacheStatus.vue';
import CloudCacheHistory from './CloudCacheHistory.vue';
import CloudIncompleteDirectories from './CloudIncompleteDirectories.vue';
const sidebarError=ref(''),auth=ref(false);
const returns=new URLSearchParams(location.search).getAll('return_search'),returnSearch=returns.length===1?validCloudSearchReturn(returns[0]||''):'';
// Return to the validated entry directory, independently of an unsent refresh draft.
const sourceDirectories=new URLSearchParams(location.search).getAll('directory');
const browseDirectory=sourceDirectories.length===1&&validCloudDirectory(sourceDirectories[0]||'')?sourceDirectories[0]:'/';
function requireLogin(){if(auth.value)return;auth.value=true;location.assign('/login?next=%2Fcloud%2Fcache');}
onMounted(()=>{document.title='缓存扫描 · CloudHarbor';});
</script>
<template><div class="workspace event-workspace cache-workspace"><WorkspaceSidebar active="/cloud/cache" @error="sidebarError=$event"/><main class="main"><header class="page-header"><div><p class="eyebrow">本地网盘缓存</p><h1>缓存扫描</h1><p class="muted">分别核对全盘遍历与指定目录刷新。默认扫描至完成，工具请求上限不代表百度官方额度。</p></div><a v-if="returnSearch" class="button secondary" data-testid="cache-return-search" :href="returnSearch">返回原搜索结果</a></header><CloudToolsNav active="/cloud/cache" :directory="browseDirectory"/><p v-if="sidebarError" class="notice danger" role="alert">{{sidebarError}}</p><CloudCacheStatus kind="scan" :disabled="auth" @authentication-required="requireLogin"/><CloudCacheStatus kind="refresh" :disabled="auth" @authentication-required="requireLogin"/><CloudIncompleteDirectories :disabled="auth" @authentication-required="requireLogin"/><CloudCacheHistory kind="scan" :disabled="auth" @authentication-required="requireLogin"/><CloudCacheHistory kind="refresh" :disabled="auth" @authentication-required="requireLogin"/></main></div></template>

<style scoped>
.cache-workspace :deep(h3),.cache-workspace :deep(.muted),.cache-workspace :deep(.notice){overflow-wrap:anywhere;min-width:0;}
.cache-workspace :deep(.cache-controls){display:grid;gap:12px;margin-top:16px;}
.cache-workspace :deep(.cache-controls select),.cache-workspace :deep(.cache-controls input:not([type=checkbox])){font:inherit;padding:9px 12px;border:1px solid #dfe3ed;border-radius:8px;background:white;max-width:100%;}
.cache-workspace :deep(.cache-controls > button){justify-self:start;}
</style>
