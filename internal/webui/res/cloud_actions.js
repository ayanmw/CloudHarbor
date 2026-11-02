(() => {
  const get = id => document.getElementById(id);
  const dialog = get('cloud-action-dialog'), form = get('cloud-action-form');
  const name = get('cloud-action-name'), confirmation = get('cloud-action-delete-confirm');
  const message = get('cloud-action-message'), submit = get('cloud-action-submit');
  let current = null, pending = false, blocked = false;

  function controls() {
    submit.disabled = pending || blocked || (current?.operation === 'delete' && !confirmation.checked);
    name.disabled = pending || blocked;
    confirmation.disabled = pending || blocked;
    get('cloud-action-cancel').disabled = pending;
  }
  function close() { if (!pending) dialog.close(); }
  function invalidate() { if (!pending && dialog.open) close(); }
  function open(options) {
    if (pending) return;
    current = options; blocked = false; message.textContent = '';
    const removing = options.operation === 'delete';
    get('cloud-action-title').textContent = ({ mkdir: '新建文件夹', rename: '重命名', delete: '删除网盘项目' })[options.operation];
    get('cloud-action-target').textContent = options.item?.path || options.directory;
    get('cloud-action-name-field').hidden = removing;
    get('cloud-action-delete-field').hidden = !removing;
    name.value = options.operation === 'rename' ? options.item.name : '';
    name.required = !removing; confirmation.checked = false;
    get('cloud-action-delete-label').textContent = options.item?.directory ? '确认删除此网盘目录及其全部内容' : '确认删除此网盘文件';
    submit.textContent = removing ? '确认删除' : '保存';
    controls(); dialog.showModal();
    if (!removing) name.focus();
  }
  form.addEventListener('submit', async event => {
    event.preventDefault();
    if (!current || pending || blocked) return;
    const options = current, removing = options.operation === 'delete', value = name.value;
    if (removing && !confirmation.checked) { message.textContent = '请先核对路径并勾选删除确认。'; return; }
    if (!removing && (!value.trim() || value === '.' || value === '..' || /[\\/\x00-\x1f\x7f]/.test(value))) {
      message.textContent = '请输入有效名称，不能包含路径分隔符或控制字符。'; return;
    }
    pending = true; controls(); message.textContent = '正在暂停下载并核对远端对象…';
    try {
      const result = await options.submit(value);
      if (current !== options) return;
      if (result?.status === 'confirmed') { pending = false; close(); return; }
      const error = result?.error;
      if (error?.httpStatus === 400) message.textContent = error.message;
      else {
        blocked = true;
        message.textContent = `未获得成功确认，请先到操作记录核对，勿重复提交。${result?.operation_id ? ' 操作 ID：' + result.operation_id : ''}${error?.message ? ' ' + error.message : ''}`;
      }
    } catch {
      if (current === options) { blocked = true; message.textContent = '请求结果未确认，请先到操作记录核对，勿重复提交。'; }
    } finally {
      pending = false; controls();
    }
  });
  confirmation.onchange = controls;
  get('cloud-action-cancel').onclick = close;
  dialog.addEventListener('cancel', event => { if (pending) event.preventDefault(); });
  dialog.addEventListener('close', () => { current = null; blocked = false; });
  globalThis.CloudActions = Object.freeze({ open, invalidate });
})();
