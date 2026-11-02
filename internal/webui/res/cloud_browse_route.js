// Browse URLs carry view state only. They never start scans or remote writes.
(() => {
  function read(search, defaults = {}) {
    const params = new URLSearchParams(search);
    const one = key => params.getAll(key).length === 1 ? params.get(key) : null;
    let directory = one('dir') ?? defaults.directory ?? '/';
    if (!directory.startsWith('/') || /[\\\x00-\x1f\x7f]/.test(directory) || directory.split('/').some(part => part === '.' || part === '..')) directory = '/';
    const source = (one('source') ?? defaults.source) === 'cache' ? 'cache' : 'live';
    const rawOffset = one('offset') ?? String(defaults.offset ?? 0);
    const offset = /^\d+$/.test(rawOffset) && Number.isSafeInteger(Number(rawOffset)) && Number(rawOffset) <= 1000000 ? Number(rawOffset) : 0;
    const rawQuery = (one('q') ?? defaults.query ?? '').trim();
    const query = source === 'cache' && Array.from(rawQuery).length <= 128 && !/[\x00-\x1f\x7f]/.test(rawQuery) ? rawQuery : '';
    return { directory, source, offset, query };
  }

  function write(state, mode = 'push') {
    if (mode === 'none') return;
    const current = new URL(location.href);
    const url = new URL(current.pathname, current.origin);
    const tab = current.searchParams.get('ui_tab');
    if (['files', 'settings', 'cache', 'cleanup', 'history'].includes(tab)) url.searchParams.set('ui_tab', tab);
    if (state.directory !== '/') url.searchParams.set('dir', state.directory);
    if (state.source === 'cache') url.searchParams.set('source', 'cache');
    if (state.source === 'cache' && state.query) url.searchParams.set('q', state.query);
    if (state.offset > 0) url.searchParams.set('offset', String(state.offset));
    if (url.href === current.href) return;
    if (mode === 'replace') history.replaceState(null, '', url);
    else history.pushState(null, '', url);
  }

  globalThis.CloudBrowseRoute = Object.freeze({ read, write });
})();
