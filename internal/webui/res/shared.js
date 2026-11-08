let adminSessionRedirecting = false;
function adminSessionExpired() {
    if (!adminSessionRedirecting) {
        adminSessionRedirecting = true;
        globalThis.stopAdminSessionPolling?.();
        const next = location.pathname + location.search;
        location.replace('/login?next=' + encodeURIComponent(next || '/'));
    }
    return '管理员登录已失效，请重新登录。';
}

// Cloud names and task errors are untrusted text, including inside templates.
function escapeHTML(value) {
    return String(value ?? '').replace(/[&<>"']/g, ch => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[ch]));
}
function credentialStatusText(auth) {
    if (!auth) return '状态未知';
    if (auth.ready) return auth.state === 'expiry_unknown' ? '可用（到期时间未知）' : (auth.next_attempt ? '可用，续期等待重试' : '账号可用');
    const labels = {unconfigured:'尚未授权', verification_required:'等待账号验证', account_mismatch:'账号不匹配', reauthorization_required:'需要重新授权', token_rejected:'凭证失效，等待续期', storage_error:'凭证保存失败', closed:'服务已停止'};
    return labels[auth.state] || '账号暂不可用';
}

let healthStatusRequestID = 0;
let healthStatusRequestInFlight = false;
// Health reads may wait on SQLite; skip timer ticks while one request is in flight.
function beginHealthStatusRequest() {
    if (healthStatusRequestInFlight) return 0;
    healthStatusRequestInFlight = true;
    return ++healthStatusRequestID;
}
function finishHealthStatusRequest(requestID) {
    if (requestID && requestID === healthStatusRequestID) healthStatusRequestInFlight = false;
}
function isLatestHealthStatusRequest(requestID) { return requestID === healthStatusRequestID; }

function renderMonitorHealth(data) {
    const target = document.getElementById('monitor-status-text');
    if (!data || typeof data !== 'object') {
        target.textContent = '目录监控状态暂不可读。';
        return;
    }
    const lastSuccess = typeof data.last_successful_scan === 'string' && data.last_successful_scan
        ? `${data.last_successful_scan_source === 'runtime' ? '本进程最近成功扫描（UTC）' : '最近成功扫描（UTC）'}：${data.last_successful_scan}` : '尚无成功扫描记录';
    if (Number.isFinite(data.queue_monitor_retry_remaining_seconds) && data.queue_monitor_retry_remaining_seconds > 0) {
        target.textContent = `百度目录请求失败，自动监控将在 ${data.queue_monitor_retry_at_utc || data.next_scan_time}（UTC）后重试；${lastSuccess}。`;
        return;
    }
    if (data.scan_in_progress === true) {
        target.textContent = `正在执行目录扫描${data.scan_history_available === false ? '；扫描历史暂不可读' : ''}；${lastSuccess}。`;
    } else if (data.scan_history_available === false) {
        target.textContent = `扫描历史暂不可读，当前结果无法与持久记录核对${data.scan_error ? '；本进程最近扫描失败' : ''}；${lastSuccess}。`;
    } else if (data.scan_error === 'authentication_wait' || data.status === 'waiting_authorization') {
        target.textContent = `目录监控等待百度授权；${lastSuccess}。`;
    } else if (data.scan_error) {
        target.textContent = `最近一次目录扫描失败，请检查服务日志；${lastSuccess}。`;
    } else if (data.status === 'starting') {
        target.textContent = `目录监控正在启动；${lastSuccess}。`;
    } else if (data.last_successful_scan) {
        target.textContent = `目录监控正常；${lastSuccess}。`;
    } else {
        target.textContent = '目录监控尚无成功扫描记录。';
    }
}

async function logoutAdmin() {
    try { await fetch('/api/logout', {method:'POST'}); } finally { location.replace('/login'); }
}

let queuePauseState = null;
let queuePausePending = false;
let queueHealthy = false;
function renderQueueHealth(healthy) {
    if (typeof healthy !== 'boolean') return false;
    queueHealthy = healthy;
    const alert = document.getElementById('queue-health-alert');
    alert.hidden = healthy;
    alert.textContent = healthy ? '' : '下载队列已停止处理任务。请查看 /api/health 中的队列错误，修复本地存储问题后重启程序。';
    const button = document.getElementById('queue-pause-toggle');
    button.disabled = !healthy || queuePausePending || !queuePauseState;
    return true;
}
function renderQueueUnavailable() {
    queueHealthy = false;
    queuePauseState = null;
    const alert = document.getElementById('queue-health-alert');
    alert.hidden = false;
    alert.textContent = '下载队列状态暂不可读；恢复连接并读取到新状态前，队列控制已禁用。';
    const status = document.getElementById('queue-pause-status');
    status.textContent = '下载队列状态暂不可读。';
    document.getElementById('queue-pause-toggle').disabled = true;
}
function renderQueuePause(state) {
    if (!state || typeof state.paused !== 'boolean') return false;
    const active = Number.isSafeInteger(Number(state.active_tasks)) && Number(state.active_tasks) >= 0 ? Number(state.active_tasks) : 0;
    queuePauseState = {paused:state.paused, maintenance:state.maintenance === true, active_tasks:active, updated_at_utc:typeof state.updated_at_utc === 'string' ? state.updated_at_utc : ''};
    const status = document.getElementById('queue-pause-status');
    const button = document.getElementById('queue-pause-toggle');
    if (queuePauseState.maintenance) {
        status.textContent = `${state.paused ? '用户暂停已设置；' : ''}队列维护中，当前不会领取新任务。维护结束后按用户暂停设置恢复。`;
    } else {
        status.textContent = `${state.paused ? '队列已暂停；' : '下载队列运行中；'}${active} 个下载正在运行。暂停只停止领取新任务，当前下载会自然完成，网盘监控仍会继续。${queuePauseState.updated_at_utc ? ` 最近切换时间（UTC）：${queuePauseState.updated_at_utc}` : ''}`;
    }
    button.textContent = state.paused ? '恢复下载队列' : '暂停下载队列';
    button.disabled = queuePausePending || !queueHealthy;
    return true;
}

function bindQueuePauseControl(refresh) {
    const button = document.getElementById('queue-pause-toggle');
    button.addEventListener('click', async event => {
        event.preventDefault();
        if (!queuePauseState || !queueHealthy || queuePausePending) return;
        queuePausePending = true;
        button.disabled = true;
        const feedback = document.getElementById('queue-pause-feedback');
        feedback.textContent = queuePauseState.paused ? '正在恢复下载队列…' : '正在暂停新任务领取…';
        try {
            const action = queuePauseState.paused ? 'resume' : 'pause';
            const response = await fetch(`/api/queue/${action}`, {method:'POST'});
            if (!response.ok) {
                if (response.status === 401) adminSessionExpired();
                throw new Error('队列状态未更改；请检查服务状态后重试。');
            }
            const result = await response.json();
            if (!renderQueuePause(result.queue_pause)) throw new Error('服务未返回有效的队列状态。');
            feedback.textContent = queuePauseState.paused ? '已暂停领取新任务；当前下载继续完成。' : '下载队列已恢复。';
            await refresh();
        } catch (error) {
            feedback.textContent = error.message || '队列状态未更改，请稍后重试。';
        } finally {
            queuePausePending = false;
            button.disabled = !queuePauseState || !queueHealthy;
        }
    });
}

const pendingTaskActions = new Set();
const taskEventPanels = new Map();
const taskEventPageSize = 20;
const taskEventFilterKinds = {
    authentication_wait_entered: '等待百度授权',
    cloud_file_observed: '扫描发现网盘文件',
    download_attempt_finished: '下载尝试结束',
    download_attempt_started: '下载尝试开始',
    download_destination_recorded: '本地目标路径已记录',
    job_completed: '任务完成',
    job_registered: '任务登记',
    job_state_saved: '任务状态保存',
    legacy_import: '旧队列导入',
    source_operation_sending: '准备处理网盘源文件',
    source_identity_changed: '网盘源文件身份变化',
    source_operation_confirmed: '网盘源文件操作已确认',
    source_operation_retryable_failure: '网盘源文件操作可重试失败',
    source_operation_unknown: '网盘源文件操作结果未知',
    startup_recovery: '启动恢复',
    status_transition: '任务状态变化',
    task_cancelled: '取消任务',
    task_cloud_identity_updated: '网盘文件身份更新',
    task_paused: '暂停任务',
    task_recovered_after_scan: '扫描后恢复任务',
    task_resumed: '继续任务',
    task_retry_requested: '手动请求重试',
    task_source_deleted_remotely: '网盘源文件已删除',
    task_source_missing: '扫描未发现网盘源文件'
};

function taskEventState(id) {
    let state = taskEventPanels.get(id);
    if (!state) {
        state = {expanded:true, loaded:false, loading:false, exporting:false, exportError:'', error:'', events:[], totalMatches:0, kindCounts:[], contexts:{}, contextRequestIDs:{}, copyStatus:{}, filter:{kind:'',fromUTC:'',throughUTC:'',exactFromUTC:'',exactBeforeUTC:'',operationID:'',attemptID:''}, requestID:0, beforeSequence:null, nextBeforeSequence:null, newerCursors:[]};
        taskEventPanels.set(id, state);
    }
    return state;
}

function taskEventKindLabel(kind) { return taskEventFilterKinds[kind] || kind; }

function taskEventFilterMarkup(id, state) {
    const options = Object.entries(taskEventFilterKinds).map(([kind, label]) => `<option value="${kind}"${state.filter.kind === kind ? ' selected' : ''}>${escapeHTML(label)}</option>`).join('');
    return `<form class="task-event-filter" data-task-event-filter="1" data-event-task-id="${id}">
    <label>事件类型 <select name="kind"><option value=""${state.filter.kind === '' ? ' selected' : ''}>全部类型</option>${options}</select></label>
    <label>本机记录 UTC 起始日 <input type="date" name="from_utc" value="${escapeHTML(state.filter.fromUTC)}"></label>
    <label>本机记录 UTC 截止日 <input type="date" name="through_utc" value="${escapeHTML(state.filter.throughUTC)}"></label>
    <label>本机记录 UTC 精确起点 <input type="datetime-local" step="1" name="recorded_from_utc" value="${escapeHTML(state.filter.exactFromUTC)}"></label>
    <label>本机记录 UTC 精确截止（不含） <input type="datetime-local" step="1" name="recorded_before_utc" value="${escapeHTML(state.filter.exactBeforeUTC)}"></label>
    <label>线上操作 ID <input type="search" name="operation_id" maxlength="128" value="${escapeHTML(state.filter.operationID)}"></label>
    <label>下载尝试 ID <input type="search" name="attempt_id" maxlength="128" value="${escapeHTML(state.filter.attemptID)}"></label>
    <button type="submit"${state.loading ? ' disabled' : ''}>筛选事件</button>
    <button type="button" data-task-events="clear-filter" data-event-task-id="${id}"${state.loading ? ' disabled' : ''}>清除筛选</button>
  </form>`;
}

function eventTime(label, value) {
    return value ? `<div><strong>${label}（UTC）:</strong> ${escapeHTML(value)}</div>` : '';
}

function localStorageKindText(local) {
    if (local?.kind === 'local_storage_full') return '写入时磁盘空间耗尽';
    if (local?.kind === 'local_storage_blocked') {
        const available = Number(local.available_bytes);
        return Number.isSafeInteger(available) && available >= 0 ? '下载前可用空间不足' : '下载前无法确认可用空间';
    }
    return '本地存储问题';
}

function localStorageByteText(value) {
    const bytes = Number(value);
    if (!Number.isSafeInteger(bytes) || bytes < 0) return '未知';
    return `${formatBytes(bytes)}（${bytes} 字节）`;
}

function localStorageDiagnosticMarkup(local) {
    if (!local || typeof local !== 'object') return '';
    const available = Number(local.available_bytes);
    const availableText = Number.isSafeInteger(available) && available >= 0 ? localStorageByteText(available) : '无法读取';
    return `<section class="local-storage-diagnostic" aria-label="本地存储诊断">
  <strong>${escapeHTML(localStorageKindText(local))}</strong>
  <div><strong>本地目标路径:</strong> ${escapeHTML(local.path || '未知')}</div>
  <div><strong>本次所需空间:</strong> ${escapeHTML(localStorageByteText(local.required_bytes))}</div>
  <div><strong>核对时可用空间:</strong> ${escapeHTML(availableText)}</div>
  <div><strong>其他下载预留:</strong> ${escapeHTML(localStorageByteText(local.reserved_bytes))}</div>
  ${eventTime('本机空间核对时间', local.checked_at_utc)}
  <div>释放空间或确认目标磁盘可用后，再手动点击“重试任务”。程序不会自动清理文件或重试。</div>
</section>`;
}

function taskEventMarkup(state, id = '') {
    if (state.loading) return '<p>正在读取事件…</p>';
    if (state.error) return `<p class="event-error">${escapeHTML(state.error)}</p>`;
    if (!state.loaded) return '<p>展开后读取该任务的 SQLite 事件。</p>';
    const count = `<p class="task-event-count">当前筛选命中 ${state.totalMatches} 条；本页 ${state.events.length} 条。</p>` + taskEventKindCountsMarkup(state, id);
    if (state.events.length === 0) return count + '<p>当前筛选没有匹配的事件记录。</p>';
    return count + state.events.map(event => {
        const evidence = event.evidence ?? null;
        const retryAt = evidence && typeof evidence === 'object' ? evidence.next_attempt_at_utc : null;
        const storage = evidence && typeof evidence === 'object' ? evidence.local_storage : null;
        const kind = storage ? localStorageKindText(storage) : taskEventKindLabel(event.kind);
        const sequence = String(event.sequence);
        const context = state.contexts?.[sequence];
        const copyStatus = state.copyStatus?.[sequence] || '';
        return `<article class="task-event">
  <div class="task-event-title">#${escapeHTML(event.sequence)} · ${escapeHTML(kind)}</div>
  ${eventTime('本机记录时间', event.recorded_at_utc)}
  ${eventTime('提供方发生时间', event.occurred_at_utc)}
  ${eventTime('云端扫描观察时间', event.cloud_observed_at_utc)}
  ${eventTime('HTTP 响应观察时间', event.response_observed_at_utc)}
  ${eventTime('未来重试计划时间', retryAt)}
  ${localStorageDiagnosticMarkup(storage)}
  <div><strong>证据类型:</strong> ${escapeHTML(event.evidence_type)}</div>
  ${event.run_id ? `<div><strong>运行 ID:</strong> ${escapeHTML(event.run_id)}</div>` : ''}
  ${event.operation_id ? `<div><strong>线上操作 ID:</strong> ${escapeHTML(event.operation_id)}</div>${id ? `<button type="button" data-task-events="correlate" data-event-task-id="${id}" data-correlation-kind="operation_id" data-correlation-value="${escapeHTML(event.operation_id)}">按此操作 ID 筛选</button>` : ''}` : ''}
  ${event.attempt_id ? `<div><strong>尝试 ID:</strong> ${escapeHTML(event.attempt_id)}</div>${id ? `<button type="button" data-task-events="correlate" data-event-task-id="${id}" data-correlation-kind="attempt_id" data-correlation-value="${escapeHTML(event.attempt_id)}">按此尝试 ID 筛选</button>` : ''}` : ''}
  ${id && event.recorded_at_utc ? `<button type="button" data-task-events="event-day" data-event-task-id="${id}" data-event-recorded-at="${escapeHTML(event.recorded_at_utc)}">筛选本 UTC 日</button>` : ''}
  ${id ? `<button type="button" data-task-events="context" data-event-task-id="${id}" data-event-sequence="${escapeHTML(sequence)}">${context?.data ? '收起前后事件' : '查看前后事件'}</button><button type="button" data-task-events="copy" data-event-task-id="${id}" data-event-sequence="${escapeHTML(sequence)}">复制脱敏 JSON</button>${copyStatus ? `<span role="status" class="task-event-copy-status">${escapeHTML(copyStatus)}</span>` : ''}` : ''}
  ${context?.loading ? '<p>正在读取前后事件…</p>' : ''}${context?.error ? `<p class="event-error" role="status">${escapeHTML(context.error)}</p>` : ''}${context?.data ? taskEventContextMarkup(context.data) : ''}
  <details><summary>结构化证据</summary><pre>${escapeHTML(JSON.stringify(evidence, null, 2))}</pre></details>
</article>`;
    }).join('');
}

function taskEventKindCountsMarkup(state, id) {
    if (!Array.isArray(state.kindCounts) || state.kindCounts.length === 0) return '';
    const counts = state.kindCounts.filter(item => Number.isSafeInteger(item.count) && item.count > 0);
    if (counts.length === 0) return '<details class="task-event-kind-counts"><summary>按类型统计：当前范围内均为 0</summary></details>';
    const buttons = counts.map(item => item.kind === 'other' ? `<span>其他/未识别事件：${item.count}</span>` : `<button type="button" data-task-events="kind-count" data-event-task-id="${id}" data-event-kind="${escapeHTML(item.kind)}">${escapeHTML(taskEventKindLabel(item.kind))}：${item.count}</button>`).join('');
    return `<details class="task-event-kind-counts"><summary>按类型统计（当前非类型筛选范围）</summary><div>${buttons}</div></details>`;
}

function taskEventContextMarkup(value) {
    const item = event => `<li${event.sequence === value.anchor?.sequence ? ' class="task-event-context-anchor"' : ''}><strong>#${escapeHTML(event.sequence)} · ${escapeHTML(taskEventKindLabel(event.kind))}</strong> · ${escapeHTML(event.recorded_at_utc || '记录时间未知')}</li>`;
    return `<div class="task-event-context"><strong>前后事件（最多各 5 条，按序号）</strong><ol>${(value.before || []).map(item).join('')}${value.anchor ? item(value.anchor) : ''}${(value.after || []).map(item).join('')}</ol></div>`;
}

function taskEventsControl(task) {
    const id = typeof task?.task_id === 'string' && /^[1-9][0-9]{0,18}$/.test(task.task_id) ? task.task_id : '';
    if (!id || task.is_root !== true) return '';
    const state = taskEventPanels.get(id) || {expanded:false, loaded:false, loading:false, exporting:false, exportError:'', error:'', events:[], totalMatches:0, kindCounts:[], contexts:{}, copyStatus:{}, filter:{kind:'',fromUTC:'',throughUTC:'',exactFromUTC:'',exactBeforeUTC:'',operationID:'',attemptID:''}, beforeSequence:null, nextBeforeSequence:null, newerCursors:[]};
    return `<div class="task-events">
  <button type="button" data-task-events="toggle" data-event-task-id="${id}" aria-expanded="${state.expanded}">${state.expanded ? '收起事件' : '查看事件'}</button>
  ${state.expanded ? `<section class="task-event-panel" aria-label="任务事件时间线">
    ${taskEventFilterMarkup(id, state)}
    <div class="task-event-actions"><button type="button" data-task-events="refresh" data-event-task-id="${id}"${state.loading ? ' disabled' : ''}>刷新事件</button><button type="button" data-task-events="export" data-event-task-id="${id}"${state.exporting ? ' disabled' : ''}>${state.exporting ? '正在导出…' : '导出当前筛选证据'}</button>${state.newerCursors.length ? `<button type="button" data-task-events="newer" data-event-task-id="${id}"${state.loading ? ' disabled' : ''}>返回较新事件</button>` : ''}${state.nextBeforeSequence !== null ? `<button type="button" data-task-events="older" data-event-task-id="${id}"${state.loading ? ' disabled' : ''}>更早事件</button>` : ''}</div>
    ${state.exportError ? `<p class="event-error" role="status">${escapeHTML(state.exportError)}</p>` : ''}${taskEventMarkup(state, id)}
  </section>` : ''}
</div>`;
}

async function fetchTaskEventPage(id, beforeSequence) {
    const state = taskEventState(id);
    invalidateTaskEventDetails(state);
    const requestID = ++state.requestID;
    const filter = {...state.filter};
    state.loading = true; state.error = '';
    try {
        const query = new URLSearchParams({limit:String(taskEventPageSize)});
        if (beforeSequence !== null) query.set('before_sequence', String(beforeSequence));
        if (filter.kind) query.set('kind', filter.kind);
        if (filter.fromUTC) query.set('from_utc', filter.fromUTC);
        if (filter.throughUTC) query.set('through_utc', filter.throughUTC);
        if (filter.exactFromUTC) query.set('recorded_from_utc', exactTaskEventUTC(filter.exactFromUTC));
        if (filter.exactBeforeUTC) query.set('recorded_before_utc', exactTaskEventUTC(filter.exactBeforeUTC));
        if (filter.operationID) query.set('operation_id', filter.operationID);
        if (filter.attemptID) query.set('attempt_id', filter.attemptID);
        const response = await fetch(`/api/tasks/${id}/events?${query}`);
        if (requestID !== state.requestID) return false;
        if (!response.ok) {
            state.error = response.status === 401 ? adminSessionExpired() : '事件读取失败，请稍后重试。';
            state.loaded = true;
            return;
        }
        const page = await response.json();
        if (requestID !== state.requestID) return false;
        const incoming = Array.isArray(page.events) ? page.events : [];
        state.events = incoming;
        state.totalMatches = Number.isSafeInteger(page.total_matches) && page.total_matches >= 0 ? page.total_matches : incoming.length;
        state.kindCounts = Array.isArray(page.kind_counts) ? page.kind_counts.filter(item => item && typeof item.kind === 'string' && Number.isSafeInteger(item.count) && item.count >= 0) : [];
        state.nextBeforeSequence = page.next_before_sequence ?? null;
        state.loaded = true;
        return true;
    } catch {
        if (requestID !== state.requestID) return false;
        state.error = '事件读取失败，请检查服务连接后重试。';
        state.loaded = true;
        return false;
    } finally { if (requestID === state.requestID) state.loading = false; }
}

function invalidateTaskEventDetails(state) {
    state.contexts ||= {};
    state.contextRequestIDs ||= {};
    for (const key of Object.keys(state.contextRequestIDs)) state.contextRequestIDs[key]++;
    state.contexts = {};
    state.copyStatus = {};
}

async function applyTaskEventFilters(id, kind, fromUTC, throughUTC, operationID = '', attemptID = '', exactFromUTC = '', exactBeforeUTC = '') {
    const state = taskEventState(id);
    state.filter = {kind:typeof kind === 'string' ? kind : '', fromUTC:typeof fromUTC === 'string' ? fromUTC : '', throughUTC:typeof throughUTC === 'string' ? throughUTC : '', exactFromUTC:typeof exactFromUTC === 'string' ? exactFromUTC : '', exactBeforeUTC:typeof exactBeforeUTC === 'string' ? exactBeforeUTC : '', operationID:typeof operationID === 'string' ? operationID : '', attemptID:typeof attemptID === 'string' ? attemptID : ''};
    state.beforeSequence = null; state.nextBeforeSequence = null; state.newerCursors = [];
    state.totalMatches = 0; state.events = [];
    if (state.filter.fromUTC && state.filter.throughUTC && state.filter.fromUTC > state.filter.throughUTC) {
        state.requestID++; state.loading = false; state.loaded = true; state.error = 'UTC 起始日不能晚于截止日。';
        return false;
    }
    if ((state.filter.fromUTC || state.filter.throughUTC) && (state.filter.exactFromUTC || state.filter.exactBeforeUTC)) {
        state.requestID++; state.loading = false; state.loaded = true; state.error = 'UTC 日期范围和精确时间范围不能同时使用。';
        return false;
    }
    const exactFrom = exactTaskEventUTC(state.filter.exactFromUTC), exactBefore = exactTaskEventUTC(state.filter.exactBeforeUTC);
    if (exactFrom === null || exactBefore === null || (exactFrom && exactBefore && exactFrom >= exactBefore)) {
        state.requestID++; state.loading = false; state.loaded = true; state.error = '请填写有效且起点早于截止点的 UTC 精确时间。';
        return false;
    }
    return fetchTaskEventPage(id, null);
}

function exactTaskEventUTC(value) {
    if (!value) return '';
    return /^\d{4}-\d\d-\d\dT\d\d:\d\d(?::\d\d)?$/.test(value) ? `${value.length === 16 ? value + ':00' : value}Z` : null;
}

async function clearTaskEventFilters(id) { return applyTaskEventFilters(id, '', '', ''); }

async function applyTaskEventCorrelationFilter(id, kind, value) {
    if (!['operation_id','attempt_id'].includes(kind) || typeof value !== 'string' || value.length === 0) return false;
    const state = taskEventState(id);
    return applyTaskEventFilters(id, state.filter.kind, state.filter.fromUTC, state.filter.throughUTC,
        kind === 'operation_id' ? value : '', kind === 'attempt_id' ? value : '', state.filter.exactFromUTC, state.filter.exactBeforeUTC);
}

async function loadTaskEventContext(id, sequence) {
    const state = taskEventState(id);
    if (!/^[1-9][0-9]*$/.test(String(sequence))) return false;
    const key = String(sequence);
    if (state.contexts[key]?.data) { delete state.contexts[key]; return true; }
    state.contexts ||= {}; state.contextRequestIDs ||= {};
    const requestID = state.contextRequestIDs[key] = (state.contextRequestIDs[key] || 0) + 1;
    state.contexts[key] = {loading:true};
    try {
        const response = await fetch(`/api/tasks/${id}/events/${encodeURIComponent(key)}/context`);
        if (state.contextRequestIDs[key] !== requestID) return false;
        if (!response.ok) { state.contexts[key] = {error:response.status === 401 ? adminSessionExpired() : '前后事件读取失败。'}; return false; }
        const data = await response.json();
        if (state.contextRequestIDs[key] !== requestID) return false;
        state.contexts[key] = {data};
        return true;
    } catch {
        if (state.contextRequestIDs[key] !== requestID) return false;
        state.contexts[key] = {error:'前后事件读取失败，请检查服务连接后重试。'};
        return false;
    }
}

async function copyTaskEventJSON(id, sequence) {
    const state = taskEventState(id);
    const event = state.events.find(item => String(item.sequence) === String(sequence));
    const clipboard = globalThis.navigator?.clipboard;
    if (!event || !clipboard || typeof clipboard.writeText !== 'function') {
        state.copyStatus[String(sequence)] = '复制不可用，请使用结构化证据导出。'; return false;
    }
    try {
        await navigator.clipboard.writeText(JSON.stringify(event, null, 2));
        state.copyStatus[String(sequence)] = '已复制（敏感字段已遮蔽）。';
        return true;
    } catch {
        state.copyStatus[String(sequence)] = '浏览器未允许剪贴板访问。';
        return false;
    }
}

async function applyTaskEventDayFilter(id, recordedAt) {
    const state = taskEventState(id);
    const date = new Date(recordedAt);
    if (!Number.isFinite(date.getTime())) return false;
    return applyTaskEventFilters(id, state.filter.kind, date.toISOString().slice(0,10), date.toISOString().slice(0,10), state.filter.operationID, state.filter.attemptID);
}

async function applyTaskEventKindCount(id, kind) {
    if (!taskEventFilterKinds[kind]) return false;
    const state = taskEventState(id);
    return applyTaskEventFilters(id, kind, state.filter.fromUTC, state.filter.throughUTC, state.filter.operationID, state.filter.attemptID, state.filter.exactFromUTC, state.filter.exactBeforeUTC);
}

async function toggleTaskEvents(id) {
    if (taskEventPanels.has(id)) { taskEventPanels.delete(id); return; }
    taskEventPanels.clear();
    const state = taskEventState(id);
    if (await fetchTaskEventPage(id, null)) {
        state.beforeSequence = null;
        state.newerCursors = [];
    }
}

async function refreshTaskEvents(id) {
    const state = taskEventPanels.get(id);
    if (!state) return;
    if (await fetchTaskEventPage(id, null)) {
        state.beforeSequence = null;
        state.newerCursors = [];
    }
}
async function loadOlderTaskEvents(id) {
    const state = taskEventPanels.get(id);
    if (!state || state.loading || state.nextBeforeSequence === null) return;
    const currentBefore = state.beforeSequence;
    const olderBefore = state.nextBeforeSequence;
    if (await fetchTaskEventPage(id, olderBefore)) {
        state.newerCursors.push(currentBefore);
        state.beforeSequence = olderBefore;
    }
}
async function loadNewerTaskEvents(id) {
    const state = taskEventPanels.get(id);
    if (!state || state.loading || !state.newerCursors.length) return;
    const newerBefore = state.newerCursors[state.newerCursors.length - 1];
    if (await fetchTaskEventPage(id, newerBefore)) {
        state.newerCursors.pop();
        state.beforeSequence = newerBefore;
    }
}

async function exportTaskEventEvidence(id) {
    if (typeof id !== 'string' || !/^[1-9][0-9]{0,18}$/.test(id)) return false;
    const state = taskEventState(id);
    if (state.exporting) return false;
    state.exporting = true; state.exportError = '';
    try {
        const query = new URLSearchParams();
        if (state.filter.kind) query.set('kind', state.filter.kind);
        if (state.filter.fromUTC) query.set('from_utc', state.filter.fromUTC);
        if (state.filter.throughUTC) query.set('through_utc', state.filter.throughUTC);
        if (state.filter.exactFromUTC) query.set('recorded_from_utc', exactTaskEventUTC(state.filter.exactFromUTC));
        if (state.filter.exactBeforeUTC) query.set('recorded_before_utc', exactTaskEventUTC(state.filter.exactBeforeUTC));
        if (state.filter.operationID) query.set('operation_id', state.filter.operationID);
        if (state.filter.attemptID) query.set('attempt_id', state.filter.attemptID);
        const suffix = query.toString();
        const response = await fetch(`/api/tasks/${id}/events/export${suffix ? `?${suffix}` : ''}`);
        if (!response.ok) {
            state.exportError = response.status === 401 ? adminSessionExpired() :
                response.status === 413 ? '当前筛选匹配超过 100 条，未导出部分证据；请缩小筛选条件。' : '事件证据导出失败，请稍后重试。';
            return false;
        }
        const blob = await response.blob();
        const disposition = response.headers?.get('Content-Disposition') || '';
        const match = disposition.match(/filename="([A-Za-z0-9._-]+)"/i);
        const link = document.createElement('a');
        link.href = URL.createObjectURL(blob);
        link.download = match ? match[1] : `task-events-${id}.json`;
        link.click();
        setTimeout(() => URL.revokeObjectURL(link.href), 0);
        return true;
    } catch {
        state.exportError = '事件证据导出失败，请检查服务连接后重试。';
        return false;
    } finally { state.exporting = false; }
}

function taskControls(task) {
    if (!task.is_root) return '';
    const mode = task.download_mode === 'delete' ? '完成后删除网盘项' : '保留并标记网盘项';
    const id = typeof task.task_id === 'string' && /^[1-9][0-9]{0,18}$/.test(task.task_id) ? task.task_id : '';
    const disabled = pendingTaskActions.has(id) ? ' disabled' : '';
    const button = (action, label) => `<button type="button" data-task-id="${id}" data-action="${action}"${disabled}>${label}</button>`;
    return `<div class="task-controls"><span>此任务：${mode} · 已启动 ${Number(task.attempts) || 0} 次</span>` +
        (task.status === 'paused' ? '<span>此项已暂停；继续后会从已验证的本地数据恢复。</span>' : '') +
        (task.next_attempt ? `<span>下次重试：${escapeHTML(formatTime(task.next_attempt))}</span>` : '') +
        (id && task.can_cancel === true ? button('cancel', '取消任务') : '') +
        (id && task.can_pause === true ? button('pause', '暂停此任务') : '') +
        (id && task.can_resume === true ? button('resume', '继续此任务') : '') +
        (id && task.can_retry === true ? button('retry', '重试任务') : '') + '</div>';
}
function taskFeedback(message) { document.getElementById('task-feedback').textContent = message; }
function actionFailure(status) {
    if (status === 409) return '任务状态已改变、仍在停止或尚未完成扫描，请刷新后重试。';
    if (status === 404) return '任务已不存在，请刷新任务列表。';
    if (status === 401) return adminSessionExpired();
    if (status === 503) return '队列存储暂不可用，操作未确认成功；检查本地状态存储后重试。';
    return '操作失败，请检查服务状态后重试。';
}
async function performTaskAction(id, action, refresh) {
    if (typeof id !== 'string' || !/^[1-9][0-9]{0,18}$/.test(id) || !['cancel','retry','pause','resume'].includes(action) || pendingTaskActions.has(id)) return;
    pendingTaskActions.add(id); taskFeedback('正在提交任务操作…');
    try {
        const response = await fetch(`/api/tasks/${id}/${action}`, {method:'POST'});
        if (!response.ok) throw new Error(actionFailure(response.status));
        const message = {cancel:'取消已提交；任务停止后才可重试。已发出的网盘操作不能撤回。',retry:'重试已提交，将按此任务原有策略执行。',pause:'暂停已记录；正在下载的任务停止后即可继续。',resume:'继续已记录，将按原队列条件恢复此任务。'};
        taskFeedback(message[action]);
    } catch (error) { taskFeedback(error.message || '网络连接失败。'); }
    finally { pendingTaskActions.delete(id); try { await refresh(); } catch {} }
}
function bindTaskControls(refresh) {
    const tasksList = document.getElementById('tasks-list');
    tasksList.addEventListener('submit', async event => {
        const form = event.target.closest('form[data-task-event-filter]');
        if (!form) return;
        event.preventDefault();
        const read = name => form.querySelector(`[name="${name}"]`)?.value || '';
        await applyTaskEventFilters(form.dataset.eventTaskId, read('kind'), read('from_utc'), read('through_utc'), read('operation_id'), read('attempt_id'), read('recorded_from_utc'), read('recorded_before_utc'));
        await refresh();
    });
    tasksList.addEventListener('click', async event => {
        const eventButton = event.target.closest('button[data-task-events]');
        if (eventButton) {
            const id = eventButton.dataset.eventTaskId;
            if (eventButton.dataset.taskEvents === 'toggle') await toggleTaskEvents(id);
            else if (eventButton.dataset.taskEvents === 'refresh') await refreshTaskEvents(id);
            else if (eventButton.dataset.taskEvents === 'export') await exportTaskEventEvidence(id);
            else if (eventButton.dataset.taskEvents === 'clear-filter') await clearTaskEventFilters(id);
            else if (eventButton.dataset.taskEvents === 'correlate') await applyTaskEventCorrelationFilter(id, eventButton.dataset.correlationKind, eventButton.dataset.correlationValue);
            else if (eventButton.dataset.taskEvents === 'context') await loadTaskEventContext(id, eventButton.dataset.eventSequence);
            else if (eventButton.dataset.taskEvents === 'copy') await copyTaskEventJSON(id, eventButton.dataset.eventSequence);
            else if (eventButton.dataset.taskEvents === 'event-day') await applyTaskEventDayFilter(id, eventButton.dataset.eventRecordedAt);
            else if (eventButton.dataset.taskEvents === 'kind-count') await applyTaskEventKindCount(id, eventButton.dataset.eventKind);
            else if (eventButton.dataset.taskEvents === 'older') await loadOlderTaskEvents(id);
            else if (eventButton.dataset.taskEvents === 'newer') await loadNewerTaskEvents(id);
            await refresh();
            return;
        }
        const button = event.target.closest('button[data-task-id]');
        if (!button) return;
        button.disabled = true;
        await performTaskAction(button.dataset.taskId, button.dataset.action, refresh);
    });
}
async function triggerScan() {
    const button = document.getElementById('scan-button'); button.disabled = true;
    try {
        const response = await fetch('/api/scan', {method:'POST'});
        if (!response.ok) throw new Error(actionFailure(response.status));
        taskFeedback('扫描请求已提交；账号未授权时会等待授权。扫描完成后更新任务列表。');
    } catch (error) { taskFeedback(error.message || '无法提交扫描。'); }
    finally { button.disabled = false; }
}
// 共享的工具函数
// Capacity audit found binary KB labels and missing units above GiB.
// Keep integer rounding consistent with frontend/src/format-bytes.ts.
function formatBytes(value) {
    if (value === null || value === undefined) return '—';
    if (typeof value === 'number') {
        if (!Number.isFinite(value) || value < 0) return '—';
        value = BigInt(Math.round(value));
    }
    let bytes;
    try {
        if (typeof value === 'string' && !/^(0|[1-9][0-9]*)$/.test(value)) return '—';
        bytes = BigInt(value);
    } catch { return '—'; }
    if (bytes < 0n) return '—';
    const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'];
    let power = 1n, unit = 0;
    while (unit < units.length - 1 && bytes >= power * 1024n) { power *= 1024n; unit++; }
    if (!unit) return bytes + ' B';
    let rounded = (bytes * 100n + power / 2n) / power;
    if (rounded >= 102400n && unit < units.length - 1) { power *= 1024n; unit++; rounded = (bytes * 100n + power / 2n) / power; }
    const fraction = (rounded % 100n).toString().padStart(2, '0').replace(/0+$/, '');
    return (rounded / 100n).toString() + (fraction ? '.' + fraction : '') + ' ' + units[unit];
}

function formatSpeed(bytesPerSecond) {
    const value = formatBytes(bytesPerSecond);
    return value === '—' ? value : value + '/s';
}

function formatTime(timeStr) {
    if (!timeStr) return '—';
    const date = new Date(timeStr);
    return date.toLocaleString('zh-CN', {
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit'
    });
}

function getStatusBadge(status) {
    const statusMap = {
        'pending': '等待中',
        'downloading': '下载中',
        'paused': '已暂停',
        'done': '已完成',
        'failed': '失败',
        'retry_wait': '等待重试',
        'auth_wait': '等待账号授权',
        'source_unknown': '网盘后处理待核对（禁止重发）',
        'cancelled': '已取消'
    };
    return `<span class="status-badge ${escapeHTML(status)}">${escapeHTML(statusMap[status] || status)}</span>`;
}

// 排序函数
function sortTasks(tasks, sortBy) {
    switch(sortBy) {
        case 'name':
            return [...tasks].sort((a, b) => {
                const nameA = (a.path || a.server_filename || '').toLowerCase();
                const nameB = (b.path || b.server_filename || '').toLowerCase();
                return nameA.localeCompare(nameB);
            });
        case 'progress':
            return [...tasks].sort((a, b) => {
                const progressA = a.size > 0 ? (a.progress / a.size) : 0;
                const progressB = b.size > 0 ? (b.progress / b.size) : 0;
                return progressB - progressA; // 降序排列，进度高的在前
            });
        case 'size':
            return [...tasks].sort((a, b) => b.size - a.size); // 降序排列，大的在前
        default:
            // 默认使用文件名排序
            return [...tasks].sort((a, b) => {
                const nameA = (a.path || a.server_filename || '').toLowerCase();
                const nameB = (b.path || b.server_filename || '').toLowerCase();
                return nameA.localeCompare(nameB);
            });
    }
}
