import {historyCount,historyRunID,historySigned,historyUTC} from './cloud-history-types';
import {validCloudDirectory} from './cloud-files-route';
export const changeKinds:Record<string,string>={added:'新增',removed:'删除',metadata_changed:'元数据变化',identity_changed:'身份变化'};
export type ChangeItem={fs_id:string;path:string;name:string;directory:boolean;size:string;provider_mtime:string;observed_at_utc:string|null;content_md5?:string;content_sha256?:string;content_sha256_verified_at_utc?:string|null};
export type RefreshChange={sequence:string;ordinal:string;kind:string;old:ChangeItem|null;new:ChangeItem|null;recorded_at_utc:string;cloud_observed_at_utc:string};
export type ChangeFilters={kind:string;query:string};
export type ChangePage={changes:RefreshChange[];counts:Record<string,string>;matched:string;next:string;more:boolean};
const nullableTime=(value:unknown)=>value===null||historyUTC(value);
function item(value:any):value is ChangeItem{return value&&historyCount(value.fs_id,false)&&typeof value.path==='string'&&value.path!=='/'&&validCloudDirectory(value.path)&&typeof value.name==='string'&&!!value.name&&typeof value.directory==='boolean'&&historyCount(value.size)&&historySigned(value.provider_mtime)&&nullableTime(value.observed_at_utc)&&(value.content_md5===undefined||typeof value.content_md5==='string'&&/^(?:[a-fA-F0-9]{32})?$/.test(value.content_md5))&&(value.content_sha256===undefined||typeof value.content_sha256==='string'&&/^(?:[a-fA-F0-9]{64})?$/.test(value.content_sha256))&&(value.content_sha256_verified_at_utc===undefined||nullableTime(value.content_sha256_verified_at_utc));}
export function parseChangePage(data:any,run:string,event:string,filters:ChangeFilters,before='0',limit=50):ChangePage{
 if(!historyRunID(run)||!historyCount(event,false)||!data||data.run_id!==run||data.event_sequence!==event||data.kind!==filters.kind||data.query!==filters.query||!Array.isArray(data.changes)||data.changes.length>limit||typeof data.has_more!=='boolean'||!historyCount(data.next_before_sequence)||!historyCount(data.total_count)||!historyCount(data.matched_count)||!data.counts||!['total',...Object.keys(changeKinds)].every(k=>historyCount(data.counts[k])))throw Error('变更身份、筛选或分页响应无效。');
 const counts=data.counts as Record<string,string>;
 if(counts.total!==data.total_count||Object.keys(changeKinds).reduce((n,k)=>n+BigInt(counts[k]),0n)!==BigInt(counts.total)||BigInt(data.matched_count)>BigInt(counts.total)||BigInt(data.changes.length)>BigInt(data.matched_count))throw Error('变更计数响应无效。');
 let previous=before==='0'?9223372036854775808n:BigInt(before);
 for(const row of data.changes){
  if(!row||!historyCount(row.sequence,false)||BigInt(row.sequence)>=previous||!historyCount(row.ordinal)||!Object.hasOwn(changeKinds,row.kind)||(filters.kind&&row.kind!==filters.kind)||!historyUTC(row.recorded_at_utc)||!historyUTC(row.cloud_observed_at_utc))throw Error('变更序号、类型或时间响应无效。');
  previous=BigInt(row.sequence);
  if((row.old!==null&&!item(row.old))||(row.new!==null&&!item(row.new))||(row.kind==='added'?row.old!==null||row.new===null:row.kind==='removed'?row.old===null||row.new!==null:row.old===null||row.new===null))throw Error('变更前后身份响应无效。');
 }
 if(data.has_more&&(!data.changes.length||data.next_before_sequence!==data.changes.at(-1).sequence||(before!=='0'&&BigInt(data.next_before_sequence)>=BigInt(before))))throw Error('变更游标未向前推进。');
 return {changes:data.changes,counts,matched:data.matched_count,next:data.next_before_sequence,more:data.has_more};
}
export function changesURL(run:string,event:string,filters:ChangeFilters,before='0',exporting=false){const q=new URLSearchParams({identity_format:'string',run_id:run,event_sequence:event});if(!exporting){q.set('limit','50');if(before!=='0')q.set('before_sequence',before)}if(filters.kind)q.set('kind',filters.kind);if(filters.query)q.set('q',filters.query);return '/api/cloud-inventory/refresh/changes'+(exporting?'/export':'')+'?'+q;}
