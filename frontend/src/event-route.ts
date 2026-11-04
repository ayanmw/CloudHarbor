export const emptyEventFilters = () => ({task_id:'',kind:'',operation_id:'',attempt_id:'',from_utc:'',through_utc:'',recorded_from_utc:'',recorded_before_utc:''});
export type EventFilters = ReturnType<typeof emptyEventFilters>;
export type EventRoute = {filters:EventFilters;asOf:string;before:string};
export function validEventID(value:string, zero=false) {
 return (zero && value==='0') || (/^[1-9][0-9]{0,18}$/.test(value) && (value.length<19 || value<='9223372036854775807'));
}
export function validDay(value:string) {
 if(!/^\d{4}-\d{2}-\d{2}$/.test(value))return false;
 const [y,m,d]=value.split('-').map(Number), days=[31,y%4===0&&(y%100!==0||y%400===0)?29:28,31,30,31,30,31,31,30,31,30,31];
 return m>=1&&m<=12&&d>=1&&d<=days[m-1];
}
function exactTime(value:string):string|null {
 const match=/^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d{1,9}))?Z$/.exec(value);
 if(!match||!validDay(match[1])||Number(match[2])>23||Number(match[3])>59||Number(match[4])>59)return null;
 const fraction=(match[5]||'').padEnd(9,'0');
 if(!fraction.endsWith('000'))return null;
 return value.slice(0,19)+'.'+fraction+'Z';
}
export function validateEventRoute(route:EventRoute,kinds:string[]):string {
 const f=route.filters;
 if(f.task_id&&!validEventID(f.task_id))return '任务 ID 必须是有效的正 int64 整数。';
 if(f.kind&&!kinds.includes(f.kind))return '事件类型无效。';
 for(const key of ['operation_id','attempt_id'] as const)if(f[key]&&(new Blob([f[key]]).size>128||f[key]!==f[key].trim()||/[\u0000-\u001f\u007f-\u009f]/.test(f[key])))return '关联 ID 最多 128 个 UTF-8 字节，前后不能有空格或控制字符。';
 if((f.from_utc||f.through_utc)&&(f.recorded_from_utc||f.recorded_before_utc))return '日期范围和精确 UTC 范围不能同时使用。';
 for(const key of ['from_utc','through_utc'] as const)if(f[key]&&!validDay(f[key]))return 'UTC 日期无效。';
 if(f.from_utc&&f.through_utc&&f.from_utc>f.through_utc)return 'UTC 起始日不能晚于截止日。';
 for(const key of ['recorded_from_utc','recorded_before_utc'] as const)if(f[key]&&!exactTime(f[key]))return '精确 UTC 必须使用 Z 结尾，且与后端一致对齐到微秒；不会自动舍入。';
 if(f.recorded_from_utc&&f.recorded_before_utc){
  const from=exactTime(f.recorded_from_utc)!,before=exactTime(f.recorded_before_utc)!;
  if(from>=before)return '精确 UTC 起点必须早于不含的截止点。';
 }
 if(route.asOf&&!validEventID(route.asOf,true))return '快照序列无效。';
 if(route.before&&(!validEventID(route.before)||!route.asOf||route.before.length>route.asOf.length||(route.before.length===route.asOf.length&&route.before>route.asOf)))return '翻页游标必须绑定有效快照且不超过快照序列。';
 return '';
}
export function readEventRoute(search:string,kinds:string[]) {
 const params=new URLSearchParams(search),filters=emptyEventFilters();
 const keys=[...Object.keys(filters),'as_of_sequence','before_sequence'];
 for(const key of Object.keys(filters) as (keyof EventFilters)[])filters[key]=params.get(key)||'';
 const route:EventRoute={filters,asOf:params.get('as_of_sequence')||'',before:params.get('before_sequence')||''};
 return {route,error:keys.some(key=>params.getAll(key).length>1)?'浏览条件不能重复。':validateEventRoute(route,kinds)};
}
export function eventRouteParams(route:EventRoute) {
 const params=new URLSearchParams();
 for(const [key,value] of Object.entries(route.filters))if(value)params.set(key,value);
 if(route.asOf)params.set('as_of_sequence',route.asOf);
 if(route.before)params.set('before_sequence',route.before);
 return params;
}
export function eventRouteURL(route:EventRoute) {const params=eventRouteParams(route);return '/task-events'+(params.size?'?'+params:'');}
