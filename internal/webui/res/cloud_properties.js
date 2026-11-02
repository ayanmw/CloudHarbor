(() => {
  const dialog = document.getElementById('cloud-properties');
  const fields = document.getElementById('cloud-property-fields');
  const message = document.getElementById('cloud-property-message');
  let entries = new Map(), metadata = {}, selected = null;

  function invalidate() {
    entries.clear(); selected = null;
    if (dialog.open) dialog.close();
  }
  function setListing(items, observed) {
    invalidate();
    entries = new Map(items.map(item => [item.path, Object.freeze({ ...item })]));
    metadata = Object.freeze({ ...observed });
  }
  function field(label, value) {
    const dt = document.createElement('dt'), dd = document.createElement('dd');
    dt.textContent = label; dd.textContent = value ?? '未记录'; fields.append(dt, dd);
  }
  function providerTime(value) {
    const date = new Date(Number(value) * 1000);
    return Number(value) > 0 && Number.isFinite(date.getTime()) ? date.toISOString() : '未记录';
  }
  function show(path) {
    const item = entries.get(path);
    if (!item) return;
    selected = item; fields.replaceChildren(); message.textContent = '';
    const cached = metadata.source === 'cache';
    document.getElementById('cloud-property-source').textContent = cached ? '本地缓存快照，可能滞后于网盘；操作前仍须在线核对。' : '本次实时网盘列表快照；后续远端内容仍可能变化。';
    field('名称', item.name); field('类型', item.directory ? '文件夹' : '文件'); field('网盘路径', item.path);
    const complete = cached ? item.directory_size_complete === true : item.size_complete === true;
    field('大小', item.directory ? (complete ? formatBytes(cached ? item.directory_size_bytes : item.size) : '未完整统计') : formatBytes(item.size));
    // Cache fs_id is a JSON number in older APIs; do not present a rounded ID as exact.
    const identity = typeof item.id === 'string' ? item.id : Number.isSafeInteger(item.fs_id) ? String(item.fs_id) : '未记录精确 ID';
    field('fs_id', identity);
    field('提供方修改 UTC', providerTime(item.server_mtime ?? item.provider_mtime));
    field(cached ? '缓存目录观察 UTC' : '本机列表读取完成 UTC', metadata.observedAtUTC);
    if (metadata.scanUpdatedAtUTC) field('全盘扫描更新 UTC', metadata.scanUpdatedAtUTC);
    dialog.showModal();
  }
  document.getElementById('entries').addEventListener('click', event => {
    const button = event.target.closest?.('[data-properties]');
    if (button) show(button.dataset.properties);
  });
  document.getElementById('cloud-property-close').onclick = () => dialog.close();
  dialog.addEventListener('close', () => { selected = null; });
  document.getElementById('cloud-property-copy').onclick = async () => {
    const item = selected;
    if (!item) return;
    try {
      await navigator.clipboard.writeText(item.path);
      if (selected === item) message.textContent = '已复制网盘路径';
    } catch {
      if (selected === item) message.textContent = '浏览器未允许复制，可在上方选中路径手动复制。';
    }
  };
  globalThis.CloudProperties = Object.freeze({ setListing, invalidate });
})();
