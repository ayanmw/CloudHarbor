(() => {
  const get = id => document.getElementById(id);
  const panel = get('cloud-progress'), body = get('cloud-progress-body');
  const states = new Map();
  let minimised = false, lastMarkup = '';
  try { minimised = localStorage.getItem('nas:cloud-progress:minimised') === 'true'; } catch {}
  const relevant = s => s.worker_active || ((s.status === 'paused' || s.status === 'running') && s.pending_count > 0) || (s.status === 'failed' && s.run_id);
  const labels = { running: '扫描中', paused: '已暂停', failed: '扫描失败', complete: '已完成' };
  const phases = { listing: '读取目录', backoff: '等待重试', retry_wait: '等待重试', request_budget_reached: '本轮请求上限已用尽', paused: '已暂停', failed: '失败', complete: '完成' };

  function render() {
    const items = [...states].filter(([,s]) => relevant(s));
    panel.hidden = items.length === 0;
    body.hidden = minimised;
    get('cloud-progress-toggle').textContent = minimised ? '展开' : '收起';
    get('cloud-progress-toggle').ariaExpanded = String(!minimised);
    get('cloud-progress-title').textContent = items.length ? `后台扫描 · ${items.some(([,s])=>s.unreadable)?'状态暂不可读':items.some(([,s])=>s.worker_active)?'运行中':'等待处理'}` : '后台扫描';
    const markup = items.map(([kind,s]) => {
      const unavailable = s.unreadable;
      const active = !!s.worker_active;
      const title = kind === 'full' ? '全盘缓存扫描' : '目录缓存刷新';
      const status = unavailable ? '状态暂不可读，以下为上次读取结果' : labels[s.status] || '等待处理';
      const controls = active ? `<button type="button" data-progress-kind="${kind}" data-progress-action="pause" ${unavailable?'disabled':''}>暂停</button>` : s.pending_count > 0 ? `<button type="button" data-progress-kind="${kind}" data-progress-action="resume" ${unavailable?'disabled':''}>${s.request_budget>0?'设置并继续':'继续'}</button>` : '';
      return `<section class="cloud-progress-item"><strong>${title} · ${status}</strong>${active&&!unavailable?'<progress aria-label="扫描中，总量尚未确定"></progress>':''}<p>${s.pages_processed||0} 页 · ${s.items_processed||0} 项 · 待扫 ${s.pending_count||0} 个目录</p><p class="truncate" title="${escapeHTML(s.current_directory||'')}">${escapeHTML(s.current_directory||s.root_path||'/')}</p><p>${escapeHTML(phases[s.phase]||'处理中')} · 请求 ${s.requests_used||0} / ${s.request_budget>0?s.request_budget:'不限次数'}</p>${s.retry_at_utc?`<p>下次重试 UTC ${escapeHTML(s.retry_at_utc)}</p>`:''}<div class="cloud-progress-actions">${controls}<button type="button" data-progress-kind="${kind}" data-progress-action="details">查看详情</button></div></section>`;
    }).join('');
    // Polling must not repeatedly destroy a keyboard user's focused controls.
    if (markup !== lastMarkup) {
      const active = document.activeElement;
      const focused = active && body.contains(active) ? active.closest('[data-progress-action]') : null;
      const focus = focused ? { kind:focused.dataset.progressKind, action:focused.dataset.progressAction } : null;
      body.innerHTML = markup; lastMarkup = markup;
      if (focus) {
        const next = body.querySelector(`[data-progress-kind="${focus.kind}"][data-progress-action="${focus.action}"]`);
        (next && !next.disabled ? next : get('cloud-progress-toggle')).focus();
      }
    }
  }
  function update(kind, state) { states.set(kind, { ...state, unreadable: false }); render(); }
  function fail(kind) {
    if (states.has(kind)) states.set(kind, { ...states.get(kind), unreadable: true });
    render();
  }
  get('cloud-progress-toggle').onclick = () => {
    minimised = !minimised;
    try { localStorage.setItem('nas:cloud-progress:minimised', String(minimised)); } catch {}
    render();
  };
  body.addEventListener('click', event => {
    const button = event.target.closest?.('[data-progress-action]');
    if (!button || button.disabled) return;
    const full = button.dataset.progressKind === 'full', action = button.dataset.progressAction;
    if (action === 'details' || (action === 'resume' && states.get(button.dataset.progressKind)?.request_budget > 0)) {
      document.querySelector(`[data-group="${full?'cache':'files'}"]`)?.click();
      if (!full) get('browser-cache-tools').open = true;
      get(full ? 'scan-details' : 'directory-refresh-details').open = true;
      return;
    }
    const target = get(full ? (action === 'pause' ? 'scan-pause' : 'scan-start') : (action === 'pause' ? 'pause-directory-refresh' : 'resume-directory-refresh'));
    if (!target.disabled) target.click();
  });
  globalThis.CloudProgress = Object.freeze({ update, fail });
})();
