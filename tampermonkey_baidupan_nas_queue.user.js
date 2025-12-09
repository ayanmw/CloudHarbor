// ==UserScript==
// @name         BaiduPan NAS Download Queue Helper
// @namespace    http://tampermonkey.net/
// @version      0.2
// @description  One-click move selected BaiduPan files to fixed directory NAS_Download_Queue
// @author       you
// @match        https://pan.baidu.com/*
// @match        https://yun.baidu.com/*
// @icon         https://pan.baidu.com/res/static/images/favicon.ico
// @grant        none
// ==/UserScript==

(function() {
    'use strict';


  console.log('[NAS_Download_Queue] userscript loaded');

  function waitForPanReady() {
    return new Promise((resolve) => {
      const maxRetry = 50;
      let count = 0;
      const timer = setInterval(() => {
        count++;
        // Baidu 网盘在全局 often 存在 require / yunData 等对象
        if (window.yunData && window.yunData.FILEINFO) {
          clearInterval(timer);
          resolve(true);
        }
        if (count > maxRetry) {
          clearInterval(timer);
          resolve(false);
        }
      }, 500);
    });
  }

  function getSelectedFiles() {
    try {
      if (window.yunData && Array.isArray(window.yunData.FILEINFO)) {
        // 这是当前页面展示的文件信息，不一定只包含“选中”的
        return window.yunData.FILEINFO;
      }
    } catch (e) {
      console.error('[NAS_Download_Queue] getSelectedFiles error', e);
    }
    return [];
  }

  function addButton() {
    const toolbar = document.querySelector('.wp-s-agile-tool-bar__header') || document.querySelector('.nd-main__toolbar');
    if (!toolbar || document.getElementById('nas-download-queue-btn')) {
      return;
    }

    const btn = document.createElement('button');
    btn.id = 'nas-download-queue-btn';
    btn.textContent = '加入 NAS 队列';
    btn.style.marginLeft = '8px';
    btn.style.padding = '0 10px';

    btn.onclick = () => {
      const files = getSelectedFiles();
      console.log('[NAS_Download_Queue] selected files (raw FILEINFO):', files);
      alert('当前脚本已加载，暂时只在控制台打印 FILEINFO，下一步将改成真正移动到 NAS_Download_Queue。\n请打开 F12 -> Console 查看日志。');
    };

    toolbar.appendChild(btn);
    console.log('[NAS_Download_Queue] button injected');
  }

  waitForPanReady().then(() => {
    addButton();
  });
})();