import {validDay} from './event-route';
export const emptyMigrationFilters=()=>({status:'',q:'',started_from_utc:'',started_through_utc:''});
export type MigrationFilters=ReturnType<typeof emptyMigrationFilters>;
export type MigrationRoute={filters:MigrationFilters;offset:number};
export function validateMigrationRoute(route:MigrationRoute){
 const f=route.filters;
 if(f.status&&!['running','failed','completed'].includes(f.status))return '迁移状态无效。';
 if(new Blob([f.q]).size>256)return '路径关键词不能超过256个UTF-8字节。';
 if(/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/.test(f.q))return '路径关键词包含无效字符。';
 for(const key of ['started_from_utc','started_through_utc'] as const)if(f[key]&&!validDay(f[key]))return 'UTC日期无效。';
 if(f.started_from_utc&&f.started_through_utc&&f.started_from_utc>f.started_through_utc)return 'UTC日期起点不能晚于终点。';
 if(!Number.isSafeInteger(route.offset)||route.offset<0||route.offset>Number.MAX_SAFE_INTEGER-20||route.offset%20!==0)return '迁移历史页码无效。';
 return '';
}
export function readMigrationRoute(search:string){
 const params=new URLSearchParams(search),filters=emptyMigrationFilters();for(const key of Object.keys(filters) as (keyof MigrationFilters)[])filters[key]=params.get(key)||'';
 const offset=params.get('offset')||'0',route={filters,offset:Number(offset)};
 const error=[...Object.keys(filters),'offset'].some(key=>params.getAll(key).length>1)?'迁移查询条件不能重复。':! /^(0|[1-9][0-9]*)$/.test(offset)?'迁移历史页码无效。':validateMigrationRoute(route);
 return {route,error};
}
export function migrationRouteURL(route:MigrationRoute){
 const params=new URLSearchParams();for(const [key,value] of Object.entries(route.filters))if(value)params.set(key,value);if(route.offset)params.set('offset',String(route.offset));return '/settings/migrations'+(params.size?'?'+params:'');
}
