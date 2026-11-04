<script setup lang="ts">
import {vModal} from './modal';
import type {Migration} from './migration-types';
import MigrationEvents from './MigrationEvents.vue';
import MigrationExport from './MigrationExport.vue';
const props=defineProps<{record:Migration;observedAt:string}>();
const emit=defineEmits<{close:[];authenticationRequired:[]}>();
const statuses:Record<string,string>={running:'迁移中',failed:'失败',completed:'完成'},phases:Record<string,string>={copying:'复制文件',verifying:'核验内容',retiring_source:'隔离原目录',source_retired:'原目录已隔离',deleting_source:'清理原目录',complete:'完成'};
</script>
<template>
 <div class="modal-backdrop" @click.self="emit('close')"><section class="detail-drawer" role="dialog" aria-modal="true" aria-labelledby="migration-details-title" v-modal="()=>emit('close')">
  <header class="page-header"><h2 id="migration-details-title">迁移记录详情</h2><button class="button secondary" data-testid="migration-details-close" @click="emit('close')">关闭</button></header>
  <p class="notice">这是固定的历史读取快照，不代表当前磁盘文件已经重新核验；查看详情不会重新执行迁移。</p>
  <p><strong>{{statuses[props.record.status]}}</strong> · {{phases[props.record.phase]}}</p>
  <dl class="path-settings-overview"><div><dt>记录 ID</dt><dd>{{props.record.id}}</dd><dt>原本地目录</dt><dd>{{props.record.source_path}}</dd><dt>目标本地目录</dt><dd>{{props.record.target_path}}</dd></div><div><dt>文件：已处理 / 总量</dt><dd>{{props.record.processed_files}} / {{props.record.total_files}}</dd><dt>字节：已处理 / 总量</dt><dd>{{props.record.processed_bytes}} / {{props.record.total_bytes}}</dd></div></dl>
  <p class="muted">开始 UTC：{{props.record.started_at_utc}}<br>更新 UTC：{{props.record.updated_at_utc}}<template v-if="props.record.finished_at_utc&&!props.record.finished_at_utc.startsWith('0001-')"><br>结束 UTC：{{props.record.finished_at_utc}}</template><br>本机读取 UTC：{{props.observedAt}}</p>
  <p v-if="props.record.last_error" class="notice danger">{{props.record.last_error}}</p>
  <MigrationExport :id="props.record.id" @authentication-required="emit('authenticationRequired')"/>
  <MigrationEvents :key="props.record.id" :id="props.record.id" @authentication-required="emit('authenticationRequired')"/>
 </section></div>
</template>
