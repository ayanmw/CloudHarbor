<script setup lang="ts">
import {Download,LogOut,UserRound} from 'lucide-vue-next';
import BackgroundTasks from './BackgroundTasks.vue';
import {workspaceModules} from './workspace-navigation';
defineProps<{module:string;inert?:boolean}>();
const emit=defineEmits<{error:[string]}>();
async function logout(){try{const response=await fetch('/api/logout',{method:'POST',credentials:'same-origin'});if(!response.ok)throw Error('退出登录失败，请重试');location.assign('/login');}catch(e:any){emit('error',e.message||'退出登录失败，请重试')}}
</script>
<template><header class="app-header" :inert="inert"><a class="brand app-brand" href="/" aria-label="NAS Downloader"><span class="brand-icon"><Download :size="21"/></span><span>NAS Downloader<small>百度网盘自动下载</small></span></a><nav class="app-module-nav" aria-label="核心功能"><a v-for="entry in workspaceModules" :key="entry.id" :href="entry.path" :data-module="entry.id" :class="{selected:module===entry.id}" :aria-current="module===entry.id?'page':undefined"><component :is="entry.icon" :size="17"/><span>{{entry.label}}</span></a></nav><div class="app-header-actions"><span class="app-service-status"><slot name="status">本机工作空间</slot></span><BackgroundTasks :disabled="inert"/><a class="header-icon-button" href="/oauth" aria-label="账号与授权" title="账号与授权"><UserRound :size="18"/><span>账号</span></a><button class="header-icon-button" type="button" aria-label="退出登录" title="退出登录" @click="logout"><LogOut :size="18"/><span>退出</span></button></div></header></template>
