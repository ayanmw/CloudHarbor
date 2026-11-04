<script setup lang="ts">
import {computed} from 'vue';
import WorkspaceHeader from './WorkspaceHeader.vue';
import {workspaceModule} from './workspace-navigation';
const props=defineProps<{active:string;inert?:boolean}>(),emit=defineEmits<{error:[message:string]}>();
const section=computed(()=>workspaceModule(props.active));
</script>
<template><WorkspaceHeader :module="section.id" :inert="inert" @error="emit('error',$event)"><template #status><slot name="status">本机工作空间</slot></template></WorkspaceHeader><aside class="sidebar workspace-sidebar" :inert="inert"><div class="nav-label">{{section.title}}</div><nav :aria-label="section.title"><a v-for="link in section.items" :key="link.path" :href="link.path" :aria-label="link.label" :title="link.label" :class="{active:active===link.path}" :aria-current="active===link.path?'page':undefined"><component :is="link.icon" :size="18"/><span>{{link.label}}</span></a></nav><p class="sidebar-footer">NAS Downloader<small>管理控制台 · 本机存储</small></p></aside></template>
