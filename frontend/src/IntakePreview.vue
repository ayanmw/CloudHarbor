<script setup lang="ts">
import {computed,ref} from 'vue';
import {intakeStates,type IntakeRecord} from './intake-types';
const props=defineProps<{record:IntakeRecord;now:number;busy:boolean;disabled:boolean}>(),emit=defineEmits<{confirm:[]}>();
const checked=ref(false),expired=computed(()=>props.now>=Date.parse(props.record.expires));
</script>
<template>
 <section class="event-results intake-selection" data-testid="intake-preview-result" aria-label="预检确认">
  <h2>核对后确认移动</h2><p class="muted">预检批次：{{record.id}}</p>
  <p>目标队列：<strong>{{record.queue_dir}}</strong></p><p>此批固定策略：<strong>{{record.download_mode==='delete'?'下载完成后删除网盘项':'下载完成后保留并标记 .可删除'}}</strong></p>
  <p class="muted">创建时间：{{record.created}} · 有效期至：{{record.expires}}（按记录原时区显示）</p>
  <p v-if="expired" class="notice danger" role="status">预检已过期，请重新预检。有效性最终由服务端核对。</p>
  <div class="event-table-scroll"><table><thead><tr><th>源项目 / 身份</th><th>目标</th><th>结果</th></tr></thead><tbody><tr v-for="item in record.items" :key="item.source.path"><td>{{item.source.path}}<small class="cell-sub">对象 ID：{{item.source_id}}</small></td><td>{{item.target}}</td><td>{{intakeStates[item.state]}}</td></tr></tbody></table></div>
  <label class="intake-confirm"><input v-model="checked" type="checkbox" data-testid="intake-confirm" :disabled="busy||disabled||expired"/>我已核对源项目、目标目录及后处理策略，确认移动。</label>
  <button class="button" data-testid="intake-commit" :disabled="busy||disabled||expired||!checked" @click="checked&&emit('confirm')">{{busy?'正在提交，请勿重复操作…':'确认移动并加入队列'}}</button>
 </section>
</template>
