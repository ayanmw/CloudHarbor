export type HistorySource='scan'|'refresh'|'hash';
export const historySources:Record<HistorySource,string>={scan:'全盘缓存扫描',refresh:'指定目录刷新',hash:'内容哈希扫描'};
export const historyStatuses:Record<string,string>={running:'运行中记录',paused:'已暂停',failed:'失败',complete:'完成'};
export const historyPhases:Record<string,string>={idle:'未开始',listing:'读取目录',hashing:'校验内容',backoff:'等待重试',paused:'暂停',request_budget_reached:'达到工具上限',failed:'失败',complete:'完成'};
export const reasonLabels={scanning_directories:'扫描中目录',stale_directories:'已失效目录',unknown_directories:'未知目录',failed_directories:'失败目录',missing_directory_status:'缺少目录状态',orphan_directory_status:'孤立目录状态',parent_not_complete:'父目录未完整',path_mismatch:'路径不一致',missing_root:'缺少根目录'};
export type HistoryReasons=Record<keyof typeof reasonLabels,string>;
export type HistoryComparison={status:string;reason_status:string;previous_run_id:string;previous_sequence:string;previous_observed_at_utc:string|null;previous_coverage_status:string|null;previous_incomplete_directories:string|null;previous_reasons:HistoryReasons|null;incomplete_directories_delta:string|null;reason_deltas:HistoryReasons|null};
type BaseRun={run_id:string;sequence:string;status:string;phase:string;started_at_utc:string;updated_at_utc:string;last_error:string};
type InventoryCounts={current_directory:string;pages_processed:string;items_processed:string;pending_count:string;request_budget:string;requests_used:string};
export type HistoryRun=(BaseRun&InventoryCounts&{source:'scan';coverage_status:string|null;coverage_incomplete_directories:string|null;coverage_generation:string|null;coverage_observed_at_utc:string|null;coverage_reasons:HistoryReasons|null;coverage_comparison:HistoryComparison|null})|(BaseRun&InventoryCounts&{source:'refresh';root_path:string;latest_kind:string;latest_sequence:string})|(BaseRun&{source:'hash';next_fs_id:string;latest_sequence:string;total_files:string;processed_files:string;total_bytes:string;processed_bytes:string});
export type HistoryPage={rows:HistoryRun[];next:string;more:boolean};
export function historyCount(value:unknown,zero=true):value is string{return typeof value==='string'&&/^(0|[1-9][0-9]*)$/.test(value)&&value.length<=19&&BigInt(value)<=9223372036854775807n&&(zero||value!=='0');}
export function historySigned(value:unknown):value is string{return typeof value==='string'&&/^(0|-?[1-9][0-9]*)$/.test(value)&&value.length<=20&&BigInt(value)>=-9223372036854775808n&&BigInt(value)<=9223372036854775807n;}
export function historyRunID(value:unknown):value is string{return typeof value==='string'&&/^[a-f0-9]{32}$/.test(value);}
export function historyUTC(value:unknown):value is string{if(typeof value!=='string'||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/.test(value)||!Number.isFinite(Date.parse(value)))return false;return new Date(value.slice(0,10)+'T00:00:00Z').toISOString().slice(0,10)===value.slice(0,10)&&Number(value.slice(11,13))<24&&Number(value.slice(14,16))<60&&Number(value.slice(17,19))<60;}
const nullableCount=(value:unknown)=>value===null||historyCount(value);
const nullableUTC=(value:unknown)=>value===null||historyUTC(value);
const label=(value:unknown)=>typeof value==='string'&&value.length>0&&value.length<=128;
function reasons(value:any,signed=false){return value===null||value&&Object.keys(reasonLabels).every(key=>signed?historySigned(value[key]):historyCount(value[key]));}
function comparison(value:any){return value===null||value&&label(value.status)&&typeof value.reason_status==='string'&&typeof value.previous_run_id==='string'&&(!value.previous_run_id||historyRunID(value.previous_run_id))&&historyCount(value.previous_sequence)&&nullableUTC(value.previous_observed_at_utc)&&(value.previous_coverage_status===null||label(value.previous_coverage_status))&&nullableCount(value.previous_incomplete_directories)&&reasons(value.previous_reasons)&&(value.incomplete_directories_delta===null||historySigned(value.incomplete_directories_delta))&&reasons(value.reason_deltas,true);}
export function parseHistoryPage(data:any,source:HistorySource,before='0'):HistoryPage{
 if(!data||!Array.isArray(data.rows)||data.rows.length>20||typeof data.has_more!=='boolean'||!historyCount(data.next_before_sequence))throw Error('历史分页响应无效');
 let previous=before==='0'?9223372036854775808n:BigInt(before);const ids=new Set();const rows:HistoryRun[]=[];
 for(const row of data.rows){if(!row||!historyRunID(row.run_id)||ids.has(row.run_id)||!historyCount(row.sequence,false)||BigInt(row.sequence)>=previous||!label(row.status)||!label(row.phase)||!historyUTC(row.started_at_utc)||!historyUTC(row.updated_at_utc)||typeof row.last_error!=='string')throw Error('历史运行身份或时间无效');previous=BigInt(row.sequence);ids.add(row.run_id);
  if(source==='hash'){if(!['next_fs_id','latest_sequence','total_files','processed_files','total_bytes','processed_bytes'].every(k=>historyCount(row[k])))throw Error('哈希历史计数无效');}
  else{if(!['pages_processed','items_processed','pending_count','request_budget','requests_used'].every(k=>historyCount(row[k]))||typeof row.current_directory!=='string')throw Error('目录历史计数无效');if(source==='refresh'&&(typeof row.root_path!=='string'||typeof row.latest_kind!=='string'||!historyCount(row.latest_sequence)))throw Error('目录刷新历史无效');if(source==='scan'&&(!(row.coverage_status===null||label(row.coverage_status))||!nullableCount(row.coverage_incomplete_directories)||!nullableCount(row.coverage_generation)||!nullableUTC(row.coverage_observed_at_utc)||!reasons(row.coverage_reasons)||!comparison(row.coverage_comparison)))throw Error('历史覆盖证据无效');}
  rows.push({...row,source});
 }
 if(data.has_more&&(!rows.length||data.next_before_sequence!==rows.at(-1)!.sequence||(before!=='0'&&BigInt(data.next_before_sequence)>=BigInt(before))))throw Error('历史游标未向前推进');
 return {rows,next:data.next_before_sequence,more:data.has_more};
}
export const historyAPIBase=(source:HistorySource)=>source==='hash'?'/api/cloud-cleanup/duplicate-scan':'/api/cloud-inventory/'+source;
export const historyTimeText=(value:string|null)=>!value||value.startsWith('0001-')?'历史未记录':value;

type EventBase={run_id:string;sequence:string;status:string;phase:string;recorded_at_utc:string};
type InventoryEvent=EventBase&InventoryCounts&{kind:string;last_error:string;restart_reason:string};
export type HistoryEvent=(InventoryEvent&{source:'scan'})|(InventoryEvent&{source:'refresh';request_budget_added:string;retry_at_utc:string|null;restart_offset:string|null;restart_fs_id:string|null;cloud_observed_at_utc:string|null;directory_changed:boolean|null;added_count:string|null;removed_count:string|null;changed_count:string|null})|(EventBase&{source:'hash';evidence_json:string});
export function parseHistoryEvents(data:any,source:HistorySource,run:string,before='0'){
 if(!data||data.run_id!==run||!Array.isArray(data.rows)||data.rows.length>20||typeof data.has_more!=='boolean'||!historyCount(data.next_before_sequence))throw Error('事件身份或分页响应无效');
 let previous=before==='0'?9223372036854775808n:BigInt(before);const rows:HistoryEvent[]=[];
 for(const row of data.rows){if(!row||row.run_id!==run||!historyCount(row.sequence,false)||BigInt(row.sequence)>=previous||typeof row.status!=='string'||typeof row.phase!=='string'||!historyUTC(row.recorded_at_utc))throw Error('事件运行身份、序号或时间无效');previous=BigInt(row.sequence);
  if(source==='hash'){if(typeof row.evidence_json!=='string')throw Error('事件原文证据无效');}
  else{if(!['pages_processed','items_processed','pending_count','request_budget','requests_used'].every(key=>historyCount(row[key]))||typeof row.current_directory!=='string'||typeof row.kind!=='string'||typeof row.last_error!=='string'||typeof row.restart_reason!=='string')throw Error('目录事件证据无效');if(source==='refresh'&&(!historyCount(row.request_budget_added)||!nullableUTC(row.retry_at_utc)||!nullableUTC(row.cloud_observed_at_utc)||![row.restart_offset,row.restart_fs_id,row.added_count,row.removed_count,row.changed_count].every(nullableCount)||(row.directory_changed!==null&&typeof row.directory_changed!=='boolean')))throw Error('刷新变更证据无效');}
  rows.push({...row,source});
 }
 if(data.has_more&&(!rows.length||data.next_before_sequence!==rows.at(-1)!.sequence||(before!=='0'&&BigInt(data.next_before_sequence)>=BigInt(before))))throw Error('事件游标未向前推进');return {rows,next:data.next_before_sequence,more:data.has_more};
}
export function historyEventsURL(source:HistorySource,run:string,before:string){const query=new URLSearchParams({identity_format:'string',limit:'20'});if(before!=='0')query.set('before_sequence',before);if(source==='hash')return historyAPIBase(source)+'/runs/'+run+'/events?'+query;query.set('run_id',run);return historyAPIBase(source)+'/events?'+query;}
