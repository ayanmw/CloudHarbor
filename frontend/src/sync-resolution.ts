export const resolutionChoices:Record<string,string>={keep_remote:'保留网盘版本',keep_local:'保留本地版本',keep_both:'保留两份',skip:'暂不处理'};
export const resolutionStates:Record<string,string>={preview:'历史预览，尚未执行',deferred:'暂不处理',unresolved:'继续保留未解决冲突',review_required:'目录处理待审阅'};
type Entry={path:string;id:string;directory:boolean;size:string;sha256?:string;observed_at_utc:string};
export const subtreeActions:Record<string,string>={keep:'保留',mkdir:'新建目录',download:'下载',upload:'上传',backup_then_download:'备份后下载',backup_then_upload:'备份后上传',backup_then_remove:'备份后移除',remove_directory_after_children:'处理子项后移除空目录',backup_then_replace_type:'备份后替换项目类型',preserve_local_copy:'保留到本地副本',preserve_copy_then_restore_remote:'保留本地副本，再恢复网盘项',restore_remote:'恢复网盘项到本地',upload_copy:'上传本地副本',create_copy_directory:'建立网盘副本目录'};
export type SubtreeReview={plan_version?:string;plan_step_count?:string;paged?:boolean;item_count?:string;version:string;root:string;local_files:string;remote_files:string;local_bytes:string;remote_bytes:string;note:string;items:{path:string;local:Entry|null;remote:Entry|null;local_action:string;remote_action:string;local_target?:string;remote_target?:string;restore_local_target?:string}[]};
export type Resolution={subtree_review?:SubtreeReview;id:string;source_run:string;conflict_event_sequence:string;snapshot_event_sequence:string;snapshot_recorded_at_utc:string;snapshot_evidence_sha256:string;source_recorded_at_utc:string;created_at_utc:string;source_config:{id:string;mode:'pull'|'both';local_root:string;remote_root:string;account_scope:string};source_conflict:{ordinal:number;status:string;plan:{action:string;path:string;local:Entry|null;remote:Entry|null}};choice:string;state:string;note:string;local_copy_path?:string;effects:{side:string;action:string;path:string;from?:string;backup_required:boolean}[]};
const id=(x:unknown)=>typeof x==='string'&&/^[a-f0-9]{32}$/.test(x);
const count=(x:unknown)=>typeof x==='string'&&/^[1-9][0-9]*$/.test(x);
const utc=(x:unknown)=>typeof x==='string'&&x.endsWith('Z')&&Number.isFinite(Date.parse(x));
const hash=(x:unknown)=>typeof x==='string'&&/^[a-f0-9]{64}$/.test(x);
const relative=(x:unknown)=>typeof x==='string'&&!!x&&!/[\\:\x00]/.test(x)&&x.split('/').every(p=>!!p&&p!=='.'&&p!=='..');
function entry(x:any){return x===null||x&&relative(x.path)&&typeof x.id==='string'&&!!x.id&&typeof x.directory==='boolean'&&typeof x.size==='string'&&/^(0|[1-9][0-9]*)$/.test(x.size)&&(x.directory||x.sha256===undefined||hash(x.sha256));}
export function parseResolution(x:any,run:string,ordinal:number,path:string):Resolution{
 if(!x||!id(x.id)||x.source_run!==run||!count(x.conflict_event_sequence)||!count(x.snapshot_event_sequence)||BigInt(x.snapshot_event_sequence)>=BigInt(x.conflict_event_sequence)||!hash(x.snapshot_evidence_sha256)||![x.snapshot_recorded_at_utc,x.source_recorded_at_utc,x.created_at_utc].every(utc)||!Object.hasOwn(resolutionChoices,x.choice)||!Object.hasOwn(resolutionStates,x.state)||typeof x.note!=='string')throw Error('决策身份或时间证据无效');
 const c=x.source_config,s=x.source_conflict;
 // Step uses omitempty for absent endpoint pointers. This is a known wire
 // representation of nil, distinct from missing traversal snapshot arrays.
 if(s?.plan){if(!Object.hasOwn(s.plan,'local'))s.plan.local=null;if(!Object.hasOwn(s.plan,'remote'))s.plan.remote=null;}
 if(!c||!id(c.id)||!['pull','both'].includes(c.mode)||typeof c.account_scope!=='string'||!c.account_scope||typeof c.local_root!=='string'||!c.local_root||typeof c.remote_root!=='string'||!c.remote_root.startsWith('/')||!s||s.ordinal!==ordinal||s.status!=='conflict'||s.plan?.action!=='conflict'||s.plan.path!==path||!relative(path)||!entry(s.plan.local)||!entry(s.plan.remote)||!Array.isArray(x.effects)||x.effects.length>3)throw Error('决策来源范围无效');
 const effects=x.effects;
 if(effects.some((e:any)=>!e||!['local','remote'].includes(e.side)||!['move','download','upload'].includes(e.action)||!relative(e.path)||(e.from!==undefined&&!relative(e.from))||typeof e.backup_required!=='boolean'))throw Error('决策操作证据无效');
 let expected:any[]=[];
 const effect=(side:string,action:string,p=path,from='',backup=false)=>({side,action,path:p,from,backup_required:backup});
 const simple=s.plan.local&&!s.plan.local.directory&&s.plan.remote&&!s.plan.remote.directory;
 if(x.choice==='skip'){if(x.state!=='deferred')throw Error('暂不处理状态无效')}
 else if(x.choice==='keep_local'&&c.mode==='pull'){if(x.state!=='unresolved')throw Error('单向同步不能隐式上传')}
 else if(!simple){if(x.state!=='review_required')throw Error('目录影响尚未展开')}
 else {
  if(x.state!=='preview'||!hash(s.plan.local.sha256)||!hash(s.plan.remote.sha256))throw Error('文件决策状态或内容摘要无效');
  if(x.choice==='keep_remote')expected=[effect('local','download',path,'',true)];
  if(x.choice==='keep_local')expected=[effect('remote','upload',path,'',true)];
  if(x.choice==='keep_both'){if(!relative(x.local_copy_path)||x.local_copy_path===path)throw Error('固定副本路径无效');expected=[effect('local','move',x.local_copy_path,path),effect('local','download')];if(c.mode==='both')expected.push(effect('remote','upload',x.local_copy_path,x.local_copy_path))}
 }
 if(effects.length!==expected.length||effects.some((e:any,i:number)=>Object.entries(expected[i]).some(([k,v])=>(k==='from'?(e[k]||''):e[k])!==v)))throw Error('决策操作与同步方向或选择不一致');
 if(x.subtree_review!==undefined){
  const review=x.subtree_review,number=(n:unknown)=>typeof n==='string'&&/^(0|[1-9][0-9]*)$/.test(n);
  if(!review||review.version!=='subtree-review-v1'||review.root!==path||typeof review.note!=='string'||!['local_files','remote_files','local_bytes','remote_bytes'].every(k=>number(review[k]))||!Array.isArray(review.items)||x.local_copy_path!==undefined&&!relative(x.local_copy_path))throw Error('子树影响范围或统计无效');
  if(review.plan_version!==undefined||review.plan_step_count!==undefined){if(review.plan_version!=='subtree-steps-v1'||!number(review.plan_step_count))throw Error('有序步骤元数据无效')}
  if(review.paged===true){if(review.items.length||!count(review.item_count))throw Error('分页子树摘要无效')}
  else{if(review.paged!==undefined&&review.paged!==false)throw Error('子树分页标记无效');const stats=validateSubtreeItems(review.items,x,true);if(String(stats.lf)!==review.local_files||String(stats.rf)!==review.remote_files||String(stats.lb)!==review.local_bytes||String(stats.rb)!==review.remote_bytes)throw Error('子树容量统计与项目不一致');}
 }

 return x;
}

function validateSubtreeItems(items:any[],resolution:Resolution,requireRoot:boolean){
 const path=resolution.source_conflict.plan.path,within=(p:string,root:string)=>p===root||p.startsWith(root+'/'),seen=new Set<string>();let lf=0,rf=0,lb=0n,rb=0n;
 for(const item of items){
  if(!item||!relative(item.path)||!within(item.path,path)||seen.has(item.path.toLowerCase())||!entry(item.local)||!entry(item.remote)||!item.local&&!item.remote||!Object.hasOwn(subtreeActions,item.local_action)||!Object.hasOwn(subtreeActions,item.remote_action))throw Error('子树影响项目证据无效');seen.add(item.path.toLowerCase());
  for(const side of ['local','remote']){const e=item[side];if(e&&e.path!==item.path)throw Error('子树对象路径不一致')}
  for(const target of [item.local_target,item.remote_target,item.restore_local_target])if(target!==undefined&&(!relative(target)||!within(target,path)&&!(resolution.local_copy_path&&within(target,resolution.local_copy_path))))throw Error('子树目标超出固定范围');
  if(item.local&&!item.local.directory){lf++;lb+=BigInt(item.local.size)}if(item.remote&&!item.remote.directory){rf++;rb+=BigInt(item.remote.size)}
 }
 const root=items.find(item=>item.path===path);if(requireRoot&&(!root||root.local?.id!==resolution.source_conflict.plan.local?.id||root.remote?.id!==resolution.source_conflict.plan.remote?.id))throw Error('子树根身份与冲突不一致');
 return {lf,rf,lb,rb};
}
export function parseSubtreePage(data:any,resolution:Resolution,offset:number):SubtreeReview['items']{
 const review=resolution.subtree_review;
 if(!review?.paged||data?.decision_id!==resolution.id||data.root!==review.root||data.total!==review.item_count||data.offset!==offset||data.limit!==100||typeof data.has_more!=='boolean'||!Array.isArray(data.items)||data.items.length>100||!Number.isSafeInteger(offset)||offset<0)throw Error('子项分页身份或范围无效');
 const remaining=BigInt(data.total)-BigInt(offset),expected=remaining<0n?0:remaining>100n?100:Number(remaining);
 if(data.items.length!==expected||data.has_more!==(BigInt(offset)+BigInt(data.items.length)<BigInt(data.total)))throw Error('子项分页数量证据无效');
 validateSubtreeItems(data.items,resolution,offset===0);return data.items;
}
