(() => {
    const $ = id => document.getElementById(id);
    const limit = 30;
    let cursors = [null], pageIndex = 0, nextCursor = null, requestID = 0;
    const eventRequests = new Map();
    const eventPages = new Map();
    function escapeHTML(value) { return String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
    function utc(value) { return value ? escapeHTML(String(value).replace('T',' ').replace(/Z$/, ' UTC')) : '无'; }
    function statusLabel(status) { return ({running:'正在运行',success:'成功',failed:'失败',interrupted:'进程中断'})[status] || status; }
    function filters() {
        const q = new URLSearchParams({limit:String(limit)});
        if ($('scan-status').value) q.set('status', $('scan-status').value);
        if ($('scan-from').value) q.set('started_from_utc', $('scan-from').value);
        if ($('scan-through').value) q.set('started_through_utc', $('scan-through').value);
        const cursor = cursors[pageIndex]; if (cursor) q.set('before_sequence', String(cursor));
        return q.toString();
    }
    async function getJSON(url) {
        const response = await fetch(url, {credentials:'same-origin'});
        if (!response.ok) throw new Error(response.status === 401 ? adminSessionExpired() : ((await response.text()) || ('HTTP ' + response.status)));
        return response.json();
    }
    async function load() {
        const id = ++requestID; $('scan-error').textContent = ''; $('scan-summary').textContent = '正在读取记录…';
        try {
            const data = await getJSON('/api/monitor-scans?' + filters()); if (id !== requestID) return;
            $('scan-list').innerHTML = data.runs.length ? data.runs.map(run => `<article class="run"><div class="run-meta"><strong>${escapeHTML(statusLabel(run.status))}</strong> · run ${escapeHTML(run.run_id)}<br>监控目录：${escapeHTML(run.monitor_path)}<br>本机开始 UTC：${utc(run.started_at_utc)}<br>本机结束 UTC：${utc(run.finished_at_utc)}<br>百度目录响应观察 UTC：${utc(run.cloud_observed_at_utc)}${run.error_code ? `<br>错误分类：${escapeHTML(run.error_code)}` : ''}</div><div class="row"><button type="button" data-events="${escapeHTML(run.run_id)}">查看事件</button><a href="/api/monitor-scans/${encodeURIComponent(run.run_id)}/export" download>导出本轮 JSON</a></div><div id="events-${escapeHTML(run.run_id)}"></div></article>`).join('') : '<p class="muted">没有符合条件的扫描记录。</p>';
            $('scan-summary').textContent = `当前第 ${pageIndex + 1} 页 · ${data.runs.length} 条`;
            nextCursor = data.has_more ? data.next_before_sequence : null; $('scan-prev').disabled = pageIndex === 0; $('scan-next').disabled = !nextCursor;
        } catch (error) { if (id !== requestID) return; $('scan-error').textContent = error.message; $('scan-summary').textContent = '读取失败'; }
    }
    async function showEvents(runID, before = null, append = false) {
        const box = $(`events-${runID}`), id = (eventRequests.get(runID) || 0) + 1; eventRequests.set(runID,id); if (!append) box.innerHTML = '<p class="muted">正在读取事件…</p>';
        const query = new URLSearchParams({run_id:runID,limit:'100'}); if (before) query.set('before_sequence',String(before));
        try { const data = await getJSON('/api/monitor-scans/events?' + query); if (eventRequests.get(runID)!==id || !box.isConnected) return;
            const state = eventPages.get(runID) || {events:[]}; state.events = append ? state.events.concat(data.events) : data.events; state.nextBefore = data.next_before_sequence; state.more = data.has_more; eventPages.set(runID,state);
            box.innerHTML = `<ol class="events">${state.events.slice().reverse().map(event => `<li>${escapeHTML(event.kind)} · 本机记录 UTC：${utc(event.recorded_at_utc)}${event.cloud_observed_at_utc ? ` · 百度目录响应 UTC：${utc(event.cloud_observed_at_utc)}` : ''}${event.error_code ? ` · ${escapeHTML(event.error_code)}` : ''}</li>`).join('')}</ol>${state.more ? `<button type="button" data-more-events="${escapeHTML(runID)}">加载更早事件</button>` : ''}`;
        } catch (error) { if (eventRequests.get(runID)===id) box.textContent = error.message; }
    }
    $('scan-list').addEventListener('click', event => { const more=event.target.closest('[data-more-events]');if(more){const state=eventPages.get(more.dataset.moreEvents);if(state?.more)showEvents(more.dataset.moreEvents,state.nextBefore,true);return;}const button=event.target.closest('[data-events]'); if(button) showEvents(button.dataset.events); });
    $('scan-search').addEventListener('click', () => { cursors=[null];pageIndex=0;load(); }); $('scan-refresh').addEventListener('click', load);
    $('scan-clear').addEventListener('click', () => { $('scan-status').value='';$('scan-from').value='';$('scan-through').value='';cursors=[null];pageIndex=0;load(); });
    $('scan-prev').addEventListener('click', () => { if(pageIndex>0){pageIndex--;load();} }); $('scan-next').addEventListener('click', () => { if(nextCursor){cursors[pageIndex+1]=nextCursor;pageIndex++;load();} });
    load();
})();
