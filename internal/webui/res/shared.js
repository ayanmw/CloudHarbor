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
