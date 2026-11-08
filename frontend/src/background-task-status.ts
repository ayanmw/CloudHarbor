import {validBatch,batchID,cleanupStatus} from './cleanup-batch';
import {operationUTC,validOperationID} from './operation-route';
export const backgroundSources=[
 {id:'download',label:'后台下载',url:'/api/health',href:'/'},
 {id:'upload',label:'文件上传',url:'/api/uploads/settings/concurrency',href:'/uploads'},
 {id:'scan',label:'全盘缓存扫描',url:'/api/cloud-inventory/scan?identity_format=string',href:'/cloud/cache'},
 {id:'refresh',label:'目录缓存刷新',url:'/api/cloud-inventory/refresh?identity_format=string',href:'/cloud/cache'},
 {id:'cleanup',label:'网盘清理批次',url:'/api/cloud/cleanup-batches?offset=0&limit=1',href:'/cloud/cleanup'},
 {id:'hash',label:'内容哈希扫描',url:'/api/cloud-cleanup/duplicate-scan?identity_format=string',href:'/cloud/cleanup'},
] as const;
export type BackgroundSource=typeof backgroundSources[number]['id'];
export type BackgroundSnapshot={summary:string;detail:string;serverUTC:string;href?:string;serverLabel?:string};
const exact=(value:unknown)=>typeof value==='string'&&/^(0|[1-9][0-9]*)$/.test(value)&&value.length<=19&&BigInt(value)<=9223372036854775807n;
const count=(value:unknown)=>Number.isSafeInteger(value)&&Number(value)>=0;
const utc=(value:unknown):value is string=>typeof value==='string'&&!!operationUTC(value);
export function backgroundSnapshot(kind:BackgroundSource,value:any):BackgroundSnapshot {
 if(!value||typeof value!=='object')throw Error('状态响应无效');
 if(kind==='cleanup'){
  if(!count(value.total)||!Array.isArray(value.batches)||value.batches.length>1||value.total<value.batches.length||(value.total===0)!==(value.batches.length===0)||!utc(value.observed_at_utc))throw Error('清理快照响应无效');
  const batch=value.batches[0];if(batch&&(!batchID(batch.batch_id)||!validBatch(batch,batch.batch_id)))throw Error('清理快照响应无效');
  return {summary:batch?`最近批次：${cleanupStatus(batch.status)} · 已确认 ${batch.confirmed} / ${batch.item_count}`:'暂无清理批次',detail:`共 ${value.total} 批，此处仅展示最近一批；记录不证明当前执行器存活。进入清理页查看其他批次或停止后续。`,serverUTC:value.observed_at_utc,serverLabel:'服务端读取',href:batch?'/cloud/cleanup?batch_id='+batch.batch_id:'/cloud/cleanup'};
 }
 if(kind==='download'){
  if(!['downloading','pending','failed','auth_wait','source_unknown'].every(key=>count(value[key])))throw Error('下载摘要响应无效');
  return {summary:`队列记录：下载中 ${value.downloading} · 等待 ${value.pending} · 失败 ${value.failed}`,detail:`等待授权 ${value.auth_wait} · 来源待核对 ${value.source_unknown}；队列状态计数，不代表当前活跃线程。`,serverUTC:''};
 }
 if(kind==='upload'){
  if(![1,2].includes(value.limit)||value.maximum!==2||!count(value.admitted_files)||!count(value.finishing_files)||value.admitted_files+value.finishing_files>2||typeof value.admission_error!=='string'||!utc(value.observed_at_utc))throw Error('上传快照响应无效');
  return {summary:`准入 ${value.admitted_files} · 收尾 ${value.finishing_files} · 上限 ${value.limit}`,detail:value.admission_error?'执行器已停止新准入，请进入上传页核对。':'当前进程所有权快照；准入不等于正在传输字节。目录任务不计入此数。',serverUTC:value.observed_at_utc};
 }
 const labels:Record<string,string>={idle:'暂无运行记录',running:'运行中',paused:'已暂停',failed:'失败',complete:'完成'};
 if(!Object.hasOwn(labels,value.status)||typeof value.worker_active!=='boolean'||typeof value.run_id!=='string'||(value.status!=='idle'&&!validOperationID(value.run_id))||(!utc(value.updated_at_utc)&&value.updated_at_utc!=='0001-01-01T00:00:00Z'))throw Error('扫描快照响应无效');
 if(kind==='hash'?(!exact(value.processed_files)||!exact(value.total_files)||BigInt(value.processed_files)>BigInt(value.total_files)):!exact(value.items_processed))throw Error('扫描进度响应无效');
 const progress=kind==='hash'?`已校验 ${value.processed_files} / ${value.total_files} 个文件。`:`已处理 ${value.items_processed} 项。`;
 const serverUTC=value.updated_at_utc.startsWith('0001-')?'':value.updated_at_utc;
 return {summary:`记录：${labels[value.status]} · ${value.worker_active?'当前进程有执行器':'当前进程无执行器'}`,detail:progress+(value.worker_active?'检查点与执行器状态分别呈现；不代表此刻正在读取网盘。':'历史运行中也可能来自重启前；请进入详情核对，不据此自动恢复。'),serverUTC};
}
