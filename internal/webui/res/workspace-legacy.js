(()=>{
 const page=location.pathname;
 document.body.classList.add('workspace-legacy');
 if(page==='/login'){document.body.classList.add('workspace-login');return}
 const modules=[['transfers','传输','/','传输任务',[['/','↓','下载任务'],['/uploads','↑','上传任务']]],['cloud','网盘','/cloud/files','网盘管理',[['/cloud/files','▤','网盘文件'],['/cloud/cache','▦','缓存与目录刷新'],['/cloud/cleanup','◇','重复与空目录'],['/cloud/history','◷','扫描历史'],['/cloud','⚙','高级管理']]],['history','记录','/intake-history','记录与诊断',[['/intake-history','≡','入队记录'],['/operations','◷','操作记录'],['/monitor-history','◉','监控历史'],['/task-events','≡','任务事件']]],['settings','设置','/settings/paths','系统设置',[['/settings/paths','▣','路径与存储'],['/settings/migrations','◷','目录迁移记录'],['/oauth','⚙','账号与授权']]]];
 const section=page.startsWith('/cloud')||page==='/intake'?modules[1]:['/intake-history','/operations','/monitor-history','/task-events'].includes(page)?modules[2]:page==='/oauth'||page.startsWith('/settings/')?modules[3]:modules[0];
 const header=document.createElement('header');header.className='workspace-app-header';
 header.innerHTML='<a class="workspace-brand" href="/" aria-label="NAS Downloader"><b>↓</b><span>NAS Downloader<small>百度网盘自动下载</small></span></a><nav aria-label="核心功能">'+modules.map(([key,label,url])=>`<a href="${url}" class="workspace-module ${key===section[0]?'selected':''}" ${key===section[0]?'aria-current="page"':''}>${label}</a>`).join('')+'</nav><div class="workspace-header-actions"><a href="/oauth">账号</a><button type="button" id="workspace-logout">退出登录</button></div>';
 const aside=document.createElement('aside');aside.className='workspace-sidebar';
 aside.innerHTML='<div class="workspace-nav-label">'+section[3]+'</div>'+section[4].map(([url,icon,label])=>`<a class="workspace-nav-link ${page===url?'active':''}" href="${url}" aria-label="${label}" title="${label}" ${page===url?'aria-current="page"':''}><i aria-hidden="true">${icon}</i><span>${label}</span></a>`).join('')+'<div class="workspace-sidebar-foot">NAS Downloader · 管理控制台</div>';
 document.body.prepend(header,aside);
 header.querySelector('#workspace-logout').addEventListener('click',async()=>{try{const response=await fetch('/api/logout',{method:'POST',credentials:'same-origin'});if(!response.ok)throw Error('退出登录失败，请重试');location.assign('/login')}catch{let message=header.querySelector('[role="alert"]');if(!message){message=document.createElement('span');message.setAttribute('role','alert');header.append(message)}message.textContent='退出登录失败，请重试'}});
 const main=document.querySelector('main')||document.querySelector('.container');
 if(!main)return;
 main.classList.add('workspace-page');
 const oldNav=main.querySelector('nav');if(oldNav)oldNav.classList.add('workspace-old-nav');
 if(page==='/cloud'){
  main.classList.add('workspace-cloud');
  const cards=[...main.querySelectorAll(':scope > section.card')];
  const groups=[['files','网盘文件',['目录浏览']],['settings','下载设置',['自动下载路径']],['cache','缓存与统计',['全盘目录缓存']],['cleanup','重复与空目录',['空文件夹','重复文件']],['history','刷新记录',['目录缓存刷新历史']]];
  for(const card of cards){const title=card.querySelector('h2')?.textContent||'';card.dataset.workspaceGroup=groups.find(([, ,titles])=>titles.some(x=>title.includes(x)))?.[0]||'files'}
  const bar=document.createElement('nav');bar.className='workspace-section-tabs';bar.setAttribute('aria-label','网盘管理功能');
  // Live browsing updates hidden for source permissions; keep tab visibility independent.
  function select(group){for(const card of cards){card.hidden=card.dataset.workspaceGroup!==group;card.classList.toggle('workspace-section-hidden',card.dataset.workspaceGroup!==group)}for(const button of bar.querySelectorAll('button')){const active=button.dataset.group===group;button.classList.toggle('selected',active);button.setAttribute('aria-selected',String(active))}const url=new URL(location.href);url.searchParams.set('ui_tab',group);history.replaceState(null,'',url)}
  for(const [id,label] of groups){const button=document.createElement('button');button.type='button';button.textContent=label;button.dataset.group=id;button.addEventListener('click',()=>select(id));bar.append(button)}
  const first=cards[0];if(first)main.insertBefore(bar,first);
  select(new URLSearchParams(location.search).get('ui_tab')&&groups.some(g=>g[0]===new URLSearchParams(location.search).get('ui_tab'))?new URLSearchParams(location.search).get('ui_tab'):'files');
  window.addEventListener('popstate',()=>{const group=new URLSearchParams(location.search).get('ui_tab');select(groups.some(g=>g[0]===group)?group:'files')});
  const entries=document.getElementById('entries');
  if(entries){
   const convert=()=>{
    if(entries.children.length===1&&entries.firstElementChild.matches('table.workspace-file-table')&&entries.childNodes.length===1)return;
    const rows=[...entries.children].filter(n=>n.classList.contains('entry')&&n.tagName==='DIV');
    const table=document.createElement('table');table.className='workspace-file-table';table.setAttribute('aria-label','网盘目录内容');
    table.innerHTML='<thead><tr><th scope="col">名称</th><th scope="col">大小</th><th scope="col">操作</th></tr></thead><tbody></tbody>';
    const tbody=table.querySelector('tbody');
    for(const entry of rows){const tr=document.createElement('tr');tr.className=entry.className;for(const attr of entry.attributes){if(attr.name!=='class')tr.setAttribute(attr.name,attr.value)}for(const child of [...entry.children]){const td=document.createElement('td');td.append(child);tr.append(td)}tbody.append(tr)}
    if(!rows.length){const row=tbody.insertRow(),cell=row.insertCell();cell.colSpan=3;cell.className='cloud-empty';cell.textContent=entries.textContent.trim()||'目录内容暂不可用，请刷新列表';cell.setAttribute('role','status')}
    entries.replaceChildren(table);
   };
   new MutationObserver(convert).observe(entries,{childList:true});convert();
  }
 }
 // Existing event delegation stays on each list container. Move its nodes
 // rather than copying HTML, retaining click handlers and escaped evidence.
 function records(container){
  if(!(container instanceof Element)||container.id==='entries')return;
  const entries=[...container.children].filter(n=>n.tagName==='DIV'&&n.classList.contains('entry'));
  if(!entries.length)return;
  const columns=Math.max(...entries.map(e=>e.children.length),1);
  const table=document.createElement('table');table.className='workspace-record-table';
  const head=table.createTHead().insertRow();for(let i=0;i<columns;i++){const th=document.createElement('th');th.textContent=i===0?'记录':i===columns-1?'详情与操作':'状态与时间';head.append(th)}
  const body=table.createTBody();
  for(const entry of entries){const row=body.insertRow();row.className=entry.className;for(const attr of entry.attributes)if(attr.name!=='class')row.setAttribute(attr.name,attr.value);const children=[...entry.children];if(!children.length)row.insertCell().textContent=entry.textContent;for(const child of children)row.insertCell().append(child);for(const pre of row.querySelectorAll('pre')){if(pre.closest('details'))continue;const details=document.createElement('details');details.className='workspace-evidence';const summary=document.createElement('summary');summary.textContent='查看证据详情';pre.replaceWith(details);details.append(summary,pre)}entry.remove()}
  container.append(table);
 }
 for(const container of main.querySelectorAll('div'))records(container);
 new MutationObserver(changes=>{for(const change of changes)records(change.target)}).observe(main,{childList:true,subtree:true});
 for(const p of main.querySelectorAll('p.muted:not([id])')){
  if(p.textContent.length<140||!p.textContent.includes('SQLite'))continue;
  const details=document.createElement('details');details.className='workspace-help';const summary=document.createElement('summary');summary.textContent='查看缓存统计与请求额度说明';p.replaceWith(details);details.append(summary,p);
 }
})();
