export const directoryNames:Record<string,string>={queued:'等待创建',sending:'请求已发送',confirmed:'创建已确认',existing:'目录已存在',failed:'明确失败',unknown:'结果待核对',cancelled:'已停止'};
export type UploadDirectory={id:string;path:string;state:string;updated_at_utc:string;actual_path:string;operation_id:string;error:string};
export type DirectorySummary={total:number;queued:number;sending:number;confirmed:number;existing:number;failed:number;unknown:number;cancelled:number};
export function directorySummary(directories:UploadDirectory[]){const counts:DirectorySummary={total:directories.length,queued:0,sending:0,confirmed:0,existing:0,failed:0,unknown:0,cancelled:0};for(const directory of directories)if(Object.hasOwn(directoryNames,directory.state))counts[directory.state as keyof Omit<DirectorySummary,'total'>]++;return counts;}
export function validUploadDirectories(value:unknown):value is UploadDirectory[]{
 if(!Array.isArray(value)||value.length>10000)return false;const ids=new Set<string>(),paths=new Set<string>();
 return value.every(d=>{if(!d||!/^[a-f0-9]{32}$/.test(d.id)||typeof d.path!=='string'||d.path.split('/').some((part:string)=>!part||part==='.'||part==='..'||/[\\\x00-\x1f\x7f]/.test(part))||ids.has(d.id)||paths.has(d.path)||(typeof d.state!=='string'||!Object.hasOwn(directoryNames,d.state))||typeof d.updated_at_utc!=='string'||typeof d.actual_path!=='string'||typeof d.operation_id!=='string'||(d.operation_id!==''&&!/^[a-f0-9]{32}$/.test(d.operation_id))||typeof d.error!=='string')return false;ids.add(d.id);paths.add(d.path);return true;});
}
