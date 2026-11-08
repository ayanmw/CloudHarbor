export type ManagedPathSource="environment"|"saved"|"startup";
export type ManagedPathSettings={queue_dir:string;local_download_dir:string;queue_editable:boolean;local_download_editable:boolean;queue_busy:boolean;queue_busy_reason:string;queue_source?:ManagedPathSource;local_download_source?:ManagedPathSource};
export function validateManagedPathSettings(value:ManagedPathSettings):ManagedPathSettings{
 if(!value||typeof value.queue_dir!=='string'||!value.queue_dir.startsWith('/')||typeof value.local_download_dir!=='string'||!value.local_download_dir||typeof value.queue_editable!=='boolean'||typeof value.local_download_editable!=='boolean'||typeof value.queue_busy!=='boolean'||typeof value.queue_busy_reason!=='string'||value.queue_busy!==(value.queue_busy_reason!==''))throw Error('路径配置响应无效，请重新读取');
 for(const [source,editable] of [[value.queue_source,value.queue_editable],[value.local_download_source,value.local_download_editable]]){if(source!==undefined&&(!['environment','saved','startup'].includes(String(source))||(source==='environment')===editable))throw Error('路径配置来源响应无效，请重新读取');}
 return {queue_dir:value.queue_dir,local_download_dir:value.local_download_dir,queue_editable:value.queue_editable,local_download_editable:value.local_download_editable,queue_busy:value.queue_busy,queue_busy_reason:value.queue_busy_reason,...(value.queue_source===undefined?{}:{queue_source:value.queue_source}),...(value.local_download_source===undefined?{}:{local_download_source:value.local_download_source})};
}
export type PathImpactTask={fs_id:string;path:string;server_filename:string;status:'pending'|'auth_wait'|'retry_wait';attempts:number;download_mode:'keep'|'delete'};
export type PathImpact={current_queue_dir:string;target_queue_dir:string;count:number;offset:number;limit:number;has_more:boolean;tasks:PathImpactTask[];digest:string};
export function queueTargetError(value:string){
 if(!value.startsWith('/')||value==='/'||/[\\\u0000-\u001f\u007f-\u009f]/u.test(value)||value.slice(1).split('/').some(part=>!part||part==='.'||part==='..'))return '监控目录必须是以 / 开头的规范绝对路径，且为非根目录';
 if(new Blob([value]).size>2048)return '监控目录路径超过服务端允许的 2048 字节';
 return '';
}
export function validatePathImpact(data:ManagedPathSettings&{queue_change_impact:PathImpact},current:string,target:string,offset:number){
 const settings=validateManagedPathSettings(data),value=data.queue_change_impact;
 if(settings.queue_dir!==current||!settings.queue_editable)throw Error('当前监控配置已变化，请刷新配置后重新预览');
 if(settings.queue_busy)throw Error(settings.queue_busy_reason);
 if(!value||value.current_queue_dir!==current||value.target_queue_dir!==target||!Number.isSafeInteger(value.count)||value.count<0||value.offset!==offset||value.limit!==50||typeof value.has_more!=='boolean'||typeof value.digest!=='string'||!/^[a-f0-9]{64}$/.test(value.digest)||!Array.isArray(value.tasks)||value.tasks.length!==Math.min(50,Math.max(0,value.count-offset))||value.has_more!==(offset+value.tasks.length<value.count))throw Error('影响预览响应无效或位置已变化，请重新预览');
 let previous=0n;
 for(const task of value.tasks){
  if(!task||typeof task.fs_id!=='string'||!/^[1-9]\d{0,18}$/.test(task.fs_id)||BigInt(task.fs_id)>9223372036854775807n||BigInt(task.fs_id)<=previous)throw Error('任务身份响应无效，不能使用舍入后的文件 ID');
  previous=BigInt(task.fs_id);
  if(typeof task.path!=='string'||!task.path.startsWith(current+'/')||typeof task.server_filename!=='string'||!['pending','auth_wait','retry_wait'].includes(task.status)||!Number.isSafeInteger(task.attempts)||task.attempts<0||!['keep','delete'].includes(task.download_mode))throw Error('影响预览任务响应无效');
 }
 return value;
}
