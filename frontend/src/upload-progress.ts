export type UploadItem={pause_requested?:boolean;id:string;path:string;size:string;sent:string;state:string;updated_at_utc:string;error:string;actual_path:string};
export const uploadNames:Record<string,string>={receiving:'等待浏览器传入',queued:'等待上传',hashing:'计算分片校验',submitting:'准备远端上传',uploading:'上传分片',finalizing:'合并并核对',confirmed:'完成',failed:'失败',unknown:'结果待核对',cancelled:'已停止',paused:'已暂停'};
const activeStates=new Set(['hashing','submitting','uploading','finalizing']);
function bytes(value:unknown):value is string{return typeof value==='string'&&/^(0|[1-9][0-9]*)$/.test(value)&&(value.length<19||(value.length===19&&value<='9223372036854775807'));}
export function validUploadItem(value:unknown):value is UploadItem{
 if(!value||typeof value!=='object')return false;const i=value as UploadItem;
 return typeof i.id==='string'&&/^[a-f0-9]{32}$/.test(i.id)&&typeof i.path==='string'&&bytes(i.size)&&bytes(i.sent)&&BigInt(i.sent)<=BigInt(i.size)&&typeof i.state==='string'&&Object.hasOwn(uploadNames,i.state)&&typeof i.updated_at_utc==='string'&&typeof i.error==='string'&&typeof i.actual_path==='string'&&(i.pause_requested===undefined||(typeof i.pause_requested==='boolean'&&(!i.pause_requested||activeStates.has(i.state))));
}
export function uploadSummary(items:UploadItem[]){
 let total=0n,confirmedBytes=0n,confirmed=0,active=0,waiting=0,failed=0,unknown=0,cancelled=0,paused=0;
 for(const item of items){total+=BigInt(item.size);if(item.state==='confirmed'){confirmed++;confirmedBytes+=BigInt(item.size);}else if(activeStates.has(item.state))active++;else if(item.state==='queued'||item.state==='receiving')waiting++;else if(item.state==='failed')failed++;else if(item.state==='unknown')unknown++;else if(item.state==='cancelled')cancelled++;else if(item.state==='paused')paused++;}
 return {total:items.length,totalBytes:total.toString(),confirmedBytes:confirmedBytes.toString(),confirmed,active,waiting,failed,unknown,cancelled,paused};
}
export function uploadItemProgress(item:UploadItem):{percent:string|null;description:string}{
 if(item.state==='confirmed')return {percent:'100',description:'远端文件已确认，实际位置见结果'};
 if(item.state==='uploading'&&BigInt(item.size)>0n){
  // Floor one decimal place so an unacknowledged final byte cannot display 100%.
  const n=BigInt(item.sent)*1000n/BigInt(item.size);
  return {percent:(n/10n).toString()+(n%10n?'.'+(n%10n).toString():''),description:'已确认分片比例（含可信恢复记录），尚未确认远端文件完成'};
 }
 const descriptions:Record<string,string>={paused:'已暂停，暂存保留；继续前核对会话和文件，未知请求保持隔离',receiving:'浏览器 → 本机：等待完整接收',queued:'等待后台处理；恢复时核对会话，跳过有可信确认的分片',hashing:'当前正在本机计算分片校验',submitting:'请求远端预创建，等待上传会话',uploading:'等待分片确认',finalizing:'合并及远端确认中，尚未完成',unknown:'提交结果待核对，不能计为完成',failed:'明确失败；重试前需保留本机暂存',cancelled:'已停止后续处理，不代表删除远端内容'};
 return {percent:null,description:descriptions[item.state]||'状态待核对'};
}
