export type CloudSearchLocation={id:string;path:string;directory:boolean;returnSearch:string};
export type CloudSearchExtra={extensions:string;modifiedFrom:string;modifiedBefore:string};
export const emptyCloudSearchExtra=():CloudSearchExtra=>({extensions:'',modifiedFrom:'',modifiedBefore:''});
export type CloudFilesRoute={directory:string;source:'live'|'cache';query:string;offset:number;generation:string;boundary:string;sizeSnapshot?:string;mixed?:boolean;order?:'name'|'size'|'time';descending?:boolean;scope?:string;kind?:'all'|'file'|'directory';location?:CloudSearchLocation;extra?:CloudSearchExtra};
const locationKeys=['focus_id','focus_path','focus_directory','return_search'];
const extraKeys=['extensions','modified_from','modified_before'];
export function validCloudSearchExtra(extra:CloudSearchExtra,kind:string){
 if(extra.extensions&&(!/^[a-z0-9]{1,16}(,[a-z0-9]{1,16}){0,9}$/.test(extra.extensions)||kind==='directory'||new Set(extra.extensions.split(',')).size!==extra.extensions.split(',').length))return false;
 for(const value of [extra.modifiedFrom,extra.modifiedBefore])if(value&&(!/^[1-9][0-9]{0,11}$/.test(value)||Number(value)>253402300800))return false;
 return (!extra.modifiedFrom||Number(extra.modifiedFrom)<253402300800)&&(!extra.modifiedFrom||!extra.modifiedBefore||Number(extra.modifiedFrom)<Number(extra.modifiedBefore));
}
export function cloudSearchUTCInput(seconds:string){
 if(!seconds)return '';
 const d=new Date(Number(seconds)*1000),pad=(n:number)=>String(n).padStart(2,'0');
 return String(d.getUTCFullYear()).padStart(4,'0')+'-'+pad(d.getUTCMonth()+1)+'-'+pad(d.getUTCDate())+'T'+pad(d.getUTCHours())+':'+pad(d.getUTCMinutes())+':'+pad(d.getUTCSeconds());
}
export function cloudSearchUTCSeconds(value:string):string|null{
 if(!value)return '';
 const match=/^(\d{4,5})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(value);if(!match)return null;
 const [,year,month,day,hour,minute,second='00']=match;
 const millis=Date.UTC(Number(year),Number(month)-1,Number(day),Number(hour),Number(minute),Number(second));
 if(!Number.isFinite(millis)||millis<=0||millis/1000>253402300800)return null;
 const seconds=String(millis/1000);return cloudSearchUTCInput(seconds)===value+(match[6]===undefined?':00':'')?seconds:null;
}
export const validCloudDirectory=(value:string)=>value==='/'||value.startsWith('/')&&!/[\\\u0000-\u001f\u007f]/.test(value)&&value.slice(1).split('/').every(part=>part!==''&&part!=='.'&&part!=='..')&&new Blob([value]).size<=2048;
export function readCloudFilesRoute(search:string){
 const params=new URLSearchParams(search),source=params.get('source')||'live',offset=params.get('offset')||'0';
 const route:CloudFilesRoute={directory:params.get('dir')||'/',source:source as 'live'|'cache',query:params.get('q')||'',offset:Number(offset),generation:params.get('generation')||'',boundary:params.get('boundary')||'',order:(params.get('order')||'name') as 'name'|'size'|'time',descending:params.get('desc')==='1',scope:params.get('scope')??'/',kind:(params.get('kind')??'all') as 'all'|'file'|'directory'};
 route.extra={extensions:params.get('extensions')??'',modifiedFrom:params.get('modified_from')??'',modifiedBefore:params.get('modified_before')??''};
 let error='';
 if(['dir','source','q','offset','generation','boundary','order','desc','size_snapshot','mixed','scope','kind',...locationKeys,...extraKeys].some(key=>params.getAll(key).length>1))error='目录浏览参数不能重复。';
 else if(!validCloudSearchExtra(route.extra,route.kind!)||extraKeys.some(key=>params.has(key)&&(!params.get(key)||!route.query)))error='扩展名或修改时间参数无效。';
 else if(!validCloudDirectory(route.scope!)||!['all','file','directory'].includes(route.kind!)||((params.has('scope')||params.has('kind'))&&!route.query))error='搜索范围或类型参数无效。';
 else if(!['name','size','time'].includes(params.get('order')??'name')||!['0','1'].includes(params.get('desc')??'0'))error='目录排序参数无效。';
 else if(!validCloudDirectory(route.directory)||!['live','cache'].includes(source)||! /^(0|[1-9][0-9]*)$/.test(offset)||!Number.isSafeInteger(route.offset)||route.offset>1000000)error='目录浏览参数无效。';
 else if(route.query&&(source!=='cache'||route.query.trim()!==route.query||Array.from(route.query).length>128))error='缓存搜索条件无效。';
 else if(route.generation&&(! /^(0|[1-9][0-9]*)$/.test(route.generation)||route.generation.length>19||BigInt(route.generation)>9223372036854775807n))error='缓存代次无效。';
 else if(route.boundary&&!/^[a-f0-9]{64}$/.test(route.boundary))error='分页边界无效。';
 else if(params.has('size_snapshot')&&(!/^[a-f0-9]{32}$/.test(params.get('size_snapshot')||'')||source!=='live'||!!route.query))error='大小排序快照参数无效。';
 if(!error&&params.has('mixed')){if(!['0','1'].includes(params.get('mixed')||'')||source!=='live'||!!route.query)error='混合排序参数无效。';else route.mixed=params.get('mixed')==='1';}
 if(!error&&params.has('size_snapshot'))route.sizeSnapshot=params.get('size_snapshot')!;
 if(!error&&locationKeys.some(key=>params.has(key))){
  const id=params.get('focus_id')||'',path=params.get('focus_path')||'',isDirectory=params.get('focus_directory'),returnSearch=validCloudSearchReturn(params.get('return_search')||'');
  if(route.source!=='live'||route.query||!validCloudDirectory(path)||path==='/'||(path.slice(0,path.lastIndexOf('/'))||'/')!==route.directory||! /^[1-9][0-9]{0,18}$/.test(id)||BigInt(id)>9223372036854775807n||!['0','1'].includes(isDirectory||'')||!returnSearch)error='搜索定位或返回参数无效。';
  else route.location={id,path,directory:isDirectory==='1',returnSearch};
 }
 return {route,error};
}
export function cloudFilesRouteURL(route:CloudFilesRoute){
 const params=new URLSearchParams();if(route.directory!=='/')params.set('dir',route.directory);if(route.source!=='live')params.set('source',route.source);if(route.query){params.set('q',route.query);if(route.scope&&route.scope!=='/')params.set('scope',route.scope);if(route.kind&&route.kind!=='all')params.set('kind',route.kind);if(route.extra?.extensions)params.set('extensions',route.extra.extensions);if(route.extra?.modifiedFrom)params.set('modified_from',route.extra.modifiedFrom);if(route.extra?.modifiedBefore)params.set('modified_before',route.extra.modifiedBefore);}if(route.source==='live'){if(route.order&&route.order!=='name')params.set('order',route.order);if(route.descending)params.set('desc','1');}if(route.source==='live'&&route.sizeSnapshot)params.set('size_snapshot',route.sizeSnapshot);if(route.source==='live'&&route.mixed!==undefined)params.set('mixed',route.mixed?'1':'0');if(route.offset){params.set('offset',String(route.offset));if(route.source==='cache'&&route.generation)params.set('generation',route.generation);if(route.source==='live'&&route.boundary)params.set('boundary',route.boundary);}if(route.location){params.set('focus_id',route.location.id);params.set('focus_path',route.location.path);params.set('focus_directory',route.location.directory?'1':'0');params.set('return_search',route.location.returnSearch);}return '/cloud/files'+(params.size?'?'+params:'');
}

// Reject nesting before parsing: a saved search can never contain another return.
export function validCloudSearchReturn(value:string):string{
 if(value.length>32768||!value.startsWith('/cloud/files?')||value.includes('#'))return '';
 const search=value.slice('/cloud/files'.length),params=new URLSearchParams(search);
 if(locationKeys.some(key=>params.has(key)))return '';
 const {route,error}=readCloudFilesRoute(search);
 return !error&&route.source==='cache'&&!!route.query?cloudFilesRouteURL(route):'';
}
