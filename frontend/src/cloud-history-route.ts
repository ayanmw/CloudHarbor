import {validDay} from './event-route';
import {historySources,historyStatuses,reasonLabels,historyCount,historyRunID,historyAPIBase,type HistorySource} from './cloud-history-types';
export const coverageLabels:Record<string,string>={unrecorded:'历史未记录',unknown:'未知',scanning:'扫描中',incomplete:'未完整',complete:'完整'};
export const emptyHistoryFilters=()=>({status:'',from:'',to:'',coverage:'',reason:'',min:'',max:''});
export type HistoryFilters=ReturnType<typeof emptyHistoryFilters>;
export type HistoryRoute={source:HistorySource;filters:HistoryFilters;before:string;run:string;eventBefore:string};
export function validateHistoryRoute(route:HistoryRoute){
 const f=route.filters;if(!Object.hasOwn(historySources,route.source))return '扫描历史类型无效。';
 if(f.status&&!Object.hasOwn(historyStatuses,f.status))return '历史状态筛选无效。';
 if((f.from&&!validDay(f.from))||(f.to&&!validDay(f.to))||(f.from&&f.to&&f.from>f.to))return 'UTC 日期范围无效。';
 if(route.source!=='scan'&&(f.coverage||f.reason||f.min||f.max))return '此类型不支持全盘覆盖筛选。';
 if((f.coverage&&!Object.hasOwn(coverageLabels,f.coverage))||(f.reason&&f.reason!=='unrecorded'&&!Object.hasOwn(reasonLabels,f.reason)))return '覆盖状态或原因无效。';
 if([f.min,f.max].some(value=>value&&(!historyCount(value)||BigInt(value)>1000000n))||(f.min&&f.max&&BigInt(f.min)>BigInt(f.max)))return '未完整目录数须为 0 至 1000000，且最小值不超过最大值。';
 if(!historyCount(route.before)||!historyCount(route.eventBefore)||(route.run&&!historyRunID(route.run))||(route.eventBefore!=='0'&&!route.run))return '运行 ID 或历史游标无效。';return '';
}
export function readHistoryRoute(search:string){
 const params=new URLSearchParams(search),rawSource=params.get('source')||'scan';const filters=emptyHistoryFilters();for(const key of Object.keys(filters) as (keyof HistoryFilters)[])filters[key]=params.get(key)||'';
 const route:HistoryRoute={source:(Object.hasOwn(historySources,rawSource)?rawSource:'scan') as HistorySource,filters,before:params.get('before')||'0',run:params.get('run')||'',eventBefore:params.get('event_before')||'0'};
 const keys=new Set(['source',...Object.keys(filters),'before','run','event_before']);let error='';params.forEach((_value,key)=>{if(!keys.has(key)||params.getAll(key).length!==1)error='历史查询参数无效或重复。';});
 if(!error&&!Object.hasOwn(historySources,rawSource))error='扫描历史类型无效。';return {route,error:error||validateHistoryRoute(route)};
}
export function historyRouteURL(route:HistoryRoute){
 const params=new URLSearchParams();if(route.source!=='scan')params.set('source',route.source);for(const [key,value] of Object.entries(route.filters))if(value)params.set(key,value);if(route.before!=='0')params.set('before',route.before);if(route.run)params.set('run',route.run);if(route.eventBefore!=='0')params.set('event_before',route.eventBefore);return '/cloud/history'+(params.size?'?'+params:'');
}
export function historyFilterQuery(filters:HistoryFilters){
 const query=new URLSearchParams();for(const [field,key] of Object.entries({status:'status',from:'started_from_utc',to:'started_through_utc',coverage:'coverage_status',reason:'coverage_reason',min:'incomplete_directories_min',max:'incomplete_directories_max'})){
  const value=filters[field as keyof HistoryFilters];if(value)query.set(key,value);
 }return query;
}
export function historyRunsURL(route:HistoryRoute){const query=historyFilterQuery(route.filters);query.set('identity_format','string');query.set('limit','20');if(route.before!=='0')query.set(route.source==='hash'?'before_start_sequence':'before_sequence',route.before);return historyAPIBase(route.source)+'/runs?'+query;}
export const historyRequestKey=(route:HistoryRoute)=>JSON.stringify([route.source,route.filters,route.before]);
export function historyFilterText(filters:HistoryFilters){const parts=[];if(filters.status)parts.push(historyStatuses[filters.status]||filters.status);if(filters.from||filters.to)parts.push((filters.from||'不限')+' 至 '+(filters.to||'不限')+' UTC');if(filters.coverage)parts.push('覆盖 '+coverageLabels[filters.coverage]);if(filters.reason)parts.push('原因 '+(filters.reason==='unrecorded'?'历史未记录':reasonLabels[filters.reason as keyof typeof reasonLabels]));if(filters.min||filters.max)parts.push('未完整目录 '+(filters.min||'0')+' 至 '+(filters.max||'不限'));return parts.join(' · ')||'全部本地记录';}
