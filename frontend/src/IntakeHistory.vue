<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';
import {intakeLabel,validateIntake,type IntakeRecord} from './intake-types';
const props=defineProps<{disabled:boolean;revision:number}>(),emit=defineEmits<{select:[record:IntakeRecord];authenticationRequired:[]}>();
const unresolved=ref(false),displayedFilter=ref<boolean|null>(null),records=ref<IntakeRecord[]|null>(null),total=ref(0),loading=ref(false),error=ref(''),pageIndex=ref(0),next=ref<string|null>(null),trail=ref<(string|null)[]>([null]);
const canPage=computed(()=>!props.disabled&&!loading.value&&records.value!==null&&displayedFilter.value===unresolved.value);
let generation=0,controller:AbortController|undefined;
async function load(before:string|null=null,index=0){
 if(props.disabled)return;
 const request=++generation,filter=unresolved.value;controller?.abort();controller=new AbortController();loading.value=true;error.value='';
 const query=new URLSearchParams({limit:'20',unresolved:filter?'1':'0'});if(before)query.set('cursor',before);
 try{
  const response=await fetch('/api/intakes?'+query,{credentials:'same-origin',signal:controller.signal});if(request!==generation)return;
  if(response.status===401){emit('authenticationRequired');throw Error('请重新登录');}
  if(!response.ok)throw Error(await response.text()||'入队历史暂不可读');
  const data=await response.json();if(request!==generation)return;
  const rows=data.records===null?[]:data.records;
  if(!Array.isArray(rows)||rows.length>20||!Number.isSafeInteger(data.total)||data.total<rows.length||!(data.next_cursor===null||(typeof data.next_cursor==='string'&&/^[A-Za-z0-9_-]{1,512}$/.test(data.next_cursor)&&rows.length>0&&data.next_cursor!==before&&!trail.value.slice(0,index+1).includes(data.next_cursor))))throw Error('入队历史分页响应无效');
  rows.forEach(validateIntake);if(new Set(rows.map((r:IntakeRecord)=>r.id)).size!==rows.length)throw Error('入队历史身份重复');
  if(filter&&rows.some((r:IntakeRecord)=>!r.items.some(item=>item.state==='unknown'||item.state==='sending')))throw Error('待核对历史响应与条件不匹配');
  records.value=rows;total.value=data.total;displayedFilter.value=filter;next.value=data.next_cursor;pageIndex.value=index;
  trail.value=index===0?[null]:[...trail.value.slice(0,index),before];
 }catch(e:any){if(request===generation)error.value=e.message||'入队历史读取失败';}
 finally{if(request===generation)loading.value=false;}
}
function navigate(delta:number){if(!canPage.value)return;if(delta>0&&next.value)load(next.value,pageIndex.value+1);else if(delta<0&&pageIndex.value>0)load(trail.value[pageIndex.value-1],pageIndex.value-1);}
watch(unresolved,()=>load());
watch(()=>props.revision,()=>load(displayedFilter.value===unresolved.value?trail.value[pageIndex.value]:null,displayedFilter.value===unresolved.value?pageIndex.value:0));
onMounted(()=>load());
onUnmounted(()=>{generation++;controller?.abort();});
</script>
<template>
 <section class="event-results intake-selection" data-testid="intake-history" aria-label="入队历史">
  <div class="intake-pagination"><h2>入队记录</h2><label><input v-model="unresolved" name="intake-unresolved" type="checkbox" :disabled="disabled"/>只看待核对记录</label><button class="button secondary" :disabled="loading||disabled" @click="load()">刷新记录</button></div>
  <p class="notice">结果未知时目标持续隔离。缺少完成凭据的目录不提供人工放行，不能仅凭目标根目录出现认定完成。</p>
  <p v-if="error" data-testid="intake-history-error" class="notice danger" role="alert">{{error}}<span v-if="records"> · 保留上次读取结果，尚未按当前条件重新核对。</span></p>
  <div class="event-result-summary" role="status" aria-live="polite">{{loading?'正在读取入队记录…':records?`实时匹配 ${total} 笔 · 本页 ${records.length} 笔`:'尚未读取记录'}}</div>
  <div class="event-table-scroll"><table><thead><tr><th>批次 / 状态</th><th>项目 / 目标队列</th><th>创建时间（原时区）</th><th>操作</th></tr></thead><tbody><tr v-for="record in records||[]" :key="record.id"><td>{{intakeLabel(record)}}<small class="cell-sub">{{record.id}}</small></td><td>{{record.items.length}} 项<small class="cell-sub">{{record.items[0]?.source.path}}{{record.items.length>1?' 等':''}}</small><small class="cell-sub">{{record.queue_dir}}</small></td><td>{{record.created}}</td><td><button class="event-detail-link" data-testid="intake-record-details" :disabled="loading||disabled" @click="emit('select',record)">查看详情</button></td></tr></tbody></table></div>
  <p v-if="records&&!records.length&&!loading&&!error" class="event-empty">暂无对应入队记录。</p>
  <div class="intake-pagination"><button class="button secondary" data-testid="intake-history-prev" :disabled="!canPage||pageIndex===0" @click="navigate(-1)">较新记录</button><span>浏览位置 {{pageIndex+1}}</span><button class="button secondary" data-testid="intake-history-next" :disabled="!canPage||!next" @click="navigate(1)">更早记录</button></div>
 </section>
</template>
