import type {Resolution} from './sync-resolution';
export type BatchItem={decision_id:string;conflict_ordinal:number;root:string;choice:string;copy_path?:string;snapshot_evidence_sha256:string;snapshot_recorded_at_utc:string;step_count:string};
export type Batch={version:string;id:string;source_run:string;source_config:Resolution['source_config'];created_at_utc:string;state:string;step_count:string;items:BatchItem[]};
const id=(x:unknown)=>typeof x==='string'&&/^[a-f0-9]{32}$/.test(x),number=(x:unknown)=>typeof x==='string'&&/^[1-9][0-9]*$/.test(x),utc=(x:unknown)=>typeof x==='string'&&x.endsWith('Z')&&!x.startsWith('0001-')&&Number.isFinite(Date.parse(x));
const relative=(p:unknown):p is string=>typeof p==='string'&&!!p&&!/[\\:\x00]/.test(p)&&p.split('/').every(x=>!!x&&x!=='.'&&x!=='..');
export function parseBatch(x:any,run:string,selected?:Resolution[]):Batch{
 const c=x?.source_config;
 if(!x||x.version!=='resolution-batch-v1'||!id(x.id)||x.source_run!==run||x.state!=='preview'||!utc(x.created_at_utc)||!number(x.step_count)||!c||!id(c.id)||!['pull','both'].includes(c.mode)||typeof c.local_root!=='string'||!c.local_root||typeof c.remote_root!=='string'||!c.remote_root.startsWith('/')||typeof c.account_scope!=='string'||!c.account_scope||!Array.isArray(x.items)||x.items.length<1||x.items.length>20)throw Error('批次身份、配置或范围无效');
 const ids=new Set<string>(),paths:string[]=[];let steps=0n;
 for(const item of x.items){
  if(!item||!id(item.decision_id)||ids.has(item.decision_id)||!Number.isSafeInteger(item.conflict_ordinal)||item.conflict_ordinal<1||!relative(item.root)||!['keep_local','keep_remote','keep_both'].includes(item.choice)||c.mode==='pull'&&item.choice==='keep_local'||!number(item.step_count)||typeof item.snapshot_evidence_sha256!=='string'||!/^[a-f0-9]{64}$/.test(item.snapshot_evidence_sha256)||!utc(item.snapshot_recorded_at_utc))throw Error('批次条目证据无效');
  ids.add(item.decision_id);steps+=BigInt(item.step_count);
  for(const path of [item.root,...(item.copy_path===undefined?[]:[item.copy_path])]){if(!relative(path))throw Error('批次路径无效');const fold=path.toLowerCase();if(paths.some(p=>p===fold||p.startsWith(fold+'/')||fold.startsWith(p+'/')))throw Error('批次条目或副本范围重叠');paths.push(fold)}
 }
 if(steps!==BigInt(x.step_count))throw Error('批次步骤总数不符');
 if(selected){if(x.items.length!==selected.length||Object.entries(selected[0]!.source_config).some(([k,v])=>c[k]!==v))throw Error('返回批次与选择配置不符');for(let i=0;i<selected.length;i++){const d=selected[i]!,item=x.items[i];if(item.decision_id!==d.id||item.root!==d.source_conflict.plan.path||item.conflict_ordinal!==d.source_conflict.ordinal||item.choice!==d.choice||(item.copy_path||'')!==(d.local_copy_path||'')||item.snapshot_evidence_sha256!==d.snapshot_evidence_sha256||item.snapshot_recorded_at_utc!==d.snapshot_recorded_at_utc||item.step_count!==(d.subtree_review?.plan_step_count||String(d.effects.length)))throw Error('返回批次改变了所选顺序或方案')}}
 return x;
}

export const batchCheckReasons:Record<string,string>={all_current_sources_match:'全部所选项与固定决策一致',one_or_more_items_changed:'部分所选项已变化，整批拒绝',configuration_changed:'同步配置已变化',unresolved_previous_write:'历史写入结果尚未核清',history_check_failed:'历史证据读取失败',decision_unavailable:'原决策暂不可读',batch_source_changed:'批次来源不一致',decision_already_admitted:'条目已有执行批次',decision_not_executable:'固定计划暂不能执行',current_scan_failed:'当前遍历失败',current_scan_incomplete:'缺少完整遍历证据',check_cancelled:'本次核对已取消',awaiting_completion:'尚无完成凭据',not_checked:'本项尚未核对',current_sources_match_preview:'本项当前观察一致',local_changed:'本地身份或内容变化',remote_changed:'网盘身份或内容变化',local_subtree_changed:'本地子树变化',remote_subtree_changed:'网盘子树变化',copy_path_occupied:'固定副本路径已占用'};
export type BatchCheck={id:string;batch_id:string;started_at_utc:string;finished_at_utc:string;status:string;reason:string;items:{decision_id:string;status:string;reason:string;subtree?:any}[];snapshot?:{local:any[];remote:any[]}};
export function parseBatchCheck(x:any,batch:Batch,full=false):BatchCheck{
 const finished=typeof x?.finished_at_utc==='string'&&x.finished_at_utc.endsWith('Z')&&Number.isFinite(Date.parse(x.finished_at_utc));
 if(!x||!id(x.id)||x.batch_id!==batch.id||!utc(x.started_at_utc)||!finished||!['incomplete','verified','rejected','failed'].includes(x.status)||!Object.hasOwn(batchCheckReasons,x.reason)||!Array.isArray(x.items)||x.items.length!==batch.items.length)throw Error('批次核对身份或条目数量无效');
 if(x.status==='incomplete'){if(!x.finished_at_utc.startsWith('0001-')||x.reason!=='awaiting_completion')throw Error('未完成核对不能带完成凭据')}else if(!utc(x.finished_at_utc)||Date.parse(x.finished_at_utc)<Date.parse(x.started_at_utc))throw Error('核对时间证据无效');
 const zeroCount=(v:unknown)=>typeof v==='string'&&/^(0|[1-9][0-9]*)$/.test(v),hash=(v:unknown)=>typeof v==='string'&&/^[a-f0-9]{64}$/.test(v);
 for(let i=0;i<x.items.length;i++){
  const item=x.items[i],source=batch.items[i]!;
  if(!item||item.decision_id!==source.decision_id||!['not_checked','verified','rejected'].includes(item.status)||!Object.hasOwn(batchCheckReasons,item.reason)||item.status==='verified'&&item.reason!=='current_sources_match_preview'||item.status==='not_checked'&&item.reason!=='not_checked'||item.status==='rejected'&&!['local_changed','remote_changed','local_subtree_changed','remote_subtree_changed','copy_path_occupied'].includes(item.reason))throw Error('逐项核对身份或结果无效');
  const p=item.subtree;if(p){if(p.version!=='subtree-identity-v1'||p.root!==source.root||p.source_snapshot_sha256!==source.snapshot_evidence_sha256||!['local_expected_sha256','local_observed_sha256','remote_expected_sha256','remote_observed_sha256'].every(k=>hash(p[k]))||!['local_expected_items','local_observed_items','remote_expected_items','remote_observed_items'].every(k=>zeroCount(p[k])))throw Error('子树核对来源证据无效');if(item.status==='verified'&&(p.local_expected_sha256!==p.local_observed_sha256||p.remote_expected_sha256!==p.remote_observed_sha256||p.local_expected_items!==p.local_observed_items||p.remote_expected_items!==p.remote_observed_items))throw Error('子树通过缺少一致证据')}
 }
 if(x.status==='incomplete'&&(x.snapshot!==undefined||x.items.some((item:any)=>item.status!=='not_checked')))throw Error('未完成核对包含虚构进度');
 if(x.status==='verified'&&(x.reason!=='all_current_sources_match'||x.items.some((item:any)=>item.status!=='verified'))||x.reason==='all_current_sources_match'&&x.status!=='verified'||x.reason==='one_or_more_items_changed'&&(x.status!=='rejected'||!x.items.some((item:any)=>item.status==='rejected')))throw Error('整批结果与逐项结果不一致');
 if(x.snapshot!==undefined&&(!Array.isArray(x.snapshot?.local)||!Array.isArray(x.snapshot?.remote)))throw Error('共同观察快照无效');
 if(full&&x.status==='verified'&&!x.snapshot)throw Error('通过核对缺少共同观察');
 if(x.snapshot){for(const side of ['local','remote']){const seen=new Set<string>();for(const entry of x.snapshot[side]){if(!entry||!relative(entry.path)||typeof entry.id!=='string'||!entry.id||typeof entry.directory!=='boolean'||!zeroCount(entry.size)||!entry.directory&&!hash(entry.sha256)||seen.has(entry.path.toLowerCase()))throw Error('共同观察项目无效');seen.add(entry.path.toLowerCase())}
  if(x.status==='verified'){for(let i=0;i<batch.items.length;i++){const source=batch.items[i]!,item=x.items[i],p=item.subtree,entries=x.snapshot[side];if(p){if(String(entries.filter((e:any)=>e.path===source.root||e.path.startsWith(source.root+'/')).length)!==p[side+'_observed_items'])throw Error('子树数量与共同观察不符')}else{const root=entries.find((e:any)=>e.path===source.root);if(!root||root.directory)throw Error('目录通过缺少完整子树证据')};if(source.copy_path){const copy=source.copy_path.toLowerCase();if([...seen].some(p=>p===copy||p.startsWith(copy+'/')))throw Error('固定副本已被占用')}}}
 }}
 return x;
}

export type BatchRun={id:string;config_id:string;status:string;started_at_utc:string;finished_at_utc:string|null;step_count:number;confirmed:number;error:string};
export type BatchExecution={batch_id:string;check_id:string;run:BatchRun;items:{decision_id:string;run:BatchRun}[]};
export const batchRunLabels:Record<string,string>={running:'执行中',verification_pending:'已执行，等待整批复核',failed:'执行未完成',conflict:'发现变化，已停止',unknown:'结果未知，保持隔离',interrupted:'中断，先核对原记录',complete:'整批复核完成'};
function parseBatchRun(x:any,batch:Batch,steps:number,verification=false):BatchRun{
 if(!x||!id(x.id)||x.config_id!==batch.source_config.id||!Object.hasOwn(batchRunLabels,x.status)||verification&&!['running','complete','failed','conflict','unknown','interrupted'].includes(x.status)||!verification&&x.status==='complete'||!utc(x.started_at_utc)||x.finished_at_utc!==null&&!utc(x.finished_at_utc)||x.status!=='running'&&!utc(x.finished_at_utc)||!Number.isSafeInteger(steps)||x.step_count!==steps||!Number.isSafeInteger(x.confirmed)||x.confirmed<0||x.confirmed>steps||typeof x.error!=='string')throw Error('批次运行身份或步骤证据无效');return x
}
export function parseBatchExecution(x:any,batch:Batch,previous?:BatchExecution|null):BatchExecution{
 if(!x||x.batch_id!==batch.id||!id(x.check_id)||!Array.isArray(x.items)||x.items.length!==batch.items.length)throw Error('执行批次范围无效');parseBatchRun(x.run,batch,batch.items.length);
 const ids=new Set<string>([x.run.id]);let confirmed=0;
 for(let i=0;i<x.items.length;i++){const item=x.items[i],source=batch.items[i]!;if(!item||item.decision_id!==source.decision_id)throw Error('执行子项决策身份无效');parseBatchRun(item.run,batch,Number(source.step_count));if(ids.has(item.run.id))throw Error('执行运行身份重复');ids.add(item.run.id);if(item.run.status==='verification_pending'){if(item.run.confirmed!==item.run.step_count)throw Error('子项未完整确认');confirmed++}}
 if(x.run.confirmed>confirmed||x.run.status==='verification_pending'&&(x.run.confirmed!==batch.items.length||confirmed!==batch.items.length))throw Error('父批次确认数与子项不符');
 if(previous&&(x.run.id!==previous.run.id||x.check_id!==previous.check_id||x.items.some((item:any,i:number)=>item.run.id!==previous.items[i]!.run.id)))throw Error('执行批次身份发生变化');return x
}
export function parseBatchVerification(x:any,batch:Batch,execution:BatchExecution|null):BatchRun{
 parseBatchRun(x,batch,0,true);if(x.confirmed!==0||execution&&(x.id===execution.run.id||execution.items.some(item=>item.run.id===x.id)))throw Error('完整复核不能使用执行批次证据');return x
}
