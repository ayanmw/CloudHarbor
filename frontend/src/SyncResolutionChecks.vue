<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import SyncResolutionExecution from './SyncResolutionExecution.vue';
import type {Resolution} from './sync-resolution';
const props=defineProps<{decision:Resolution;disabled:boolean;syncActive?:boolean}>(),emit=defineEmits<{authenticationRequired:[]}>();
type SubtreeProof={version:string;root:string;source_snapshot_sha256:string}&Record<string,string>;
type Check={check_scope?:string;execution_supported?:boolean;subtree?:SubtreeProof;id:string;decision_id:string;started_at_utc:string;finished_at_utc:string;status:string;reason:string;snapshot?:{local:unknown[];remote:unknown[]}};
const statuses:Record<string,string>={incomplete:'缺少完成凭据',verified:'本次观察一致',rejected:'已拒绝继续',failed:'本次核对失败'};
const reasons:Record<string,string>={decision_not_executable:'此决策没有可执行预览',subtree_plan_incomplete:'固定子树步骤证据不完整',local_subtree_changed:'本地目录树身份、内容或元数据已变化',remote_subtree_changed:'网盘目录树身份、内容或元数据已变化',configuration_missing:'同步配置已不存在',configuration_changed:'同步配置已变化',history_check_failed:'历史写入状态读取失败',unresolved_previous_write:'历史写入结果尚未核清',current_scan_failed:'当前根身份或两端内容遍历失败',check_cancelled:'核对已取消',local_changed:'本地身份或内容已变化',remote_changed:'网盘身份或内容已变化',copy_path_occupied:'固定副本路径已被占用',current_sources_match_preview:'当前两端与原预览一致'};
const result=ref<Check|null>(null),history=ref<Check[]>([]),busy=ref(false),error=ref(''),offset=ref(0),more=ref(false);
const canCheck=computed(()=>props.decision.state==='preview'||props.decision.state==='review_required'&&props.decision.subtree_review?.plan_version==='subtree-steps-v1'&&BigInt(props.decision.subtree_review.plan_step_count||'0')>0n);
let active=true,generation=0,controller:AbortController|undefined;
const current=(n:number)=>active&&n===generation;
const validID=(v:unknown)=>typeof v==='string'&&/^[a-f0-9]{32}$/.test(v);
const utc=(v:unknown)=>typeof v==='string'&&v.endsWith('Z')&&Number.isFinite(Date.parse(v));
function parse(v:any,full=false):Check{
 if(!v||!validID(v.id)||v.decision_id!==props.decision.id||!utc(v.started_at_utc)||v.started_at_utc.startsWith('0001-')||!Object.hasOwn(statuses,v.status)||typeof v.reason!=='string'||!utc(v.finished_at_utc))throw Error('核对身份或时间证据无效');
 if(v.status==='incomplete'){if(!v.finished_at_utc.startsWith('0001-'))throw Error('未完成核对不能带完成凭据')}
 else if(v.finished_at_utc.startsWith('0001-')||!Object.hasOwn(reasons,v.reason)||v.status==='verified'&&v.reason!=='current_sources_match_preview'||v.status!=='verified'&&v.reason==='current_sources_match_preview')throw Error('核对结果证据无效');
 if(v.check_scope!==undefined&&!['file','subtree'].includes(v.check_scope)||v.execution_supported!==undefined&&typeof v.execution_supported!=='boolean')throw Error('核对范围标记无效');
 if(v.check_scope==='subtree'){
  if(props.decision.subtree_review?.plan_version!=='subtree-steps-v1'||typeof v.execution_supported!=='boolean')throw Error('子树核对与执行边界无效');
  const proof=v.subtree,number=(x:unknown)=>typeof x==='string'&&/^(0|[1-9][0-9]*)$/.test(x),hash=(x:unknown)=>typeof x==='string'&&/^[a-f0-9]{64}$/.test(x);
  if(proof){if(proof.version!=='subtree-identity-v1'||proof.root!==props.decision.source_conflict.plan.path||proof.source_snapshot_sha256!==props.decision.snapshot_evidence_sha256||!['local_expected_sha256','local_observed_sha256','remote_expected_sha256','remote_observed_sha256'].every(k=>hash(proof[k]))||!['local_expected_items','local_observed_items','remote_expected_items','remote_observed_items'].every(k=>number(proof[k])))throw Error('子树指纹或来源证据无效');}
  if(v.status==='verified'&&(!proof||proof.local_expected_sha256!==proof.local_observed_sha256||proof.remote_expected_sha256!==proof.remote_observed_sha256||proof.local_expected_items!==proof.local_observed_items||proof.remote_expected_items!==proof.remote_observed_items))throw Error('子树核对通过缺少一致的完整证据');
 }else if(v.status==='verified'&&props.decision.state!=='preview')throw Error('目录核对缺少完整子树证据');
 if(v.snapshot!==undefined&&(!Array.isArray(v.snapshot?.local)||!Array.isArray(v.snapshot?.remote)))throw Error('两端核对快照无效');
 if(full&&v.status==='verified'&&!v.snapshot)throw Error('核对通过缺少当前快照');
 if(full&&v.snapshot){
  for(const side of ['local','remote'] as const){
   const seen=new Set<string>();
   for(const entry of v.snapshot[side]){
    if(!entry||typeof entry.path!=='string'||!entry.path||entry.path.split('/').some((p:string)=>!p||p==='.'||p==='..')||/[\\:\x00]/.test(entry.path)||typeof entry.id!=='string'||!entry.id||typeof entry.directory!=='boolean'||typeof entry.size!=='string'||!/^(0|[1-9][0-9]*)$/.test(entry.size)||!entry.directory&&!/^[a-f0-9]{64}$/.test(entry.sha256))throw Error('核对快照内容证据无效');
    const key=entry.path.toLowerCase();if(seen.has(key))throw Error('核对快照路径重复');seen.add(key);
   }
   if(v.status==='verified'){
    if(v.check_scope==='subtree'){
     const root=v.subtree.root,count=v.snapshot[side].filter((entry:any)=>entry.path===root||entry.path.startsWith(root+'/')).length;
     if(String(count)!==v.subtree[side+'_observed_items'])throw Error('子树项目数与当前快照不一致');
    }else{
     const want=props.decision.source_conflict.plan[side],got=v.snapshot[side].find((entry:any)=>entry.path===props.decision.source_conflict.plan.path);
     if(!want||!got||(['path','id','directory','size','sha256'] as const).some(k=>got[k]!==want[k]))throw Error('核对通过与两端内容证据不一致');
    }
    if(props.decision.local_copy_path){const copy=props.decision.local_copy_path.toLowerCase();if([...seen].some(p=>p===copy||p.startsWith(copy+'/')))throw Error('核对通过但固定副本路径已占用');}
   }
  }
 }
 return v;
}
async function request(path:string,body?:unknown){const n=generation;const response=await fetch(path,{method:body===undefined?'GET':'POST',credentials:'same-origin',signal:controller?.signal,headers:body===undefined?{}:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body)});if(!current(n))throw new DOMException('旧请求','AbortError');if(response.status===401){emit('authenticationRequired');throw Error('登录已失效')}if(!response.ok)throw Error((await response.text()).slice(0,1000)||'核对请求失败');return response.json()}
async function check(){if(!active||busy.value||props.disabled||props.syncActive||!canCheck.value)return;const n=++generation;controller=new AbortController();busy.value=true;try{const data=await request('/api/sync/resolutions/'+props.decision.id+'/checks',{});if(!current(n))return;result.value=parse(data,true);error.value=''}catch(e){if(current(n))error.value=(e instanceof Error?e.message:String(e))+'；请读取核对历史查找完成记录，不会自动重发。'}finally{if(current(n))busy.value=false}}
async function list(at=0){if(!active||busy.value||props.disabled)return;const n=++generation;controller=new AbortController();busy.value=true;try{const data=await request('/api/sync/resolutions/'+props.decision.id+'/checks?offset='+at);if(!current(n))return;if(data.decision_id!==props.decision.id||data.offset!==at||typeof data.has_more!=='boolean'||!Array.isArray(data.checks)||data.checks.length>25||data.has_more&&data.checks.length!==25)throw Error('核对历史范围无效');const rows=data.checks.map((v:any)=>parse(v));if(new Set(rows.map((v:Check)=>v.id)).size!==rows.length)throw Error('核对历史身份重复');history.value=rows;offset.value=at;more.value=data.has_more;error.value=''}catch(e){if(current(n))error.value=e instanceof Error?e.message:String(e)}finally{if(current(n))busy.value=false}}
function reset(){generation++;controller?.abort();busy.value=false;result.value=null;history.value=[];offset.value=0;more.value=false;error.value=''}
function stop(){active=false;reset()}
function restore(){active=true}
watch(()=>props.decision.id,reset);watch(()=>props.disabled,v=>{if(v)reset()});
onMounted(()=>{window.addEventListener('pagehide',stop);window.addEventListener('pageshow',restore)});onUnmounted(()=>{stop();window.removeEventListener('pagehide',stop);window.removeEventListener('pageshow',restore)});
</script>
<template><section class="resolution-checks"><header><h3>核对当前两端</h3><button class="button secondary compact" data-testid="sync-resolution-check" :disabled="busy||disabled||syncActive||!canCheck" @click="check">{{busy?'正在读取 / 核对…':'重新核对当前两端'}}</button></header><p class="muted">会重新遍历此同步目录并下载校验网盘内容，可能需要等待。结果只代表当时观察，执行时服务端还会重新核对；核对本身不会执行文件操作。</p><p v-if="busy" role="status">请求处理中；离开会取消当前读取，不会自动重发。可稍后读取核对历史。</p><p v-if="error" class="notice danger" role="alert">{{error}}<span v-if="result"> · 保留上次读取的固定核对结果。</span></p><section v-if="result" data-testid="sync-check-result" class="check-observation"><strong>{{statuses[result.status]}}</strong><p>{{reasons[result.reason]||'尚无完成凭据，请查看原始证据'}}</p><p>核对 {{result.id}}<br>开始 UTC {{result.started_at_utc}}<br>完成 UTC {{result.status==='incomplete'?'尚无完成记录':result.finished_at_utc}}</p><p v-if="result.snapshot">遍历到本地 {{result.snapshot.local.length}} 项、网盘 {{result.snapshot.remote.length}} 项。</p><a class="button secondary compact" :href="'/api/sync/resolution-checks/'+result.id" target="_blank" rel="noopener">查看完整核对证据 JSON</a><p v-if="result.check_scope==='subtree'" class="muted">本次为整段子树只读核对，包含子项身份、内容与修改时间；<template v-if="result.execution_supported">可在确认固定步骤后执行；服务端还会再次核对。</template><template v-else>此历史核对只供读取，需要重新核对才能确认当前执行能力。</template></p><p v-if="result.subtree">子树 {{result.subtree.root}} · 本地 {{result.subtree.local_expected_items}} → {{result.subtree.local_observed_items}} 项 · 网盘 {{result.subtree.remote_expected_items}} → {{result.subtree.remote_observed_items}} 项</p><SyncResolutionExecution v-if="result.status==='verified'&&(decision.state==='preview'||result.check_scope==='subtree'&&result.execution_supported===true)" :decision="decision" :check="result" :disabled="busy||disabled" :sync-active="syncActive" @authentication-required="emit('authenticationRequired')"/></section><header><h4>已记录的核对</h4><button class="button secondary compact" data-testid="sync-check-history" :disabled="busy||disabled" @click="list()">读取核对历史</button></header><div v-if="history.length" class="table-wrap"><table><thead><tr><th>开始 UTC</th><th>完成 UTC</th><th>结果 / 原因</th><th>查看</th></tr></thead><tbody><tr v-for="record in history" :key="record.id"><td>{{record.started_at_utc}}</td><td>{{record.status==='incomplete'?'没有完成凭据':record.finished_at_utc}}</td><td>{{statuses[record.status]}}<small>{{reasons[record.reason]||'请查看完整证据'}}</small></td><td><button class="button secondary compact" :disabled="busy" @click="result=record">查看记录</button></td></tr></tbody></table><footer><button class="button secondary compact" :disabled="busy||offset===0" @click="list(Math.max(0,offset-25))">上一页</button><button class="button secondary compact" :disabled="busy||!more" @click="list(offset+25)">下一页</button></footer></div></section></template>
<style scoped>.resolution-checks{margin-top:18px;border-top:1px solid #dbe4ef;padding-top:12px}.resolution-checks header{display:flex;align-items:center;justify-content:space-between;gap:8px;flex-wrap:wrap}.check-observation{padding:12px 14px;border:1px solid #dce5ef;border-radius:8px;background:#f5f8fc;margin:12px 0;overflow-wrap:anywhere}table{width:100%;border-collapse:collapse;font-size:12px;text-align:left}th,td{padding:8px 10px;border-bottom:1px solid #e4eaf2;vertical-align:top}th{background:#f5f7fb}small{display:block;color:#65748b;margin-top:4px}.table-wrap{overflow:auto}footer{display:flex;gap:8px;margin-top:10px}</style>
