import type {CloudFileEntry} from './cloud-files-types';
import {historyCount,historyUTC} from './cloud-history-types';
import {validCloudDirectory} from './cloud-files-route';
export type PreviewExpected={fs_id:string;path:string;server_filename:string;size:string;isdir:number;server_mtime:string;md5:string};
export type PreviewRequest={request_id:string;phase:'sources'|'destination';operation:'move'|'copy';destination_directory:string;expected:PreviewExpected[];offset:number;expected_boundary:string};
export type PreviewRow={source_id:string;source_path:string;current_path:string;status:'matched'|'changed';observed_at_utc:string};
export type PreviewMatch={source_id:string;destination_path:string;conflict_id:string;conflict_path:string};
export type PreviewNameConflict={source_id:string;other_source_id:string;destination_path:string};
export type PreviewResult={started_at_utc:string;observed_at_utc:string;checked_count:number;rows:PreviewRow[];matches:PreviewMatch[];batch_conflicts:PreviewNameConflict[];next_offset:number|null;next_boundary:string};
export function previewExpected(item:CloudFileEntry):PreviewExpected{return {fs_id:item.fs_id,path:item.path,server_filename:item.name,size:item.provider_size,isdir:item.directory?1:0,server_mtime:item.server_mtime,md5:item.md5||''};}
export function previewTarget(directory:string,name:string){return (directory==='/'?'':directory)+'/'+name;}
// Normalize already validated UTC text without losing sub-millisecond evidence.
export function previewTime(value:string){return value.slice(0,19)+'.'+(value[19]==='.'?value.slice(20,-1):'').padEnd(9,'0')+'Z';}
export function parseBatchPreview(value:any,request:PreviewRequest):PreviewResult{
 const invalid=()=>{throw Error('在线预览响应与固定清单不一致；请重新核对。');};
 const expected=new Map(request.expected.map(e=>[e.fs_id,e]));
 if(!value||value.request_id!==request.request_id||value.phase!==request.phase||value.operation!==request.operation||value.directory!==request.destination_directory||value.offset!==request.offset||!Array.isArray(value.selection_ids)||value.selection_ids.length!==request.expected.length||value.selection_ids.some((id:unknown,i:number)=>id!==request.expected[i].fs_id)||!historyUTC(value.started_at_utc)||!historyUTC(value.observed_at_utc)||previewTime(value.observed_at_utc)<previewTime(value.started_at_utc)||!Number.isSafeInteger(value.checked_count)||value.checked_count<0||!Array.isArray(value.rows)||!Array.isArray(value.matches)||!Array.isArray(value.batch_conflicts))invalid();
 const rows:PreviewRow[]=[],matches:PreviewMatch[]=[],conflicts:PreviewNameConflict[]=[];
 if(request.phase==='sources'){
  if(value.checked_count!==request.expected.length||value.rows.length!==request.expected.length||value.matches.length||value.batch_conflicts.length||value.next_offset!==null||value.next_boundary!=='')invalid();
  const seen=new Set<string>();
  for(const row of value.rows){const item=expected.get(row?.source_id);if(!item||seen.has(row.source_id)||row.source_path!==item.path||!validCloudDirectory(row.current_path)||!['matched','changed'].includes(row.status)||row.status==='matched'&&row.current_path!==item.path||!historyUTC(row.observed_at_utc)||previewTime(row.observed_at_utc)<previewTime(value.started_at_utc)||previewTime(row.observed_at_utc)>previewTime(value.observed_at_utc))invalid();seen.add(row.source_id);rows.push({source_id:row.source_id,source_path:row.source_path,current_path:row.current_path,status:row.status,observed_at_utc:row.observed_at_utc});}
 }else{
  if(value.checked_count>100||value.rows.length||value.matches.length>expected.size||value.batch_conflicts.length>=expected.size)invalid();
  if(value.next_offset===null){if(value.next_boundary!=='')invalid();}else if(value.checked_count!==100||!Number.isSafeInteger(value.next_offset)||value.next_offset!==request.offset+100||value.next_offset>1000000||typeof value.next_boundary!=='string'||!/^[a-f0-9]{64}$/.test(value.next_boundary))invalid();
  const seen=new Set<string>();
  for(const match of value.matches){const item=expected.get(match?.source_id);if(!item||seen.has(match.source_id)||match.destination_path!==previewTarget(request.destination_directory,item.server_filename)||!historyCount(match.conflict_id,false)||!validCloudDirectory(match.conflict_path)||(match.conflict_path.slice(0,match.conflict_path.lastIndexOf('/'))||'/')!==request.destination_directory)invalid();seen.add(match.source_id);matches.push({source_id:match.source_id,destination_path:match.destination_path,conflict_id:match.conflict_id,conflict_path:match.conflict_path});}
  const paired=new Set<string>();
  for(const pair of value.batch_conflicts){const item=expected.get(pair?.source_id);if(!item||!expected.has(pair.other_source_id)||pair.source_id===pair.other_source_id||paired.has(pair.source_id)||pair.destination_path!==previewTarget(request.destination_directory,item.server_filename))invalid();paired.add(pair.source_id);conflicts.push({source_id:pair.source_id,other_source_id:pair.other_source_id,destination_path:pair.destination_path});}
 }
 return {started_at_utc:value.started_at_utc,observed_at_utc:value.observed_at_utc,checked_count:value.checked_count,rows,matches,batch_conflicts:conflicts,next_offset:value.next_offset,next_boundary:value.next_boundary};
}
