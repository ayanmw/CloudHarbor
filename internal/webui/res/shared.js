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

const pendingTaskActions = new Set();
function taskControls(task) {
    if (!task.is_root) return '';
    const mode = task.download_mode === 'delete' ? '完成后删除网盘项' : '保留并标记网盘项';
    const id = typeof task.task_id === 'string' && /^[1-9][0-9]{0,18}$/.test(task.task_id) ? task.task_id : '';
    const disabled = pendingTaskActions.has(id) ? ' disabled' : '';
    const button = (action, label) => `<button type="button" data-task-id="${id}" data-action="${action}"${disabled}>${label}</button>`;
    return `<div class="task-controls"><span>此任务：${mode} · 已启动 ${Number(task.attempts) || 0} 次</span>` +
        (task.next_attempt ? `<span>下次重试：${escapeHTML(formatTime(task.next_attempt))}</span>` : '') +
        (id && task.can_cancel === true ? button('cancel', '取消任务') : '') +
        (id && task.can_retry === true ? button('retry', '重试任务') : '') + '</div>';
}
function taskFeedback(message) { document.getElementById('task-feedback').textContent = message; }
function actionFailure(status) {
    if (status === 409) return '任务状态已改变、仍在停止或尚未完成扫描，请刷新后重试。';
    if (status === 404) return '任务已不存在，请刷新任务列表。';
    if (status === 401) return '管理登录已失效，请刷新页面重新登录。';
    return '操作失败，请检查服务状态后重试。';
}
async function performTaskAction(id, action, refresh) {
    if (typeof id !== 'string' || !/^[1-9][0-9]{0,18}$/.test(id) || !['cancel','retry'].includes(action) || pendingTaskActions.has(id)) return;
    pendingTaskActions.add(id); taskFeedback('正在提交任务操作…');
    try {
        const response = await fetch(`/api/tasks/${id}/${action}`, {method:'POST'});
        if (!response.ok) throw new Error(actionFailure(response.status));
        taskFeedback(action === 'cancel' ? '取消已提交；任务停止后才可重试。已发出的网盘操作不能撤回。' : '重试已提交，将按此任务原有策略执行。');
    } catch (error) { taskFeedback(error.message || '网络连接失败。'); }
    finally { pendingTaskActions.delete(id); try { await refresh(); } catch {} }
}
function bindTaskControls(refresh) {
    document.getElementById('tasks-list').addEventListener('click', async event => {
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
function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
}

function formatSpeed(bytesPerSecond) {
    if (bytesPerSecond === 0) return '0 B/s';
    const k = 1024;
    const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
    const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k));
    return Math.round(bytesPerSecond / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
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
