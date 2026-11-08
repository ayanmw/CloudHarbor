import {operationUTC,validOperationID} from './operation-route';

export type CloudDisplaySizeInfo={
 snapshot_id:string;mixed:boolean;cache_generation:string;unknown_directories:number;
 remote_read_started_at_utc:string;remote_observed_at_utc:string;cache_read_at_utc:string;
 created_at_utc:string;expires_at_utc:string;read_at_utc:string;
};

// Validate server evidence without comparing it with the browser's wall clock.
// UTC fields describe separate observations, not an atomic remote filesystem view.
export function validDisplaySizeListing(value:any,offset:number,requestedSnapshot:string,mixed=true):value is {display_size:CloudDisplaySizeInfo}&Record<string,any>{
 const info=value?.display_size;
 if(!info||info.mixed!==mixed||!validOperationID(info.snapshot_id)||(requestedSnapshot&&info.snapshot_id!==requestedSnapshot))return false;
 if(typeof info.cache_generation!=='string'||!/^(0|[1-9][0-9]*)$/.test(info.cache_generation)||info.cache_generation.length>19||BigInt(info.cache_generation)>9223372036854775807n)return false;
 for(const key of ['remote_read_started_at_utc','remote_observed_at_utc','cache_read_at_utc','created_at_utc','expires_at_utc','read_at_utc'])if(typeof info[key]!=='string'||!operationUTC(info[key]))return false;
 if(operationUTC(info.expires_at_utc)!<=operationUTC(info.created_at_utc)!)return false;
 if(value.observed_at_utc!==info.remote_observed_at_utc||!Number.isSafeInteger(value.total)||value.total<0||!Number.isSafeInteger(info.unknown_directories)||info.unknown_directories<0||info.unknown_directories>value.total)return false;
 if(value.offset!==offset||!Array.isArray(value.entries)||value.entries.length!==Math.min(100,Math.max(0,value.total-offset)))return false;
 return (value.next_offset??null)===(value.total-offset>100?offset+100:null);
}
