export type IntakeItem={source:{path:string};source_id:string;target:string;state:string;error?:string;async_task_id?:string;sync_confirmed?:boolean};
export type IntakeRecord={id:string;state:string;queue_dir:string;download_mode:'keep'|'delete';created:string;expires:string;items:IntakeItem[]};
export const intakeStates:Record<string,string>={ready:'待确认',sending:'已发送，等待结果',moved:'移动已核对',failed:'移动失败',not_sent:'未发送',unknown:'结果未知（隔离）'};
export const intakeErrors:Record<string,string>={move_unconfirmed:'移动结果无法确认，继续隔离',directory_completion_unconfirmed:'缺少目录完成凭据，继续隔离',async_move_unconfirmed:'异步移动尚未证明完成，继续隔离',restart_during_move:'移动期间重启，继续隔离等待核对',restart_before_send:'重启前尚未发送',provider_rejected:'网盘拒绝移动',batch_stopped:'本批已停止，未发送此项',authorization_changed_or_cancelled:'认证变化或请求取消，未发送',queue_registration_failed:'下载队列登记失败，目标仍隔离'};
export const validIntakeID=(id:unknown):id is string=>typeof id==='string'&&/^[0-9a-f]{32}$/.test(id);
export function intakeLabel(record:IntakeRecord){
 if(record.state==='expired')return '预检已失效（未提交）';
 if(record.state==='preview')return '预检记录（未提交）';
 if(record.items.some(item=>item.state==='unknown'||item.state==='sending'))return '结果未知 / 等待核对';
 return record.state==='running'?'正在处理':'处理记录';
}
const timestamp=(value:unknown):value is string=>typeof value==='string'&&/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)&&Number.isFinite(Date.parse(value));
export function validateIntake(value:IntakeRecord):IntakeRecord{
 if(!value||!validIntakeID(value.id)||!['preview','expired','running','finished'].includes(value.state)||typeof value.queue_dir!=='string'||!value.queue_dir.startsWith('/')||!['keep','delete'].includes(value.download_mode)||!timestamp(value.created)||!timestamp(value.expires)||!Array.isArray(value.items)||!value.items.length||value.items.length>20)throw Error('入队记录响应无效');
 if(value.items.some(item=>!item||!item.source||typeof item.source.path!=='string'||!item.source.path.startsWith('/')||typeof item.source_id!=='string'||!/^\d+$/.test(item.source_id)||typeof item.target!=='string'||!item.target.startsWith('/')||!Object.hasOwn(intakeStates,item.state)||(item.error!==undefined&&typeof item.error!=='string')))throw Error('入队项目响应无效');
 return value;
}
export function resultMessage(record:IntakeRecord){
 if(record.items.some(item=>item.state==='unknown'||item.state==='sending'))return '部分项目结果未知，目标保持隔离。请核对入队记录；本次不会自动重发移动，缺少完成凭据的目录没有人工放行入口。';
 if(record.items.some(item=>item.state==='failed'||item.state==='not_sent'))return '本批包含失败或未发送项目，请逐项核对；已成功项目会进入扫描下载。';
 return record.items.every(item=>item.state==='moved')?'移动结果已核对，后台将扫描并处理已确认的项目。':'请查看入队记录中的逐项结果。';
}
