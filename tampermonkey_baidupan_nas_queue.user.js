// ==UserScript==
// @name         BaiduPan NAS Queue Selection Helper
// @namespace    nas-baidupan-auto-downloader
// @version      0.3.0
// @description  将明确选中的路径交给 NAS 管理页核对，不自动移动或下载
// @match        https://pan.baidu.com/disk/*
// @match        https://yun.baidu.com/disk/*
// @run-at       document-idle
// @noframes
// @grant        unsafeWindow
// @grant        GM_getValue
// @grant        GM_setValue
// @grant        GM_registerMenuCommand
// @grant        GM_openInTab
// ==/UserScript==

(function () {
    'use strict';
    const defaultAddress = 'http://127.0.0.1:18089';
    const addressKey = 'nas-management-address';

    function managementAddress(value) {
        const url = new URL(value);
        const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname);
        if (!(url.protocol === 'https:' || (url.protocol === 'http:' && loopback)) || url.username || url.password || url.search || url.hash || !['/', '/intake'].includes(url.pathname)) {
            throw new Error('管理地址须为本机 HTTP 或 HTTPS 地址，不能包含密码、查询参数或其他页面路径。');
        }
        return url.origin + '/intake';
    }

    function selectedPaths(files) {
        if (!Array.isArray(files) || files.length < 1 || files.length > 20) throw new Error('请明确选择 1 到 20 个项目。');
        const result = files.map(file => {
            const value = file?.path;
            if (typeof value !== 'string' || value.trim() !== value || value.length > 2048 || !value.startsWith('/') || value === '/' || /[\\\u0000-\u001f\u007f-\u009f]/u.test(value) || value.slice(1).split('/').some(part => !part || part === '.' || part === '..')) {
                throw new Error('选中项没有可核对的完整路径，请在 NAS 管理页直接选择。');
            }
            return value;
        });
        if (new Set(result).size !== result.length) throw new Error('选中列表包含重复路径，请在 NAS 管理页直接选择。');
        return result;
    }

    function readSelection() {
        if (!/^\/disk\/(main|home)\/?$/.test(unsafeWindow.location.pathname)) throw new Error('仅支持自己的文件列表页，请在 NAS 管理页直接选择。');
        const candidates = [];
        // Read selection-specific state only. FILEINFO is a page listing and
        // was incorrectly used as selection by the original placeholder.
        try {
            const list = unsafeWindow.require('system-core:context/context.js')?.instanceForSystem?.list;
            if (typeof list?.getSelected === 'function') candidates.push(list.getSelected());
        } catch (_) { /* A different page generation may expose Vue selection. */ }
        for (const selector of ['.wp-s-core-pan', '.nd-main-list', '.nd-new-main-list']) {
            // Use page-world nodes: sandbox wrappers may hide Vue expandos.
            for (const node of unsafeWindow.document.querySelectorAll(selector)) {
                const list = node.__vue__?.selectedList;
                if (list !== undefined) candidates.push(list);
            }
        }
        if (!candidates.length) throw new Error('无法识别当前页面的明确选中列表，请在 NAS 管理页直接选择。');
        const selections = candidates.map(selectedPaths);
        const canonical = paths => JSON.stringify([...paths].sort());
        if (selections.some(paths => canonical(paths) !== canonical(selections[0]))) throw new Error('页面选中状态不一致，请刷新或在 NAS 管理页直接选择。');
        return selections[0];
    }

    GM_registerMenuCommand('设置 NAS 管理地址（不含密码）', () => {
        const value = window.prompt('管理页地址；仅保存地址，不填写管理员密码或百度凭证。', GM_getValue(addressKey, defaultAddress) || defaultAddress);
        if (value === null) return;
        try { GM_setValue(addressKey, managementAddress(value.trim())); window.alert('管理地址已保存。'); }
        catch (error) { window.alert(error.message); }
    });

    if (!document.body || document.getElementById('nas-download-queue-btn')) return;
    const button = document.createElement('button');
    button.id = 'nas-download-queue-btn';
    button.textContent = '交给 NAS 管理页核对';
    Object.assign(button.style, {position:'fixed', right:'20px', bottom:'24px', zIndex:'2147483647', padding:'10px 14px', background:'#435dc8', color:'#fff', border:'0', borderRadius:'6px', cursor:'pointer'});
    button.addEventListener('click', () => {
        try {
            const paths = readSelection();
            const address = managementAddress(GM_getValue(addressKey, defaultAddress) || defaultAddress);
            const payload = encodeURIComponent(JSON.stringify({version:1, paths}));
            if (payload.length > 60000) throw new Error('选中路径过长，请在 NAS 管理页直接选择。');
            if (!window.confirm(`将以下 ${paths.length} 项的路径交给 ${address}：\n\n${paths.join('\n')}\n\n请核对管理地址；到管理页后仍需核对授权账号、预检并确认移动。本步骤不移动文件。`)) return;
            GM_openInTab(address + '#nas-intake=' + payload, {active:true, insert:true, setParent:false});
        } catch (error) { window.alert(error.message || '无法读取选中项目，请在 NAS 管理页直接选择。'); }
    });
    document.body.appendChild(button);
})();
