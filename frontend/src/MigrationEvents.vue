<script setup lang="ts">
import {onUnmounted,ref} from 'vue';
const props=defineProps<{id:string}>(),emit=defineEmits<{authenticationRequired:[]}>();
type Event={sequence:string;status:string;phase:string;recorded_at_utc:string;evidence_json:string};
const events=ref<Event[]>([]),loading=ref(false),error=ref(''),ready=ref(false),next=ref('0'),before=ref(''),previous=ref<string[]>([]),observed=ref('');
let active=true,generation=0,controller:AbortController|undefined;
const validSequence=(value:unknown,zero=false):value is string=>typeof value==='string'&&/^(0|[1-9][0-9]*)$/.test(value)&&value.length<=19&&BigInt(value)<=9223372036854775807n&&(zero||value!=='0');
async function load(cursor='',direction:'refresh'|'older'|'newer'='refresh'){
 if(!active||loading.value)return;const request=++generation;controller=new AbortController();loading.value=true;
 const params=new URLSearchParams({limit:'20',identity_format:'string'});if(cursor)params.set('before_sequence',cursor);
 try{const response=await fetch('/api/settings/local-migrations/'+encodeURIComponent(props.id)+'/events?'+params,{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){stop();emit('authenticationRequired');return;}
  if(!response.ok)throw Error(await response.text()||'事件读取失败');const data=await response.json();if(request!==generation)return;
  if(!data||data.migration_id!==props.id||!Array.isArray(data.events)||data.events.length>20||typeof data.has_more!=='boolean'||!validSequence(data.next_before_sequence,true)||!data.events.every((event:any,index:number)=>event&&validSequence(event.sequence)&&(!cursor||BigInt(event.sequence)<BigInt(cursor))&&(!index||BigInt(event.sequence)<BigInt(data.events[index-1].sequence))&&['running','failed','completed'].includes(event.status)&&['copying','verifying','retiring_source','source_retired','deleting_source','complete'].includes(event.phase)&&typeof event.recorded_at_utc==='string'&&/Z$/.test(event.recorded_at_utc)&&Number.isFinite(Date.parse(event.recorded_at_utc))&&typeof event.evidence_json==='string'))throw Error('迁移事件响应无效');
  if(data.has_more?data.events.length!==20||data.next_before_sequence!==data.events.at(-1)?.sequence:data.next_before_sequence!=='0')throw Error('迁移事件游标无效');
  if(direction==='older')previous.value.push(before.value);else if(direction==='newer')previous.value.pop();else previous.value=[];
  before.value=cursor;events.value=data.events;next.value=data.has_more?data.next_before_sequence:'0';ready.value=true;observed.value=new Date().toISOString();error.value='';
 }catch(e:any){if(request===generation)error.value=e.message||'事件读取失败';}
 finally{if(request===generation)loading.value=false;}
}
function stop(){active=false;generation++;controller?.abort();loading.value=false;}
onUnmounted(stop);
</script>
<template>
 <section aria-label="迁移关键事件"><h3>关键事件</h3><button class="button secondary" data-testid="migration-events-load" :disabled="loading||!active" @click="load()">{{ready?'重新读取最新事件':'读取关键事件'}}</button><p v-if="loading" role="status">正在读取…</p><p v-if="error" class="notice danger" role="alert">{{error}}<span v-if="ready"> · 下方为上次读取结果，分页已停用。</span></p>
  <div v-if="ready" class="event-table-scroll"><table data-testid="migration-events-table"><thead><tr><th>序号</th><th>状态 / 阶段</th><th>本机记录 UTC</th><th>证据原文</th></tr></thead><tbody><tr v-for="event in events" :key="event.sequence"><td>{{event.sequence}}</td><td>{{event.status}} / {{event.phase}}</td><td>{{event.recorded_at_utc}}</td><td><details><summary>查看证据</summary><pre>{{event.evidence_json}}</pre></details></td></tr><tr v-if="!events.length"><td colspan="4">暂无关键事件</td></tr></tbody></table></div>
  <p v-if="observed" class="muted">本机读取 UTC：{{observed}}；事件不能证明当前磁盘已重新核验。</p><div v-if="ready" class="toolbar"><button class="button secondary" data-testid="migration-events-newer" :disabled="loading||!!error||!previous.length" @click="load(previous[previous.length-1],'newer')">较新事件</button><button class="button secondary" data-testid="migration-events-older" :disabled="loading||!!error||next==='0'" @click="load(next,'older')">更早事件</button></div>
 </section>
</template>
