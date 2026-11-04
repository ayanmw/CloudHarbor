import {Download,Upload,Cloud,History,Activity,Settings,Folder,Database,Copy,FileClock,ClipboardList,SlidersHorizontal,ArrowRightLeft} from 'lucide-vue-next';
export const workspaceModules=[
 {id:'transfers',label:'传输',path:'/',icon:ArrowRightLeft,title:'下载与上传任务',items:[{path:'/',label:'下载任务',icon:Download},{path:'/uploads',label:'上传任务',icon:Upload}]},
 {id:'cloud',label:'网盘',path:'/cloud/files',icon:Cloud,title:'网盘管理',items:[{path:'/cloud/files',label:'网盘文件',icon:Folder},{path:'/cloud/cache',label:'缓存与目录刷新',icon:Database},{path:'/cloud/cleanup',label:'重复与空目录',icon:Copy},{path:'/cloud/history',label:'扫描历史',icon:FileClock},{path:'/cloud',label:'高级管理',icon:SlidersHorizontal}]},
 {id:'history',label:'记录',path:'/intake-history',icon:History,title:'记录与诊断',items:[{path:'/intake-history',label:'入队记录',icon:ClipboardList},{path:'/operations',label:'操作记录',icon:History},{path:'/monitor-history',label:'监控历史',icon:Activity},{path:'/task-events',label:'任务事件',icon:FileClock}]},
 {id:'settings',label:'设置',path:'/settings/paths',icon:Settings,title:'系统设置',items:[{path:'/settings/paths',label:'路径与存储',icon:Folder},{path:'/settings/migrations',label:'目录迁移记录',icon:History},{path:'/oauth',label:'账号与授权',icon:Settings}]},
] as const;
export function workspaceModule(path:string){if(path.startsWith('/cloud')||path==='/intake')return workspaceModules[1];if(['/intake-history','/operations','/monitor-history','/task-events'].includes(path))return workspaceModules[2];if(path.startsWith('/settings/')||path==='/oauth')return workspaceModules[3];return workspaceModules[0];}
