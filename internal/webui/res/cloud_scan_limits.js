(() => {
  const controls = new Map();
  for (const [kind,prefix,inputID] of [['scan','scan','scan-budget'],['refresh','directory-refresh','directory-refresh-budget']]) {
    const mode = document.getElementById(prefix+'-limit-mode'), input = document.getElementById(inputID);
    const fields = document.getElementById(prefix+'-limit-fields'), hint = document.getElementById(prefix+'-limit-hint');
    const control = { mode,input,fields,hint,edited:false,runID:null };
    if (!mode.value) mode.value = 'unlimited';
    const render = () => { fields.hidden = mode.value !== 'limited'; input.disabled = fields.hidden; };
    mode.onchange = () => { control.edited = true; render(); };
    input.oninput = () => { control.edited = true; };
    render(); controls.set(kind,control);
  }
  function request(kind) {
    const c = controls.get(kind);
    if (c.mode.value === 'unlimited') return { request_limit_mode:'unlimited',request_budget:0 };
    const value = Number(c.input.value);
    if (c.mode.value !== 'limited' || !Number.isSafeInteger(value) || value < 1 || value > 100000) throw new Error('限制次数须为 1 至 100000；不设上限请选“扫描至完成”。');
    return { request_limit_mode:'limited',request_budget:value };
  }
  function observe(kind,state) {
    const c = controls.get(kind);
    if (state.run_id) {
      // A choice made for another checkpoint must never cancel this run's cap.
      if (c.runID !== state.run_id) { c.runID = state.run_id; c.edited = false; }
      if (!c.edited) {
        const limited = state.pending_count > 0 && state.request_budget > 0;
        c.mode.value = limited ? 'limited' : 'unlimited';
        c.fields.hidden = !limited; c.input.disabled = !limited;
      }
    }
    c.hint.textContent = state.pending_count > 0 && state.request_budget > 0
      ? `本轮已用 ${state.requests_used||0} / ${state.request_budget} 次；恢复时数字为追加次数。选择“扫描至完成”将显式取消本轮次数上限。`
      : '扫描至完成不设本工具次数上限；接口限流或网络失败仍按既有策略等待重试。';
  }
  globalThis.CloudScanLimits = Object.freeze({ request,observe });
})();
