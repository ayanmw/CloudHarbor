import {validDay,validEventID} from './event-route';
export const emptyMonitorFilters=()=>({status:'',started_from_utc:'',started_through_utc:''});
export type MonitorFilters=ReturnType<typeof emptyMonitorFilters>;
export type MonitorRoute={filters:MonitorFilters;before:string;run:string};
export function validateMonitorRoute(route:MonitorRoute):string {
 const f=route.filters;
 if(f.status&&!['running','success','failed','interrupted'].includes(f.status))return '监控状态无效。';
 for(const key of ['started_from_utc','started_through_utc'] as const)if(f[key]&&!validDay(f[key]))return 'UTC 日期格式无效。';
 if(f.started_from_utc&&f.started_through_utc&&f.started_from_utc>f.started_through_utc)return 'UTC 日期起点不能晚于终点。';
 if(route.before&&!validEventID(route.before))return '监控历史游标无效。';
 if(route.run&&!/^[a-f0-9]{32}$/.test(route.run))return '扫描运行 ID 无效。';
 return '';
}
export function readMonitorRoute(search:string){
 const params=new URLSearchParams(search),filters=emptyMonitorFilters();
 for(const key of Object.keys(filters) as (keyof MonitorFilters)[])filters[key]=params.get(key)||'';
 const route:MonitorRoute={filters,before:params.get('before_sequence')||'',run:params.get('run_id')||''};
 const duplicate=[...Object.keys(filters),'before_sequence','run_id'].some(key=>params.getAll(key).length>1);
 return {route,error:duplicate?'监控浏览条件不能重复。':validateMonitorRoute(route)};
}
export function monitorRouteURL(route:MonitorRoute){
 const params=new URLSearchParams();for(const [key,value] of Object.entries(route.filters))if(value)params.set(key,value);
 if(route.before)params.set('before_sequence',route.before);if(route.run)params.set('run_id',route.run);
 return '/monitor-history'+(params.size?'?'+params:'');
}
