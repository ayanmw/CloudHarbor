export type CloudFilesRoute={directory:string;source:'live'|'cache';query:string;offset:number;generation:string;boundary:string};
export const validCloudDirectory=(value:string)=>value==='/'||value.startsWith('/')&&!/[\\\u0000-\u001f\u007f]/.test(value)&&value.slice(1).split('/').every(part=>part!==''&&part!=='.'&&part!=='..')&&new Blob([value]).size<=2048;
export function readCloudFilesRoute(search:string){
 const params=new URLSearchParams(search),source=params.get('source')||'live',offset=params.get('offset')||'0';
 const route:CloudFilesRoute={directory:params.get('dir')||'/',source:source as 'live'|'cache',query:params.get('q')||'',offset:Number(offset),generation:params.get('generation')||'',boundary:params.get('boundary')||''};
 let error='';
 if(['dir','source','q','offset','generation','boundary'].some(key=>params.getAll(key).length>1))error='目录浏览参数不能重复。';
 else if(!validCloudDirectory(route.directory)||!['live','cache'].includes(source)||! /^(0|[1-9][0-9]*)$/.test(offset)||!Number.isSafeInteger(route.offset)||route.offset>1000000)error='目录浏览参数无效。';
 else if(route.query&&(source!=='cache'||route.query.trim()!==route.query||Array.from(route.query).length>128))error='缓存搜索条件无效。';
 else if(route.generation&&(! /^(0|[1-9][0-9]*)$/.test(route.generation)||route.generation.length>19||BigInt(route.generation)>9223372036854775807n))error='缓存代次无效。';
 else if(route.boundary&&!/^[a-f0-9]{64}$/.test(route.boundary))error='分页边界无效。';
 return {route,error};
}
export function cloudFilesRouteURL(route:CloudFilesRoute){
 const params=new URLSearchParams();if(route.directory!=='/')params.set('dir',route.directory);if(route.source!=='live')params.set('source',route.source);if(route.query)params.set('q',route.query);if(route.offset){params.set('offset',String(route.offset));if(route.source==='cache'&&route.generation)params.set('generation',route.generation);if(route.source==='live'&&route.boundary)params.set('boundary',route.boundary);}return '/cloud/files'+(params.size?'?'+params:'');
}
