import {validDay} from './event-route';
export const operationKinds:Record<string,string>={mkdir:'新建目录',move:'移动',rename:'重命名',delete:'删除'};
export const operationStatuses:Record<string,string>={prepared:'待确认',confirmed:'已确认',unknown:'结果未知（隔离）',failed:'失败'};
export const emptyOperationFilters=()=>({status:'',kind:'',q:'',from:'',to:''});
export type OperationFilters=ReturnType<typeof emptyOperationFilters>;
export type OperationCursor={created_at_utc:string;operation_id:string};
export type OperationRoute={filters:OperationFilters;before:OperationCursor|null;operation:string};
export function validOperationID(value:unknown):value is string {return typeof value==='string'&&/^[a-f0-9]{32}$/.test(value);}
// Compare canonical UTC strings without routing nanoseconds through Date/Number.
export function operationUTC(value:string):string|null {
 const match=/^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?Z$/.exec(value);
 if(!match||!validDay(match[1])||Number(match[2])>23||Number(match[3])>59||Number(match[4])>59)return null;
 const canonical=value.slice(0,19)+'.'+(match[5]||'').padEnd(9,'0')+'Z';
 if(canonical<'1677-09-21T00:12:43.145224192Z'||canonical>'2262-04-11T23:47:16.854775807Z')return null;
 return canonical;
}
export function validateOperationFilters(filters:OperationFilters):string {
 if(filters.status&&!Object.keys(operationStatuses).includes(filters.status))return '操作状态无效。';
 if(filters.kind&&!Object.keys(operationKinds).includes(filters.kind))return '操作类型无效。';
 if(Array.from(filters.q.trim()).length>128)return '关键字最多 128 个字符。';
 const from=filters.from?operationUTC(filters.from):null,to=filters.to?operationUTC(filters.to):null;
 if((filters.from&&!from)||(filters.to&&!to))return '时间须为有效 UTC（Z 结尾），最多 9 位小数，且在 int64 纳秒范围内；不会自动舍入。';
 if(from&&to&&from>to)return '创建时间起点不能晚于终点（两端均包含）。';
 return '';
}
export function validOperationCursor(value:unknown):value is OperationCursor {
 if(!value||typeof value!=='object')return false;
 const cursor=value as OperationCursor;
 return typeof cursor.created_at_utc==='string'&&!!operationUTC(cursor.created_at_utc)&&validOperationID(cursor.operation_id);
}
export function olderOperationKey(a:OperationCursor,b:OperationCursor):boolean {
 const at=operationUTC(a.created_at_utc),bt=operationUTC(b.created_at_utc);
 return !!at&&!!bt&&(at<bt||(at===bt&&a.operation_id<b.operation_id));
}
export function readOperationRoute(search:string) {
 const params=new URLSearchParams(search),filters=emptyOperationFilters();
 for(const key of Object.keys(filters) as (keyof OperationFilters)[])filters[key]=params.get(key)||'';
 filters.q=filters.q.trim();
 const before=params.has('before_created_at_utc')||params.has('before_operation_id')?{created_at_utc:params.get('before_created_at_utc')||'',operation_id:params.get('before_operation_id')||''}:null;
 const route:OperationRoute={filters,before,operation:params.get('operation_id')||''};
 const duplicate=[...Object.keys(filters),'before_created_at_utc','before_operation_id','operation_id'].some(key=>params.getAll(key).length>1);
 const error=duplicate?'操作浏览条件不能重复。':validateOperationFilters(filters)||(before&&!validOperationCursor(before)?'组合游标必须包含有效创建UTC和操作ID。':'')||(route.operation&&!validOperationID(route.operation)?'操作ID无效。':'');
 return {route,error};
}
export function operationParams(route:OperationRoute){
 const params=new URLSearchParams();for(const [key,value] of Object.entries(route.filters))if(value)params.set(key,value);
 if(route.before){params.set('before_created_at_utc',route.before.created_at_utc);params.set('before_operation_id',route.before.operation_id);}
 return params;
}
export function operationRouteURL(route:OperationRoute){const params=operationParams(route);if(route.operation)params.set('operation_id',route.operation);return '/operations'+(params.size?'?'+params:'');}
