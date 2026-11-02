(() => {
    const $ = id => document.getElementById(id);
    const limit = 20;
    let pageCursors = [null];
    let pageIndex = 0;
    let nextCursor = null;
    let loaded = false;
    let operationRequest = 0;
    const eventPages = new Map();
    const eventNextBefore = new Map();
    const eventStatuses = new Map();
    const eventDateRanges = new Map();
    const eventRequests = new Map();
    const eventMessages = new Map();
    const eventLoading = new Set();

    function escapeHTML(value) {
        return String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
    }
    function statusLabel(status) {
        return ({prepared:'待确认',confirmed:'已确认',unknown:'结果未知（隔离，不可重发）',failed:'失败'})[status] || status;
    }
    function kindLabel(kind) {
        return ({mkdir:'新建目录',move:'移动',rename:'重命名',delete:'删除'})[kind] || kind;
    }
    function localEventSummary(evidence) {
        const descriptions = {
            local_cache_recovery_complete:'本地缓存恢复完成：只确认受影响目录已标为 stale，不表示任务队列已同步。',
            local_reconciliation_complete:'本地同步完成：缓存处理、队列身份处理和监控目录核对均已完成。',
            confirmed_cache_recovery:'已确认操作的缓存恢复：重启后将受影响目录标为 stale。',
            unknown_cache_recovery:'unknown 操作缓存恢复：重启后重试目录失效，unknown 隔离仍保留。',
            unknown_result_persistence_failed:'远端结果未知：unknown 状态未能写入 SQLite，来源已在当前进程隔离；启动恢复会重试。'
        };
        if (!evidence || typeof evidence !== 'object') return '';
        let summary = descriptions[evidence.event] || '';
        if (evidence.event === 'cache_refresh_failed') {
            summary = '本地缓存增量更新失败' + (evidence.reason ? '：' + evidence.reason : '。');
        }
        if (Array.isArray(evidence.invalidated_cache_paths) && evidence.invalidated_cache_paths.length) {
            summary += (summary ? '\n' : '') + '影响缓存路径：' + evidence.invalidated_cache_paths.join('、');
        }
        return summary;
    }
    function utc(value) {
        return value ? escapeHTML(value.replace('T',' ').replace('Z',' UTC')) : '未观察到';
    }
    async function getJSON(url) {
        const response = await fetch(url, {credentials:'same-origin',headers:{'Content-Type':'application/json'}});
        if (!response.ok) throw new Error(response.status === 401 ? adminSessionExpired() : ((await response.text()) || ('HTTP ' + response.status)));
        return response.json();
    }
    function localTimeAsUTC(id) {
        const value = $(id).value;
        if (!value) return '';
        const parsed = new Date(value);
        if (Number.isNaN(parsed.getTime())) throw new Error('请输入有效的本地日期时间');
        return parsed.toISOString();
    }
    function operationFilters() {
        const query = new URLSearchParams();
        if ($('remote-operation-status').value) query.set('status', $('remote-operation-status').value);
        if ($('remote-operation-kind').value) query.set('kind', $('remote-operation-kind').value);
        const search = ($('remote-operation-query').value || '').trim();
        if (search) query.set('q', search);
        const from = localTimeAsUTC('remote-operation-from');
        const to = localTimeAsUTC('remote-operation-to');
        if (from) query.set('from', from);
        if (to) query.set('to', to);
        return query;
    }
    function renderEvents(operationID, allEvents, nextBefore) {
        const target = $('remote-operation-events-' + operationID);
        if (!target) return;
        const ordered = allEvents.slice().reverse();
        const content = ordered.map(event => {
            const summary = localEventSummary(event.evidence);
            return [
                '事件 #' + event.sequence + ' · ' + statusLabel(event.status),
                '本机记录时间（UTC）：' + (event.recorded_at_utc || '未知'),
                '响应观测时间（UTC）：' + (event.response_observed_at_utc || '无'),
                ...(summary ? ['本地说明：' + summary] : []),
                '证据：' + JSON.stringify(event.evidence || {})
            ].join('\n');
        }).join('\n\n');
        const status = eventStatuses.get(operationID) || '';
        const range = eventDateRanges.get(operationID) || {from:'',through:''};
        const note = eventMessages.get(operationID);
        const options = [['','全部'],['prepared','待确认'],['confirmed','已确认'],['unknown','结果未知'],['failed','失败']].map(([value,label]) => '<option value="' + value + '"' + (value === status ? ' selected' : '') + '>' + label + '</option>').join('');
        const noteHTML = note ? '<p class="' + (eventLoading.has(operationID) ? 'muted' : 'message') + '" role="status" aria-live="polite">' + escapeHTML(note) + '</p>' : '';
        const emptyHTML = content || eventLoading.has(operationID) || note ? '' : '<p class="muted">当前筛选条件下没有事件。</p>';
        const olderHTML = nextBefore && !eventLoading.has(operationID) ? '<button type="button" data-older-events="' + escapeHTML(operationID) + '" data-before="' + nextBefore + '">读取更早事件</button>' : '';
        target.innerHTML = '<div class="operation-event-filter"><label>事件状态 <select id="remote-operation-event-status-' + escapeHTML(operationID) + '">' + options + '</select></label> <label>UTC 起始日 <input type="date" id="remote-operation-event-from-' + escapeHTML(operationID) + '" value="' + escapeHTML(range.from) + '"></label> <label>UTC 结束日 <input type="date" id="remote-operation-event-through-' + escapeHTML(operationID) + '" value="' + escapeHTML(range.through) + '"></label> <button type="button" data-filter-events="' + escapeHTML(operationID) + '">筛选事件</button> <button type="button" data-clear-event-filter="' + escapeHTML(operationID) + '">清除筛选</button> <button type="button" data-export-filtered-events="' + escapeHTML(operationID) + '">导出当前筛选证据</button></div>' + noteHTML + (content ? '<pre class="progress">' + escapeHTML(content) + '</pre>' : '') + emptyHTML + olderHTML;
        target.onclick = event => {
            const button = event.target.closest('[data-older-events]');
            if (button) loadEvents(button.dataset.olderEvents, Number(button.dataset.before));
            const apply = event.target.closest('[data-filter-events]');
            if (apply) {
                const id = apply.dataset.filterEvents;
                const from = $('remote-operation-event-from-' + id).value || '';
                const through = $('remote-operation-event-through-' + id).value || '';
                if ((from && !validUTCDate(from)) || (through && !validUTCDate(through)) || (from && through && from > through)) {
                    $('remote-operation-error').textContent = 'UTC 日期范围无效，请检查起始日和结束日。';
                    return;
                }
                $('remote-operation-error').textContent = '';
                eventStatuses.set(id, $('remote-operation-event-status-' + id).value || '');
                eventDateRanges.set(id, {from, through});
                eventPages.delete(id);
                eventNextBefore.delete(id);
                loadEvents(id, 0);
            }
            const clear = event.target.closest('[data-clear-event-filter]');
            if (clear) {
                const id = clear.dataset.clearEventFilter;
                $('remote-operation-error').textContent = '';
                eventStatuses.delete(id);
                eventDateRanges.delete(id);
                eventPages.delete(id);
                eventNextBefore.delete(id);
                loadEvents(id, 0);
            }
            const exportButton = event.target.closest('[data-export-filtered-events]');
            if (exportButton) exportFilteredEvents(exportButton.dataset.exportFilteredEvents, exportButton);
        };
    }
    function validUTCDate(value) {
        if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
        const parsed = new Date(value + 'T00:00:00Z');
        return !Number.isNaN(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value;
    }
    function eventFilterQuery(operationID) {
        const query = new URLSearchParams();
        const status = eventStatuses.get(operationID) || '';
        const range = eventDateRanges.get(operationID) || {from:'',through:''};
        if (status) query.set('status', status);
        if (range.from) query.set('recorded_from_utc', range.from);
        if (range.through) query.set('recorded_through_utc', range.through);
        return query;
    }
    async function loadEvents(operationID, beforeSequence) {
        const target = $('remote-operation-events-' + operationID);
        if (!target) return;
        const request = (eventRequests.get(operationID) || 0) + 1;
        eventRequests.set(operationID, request);
        eventLoading.add(operationID);
        eventMessages.set(operationID, '正在读取本地操作证据…');
        renderEvents(operationID, eventPages.get(operationID) || [], eventNextBefore.get(operationID));
        try {
            const query = eventFilterQuery(operationID);
            query.set('limit', '20');
            if (beforeSequence) query.set('before_sequence', String(beforeSequence));
            const result = await getJSON('/api/cloud/operations/' + encodeURIComponent(operationID) + '/events?' + query.toString());
            if (eventRequests.get(operationID) !== request) return;
            const current = beforeSequence ? (eventPages.get(operationID) || []) : [];
            const events = current.concat(result.events || []);
            eventPages.set(operationID, events);
            eventNextBefore.set(operationID, result.next_before_sequence || null);
            eventLoading.delete(operationID);
            eventMessages.delete(operationID);
            renderEvents(operationID, events, result.next_before_sequence);
        } catch (error) {
            if (eventRequests.get(operationID) !== request) return;
            eventLoading.delete(operationID);
            eventMessages.set(operationID, error.message);
            renderEvents(operationID, eventPages.get(operationID) || [], eventNextBefore.get(operationID));
        }
    }
    async function exportFilteredEvents(operationID, button) {
        if (button.disabled) return;
        button.disabled = true;
        try {
            const query = eventFilterQuery(operationID);
            const filterQuery = query.toString();
            const url = '/api/cloud/operations/' + encodeURIComponent(operationID) + '/events/export' + (filterQuery ? '?' + filterQuery : '');
            const response = await fetch(url, {credentials:'same-origin'});
            if (!response.ok) throw new Error(response.status === 401 ? adminSessionExpired() : ((await response.text()) || ('HTTP ' + response.status)));
            const blob = await response.blob();
            const disposition = response.headers.get('Content-Disposition') || '';
            const match = disposition.match(/filename="([A-Za-z0-9._-]+)"/i);
            const link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = match ? match[1] : 'remote-operation-events.json';
            document.body.append(link);
            link.click();
            link.remove();
            setTimeout(() => URL.revokeObjectURL(link.href), 60000);
            $('remote-operation-summary').textContent = '已开始下载：' + link.download;
            $('remote-operation-error').textContent = '';
        } catch (error) {
            $('remote-operation-error').textContent = error.message;
        } finally {
            button.disabled = false;
        }
    }
    async function exportOperationEvidence(operationID, button) {
        if (button.disabled) return;
        button.disabled = true;
        $('remote-operation-error').textContent = '';
        try {
            const response = await fetch('/api/cloud/operations/' + encodeURIComponent(operationID) + '/export', {credentials:'same-origin'});
            if (!response.ok) throw new Error(response.status === 401 ? adminSessionExpired() : ((await response.text()) || ('HTTP ' + response.status)));
            const blob = await response.blob();
            const disposition = response.headers.get('Content-Disposition') || '';
            const match = disposition.match(/filename="([A-Za-z0-9._-]+)"/i);
            const link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = match ? match[1] : 'remote-operation-evidence.json';
            document.body.append(link);
            link.click();
            link.remove();
            setTimeout(() => URL.revokeObjectURL(link.href), 60000);
            $('remote-operation-summary').textContent = '已开始下载：' + link.download;
        } catch (error) {
            $('remote-operation-error').textContent = error.message;
        } finally {
            button.disabled = false;
        }
    }
    async function loadOperations(reset) {
        if (reset) { pageCursors = [null]; pageIndex = 0; }
        const request = ++operationRequest;
        loaded = true;
        nextCursor = null;
        $('remote-operation-prev').disabled = pageIndex === 0;
        $('remote-operation-next').disabled = true;
        const list = $('remote-operation-list');
        $('remote-operation-error').textContent = '';
        $('remote-operation-summary').textContent = '读取中…';
        try {
            const query = operationFilters();
            const cursor = pageCursors[pageIndex];
            query.set('limit', String(limit));
            if (cursor) {
                query.set('before_created_at_utc', cursor.created_at_utc);
                query.set('before_operation_id', cursor.operation_id);
            }
            const result = await getJSON('/api/cloud/operations?' + query.toString());
            if (request !== operationRequest) return;
            nextCursor = result.next_cursor ?? null;
            const operations = result.operations || [];
            $('remote-operation-summary').textContent = '共 ' + result.total + ' 笔（实时匹配总数）· 第 ' + (result.total ? pageIndex + 1 : 0) + ' 页 · 当前显示 ' + operations.length + ' 笔';
            list.innerHTML = operations.map(operation => {
                const id = escapeHTML(operation.operation_id);
                const unknown = operation.status === 'unknown' ? '<strong class="status-badge source_unknown">结果未知（隔离，不可重发）</strong>' : '<strong>' + escapeHTML(statusLabel(operation.status)) + '</strong>';
                return '<article class="entry operation-history-entry"><div><strong>' + escapeHTML(kindLabel(operation.kind)) + '</strong> · ' + unknown + '<small>操作 ID：' + id + '</small><small>fs_id：' + escapeHTML(operation.object_id) + '</small><small>发起时间（UTC）：' + utc(operation.created_at_utc) + '</small><small>响应观测时间（UTC）：' + utc(operation.response_observed_at_utc) + '</small><small>源路径：' + escapeHTML(operation.source_path) + '</small><small>目标路径：' + escapeHTML(operation.destination_path) + '</small></div><div><button type="button" data-load-events="' + id + '">查看事件证据</button><button type="button" data-export-evidence="' + id + '">导出此操作证据 JSON</button></div><div id="remote-operation-events-' + id + '" class="operation-history-events muted" hidden></div></article>';
            }).join('') || '<p class="muted">当前筛选下没有远端操作记录</p>';
            list.onclick = event => {
                const exportButton = event.target.closest('[data-export-evidence]');
                if (exportButton) {
                    exportOperationEvidence(exportButton.dataset.exportEvidence, exportButton);
                    return;
                }
                const button = event.target.closest('[data-load-events]');
                if (!button) return;
                const target = $('remote-operation-events-' + button.dataset.loadEvents);
                if (target.hidden) {
                    target.hidden = false;
                    if (!eventPages.has(button.dataset.loadEvents)) loadEvents(button.dataset.loadEvents, 0);
                } else target.hidden = true;
            };
            $('remote-operation-prev').disabled = pageIndex === 0;
            $('remote-operation-next').disabled = nextCursor === null;
        } catch (error) {
            if (request !== operationRequest) return;
            list.textContent = '';
            $('remote-operation-summary').textContent = '读取失败';
            $('remote-operation-error').textContent = error.message;
        }
    }

    $('remote-operation-details').addEventListener('toggle', event => {
        if (event.currentTarget.open && !loaded) loadOperations(true);
    });
    $('remote-operation-search').onclick = () => loadOperations(true);
    $('remote-operation-export').onclick = async () => {
        const button = $('remote-operation-export');
        $('remote-operation-error').textContent = '';
        button.disabled = true;
        try {
            const query = operationFilters();
            const response = await fetch('/api/cloud/operations/export?' + query.toString(), {credentials:'same-origin'});
            if (!response.ok) throw new Error(response.status === 401 ? adminSessionExpired() : ((await response.text()) || ('HTTP ' + response.status)));
            const blob = await response.blob();
            const disposition = response.headers.get('Content-Disposition') || '';
            const match = disposition.match(/filename="([A-Za-z0-9._-]+)"/i);
            const link = document.createElement('a');
            link.href = URL.createObjectURL(blob);
            link.download = match ? match[1] : 'remote-operations.json';
            document.body.append(link);
            link.click();
            link.remove();
            setTimeout(() => URL.revokeObjectURL(link.href), 60000);
            $('remote-operation-summary').textContent = '已开始下载：' + link.download;
        } catch (error) {
            $('remote-operation-error').textContent = error.message;
        } finally {
            button.disabled = false;
        }
    };
    $('remote-operation-clear').onclick = () => {
        $('remote-operation-query').value = '';
        $('remote-operation-from').value = '';
        $('remote-operation-to').value = '';
        $('remote-operation-status').value = '';
        $('remote-operation-kind').value = '';
        loadOperations(true);
    };
    for (const id of ['remote-operation-query','remote-operation-from','remote-operation-to']) {
        $(id).addEventListener('keydown', event => {
            if (event.key === 'Enter') {
                event.preventDefault();
                loadOperations(true);
            }
        });
    }
    $('remote-operation-refresh').onclick = () => loadOperations(true);
    $('remote-operation-status').onchange = () => loadOperations(true);
    $('remote-operation-kind').onchange = () => loadOperations(true);
    $('remote-operation-prev').onclick = () => { if (pageIndex > 0) { pageIndex--; loadOperations(false); } };
    $('remote-operation-next').onclick = () => {
        if (nextCursor !== null) {
            pageCursors = pageCursors.slice(0, pageIndex + 1);
            pageCursors.push(nextCursor);
            pageIndex++;
            loadOperations(false);
        }
    };
    if (typeof autoLoadRemoteOperationHistory !== 'undefined' && autoLoadRemoteOperationHistory) {
        setTimeout(() => { if (!loaded) loadOperations(true); }, 0);
    }
})();
