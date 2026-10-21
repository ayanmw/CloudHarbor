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
        'cancelled': '已取消'
    };
    return `<span class="status-badge ${status}">${statusMap[status] || status}</span>`;
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
