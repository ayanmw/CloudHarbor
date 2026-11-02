let activePreview = null;
let intakeBusy = false;
let inputGeneration = 0;
let cloudGeneration = 0;
let historyGeneration = 0;
let cloudPage = {directory:'/', offset:0, next_offset:null};
let historyPage = {cursor:null, next_cursor:null};
let historyCursorStack = [null];

const intakeStates = {ready:'待确认', sending:'已发送，等待结果', moved:'移动已核对', failed:'移动失败', not_sent:'未发送', unknown:'结果未知（隔离）'};
const intakeErrors = {move_unconfirmed:'移动结果无法确认，继续隔离', directory_completion_unconfirmed:'缺少目录完成凭据，继续隔离', async_move_unconfirmed:'异步移动尚未证明完成，继续隔离', restart_during_move:'移动期间重启，继续隔离等待核对', restart_before_send:'重启前尚未发送', provider_rejected:'网盘拒绝移动', batch_stopped:'本批已停止，未发送此项', authorization_changed_or_cancelled:'认证变化或请求取消，未发送', queue_registration_failed:'下载队列登记失败，目标仍隔离'};
function intakeMessage(text) { document.getElementById('intake-message').textContent = text; }
function inputPaths() { return document.getElementById('intake-paths').value.split(/\r?\n/).filter(value=>value.trim()!==''); }
function previewValid() { return activePreview && activePreview.state === 'preview' && Date.now() < Date.parse(activePreview.expires); }
function setIntakeBusy(value) {
    intakeBusy = value;
    document.getElementById('intake-paths').disabled = value;
    document.getElementById('intake-preview').disabled = value;
    document.getElementById('intake-confirm-check').disabled = value;
    document.getElementById('intake-commit').disabled = value || !previewValid() || !document.getElementById('intake-confirm-check').checked;
    document.querySelectorAll('[data-source]').forEach(node=>{node.disabled=value || node.dataset.allowed!=='true';});
    document.querySelectorAll('[data-recheck]').forEach(node=>{node.disabled=value;});
}
function discardPreview() {
    inputGeneration++; activePreview = null;
    document.getElementById('preview-card').hidden = true;
    document.getElementById('intake-confirm-check').checked = false;
    setIntakeBusy(intakeBusy);
}
async function intakeRequest(url, body) {
    const response = await fetch(url, body === undefined ? {} : {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
    if (!response.ok) {
        if (response.status===401) throw new Error(adminSessionExpired());
        throw new Error((await response.text()).trim() || '请求失败。');
    }
    return response.json();
}
function intakeTable(record) {
    const mode = record.download_mode === 'delete' ? '下载完成后删除网盘项' : '下载完成后保留并标记 .可删除';
    return `<p>目标队列：<strong>${escapeHTML(record.queue_dir)}</strong></p><p>此批任务固定策略：<strong>${mode}</strong></p>` +
        `<table class="intake-table"><thead><tr><th>源项目</th><th>目标</th><th>结果</th></tr></thead><tbody>` +
        (record.items || []).map(item=>`<tr><td>${escapeHTML(item.source?.path)}</td><td>${escapeHTML(item.target)}</td><td>${escapeHTML(intakeStates[item.state] || item.state)}${item.error?`<br>${escapeHTML(intakeErrors[item.error] || item.error)}`:''}</td></tr>`).join('') + '</tbody></table>';
}
function resultMessage(record) {
    const unknown = (record.items || []).some(item=>item.state==='unknown' || item.state==='sending');
    if (unknown) return '部分项目结果未知，目标保持隔离。本次不会自动重发移动；缺少完成凭据的目录没有人工放行入口。';
    if ((record.items || []).some(item=>item.state==='failed' || item.state==='not_sent')) return '本批包含失败或未发送项目，请逐项核对；已成功项目会进入扫描下载。';
    return '移动结果已核对，后台将扫描并处理已确认的项目。';
}
async function createPreview() {
    if (intakeBusy) return;
    const paths = inputPaths(); if (!paths.length || paths.length>20) { intakeMessage('每批请选择或输入 1 到 20 个项目。'); return; }
    discardPreview(); const generation = inputGeneration; setIntakeBusy(true); intakeMessage('正在核对源项目与目标冲突；此步骤不会移动文件…');
    try {
        const record = await intakeRequest('/api/intakes/preview',{paths});
        if (generation !== inputGeneration) return;
        activePreview = record; document.getElementById('preview-card').hidden = false;
        document.getElementById('intake-preview-result').innerHTML = intakeTable(record) + `<p class="notice">确认后会移动以上项目。预检有效期至 ${escapeHTML(formatTime(record.expires))}；修改输入或认证变化后需要重新预检。</p>`;
        intakeMessage('预检完成。请核对以上路径和策略，勾选确认后再提交。');
    } catch (error) { intakeMessage(error.message); }
    finally { setIntakeBusy(false); }
}
async function commitPreview() {
    if (intakeBusy || !activePreview) return;
    if (!previewValid() || !document.getElementById('intake-confirm-check').checked) { intakeMessage('请先完成有效预检并勾选确认。'); return; }
    const id = activePreview.id; activePreview = null; setIntakeBusy(true); intakeMessage('正在提交移动，请勿重复操作。中断后请查询记录，不要直接重发。');
    try {
        const record = await intakeRequest(`/api/intakes/${encodeURIComponent(id)}/commit`,{});
        document.getElementById('intake-preview-result').innerHTML = intakeTable(record);
        intakeMessage(resultMessage(record));
    } catch (error) { intakeMessage(error.message + ' 请刷新入队记录核对结果；当前确认不会自动重发。'); }
    finally { document.getElementById('intake-confirm-check').checked=false; setIntakeBusy(false); await refreshIntakeRecords(); }
}
async function loadCloud(directory='/', offset=0) {
    const generation=++cloudGeneration; cloudPage={directory,offset,next_offset:null}; document.getElementById('cloud-directory').value=directory; document.getElementById('cloud-summary').textContent='正在读取目录…'; document.getElementById('cloud-entries').innerHTML=''; document.getElementById('cloud-prev').disabled=true; document.getElementById('cloud-next').disabled=true;
    try {
        const listing=await intakeRequest('/api/cloud?dir='+encodeURIComponent(directory)+'&offset='+offset+'&limit=100');
        if (generation!==cloudGeneration) return;
        cloudPage=listing; document.getElementById('cloud-directory').value=listing.directory;
        const selected=new Set(inputPaths());
        document.getElementById('cloud-entries').innerHTML=listing.entries.length?listing.entries.map(entry=>`<div class="cloud-entry"><input type="checkbox" data-source="${escapeHTML(entry.path)}" data-allowed="${entry.can_enqueue===true}" ${selected.has(entry.path)?'checked':''} ${intakeBusy || !entry.can_enqueue?'disabled':''} aria-label="选择 ${escapeHTML(entry.name)}"><div class="cloud-name">${entry.directory?`<button type="button" data-directory="${escapeHTML(entry.path)}">📁 ${escapeHTML(entry.name)}</button>`:escapeHTML(entry.name)}</div><span class="muted">${entry.directory?'目录':formatBytes(entry.size)}</span></div>`).join(''):'<p class="muted">此目录暂无项目。</p>';
        const range=listing.entries.length?`当前 ${listing.offset+1}–${listing.offset+listing.entries.length}`:'当前页为空';
        const total=Number.isInteger(listing.total)?`共 ${listing.total} 项，`:'总数未知，';
        const more=listing.next_offset==null?'':' 仍有下一页。';
        document.getElementById('cloud-summary').textContent=`${listing.directory} · ${total}${range}。${more}固定队列：${listing.queue_dir}；不可选项可能已在队列、历史或隔离范围内。`;
        document.getElementById('cloud-prev').disabled=listing.offset===0;
        document.getElementById('cloud-next').disabled=listing.next_offset==null;
    } catch(error) { if(generation===cloudGeneration){cloudPage={directory,offset,next_offset:null};document.getElementById('cloud-directory').value=directory;document.getElementById('cloud-summary').textContent=error.message;document.getElementById('cloud-entries').innerHTML='';document.getElementById('cloud-prev').disabled=true;document.getElementById('cloud-next').disabled=true;} }
}
async function refreshIntakeRecords(cursor=null, commitNavigation=null) {
    const generation=++historyGeneration;
    try {
        const unresolved=document.getElementById('intake-unresolved').checked?'1':'0';
        const params=new URLSearchParams({limit:'20',unresolved});if(cursor)params.set('cursor',cursor);
        const listing=await intakeRequest(`/api/intakes?${params}`);
        if(generation!==historyGeneration)return;
        if(commitNavigation)commitNavigation();
        historyPage={...listing,cursor};
        document.getElementById('intake-records').innerHTML=(listing.records || []).map(record=>{
            const pending=(record.items || []).some(item=>item.state==='unknown' || item.state==='sending');
            const validID=/^[0-9a-f]{32}$/.test(record.id);
            return `<article class="record"><p>记录 ${escapeHTML(record.id)} · ${escapeHTML(formatTime(record.created))} · ${record.state==='expired'?'预检已失效（未提交）':record.state==='preview'?'预检记录（未提交）':record.state==='running'?'正在处理':'处理记录'}</p>${intakeTable(record)}${pending && validID?`<button type="button" data-recheck="${record.id}" ${intakeBusy?'disabled':''}>重新核对可确认的项目</button>`:''}</article>`;
        }).join('') || '<p class="muted">暂无对应记录。</p>';
        document.getElementById('records-prev').disabled=historyCursorStack.length<2;
        document.getElementById('records-next').disabled=listing.next_cursor==null;
    } catch(error) { if(generation===historyGeneration)document.getElementById('intake-records').textContent=error.message; }
}
async function recheckIntake(id) {
    if(intakeBusy || !/^[0-9a-f]{32}$/.test(id))return;
    setIntakeBusy(true);intakeMessage('正在核对可确认项目，不会重新发出移动请求…');
    try { const record=await intakeRequest(`/api/intakes/${id}/recheck`,{});intakeMessage(resultMessage(record)); }
    catch(error) { intakeMessage(error.message); }
    finally {setIntakeBusy(false);await refreshIntakeRecords(historyPage.cursor || null);}
}
function consumeIntakeHandoff() {
    const prefix = '#nas-intake=';
    if (!location.hash?.startsWith(prefix)) return;
    const encoded = location.hash.slice(prefix.length);
    // Consume before any API request. Fragments are not sent in HTTP requests;
    // remove the input from this history entry as soon as the page can read it.
    try { history.replaceState(null, '', location.pathname + location.search); }
    catch (_) { intakeMessage('无法清除外部输入，请关闭此页并在管理页直接选择。'); return; }
    try {
        if (encoded.length > 60000) throw new Error('oversize');
        const value = JSON.parse(decodeURIComponent(encoded));
        if (!value || Array.isArray(value) || Object.keys(value).sort().join(',') !== 'paths,version' || value.version !== 1 || !Array.isArray(value.paths) || !value.paths.length || value.paths.length > 20) throw new Error('shape');
        for (const path of value.paths) {
            if (typeof path !== 'string' || !path.length || path.length > 2048 || !path.startsWith('/') || path === '/' || /[\\\u0000-\u001f\u007f-\u009f]/u.test(path) || path.slice(1).split('/').some(part => !part || part === '.' || part === '..')) throw new Error('path');
        }
        if (new Set(value.paths).size !== value.paths.length) throw new Error('duplicate');
        discardPreview();
        document.getElementById('intake-paths').value = value.paths.join('\n');
        intakeMessage('已接收外部页面提供的路径，尚未预检或移动。请核对本服务当前授权的百度账号及路径，再手动预检和确认。');
    } catch (_) { intakeMessage('外部选择数据无效，未导入；请在本页直接选择项目。'); }
}

document.addEventListener('DOMContentLoaded',()=>{
    consumeIntakeHandoff();
    document.getElementById('cloud-form').addEventListener('submit',event=>{event.preventDefault();const directory=document.getElementById('cloud-directory').value;loadCloud(directory.trim()===''?'/':directory,0);});
    document.getElementById('cloud-up').addEventListener('click',()=>{const dir=cloudPage.directory;loadCloud(dir==='/'?'/':dir.slice(0,dir.lastIndexOf('/')) || '/',0);});
    document.getElementById('cloud-prev').addEventListener('click',()=>loadCloud(cloudPage.directory,Math.max(0,cloudPage.offset-100)));
    document.getElementById('cloud-next').addEventListener('click',()=>{if(cloudPage.next_offset!=null)loadCloud(cloudPage.directory,cloudPage.next_offset);});
    document.getElementById('cloud-entries').addEventListener('click',event=>{const button=event.target.closest('[data-directory]');if(button)loadCloud(button.dataset.directory,0);});
    document.getElementById('cloud-entries').addEventListener('change',event=>{
        const checkbox=event.target.closest('[data-source]');if(!checkbox)return;
        const paths=new Set(inputPaths());if(intakeBusy || checkbox.dataset.allowed!=='true'){checkbox.checked=paths.has(checkbox.dataset.source);return;}
        if(checkbox.checked){if(paths.size>=20){checkbox.checked=false;intakeMessage('每批最多选择 20 项。');return;}paths.add(checkbox.dataset.source);}else paths.delete(checkbox.dataset.source);
        document.getElementById('intake-paths').value=[...paths].join('\n');discardPreview();
    });
    document.getElementById('intake-paths').addEventListener('input',discardPreview);
    document.getElementById('intake-confirm-check').addEventListener('change',()=>setIntakeBusy(intakeBusy));
    document.getElementById('intake-preview').addEventListener('click',createPreview);
    document.getElementById('intake-commit').addEventListener('click',commitPreview);
    document.getElementById('intake-refresh').addEventListener('click',()=>{historyCursorStack=[null];refreshIntakeRecords();});
    document.getElementById('intake-unresolved').addEventListener('change',()=>{historyCursorStack=[null];refreshIntakeRecords();});
    document.getElementById('records-prev').addEventListener('click',()=>{if(historyCursorStack.length>1){const previous=historyCursorStack[historyCursorStack.length-2];refreshIntakeRecords(previous,()=>historyCursorStack.pop());}});
    document.getElementById('records-next').addEventListener('click',()=>{const next=historyPage.next_cursor;if(next){refreshIntakeRecords(next,()=>{if(historyCursorStack[historyCursorStack.length-1]!==next)historyCursorStack.push(next);});}});
    document.getElementById('intake-records').addEventListener('click',event=>{const button=event.target.closest('[data-recheck]');if(button)recheckIntake(button.dataset.recheck);});
    intakeRequest('/api/oauth/status').then(auth=>{document.getElementById('intake-auth').textContent=credentialStatusText(auth);}).catch(error=>{document.getElementById('intake-auth').textContent=error.message;});
    loadCloud('/');refreshIntakeRecords();setInterval(()=>setIntakeBusy(intakeBusy),1000);
});
