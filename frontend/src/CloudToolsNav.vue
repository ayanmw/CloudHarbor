<script setup lang="ts">
import {Folder,Database,Copy,FileClock,SlidersHorizontal} from 'lucide-vue-next';
const props=defineProps<{active:string;directory?:string}>();
const links=[{path:'/cloud/files',label:'网盘文件',icon:Folder},{path:'/cloud/cache',label:'缓存与刷新',icon:Database},{path:'/cloud/cleanup',label:'重复与空目录',icon:Copy},{path:'/cloud/history',label:'扫描历史',icon:FileClock},{path:'/cloud',label:'高级管理',icon:SlidersHorizontal}];
function href(path:string){return path==='/cloud/cache'&&props.directory!==undefined?path+'?directory='+encodeURIComponent(props.directory):path;}
</script>
<template><nav class="cloud-tools-nav" aria-label="网盘管理快捷导航"><a v-for="link in links" :key="link.path" class="button secondary" :class="{current:active===link.path}" :href="href(link.path)" :aria-current="active===link.path?'page':undefined"><component :is="link.icon" :size="15"/>{{link.label}}</a></nav></template>
