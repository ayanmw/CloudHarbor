(() => {
    const $ = id => document.getElementById(id);
    const pageSize = 50;
    const state = {filters:null, asOfSequence:null, cursors:[null], pageIndex:0, nextBefore:null, hasMore:false, events:[], kindCounts:[], total:0, requestID:0, loading:false, exporting:false};
    const keys = {taskID:'task-event-task-id', kind:'task-event-kind', operationID:'task-event-operation-id', attemptID:'task-event-attempt-id', fromUTC:'task-event-from-date', throughUTC:'task-event-through-date', exactFromUTC:'task-event-from-exact', exactBeforeUTC:'task-event-before-exact'};
    const utc = value => value ? `${escapeHTML(String(value).replace('T',' ').replace(/Z$/, ' UTC'))}` : '';

    function readFilters() {
        const value = id => String($(id).value ?? '');
        return {
            task_id:value(keys.taskID).trim(), kind:value(keys.kind),
            operation_id:value(keys.operationID).trim(), attempt_id:value(keys.attemptID).trim(),
            from_utc:value(keys.fromUTC), through_utc:value(keys.throughUTC),
            recorded_from_utc:value(keys.exactFromUTC), recorded_before_utc:value(keys.exactBeforeUTC)
        };
    }

    function validateFilters(filters) {
        if (filters.task_id && (!/^\d+$/.test(filters.task_id) || /^0+$/.test(filters.task_id))) return '任务 ID 必须是正整数。';
        if ((filters.from_utc || filters.through_utc) && (filters.recorded_from_utc || filters.recorded_before_utc)) return 'UTC 日期范围和精确时间范围不能同时使用。';
        if (filters.from_utc && filters.through_utc && filters.through_utc < filters.from_utc) return 'UTC 起始日不能晚于截止日。';
        if (filters.operation_id.length > 128 || filters.attempt_id.length > 128) return '关联 ID 最多 128 个字符。';
        if (filters.operation_id !== filters.operation_id.trim() || filters.attempt_id !== filters.attempt_id.trim()) return '关联 ID 前后不能有空格。';
        const from = exactTaskEventUTC(filters.recorded_from_utc), before = exactTaskEventUTC(filters.recorded_before_utc);
        if (from === null || before === null || (from && before && from >= before)) return '精确 UTC 范围必须有效，且起点早于不含的截止点。';
        return '';
    }

    function queryParams(includeSnapshot = true, includeCursor = false) {
        const query = new URLSearchParams({limit:String(pageSize)});
        const filters = state.filters || readFilters();
        for (const [key, value] of Object.entries(filters)) {
            if (!value) continue;
            if (key === 'recorded_from_utc' || key === 'recorded_before_utc') query.set(key, exactTaskEventUTC(value));
            else query.set(key, value);
        }
        if (includeSnapshot && state.asOfSequence !== null) query.set('as_of_sequence', state.asOfSequence);
        if (includeCursor) {
            const before = state.cursors[state.pageIndex];
            if (before) query.set('before_sequence', before);
        }
        return query;
    }

    function renderKindCounts() {
        const kinds = Array.isArray(state.kindCounts) ? state.kindCounts : [];
        $('task-event-kinds').innerHTML = kinds.filter(item => Number(item.count) > 0).map(item => {
            const label = item.kind === 'other' ? '其他历史类型' : taskEventKindLabel(item.kind);
            if (item.kind === 'other') return `<span class="muted">${escapeHTML(label)}：${Number(item.count)}</span>`;
            return `<button type="button" data-task-event-kind="${escapeHTML(item.kind)}">${escapeHTML(label)}：${Number(item.count)}</button>`;
        }).join('') || '<span class="muted">当前筛选没有事件</span>';
    }

    function renderEvents() {
        $('task-event-list').innerHTML = state.events.map(event => `<article class="event"><div class="event-meta"><strong>${escapeHTML(taskEventKindLabel(event.kind))}</strong> · sequence ${escapeHTML(event.sequence)}<br>任务 ID：${escapeHTML(event.job_id)}<br>本机记录 UTC：${utc(event.recorded_at_utc)}${event.occurred_at_utc ? `<br>事件发生 UTC：${utc(event.occurred_at_utc)}` : ''}${event.cloud_observed_at_utc ? `<br>百度观察 UTC：${utc(event.cloud_observed_at_utc)}` : ''}${event.response_observed_at_utc ? `<br>百度响应 UTC：${utc(event.response_observed_at_utc)}` : ''}${event.operation_id ? `<br>远端操作 ID：${escapeHTML(event.operation_id)}` : ''}${event.attempt_id ? `<br>下载尝试 ID：${escapeHTML(event.attempt_id)}` : ''}${event.run_id ? `<br>运行 ID：${escapeHTML(event.run_id)}` : ''}</div><pre>${escapeHTML(JSON.stringify(event.evidence ?? {}, null, 2))}</pre></article>`).join('') || '<p class="muted">当前筛选没有匹配的事件记录。</p>';
    }

    function render() {
        renderKindCounts(); renderEvents();
        const start = state.total === 0 ? 0 : state.pageIndex * pageSize + 1;
        const end = Math.min(state.pageIndex * pageSize + state.events.length, state.total);
        $('task-event-summary').textContent = `快照上界 sequence ${state.asOfSequence ?? '读取中'} · 匹配 ${state.total} 条 · 当前显示 ${start}–${end}`;
        $('task-event-prev').disabled = state.loading || state.pageIndex === 0;
        $('task-event-next').disabled = state.loading || !state.hasMore;
        $('task-event-search').disabled = state.loading;
        $('task-event-refresh').disabled = state.loading;
        $('task-event-export').disabled = state.loading || state.exporting || state.asOfSequence === null;
    }

    async function loadPage() {
        const requestID = ++state.requestID; state.loading = true; $('task-event-error').textContent = ''; render();
        try {
            const response = await fetch('/api/task-events?' + queryParams(true, true), {credentials:'same-origin'});
            if (!response.ok) throw new Error(response.status === 401 ? adminSessionExpired() : ((await response.text()) || `HTTP ${response.status}`));
            const data = await response.json();
            if (requestID !== state.requestID) return;
            if (typeof data.as_of_sequence !== 'string' || !/^\d+$/.test(data.as_of_sequence)) throw new Error('事件历史快照无效，请刷新后重试。');
            if (state.asOfSequence !== null && data.as_of_sequence !== state.asOfSequence) throw new Error('事件历史快照已变化，请刷新后重新查询。');
            state.asOfSequence = data.as_of_sequence; state.events = Array.isArray(data.events) ? data.events : [];
            state.kindCounts = Array.isArray(data.kind_counts) ? data.kind_counts : []; state.total = Number(data.total_matches) || 0;
            state.nextBefore = typeof data.next_before_sequence === 'string' ? data.next_before_sequence : null; state.hasMore = data.has_more === true;
        } catch (error) {
            if (requestID !== state.requestID) return;
            $('task-event-error').textContent = error.message; state.events = []; state.hasMore = false; state.nextBefore = null;
        } finally {
            if (requestID === state.requestID) { state.loading = false; render(); }
        }
    }

    function startSearch(filters = readFilters()) {
        const error = validateFilters(filters);
        if (error) { state.requestID++; state.loading = false; $('task-event-error').textContent = error; return false; }
        state.filters = filters; state.asOfSequence = null; state.cursors = [null]; state.pageIndex = 0; state.nextBefore = null; state.hasMore = false; state.events = []; state.kindCounts = []; state.total = 0;
        return loadPage();
    }

    function olderPage() {
        if (state.loading || !state.hasMore || !state.nextBefore) return;
        state.cursors = state.cursors.slice(0, state.pageIndex + 1); state.cursors.push(state.nextBefore); state.pageIndex++; loadPage();
    }

    function newerPage() { if (!state.loading && state.pageIndex > 0) { state.pageIndex--; loadPage(); } }

    async function exportGlobalTaskEventHistory() {
        if (state.loading || state.exporting || state.asOfSequence === null) return;
        const requestID = state.requestID; state.exporting = true; $('task-event-export-status').textContent = '正在导出当前快照的脱敏证据…'; render();
        try {
            const params = queryParams(true, false); params.delete('limit');
            const response = await fetch('/api/task-events/export?' + params.toString(), {credentials:'same-origin'});
            if (!response.ok) throw new Error(response.status === 401 ? adminSessionExpired() : ((await response.text()) || `HTTP ${response.status}`));
            if (requestID !== state.requestID) return;
            const blob = await response.blob(); if (requestID !== state.requestID) return;
            const disposition = response.headers.get('Content-Disposition') || '';
            const match = disposition.match(/filename="(task-events-global-[A-Za-z0-9._-]+\.json)"/i);
            const link = document.createElement('a'); link.href = URL.createObjectURL(blob); link.download = match ? match[1] : 'task-events-global-evidence.json'; document.body.append(link); link.click(); link.remove(); setTimeout(() => URL.revokeObjectURL(link.href), 60000);
            $('task-event-export-status').textContent = '当前快照证据已开始下载。';
        } catch (error) { if (requestID === state.requestID) $('task-event-export-status').textContent = error.message; }
        finally { if (requestID === state.requestID) { state.exporting = false; render(); } }
    }

    $('task-event-filter').addEventListener('submit', event => { event.preventDefault(); startSearch(); });
    $('task-event-clear').addEventListener('click', () => { for (const id of Object.values(keys)) $(id).value = ''; $('task-event-error').textContent = ''; $('task-event-export-status').textContent = ''; startSearch(); });
    $('task-event-refresh').addEventListener('click', () => startSearch(state.filters || readFilters()));
    $('task-event-prev').addEventListener('click', newerPage); $('task-event-next').addEventListener('click', olderPage);
    $('task-event-export').addEventListener('click', exportGlobalTaskEventHistory);
    $('task-event-kinds').addEventListener('click', event => { const button = event.target.closest('[data-task-event-kind]'); if (!button) return; $(keys.kind).value = button.dataset.taskEventKind; startSearch(); });
    globalThis.globalTaskEventHistoryState = state;
    startSearch();
})();
