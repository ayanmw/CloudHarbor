const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '..', 'internal', 'webui');

test('cloud refresh history shows the persisted boundary restart reason safely',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/'});await new Promise(resolve=>setImmediate(resolve));
 const event={sequence:7,run_id:'run',kind:'boundary_restart',status:'running',phase:'listing',recorded_at_utc:'2026-11-02T00:00:00Z',restart_reason:'staged_page_duplicate_id',current_directory:'/safe',pages_processed:2,items_processed:100,pending_count:1,requests_used:2,request_budget:0};
 vm.runInContext(`refreshEvents=[${JSON.stringify(event)}];renderRefreshEvents()`,p.context);
 assert.match(p.get('refresh-events-list').innerHTML,/本地暂存页之间发现重复文件 ID/);
 assert.doesNotMatch(p.get('refresh-events-list').innerHTML,/<script/i);
});

test('live cloud page translates the reason for an automatic page reanchor',()=>{
 const html=fs.readFileSync(path.join(root,'index_cloud.html'),'utf8');
 assert.match(html,/boundary_change_reason/);
 assert.match(html,/百度返回重复文件 ID/);
 assert.match(html,/上一页边界身份变化/);
});

test('OAuth page provides first-time four-field app setup and masked read-only status',()=>{
 const html=fs.readFileSync(path.join(root,'index_oauth.html'),'utf8');
 assert.ok(html.includes('id="app-id"'));
 assert.ok(html.includes('id="app-key"'));
 assert.ok(html.includes('id="secret-key"'));
 assert.ok(html.includes('id="sign-key"'));
 assert.ok(html.includes('async function loadAppConfig()'));
 assert.ok(html.includes("fetch('/api/app-config'"));
 assert.ok(!html.includes('localStorage')&&!html.includes('sessionStorage'));
});

test('OAuth page shows only server masks after configuration and disables authorization while empty',async()=>{
 const p=page('index_oauth.html');await new Promise(resolve=>setImmediate(resolve));
 p.context.fetch=async url=>({ok:true,json:async()=>({configured:true,editable:false,source:'queue',app_id_mask:'ap***12',app_key_mask:'ke***34',secret_key_mask:'se***56',sign_key_mask:'si***78'})});
 await vm.runInContext('loadAppConfig()',p.context);
 assert.equal(p.get('app-config-form').hidden,true);assert.equal(p.get('app-config-masks').hidden,false);
 assert.equal(p.get('secret-key-mask').textContent,'se***56');assert.equal(p.get('sign-key-mask').textContent,'si***78');
 p.context.fetch=async url=>({ok:true,json:async()=>({configured:false,editable:true,source:''})});
 await vm.runInContext('loadAppConfig()',p.context);
 assert.equal(p.get('app-config-form').hidden,false);assert.equal(p.get('app-config-masks').hidden,true);assert.equal(p.get('device').disabled,true);
});

test('login page and management pages expose explicit logout without Basic auth',()=>{
 const login=fs.readFileSync(path.join(root,'index_login.html'),'utf8');
 assert.match(login,/id="password"/);assert.match(login,/fetch\('\/api\/login'/);assert.match(login,/new URL\(destination,location\.origin\)/);
 for(const name of ['index_ws.html','index_poll.html','index_intake.html','index_oauth.html']){
  const html=fs.readFileSync(path.join(root,name),'utf8');assert.match(html,/logoutAdmin\(\)/,name);
 }
 const shared=fs.readFileSync(path.join(root,'res','shared.js'),'utf8');assert.match(shared,/fetch\('\/api\/logout'/);
});

test('queue persistence failure is visible and prevents the pause control from implying a working scheduler',async()=>{
 for(const name of ['index_poll.html','index_ws.html']){
  const p=page(name);
  const data={queue_healthy:false,queue_pause:{paused:false,active_tasks:0},tasks:[],total_tasks:0,queue_total_tasks:0,has_more:false};
  if(name==='index_poll.html'){
   p.context.fetch=async()=>({ok:true,json:async()=>data});
   await vm.runInContext('fetchStatus()',p.context);
  }else{
   vm.runInContext(`renderTasks(${JSON.stringify(data)})`,p.context);
  }
  assert.equal(p.get('queue-health-alert').hidden,false,name);
  assert.match(p.get('queue-health-alert').textContent,/队列已停止/);
  assert.equal(p.get('queue-pause-toggle').disabled,true,name);
  vm.runInContext('renderQueueHealth(true); renderQueuePause({paused:false,active_tasks:0})',p.context);
  assert.equal(p.get('queue-health-alert').hidden,true,name);
  assert.equal(p.get('queue-pause-toggle').disabled,false,name);
 }
});

test('task pages show last cloud scan outcome and successful scan time',async()=>{
 for(const name of ['index_poll.html','index_ws.html']){
  const p=page(name);await new Promise(resolve=>setImmediate(resolve));const html=fs.readFileSync(path.join(root,name),'utf8');
  assert.match(html,/id="monitor-status-text"/);
  p.context.fetch=async()=>({ok:true,json:async()=>({status:'degraded',scan_error:'scan_failed',last_successful_scan:'2026-11-02T01:02:03Z',authentication:{ready:true,state:'ready'}})});
  await vm.runInContext('checkTokenStatus()',p.context);
  assert.match(p.get('monitor-status-text').textContent,/最近一次目录扫描失败/);
  assert.match(p.get('monitor-status-text').textContent,/2026-11-02T01:02:03Z/);
  p.context.fetch=async()=>({ok:true,json:async()=>({status:'degraded',scan_error:'scan_failed',scan_in_progress:true,last_successful_scan:'2026-11-02T01:02:03Z',authentication:{ready:true,state:'ready'}})});
  await vm.runInContext('checkTokenStatus()',p.context);
  assert.match(p.get('monitor-status-text').textContent,/正在执行目录扫描/);
  assert.doesNotMatch(p.get('monitor-status-text').textContent,/最近一次目录扫描失败/);
  vm.runInContext(`renderMonitorHealth({status:'ok',last_successful_scan:'2026-11-02T02:02:03Z'})`,p.context);
  assert.match(p.get('monitor-status-text').textContent,/目录监控正常/);
  assert.doesNotMatch(p.get('monitor-status-text').textContent,/scan_failed/);
  vm.runInContext(`renderMonitorHealth({status:'degraded',queue_monitor_retry_remaining_seconds:30,queue_monitor_retry_at_utc:'2026-11-02T03:30:00Z',last_successful_scan:'2026-11-02T02:02:03Z'})`,p.context);
  assert.match(p.get('monitor-status-text').textContent,/自动监控将在 2026-11-02T03:30:00Z（UTC）后重试/);
  vm.runInContext(`renderMonitorHealth({status:'degraded',scan_history_available:false,last_successful_scan_source:'runtime',last_successful_scan:'2026-11-02T04:02:03Z'})`,p.context);
  assert.match(p.get('monitor-status-text').textContent,/扫描历史暂不可读/);
  assert.match(p.get('monitor-status-text').textContent,/本进程/);
  assert.doesNotMatch(p.get('monitor-status-text').textContent,/目录监控正常/);
  vm.runInContext(`renderMonitorHealth({status:'starting',last_successful_scan:'2026-11-02T05:02:03Z'})`,p.context);
  assert.match(p.get('monitor-status-text').textContent,/目录监控正在启动/);
  assert.match(p.get('monitor-status-text').textContent,/2026-11-02T05:02:03Z/);
  assert.doesNotMatch(p.get('monitor-status-text').textContent,/目录监控正常/);
 }
});

test('task health polling keeps at most one request in flight',async()=>{
 for(const name of ['index_poll.html','index_ws.html']){
  const p=page(name);await new Promise(resolve=>setImmediate(resolve));const pending=[];let requestCount=0;
  p.context.fetch=()=>{requestCount++;return new Promise(resolve=>pending.push(resolve))};
  const first=vm.runInContext('checkTokenStatus()',p.context);
  const overlapping=vm.runInContext('checkTokenStatus()',p.context);
  assert.equal(requestCount,1,'a slow health endpoint must not accumulate interval requests');
  const response=data=>({ok:true,json:async()=>data});
  pending[0](response({status:'ok',last_successful_scan:'2026-11-02T03:00:00Z',authentication:{ready:true,state:'ready'}}));
  await Promise.all([first,overlapping]);
  assert.match(p.get('monitor-status-text').textContent,/2026-11-02T03:00:00Z/);
  assert.match(p.get('token-status-text').textContent,/账号可用/);
  p.context.fetch=async()=>{requestCount++;return response({status:'degraded',scan_error:'scan_failed',last_successful_scan:'2026-11-02T01:00:00Z',authentication:{ready:false,state:'token_rejected'}})};
  await vm.runInContext('checkTokenStatus()',p.context);
  assert.equal(requestCount,2,'polling resumes after the in-flight request settles');
  assert.match(p.get('monitor-status-text').textContent,/最近一次目录扫描失败/);
 }
});

test('poll task status keeps at most one request in flight',async()=>{
 const p=page('index_poll.html');await new Promise(resolve=>setImmediate(resolve));let resolveRequest;let requestCount=0;
 p.context.fetch=()=>{requestCount++;if(requestCount>1)return Promise.resolve({ok:true,json:async()=>({tasks:[],queue_healthy:true,queue_pause:{paused:false,active_tasks:0},has_more:false})});return new Promise(resolve=>{resolveRequest=resolve})};
 const first=vm.runInContext('fetchStatus(true)',p.context);
 const overlapping=vm.runInContext('fetchStatus(true)',p.context);
 assert.equal(requestCount,1,'a slow status endpoint must not accumulate interval requests');
 resolveRequest({ok:true,json:async()=>({tasks:[],queue_healthy:true,queue_pause:{paused:false,active_tasks:0},has_more:false})});
 await Promise.all([first,overlapping]);
 await vm.runInContext('fetchStatus(true)',p.context);
 assert.equal(requestCount,2,'polling resumes after the in-flight response settles');
});

test('failed task status reads mark queue state unavailable and disable controls',async()=>{
 const p=page('index_poll.html');await new Promise(resolve=>setImmediate(resolve));
 p.context.fetch=async()=>({ok:true,json:async()=>({tasks:[],queue_healthy:true,queue_pause:{paused:false,active_tasks:0},has_more:false})});
 await vm.runInContext('fetchStatus()',p.context);
 assert.equal(p.get('queue-pause-toggle').disabled,false);
 p.context.fetch=async()=>{throw new Error('connection lost')};
 await vm.runInContext('fetchStatus()',p.context);
 assert.equal(p.get('queue-pause-toggle').disabled,true);
 assert.match(p.get('queue-pause-status').textContent,/状态暂不可读/);
 assert.match(p.get('queue-health-alert').textContent,/状态暂不可读/);
 assert.doesNotMatch(p.get('queue-health-alert').textContent,/队列已停止/);
});

test('websocket disconnect invalidates the last confirmed queue control state',()=>{
 const p=page('index_ws.html');p.listeners.get('DOMContentLoaded')();
 const socket=p.sockets[0];
 socket.onmessage({data:JSON.stringify({type:'status',tasks:[],queue_healthy:true,queue_pause:{paused:false,active_tasks:0},has_more:false})});
 assert.equal(p.get('queue-pause-toggle').disabled,false);
 socket.onclose();
 assert.equal(p.get('queue-pause-toggle').disabled,true);
 assert.match(p.get('queue-pause-status').textContent,/状态暂不可读/);
 assert.match(p.get('queue-health-alert').textContent,/状态暂不可读/);
});

test('local storage diagnosis is rendered safely in polling and websocket task views and event history',()=>{
 const diagnostic={kind:'local_storage_blocked',path:'D:\\\\downloads <img src=x>',required_bytes:4096,available_bytes:-1,reserved_bytes:512,checked_at_utc:'2026-11-02T01:02:03Z'};
 for(const name of ['index_poll.html','index_ws.html']){
  let requestCount=0;
  const p=page(name,[{task_id:'123',is_root:true,status:'failed',path:'/remote/file.bin',local_path:'D:\\\\downloads',error:'space blocked',can_retry:true,local_storage:diagnostic}],{},async()=>{requestCount++;return{ok:true,json:async()=>({tasks:[]})}});
  const requestsBeforeRendering=requestCount;
  const html=name==='index_poll.html'?fs.readFileSync(path.join(root,name),'utf8'):'';
  const renderer=vm.runInContext('localStorageDiagnosticMarkup',p.context);
  assert.equal(typeof renderer,'function');
  const markup=renderer(diagnostic);
  assert.match(markup,/D:\\\\downloads &lt;img src=x&gt;/);
  assert.match(markup,/4096/);assert.match(markup,/无法读取/);assert.match(markup,/下载前无法确认可用空间/);assert.match(markup,/本机空间核对时间（UTC）/);
  assert.doesNotMatch(markup,/<img/);
  if(html) assert.match(html,/localStorageDiagnosticMarkup\(t\.local_storage\)/);
  else assert.match(fs.readFileSync(path.join(root,name),'utf8'),/localStorageDiagnosticMarkup\(t\.local_storage\)/);
  const controls=vm.runInContext("taskControls({is_root:true,task_id:'123',can_retry:true})",p.context);
  assert.match(controls,/重试任务/);
  assert.equal(requestCount,requestsBeforeRendering,'showing the diagnosis must not retry automatically');
 }
 const blocked=vm.runInContext("localStorageDiagnosticMarkup({kind:'local_storage_blocked',path:'x',required_bytes:1,available_bytes:0,reserved_bytes:0})",page('index_poll.html').context);
 const full=vm.runInContext("localStorageDiagnosticMarkup({kind:'local_storage_full',path:'x',required_bytes:1,available_bytes:0,reserved_bytes:0})",page('index_poll.html').context);
 assert.match(blocked,/下载前可用空间不足/);assert.match(full,/写入时磁盘空间耗尽/);
 const p=page('index_poll.html');
 const event={sequence:1,kind:'download_attempt_finished',evidence_type:'local_attempt_result',recorded_at_utc:'2026-11-02T01:02:04Z',evidence:{outcome_class:'local_storage_blocked',local_storage:diagnostic}};
 const markup=vm.runInContext(`taskEventMarkup({loaded:true,events:[${JSON.stringify(event)}]})`,p.context);
 assert.match(markup,/本机空间核对时间（UTC）/);assert.match(markup,/本机记录时间（UTC）/);assert.match(markup,/local_storage_blocked/);
 assert.match(markup,/结构化证据/);assert.doesNotMatch(markup,/<img/);
});

test('poll and websocket pages display queue pause state and only change it after an explicit click',async()=>{
 for(const name of ['index_poll.html','index_ws.html']){
  const html=fs.readFileSync(path.join(root,name),'utf8');
  assert.match(html,/id="queue-pause-status"/);assert.match(html,/id="queue-pause-toggle"/);assert.match(html,/bindQueuePauseControl\(/);
  const p=page(name);
  assert.equal(typeof vm.runInContext('renderQueuePause',p.context),'function');
  vm.runInContext('renderQueueHealth(true)',p.context);
  vm.runInContext("renderQueuePause({paused:true,active_tasks:2,updated_at_utc:'2026-11-02T01:02:03Z'})",p.context);
  assert.match(p.get('queue-pause-status').textContent,/已暂停/);
  assert.match(p.get('queue-pause-status').textContent,/2 个下载/);
  assert.match(p.get('queue-pause-status').textContent,/UTC/);
  assert.equal(p.get('queue-pause-toggle').textContent,'恢复下载队列');
  vm.runInContext('renderQueuePause({paused:false,maintenance:true,active_tasks:0})',p.context);
  assert.match(p.get('queue-pause-status').textContent,/维护中/);
  assert.doesNotMatch(p.get('queue-pause-status').textContent,/下载队列运行中/);
  vm.runInContext('renderQueuePause({paused:true,maintenance:false,active_tasks:2,updated_at_utc:"2026-11-02T01:02:03Z"})',p.context);
  let request=null;
  p.context.fetch=async(url,options={})=>{request={url:String(url),method:options.method};return{ok:true,json:async()=>({queue_healthy:true,queue_pause:{paused:false,active_tasks:2,updated_at_utc:'2026-11-02T01:03:03Z'}})}};
  vm.runInContext('bindQueuePauseControl(async()=>{})',p.context);
  assert.equal(request,null,'the page must not change queue state without a click');
  await p.listeners.get('queue-pause-toggle:click')({preventDefault(){}});
  assert.deepEqual(request,{url:'/api/queue/resume',method:'POST'});
  assert.match(p.get('queue-pause-status').textContent,/正在运行/);
  assert.equal(p.get('queue-pause-toggle').textContent,'暂停下载队列');
  p.context.fetch=async()=>({ok:false,status:503});
  await p.listeners.get('queue-pause-toggle:click')({preventDefault(){}});
  assert.match(p.get('queue-pause-feedback').textContent,/状态未更改/);
  assert.match(p.get('queue-pause-status').textContent,/正在运行/,'failed control must retain last server-confirmed state');
 }
});

test('task pause and resume controls use explicit actions and show the durable paused state',async()=>{
 for(const name of ['index_poll.html','index_ws.html']){
  const p=page(name,[{task_id:'123',is_root:true,status:'paused',can_pause:false,can_resume:true}]);
  const filter=fs.readFileSync(path.join(root,name),'utf8');assert.match(filter,/<option value="paused">已暂停<\/option>/);
  const status=vm.runInContext("getStatusBadge('paused')",p.context);assert.match(status,/已暂停/);
  const control=vm.runInContext("taskControls({is_root:true,task_id:'123',status:'paused',can_pause:false,can_resume:true})",p.context);
  assert.match(control,/继续此任务/);assert.match(control,/已验证的本地数据/);assert.doesNotMatch(control,/暂停此任务/);
  let request=null;p.context.fetch=async(url,options={})=>{request={url:String(url),method:options.method};return{ok:true,json:async()=>({})}};
  assert.equal(request,null,'rendering a paused task must not submit an action');
  await vm.runInContext("performTaskAction('123','resume',async()=>{})",p.context);
  assert.deepEqual(request,{url:'/api/tasks/123/resume',method:'POST'});
  assert.match(p.get('task-feedback').textContent,/继续已记录/);
  p.context.fetch=async()=>({ok:false,status:503});
  await vm.runInContext("performTaskAction('123','pause',async()=>{})",p.context);
  assert.match(p.get('task-feedback').textContent,/队列存储暂不可用/);
 }
});

function page(name, tasks = [], initialValues = {}, fetchImpl = null) {
    const nodes = new Map();
    const get = id => {
        if (!nodes.has(id)) nodes.set(id, {style:{}, textContent:'', innerHTML:'', isConnected:true, addEventListener:(event,fn)=>listeners.set(id+':'+event,fn),querySelector:()=>get(id+' span')});
        return nodes.get(id);
    };
    const listeners = new Map();
    const intervals = [], clearedIntervals = [];
    const sockets = [];
    class WebSocket {
        static OPEN = 1;
        constructor() { this.sent=[]; sockets.push(this); }
        send(message) { this.sent.push(message); }
    }
    const context = vm.createContext({URL, URLSearchParams, AbortController, console, WebSocket, location:{protocol:'http:',host:'localhost',pathname:'/',search:'',replace(){}},
        document:{getElementById:get, querySelectorAll:()=>[], addEventListener:(event,fn)=>listeners.set(event,fn)},
        setTimeout:()=>0, setInterval:(callback,delay)=>{const id=intervals.length+1;intervals.push({id,callback,delay});return id;}, clearInterval:id=>clearedIntervals.push(id),
        fetch:fetchImpl|| (async url=>({ok:true,json:async()=>url.includes('status') && !url.includes('oauth') ? {tasks} : {authentication:{state:'unconfigured',ready:false},state:'unconfigured',ready:false}}))
    });
    for (const [id, value] of Object.entries(initialValues)) get(id).value = value;
    vm.runInContext(fs.readFileSync(path.join(root, 'res', 'shared.js'), 'utf8'), context);
    const html = fs.readFileSync(path.join(root, name), 'utf8');
    for (const match of html.matchAll(/<script>([\s\S]*?)<\/script>/g)) vm.runInContext(match[1], context);
    if(html.includes('/res/intake.js'))vm.runInContext(fs.readFileSync(path.join(root,'res','intake.js'),'utf8'),context);
    if(html.includes('/res/cloud_operations.js'))vm.runInContext(fs.readFileSync(path.join(root,'res','cloud_operations.js'),'utf8'),context);
    if(html.includes('/res/monitor_history.js'))vm.runInContext(fs.readFileSync(path.join(root,'res','monitor_history.js'),'utf8'),context);
    if(html.includes('/res/task_event_history.js'))vm.runInContext(fs.readFileSync(path.join(root,'res','task_event_history.js'),'utf8'),context);
    return {context,get,listeners,sockets,intervals,clearedIntervals};
}

test('global task event history is a separate read-only page linked from both task views',()=>{
 const html=fs.readFileSync(path.join(root,'index_task_events.html'),'utf8');
 assert.match(html,/全局任务事件历史/);assert.match(html,/本机 SQLite/);assert.match(html,/只读/);
 for(const id of ['task-event-task-id','task-event-kind','task-event-operation-id','task-event-attempt-id','task-event-from-date','task-event-through-date','task-event-from-exact','task-event-before-exact','task-event-list','task-event-kinds','task-event-export'])assert.match(html,new RegExp(`id="${id}"`));
 for(const name of ['index_poll.html','index_ws.html'])assert.match(fs.readFileSync(path.join(root,name),'utf8'),/href="\/task-events"[^>]*>任务事件历史/);
 assert.match(fs.readFileSync(path.join(root,'webui.go'),'utf8'),/go:embed index_task_events\.html/);
});

test('global task event paging preserves exact 64-bit snapshot and cursor values',async()=>{
 const requests=[];const p=page('index_task_events.html',[],{},async url=>{
  requests.push(String(url));const query=new URL(url,'http://localhost').searchParams;
  return{ok:true,json:async()=>({events:[{job_id:'9007199254740993',sequence:query.has('before_sequence')?'9007199254740991':'9007199254740992',kind:'task_retry_requested',recorded_at_utc:'2026-11-02T10:00:00Z',evidence:{safe:true}}],total_matches:2,as_of_sequence:'9007199254740993',next_before_sequence:query.has('before_sequence')?null:'9007199254740991',has_more:!query.has('before_sequence'),kind_counts:[{kind:'task_retry_requested',count:2}]})};
 });
 await new Promise(resolve=>setImmediate(resolve));
 assert.ok(requests[0].includes('limit=50'));
 p.listeners.get('task-event-next:click')();await new Promise(resolve=>setImmediate(resolve));
 const next=new URL(requests.at(-1),'http://localhost').searchParams;
 assert.equal(next.get('as_of_sequence'),'9007199254740993');assert.equal(next.get('before_sequence'),'9007199254740991');
 assert.match(p.get('task-event-list').innerHTML,/9007199254740993/);assert.equal(p.get('task-event-summary').textContent.includes('2'),true);
 p.listeners.get('task-event-prev:click')();await new Promise(resolve=>setImmediate(resolve));
 const previous=new URL(requests.at(-1),'http://localhost').searchParams;
 assert.equal(previous.get('as_of_sequence'),'9007199254740993');assert.equal(previous.has('before_sequence'),false);
});

test('global task event type shortcut preserves other filters and export uses the pinned snapshot',async()=>{
 const requests=[],downloads=[];const p=page('index_task_events.html',[],{'task-event-task-id':'42','task-event-operation-id':'operation-1','task-event-attempt-id':'attempt-1','task-event-from-date':'2026-11-02'},async(url)=>{
  requests.push(String(url));if(String(url).includes('/export'))return{ok:true,headers:{get:()=> 'attachment; filename="task-events-global-evidence.json"'},blob:async()=>({size:12})};
  return{ok:true,json:async()=>({events:[{job_id:'42',sequence:'17',kind:'source_operation_unknown',recorded_at_utc:'2026-11-02T10:00:00Z',evidence:{token:'[REDACTED]'}}],total_matches:1,as_of_sequence:'17',has_more:false,kind_counts:[{kind:'source_operation_unknown',count:1}]})};
 });
 p.context.document.createElement=()=>({click(){downloads.push(this.download)},remove(){}});p.context.document.body={append(){}};p.context.URL.createObjectURL=()=> 'blob:task-events';p.context.URL.revokeObjectURL=()=>{};
 await new Promise(resolve=>setImmediate(resolve));
 assert.ok(p.listeners.get('task-event-kinds:click'),`missing listener; rendered=${p.get('task-event-kinds').innerHTML}`);
 assert.match(p.get('task-event-kinds').innerHTML,/data-task-event-kind="source_operation_unknown"/);
 p.listeners.get('task-event-kinds:click')({target:{closest:selector=>selector==='[data-task-event-kind]'?{dataset:{taskEventKind:'source_operation_unknown'}}:null}});
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(p.context.globalTaskEventHistoryState.filters.kind,'source_operation_unknown');
 assert.ok(requests.length>=2,`expected type filter request, calls=${requests.join('|')}`);
 let query=new URL(requests.at(-1),'http://localhost').searchParams;
 assert.equal(query.get('task_id'),'42');assert.equal(query.get('operation_id'),'operation-1');assert.equal(query.get('attempt_id'),'attempt-1');assert.equal(query.get('from_utc'),'2026-11-02');assert.equal(query.get('kind'),'source_operation_unknown');
 p.listeners.get('task-event-export:click')();await new Promise(resolve=>setImmediate(resolve));
 query=new URL(requests.at(-1),'http://localhost').searchParams;
 assert.equal(query.get('as_of_sequence'),'17');assert.equal(query.get('task_id'),'42');assert.equal(query.get('operation_id'),'operation-1');assert.equal(query.get('attempt_id'),'attempt-1');assert.equal(query.get('kind'),'source_operation_unknown');
 assert.deepEqual(downloads,['task-events-global-evidence.json']);
});

async function waitForPollIdle(p, flag) {
    for (let attempt = 0; attempt < 20; attempt++) {
        if (!vm.runInContext(flag, p.context)) return;
        await new Promise(resolve => setImmediate(resolve));
    }
    throw new Error(`${flag} did not become idle`);
}

test('remote operation history has its own read-only page linked from cloud management',()=>{
 const cloud=fs.readFileSync(path.join(root,'index_cloud.html'),'utf8');const history=fs.readFileSync(path.join(root,'index_operations.html'),'utf8');
 assert.match(cloud,/href="\/operations"[^>]*>只读操作历史/);
 assert.match(history,/远端操作历史（只读）/);assert.match(history,/href="\/cloud"/);assert.match(history,/退出登录/);
 assert.doesNotMatch(history,/id="directory"|id="mkdir"|id="empty-delete"|id="duplicate-delete-all"|api\/cloud\/mutations/);
});

test('managed path form preserves meaningful boundary whitespace',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 let submitted=null;
 p.context.fetch=async(url,options={})=>{if(String(url).includes('/api/settings/paths?queue_dir='))return{ok:true,json:async()=>({queue_change_impact:{current_queue_dir:'/monitor',target_queue_dir:'/monitor ',count:0,tasks:[],digest:'whitespace-preview'}})};if(String(url)==='/api/settings/paths'&&options.method==='POST')submitted=JSON.parse(options.body);return{ok:true,json:async()=>({queue_dir:'/monitor ',local_download_dir:'downloads'})};};
 p.get('queue-path').value='/monitor ';p.get('local-path').value='downloads';
 await p.get('path-form').onsubmit({preventDefault(){}});
 assert.equal(submitted.queue_dir,'/monitor ');assert.equal(submitted.local_download_dir,'downloads');
});

test('cloud management redirects expired admin sessions to login with a return path',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 let destination='';p.context.location.pathname='/cloud';p.context.location.search='?folder=%2Fcurrent';p.context.location.replace=url=>{destination=url};
 p.context.fetch=async()=>({ok:false,status:401,text:async()=>'administrator authentication required'});
 await assert.rejects(vm.runInContext("api('/api/settings/paths')",p.context),/管理员登录已失效/);
 assert.equal(destination,'/login?next=%2Fcloud%3Ffolder%3D%252Fcurrent');
});

test('cloud management migration evidence export handles expired admin session',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 let destination='';p.context.location.pathname='/cloud';p.context.location.search='';p.context.location.replace=url=>{destination=url};
 p.context.fetch=async()=>({ok:false,status:401,text:async()=>'administrator authentication required'});
 await vm.runInContext("exportMigrationEvidence()",p.context);
 assert.equal(destination,'/login?next=%2Fcloud');
 assert.match(p.get('migration-export-status').textContent,/管理员登录已失效/);
});

test('cloud inventory refresh exports redirect expired sessions',async()=>{
 for(const [setup,call] of [
  ['',"exportRefreshRunSummaries()"],
  ["refreshEventsRunID='run-1'","exportRefreshRunEvidence()"],
  ["refreshEventsRunID='run-1';refreshEvents=[{sequence:7}];refreshChangePages.set('7',{loaded:true,exporting:false,kind:'',query:'',notice:''})","exportRefreshChanges(7)"]
 ]){
  const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});let target='',calls=0;p.context.location.pathname='/cloud';p.context.location.search='?view=refresh';p.context.location.replace=url=>{target=url;};p.context.fetch=async()=>{calls++;return{ok:false,status:401,text:async()=>'administrator authentication required'}};
  if(setup)vm.runInContext(setup,p.context);
  await vm.runInContext(call,p.context);
  assert.equal(target,'/login?next=%2Fcloud%3Fview%3Drefresh');assert.equal(calls,1,call);
 }
});

test('cloud management stops background status polling after an expired session',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});p.context.location.pathname='/cloud';p.context.location.search='?tab=cache';p.context.location.replace=()=>{};p.context.fetch=async()=>({ok:false,status:401,text:async()=>''});
 await assert.rejects(vm.runInContext("api('/api/settings/paths')",p.context),/管理员登录已失效/);
 assert.deepEqual(p.clearedIntervals.filter(id=>id!==null),p.intervals.map(item=>item.id));
});

test('shared admin session recovery keeps the current page and redirects only once',()=>{
 const p=page('index_operations.html');let target='',navigations=0;p.context.location.pathname='/operations';p.context.location.search='?status=unknown&from=%2F';p.context.location.replace=url=>{target=url;navigations++;};
 assert.equal(vm.runInContext('adminSessionExpired()',p.context),'管理员登录已失效，请重新登录。');
 vm.runInContext('adminSessionExpired()',p.context);
 assert.equal(target,'/login?next=%2Foperations%3Fstatus%3Dunknown%26from%3D%252F');assert.equal(navigations,1);
});

test('intake requests redirect expired sessions without replaying the request',async()=>{
 const p=page('index_intake.html');let target='',calls=0;p.context.location.pathname='/intake';p.context.location.search='?tab=history';p.context.location.replace=url=>{target=url;};
 p.context.fetch=async()=>{calls++;return{ok:false,status:401,text:async()=>'{"error":"administrator authentication required"}'}};
 await assert.rejects(vm.runInContext("intakeRequest('/api/intakes/preview',{paths:['/folder']})",p.context),/管理员登录已失效/);
 assert.equal(target,'/login?next=%2Fintake%3Ftab%3Dhistory');assert.equal(calls,1);
});

test('OAuth status 401 redirects and stops its periodic status polling',async()=>{
 const p=page('index_oauth.html');let target='',calls=0;p.context.location.pathname='/oauth';p.context.location.search='?view=credentials';p.context.location.replace=url=>{target=url;};p.context.fetch=async()=>{calls++;return{ok:false,status:401,text:async()=>''}};
 await vm.runInContext('updateStatus()',p.context);assert.equal(target,'/login?next=%2Foauth%3Fview%3Dcredentials');
 assert.ok(p.intervals.some(item=>item.delay===5000&&p.clearedIntervals.includes(item.id)));
});

test('OAuth protected actions redirect expired sessions without replay',async()=>{
 const p=page('index_oauth.html');let target='',calls=0;p.context.location.pathname='/oauth';p.context.location.search='';p.context.location.replace=url=>{target=url;};p.context.fetch=async()=>{calls++;return{ok:false,status:401,text:async()=>'{"error":"administrator authentication required"}'}};
 await assert.rejects(vm.runInContext("request('/api/oauth/device-code')",p.context),/管理员登录已失效/);
 assert.equal(target,'/login?next=%2Foauth');assert.equal(calls,1);
});

test('remote operation history redirects list 401 and does not retry',async()=>{
 const p=page('index_operations.html');let target='',calls=0;p.context.location.pathname='/operations';p.context.location.search='?kind=move';p.context.location.replace=url=>{target=url;};p.context.fetch=async()=>{calls++;return{ok:false,status:401,text:async()=>''}};
 const details=p.get('remote-operation-details');details.open=true;p.listeners.get('remote-operation-details:toggle')({currentTarget:details});await new Promise(resolve=>setImmediate(resolve));
 assert.equal(target,'/login?next=%2Foperations%3Fkind%3Dmove');assert.equal(calls,1);
});

test('task actions redirect expired sessions without repeating the action',async()=>{
 const p=page('index_ws.html');let target='',calls=0;p.context.location.pathname='/';p.context.location.search='?status=failed';p.context.location.replace=url=>{target=url;};p.context.fetch=async()=>{calls++;return{ok:false,status:401,text:async()=>''}};
 await vm.runInContext("performTaskAction('123','retry',async()=>{})",p.context);
 assert.equal(target,'/login?next=%2F%3Fstatus%3Dfailed');assert.equal(calls,1);
});

test('task event reads redirect expired sessions and preserve the current task page',async()=>{
 const p=page('index_ws.html');let target='',calls=0;p.context.location.pathname='/';p.context.location.search='?task=123';p.context.location.replace=url=>{target=url;};p.context.fetch=async()=>{calls++;return{ok:false,status:401,text:async()=>''}};
 await vm.runInContext("fetchTaskEventPage('123',null)",p.context);
 assert.equal(target,'/login?next=%2F%3Ftask%3D123');assert.equal(calls,1);
});

test('task list polling and account health checks redirect expired sessions',async()=>{
 for(const pageName of ['index_ws.html','index_poll.html']){
  for(const action of ['refreshCurrentPage()','fetchStatus()','checkTokenStatus()']){
   if(pageName==='index_poll.html'&&action==='refreshCurrentPage()')continue;
   if(pageName==='index_ws.html'&&action==='fetchStatus()')continue;
   const p=page(pageName);await new Promise(resolve=>setImmediate(resolve));let target='',calls=0;p.context.location.pathname='/';p.context.location.search='?status=active';p.context.location.replace=url=>{target=url;};p.context.fetch=async()=>{calls++;return{ok:false,status:401,text:async()=>''}};
   try{await vm.runInContext(action,p.context);}catch{}
   assert.equal(target,'/login?next=%2F%3Fstatus%3Dactive',`${pageName}: ${action}`);assert.equal(calls,1);
   assert.deepEqual(p.clearedIntervals.filter(id=>id!==null),p.intervals.map(item=>item.id),`${pageName}: ${action} should stop protected polling`);
   for(const interval of p.intervals)await interval.callback();assert.equal(calls,1,`${pageName}: ${action} should not issue another poll`);
  }
 }
});

test('cloud scan status failures retain last known state instead of claiming no record',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));let available=true;
 p.context.fetch=async url=>{
  if(!available)return{ok:false,status:503,text:async()=>'database temporarily unavailable'};
  const refresh=String(url).includes('/refresh');
  return{ok:true,json:async()=>refresh?{status:'running',root_path:'/root',phase:'listing',pages_processed:2,items_processed:5}:{status:'running',phase:'listing',current_directory:'/root',pending_count:1,pages_processed:3,items_processed:7}};
 };
 await vm.runInContext('Promise.all([scanStatus(),directoryRefreshStatus()])',p.context);
 const scanBefore=p.get('scan-summary').textContent,refreshBefore=p.get('directory-refresh-summary').textContent;
 available=false;await vm.runInContext('Promise.all([scanStatus(),directoryRefreshStatus()])',p.context);
 assert.notEqual(p.get('scan-summary').textContent,scanBefore);assert.match(p.get('scan-summary').textContent,/当前扫描\/缓存状态暂不可读/);assert.match(p.get('scan-summary').textContent,/上次成功读取/);assert.match(p.get('scan-progress').textContent,/读取扫描状态失败/);
 assert.equal(p.get('directory-refresh-summary').textContent,refreshBefore);assert.match(p.get('directory-refresh-progress').textContent,/读取目录刷新状态失败/);
 assert.doesNotMatch(p.get('scan-summary').textContent,/尚无缓存扫描记录/);assert.doesNotMatch(p.get('directory-refresh-summary').textContent,/尚无目录刷新记录/);
});

test('cloud scanner status polling never overlaps requests',async()=>{
 for(const [action,endpoint] of [['scanStatus()','/api/cloud-inventory/scan'],['directoryRefreshStatus()','/api/cloud-inventory/refresh'],['duplicateStatus()','/api/cloud-cleanup/duplicate-scan']]){
  const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await waitForPollIdle(p,`${action.startsWith('scanStatus')?'scanStatusInFlight':action.startsWith('directoryRefreshStatus')?'directoryRefreshStatusInFlight':'duplicateStatusInFlight'}`);let release,active=0,maxActive=0,calls=0;
  p.context.fetch=async url=>{if(String(url)!==endpoint)return{ok:true,json:async()=>({})};calls++;active++;maxActive=Math.max(maxActive,active);if(calls===1)await new Promise(resolve=>{release=resolve});active--;return{ok:true,json:async()=>({status:'running',phase:'listing',root_path:'/',current_directory:'/',pending_count:1,pages_processed:calls,items_processed:calls})};};
  const first=vm.runInContext(action,p.context);await new Promise(resolve=>setImmediate(resolve));const second=vm.runInContext(action,p.context);await second;
  assert.equal(calls,1,`${action} should skip a poll while its prior request is pending`);release();await first;await vm.runInContext(action,p.context);
  assert.equal(calls,2);assert.equal(maxActive,1,action);
 }
});

test('duplicate scan status polling retains the last state after a temporary API failure',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await waitForPollIdle(p,'duplicateStatusInFlight');let available=true;
 p.context.fetch=async url=>String(url)==='/api/cloud-cleanup/duplicate-scan'?(available?{ok:true,json:async()=>({status:'running',phase:'hashing',processed_files:3,total_files:9,processed_bytes:300,current_bytes:30,total_bytes:900})}:{ok:false,status:503,text:async()=>'database temporarily unavailable'}):{ok:true,json:async()=>({})};
 await vm.runInContext('duplicateStatus()',p.context);const before=p.get('duplicate-summary').textContent;
 available=false;await vm.runInContext('duplicateStatus()',p.context);
 assert.equal(p.get('duplicate-summary').textContent,before);assert.match(p.get('duplicate-progress').textContent,/database temporarily unavailable/);
});

test('migration status polling never overlaps requests',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));let release,active=0,maxActive=0,calls=0;
 p.context.fetch=async url=>{if(String(url)!=='/api/settings/local-migration')return{ok:true,json:async()=>({})};calls++;active++;maxActive=Math.max(maxActive,active);if(calls===1)await new Promise(resolve=>{release=resolve});active--;return{ok:true,json:async()=>({status:'running',phase:'copying',source_path:'old',target_path:'new',processed_files:1,total_files:2,processed_bytes:10,total_bytes:20})};};
 const first=vm.runInContext('pollMigration()',p.context);await new Promise(resolve=>setImmediate(resolve));const second=vm.runInContext('pollMigration()',p.context);await second;
 assert.equal(calls,1);release();await first;await vm.runInContext('pollMigration()',p.context);
 assert.equal(calls,2);assert.equal(maxActive,1);
});

test('local migration status polling retains the last progress after a temporary API failure',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));let available=true;
 p.context.fetch=async url=>String(url)==='/api/settings/local-migration'?(available?{ok:true,json:async()=>({status:'running',phase:'copying',source_path:'old',target_path:'new',processed_files:4,total_files:9,processed_bytes:400,total_bytes:900,started_at_utc:'start',updated_at_utc:'update'})}:{ok:false,status:503,text:async()=>'database temporarily unavailable'}):{ok:true,json:async()=>({})};
 await vm.runInContext('pollMigration()',p.context);const before=p.get('migration-progress').textContent;
 available=false;await vm.runInContext('pollMigration()',p.context);
 assert.match(before,/4 \/ 9/);assert.match(p.get('migration-progress').textContent,/4 \/ 9/);assert.match(p.get('migration-progress').textContent,/database temporarily unavailable/);
});

test('inventory scan status shows local retry deadline and remaining wait',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const deadline=new Date(Date.now()+30000).toISOString();
 p.context.fetch=async url=>({ok:true,json:async()=>String(url).includes('/refresh')?{status:'running',root_path:'/root',phase:'backoff',pages_processed:2,items_processed:50,retry_at_utc:deadline,retry_delay_millis:30000}:{status:'running',phase:'backoff',current_directory:'/',pending_count:1,pages_processed:3,items_processed:60,retry_at_utc:deadline,retry_delay_millis:30000}});
 await vm.runInContext('scanStatus()',p.context);await vm.runInContext('directoryRefreshStatus()',p.context);
 assert.match(p.get('scan-summary').textContent,/下次重试/);assert.match(p.get('scan-progress').textContent,/本机重试计划 UTC/);assert.match(p.get('scan-progress').textContent,/预计等待 30 秒/);
 assert.match(p.get('directory-refresh-summary').textContent,/下次重试/);assert.match(p.get('directory-refresh-progress').textContent,/本机重试计划 UTC/);assert.match(p.get('directory-refresh-progress').textContent,/预计等待 30 秒/);
});

test('full scan completion is shown separately from incomplete current cache coverage',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));const calls=[];
 p.context.fetch=async url=>{calls.push(String(url));return{ok:true,json:async()=>({status:'complete',phase:'complete',pending_count:0,pages_processed:12,items_processed:300,worker_active:false,inventory_coverage:{status:'incomplete',incomplete_directories:1,cache_generation:5946,scan_updated_at_utc:'2026-11-02T03:00:00Z',incomplete_reasons:{stale_directories:976,missing_directory_status:3,parent_not_complete:2,path_mismatch:1}}})}};
 await vm.runInContext('scanStatus()',p.context);
 assert.match(p.get('scan-summary').textContent,/扫描运行 complete/);
 assert.match(p.get('scan-summary').textContent,/缓存覆盖 incomplete/);
 assert.match(p.get('scan-progress').textContent,/976 个 stale/);assert.match(p.get('scan-progress').textContent,/缺少状态的缓存目录 3/);assert.match(p.get('scan-progress').textContent,/父目录未完整的缓存子项 2/);assert.match(p.get('scan-progress').textContent,/路径异常条目 1/);assert.match(p.get('scan-progress').textContent,/结构原因可重叠/);
 assert.match(p.get('scan-progress').textContent,/1 个目录未完整/);
 assert.match(p.get('scan-progress').textContent,/generation 5946/);
 assert.match(p.get('scan-progress').textContent,/扫描状态更新时间 2026-11-02T03:00:00Z/);
 assert.equal(calls.some(url=>url.startsWith('/api/cloud?')),false,'reading scan and cache status must not call the Baidu listing API');
});

test('full scan status distinguishes an inactive persisted checkpoint from an active worker',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 let workerActive=false,status='running';
 p.context.fetch=async url=>String(url)==='/api/cloud-inventory/scan'?{ok:true,json:async()=>({status,phase:'listing',current_directory:'/large',pending_count:2,pages_processed:8,items_processed:800,worker_active:workerActive})}:{ok:true,json:async()=>({})};
 await vm.runInContext('scanStatus()',p.context);
 assert.match(p.get('scan-summary').textContent,/本进程无扫描 worker/);
 assert.match(p.get('scan-summary').textContent,/开始 \/ 继续扫描.*恢复/);
 assert.match(p.get('scan-progress').textContent,/worker：未运行/);
 assert.equal(!!p.get('scan-start').disabled,false);
 workerActive=true;await vm.runInContext('scanStatus()',p.context);
 assert.match(p.get('scan-summary').textContent,/worker 运行中/);
 assert.doesNotMatch(p.get('scan-summary').textContent,/点击.*恢复/);
 assert.equal(!!p.get('scan-start').disabled,true,'do not offer another start request while the current worker is active');
 assert.equal(!!p.get('refresh-directory').disabled,true,'a full-scan worker must block scoped refresh starts');
 status='failed';workerActive=false;await vm.runInContext('scanStatus()',p.context);
 assert.equal(!!p.get('scan-start').disabled,false,'allow resuming the failed checkpoint after its worker exits');
 assert.equal(!!p.get('refresh-directory').disabled,false);
});

test('full scan submits request budget and displays durable usage and boundary rechecks',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const calls=[];p.context.fetch=async(url,options={})=>{calls.push({url:String(url),options});return{ok:true,json:async()=>String(url)==='/api/cloud-inventory/scan'?{status:'paused',phase:'request_budget_reached',request_budget:12,requests_used:12,pending_count:1,current_directory:'/large',pages_processed:12,items_processed:1200,boundary_restart_count:1}:{}};};
 p.get('scan-budget').value='12';await vm.runInContext("$('scan-start').onclick()",p.context);await waitForPollIdle(p,'scanStatusInFlight');
 assert.equal(calls.length,2,'start submits once and refreshes scan status');
 assert.deepEqual(JSON.parse(calls[0].options.body),{request_budget:12});
 assert.match(p.get('scan-summary').textContent,/已预留 12 \/ 12 次/);assert.match(p.get('scan-summary').textContent,/额度用尽/);assert.match(p.get('scan-summary').textContent,/边界重扫 1 \/ 2/);
 assert.match(p.get('scan-progress').textContent,/已预留百度列表请求：12 \/ 12/);
 assert.match(p.get('scan-progress').textContent,/边界自动重扫：1 \/ 2/);
});

test('full scan history separates captured historical coverage from unknown legacy rows',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const calls=[];p.context.fetch=async url=>{calls.push(String(url));return{ok:true,json:async()=>String(url).includes('/events')?{events:[{kind:'boundary_restart',status:'running',phase:'listing',current_directory:'/x',recorded_at_utc:'2026-10-31T12:00:00Z',pages_processed:2,items_processed:20,request_budget:3,requests_used:2,pending_count:1,last_error:'<script>bad</script>'}],next_before_sequence:0,has_more:false}:{runs:[{sequence:9,run_id:'run-abc',status:'complete',phase:'complete',current_directory:'/',started_at_utc:'2026-10-31T11:00:00Z',pages_processed:3,items_processed:30,request_budget:5,requests_used:3,pending_count:0,coverage_status:'complete',coverage_incomplete_directories:0,coverage_generation:42,coverage_observed_at_utc:'2026-10-31T11:03:00Z',coverage_reasons:{stale_directories:2},coverage_comparison:{status:'comparable',reason_status:'comparable',previous_run_id:'run-prev',previous_sequence:7,previous_observed_at_utc:'2026-10-30T11:03:00Z',previous_coverage_status:'incomplete',previous_incomplete_directories:4,incomplete_directories_delta:-4,reason_deltas:{stale_directories:-2,missing_root:-1}}},{sequence:8,run_id:'run-legacy',status:'complete',phase:'complete',current_directory:'/',started_at_utc:'2026-10-30T11:00:00Z',pages_processed:2,items_processed:20,request_budget:0,requests_used:2,pending_count:0,coverage_comparison:{status:'no_previous_run'}}],next_before_sequence:0,has_more:false}};};
 await vm.runInContext('loadScanHistory()',p.context);assert.match(p.get('scan-history-list').innerHTML,/run-abc/);assert.match(p.get('scan-history-list').innerHTML,/扫描完成时缓存覆盖 完整/);assert.match(p.get('scan-history-list').innerHTML,/generation 42/);assert.match(p.get('scan-history-list').innerHTML,/2026-10-31T11:03:00Z/);assert.match(p.get('scan-history-list').innerHTML,/当前缓存状态可能已变化/);assert.match(p.get('scan-history-list').innerHTML,/2 个 stale/);assert.match(p.get('scan-history-list').innerHTML,/与上一完成轮 run-prev/);assert.match(p.get('scan-history-list').innerHTML,/2026-10-30T11:03:00Z/);assert.match(p.get('scan-history-list').innerHTML,/未完整目录变化 -4/);assert.match(p.get('scan-history-list').innerHTML,/stale -2/);assert.match(p.get('scan-history-list').innerHTML,/run-legacy/);assert.match(p.get('scan-history-list').innerHTML,/没有更早的完成扫描可比较/);assert.match(p.get('scan-history-list').innerHTML,/历史未记录覆盖快照/);assert.match(p.get('scan-history-list').innerHTML,/历史未记录覆盖原因/);assert.match(calls[0],/^\/api\/cloud-inventory\/scan\/runs/);
 const previousMissing=vm.runInContext("cloudScanCoverageComparisonSummary({coverage_comparison:{status:'previous_snapshot_unrecorded',previous_run_id:'<script>'}})",p.context);assert.match(previousMissing,/覆盖快照未记录/);assert.match(previousMissing,/&lt;script&gt;/);assert.doesNotMatch(previousMissing,/<script>/);const currentMissing=vm.runInContext("cloudScanCoverageComparisonSummary({coverage_comparison:{status:'current_snapshot_unrecorded'}})",p.context);assert.match(currentMissing,/本轮覆盖快照未记录/);const reasonsMissing=vm.runInContext("cloudScanCoverageComparisonSummary({coverage_status:'complete',coverage_observed_at_utc:'2026-10-31T11:03:00Z',coverage_comparison:{status:'comparable',reason_status:'reasons_unrecorded',previous_run_id:'run-old',previous_sequence:4,previous_coverage_status:'incomplete',previous_incomplete_directories:2,incomplete_directories_delta:-2,previous_observed_at_utc:'2026-10-30T11:03:00Z'}})",p.context);assert.match(reasonsMissing,/原因未记录/);
 await vm.runInContext("loadScanHistoryEvents('run-abc')",p.context);assert.match(calls[1],/^\/api\/cloud-inventory\/scan\/events\?run_id=run-abc/);assert.match(p.get('scan-history-events').innerHTML,/分页边界漂移，重扫目录/);assert.match(p.get('scan-history-events').innerHTML,/2026-10-31T12:00:00Z/);assert.match(p.get('scan-history-events').innerHTML,/&lt;script&gt;bad&lt;\/script&gt;/);assert.doesNotMatch(p.get('scan-history-events').innerHTML,/<script>/);
});

test('full scan history compares two selected runs across pages and exports only on click',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));const calls=[],downloads=[];let exportedBlob=null;
 p.context.Blob=Blob;p.context.URL.createObjectURL=blob=>{exportedBlob=blob;return'blob:comparison'};p.context.URL.revokeObjectURL=()=>{};p.context.document.createElement=()=>({click(){downloads.push(this.download)},remove(){}});p.context.document.body={append(){}};
 const old={sequence:3,run_id:'old-run',status:'complete',started_at_utc:'2026-10-30T10:00:00Z',coverage_status:'incomplete',coverage_incomplete_directories:7,coverage_generation:11,coverage_observed_at_utc:'2026-10-30T10:02:00Z',coverage_reasons:{scanning_directories:0,stale_directories:4,unknown_directories:0,failed_directories:0,missing_directory_status:0,orphan_directory_status:0,parent_not_complete:0,path_mismatch:0,missing_root:1}};
 const current={sequence:8,run_id:'new-run',status:'complete',started_at_utc:'2026-10-31T10:00:00Z',coverage_status:'complete',coverage_incomplete_directories:2,coverage_generation:14,coverage_observed_at_utc:'2026-10-31T10:02:00Z',coverage_reasons:{scanning_directories:0,stale_directories:1,unknown_directories:0,failed_directories:0,missing_directory_status:0,orphan_directory_status:0,parent_not_complete:0,path_mismatch:0,missing_root:0}};
 p.context.fetch=async url=>{calls.push(String(url));return{ok:true,json:async()=>String(url).includes('before_sequence')?{runs:[old,{sequence:9,run_id:'third-run',status:'complete'}],has_more:false}:{runs:[current,{sequence:4,run_id:'running-run',status:'running'}],next_before_sequence:2,has_more:true}}};
 await vm.runInContext('loadScanHistory()',p.context);assert.match(p.get('scan-history-list').innerHTML,/data-scan-history-compare-select="new-run"/);assert.doesNotMatch(p.get('scan-history-list').innerHTML,/data-scan-history-compare-select="running-run"/);
 const select=(id,checked)=>{const target={checked,dataset:{scanHistoryCompareSelect:id},closest(selector){return selector==='[data-scan-history-compare-select]'?this:null}};p.get('scan-history-list').onchange({target})};
 select('new-run',true);assert.match(p.get('scan-history-compare-selection').textContent,/1 \/ 2/);await vm.runInContext('loadScanHistory(scanHistoryNext,true)',p.context);assert.equal(vm.runInContext("scanHistorySelectedRuns.has('new-run')",p.context),true);select('old-run',true);
 await p.get('scan-history-compare').onclick();assert.match(p.get('scan-history-comparison').innerHTML,/old-run/);assert.match(p.get('scan-history-comparison').innerHTML,/new-run/);assert.match(p.get('scan-history-comparison').innerHTML,/未完整目录变化 -5/);assert.match(p.get('scan-history-comparison').innerHTML,/stale -3/);assert.match(p.get('scan-history-comparison').innerHTML,/2026-10-30T10:02:00Z/);assert.match(p.get('scan-history-comparison').innerHTML,/2026-10-31T10:02:00Z/);
 assert.equal(calls.length,2,'compare and export use snapshots already returned by history pages');await p.get('scan-history-compare-export').onclick();assert.equal(calls.length,2);assert.equal(downloads[0],'cloud-scan-comparison.json');const payload=JSON.parse(await exportedBlob.text());assert.equal(payload.earlier_run.run_id,'old-run');assert.equal(payload.later_run.run_id,'new-run');assert.equal(payload.comparison.incomplete_directories_delta,-5);assert.equal(payload.comparison.reason_deltas.stale_directories,-3);assert.equal('account_fingerprint'in payload,false);assert.equal('account_fingerprint'in payload.earlier_run,false);
 select('third-run',true);assert.match(p.get('scan-history-compare-status').textContent,/最多选择两轮/);assert.match(p.get('scan-history-compare-selection').textContent,/2 \/ 2/);p.get('scan-history-compare-clear').onclick();assert.match(p.get('scan-history-compare-selection').textContent,/0 \/ 2/);assert.equal(!!p.get('scan-history-compare').disabled,true);assert.equal(!!p.get('scan-history-compare-export').disabled,true);
 const missing=vm.runInContext("buildSelectedScanHistoryComparison([{sequence:1,run_id:'a',status:'complete',coverage_status:null},{sequence:2,run_id:'b',status:'complete',coverage_status:'complete',coverage_incomplete_directories:0,coverage_observed_at_utc:'2026-10-31T00:00:00Z'}])",p.context);assert.equal(missing.comparison.status,'coverage_snapshot_unrecorded');assert.equal(missing.comparison.incomplete_directories_delta,null);const noReasons=vm.runInContext("buildSelectedScanHistoryComparison([{sequence:1,run_id:'a',status:'complete',coverage_status:'complete',coverage_incomplete_directories:3,coverage_observed_at_utc:'2026-10-30T00:00:00Z',coverage_reasons:null},{sequence:2,run_id:'b',status:'complete',coverage_status:'complete',coverage_incomplete_directories:1,coverage_observed_at_utc:'2026-10-31T00:00:00Z',coverage_reasons:null}])",p.context);assert.equal(noReasons.comparison.status,'comparable');assert.equal(noReasons.comparison.incomplete_directories_delta,-2);assert.equal(noReasons.comparison.reason_status,'reasons_unrecorded');
});

test('full scan history exports the selected run only after an explicit click',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));const id='0123456789abcdef0123456789abcdef';let requested='',downloaded='';
 p.context.URL.createObjectURL=()=> 'blob:scan-evidence';p.context.URL.revokeObjectURL=()=>{};p.context.document.body={append:link=>{downloaded=link.download}};p.context.document.createElement=()=>({click(){},remove(){}});p.context.fetch=async url=>{requested=String(url);return{ok:true,headers:{get:()=>`attachment; filename="cloud-scan-${id}.json"`},blob:async()=>({})}};
 await vm.runInContext(`loadScanHistoryEvents('${id}')`,p.context);await vm.runInContext('exportScanHistory()',p.context);
 assert.equal(requested,`/api/cloud-inventory/scan/runs/${id}/export`);assert.equal(downloaded,`cloud-scan-${id}.json`);assert.match(p.get('scan-history-export-status').textContent,/开始下载/);assert.equal(!!p.get('scan-history-export').disabled,false);
});

test('full scan history filters UTC dates and status while retaining filters for paging',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));const calls=[];
 p.context.fetch=async url=>{calls.push(String(url));return{ok:true,json:async()=>({runs:[],next_before_sequence:17,has_more:true})}};
 p.get('scan-history-status-filter').value='failed';p.get('scan-history-from-utc').value='2026-10-01';p.get('scan-history-through-utc').value='2026-10-31';await vm.runInContext('scanHistoryFilter={status:"failed",started_from_utc:"2026-10-01",started_through_utc:"2026-10-31"};loadScanHistory(0,false)',p.context);
 assert.match(calls[0],/status=failed/);assert.match(calls[0],/started_from_utc=2026-10-01/);assert.match(calls[0],/started_through_utc=2026-10-31/);
 await vm.runInContext('loadScanHistory(scanHistoryNext,true)',p.context);assert.match(calls[1],/before_sequence=17/);assert.match(calls[1],/status=failed/);assert.match(calls[1],/started_from_utc=2026-10-01/);
});

test('full scan history filters coverage and retains or clears all filters',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));const calls=[];
 p.context.fetch=async url=>{calls.push(String(url));return{ok:true,json:async()=>({runs:[],next_before_sequence:17,has_more:calls.length===1})}};
 p.get('scan-history-coverage-status').value='unrecorded';p.get('scan-history-incomplete-min').value='0';p.get('scan-history-incomplete-max').value='12';p.get('scan-history-coverage-reason').value='unrecorded';
 p.get('scan-history-filter').onsubmit({preventDefault(){}});await new Promise(resolve=>setImmediate(resolve));assert.match(calls[0],/coverage_status=unrecorded/);assert.match(calls[0],/incomplete_directories_min=0/);assert.match(calls[0],/incomplete_directories_max=12/);assert.match(calls[0],/coverage_reason=unrecorded/);
 p.get('scan-history-older').onclick();await new Promise(resolve=>setImmediate(resolve));assert.match(calls[1],/before_sequence=17/);assert.match(calls[1],/coverage_status=unrecorded/);assert.match(calls[1],/coverage_reason=unrecorded/);
 p.get('scan-history-clear').onclick();await new Promise(resolve=>setImmediate(resolve));assert.doesNotMatch(calls[2],/coverage_status|incomplete_directories_|coverage_reason|before_sequence/);assert.equal(p.get('scan-history-coverage-status').value,'');assert.equal(p.get('scan-history-incomplete-min').value,'');assert.equal(p.get('scan-history-incomplete-max').value,'');assert.equal(p.get('scan-history-coverage-reason').value,'');
});

test('standalone remote operation history loads the local first page on entry',async()=>{
 const p=page('index_operations.html');const calls=[];let start;
 p.context.fetch=async url=>{calls.push(String(url));return{ok:true,json:async()=>({operations:[],total:0,limit:20,next_cursor:null})};};
 p.context.setTimeout=callback=>{start=callback;return 1;};
 vm.runInContext(fs.readFileSync(path.join(root,'res','cloud_operations.js'),'utf8'),p.context);
 start();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls.length,1);assert.match(calls[0],/^\/api\/cloud\/operations\?/);
});

test('remote operation history cursor pages keep their boundary when a new operation is inserted',async()=>{
 const p=page('index_operations.html');const calls=[];let pageNumber=0;
 const operation=(letter,at)=>({operation_id:letter.repeat(32),kind:'move',object_id:1,status:'confirmed',created_at_utc:at,source_path:'/'+letter,destination_path:'/dest'});
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');calls.push(parsed);
  if(parsed.pathname.endsWith('/export'))throw new Error('unexpected export');
  pageNumber++;
  if(pageNumber===1)return{ok:true,json:async()=>({operations:[operation('c','2026-10-30T00:00:00Z'),operation('b','2026-10-29T00:00:00Z')],total:3,limit:2,next_cursor:{created_at_utc:'2026-10-29T00:00:00Z',operation_id:'b'.repeat(32)}})};
  if(pageNumber===2)return{ok:true,json:async()=>({operations:[operation('a','2026-10-28T00:00:00Z')],total:4,limit:2,next_cursor:null})};
  if(pageNumber===3)return{ok:true,json:async()=>({operations:[operation('d','2026-10-31T00:00:00Z'),operation('c','2026-10-30T00:00:00Z')],total:4,limit:2,next_cursor:{created_at_utc:'2026-10-30T00:00:00Z',operation_id:'c'.repeat(32)}})};
  return{ok:true,json:async()=>({operations:[],total:0,limit:2,next_cursor:null})};
 };
 const details=p.get('remote-operation-details');details.open=true;p.listeners.get('remote-operation-details:toggle')({currentTarget:details});
 await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls[0].searchParams.has('offset'),false);assert.equal(calls[0].searchParams.get('limit'),'20');
 p.get('remote-operation-next').onclick();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls[1].searchParams.get('before_created_at_utc'),'2026-10-29T00:00:00Z');assert.equal(calls[1].searchParams.get('before_operation_id'),'b'.repeat(32));
 assert.ok(p.get('remote-operation-list').innerHTML.includes('源路径：/a</small>'));assert.ok(!p.get('remote-operation-list').innerHTML.includes('源路径：/b</small>'));
 assert.match(p.get('remote-operation-summary').textContent,/实时匹配总数/);
 p.get('remote-operation-prev').onclick();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls[2].searchParams.has('before_operation_id'),false);assert.ok(p.get('remote-operation-list').innerHTML.includes('源路径：/d</small>'));
 p.get('remote-operation-query').value='new filter';p.get('remote-operation-search').onclick();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls[3].searchParams.has('before_operation_id'),false);assert.equal(calls[3].searchParams.get('q'),'new filter');
});

test('WebSocket page separates connection status and account readiness and escapes cloud text',async()=>{
    const p = page('index_ws.html');
    p.listeners.get('DOMContentLoaded')();
    p.sockets[0].onopen();
    await new Promise(resolve=>setImmediate(resolve));
    assert.equal(p.get('token-status-text').textContent,'尚未授权');
    p.context.payload = {tasks:[{status:'failed',path:'<img src=x onerror=alert(1)>',local_path:'<svg onload=alert(2)>',error:'<script>alert(3)</script>',size:3,progress:0}]};
    vm.runInContext('renderTasks(payload)',p.context);
    const html = p.get('tasks-list').innerHTML;
    assert.ok(html.includes('&lt;img'));
    assert.ok(html.includes('&lt;svg'));
    assert.ok(html.includes('&lt;script&gt;'));
    assert.ok(!html.includes('<img') && !html.includes('<script>'));
});
test('poll page escapes cloud text',async()=>{
    const p=page('index_poll.html',[{status:'failed',path:'<img src=x>',error:'<svg>',size:1}]);
    await new Promise(resolve=>setImmediate(resolve));
    assert.ok(p.get('tasks-list').innerHTML.includes('&lt;img'));
    assert.ok(!p.get('tasks-list').innerHTML.includes('<svg>'));
});
test('poll page shows local storage guidance safely and keeps manual retry available',async()=>{
    const message='本地可用空间不足，释放空间后手动重试 <img src=x>';
    const p=page('index_poll.html',[{status:'failed',path:'payload.bin',error:message,size:1,task_id:'505',fs_id:505,is_root:true,can_retry:true,can_cancel:false}]);
    await new Promise(resolve=>setImmediate(resolve));
    const html=p.get('tasks-list').innerHTML;
    assert.ok(html.includes('本地可用空间不足，释放空间后手动重试'));
    assert.ok(html.includes('&lt;img src=x&gt;'));
    assert.ok(!html.includes('<img src=x>'));
    assert.ok(html.includes('data-action="retry"'));
});

test('WebSocket task filters reset paging, persist across pages and ignore stale responses',()=>{
 const p=page('index_ws.html');p.listeners.get('DOMContentLoaded')();p.sockets[0].readyState=1;p.sockets[0].onopen();
 vm.runInContext('taskOffset=200',p.context);
 p.get('task-status-filter').value='failed';p.get('task-search-query').value='  AI量化  ';
 p.listeners.get('task-search-button:click')();
 let request=JSON.parse(p.sockets[0].sent.at(-1));
 assert.equal(request.type,'status_page');assert.equal(request.offset,0);assert.equal(request.status,'failed');assert.equal(request.q,'AI量化');
 vm.runInContext('updateTaskPageControls({offset:0,limit:100,has_more:true,next_offset:100})',p.context);
 vm.runInContext('changeTaskPage(1)',p.context);
 request=JSON.parse(p.sockets[0].sent.at(-1));assert.equal(request.offset,100);assert.equal(request.status,'failed');assert.equal(request.q,'AI量化');
 p.get('task-search-query').value='new';p.listeners.get('task-search-button:click')();
 const latest=JSON.parse(p.sockets[0].sent.at(-1));
 p.context.message={tasks:[{status:'failed',path:'stale'}],offset:100,limit:100,total_tasks:1,request_id:latest.request_id-1};
 vm.runInContext('renderTasks(message)',p.context);assert.ok(!p.get('tasks-list').innerHTML.includes('stale'));
 p.listeners.get('task-search-clear:click')();request=JSON.parse(p.sockets[0].sent.at(-1));assert.equal(request.status,'');assert.equal(request.q,'');assert.equal(request.offset,0);
 const sentCount=p.sockets[0].sent.length;p.get('task-search-query').value='界'.repeat(129);vm.runInContext('applyTaskFilters()',p.context);
 assert.equal(p.sockets[0].sent.length,sentCount,'an overlong Unicode query was sent');assert.match(p.get('task-feedback').textContent,/128 个字符/);
});

test('poll task filters persist across pagination and refresh and ignore stale responses',async()=>{
 const p=page('index_poll.html');await new Promise(resolve=>setImmediate(resolve));const calls=[];let releaseStale;
 p.context.fetch=url=>{
  const value=String(url);calls.push(value);
  if(value.includes('q=old')&&value.includes('offset=100'))return new Promise(resolve=>{releaseStale=()=>resolve({ok:true,json:async()=>({tasks:[{status:'failed',path:'old result'}],offset:100,limit:100,total_tasks:1,queue_total_tasks:5,has_more:false})})});
  return Promise.resolve({ok:true,json:async()=>({tasks:[],offset:value.includes('offset=100')?100:0,limit:100,total_tasks:0,queue_total_tasks:5,has_more:value.includes('q=old'),next_offset:100})});
 };
 p.get('task-status-filter').value='failed';p.get('task-search-query').value=' old ';
 vm.runInContext('applyTaskFilters()',p.context);await new Promise(resolve=>setImmediate(resolve));
 assert.ok(calls.at(-1).includes('status=failed'));assert.ok(calls.at(-1).includes('q=old'));assert.ok(calls.at(-1).includes('offset=0'));
 vm.runInContext('changeTaskPage(1)',p.context);await new Promise(resolve=>setImmediate(resolve));
 assert.ok(calls.at(-1).includes('offset=100'));assert.ok(calls.at(-1).includes('status=failed'));assert.ok(calls.at(-1).includes('q=old'));
 const stale=vm.runInContext('fetchStatus()',p.context);
 p.get('task-search-query').value='new';vm.runInContext('applyTaskFilters()',p.context);
 await new Promise(resolve=>setImmediate(resolve));releaseStale();await stale;
 assert.ok(!p.get('tasks-list').innerHTML.includes('old result'));
 await vm.runInContext('fetchStatus()',p.context);assert.ok(calls.at(-1).includes('q=new'),'periodic refresh did not preserve the active query');
 vm.runInContext('clearTaskFilters()',p.context);await new Promise(resolve=>setImmediate(resolve));
 assert.ok(calls.at(-1).includes('offset=0'));assert.ok(!calls.at(-1).includes('status=')&&!calls.at(-1).includes('q='));
 assert.match(p.get('tasks-page').textContent,/全队列 5 项/);
 const callCount=calls.length;p.get('task-search-query').value='界'.repeat(129);vm.runInContext('applyTaskFilters()',p.context);
 assert.equal(calls.length,callCount,'an overlong Unicode query was sent');assert.match(p.get('task-feedback').textContent,/128 个字符/);
});

test('task event timeline loads on demand, pages older evidence and escapes payload text',async()=>{
 const p=page('index_ws.html');const calls=[];
 p.context.task={task_id:'123',is_root:true};
 p.context.fetch=async url=>{
  calls.push(String(url));
  if(String(url).includes('before_sequence=2'))return{ok:true,json:async()=>({events:[{sequence:1,kind:'job_registered',recorded_at_utc:'2026-10-28T01:00:00Z',evidence_type:'local_state',evidence:{note:'older'}}],next_before_sequence:null})};
  return{ok:true,json:async()=>({events:[{sequence:2,kind:'task_cancelled',recorded_at_utc:'2026-10-28T02:00:00Z',cloud_observed_at_utc:'2026-10-28T02:00:01Z',response_observed_at_utc:null,evidence_type:'local_user_action',evidence:{note:'<img src=x onerror=alert(1)>'}}],next_before_sequence:2})};
 };
 assert.equal(calls.length,0,'events must not load before expansion');
 await vm.runInContext("toggleTaskEvents('123')",p.context);
 let html=vm.runInContext('taskEventsControl(task)',p.context);
 assert.equal(calls.length,1);assert.ok(html.includes('本机记录时间'));assert.ok(html.includes('task_cancelled'));
 assert.ok(html.includes('&lt;img'));assert.ok(!html.includes('<img'));
 await vm.runInContext("loadOlderTaskEvents('123')",p.context);
 html=vm.runInContext('taskEventsControl(task)',p.context);
 assert.equal(calls.length,2);assert.ok(calls[1].includes('before_sequence=2'));
 assert.ok(html.includes('#1 · 任务登记'));assert.ok(!html.includes('#2 ·'),'older page should replace the current page');
 assert.equal(vm.runInContext("taskEventPanels.get('123').events.length",p.context),1,'only the current page should remain in memory');
 const olderEvent=html.slice(html.indexOf('任务登记'),html.indexOf('</article>',html.indexOf('任务登记')));
 assert.ok(!olderEvent.includes('云端扫描观察时间'),'missing cloud observation time must stay absent');
 assert.ok(html.includes('返回较新事件'));
 await vm.runInContext("loadNewerTaskEvents('123')",p.context);
 html=vm.runInContext('taskEventsControl(task)',p.context);
 assert.equal(calls.length,3);assert.ok(!calls[2].includes('before_sequence='));
 assert.ok(html.includes('#2 · 取消任务'));assert.ok(!html.includes('#1 · 任务登记'));
 assert.equal(vm.runInContext("taskEventPanels.get('123').events.length",p.context),1,'newer navigation must also retain only one page');
 await vm.runInContext("toggleTaskEvents('456')",p.context);
 assert.equal(vm.runInContext('taskEventPanels.size',p.context),1,'only one task timeline should stay expanded in memory');
 assert.equal(vm.runInContext("taskEventPanels.has('123')",p.context),false);
});

test('task event filters count matches, survive paging and refresh, and reset to the newest page',async()=>{
 const p=page('index_ws.html');const calls=[];
 p.context.fetch=async url=>{
  const value=String(url);calls.push(value);const query=new URL(value,'http://local').searchParams;
  return{ok:true,json:async()=>({total_matches:3,events:[{sequence:query.has('before_sequence')?2:5,kind:query.get('kind')||'download_attempt_started',recorded_at_utc:'2026-11-02T01:00:00Z',evidence_type:'test',evidence:{}}],next_before_sequence:query.has('before_sequence')?null:3})};
 };
 vm.runInContext("taskEventState('123').expanded=true",p.context);
 await vm.runInContext("applyTaskEventFilters('123','download_attempt_finished','2026-11-01','2026-11-02')",p.context);
 let html=vm.runInContext("taskEventsControl({task_id:'123',is_root:true})",p.context);
 assert.match(html,/筛选命中 3 条/);assert.match(html,/name="from_utc"/);assert.match(html,/name="through_utc"/);
 assert.ok(calls[0].includes('kind=download_attempt_finished')&&calls[0].includes('from_utc=2026-11-01')&&calls[0].includes('through_utc=2026-11-02'));
 await vm.runInContext("loadOlderTaskEvents('123')",p.context);
 assert.ok(calls[1].includes('before_sequence=3')&&calls[1].includes('kind=download_attempt_finished')&&calls[1].includes('from_utc=2026-11-01'));
 await vm.runInContext("refreshTaskEvents('123')",p.context);
 assert.ok(!calls[2].includes('before_sequence=')&&calls[2].includes('kind=download_attempt_finished')&&calls[2].includes('through_utc=2026-11-02'));
 await vm.runInContext("clearTaskEventFilters('123')",p.context);
 assert.ok(!calls[3].includes('kind=')&&!calls[3].includes('from_utc=')&&!calls[3].includes('through_utc='));
 assert.equal(vm.runInContext("taskEventPanels.get('123').beforeSequence",p.context),null);
 const callCount=calls.length;await vm.runInContext("applyTaskEventFilters('123','','2026-11-03','2026-11-02')",p.context);
 assert.equal(calls.length,callCount,'reversed UTC date ranges must be rejected before reading');
 html=vm.runInContext("taskEventsControl({task_id:'123',is_root:true})",p.context);assert.match(html,/UTC 起始日不能晚于截止日/);
});

test('task event correlation IDs focus from cards and persist through paging refresh and export',async()=>{
 const p=page('index_ws.html');const calls=[];let clicked=null;
 const event={sequence:8,kind:'source_operation_sending',recorded_at_utc:'2026-11-02T01:00:00Z',operation_id:'op-<>&"',attempt_id:'attempt-<>' ,evidence_type:'test',evidence:{}};
 p.context.fetch=async url=>{
  const value=String(url);calls.push(value);
  if(value.includes('/export'))return{ok:true,headers:{get:()=> 'attachment; filename="task-events-123.json"'},blob:async()=>({size:10})};
  const query=new URL(value,'http://local').searchParams;
  return{ok:true,json:async()=>({total_matches:2,events:[event],next_before_sequence:query.has('before_sequence')?null:4})};
 };
 vm.runInContext("taskEventState('123').expanded=true",p.context);
 await vm.runInContext("applyTaskEventFilters('123','source_operation_sending','2026-11-01','2026-11-02')",p.context);
 let html=vm.runInContext("taskEventsControl({task_id:'123',is_root:true})",p.context);
 assert.match(html,/线上操作 ID/);assert.match(html,/尝试 ID/);assert.match(html,/data-correlation-kind="operation_id"/);assert.match(html,/data-correlation-kind="attempt_id"/);
 assert.match(html,/op-&lt;&gt;&amp;&quot;/);assert.doesNotMatch(html,/data-correlation-value="op-<>&"/);
 vm.runInContext('bindTaskControls(async()=>{})',p.context);
 const clickCorrelation=async(kind,value)=>p.listeners.get('tasks-list:click')({target:{closest:selector=>selector==='button[data-task-events]'?({dataset:{taskEvents:'correlate',eventTaskId:'123',correlationKind:kind,correlationValue:value}}):null}});
 await clickCorrelation('operation_id',event.operation_id);
 let query=new URL(calls.at(-1),'http://local').searchParams;
 assert.equal(query.get('operation_id'),event.operation_id);assert.equal(query.get('attempt_id'),null);assert.equal(query.get('kind'),'source_operation_sending');assert.equal(query.get('from_utc'),'2026-11-01');assert.equal(query.get('through_utc'),'2026-11-02');assert.equal(query.has('before_sequence'),false);
 await clickCorrelation('attempt_id',event.attempt_id);
 query=new URL(calls.at(-1),'http://local').searchParams;
 assert.equal(query.get('operation_id'),null);assert.equal(query.get('attempt_id'),event.attempt_id);assert.equal(query.get('kind'),'source_operation_sending');
 await vm.runInContext("loadOlderTaskEvents('123')",p.context);
 assert.equal(new URL(calls.at(-1),'http://local').searchParams.get('attempt_id'),event.attempt_id);
 await vm.runInContext("loadNewerTaskEvents('123')",p.context);
 assert.equal(new URL(calls.at(-1),'http://local').searchParams.get('attempt_id'),event.attempt_id);
 await vm.runInContext("refreshTaskEvents('123')",p.context);
 assert.equal(new URL(calls.at(-1),'http://local').searchParams.get('attempt_id'),event.attempt_id);
 p.context.URL.createObjectURL=()=> 'blob:events';p.context.URL.revokeObjectURL=()=>{};p.context.document.createElement=()=>({click(){clicked=this},remove(){}});p.context.document.body={append(){}};
 await vm.runInContext("exportTaskEventEvidence('123')",p.context);
 assert.equal(new URL(calls.at(-1),'http://local').searchParams.get('attempt_id'),event.attempt_id);assert.equal(clicked.download,'task-events-123.json');
 await vm.runInContext("clearTaskEventFilters('123')",p.context);
 query=new URL(calls.at(-1),'http://local').searchParams;
 assert.equal(query.has('operation_id'),false);assert.equal(query.has('attempt_id'),false);assert.equal(query.has('kind'),false);assert.equal(query.has('from_utc'),false);
 const fields={kind:{value:'download_attempt_started'},from_utc:{value:'2026-11-01'},through_utc:{value:'2026-11-02'},operation_id:{value:'manual-op'},attempt_id:{value:'manual-attempt'}};
 await p.listeners.get('tasks-list:submit')({target:{closest:selector=>selector==='form[data-task-event-filter]'?{dataset:{eventTaskId:'123'},querySelector:selector=>fields[selector.match(/name="([^"]+)"/)[1]]}:null},preventDefault(){}});
 query=new URL(calls.at(-1),'http://local').searchParams;
 assert.equal(query.get('operation_id'),'manual-op');assert.equal(query.get('attempt_id'),'manual-attempt');assert.equal(query.get('kind'),'download_attempt_started');assert.equal(query.get('from_utc'),'2026-11-01');assert.equal(query.get('through_utc'),'2026-11-02');
});

test('task event workbench keeps exact UTC time filters on pages and exports',async()=>{
 const p=page('index_ws.html');const calls=[];let clicked=null;
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value.includes('/export'))return{ok:true,headers:{get:()=> 'attachment; filename="events.json"'},blob:async()=>({})};return{ok:true,json:async()=>({events:[{sequence:7,kind:'download_attempt_started',recorded_at_utc:'2026-11-02T10:00:00Z',evidence_type:'test',evidence:{}}],total_matches:1,kind_counts:[{kind:'download_attempt_started',count:1}],next_before_sequence:4})}};
 vm.runInContext("taskEventState('123').expanded=true",p.context);
 await vm.runInContext("applyTaskEventFilters('123','download_attempt_started','','','','','2026-11-02T10:00:00','2026-11-02T10:00:01')",p.context);
 let html=vm.runInContext("taskEventsControl({task_id:'123',is_root:true})",p.context);
 assert.match(html,/UTC 精确起点/);assert.match(html,/UTC 精确截止/);assert.match(html,/按类型统计/);
 assert.equal(new URL(calls[0],'http://local').searchParams.get('recorded_from_utc'),'2026-11-02T10:00:00Z');
 await vm.runInContext("loadOlderTaskEvents('123')",p.context);
 assert.equal(new URL(calls[1],'http://local').searchParams.get('recorded_before_utc'),'2026-11-02T10:00:01Z');
 await vm.runInContext("refreshTaskEvents('123')",p.context);
 assert.equal(new URL(calls[2],'http://local').searchParams.get('recorded_from_utc'),'2026-11-02T10:00:00Z');
 p.context.document.createElement=()=>({click(){clicked=this}});p.context.URL.createObjectURL=()=> 'blob:exact';p.context.URL.revokeObjectURL=()=>{};
 await vm.runInContext("exportTaskEventEvidence('123')",p.context);
 assert.equal(new URL(calls[3],'http://local').searchParams.get('recorded_before_utc'),'2026-11-02T10:00:01Z');assert.equal(clicked.download,'events.json');
 const count=calls.length;
 await vm.runInContext("applyTaskEventFilters('123','','2026-11-02','','','','2026-11-02T10:00:00','')",p.context);
 assert.equal(calls.length,count,'date and exact UTC filters must not be sent together');assert.match(vm.runInContext("taskEventsControl({task_id:'123',is_root:true})",p.context),/不能同时使用/);
});

test('task event type totals, event-day shortcut, context window, and redacted clipboard work together',async()=>{
 const p=page('index_ws.html');const calls=[];const copied=[];
 p.context.navigator={clipboard:{writeText:async value=>copied.push(value)}};
 const event={sequence:10,kind:'source_operation_confirmed',recorded_at_utc:'2026-11-02T23:30:00Z',operation_id:'operation-a',attempt_id:'attempt-a',evidence_type:'test',evidence:{access_token:'[REDACTED]'}};
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value.endsWith('/context'))return{ok:true,json:async()=>({before:[{sequence:9,kind:'source_operation_sending',recorded_at_utc:'2026-11-02T23:29:00Z'}],anchor:event,after:[]})};const query=new URL(value,'http://local').searchParams;return{ok:true,json:async()=>({events:[event],total_matches:1,kind_counts:[{kind:'source_operation_confirmed',count:1},{kind:'download_attempt_started',count:0}],next_before_sequence:null})}};
 vm.runInContext("taskEventState('123').expanded=true",p.context);
 await vm.runInContext("applyTaskEventFilters('123','','','','operation-a','attempt-a','2026-11-02T10:00:00','')",p.context);
 await vm.runInContext("applyTaskEventKindCount('123','source_operation_confirmed')",p.context);
 let query=new URL(calls.at(-1),'http://local').searchParams;assert.equal(query.get('kind'),'source_operation_confirmed');assert.equal(query.get('operation_id'),'operation-a');assert.equal(query.get('recorded_from_utc'),'2026-11-02T10:00:00Z');
 await vm.runInContext("applyTaskEventDayFilter('123','2026-11-02T23:30:00Z')",p.context);
 query=new URL(calls.at(-1),'http://local').searchParams;assert.equal(query.get('from_utc'),'2026-11-02');assert.equal(query.get('through_utc'),'2026-11-02');assert.equal(query.get('operation_id'),'operation-a');assert.equal(query.has('recorded_from_utc'),false);
 await vm.runInContext("loadTaskEventContext('123',10)",p.context);assert.ok(calls.at(-1).endsWith('/api/tasks/123/events/10/context'));
 let html=vm.runInContext("taskEventsControl({task_id:'123',is_root:true})",p.context);assert.match(html,/#9 · 准备处理网盘源文件/);assert.match(html,/前后事件/);
 await vm.runInContext("copyTaskEventJSON('123',10)",p.context);assert.equal(copied.length,1);assert.match(copied[0],/\[REDACTED\]/);assert.doesNotMatch(copied[0],/operation-a.*secret/);
 p.context.navigator.clipboard.writeText=async()=>{throw new Error('denied')};
 await vm.runInContext("copyTaskEventJSON('123',10)",p.context);html=vm.runInContext("taskEventsControl({task_id:'123',is_root:true})",p.context);assert.match(html,/浏览器未允许剪贴板访问/);
});

test('a late task event context response cannot replace a newer read',async()=>{
 const p=page('index_ws.html');const pending=[];let releaseOldJSON;
 p.context.fetch=url=>new Promise(resolve=>pending.push({url:String(url),resolve}));
 const old=vm.runInContext("loadTaskEventContext('123',10)",p.context);
 pending[0].resolve({ok:true,json:()=>new Promise(resolve=>{releaseOldJSON=resolve})});await new Promise(resolve=>setImmediate(resolve));
 const latest=vm.runInContext("loadTaskEventContext('123',10)",p.context);
 pending[1].resolve({ok:true,json:async()=>({before:[],anchor:{sequence:10,kind:'task_paused'},after:[]})});await latest;
 releaseOldJSON({before:[],anchor:{sequence:10,kind:'task_retry_requested'},after:[]});await old;
 assert.equal(vm.runInContext("taskEventState('123').contexts['10'].data.anchor.kind",p.context),'task_paused');
});

test('task event filter changes ignore late responses and filtered export waits for an explicit click',async()=>{
 const p=page('index_ws.html');const calls=[];const pending=[];const downloads=[];
 p.context.downloads=downloads;p.context.document.createElement=()=>({click(){downloads.push(this.download)}});p.context.URL.createObjectURL=()=> 'blob:filtered-events';p.context.URL.revokeObjectURL=()=>{};
 p.context.fetch=url=>{calls.push(String(url));return new Promise(resolve=>pending.push(resolve))};
 vm.runInContext("taskEventState('123').expanded=true",p.context);
 const older=vm.runInContext("applyTaskEventFilters('123','download_attempt_started','','')",p.context);
 const latest=vm.runInContext("applyTaskEventFilters('123','task_retry_requested','','')",p.context);
 assert.equal(calls.length,2);assert.ok(!calls[0].includes('kind=task_retry_requested'));assert.ok(calls[1].includes('kind=task_retry_requested'));
 pending[1]({ok:true,json:async()=>({total_matches:1,events:[{sequence:8,kind:'task_retry_requested',recorded_at_utc:'2026-11-02T01:00:00Z',evidence_type:'test',evidence:{}}],next_before_sequence:null})});
 await latest;
 pending[0]({ok:true,json:async()=>({total_matches:1,events:[{sequence:7,kind:'download_attempt_started',recorded_at_utc:'2026-11-02T00:00:00Z',evidence_type:'test',evidence:{}}],next_before_sequence:null})});
 await older;
 assert.equal(vm.runInContext("taskEventPanels.get('123').events[0].kind",p.context),'task_retry_requested','a late response replaced results for the current filter');
 const exportPromise=vm.runInContext("exportTaskEventEvidence('123')",p.context);
 assert.equal(calls.length,3);assert.ok(calls[2].includes('/events/export?kind=task_retry_requested'));
 pending[2]({ok:true,headers:{get:()=> 'attachment; filename="task-events-123.json"'},blob:async()=>({size:10})});await exportPromise;
 assert.deepEqual(downloads,['task-events-123.json']);
});

test('task event evidence export starts only on explicit click and downloads a file once',async()=>{
 const p=page('index_ws.html');const calls=[];const downloads=[];let resolveFetch;
 p.context.downloads=downloads;
 p.context.document.createElement=()=>({click(){downloads.push({download:this.download,href:this.href})}});
 p.context.URL.createObjectURL=()=> 'blob:task-evidence';p.context.URL.revokeObjectURL=()=>{};
 p.context.fetch=url=>{calls.push(String(url));return new Promise(resolve=>{resolveFetch=resolve})};
 vm.runInContext("taskEventState('123').expanded=true",p.context);
 assert.match(vm.runInContext("taskEventsControl({task_id:'123',is_root:true})",p.context),/导出当前筛选证据/);
 const first=vm.runInContext("exportTaskEventEvidence('123')",p.context);
 const second=vm.runInContext("exportTaskEventEvidence('123')",p.context);
 await second;assert.equal(calls.length,1,'concurrent clicks must share one request');
 resolveFetch({ok:true,headers:{get:()=> 'attachment; filename="task-events-123.json"'},blob:async()=>({size:20})});
 await first;
 assert.equal(calls[0],'/api/tasks/123/events/export');
 assert.deepEqual(downloads,[{download:'task-events-123.json',href:'blob:task-evidence'}]);
});

test('task event evidence export reports auth expiry and all-or-nothing limit safely',async()=>{
 for(const [status,message] of [[401,'管理员登录已失效，请重新登录。'],[413,'超过 100 条']]){
  const p=page('index_ws.html');const downloads=[];let calls=0;
  p.context.document.createElement=()=>({click(){downloads.push(this.download)}});
  p.context.URL.createObjectURL=()=> 'blob:task-evidence';p.context.URL.revokeObjectURL=()=>{};
  p.context.fetch=async()=>{calls++;return{ok:false,status,headers:{get:()=>null},text:async()=> 'private database error'}};
  vm.runInContext("taskEventState('123').expanded=true",p.context);
  await vm.runInContext("exportTaskEventEvidence('123')",p.context);
  const html=vm.runInContext("taskEventsControl({task_id:'123',is_root:true})",p.context);
  assert.ok(html.includes(message));assert.ok(!html.includes('private database error'));assert.equal(calls,1);assert.deepEqual(downloads,[]);
 }
});

test('both task list renderers expose event inspection only for root tasks',async()=>{
 const task={task_id:'123',is_root:true,status:'done',path:'/queue/a',server_filename:'a',size:1};
 const ws=page('index_ws.html');ws.context.payload={tasks:[task]};vm.runInContext('renderTasks(payload)',ws.context);
 assert.ok(ws.get('tasks-list').innerHTML.includes('data-task-events="toggle"'));
 const poll=page('index_poll.html',[task]);await new Promise(resolve=>setImmediate(resolve));
 assert.ok(poll.get('tasks-list').innerHTML.includes('data-task-events="toggle"'));
 const child=page('index_ws.html');child.context.task={task_id:'124',is_root:false};
 assert.equal(vm.runInContext('taskEventsControl(task)',child.context),'');
});

test('poll status page requests the next server page without loading full history',async()=>{
 const p=page('index_poll.html');const requests=[];
 p.context.fetch=async url=>{
  requests.push(url);
  const offset=Number(new URL(url,'http://localhost').searchParams.get('offset'));
  return{ok:true,json:async()=>({tasks:[],total_tasks:201,status_counts:{done:201},offset,limit:100,has_more:offset<200,next_offset:offset<200?offset+100:null})};
 };
 await vm.runInContext('fetchStatus()',p.context);
 p.listeners.get('tasks-next:click')();
 await new Promise(resolve=>setImmediate(resolve));
 assert.ok(requests.some(url=>url.includes('offset=100')));
 assert.equal(p.get('tasks-next').disabled,false);
});

test('WebSocket status pages request server sorting and update global statistics',()=>{
 const p=page('index_ws.html');
 p.listeners.get('DOMContentLoaded')();
 p.context.payload={tasks:[],total_tasks:250,status_counts:{downloading:2,paused:3,done:237,failed:8},offset:0,limit:100,has_more:true,next_offset:100};
 vm.runInContext('renderTasks(payload)',p.context);
 assert.equal(p.get('total-tasks').textContent,250);
 assert.equal(p.get('paused-tasks').textContent,3);
 assert.equal(p.get('done-tasks').textContent,237);
 p.sockets[0].readyState=1;
 p.listeners.get('tasks-next:click')();
 assert.equal(JSON.parse(p.sockets[0].sent.at(-1)).offset,100);
});
test('OAuth page only accepts HTTPS Baidu authorization links',()=>{
    const p=page('index_oauth.html');
    for(const url of ['javascript:alert(1)','http://openapi.baidu.com/device','https://baidu.com.evil.example/','https://user:password@baidu.com/']){
        p.context.inputURL=url;
        assert.equal(vm.runInContext('safeBaiduURL(inputURL)',p.context),'');
    }
    assert.equal(vm.runInContext("safeBaiduURL('https://openapi.baidu.com/device')",p.context),'https://openapi.baidu.com/device');
});

test('OAuth page tells new users where to create a Baidu Netdisk app and configure its keys',()=>{
    const html=fs.readFileSync(path.join(root,'index_oauth.html'),'utf8');
    assert.match(html,/https:\/\/yun\.baidu\.com\/union/);
    assert.match(html,/AppID/);
    assert.match(html,/AppKey/);
    assert.match(html,/SecretKey/);
    assert.match(html,/SignKey/);
    assert.match(html,/设备码 OAuth/);
    assert.match(html,/不需要 OAuth 授权回调地址/);
    assert.match(html,/百度网盘应用/);
    assert.match(html,/请勿发给他人/);
});

test('root task actions use exact string IDs and server capabilities',async()=>{
    const p=page('index_ws.html');
    p.context.task={task_id:'3400000000000000123',fs_id:3400000000000000123,is_root:true,can_retry:true,can_cancel:false,download_mode:'delete',attempts:2,next_attempt:'2026-10-21T10:00:00Z'};
    const html=vm.runInContext('taskControls(task)',p.context);
    assert.ok(html.includes('3400000000000000123'));
    assert.ok(html.includes('data-action="retry"'));
    assert.ok(!html.includes('data-action="cancel"'));
    assert.ok(html.includes('删除'));
    p.context.task.is_root=false;
    assert.equal(vm.runInContext('taskControls(task)',p.context),'');
    p.context.task.is_root=true; delete p.context.task.task_id;
    assert.ok(!vm.runInContext('taskControls(task)',p.context).includes('data-action='), 'numeric fs_id must not become an action URL');
    const calls=[];
    p.context.fetch=async(url,options)=>{calls.push([url,options.method]);return {ok:true}};
    await vm.runInContext("performTaskAction('3400000000000000123','retry',async()=>{})",p.context);
    assert.deepEqual(calls,[['/api/tasks/3400000000000000123/retry','POST']]);
});

test('task actions prevent double submission and report stale state',async()=>{
    const p=page('index_poll.html');
    let release;
    p.context.fetch=()=>new Promise(resolve=>{release=resolve});
    const first=vm.runInContext("performTaskAction('123','cancel',async()=>{})",p.context);
    let calls=0;
    const previous=p.context.fetch;
    p.context.fetch=(...args)=>{calls++;return previous(...args)};
    await vm.runInContext("performTaskAction('123','cancel',async()=>{})",p.context);
    assert.equal(calls,0);
    release({ok:false,status:409});await first;
    assert.ok(p.get('task-feedback').textContent.includes('状态'));
});

test('intake UI renders unknown directories without a manual release action and escapes paths',()=>{
 const p=page('index_intake.html');
 p.context.record={id:'a'.repeat(32),state:'finished',queue_dir:'/queue',download_mode:'delete',items:[{source:{path:'<img src=x>',isdir:1},target:'/queue/<svg>',state:'unknown',error:'directory_completion_unconfirmed'}]};
 const html=vm.runInContext('intakeTable(record)',p.context);
 assert.ok(html.includes('&lt;img'));
 assert.ok(!html.includes('<svg>'));
 assert.ok(html.includes('隔离'));
 assert.ok(!html.includes('data-release') && !html.includes('人工确认按钮'));
});

test('intake record navigation uses stable cursors and resets on filter changes',async()=>{
 const p=page('index_intake.html');const requests=[];
 p.context.fetch=async url=>{
  const value=String(url);
  if(value.startsWith('/api/intakes?')){
   const parsed=new URL(value,'http://localhost');requests.push(parsed);
   const cursor=parsed.searchParams.get('cursor');
   return{ok:true,json:async()=>({records:[],offset:0,total:40,next_cursor:cursor==='c1'?'c2':'c1'})};
  }
  return{ok:true,json:async()=>({})};
 };
 p.listeners.get('DOMContentLoaded')();await new Promise(resolve=>setImmediate(resolve));
 p.listeners.get('records-next:click')();await new Promise(resolve=>setImmediate(resolve));
 p.listeners.get('records-next:click')();await new Promise(resolve=>setImmediate(resolve));
 p.listeners.get('records-prev:click')();await new Promise(resolve=>setImmediate(resolve));
 assert.deepEqual(requests.slice(0,4).map(request=>request.searchParams.get('cursor')),[null,'c1','c2','c1']);
 p.get('intake-unresolved').checked=true;p.listeners.get('intake-unresolved:change')();await new Promise(resolve=>setImmediate(resolve));
 const filtered=requests.at(-1);assert.equal(filtered.searchParams.get('cursor'),null);assert.equal(filtered.searchParams.get('unresolved'),'1');
 p.listeners.get('records-next:click')();await new Promise(resolve=>setImmediate(resolve));assert.equal(requests.at(-1).searchParams.get('cursor'),'c1');
 p.listeners.get('intake-refresh:click')();await new Promise(resolve=>setImmediate(resolve));assert.equal(requests.at(-1).searchParams.get('cursor'),null);
});

test('expired intake previews are labeled and expose no action',async()=>{
 const p=page('index_intake.html');
 const record={id:'b'.repeat(32),state:'expired',queue_dir:'/queue',download_mode:'keep',created:'2026-10-31T00:00:00Z',expires:'2026-10-31T00:02:00Z',items:[{source:{path:'/source/a.txt'},target:'/queue/a.txt',state:'ready'}]};
 p.context.fetch=async url=>String(url).startsWith('/api/intakes?')?{ok:true,json:async()=>({records:[record],offset:0,total:1,next_cursor:null})}:{ok:true,json:async()=>({state:'ready',ready:true})};
 p.listeners.get('DOMContentLoaded')();await new Promise(resolve=>setImmediate(resolve));
 const html=p.get('intake-records').innerHTML;
 assert.ok(html.includes('预检已失效（未提交）'),html);
 assert.ok(!html.includes('data-recheck')&&!html.includes('data-commit'),html);
});

test('intake record navigation does not commit a page cursor when loading fails',async()=>{
 const p=page('index_intake.html');const cursors=[];let failNext=true;
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');
  if(parsed.pathname==='/api/intakes'){
   const cursor=parsed.searchParams.get('cursor');cursors.push(cursor);
   if(cursor==='c1'&&failNext){failNext=false;return{ok:false,status:503,text:async()=>'暂时不可用'};}
   return{ok:true,json:async()=>({records:[],offset:0,total:40,next_cursor:cursor==='c1'?'c2':'c1'})};
  }
  return{ok:true,json:async()=>({})};
 };
 p.listeners.get('DOMContentLoaded')();await new Promise(resolve=>setImmediate(resolve));
 p.listeners.get('records-next:click')();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(p.get('records-prev').disabled,true);
 p.listeners.get('records-next:click')();await new Promise(resolve=>setImmediate(resolve));
 assert.deepEqual(cursors,[null,'c1','c1']);
 p.listeners.get('records-prev:click')();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(cursors.at(-1),null);
});

test('cloud browser displays provider-page results without inventing a total',async()=>{
 const p=page('index_intake.html');const cloudRequests=[];
 p.context.fetch=async url=>{
  if(url.startsWith('/api/cloud')){
   cloudRequests.push(url);
   const offset=Number(new URL(url,'http://localhost').searchParams.get('offset'));
   return{ok:true,json:async()=>({directory:'/large',queue_dir:'/queue',entries:[{id:String(offset+1),path:`/large/f${offset}`,name:`f${offset}`,can_enqueue:true}],total:null,offset,next_offset:offset+100})};
  }
  if(url==='/api/intakes')return{ok:true,json:async()=>({records:[],offset:0,next_offset:null})};
  return{ok:true,json:async()=>({state:'ready',ready:true})};
 };
 p.get('intake-paths').value='';
 p.listeners.get('DOMContentLoaded')();await vm.runInContext("loadCloud('/large',0)",p.context);
 assert.ok(p.get('cloud-summary').textContent.includes('总数未知'),p.get('cloud-summary').textContent);
 assert.ok(p.get('cloud-summary').textContent.includes('仍有下一页'));
 assert.equal(p.get('cloud-next').disabled,false);
 p.listeners.get('cloud-next:click')();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(cloudRequests.length,3);
 assert.ok(cloudRequests[2].includes('offset=100'));
});

test('cloud browser clears stale actionable entries after a directory request fails',async()=>{
 const p=page('index_intake.html');const requests=[];let releaseFailure;
 p.get('intake-paths').value='';
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');requests.push(parsed);
  if(parsed.searchParams.get('dir')==='/parent')return{ok:true,json:async()=>({directory:'/parent',queue_dir:'/queue',entries:[{id:'1',path:'/parent/item.zip',name:'item.zip',can_enqueue:true}],total:101,offset:0,next_offset:100})};
  return await new Promise(resolve=>{releaseFailure=()=>resolve({ok:false,status:503,text:async()=>'目录暂时无法读取'});});
 };
 await vm.runInContext("loadCloud('/parent',0)",p.context);
 assert.ok(p.get('cloud-entries').innerHTML.includes('/parent/item.zip'));
 const pending=vm.runInContext("loadCloud('/parent/sub',0)",p.context);
 assert.equal(p.get('cloud-entries').innerHTML,'','old directory entries must not remain clickable while the new directory is loading');
 assert.equal(p.get('cloud-prev').disabled,true);assert.equal(p.get('cloud-next').disabled,true);
 releaseFailure();await pending;
 assert.equal(requests.length,2);assert.equal(requests[1].searchParams.get('dir'),'/parent/sub');
 assert.equal(p.get('cloud-entries').innerHTML,'');
 assert.equal(p.get('cloud-directory').value,'/parent/sub');
 assert.equal(p.get('cloud-prev').disabled,true);assert.equal(p.get('cloud-next').disabled,true);
 assert.match(p.get('cloud-summary').textContent,/目录暂时无法读取/);
});

test('cloud management ignores an older directory response after faster navigation',async()=>{
 const p=page('index_cloud.html',[],{directory:'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const pending=new Map();
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');const directory=parsed.searchParams.get('dir');
  if(parsed.pathname==='/api/cloud')return await new Promise(resolve=>pending.set(directory,()=>resolve({ok:true,json:async()=>({directory,entries:[],total:0,offset:0,next_offset:null})})));
  return{ok:true,json:async()=>({bytes:0,files:0,directories:0,complete:true,observed_at_utc:'',cached_at_utc:''})};
 };
 p.get('directory').value='/slow';const slow=p.context.browse();
 p.get('directory').value='/fast';assert.equal(vm.runInContext('current()',p.context),'/fast');const fast=p.context.browse();
 assert.deepEqual(Array.from(pending.keys()),['/slow','/fast']);
 pending.get('/fast')();await fast;
 assert.match(p.get('listing-summary').textContent,/\/fast/);
 pending.get('/slow')();await slow;
 assert.match(p.get('listing-summary').textContent,/\/fast/);
});

test('cloud management returns to the first page when a successful mutation empties the current page',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/docs','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const offsets=[];p.context.confirm=()=>true;
 p.context.fetch=async(url,options={})=>{
  const parsed=new URL(String(url),'http://localhost');
  if(parsed.pathname==='/api/cloud/mutations')return{ok:true,json:async()=>({status:'confirmed',operation_id:'a'.repeat(32)})};
  if(parsed.pathname==='/api/cloud'){
   const offset=Number(parsed.searchParams.get('offset')||0);offsets.push(offset);
   const entries=offset===0?[{id:'2',fs_id:2,path:'/docs/remaining.txt',name:'remaining.txt',directory:false,size:10}]:[];
   return{ok:true,json:async()=>({directory:'/docs',entries,offset,next_offset:null})};
  }
  if(parsed.pathname==='/api/cloud-inventory/size')return{ok:true,json:async()=>({bytes:10,files:1,directories:0,complete:true})};
  return{ok:true,json:async()=>({status:'idle',pending_count:0,worker_active:false})};
 };
 vm.runInContext('offset=100',p.context);
 await p.context.mutate('delete',{id:'1',fs_id:1,path:'/docs/last.txt',name:'last.txt',directory:false,size:10});
 assert.deepEqual(offsets,[100,0]);
 assert.match(p.get('listing-summary').textContent,/第 1 项起/);
 assert.match(p.get('entries').innerHTML,/remaining\.txt/);
});

test('cloud cache browsing reanchors an empty page after the cached result count shrinks',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/docs','browse-source':'cache'});await new Promise(resolve=>setImmediate(resolve));
 const offsets=[];
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');
  if(parsed.pathname==='/api/cloud-inventory'){
   const offset=Number(parsed.searchParams.get('offset')||0);offsets.push(offset);
   const entries=offset===0?[{fs_id:2,path:'/docs/remaining.txt',name:'remaining.txt',directory:false,size:10}]:[];
   return{ok:true,json:async()=>({directory:'/docs',entries,metadata:{path:'/docs',status:'complete'},total:50,offset,next_offset:null})};
  }
  if(parsed.pathname==='/api/cloud-inventory/size')return{ok:true,json:async()=>({bytes:10,files:1,directories:0,complete:true,observed_at_utc:'2026-10-31T00:00:00Z',cached_at_utc:'2026-10-31T00:00:00Z'})};
  return{ok:true,json:async()=>({})};
 };
 vm.runInContext('offset=100',p.context);
 await p.context.browse();
 assert.deepEqual(offsets,[100,0]);
 assert.match(p.get('listing-summary').textContent,/第 1 项起/);
 assert.match(p.get('entries').innerHTML,/remaining\.txt/);
});

test('cloud cache search reanchors to its last valid page after matches shrink',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'cache'});await new Promise(resolve=>setImmediate(resolve));
 const offsets=[];vm.runInContext("activeCacheSearch='report'",p.context);
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');
  if(parsed.pathname==='/api/cloud-inventory/search'){
   const offset=Number(parsed.searchParams.get('offset')||0);offsets.push(offset);
   const entries=offset===100?[{fs_id:3,path:'/reports/final.pdf',name:'final.pdf',directory:false,size:5}]:[];
   return{ok:true,json:async()=>({query:'report',entries,total:135,offset,next_offset:null,coverage:{status:'complete',incomplete_directories:0}})};
  }
  return{ok:true,json:async()=>({})};
 };
 vm.runInContext('offset=200',p.context);
 await p.context.browse();
 assert.deepEqual(offsets,[200,100]);
 assert.match(p.get('listing-summary').textContent,/第 101 项起/);
 assert.match(p.get('entries').innerHTML,/final\.pdf/);
});

test('live cloud paging sends the saved boundary and reanchors after remote drift',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/docs','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 p.context.document.querySelector=()=>({});
 const boundary={fs_id:101,path:'/docs/item-100',name:'item-100',directory:false,size:100,provider_mtime:1000,content_md5:'md5-100',fingerprint:'a'.repeat(64)};
 const requests=[];
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');
  if(parsed.pathname==='/api/cloud'){
   const offset=Number(parsed.searchParams.get('offset')||0),expected=parsed.searchParams.get('expected_boundary');requests.push({offset,expected});
   if(offset===0)return{ok:true,json:async()=>({directory:'/docs',entries:[{id:'1',fs_id:1,path:'/docs/item-000',name:'item-000',directory:false,size:1}],offset:0,next_offset:100,next_boundary:boundary})};
   return{ok:true,json:async()=>({directory:'/docs',entries:[{id:'1',fs_id:1,path:'/docs/item-000',name:'item-000',directory:false,size:1}],offset:0,boundary_changed:true,next_offset:100,next_boundary:boundary})};
  }
  if(parsed.pathname==='/api/cloud-inventory/size')return{ok:true,json:async()=>({bytes:1,files:1,directories:0,complete:true})};
  return{ok:true,json:async()=>({})};
 };
 await p.context.browse();
 vm.runInContext('offset=100',p.context);
 await p.context.browse();
 assert.deepEqual(requests.map(x=>x.offset),[0,100]);
 assert.deepEqual(JSON.parse(requests[1].expected),{fingerprint:boundary.fingerprint});
 assert.ok(!requests[1].expected.includes(boundary.path)&&!requests[1].expected.includes(boundary.name),'boundary query exposed remote item identity');
 assert.match(p.get('listing-summary').textContent,/第 1 项起/);
 assert.match(p.get('message').textContent,/翻页期间.*变化/);
});

test('live cloud browser labels when this page was locally observed',async()=>{
 const p=page('index_cloud.html',[],{directory:'/docs','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));p.context.document.querySelector=()=>({});
 p.context.fetch=async url=>{const parsed=new URL(String(url),'http://localhost');if(parsed.pathname==='/api/cloud')return{ok:true,json:async()=>({directory:'/docs',entries:[],offset:0,next_offset:null,observed_at_utc:'2026-10-31T02:03:04.123Z'})};if(parsed.pathname==='/api/cloud-inventory/size')return{ok:true,json:async()=>({bytes:0,files:0,directories:0,complete:true})};return{ok:true,json:async()=>({})};};
 await p.context.browse();assert.match(p.get('listing-summary').textContent,/本机读取完成 UTC 2026-10-31T02:03:04\.123Z/);
});

test('move picker labels the local observation time of its current page',async()=>{
 const p=page('index_cloud.html',[],{directory:'/docs','browse-source':'live'});const dialog=p.get('move-picker');dialog.showModal=()=>{dialog.open=true};dialog.close=()=>{dialog.open=false};
 p.context.fetch=async url=>{const parsed=new URL(String(url),'http://localhost');if(parsed.pathname==='/api/cloud')return{ok:true,json:async()=>({directory:'/docs',entries:[],offset:0,next_offset:null,observed_at_utc:'2026-10-31T02:03:04.123Z'})};return{ok:true,json:async()=>({})};};
 p.context.openMovePicker({fs_id:5,path:'/source.bin',name:'source.bin',directory:false,size:1,server_mtime:1});await new Promise(resolve=>setImmediate(resolve));
 assert.match(p.get('move-picker-page').textContent,/本机读取完成 UTC 2026-10-31T02:03:04\.123Z/);
});

test('cloud management ignores a size refresh that finishes after navigating elsewhere',async()=>{
 const p=page('index_cloud.html',[],{directory:'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 let releaseOldSize;const requested=[];
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');
  requested.push(parsed.pathname+parsed.search);
  if(parsed.pathname==='/api/cloud-inventory/size'&&parsed.searchParams.get('dir')==='/old')return await new Promise(resolve=>{releaseOldSize=()=>resolve({ok:true,json:async()=>({bytes:10,files:1,directories:0,complete:true})});});
  if(parsed.pathname==='/api/cloud')return{ok:true,json:async()=>({directory:'/new',entries:[],total:0,offset:0,next_offset:null})};
  return{ok:true,json:async()=>({bytes:20,files:2,directories:0,complete:true})};
 };
 p.get('directory').value='/old';const oldSize=p.get('refresh-size').onclick();
 p.get('directory').value='/new';assert.equal(p.context.current(),'/new');await p.context.browse();
 assert.match(p.get('directory-size').textContent,/\/new/,requested.join('\n'));
 releaseOldSize();await oldSize;
 assert.match(p.get('directory-size').textContent,/\/new/);
});

test('failed local migration explains how to recover from files in use',async()=>{
 const p=page('index_cloud.html',[],{directory:'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 p.context.clearInterval=()=>{};
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');
  if(parsed.pathname==='/api/settings/local-migration')return{ok:true,json:async()=>({status:'failed',phase:'copying',source_path:'D:/old',target_path:'D:/new',processed_files:2,total_files:5,processed_bytes:10,total_bytes:30,started_at_utc:'2026-10-30T00:00:00Z',updated_at_utc:'2026-10-30T00:01:00Z',last_error:'文件正被另一进程使用'})};
  if(parsed.pathname==='/api/settings/local-migrations')return{ok:true,json:async()=>({entries:[],offset:0,next_offset:null,total:0})};
  return{ok:true,json:async()=>({})};
 };
 await p.context.pollMigration();
 assert.match(p.get('migration-progress').textContent,/文件正被另一进程使用/);
 assert.match(p.get('migration-progress').textContent,/关闭.*使用该目录的程序/);
 assert.match(p.get('migration-progress').textContent,/重新保存.*重试迁移/);
});

test('intake commit requires fresh explicit confirmation and does not replay after unknown result',async()=>{
 const p=page('index_intake.html');
 const calls=[];
 const record={id:'a'.repeat(32),state:'preview',expires:new Date(Date.now()+60000).toISOString(),items:[]};
 p.context.proposed=record;vm.runInContext('activePreview=proposed',p.context);
 p.context.fetch=async(url,options)=>{calls.push([url,options?.method]);return{ok:true,json:async()=>url==='/api/intakes/'+record.id+'/commit'?{...record,state:'finished',items:[{state:'unknown'}]}:{records:[]}}};
 await vm.runInContext('commitPreview()',p.context);assert.equal(calls.length,0);
 p.get('intake-confirm-check').checked=true;
 await vm.runInContext('commitPreview()',p.context);
 await vm.runInContext('commitPreview()',p.context);
 assert.equal(calls.filter(call=>call[0].endsWith('/commit')).length,1);
 assert.ok(p.get('intake-message').textContent.includes('未知'));
});

test('intake preview displays the safe server network diagnostic without retrying',async()=>{
 const p=page('index_intake.html');let calls=0;
 p.get('intake-paths').value='/AI量化交易第1期';
 p.context.fetch=async(_url,options)=>{calls++;assert.equal(options.method,'POST');return{ok:false,status:502,text:async()=> '无法解析百度网盘域名，请检查 NAS DNS/网络；修复后只读查询可安全重试。'};};
 await vm.runInContext('createPreview()',p.context);
 assert.equal(calls,1);assert.match(p.get('intake-message').textContent,/无法解析百度网盘域名/);assert.match(p.get('intake-message').textContent,/只读查询可安全重试/);
 assert.equal(p.get('intake-commit').disabled,true);assert.equal(p.get('intake-confirm-check').checked,false);
});

test('external selection fragment is removed and fills input without preview or move',()=>{
 const p=page('index_intake.html');let calls=0;const removed=[];
 p.context.fetch=()=>{calls++;throw Error('no automatic request');};
 p.context.location.hash='#nas-intake='+encodeURIComponent(JSON.stringify({version:1,paths:['/中文 目录/a.zip','/b']}));
 p.context.location.pathname='/intake';p.context.location.search='';
 p.context.history={replaceState:(_state,_title,url)=>removed.push(url)};
 vm.runInContext('consumeIntakeHandoff()',p.context);
 assert.deepEqual(removed,['/intake']);assert.equal(p.get('intake-paths').value,'/中文 目录/a.zip\n/b');
 assert.equal(calls,0);assert.ok(p.get('intake-message').textContent.includes('账号'));
 assert.equal(p.get('intake-confirm-check').checked,false);assert.equal(p.get('intake-commit').disabled,true);
});

test('intake paths preserve meaningful leading and trailing spaces',async()=>{
 const p=page('index_intake.html');const requests=[];
 p.get('intake-paths').value='/folder/file.zip \n\n   \n/folder2/trailing ';
 assert.deepEqual(Array.from(vm.runInContext('inputPaths()',p.context)),['/folder/file.zip ','/folder2/trailing ']);
 p.context.fetch=async(_url,options)=>{requests.push(JSON.parse(options.body));return{ok:true,json:async()=>({state:'preview',id:'a'.repeat(32),expires:new Date(Date.now()+60000).toISOString(),items:[]})};};
 await vm.runInContext('createPreview()',p.context);
 assert.deepEqual(requests[0].paths,['/folder/file.zip ','/folder2/trailing ']);
});

test('manual cloud directory input preserves path whitespace',async()=>{
 const p=page('index_intake.html');const requested=[];
 p.get('intake-paths').value='';
 p.context.fetch=async url=>{const parsed=new URL(String(url),'http://localhost');if(parsed.pathname==='/api/cloud'){requested.push(parsed.searchParams.get('dir'));return{ok:true,json:async()=>({directory:parsed.searchParams.get('dir'),queue_dir:'/queue',entries:[],total:0,offset:0,next_offset:null})};}return{ok:true,json:async()=>({records:[],offset:0,next_cursor:null})};};
 p.listeners.get('DOMContentLoaded')();await new Promise(resolve=>setImmediate(resolve));
 p.get('cloud-directory').value='/folder/ leading and trailing ';
 p.listeners.get('cloud-form:submit')({preventDefault(){}});await new Promise(resolve=>setImmediate(resolve));
 assert.equal(requested.at(-1),'/folder/ leading and trailing ');
 const management=page('index_cloud.html',[],{directory:'/', 'browse-source':'live'});
 management.get('directory').value='/folder/ leading and trailing ';
 assert.equal(vm.runInContext('current()',management.context),'/folder/ leading and trailing ');
});

test('external selection handoff preserves spaces in cloud paths',()=>{
 const p=page('index_intake.html');let calls=0;
 p.context.location.hash='#nas-intake='+encodeURIComponent(JSON.stringify({version:1,paths:['/folder/file.zip ','/folder2/trailing ']}));
 p.context.location.pathname='/intake';p.context.location.search='';p.context.history={replaceState:()=>{}};p.context.fetch=()=>{calls++;throw Error('handoff must not request');};
 vm.runInContext('consumeIntakeHandoff()',p.context);
 assert.equal(p.get('intake-paths').value,'/folder/file.zip \n/folder2/trailing ');
 assert.equal(calls,0);assert.ok(p.get('intake-message').textContent.includes('已接收'));
});

test('malformed, extra-field and noncanonical handoffs never become input or requests',()=>{
 for(const value of ['%bad',encodeURIComponent(JSON.stringify({version:2,paths:['/a']})),encodeURIComponent(JSON.stringify({version:1,paths:['/a'],token:'secret'})),encodeURIComponent(JSON.stringify({version:1,paths:['/a/../b']})),encodeURIComponent(JSON.stringify({version:1,paths:['/a','/a']})),'x'.repeat(60001)]) {
  const p=page('index_intake.html');let cleared=0;
  p.context.location.hash='#nas-intake='+value;p.context.location.pathname='/intake';p.context.location.search='';
  p.context.history={replaceState:()=>cleared++};p.context.fetch=()=>{throw Error('no request');};
  vm.runInContext('consumeIntakeHandoff()',p.context);
  assert.equal(cleared,1);assert.equal(p.get('intake-paths').value,undefined);assert.ok(p.get('intake-message').textContent.includes('无效'));
 }
});

test('handoff fails without importing if browser cannot clear the fragment',()=>{
 const p=page('index_intake.html');
 p.context.location.hash='#nas-intake='+encodeURIComponent(JSON.stringify({version:1,paths:['/a']}));
 p.context.history={replaceState:()=>{throw Error('blocked');}};
 p.context.fetch=()=>{throw Error('no request');};
 vm.runInContext('consumeIntakeHandoff()',p.context);
 assert.equal(p.get('intake-paths').value,undefined);
 assert.ok(p.get('intake-message').textContent.includes('无法清除'));
});

test('uncertain source operation is visible and has no root action controls',()=>{
 const p=page('index_ws.html');
 p.context.task={task_id:'123',is_root:true,status:'source_unknown',can_cancel:false,can_retry:false,download_mode:'keep'};
 assert.ok(vm.runInContext("getStatusBadge('source_unknown')",p.context).includes('禁止重发'));
 const controls=vm.runInContext('taskControls(task)',p.context);
 assert.ok(!controls.includes('data-action='));
});

test('cloud operation history reads local pages on demand and escapes evidence',async()=>{
 const p=page('index_operations.html',[],{directory:'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const calls=[];const id='a'.repeat(32);
 p.context.fetch=async(url,options)=>{
  calls.push([String(url),options?.method]);
  if(String(url).includes('/events?'))return{ok:true,json:async()=>({events:[{sequence:2,status:'unknown',recorded_at_utc:'2026-10-30T02:00:00Z',response_observed_at_utc:null,evidence:{note:'<img src=x onerror=alert(1)>'}}],next_before_sequence:null})};
  return{ok:true,json:async()=>({operations:[{operation_id:id,kind:'move',object_id:77,status:'unknown',created_at_utc:'2026-10-30T01:00:00Z',response_observed_at_utc:null,source_path:'/source/<img>',destination_path:'/queue/item'}],total:1,offset:0,limit:20,next_offset:null})};
 };
 p.get('remote-operation-status').value='unknown';
 assert.equal(calls.length,0,'collapsed history must not load until opened');
 const history=p.get('remote-operation-details');history.open=true;
 p.listeners.get('remote-operation-details:toggle')({currentTarget:history});
 await new Promise(resolve=>setImmediate(resolve));
 const html=p.get('remote-operation-list').innerHTML;
 assert.ok(calls[0][0].includes('/api/cloud/operations?'));assert.ok(calls[0][0].includes('status=unknown'));
 assert.ok(html.includes('结果未知（隔离，不可重发）'));assert.ok(html.includes('/source/&lt;img&gt;'));assert.ok(!html.includes('/source/<img'));
 assert.ok(html.includes('发起时间（UTC）'));assert.ok(html.includes('查看事件证据'));
 assert.equal(p.get('remote-operation-next').disabled,true);assert.equal(p.get('remote-operation-prev').disabled,true);
 assert.ok(!html.includes('data-action=')&&!html.includes('>放行<')&&!html.includes('删除记录'));
 const list=p.get('remote-operation-list');
 p.get('remote-operation-events-'+id).hidden=true;
 list.onclick({target:{closest:selector=>selector==='[data-load-events]'?{dataset:{loadEvents:id}}:null}});
 await new Promise(resolve=>setImmediate(resolve));
 const events=p.get('remote-operation-events-'+id);
 assert.ok(calls.some(call=>call[0].includes('/api/cloud/operations/'+id+'/events?')));
 assert.ok(events.innerHTML.includes('&lt;img src=x'));assert.ok(!events.innerHTML.includes('<img src=x'));
 assert.ok(calls.every(call=>call[1]===undefined),'history requests must remain GET-only');
});

test('remote operation history explains cache recovery without implying queue reconciliation',async()=>{
 const p=page('index_operations.html');await new Promise(resolve=>setImmediate(resolve));
 const id='c'.repeat(32);
 p.context.fetch=async url=>String(url).includes('/events?')
  ?{ok:true,json:async()=>({events:[{sequence:4,status:'unknown',recorded_at_utc:'2026-10-31T03:00:00Z',evidence:{event:'local_cache_recovery_complete',invalidated_cache_paths:['/source/<img>']}}],next_before_sequence:null})}
  :{ok:true,json:async()=>({operations:[{operation_id:id,kind:'move',object_id:8,status:'unknown',created_at_utc:'2026-10-31T02:00:00Z',source_path:'/source/item',destination_path:'/archive/item'}],total:1,offset:0,limit:20,next_offset:null})};
 const details=p.get('remote-operation-details');details.open=true;p.listeners.get('remote-operation-details:toggle')({currentTarget:details});await new Promise(resolve=>setImmediate(resolve));
 const list=p.get('remote-operation-list');p.get('remote-operation-events-'+id).hidden=true;
 list.onclick({target:{closest:selector=>selector==='[data-load-events]'?{dataset:{loadEvents:id}}:null}});await new Promise(resolve=>setImmediate(resolve));
 const html=p.get('remote-operation-events-'+id).innerHTML;
 assert.ok(html.includes('本地缓存恢复完成'));
 assert.ok(html.includes('只确认受影响目录已标为 stale'));
 assert.ok(html.includes('本机记录时间（UTC）'));
 assert.ok(html.includes('/source/&lt;img&gt;'));
 assert.ok(!html.includes('/source/<img'));
 assert.ok(html.includes('local_cache_recovery_complete'),'raw JSON evidence remains available');
});

test('remote operation event status filter resets cursor, persists for older pages, clears and ignores stale responses',async()=>{
 const p=page('index_operations.html');await new Promise(resolve=>setImmediate(resolve));
 const id='f'.repeat(32),calls=[];let resolveOld,eventCalls=0;
 p.context.fetch=url=>{
  const text=String(url);calls.push(text);
  if(text.includes('/events?')){
   eventCalls++;
   if(eventCalls===4)return new Promise(resolve=>{resolveOld=resolve;});
   return Promise.resolve({ok:true,json:async()=>({events:[],next_before_sequence:null})});
  }
  return Promise.resolve({ok:true,json:async()=>({operations:[{operation_id:id,kind:'move',object_id:1,status:'unknown',created_at_utc:'2026-10-30T00:00:00Z',source_path:'/a',destination_path:'/b'}],total:1,offset:0,limit:20,next_offset:null})});
 };
 const details=p.get('remote-operation-details');details.open=true;p.listeners.get('remote-operation-details:toggle')({currentTarget:details});await new Promise(resolve=>setImmediate(resolve));
 const list=p.get('remote-operation-list');
 const target=(key,value)=>({target:{closest:selector=>selector==='['+key+']'?{dataset:{[value]:id}}:null}});
 p.get('remote-operation-events-'+id).hidden=true;
 list.onclick(target('data-load-events','loadEvents'));await new Promise(resolve=>setImmediate(resolve));
 const panel=p.get('remote-operation-events-'+id);p.get('remote-operation-event-status-'+id).value='failed';
 panel.onclick(target('data-filter-events','filterEvents'));await new Promise(resolve=>setImmediate(resolve));
 const filtered=new URL(calls.at(-1),'http://localhost');assert.equal(filtered.searchParams.get('status'),'failed');assert.equal(filtered.searchParams.has('before_sequence'),false);
 panel.onclick({target:{closest:selector=>selector==='[data-older-events]'?{dataset:{olderEvents:id,before:'9'}}:null}});await new Promise(resolve=>setImmediate(resolve));
 const older=new URL(calls.at(-1),'http://localhost');assert.equal(older.searchParams.get('status'),'failed');assert.equal(older.searchParams.get('before_sequence'),'9');
 p.get('remote-operation-event-status-'+id).value='unknown';panel.onclick(target('data-filter-events','filterEvents'));await new Promise(resolve=>setImmediate(resolve));
 panel.onclick(target('data-clear-event-filter','clearEventFilter'));await new Promise(resolve=>setImmediate(resolve));
 assert.equal(new URL(calls.at(-1),'http://localhost').searchParams.has('status'),false);
 resolveOld({ok:true,json:async()=>({events:[{sequence:99,status:'unknown',evidence:{}}],next_before_sequence:null})});await new Promise(resolve=>setImmediate(resolve));
 assert.ok(!panel.innerHTML.includes('事件 #99'),'late response for the cleared filter must be ignored');
});

test('remote operation events filter by UTC dates across pages and export the current evidence filter',async()=>{
 const p=page('index_operations.html');await new Promise(resolve=>setImmediate(resolve));
 const id='9'.repeat(32),calls=[];let downloaded=null;
 p.context.URL.createObjectURL=()=> 'blob:events';p.context.URL.revokeObjectURL=()=>{};
 p.context.document.createElement=()=>({click(){downloaded=this;},remove(){}});p.context.document.body={append(){}};
 p.context.fetch=async url=>{
  const text=String(url);calls.push(text);
  if(text.includes('/events/export?'))return{ok:true,headers:{get:()=> 'attachment; filename="remote-operation-events-test.json"'},blob:async()=>({size:40})};
  if(text.includes('/events?'))return{ok:true,json:async()=>({events:[],next_before_sequence:7})};
  return{ok:true,json:async()=>({operations:[{operation_id:id,kind:'move',object_id:1,status:'confirmed',created_at_utc:'2026-10-30T00:00:00Z',source_path:'/a',destination_path:'/b'}],total:1,offset:0,limit:20,next_offset:null})};
 };
 const details=p.get('remote-operation-details');details.open=true;p.listeners.get('remote-operation-details:toggle')({currentTarget:details});await new Promise(resolve=>setImmediate(resolve));
 const list=p.get('remote-operation-list');p.get('remote-operation-events-'+id).hidden=true;
 list.onclick({target:{closest:s=>s==='[data-load-events]'?{dataset:{loadEvents:id}}:null}});await new Promise(resolve=>setImmediate(resolve));
 const panel=p.get('remote-operation-events-'+id);p.get('remote-operation-event-status-'+id).value='confirmed';
 p.get('remote-operation-event-from-'+id).value='2026-12-01';p.get('remote-operation-event-through-'+id).value='2026-11-30';
 const invalidCount=calls.length;panel.onclick({target:{closest:s=>s==='[data-filter-events]'?{dataset:{filterEvents:id}}:null}});
 assert.equal(calls.length,invalidCount,'invalid UTC range must not query the API');assert.match(p.get('remote-operation-error').textContent,/UTC 日期范围无效/);
 p.get('remote-operation-event-from-'+id).value='2026-10-29';p.get('remote-operation-event-through-'+id).value='2026-10-30';
 const filterClick={target:{closest:s=>s==='[data-filter-events]'?{dataset:{filterEvents:id}}:null}};panel.onclick(filterClick);await new Promise(resolve=>setImmediate(resolve));
 let query=new URL(calls.at(-1),'http://localhost');assert.equal(query.searchParams.get('status'),'confirmed');assert.equal(query.searchParams.get('recorded_from_utc'),'2026-10-29');assert.equal(query.searchParams.get('recorded_through_utc'),'2026-10-30');
 panel.onclick({target:{closest:s=>s==='[data-older-events]'?{dataset:{olderEvents:id,before:'7'}}:null}});await new Promise(resolve=>setImmediate(resolve));
 query=new URL(calls.at(-1),'http://localhost');assert.equal(query.searchParams.get('recorded_from_utc'),'2026-10-29');assert.equal(query.searchParams.get('before_sequence'),'7');
 const exportButton={dataset:{exportFilteredEvents:id},disabled:false};panel.onclick({target:{closest:s=>s==='[data-export-filtered-events]'?exportButton:null}});await new Promise(resolve=>setImmediate(resolve));
 query=new URL(calls.at(-1),'http://localhost');assert.equal(query.pathname,'/api/cloud/operations/'+id+'/events/export');assert.equal(query.searchParams.get('status'),'confirmed');assert.equal(query.searchParams.get('recorded_through_utc'),'2026-10-30');assert.equal(downloaded.download,'remote-operation-events-test.json');
 panel.onclick({target:{closest:s=>s==='[data-clear-event-filter]'?{dataset:{clearEventFilter:id}}:null}});await new Promise(resolve=>setImmediate(resolve));
 query=new URL(calls.at(-1),'http://localhost');assert.equal(query.searchParams.has('status'),false);assert.equal(query.searchParams.has('recorded_from_utc'),false);assert.equal(query.searchParams.has('recorded_through_utc'),false);
 p.context.fetch=async()=>({ok:false,status:413,text:async()=> '当前筛选命中超过 100 条'});
 panel.onclick({target:{closest:s=>s==='[data-export-filtered-events]'?{dataset:{exportFilteredEvents:id},disabled:false}:null}});await new Promise(resolve=>setImmediate(resolve));
 assert.equal(p.get('remote-operation-error').textContent,'当前筛选命中超过 100 条');
});

test('remote operation event request failures keep the current filters and retry controls visible',async()=>{
 const p=page('index_operations.html');await new Promise(resolve=>setImmediate(resolve));
 const id='8'.repeat(32);let eventRequests=0;
 p.context.fetch=async url=>{
  if(String(url).includes('/events?')){eventRequests++;return eventRequests===1?{ok:true,json:async()=>({events:[{sequence:1,status:'prepared',recorded_at_utc:'2026-10-30T00:00:00Z',evidence:{}}],next_before_sequence:null})}:{ok:false,status:503,text:async()=> '本地操作事件读取失败'};}
  return{ok:true,json:async()=>({operations:[{operation_id:id,kind:'move',object_id:1,status:'prepared',created_at_utc:'2026-10-30T00:00:00Z',source_path:'/a',destination_path:'/b'}],total:1,offset:0,limit:20,next_offset:null})};
 };
 const details=p.get('remote-operation-details');details.open=true;p.listeners.get('remote-operation-details:toggle')({currentTarget:details});await new Promise(resolve=>setImmediate(resolve));
 const panel=p.get('remote-operation-events-'+id);panel.hidden=true;
 p.get('remote-operation-list').onclick({target:{closest:s=>s==='[data-load-events]'?{dataset:{loadEvents:id}}:null}});await new Promise(resolve=>setImmediate(resolve));
 p.get('remote-operation-event-status-'+id).value='failed';p.get('remote-operation-event-from-'+id).value='2026-10-30';
 panel.onclick({target:{closest:s=>s==='[data-filter-events]'?{dataset:{filterEvents:id}}:null}});await new Promise(resolve=>setImmediate(resolve));
 assert.equal(eventRequests,2);assert.ok(panel.innerHTML.includes('本地操作事件读取失败'));assert.ok(panel.innerHTML.includes('id="remote-operation-event-status-'+id+'"'));
 assert.ok(panel.innerHTML.includes('id="remote-operation-event-from-'+id+'"'));assert.ok(panel.innerHTML.includes('data-filter-events="'+id+'"'));
 assert.ok(panel.innerHTML.includes('value="2026-10-30"'),'failed response must retain the applied UTC range');
});

test('cloud operation history sends local search filters as UTC and clear restores all history',async()=>{
 const p=page('index_operations.html',[],{directory:'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const calls=[];
 p.context.fetch=async url=>{calls.push(String(url));return{ok:true,json:async()=>({operations:[],total:0,offset:0,limit:20,next_offset:null})};};
 const history=p.get('remote-operation-details');history.open=true;
 p.listeners.get('remote-operation-details:toggle')({currentTarget:history});await new Promise(resolve=>setImmediate(resolve));
 p.get('remote-operation-query').value='target /路径';
 p.get('remote-operation-from').value='2026-10-30T09:00';
 p.get('remote-operation-to').value='2026-10-30T10:30';
 p.get('remote-operation-status').value='confirmed';
 p.get('remote-operation-kind').value='move';
 p.get('remote-operation-search').onclick();await new Promise(resolve=>setImmediate(resolve));
 const searched=new URL(calls.at(-1),'http://localhost');
 assert.equal(searched.searchParams.get('q'),'target /路径');assert.equal(searched.searchParams.get('status'),'confirmed');assert.equal(searched.searchParams.get('kind'),'move');
 assert.equal(searched.searchParams.get('from'),new Date('2026-10-30T09:00').toISOString());
 assert.equal(searched.searchParams.get('to'),new Date('2026-10-30T10:30').toISOString());assert.equal(searched.searchParams.has('offset'),false);
 p.get('remote-operation-clear').onclick();await new Promise(resolve=>setImmediate(resolve));
 const cleared=new URL(calls.at(-1),'http://localhost');
 for(const key of ['q','status','kind','from','to'])assert.equal(cleared.searchParams.has(key),false,key);
 assert.equal(cleared.searchParams.has('offset'),false);
});

test('cloud operation history exports current filters without paging or extra requests',async()=>{
 const p=page('index_operations.html',[],{'directory':'/', 'browse-source':'live'});
 p.get('remote-operation-query').value='target /路径';
 p.get('remote-operation-from').value='2026-10-30T09:00';
 p.get('remote-operation-to').value='2026-10-30T10:30';
 p.get('remote-operation-status').value='confirmed';
 p.get('remote-operation-kind').value='delete';
 let requests=0,clicked=null;p.context.URL.createObjectURL=()=> 'blob:export';p.context.URL.revokeObjectURL=()=>{};
 p.context.document.createElement=tag=>({tagName:tag,click(){clicked=this;},remove(){}});p.context.document.body={append(){}};
 p.context.fetch=async(url,options)=>{requests++;assert.equal(options.credentials,'same-origin');const download=new URL(String(url),'http://localhost');
  assert.equal(download.pathname,'/api/cloud/operations/export');
  assert.equal(download.searchParams.get('q'),'target /路径');assert.equal(download.searchParams.get('status'),'confirmed');assert.equal(download.searchParams.get('kind'),'delete');
  assert.equal(download.searchParams.get('from'),new Date('2026-10-30T09:00').toISOString());
  assert.equal(download.searchParams.get('to'),new Date('2026-10-30T10:30').toISOString());
  assert.equal(download.searchParams.has('offset'),false);assert.equal(download.searchParams.has('limit'),false);
  return{ok:true,headers:{get:()=> 'attachment; filename="remote-operations-test.json"'},blob:async()=>({size:12})};
 };
 await p.get('remote-operation-export').onclick();
 assert.equal(requests,1);assert.equal(clicked.download,'remote-operations-test.json');assert.equal(clicked.href,'blob:export');
 assert.match(p.get('remote-operation-summary').textContent,/已开始下载/);
});

test('cloud operation history shows export errors in the history status area',async()=>{
 const p=page('index_operations.html',[],{'directory':'/', 'browse-source':'live'});
 p.context.fetch=async()=>({ok:false,status:413,text:async()=> '请缩小筛选范围'});
 await p.get('remote-operation-export').onclick();
 assert.equal(p.get('remote-operation-error').textContent,'请缩小筛选范围');
});

test('cloud operation history exports one operation event timeline without changing its unknown state',async()=>{
 const p=page('index_operations.html',[],{'directory':'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const id='c'.repeat(32);let clicked=null,requestURL='',requestOptions=null;
 p.context.URL.createObjectURL=()=> 'blob:evidence';p.context.URL.revokeObjectURL=()=>{};
 p.context.document.createElement=tag=>({tagName:tag,click(){clicked=this;},remove(){}});p.context.document.body={append(){}};
 p.context.fetch=async(url,options)=>{requestURL=String(url);requestOptions=options;return requestURL.includes('/export')?{ok:true,headers:{get:()=> 'attachment; filename="remote-operation-test.json"'},blob:async()=>({size:20})}:{ok:true,json:async()=>({operations:[{operation_id:id,kind:'move',object_id:1,status:'unknown',created_at_utc:'2026-10-30T00:00:00Z',source_path:'/a',destination_path:'/b'}],total:1,offset:0,limit:20,next_offset:null})};};
 const details=p.get('remote-operation-details');details.open=true;p.listeners.get('remote-operation-details:toggle')({currentTarget:details});await new Promise(resolve=>setImmediate(resolve));
 const html=p.get('remote-operation-list').innerHTML;assert.ok(html.includes('data-export-evidence="'+id+'"'));assert.ok(html.includes('结果未知（隔离，不可重发）'));
 const list=p.get('remote-operation-list');list.onclick({target:{closest:selector=>selector==='[data-export-evidence]'?{dataset:{exportEvidence:id},disabled:false}:null}});await new Promise(resolve=>setImmediate(resolve));
 assert.equal(requestURL,'/api/cloud/operations/'+id+'/export');assert.equal(requestOptions.credentials,'same-origin');assert.equal(clicked.download,'remote-operation-test.json');
 assert.ok(!requestURL.includes('offset=')&&!requestURL.includes('limit='));assert.ok(!html.includes('data-action='));
});

test('cloud operation evidence export surfaces server errors',async()=>{
 const p=page('index_operations.html',[],{'directory':'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const id='d'.repeat(32);p.context.fetch=async url=>String(url).includes('/export')?{ok:false,status:413,text:async()=> '操作事件超过 100 条'}:{ok:true,json:async()=>({operations:[{operation_id:id,kind:'move',object_id:1,status:'confirmed',created_at_utc:'2026-10-30T00:00:00Z',source_path:'/a',destination_path:'/b'}],total:1,offset:0,limit:20,next_offset:null})};
 const details=p.get('remote-operation-details');details.open=true;p.listeners.get('remote-operation-details:toggle')({currentTarget:details});await new Promise(resolve=>setImmediate(resolve));
 p.get('remote-operation-list').onclick({target:{closest:selector=>selector==='[data-export-evidence]'?{dataset:{exportEvidence:id},disabled:false}:null}});await new Promise(resolve=>setImmediate(resolve));
 assert.equal(p.get('remote-operation-error').textContent,'操作事件超过 100 条');
});

test('cloud operation evidence export prevents duplicate clicks while a download is pending',async()=>{
 const p=page('index_operations.html',[],{'directory':'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const id='e'.repeat(32);let resolveExport,exportRequests=0;p.context.URL.createObjectURL=()=> 'blob:evidence';p.context.URL.revokeObjectURL=()=>{};
 p.context.document.createElement=tag=>({tagName:tag,click(){},remove(){}});p.context.document.body={append(){}};
 p.context.fetch=url=>String(url).includes('/export')?(exportRequests++,new Promise(resolve=>{resolveExport=resolve;})):Promise.resolve({ok:true,json:async()=>({operations:[{operation_id:id,kind:'move',object_id:1,status:'confirmed',created_at_utc:'2026-10-30T00:00:00Z',source_path:'/a',destination_path:'/b'}],total:1,offset:0,limit:20,next_offset:null})});
 const details=p.get('remote-operation-details');details.open=true;p.listeners.get('remote-operation-details:toggle')({currentTarget:details});await new Promise(resolve=>setImmediate(resolve));
 const exportButton={dataset:{exportEvidence:id},disabled:false};const list=p.get('remote-operation-list');const click={target:{closest:selector=>selector==='[data-export-evidence]'?exportButton:null}};
 list.onclick(click);assert.equal(exportButton.disabled,true);list.onclick(click);await new Promise(resolve=>setImmediate(resolve));assert.equal(exportRequests,1);
 resolveExport({ok:true,headers:{get:()=> 'attachment; filename="remote-operation-test.json"'},blob:async()=>({size:10})});await new Promise(resolve=>setImmediate(resolve));assert.equal(exportButton.disabled,false);
});

test('cloud operation history ignores stale search responses',async()=>{
 const p=page('index_operations.html',[],{directory:'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const requests=[];
 p.context.fetch=url=>new Promise(resolve=>requests.push({url:String(url),resolve}));
 const history=p.get('remote-operation-details');history.open=true;
 p.listeners.get('remote-operation-details:toggle')({currentTarget:history});await new Promise(resolve=>setImmediate(resolve));
 p.get('remote-operation-query').value='new';p.get('remote-operation-search').onclick();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(requests.length,2);
 const response=(id)=>({ok:true,json:async()=>({operations:[{operation_id:id.repeat(32),kind:'move',object_id:1,status:'confirmed',created_at_utc:'2026-10-30T00:00:00Z',source_path:'/'+id,destination_path:'/dest'}],total:1,offset:0,limit:20,next_offset:null})});
 requests[1].resolve(response('b'));await new Promise(resolve=>setImmediate(resolve));
 requests[0].resolve(response('a'));await new Promise(resolve=>setImmediate(resolve));
 assert.ok(p.get('remote-operation-list').innerHTML.includes('源路径：/b</small>'));
 assert.ok(!p.get('remote-operation-list').innerHTML.includes('源路径：/a</small>'));
});

test('cloud management page exposes live browsing, cache sizing, scan controls and remote CRUD without upload',()=>{
 const html=fs.readFileSync(path.join(root,'index_cloud.html'),'utf8');
 const scripts=[...html.matchAll(/<script>([\s\S]*?)<\/script>/g)].map(match=>match[1]);
 assert.equal(scripts.length,1);
 new vm.Script(scripts[0]);
 assert.ok(html.includes('/api/cloud-inventory/size?dir='));
 const browser=page('index_cloud.html',[],{directory:'/', 'browse-source':'live'});
 const row=vm.runInContext('cloudEntryMarkup',browser.context);
 assert.match(row({directory:true,path:'/folder',name:'folder',size:42,size_complete:false},'live'),/未统计/);
 assert.match(row({directory:true,path:'/folder',name:'folder',size:42,size_complete:true},'live'),/42 B/);
 assert.ok(html.includes('size:x.provider_size??x.size'));
 assert.ok(html.includes('d.cached_at_utc'));
 assert.ok(html.includes('云端最近观测'));
 assert.ok(html.includes('/api/cloud-inventory/scan'));
	assert.ok(html.includes('/api/cloud-inventory/refresh'));
	assert.ok(html.includes('id="refresh-directory"'));
	assert.ok(html.includes('id="resume-directory-refresh"'));
	assert.ok(html.includes('worker_active'));
	assert.ok(html.includes('id="pause-directory-refresh"'));
	assert.ok(html.includes('id="refresh-runs-load"'));
	assert.ok(html.includes('id="refresh-runs-older"'));
	assert.ok(html.includes('id="refresh-events-older"'));
	assert.ok(html.includes('id="refresh-events-kind"'));
 assert.ok(html.includes('id="refresh-events-kind"'));
	assert.ok(html.includes('本机记录 UTC：'));
	assert.ok(html.includes('百度目录读取完成 UTC：'));
	assert.ok(html.includes('变更计数只统计当前目录的直接子项'));
	assert.ok(html.includes('/api/cloud-inventory/refresh/events'));
	assert.ok(html.includes('directoryRefreshStatus'));
	assert.ok(html.includes('activeDirectoryRefreshRun'));
	assert.ok(html.includes('缓存浏览使用上次完整列表'));
	assert.ok(html.includes('当前目录完整读取后才发布变化'));
	assert.ok(html.includes('以线上结果修正本地缓存并记录证据'));
	assert.ok(html.includes('刷新当前目录及子目录缓存'));
	assert.ok(html.includes('/api/cloud-cleanup/empty-directories'));
	assert.ok(html.includes('require_empty_directory:true'));
	assert.ok(html.includes('已确认删除'));
 assert.ok(html.includes('/api/cloud/mutations'));
 assert.ok(html.includes("mutate('delete',x)"));
 assert.ok(html.includes('onclick=()=>openMovePicker(x)'));
 assert.ok(html.includes("mutate('rename',x"));
 assert.ok(html.includes('result.warning'));
 assert.ok(!/<input[^>]+type=["']file/i.test(html));
 assert.ok(html.includes('id="scan-start"'));
	assert.ok(html.includes('s.pending_count'));
	assert.ok(html.includes('/api/cloud-cleanup/duplicate-scan'));
	assert.ok(html.includes('/api/cloud-cleanup/duplicates'));
	assert.ok(html.includes('expected_content_sha256'));
	assert.ok(html.includes('id="duplicate-delete-all"'));
	assert.ok(html.includes('/api/cloud-cleanup/duplicate-scan'));
	assert.ok(html.includes('/api/cloud-cleanup/duplicates'));
	assert.ok(html.includes('expected_content_sha256'));
	assert.ok(html.includes('id="duplicate-start"'));
	assert.ok(html.includes('/api/cloud-cleanup/duplicate-scan?estimate=1'));
	assert.ok(html.includes('估算读取量'));
 assert.ok(html.includes('id="directory"'));
	assert.ok(html.includes('id="move-picker"'));
	assert.ok(html.includes('onclick=()=>openMovePicker(x)'));
	assert.ok(!html.includes("prompt('输入目标目录的绝对网盘路径'"));
});

test('cloud refresh offers a same-scope resume action for paused or persisted inactive checkpoints',async()=>{
 const p=page('index_cloud.html',[],{directory:'/', 'browse-source':'live'});await waitForPollIdle(p,'directoryRefreshStatusInFlight');
 let active=false,status='running',submitted=null;
 const checkpoint=()=>({account_fingerprint:'acct',run_id:'restart-run',root_path:'/target',status,worker_active:active,phase:status==='paused'?'request_budget_reached':'listing',request_budget:10,requests_used:2,pending_count:1,current_directory:'/target',pages_processed:2,items_processed:4});
 p.context.fetch=async(url,options={})=>{
  const parsed=new URL(String(url),'http://localhost');
  if(parsed.pathname==='/api/cloud-inventory/refresh'&&options.method==='POST'){
   submitted=JSON.parse(options.body);active=true;status='running';return{ok:true,json:async()=>checkpoint()};
  }
  if(parsed.pathname==='/api/cloud-inventory/refresh')return{ok:true,json:async()=>checkpoint()};
  if(parsed.pathname==='/api/cloud')return{ok:true,json:async()=>({directory:'/',entries:[],total:0,next_offset:null})};
  if(parsed.pathname==='/api/cloud-inventory/size')return{ok:true,json:async()=>({complete:true,bytes:0,files:0,directories:0})};
  return{ok:true,json:async()=>({})};
 };
 await vm.runInContext('directoryRefreshStatus()',p.context);
 assert.equal(p.get('resume-directory-refresh').hidden,false);
 assert.match(p.get('directory-refresh-summary').textContent,/待继续/);
 status='paused';active=true;
 await vm.runInContext('directoryRefreshStatus()',p.context);
 assert.equal(p.get('resume-directory-refresh').hidden,true,'do not offer resume while a paused worker is still exiting');
 assert.equal(!!p.get('scan-start').disabled,true,'a directory worker must block full-scan starts');
 assert.equal(!!p.get('refresh-directory').disabled,true,'a directory worker must block another scoped refresh');
 status='paused';active=false;
 await vm.runInContext('directoryRefreshStatus()',p.context);
 assert.equal(p.get('resume-directory-refresh').hidden,false,'paused checkpoint should be resumable after browsing elsewhere');
 assert.match(p.get('resume-directory-refresh').textContent,/追加额度并继续刷新/);
 assert.match(p.get('directory-refresh-summary').textContent,/已预留额度 2 \/ 10/);
 assert.match(p.get('directory-refresh-progress').textContent,/额度已耗尽/);
 assert.equal(!!p.get('scan-start').disabled,false);
 assert.equal(!!p.get('refresh-directory').disabled,false);
 p.get('directory-refresh-budget').value='8';
 await p.get('resume-directory-refresh').onclick();
 assert.deepEqual(submitted,{directory:'/target',request_budget:8});
 assert.equal(p.get('directory').value,'/','resuming a saved scope keeps the current browse path');
 assert.equal(vm.runInContext('activeDirectoryRefreshRoot',p.context),'/target');
 assert.equal(p.get('resume-directory-refresh').hidden,true);
 assert.match(p.get('directory-refresh-progress').textContent,/本进程 worker：运行中/);
});

test('completed directory refresh only reloads the affected cached browse context',async()=>{
 async function completeRefresh(initialValues,search,offset){
  const p=page('index_cloud.html',[],initialValues);await waitForPollIdle(p,'directoryRefreshStatusInFlight');
  const calls=[];p.context.fetch=async(url)=>{const parsed=new URL(String(url),'http://localhost');calls.push(parsed);if(parsed.pathname==='/api/cloud-inventory/refresh')return{ok:true,json:async()=>({run_id:'run-1',root_path:'/target',status:'complete',worker_active:false,phase:'complete',pending_count:0})};if(parsed.pathname==='/api/cloud-inventory')return{ok:true,json:async()=>({directory:initialValues.directory,entries:[],metadata:{status:'complete',generation:3},cache_generation:3,total:0,next_offset:null})};if(parsed.pathname==='/api/cloud-inventory/search')return{ok:true,json:async()=>({entries:[],coverage:{status:'complete'},cache_generation:3,total:0,next_offset:null})};return{ok:true,json:async()=>({complete:true,bytes:0,files:0,directories:0})};};
  vm.runInContext("activeDirectoryRefreshRun='run-1';activeDirectoryRefreshRoot='/target';activeCacheSearch="+JSON.stringify(search||'')+";offset="+(offset||0),p.context);if(search)p.get('cache-search-query').value=search;
  await vm.runInContext('directoryRefreshStatus()',p.context);return{p,calls};
 }
 const live=await completeRefresh({directory:'/target/sub','browse-source':'live'});
 assert.equal(live.calls.length,1,'completion may read refresh status but must not browse the live provider or request size');
 assert.equal(live.calls[0].pathname,'/api/cloud-inventory/refresh');
 const unrelated=await completeRefresh({directory:'/targeted/child','browse-source':'cache'});
 assert.equal(unrelated.calls.length,1,'an unrelated cached directory stays untouched');
 const affected=await completeRefresh({directory:'/target/sub','browse-source':'cache'},'',50);
 const affectedList=affected.calls.find(call=>call.pathname==='/api/cloud-inventory');
 assert.equal(affectedList.searchParams.get('dir'),'/target/sub');assert.equal(affectedList.searchParams.get('offset'),'0','affected cached folders return to page one');
 assert.ok(affected.calls.some(call=>call.pathname==='/api/cloud-inventory/size'&&call.searchParams.get('dir')==='/target/sub'));
 const search=await completeRefresh({directory:'/','browse-source':'cache'},'alpha',50);
 const searchCall=search.calls.find(call=>call.pathname==='/api/cloud-inventory/search');
 assert.equal(searchCall.searchParams.get('q'),'alpha');assert.equal(searchCall.searchParams.get('offset'),'0','refresh returns cached searches to their first page');
});

test('stale refresh completion cannot reload the current browse context',async()=>{
 const p=page('index_cloud.html',[],{directory:'/target/sub','browse-source':'cache'});await waitForPollIdle(p,'directoryRefreshStatusInFlight');const calls=[];
 p.context.fetch=async url=>{calls.push(String(url));return{ok:true,json:async()=>({run_id:'new-run',root_path:'/target',status:'complete',worker_active:false,phase:'complete',pending_count:0})}};
 vm.runInContext("activeDirectoryRefreshRun='old-run';activeDirectoryRefreshRoot='/target'",p.context);
 await vm.runInContext('directoryRefreshStatus()',p.context);
 assert.deepEqual(calls,['/api/cloud-inventory/refresh'],'a late status for another run cannot trigger browse or size requests');
});

test('failed cloud refresh can start a fresh verification for the saved scope',async()=>{
 const p=page('index_cloud.html',[],{directory:'/', 'browse-source':'live'});await waitForPollIdle(p,'directoryRefreshStatusInFlight');
 let active=false,status='failed',submitted=null,runID='failed-run';
 const checkpoint=()=>({account_fingerprint:'acct',run_id:runID,root_path:'/target',status,worker_active:active,phase:status==='failed'?'failed':'listing',pending_count:status==='failed'?0:1,current_directory:'/target/sub',pages_processed:2,items_processed:4,last_error:status==='failed'?'分页边界发生变化':''});
 p.context.fetch=async(url,options={})=>{
  const parsed=new URL(String(url),'http://localhost');
  if(parsed.pathname==='/api/cloud-inventory/refresh'&&options.method==='POST'){
   submitted=JSON.parse(options.body);active=true;status='running';runID='new-run';return{ok:true,json:async()=>checkpoint()};
  }
  if(parsed.pathname==='/api/cloud-inventory/refresh')return{ok:true,json:async()=>checkpoint()};
  if(parsed.pathname==='/api/cloud')return{ok:true,json:async()=>({directory:'/',entries:[],total:0,next_offset:null})};
  if(parsed.pathname==='/api/cloud-inventory/size')return{ok:true,json:async()=>({complete:true,bytes:0,files:0,directories:0})};
  return{ok:true,json:async()=>({})};
 };
 await vm.runInContext('directoryRefreshStatus()',p.context);
 const retry=p.get('resume-directory-refresh');
 assert.equal(retry.hidden,false);
 assert.match(retry.textContent,/重新刷新失败范围/);
 assert.match(p.get('directory-refresh-progress').textContent,/从头开始新一轮/);
 await retry.onclick();
 assert.deepEqual(submitted,{directory:'/target',request_budget:0});
 assert.equal(p.get('directory').value,'/','restarting a failed scope must not navigate away from the current browse path');
 assert.equal(vm.runInContext('activeDirectoryRefreshRoot',p.context),'/target');
 assert.equal(retry.hidden,true);
 assert.match(p.get('directory-refresh-progress').textContent,/本进程 worker：运行中/);
});

test('failed cloud refresh retry waits until the old worker has stopped',async()=>{
 const p=page('index_cloud.html',[],{directory:'/', 'browse-source':'live'});await waitForPollIdle(p,'directoryRefreshStatusInFlight');
 let active=true;
 p.context.fetch=async(url)=>String(url)==='/api/cloud-inventory/refresh'?{ok:true,json:async()=>({run_id:'failed-run',root_path:'/target',status:'failed',worker_active:active,phase:'failed',pending_count:0,current_directory:'/target'})}:{ok:true,json:async()=>({})};
 await vm.runInContext('directoryRefreshStatus()',p.context);
 const retry=p.get('resume-directory-refresh');
 assert.equal(retry.hidden,true,'retry must stay hidden while the failed worker is still exiting');
 assert.match(p.get('directory-refresh-progress').textContent,/worker：运行中/);
 active=false;
 await vm.runInContext('directoryRefreshStatus()',p.context);
 assert.equal(retry.hidden,false,'retry should appear after the old worker has stopped');
 assert.match(retry.textContent,/重新刷新失败范围/);
});

test('retrying a failed root refresh requires the whole-account confirmation',async()=>{
 const p=page('index_cloud.html',[],{directory:'/','browse-source':'live'});await waitForPollIdle(p,'directoryRefreshStatusInFlight');
 let confirmCalls=0,postCalls=0,confirmed=false;
 p.context.confirm=()=>{confirmCalls++;return confirmed};
 p.context.fetch=async(url,options={})=>{
  if(options.method==='POST')postCalls++;
  if(String(url)==='/api/cloud-inventory/refresh')return{ok:true,json:async()=>({run_id:'root-failed',root_path:'/',status:'failed',worker_active:false,phase:'failed',pending_count:0,current_directory:'/',last_error:'page drift'})};
  return{ok:true,json:async()=>({})};
 };
 await vm.runInContext('directoryRefreshStatus()',p.context);
 await p.get('resume-directory-refresh').onclick();
 assert.equal(confirmCalls,1);
 assert.equal(postCalls,0,'cancelled root refresh retry must not start a request');
 confirmed=true;
 await p.get('resume-directory-refresh').onclick();
 assert.equal(confirmCalls,2);
 assert.equal(postCalls,1);
});

test('directory refresh run history loads on demand and pages runs and selected run events',async()=>{
 const p=page('index_cloud.html',[],{directory:'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const calls=[];let runPage=0,eventPage=0;
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');calls.push(parsed);
  if(parsed.pathname==='/api/cloud-inventory/refresh/runs'){
   runPage++;
   if(runPage===1)return{ok:true,json:async()=>({runs:[{start_sequence:20,run_id:'new-run',started_at_utc:'2026-10-30T00:03:00Z',latest_kind:'completed',status:'complete',phase:'complete',root_path:'/new',current_directory:'/new',pages_processed:3,items_processed:8,request_budget:20,requests_used:5,pending_count:0,last_recorded_at_utc:'2026-10-30T00:04:00Z'},{start_sequence:10,run_id:'old-run',started_at_utc:'2026-10-30T00:00:00Z',latest_kind:'paused',status:'paused',phase:'paused',root_path:'/scope',current_directory:'/&lt;folder&gt;',pages_processed:2,items_processed:4,request_budget:11,requests_used:7,pending_count:1,last_recorded_at_utc:'2026-10-30T00:02:00Z'}],next_before_sequence:10,has_more:true})};
   return{ok:true,json:async()=>({runs:[{start_sequence:5,run_id:'older-run',started_at_utc:'2026-10-29T00:00:00Z',latest_kind:'failed',status:'failed',phase:'failed',pages_processed:1,items_processed:1,pending_count:0,last_recorded_at_utc:'2026-10-29T00:01:00Z'}],has_more:false})};
  }
  if(parsed.pathname==='/api/cloud-inventory/refresh/events'){
   eventPage++;
   if(eventPage===1)return{ok:true,json:async()=>({run_id:'old-run',events:[{sequence:13,run_id:'old-run',kind:'retry_scheduled',status:'running',phase:'backoff',current_directory:'/scope',recorded_at_utc:'2026-10-30T00:03:00Z',retry_at_utc:'2026-10-30T00:03:05Z',request_budget:11,requests_used:7,request_budget_added:2,last_error:'temporary &lt;provider&gt; issue',pages_processed:2,items_processed:4,pending_count:1},{sequence:12,run_id:'old-run',kind:'directory_published',status:'running',phase:'listing',current_directory:'/&lt;folder&gt;',recorded_at_utc:'2026-10-30T00:02:00Z',cloud_observed_at_utc:'2026-10-30T00:01:59Z',directory_changed:true,pages_processed:2,items_processed:4,pending_count:1}],next_before_sequence:12,has_more:true})};
   if(eventPage===2)return{ok:true,json:async()=>({run_id:'old-run',events:[{sequence:11,run_id:'old-run',kind:'started',status:'running',phase:'listing',recorded_at_utc:'2026-10-30T00:00:00Z',pages_processed:0,items_processed:0,pending_count:1}],has_more:false})};
   if(eventPage===3)return{ok:true,json:async()=>({run_id:'old-run',events:[{sequence:10,run_id:'old-run',kind:'directory_published',status:'running',phase:'listing',current_directory:'/filtered',recorded_at_utc:'2026-10-29T00:00:00Z',pages_processed:1,items_processed:2,pending_count:0}],next_before_sequence:10,has_more:true})};
   if(eventPage===5)return{ok:true,json:async()=>({run_id:'old-run',events:[{sequence:8,run_id:'old-run',kind:'boundary_restart',status:'running',phase:'listing',current_directory:'/dup',recorded_at_utc:'2026-10-27T00:00:00Z',restart_reason:'provider_duplicate_page_id',restart_offset:100,restart_fs_id:7788,pages_processed:2,items_processed:100,pending_count:1}],has_more:false})};
   return{ok:true,json:async()=>({run_id:'old-run',events:[{sequence:9,run_id:'old-run',kind:'directory_published',status:'running',phase:'listing',current_directory:'/filtered-older',recorded_at_utc:'2026-10-28T00:00:00Z',pages_processed:1,items_processed:2,pending_count:0}],has_more:false})};
  }
  return{ok:true,json:async()=>({entries:[],total:0,metadata:{status:'complete'},status:'idle',authentication:{ready:true},directory:'/'})};
 };
 assert.equal(calls.some(call=>call.pathname==='/api/cloud-inventory/refresh/runs'),false,'run history must not load automatically');
 assert.equal(calls.some(call=>call.pathname==='/api/cloud-inventory/refresh/events'),false,'run events must not load automatically');
 p.get('refresh-runs-load').onclick();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls.filter(call=>call.pathname==='/api/cloud-inventory/refresh/runs').length,1);
 assert.ok(p.get('refresh-runs-list').innerHTML.includes('new-run'));assert.ok(p.get('refresh-runs-list').innerHTML.includes('old-run'));
 assert.ok(p.get('refresh-runs-list').innerHTML.includes('核对范围：/scope'));assert.ok(p.get('refresh-runs-list').innerHTML.includes('已预留请求 7 / 11'));
 const click=p.listeners.get('refresh-runs-list:click');click({target:{closest:selector=>selector==='[data-refresh-run]'?{dataset:{refreshRun:'old-run'}}:null}});await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls.filter(call=>call.pathname==='/api/cloud-inventory/refresh/events')[0].searchParams.get('run_id'),'old-run');
 const html=p.get('refresh-events-list').innerHTML;assert.ok(html.includes('本机记录 UTC：2026-10-30T00:02:00Z'));assert.ok(html.includes('百度目录读取完成 UTC：2026-10-30T00:01:59Z'));assert.ok(html.includes('本机计划重试 UTC：2026-10-30T00:03:05Z'));assert.ok(html.includes('本次追加额度：2'));assert.ok(html.includes('错误摘要：temporary &amp;lt;provider&amp;gt; issue'));assert.ok(html.includes('&amp;lt;folder&amp;gt;'));assert.equal(p.get('refresh-events-older').disabled,false);
 p.get('refresh-events-older').onclick();await new Promise(resolve=>setImmediate(resolve));
 const olderEventRequest=calls.filter(call=>call.pathname==='/api/cloud-inventory/refresh/events')[1];assert.equal(olderEventRequest.searchParams.get('before_sequence'),'12');assert.ok(p.get('refresh-events-list').innerHTML.includes('开始刷新'));assert.equal(p.get('refresh-events-older').disabled,true);
 p.get('refresh-events-kind').value='directory_published';p.get('refresh-events-kind').onchange();await new Promise(resolve=>setImmediate(resolve));
 const filteredRequest=calls.filter(call=>call.pathname==='/api/cloud-inventory/refresh/events')[2];assert.equal(filteredRequest.searchParams.get('kind'),'directory_published');assert.equal(filteredRequest.searchParams.has('before_sequence'),false);assert.ok(p.get('refresh-events-status').textContent.includes('目录核对并发布'));
 p.get('refresh-events-older').onclick();await new Promise(resolve=>setImmediate(resolve));const filteredOlder=calls.filter(call=>call.pathname==='/api/cloud-inventory/refresh/events')[3];assert.equal(filteredOlder.searchParams.get('kind'),'directory_published');assert.equal(filteredOlder.searchParams.get('before_sequence'),'10');assert.ok(p.get('refresh-events-list').innerHTML.includes('/filtered-older'));
 p.get('refresh-events-reason').value='provider_duplicate_page_id';p.get('refresh-events-reason').onchange();await new Promise(resolve=>setImmediate(resolve));const reasonRequest=calls.filter(call=>call.pathname==='/api/cloud-inventory/refresh/events')[4];assert.equal(reasonRequest.searchParams.get('restart_reason'),'provider_duplicate_page_id');assert.equal(reasonRequest.searchParams.has('before_sequence'),false);assert.ok(p.get('refresh-events-list').innerHTML.includes('重核起始偏移量：100'));assert.ok(p.get('refresh-events-list').innerHTML.includes('重复 fs_id：7788'));
 p.get('refresh-runs-older').onclick();await new Promise(resolve=>setImmediate(resolve));
 const olderRunRequest=calls.filter(call=>call.pathname==='/api/cloud-inventory/refresh/runs')[1];assert.equal(olderRunRequest.searchParams.get('before_sequence'),'10');assert.ok(p.get('refresh-runs-list').innerHTML.includes('older-run'));
});

test('directory refresh run history ignores event responses from an unselected run',async()=>{
 const p=page('index_cloud.html',[],{directory:'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 p.context.fetch=async url=>{
  const parsed=new URL(String(url),'http://localhost');
  if(parsed.pathname==='/api/cloud-inventory/refresh/runs')return{ok:true,json:async()=>({runs:[{start_sequence:2,run_id:'old-run',started_at_utc:'2026-10-29T00:00:00Z',status:'paused',phase:'paused'},{start_sequence:3,run_id:'new-run',started_at_utc:'2026-10-30T00:00:00Z',status:'running',phase:'listing'}]})};
  if(parsed.pathname==='/api/cloud-inventory/refresh/events')return{ok:true,json:async()=>({run_id:parsed.searchParams.get('run_id'),events:[{sequence:2,run_id:parsed.searchParams.get('run_id'),kind:'started',status:'running',phase:'listing',recorded_at_utc:'2026-10-30T00:00:00Z',pages_processed:0,items_processed:0,pending_count:1}],has_more:false})};
  return{ok:true,json:async()=>({entries:[],total:0,metadata:{status:'complete'},status:'idle',authentication:{ready:true},directory:'/'})};
 };
 await p.context.loadRefreshRuns();await new Promise(resolve=>setImmediate(resolve));
 p.context.selectRefreshRun('old-run');await new Promise(resolve=>setImmediate(resolve));
 p.context.selectRefreshRun('new-run');await new Promise(resolve=>setImmediate(resolve));
 assert.equal(p.get('refresh-events-status').textContent.includes('new-run'),true);
 assert.ok(p.get('refresh-events-list').innerHTML.includes('开始刷新'));
 assert.ok(!p.get('refresh-events-status').textContent.includes('old-run'));
});

test('directory refresh history displays delta counts and marks missing baselines',()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});
 vm.runInContext(`refreshEvents=[{sequence:1,run_id:'run-a',kind:'directory_published',status:'running',phase:'listing',current_directory:'/added & <changed>',recorded_at_utc:'2026-10-30T00:00:00Z',directory_changed:true,added_count:2,removed_count:1,changed_count:3,pages_processed:1,items_processed:6,pending_count:0},{sequence:2,run_id:'run-a',kind:'directory_published',status:'running',phase:'listing',current_directory:'/first',recorded_at_utc:'2026-10-30T00:01:00Z',directory_changed:true,pages_processed:1,items_processed:1,pending_count:0}]`,p.context);
 p.context.renderRefreshEvents();const html=p.get('refresh-events-list').innerHTML;
 assert.ok(html.includes('直系子项：新增 2 · 移除 1 · 变化 3'));
 assert.ok(html.includes('无完整旧缓存基线'));
 assert.ok(html.includes('/added &amp; &lt;changed&gt;'));
});

test('directory refresh change details load on demand, retain UTC/hash evidence and paginate',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));const calls=[];let detailPage=0;
 p.context.fetch=async url=>{const parsed=new URL(String(url),'http://localhost');calls.push(parsed);if(parsed.pathname==='/api/cloud-inventory/refresh/runs')return{ok:true,json:async()=>({runs:[{run_id:'detail-run'}]})};if(parsed.pathname==='/api/cloud-inventory/refresh/events')return{ok:true,json:async()=>({run_id:'detail-run',events:[{sequence:50,run_id:'detail-run',kind:'directory_published',status:'running',phase:'listing',current_directory:'/folder',recorded_at_utc:'2026-10-30T01:00:00Z',cloud_observed_at_utc:'2026-10-30T00:59:00Z',added_count:1,removed_count:0,changed_count:0,pages_processed:1,items_processed:1,pending_count:0}],has_more:false})};if(parsed.pathname==='/api/cloud-inventory/refresh/changes'){detailPage++;if(detailPage===1)return{ok:true,json:async()=>({run_id:'detail-run',event_sequence:50,changes:[{sequence:9,ordinal:1,kind:'added',new:{fs_id:9,path:'/new/<img src=x>',name:'<img src=x>',directory:false,size:4,provider_mtime:12,content_md5:'md5-new',observed_at_utc:'2026-10-30T00:59:00Z',content_sha256:'a'.repeat(64),content_sha256_verified_at_utc:'2026-10-30T00:58:00Z'},recorded_at_utc:'2026-10-30T01:00:00Z',cloud_observed_at_utc:'2026-10-30T00:59:00Z'}],next_before_sequence:9,has_more:true})};return{ok:true,json:async()=>({run_id:'detail-run',event_sequence:50,changes:[{sequence:8,ordinal:2,kind:'removed',old:{fs_id:8,path:'/removed',name:'removed',directory:false,size:3,provider_mtime:11,observed_at_utc:null},recorded_at_utc:'2026-10-30T01:00:00Z',cloud_observed_at_utc:'2026-10-30T00:59:00Z'}],has_more:false})}}return{ok:true,json:async()=>({})}};
 await p.context.loadRefreshRuns();p.context.selectRefreshRun('detail-run');await new Promise(resolve=>setImmediate(resolve));assert.equal(calls.some(call=>call.pathname==='/api/cloud-inventory/refresh/changes'),false,'details must be fetched only after click');
 const click=p.listeners.get('refresh-events-list:click');click({target:{closest(selector){return selector==='[data-refresh-changes-toggle]'?{dataset:{refreshChangesToggle:'50'}}:null}}});await new Promise(resolve=>setImmediate(resolve));
 const first=calls.find(call=>call.pathname==='/api/cloud-inventory/refresh/changes');assert.equal(first.searchParams.get('event_sequence'),'50');assert.equal(first.searchParams.get('limit'),'50');const html=p.get('refresh-events-list').innerHTML;assert.ok(html.includes('&lt;img src=x&gt;'),JSON.stringify({html,state:vm.runInContext("JSON.stringify(refreshChangePages.get('50'))",p.context),calls:calls.map(c=>c.href)}));assert.ok(!html.includes('<img src=x>'));assert.ok(html.includes('SHA-256：'+'a'.repeat(64)));assert.ok(html.includes('验证 UTC：2026-10-30T00:58:00Z'));assert.ok(html.includes('百度目录观测 UTC：2026-10-30T00:59:00Z'));
 click({target:{closest(selector){return selector==='[data-refresh-changes-older]'?{dataset:{refreshChangesOlder:'50'}}:null}}});await new Promise(resolve=>setImmediate(resolve));const older=calls.filter(call=>call.pathname==='/api/cloud-inventory/refresh/changes')[1];assert.equal(older.searchParams.get('before_sequence'),'9');assert.ok(p.get('refresh-events-list').innerHTML.includes('/removed'));assert.ok(p.get('refresh-events-list').innerHTML.includes('缓存观测 UTC：未知'));
});

test('directory refresh detail response is ignored after event filter changes',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));let resolveDetails;
 p.context.fetch=async url=>{const parsed=new URL(String(url),'http://localhost');if(parsed.pathname==='/api/cloud-inventory/refresh/runs')return{ok:true,json:async()=>({runs:[{run_id:'late-run'}]})};if(parsed.pathname==='/api/cloud-inventory/refresh/events'){const kind=parsed.searchParams.get('kind');return{ok:true,json:async()=>({run_id:'late-run',events:[kind==='started'?{sequence:2,run_id:'late-run',kind:'started',status:'running',phase:'listing',recorded_at_utc:'2026-10-30T00:00:00Z',pages_processed:0,items_processed:0,pending_count:1}:{sequence:1,run_id:'late-run',kind:'directory_published',status:'running',phase:'listing',recorded_at_utc:'2026-10-30T00:00:00Z',added_count:1,removed_count:0,changed_count:0,pages_processed:1,items_processed:1,pending_count:0}],has_more:false})}};if(parsed.pathname==='/api/cloud-inventory/refresh/changes')return await new Promise(resolve=>{resolveDetails=()=>resolve({ok:true,json:async()=>({run_id:'late-run',event_sequence:1,changes:[{sequence:1,kind:'added',new:{fs_id:1,path:'/stale-detail',name:'stale-detail',directory:false,size:1,provider_mtime:1,observed_at_utc:'2026-10-30T00:00:00Z'},recorded_at_utc:'2026-10-30T00:00:00Z',cloud_observed_at_utc:'2026-10-30T00:00:00Z'}],has_more:false})})});return{ok:true,json:async()=>({})}};
 await p.context.loadRefreshRuns();p.context.selectRefreshRun('late-run');await new Promise(resolve=>setImmediate(resolve));const click=p.listeners.get('refresh-events-list:click');click({target:{closest(selector){return selector==='[data-refresh-changes-toggle]'?{dataset:{refreshChangesToggle:'1'}}:null}}});await new Promise(resolve=>setImmediate(resolve));p.get('refresh-events-kind').value='started';p.get('refresh-events-kind').onchange();await new Promise(resolve=>setImmediate(resolve));resolveDetails();await new Promise(resolve=>setImmediate(resolve));assert.ok(p.get('refresh-events-list').innerHTML.includes('开始刷新'));assert.ok(!p.get('refresh-events-list').innerHTML.includes('stale-detail'));
});

test('directory refresh detail filters server results and preserves unfiltered category counts',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const calls=[];let detailRequest=0;
 p.context.fetch=async url=>{const parsed=new URL(String(url),'http://localhost');calls.push(parsed);if(parsed.pathname==='/api/cloud-inventory/refresh/runs')return{ok:true,json:async()=>({runs:[{run_id:'filter-run'}]})};if(parsed.pathname==='/api/cloud-inventory/refresh/events')return{ok:true,json:async()=>({run_id:'filter-run',events:[{sequence:50,run_id:'filter-run',kind:'directory_published',status:'running',phase:'listing',current_directory:'/folder',recorded_at_utc:'2026-10-30T01:00:00Z',cloud_observed_at_utc:'2026-10-30T00:59:00Z',added_count:2,removed_count:1,changed_count:1,pages_processed:1,items_processed:4,pending_count:0}],has_more:false})};if(parsed.pathname==='/api/cloud-inventory/refresh/changes'){detailRequest++;return{ok:true,json:async()=>({run_id:'filter-run',event_sequence:50,kind:detailRequest===1?'':'added',query:detailRequest===1?'':'report',changes:[],counts:{total:4,added:2,removed:1,metadata_changed:1,identity_changed:0},total_count:4,matched_count:detailRequest===1?4:1,next_before_sequence:detailRequest===1?9:0,has_more:detailRequest===1})}}return{ok:true,json:async()=>({})}};
 await p.context.loadRefreshRuns();p.context.selectRefreshRun('filter-run');await new Promise(resolve=>setImmediate(resolve));const click=p.listeners.get('refresh-events-list:click');click({target:{closest:s=>s==='[data-refresh-changes-toggle]'?{dataset:{refreshChangesToggle:'50'}}:null}});await new Promise(resolve=>setImmediate(resolve));
 assert.ok(p.get('refresh-events-list').innerHTML.includes('全部 4 条'));assert.ok(p.get('refresh-events-list').innerHTML.includes('新增 2 · 删除 1 · 元数据变化 1 · 身份变化 0'));
 p.get('refresh-change-kind-50').value='added';p.get('refresh-change-query-50').value='report';const submit=p.listeners.get('refresh-events-list:submit');submit({preventDefault(){},target:{closest:s=>s==='[data-refresh-change-filter]'?{dataset:{refreshChangeFilter:'50'}}:null}});await new Promise(resolve=>setImmediate(resolve));
 const filtered=calls.filter(c=>c.pathname==='/api/cloud-inventory/refresh/changes')[1];assert.equal(filtered.searchParams.get('kind'),'added');assert.equal(filtered.searchParams.get('q'),'report');assert.equal(filtered.searchParams.has('before_sequence'),false);assert.ok(p.get('refresh-events-list').innerHTML.includes('筛选命中 1 条'));
});

test('directory refresh detail export starts only on click and uses current filters',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const calls=[];const downloads=[];p.context.document.createElement=()=>({click(){downloads.push(this.download)},remove(){}});p.context.document.body={append(){}};p.context.URL.createObjectURL=()=>'/blob/changes';p.context.URL.revokeObjectURL=()=>{};
 p.context.fetch=async url=>{const parsed=new URL(String(url),'http://localhost');calls.push(parsed);if(parsed.pathname==='/api/cloud-inventory/refresh/events')return{ok:true,json:async()=>({run_id:'export-run',events:[{sequence:50,run_id:'export-run',kind:'directory_published',status:'running',phase:'listing',current_directory:'/folder',recorded_at_utc:'2026-10-30T01:00:00Z',added_count:1,removed_count:0,changed_count:0,pages_processed:1,items_processed:1,pending_count:0}],has_more:false})};if(parsed.pathname==='/api/cloud-inventory/refresh/changes')return{ok:true,json:async()=>({run_id:'export-run',event_sequence:50,kind:parsed.searchParams.get('kind')||'',query:parsed.searchParams.get('q')||'',changes:[],counts:{total:1,removed:1},matched_count:1,has_more:false})};if(parsed.pathname==='/api/cloud-inventory/refresh/changes/export')return{ok:true,headers:{get:()=> 'attachment; filename="cloud-refresh-changes.json"'},blob:async()=>({size:4})};return{ok:true,json:async()=>({runs:[{run_id:'export-run'}]})}};
 await p.context.loadRefreshRuns();p.context.selectRefreshRun('export-run');await new Promise(resolve=>setImmediate(resolve));const click=p.listeners.get('refresh-events-list:click');click({target:{closest:s=>s==='[data-refresh-changes-toggle]'?{dataset:{refreshChangesToggle:'50'}}:null}});await new Promise(resolve=>setImmediate(resolve));assert.equal(calls.some(c=>c.pathname.endsWith('/export')),false);p.get('refresh-change-kind-50').value='removed';p.get('refresh-change-query-50').value='old';p.listeners.get('refresh-events-list:submit')({preventDefault(){},target:{closest:s=>s==='[data-refresh-change-filter]'?{dataset:{refreshChangeFilter:'50'}}:null}});await new Promise(resolve=>setImmediate(resolve));
 const exportClick=click;exportClick({target:{closest:s=>s==='[data-refresh-changes-export]'?{dataset:{refreshChangesExport:'50'}}:null}});await new Promise(resolve=>setImmediate(resolve));const request=calls.find(c=>c.pathname==='/api/cloud-inventory/refresh/changes/export');assert.equal(request.searchParams.get('run_id'),'export-run');assert.equal(request.searchParams.get('event_sequence'),'50');assert.equal(request.searchParams.get('kind'),'removed');assert.equal(request.searchParams.get('q'),'old');assert.deepEqual(downloads,['cloud-refresh-changes.json']);
});

test('directory refresh detail copies paths and fs_id and reports clipboard failures',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const copied=[];p.context.navigator={clipboard:{writeText:async value=>{copied.push(value)}}};
 p.context.fetch=async url=>{const parsed=new URL(String(url),'http://localhost');if(parsed.pathname==='/api/cloud-inventory/refresh/events')return{ok:true,json:async()=>({run_id:'copy-run',events:[{sequence:50,run_id:'copy-run',kind:'directory_published',status:'running',phase:'listing',current_directory:'/folder',recorded_at_utc:'2026-10-30T01:00:00Z',added_count:1,removed_count:0,changed_count:0,pages_processed:1,items_processed:1,pending_count:0}],has_more:false})};if(parsed.pathname==='/api/cloud-inventory/refresh/changes')return{ok:true,json:async()=>({run_id:'copy-run',event_sequence:50,changes:[{sequence:9,kind:'added',new:{fs_id:987,path:'/safe/<folder>',name:'<folder>',directory:false,size:1,provider_mtime:1,observed_at_utc:'2026-10-30T00:59:00Z'}}],counts:{total:1,added:1},matched_count:1,has_more:false})};return{ok:true,json:async()=>({runs:[{run_id:'copy-run'}]})}};
 await p.context.loadRefreshRuns();p.context.selectRefreshRun('copy-run');await new Promise(resolve=>setImmediate(resolve));const click=p.listeners.get('refresh-events-list:click');click({target:{closest:s=>s==='[data-refresh-changes-toggle]'?{dataset:{refreshChangesToggle:'50'}}:null}});await new Promise(resolve=>setImmediate(resolve));
 click({target:{closest:s=>s==='[data-copy-refresh-value]'?{dataset:{copyRefreshValue:'/safe/<folder>',copyRefreshEvent:'50',copyRefreshLabel:'路径'}}:null}});await new Promise(resolve=>setImmediate(resolve));assert.deepEqual(copied,['/safe/<folder>']);assert.ok(p.get('refresh-events-list').innerHTML.includes('已复制'));
 p.context.navigator.clipboard.writeText=async()=>{throw new Error('denied')};click({target:{closest:s=>s==='[data-copy-refresh-value]'?{dataset:{copyRefreshValue:'987',copyRefreshEvent:'50',copyRefreshLabel:'fs_id'}}:null}});await new Promise(resolve=>setImmediate(resolve));assert.ok(p.get('refresh-events-list').innerHTML.includes('denied'));
});

test('directory refresh run evidence exports only the selected run on explicit click',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const id='a'.repeat(32);const calls=[];const downloads=[];
 p.context.document.createElement=()=>({click(){downloads.push(this.download)},remove(){}});p.context.document.body={append(){}};p.context.URL.createObjectURL=()=>'/blob/evidence';p.context.URL.revokeObjectURL=()=>{};
 vm.runInContext(`refreshRuns=[{run_id:'${id}'}]`,p.context);p.context.fetch=async(url)=>{calls.push(String(url));if(String(url).includes('/events?'))return{ok:true,json:async()=>({run_id:id,events:[],has_more:false})};return{ok:true,headers:{get:()=>`attachment; filename="cloud-refresh-${id}.json"`},blob:async()=>({size:100})}};
 assert.match(fs.readFileSync(path.join(root,'index_cloud.html'),'utf8'),/id="refresh-events-export" disabled/);
 p.get('refresh-events-kind').value='directory_published';p.get('refresh-events-kind').onchange();
 p.context.selectRefreshRun(id);await new Promise(resolve=>setImmediate(resolve));assert.equal(calls.some(url=>url.includes('/export')),false);
 await p.context.exportRefreshRunEvidence();
 assert.deepEqual(calls.filter(url=>url.includes('/export')),[`/api/cloud-inventory/refresh/runs/${id}/export`]);assert.deepEqual(downloads,[`cloud-refresh-${id}.json`]);assert.equal(p.get('refresh-evidence-status').textContent,'证据已开始下载');
});

test('directory refresh run evidence ignores a response after selecting another run',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const first='a'.repeat(32),second='b'.repeat(32);let resolveExport;const downloads=[];
 p.context.document.createElement=()=>({click(){downloads.push(this.download)},remove(){}});p.context.document.body={append(){}};p.context.URL.createObjectURL=()=>'/blob/evidence';p.context.URL.revokeObjectURL=()=>{};
 vm.runInContext(`refreshRuns=[{run_id:'${first}'},{run_id:'${second}'}]`,p.context);p.context.fetch=async(url)=>{if(String(url).includes('/events?'))return{ok:true,json:async()=>({run_id:second,events:[],has_more:false})};return new Promise(resolve=>{resolveExport=resolve})};
 p.context.selectRefreshRun(first);const exporting=p.context.exportRefreshRunEvidence();p.context.selectRefreshRun(second);
 resolveExport({ok:true,headers:{get:()=>`attachment; filename="cloud-refresh-${first}.json"`},blob:async()=>({size:1})});await exporting;
 assert.deepEqual(downloads,[]);assert.equal(p.get('refresh-evidence-status').textContent,'');
});

test('directory refresh run filters reset paging, preserve conditions and clear to all runs',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const calls=[];let runPage=0;
 p.context.fetch=async url=>{const parsed=new URL(String(url),'http://localhost');if(parsed.pathname==='/api/cloud-inventory/refresh/runs'){calls.push(parsed);runPage++;return{ok:true,json:async()=>({runs:[],next_before_sequence:runPage===1?99:runPage===2?40:0,has_more:runPage===1||runPage===2})}}if(parsed.pathname==='/api/cloud-inventory/refresh/events')return{ok:true,json:async()=>({events:[],has_more:false})};return{ok:true,json:async()=>({entries:[],total:0,metadata:{status:'complete'},status:'idle',authentication:{ready:true},directory:'/'})}};
 await p.context.loadRefreshRuns();assert.equal(calls[0].searchParams.has('before_sequence'),false);
 p.get('refresh-runs-status-filter').value='failed';p.get('refresh-runs-from-utc').value='2026-10-01';p.get('refresh-runs-through-utc').value='2026-10-30';p.get('refresh-runs-filter').onsubmit({preventDefault(){}});await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls[1].searchParams.has('before_sequence'),false);assert.equal(calls[1].searchParams.get('status'),'failed');assert.equal(calls[1].searchParams.get('started_from_utc'),'2026-10-01');assert.equal(calls[1].searchParams.get('started_through_utc'),'2026-10-30');
 p.get('refresh-runs-older').onclick();await new Promise(resolve=>setImmediate(resolve));assert.equal(calls[2].searchParams.get('before_sequence'),'40');assert.equal(calls[2].searchParams.get('status'),'failed');assert.equal(calls[2].searchParams.get('started_from_utc'),'2026-10-01');
 p.get('refresh-runs-clear').onclick();await new Promise(resolve=>setImmediate(resolve));assert.equal(calls[3].searchParams.has('before_sequence'),false);assert.equal(calls[3].searchParams.has('status'),false);assert.equal(calls[3].searchParams.has('started_from_utc'),false);assert.equal(p.get('refresh-runs-status-filter').value,'');
});

test('directory refresh run summaries export the committed filters only on click and discard stale responses',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const calls=[];const downloads=[];let resolveExport;let exportCount=0;
 p.context.document.createElement=()=>({click(){downloads.push(this.download)},remove(){}});p.context.document.body={append(){}};p.context.URL.createObjectURL=()=>'/blob/runs';p.context.URL.revokeObjectURL=()=>{};
 p.context.fetch=async(url)=>{const parsed=new URL(String(url),'http://localhost');calls.push(parsed);if(parsed.pathname==='/api/cloud-inventory/refresh/runs/export'){exportCount++;if(exportCount===1)return new Promise(resolve=>{resolveExport=resolve});return{ok:true,headers:{get:()=> 'attachment; filename="cloud-refresh-runs-20261030T120000Z.json"'},blob:async()=>({size:100})}}return{ok:true,json:async()=>({runs:[],has_more:false})}};
 assert.equal(calls.some(call=>call.pathname.endsWith('/export')),false,'summary export must be explicit');
 p.get('refresh-runs-status-filter').value='failed';p.get('refresh-runs-from-utc').value='2026-10-01';p.get('refresh-runs-through-utc').value='2026-10-30';p.get('refresh-runs-filter').onsubmit({preventDefault(){}});await new Promise(resolve=>setImmediate(resolve));
 const exporting=p.context.exportRefreshRunSummaries();await new Promise(resolve=>setImmediate(resolve));const request=calls.find(call=>call.pathname==='/api/cloud-inventory/refresh/runs/export');assert.equal(request.searchParams.get('status'),'failed');assert.equal(request.searchParams.get('started_from_utc'),'2026-10-01');assert.equal(request.searchParams.get('started_through_utc'),'2026-10-30');
 p.get('refresh-runs-clear').onclick();await new Promise(resolve=>setImmediate(resolve));resolveExport({ok:true,headers:{get:()=>`attachment; filename="cloud-refresh-runs-20261030T120000Z.json"`},blob:async()=>({size:100})});await exporting;
 assert.deepEqual(downloads,[],'response for the previous filter must not download');
 await p.context.exportRefreshRunSummaries();
 assert.equal(calls.filter(call=>call.pathname==='/api/cloud-inventory/refresh/runs/export').length,2);
});

test('cloud move picker selects a live directory and mutates only after explicit confirmation',async()=>{
	const p=page('index_cloud.html',[],{'directory':'/current','browse-source':'live'});
	const dialog=p.get('move-picker');dialog.showModal=()=>{dialog.open=true};dialog.close=()=>{dialog.open=false};
	const source={fs_id:701,path:'/current/file.bin',name:'file.bin',directory:false,size:12,provider_size:12,server_mtime:55,md5:'a'.repeat(32)};
	const calls=[];
	p.context.fetch=async(url,options={})=>{
		calls.push({url:String(url),options});
		if(String(url).startsWith('/api/cloud?dir=%2Fcurrent'))return{ok:true,json:async()=>({directory:'/current',entries:[{id:'702',fs_id:702,path:'/current/archive',name:'<archive>',directory:true}],total:1,offset:0})};
		if(String(url).startsWith('/api/cloud?dir=%2Farchive'))return{ok:true,json:async()=>({directory:'/archive',entries:[{id:'703',fs_id:703,path:'/archive/child',name:'child',directory:true}],total:1,offset:0})};
		if(String(url)==='/api/cloud/mutations')return{ok:true,json:async()=>({status:'confirmed',operation_id:'a'.repeat(32)})};
		if(String(url).startsWith('/api/cloud?dir=%2Fcurrent'))return{ok:true,json:async()=>({directory:'/current',entries:[],total:0,offset:0})};
		return{ok:true,json:async()=>({status:'idle',entries:[],groups:[]})};
	};
	p.context.openMovePicker(source);await new Promise(resolve=>setImmediate(resolve));
	assert.equal(dialog.open,true);
	assert.equal(p.get('move-picker-path').textContent,'/current');
	assert.ok(p.get('move-picker-entries').innerHTML.includes('&lt;archive&gt;'));
	assert.equal(calls.some(call=>call.url==='/api/cloud/mutations'),false,'opening the picker mutated the remote object');
	p.context.loadMovePickerDirectory('/archive',0);await new Promise(resolve=>setImmediate(resolve));
	assert.equal(p.get('move-picker-path').textContent,'/archive');
	assert.ok(p.get('move-picker-entries').innerHTML.includes('/archive/child'));
	assert.equal(calls.some(call=>call.url==='/api/cloud/mutations'),false,'directory navigation mutated the remote object');
	p.context.confirmMoveDestination();await new Promise(resolve=>setImmediate(resolve));
	const mutation=calls.find(call=>call.url==='/api/cloud/mutations');
	assert.ok(mutation,'explicit target confirmation did not call the existing mutation endpoint');
	assert.equal(JSON.parse(mutation.options.body).destination_directory,'/archive');
	assert.equal(JSON.parse(mutation.options.body).expected.fs_id,701);
});

test('cloud move picker ignores stale folder responses and cancel never mutates',async()=>{
	const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});
	const dialog=p.get('move-picker');dialog.showModal=()=>{dialog.open=true};dialog.close=()=>{dialog.open=false};
	let resolveSlow;const calls=[];
	p.context.fetch=url=>{calls.push(String(url));if(String(url).includes('dir=%2Fslow'))return new Promise(resolve=>{resolveSlow=resolve});return Promise.resolve({ok:true,json:async()=>({directory:'/fast',entries:[{id:'2',fs_id:2,path:'/fast/folder',name:'folder',directory:true}],total:1,offset:0})})};
	p.context.openMovePicker({fs_id:1,path:'/source.bin',name:'source.bin',directory:false,size:1,server_mtime:1});
	p.context.loadMovePickerDirectory('/slow',0);
	p.context.loadMovePickerDirectory('/fast',0);await new Promise(resolve=>setImmediate(resolve));
	resolveSlow({ok:true,json:async()=>({directory:'/slow',entries:[{id:'3',fs_id:3,path:'/slow/wrong',name:'wrong',directory:true}],total:1,offset:0})});
	await new Promise(resolve=>setImmediate(resolve));
	assert.equal(p.get('move-picker-path').textContent,'/fast');
	assert.ok(p.get('move-picker-entries').innerHTML.includes('/fast/folder'));
	assert.ok(!p.get('move-picker-entries').innerHTML.includes('/slow/wrong'));
	p.context.closeMovePicker();
	assert.equal(dialog.open,false);
	assert.equal(calls.some(url=>url==='/api/cloud/mutations'),false,'cancel mutated the remote object');
});

test('cloud move picker pages large directories and disables selection after a failed read',async()=>{
	const p=page('index_cloud.html',[],{'directory':'/many','browse-source':'live'});
	const dialog=p.get('move-picker');dialog.showModal=()=>{dialog.open=true};dialog.close=()=>{dialog.open=false};
	const calls=[];let fail=false;
	p.context.fetch=async url=>{calls.push(String(url));if(fail)throw new Error('目录读取失败');return{ok:true,json:async()=>({directory:'/many',entries:[{id:'5',fs_id:5,path:'/many/next',name:'next',directory:true}],total:150,offset:100,next_offset:null})}};
	p.context.openMovePicker({fs_id:1,path:'/source.bin',name:'source.bin',directory:false,size:1,server_mtime:1});await new Promise(resolve=>setImmediate(resolve));
	assert.ok(calls[0].includes('dir=%2Fmany&offset=0&limit=100'));
	p.context.loadMovePickerDirectory('/many',100);await new Promise(resolve=>setImmediate(resolve));
	assert.ok(calls.some(url=>url.includes('dir=%2Fmany&offset=100&limit=100')));
	assert.equal(p.get('move-picker-next').disabled,true);
	fail=true;p.context.loadMovePickerDirectory('/unavailable',0);await new Promise(resolve=>setImmediate(resolve));
	assert.equal(p.get('move-picker-confirm').disabled,true);
	assert.equal(p.get('move-picker-status').textContent,'目录读取失败');
	assert.equal(calls.some(url=>url==='/api/cloud/mutations'),false,'failed browse allowed a remote move');
});

test('cloud move picker reanchors its destination page when the saved first folder changes',async()=>{
	const p=page('index_cloud.html',[],{'directory':'/many','browse-source':'live'});
	const dialog=p.get('move-picker');dialog.showModal=()=>{dialog.open=true};dialog.close=()=>{dialog.open=false};
	const boundary={fs_id:101,path:'/many/folder-100',name:'folder-100',directory:true,size:0,provider_mtime:1000,content_md5:'',fingerprint:'b'.repeat(64)};
	const calls=[];
	p.context.fetch=async url=>{const parsed=new URL(String(url),'http://localhost');calls.push(parsed);if(parsed.pathname!=='/api/cloud')return{ok:true,json:async()=>({})};if(parsed.searchParams.get('offset')==='100')return{ok:true,json:async()=>({directory:'/many',entries:[{id:'1',fs_id:1,path:'/many/first',name:'first',directory:true}],offset:0,boundary_changed:true,next_offset:100,next_boundary:boundary})};return{ok:true,json:async()=>({directory:'/many',entries:[{id:'1',fs_id:1,path:'/many/first',name:'first',directory:true}],offset:0,next_offset:100,next_boundary:boundary})};};
	p.context.openMovePicker({fs_id:5,path:'/source.bin',name:'source.bin',directory:false,size:1,server_mtime:1});await new Promise(resolve=>setImmediate(resolve));
	p.context.loadMovePickerDirectory('/many',100);await new Promise(resolve=>setImmediate(resolve));
	const cloudCalls=calls.filter(call=>call.pathname==='/api/cloud');
	assert.deepEqual(JSON.parse(cloudCalls[1].searchParams.get('expected_boundary')),{fingerprint:boundary.fingerprint});
	assert.ok(!cloudCalls[1].searchParams.get('expected_boundary').includes(boundary.path));
	assert.equal(p.get('move-picker-page').textContent,'第 1 项起');
	assert.match(p.get('move-picker-status').textContent,/列表变化.*首页/);
});

test('duplicate preview lets the user change the keeper without a remote request and resets on refresh',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});
 const original={fs_id:81,path:'/archive/a.bin',name:'a.bin',size:12,provider_mtime:11,content_md5:'a'.repeat(32),content_verified_at_utc:'2026-10-01T00:00:00Z'};
 const chosen={fs_id:82,path:'/wanted/<b>.bin',name:'<b>.bin',size:12,provider_mtime:12,content_md5:'b'.repeat(32),content_verified_at_utc:'2026-10-02T00:00:00Z'};
 const groups=[{sha256:'c'.repeat(64),size:12,keep:original,delete:[chosen]}];
 let calls=[];
 p.context.fetch=async(url,options={})=>{calls.push({url:String(url),options});return{ok:true,json:async()=>JSON.parse(JSON.stringify({groups,total:groups.length,delete_count:groups.reduce((n,g)=>n+g.delete.length,0),offset:0,limit:25,next_offset:null}))}};
 await vm.runInContext('duplicatePreview()',p.context);
 calls=[];
 assert.match(fs.readFileSync(path.join(root,'index_cloud.html'),'utf8'),/此选择仅对当前预览有效/);
 const html=fs.readFileSync(path.join(root,'index_cloud.html'),'utf8');
 assert.match(html,/data-keep/);
 assert.match(html,/querySelectorAll\('\[data-keep-group\]'\)[\s\S]*setDuplicateKeeper/);
 vm.runInContext('setDuplicateKeeper(0,0)',p.context);
 assert.equal(calls.length,0,'changing a keeper should not call any API');
 assert.ok(p.get('duplicate-groups').innerHTML.includes('/wanted/&lt;b&gt;.bin'));
 const selected=vm.runInContext('duplicateGroups[0]',p.context);
 assert.equal(selected.keep.fs_id,chosen.fs_id);
 assert.deepEqual(Array.from(selected.delete,x=>x.fs_id),[original.fs_id]);
 assert.equal(vm.runInContext('duplicateDeleteCount()',p.context),1);
 await vm.runInContext('duplicatePreview()',p.context);
 const refreshed=vm.runInContext('duplicateGroups[0]',p.context);
 assert.equal(refreshed.keep.fs_id,original.fs_id,'refresh should restore the server-provided default keeper');
 assert.deepEqual(Array.from(refreshed.delete,x=>x.fs_id),[chosen.fs_id]);
});

test('duplicate batch deletion walks changing first pages and preserves hash verification',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});
 const groups=Array.from({length:3},(_,i)=>({sha256:String(i+1).repeat(64),size:20,keep:{fs_id:91+i*2,path:`/g${i}/keep.bin`,name:'keep.bin',size:20,provider_mtime:11,content_md5:'a'.repeat(32)},delete:[{fs_id:92+i*2,path:`/g${i}/delete.bin`,name:'delete.bin',size:20,provider_mtime:12,content_md5:'b'.repeat(32)}]}));
 const calls=[];let previewCount=0;
 p.context.confirm=()=>true;
 p.context.fetch=async(url,options={})=>{
  calls.push({url:String(url),options});
  if(String(url).startsWith('/api/cloud-cleanup/duplicates?')){previewCount++;const current=JSON.parse(JSON.stringify(groups));return{ok:true,json:async()=>({groups:current,total:current.length,delete_count:current.length,offset:0,limit:25,next_offset:null})}}
  if(String(url)==='/api/cloud/mutations'){const payload=JSON.parse(options.body);const index=groups.findIndex(g=>[g.keep,...g.delete].some(x=>x.fs_id===payload.expected.fs_id));assert.notEqual(index,-1,`fs_id ${payload.expected.fs_id}; groups ${JSON.stringify(groups)}`);groups.splice(index,1);return{ok:true,json:async()=>({status:'confirmed'})}}
  if(String(url).startsWith('/api/cloud?dir='))return{ok:true,json:async()=>({directory:'/',entries:[],total:0,offset:0,next_offset:null})};
  if(String(url).startsWith('/api/cloud/size'))return{ok:true,json:async()=>({directory:'/',size:0,complete:true,files:0,directories:0})};
  return{ok:true,json:async()=>({})};
 };
 await vm.runInContext('duplicatePreview()',p.context);
 vm.runInContext('setDuplicateKeeper(0,0)',p.context);
 await vm.runInContext('deleteAllDuplicates()',p.context);
 const mutations=calls.filter(call=>call.url==='/api/cloud/mutations');
 assert.equal(mutations.length,3,p.get('duplicate-progress').textContent);
 assert.deepEqual(mutations.map(call=>JSON.parse(call.options.body).expected.path),['/g0/keep.bin','/g1/delete.bin','/g2/delete.bin']);
 assert.deepEqual(mutations.map(call=>JSON.parse(call.options.body).expected_content_sha256),['1'.repeat(64),'2'.repeat(64),'3'.repeat(64)]);
 assert.ok(calls.filter(call=>call.url.startsWith('/api/cloud-cleanup/duplicates?')).every(call=>call.url.includes('offset=0&limit=25')),'batch traversal should re-read from page zero after groups shrink');
 assert.ok(previewCount>=5,'batch should refresh the first page after processing each group');
});

test('duplicate batch deletion stops after an unknown result',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});
 const group={sha256:'e'.repeat(64),size:20,keep:{fs_id:101,path:'/keep.bin',name:'keep.bin',size:20,provider_mtime:1},delete:[{fs_id:102,path:'/delete.bin',name:'delete.bin',size:20,provider_mtime:2}]};
 const calls=[];p.context.confirm=()=>true;
 p.context.fetch=async(url,options={})=>{calls.push({url:String(url),options});if(String(url).startsWith('/api/cloud-cleanup/duplicates?'))return{ok:true,json:async()=>({groups:[group],total:1,delete_count:1,offset:0,limit:25,next_offset:null})};if(String(url)==='/api/cloud/mutations')return{ok:true,json:async()=>({status:'unknown'})};return{ok:true,json:async()=>({})}};
 await vm.runInContext('duplicatePreview()',p.context);
 await vm.runInContext('deleteAllDuplicates()',p.context);
 assert.equal(calls.filter(x=>x.url==='/api/cloud/mutations').length,1);
 assert.match(p.get('duplicate-progress').textContent,/已停止后续操作/);
});

test('duplicate batch deletion stops when the remaining preview count drifts',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});
 const groups=[{sha256:'f'.repeat(64),size:20,keep:{fs_id:111,path:'/keep.bin',name:'keep.bin',size:20,provider_mtime:1},delete:[{fs_id:112,path:'/delete.bin',name:'delete.bin',size:20,provider_mtime:2}]}];
 let mutations=0;p.context.confirm=()=>true;
 p.context.fetch=async(url,options={})=>{if(String(url).startsWith('/api/cloud-cleanup/duplicates?'))return{ok:true,json:async()=>({groups:JSON.parse(JSON.stringify(groups)),total:groups.length,delete_count:groups.reduce((n,g)=>n+g.delete.length,0),offset:0,limit:25,next_offset:null})};if(String(url)==='/api/cloud/mutations'){mutations++;groups.splice(0,groups.length,{sha256:'a'.repeat(64),size:20,keep:{fs_id:121,path:'/new-keep.bin',name:'new-keep.bin',size:20},delete:[{fs_id:122,path:'/new-delete.bin',name:'new-delete.bin',size:20}]});return{ok:true,json:async()=>({status:'confirmed'})}}if(String(url).startsWith('/api/cloud?dir='))return{ok:true,json:async()=>({directory:'/',entries:[]})};if(String(url).startsWith('/api/cloud/size'))return{ok:true,json:async()=>({complete:true})};return{ok:true,json:async()=>({})}};
 await vm.runInContext('duplicatePreview()',p.context);
 await vm.runInContext('deleteAllDuplicates()',p.context);
 assert.equal(mutations,1);
 assert.match(p.get('duplicate-progress').textContent,/剩余预览计数发生变化/);
});

test('duplicate preview requests bounded server pages and retains keeper overrides across pages',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});
 const makeGroups=count=>Array.from({length:count},(_,i)=>({sha256:String(i+1).padStart(64,'0'),size:10,keep:{fs_id:i*2+1,path:`/g${i}/keep`,name:'keep',size:10},delete:[{fs_id:i*2+2,path:`/g${i}/delete`,name:'delete',size:10}]}));
 let responseGroups=makeGroups(51);const calls=[];
 p.context.fetch=async url=>{calls.push(String(url));const parsed=new URL(String(url),'http://local');const offset=Number(parsed.searchParams.get('offset'));const limit=Number(parsed.searchParams.get('limit'));return{ok:true,json:async()=>JSON.parse(JSON.stringify({groups:responseGroups.slice(offset,offset+limit),total:responseGroups.length,delete_count:responseGroups.length,offset,limit,next_offset:offset+limit<responseGroups.length?offset+limit:null}))}};
 await vm.runInContext('duplicatePreview()',p.context);
 const renderedCount=()=>[...p.get('duplicate-groups').innerHTML.matchAll(/class="entry duplicate-group"/g)].length;
 assert.equal(renderedCount(),25);
 assert.match(p.get('duplicate-page-summary').textContent,/第 1–25 组，共 51 组/);
 assert.equal(p.get('duplicate-prev').disabled,true);
 assert.equal(p.get('duplicate-next').disabled,false);
 assert.equal(vm.runInContext('duplicateDeleteCount()',p.context),51);
 vm.runInContext('setDuplicateKeeper(0,0)',p.context);
 const requestCount=calls.length;calls.length=0;
 await p.get('duplicate-next').onclick();
 assert.equal(calls.length,1,'paging should request one bounded API page');
 assert.match(calls[0],/offset=25&limit=25/);
 assert.equal(renderedCount(),25);
 assert.match(p.get('duplicate-page-summary').textContent,/第 26–50 组，共 51 组/);
 assert.equal(vm.runInContext('duplicateGroups[0].keep.path',p.context),'/g25/keep');
 vm.runInContext('setDuplicateKeeper(0,0)',p.context);
 assert.equal(vm.runInContext('duplicateGroups[0].keep.path',p.context),'/g25/delete','page keeper selection should apply to its current group');
 responseGroups[25].delete[0]={...responseGroups[25].delete[0],fs_id:9999,path:'/g25/replaced'};
 await p.get('duplicate-next').onclick();
 assert.equal(renderedCount(),1);
 assert.match(p.get('duplicate-page-summary').textContent,/第 51–51 组，共 51 组/);
 assert.equal(p.get('duplicate-next').disabled,true);
 await p.get('duplicate-prev').onclick();
 assert.equal(vm.runInContext('duplicateGroups[0].keep.path',p.context),'/g25/keep','a stale keeper override should not survive when its identity leaves the group');
 await p.get('duplicate-prev').onclick();
 assert.equal(vm.runInContext('duplicateGroups[0].keep.path',p.context),'/g0/delete','off-page keeper selection was lost');
 assert.equal(calls.length,4);
 responseGroups=makeGroups(26);
 await vm.runInContext('duplicatePreview()',p.context);
 assert.equal(renderedCount(),25);
 assert.equal(p.get('duplicate-next').disabled,false);
 await p.get('duplicate-next').onclick();
 assert.equal(renderedCount(),1);
 assert.match(p.get('duplicate-page-summary').textContent,/第 26–26 组，共 26 组/);
 await p.get('duplicate-prev').onclick();
 responseGroups=makeGroups(25);
 await vm.runInContext('duplicatePreview()',p.context);
 assert.equal(renderedCount(),25);
 assert.equal(p.get('duplicate-prev').disabled,true);
 assert.equal(p.get('duplicate-next').disabled,true);
 assert.equal(vm.runInContext('duplicateGroups[0].keep.path',p.context),'/g0/keep','a fresh preview should reset temporary keeper choices');
 responseGroups=[];
 await vm.runInContext('duplicatePreview()',p.context);
 assert.equal(renderedCount(),0);
 assert.match(p.get('duplicate-page-summary').textContent,/共 0 组/);
 assert.equal(p.get('duplicate-prev').disabled,true);
 assert.equal(p.get('duplicate-next').disabled,true);
 assert.ok(requestCount>0);
});

test('duplicate scan events load only on request, append older pages and render escaped chronological evidence',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const calls=[];
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value==='/api/cloud-cleanup/duplicate-scan/events?limit=20')return{ok:true,json:async()=>({events:[{sequence:10,run_id:'run-new',status:'complete',phase:'complete',recorded_at_utc:'2026-10-30T02:00:00Z',evidence:{last_error:'<img src=x>'}},{sequence:9,run_id:'run-old',status:'running',phase:'hashing',recorded_at_utc:'2026-10-30T01:00:00Z',evidence:{processed_files:3}}],next_before_sequence:9})};if(value.includes('before_sequence=9'))return{ok:true,json:async()=>({events:[{sequence:8,run_id:'run-old',status:'running',phase:'backoff',recorded_at_utc:'2026-10-30T00:00:00Z',evidence:{retry_delay_ms:1000}}],next_before_sequence:null})};throw new Error(`unexpected URL ${value}`)};
 const eventCalls=()=>calls.filter(url=>url.includes('/api/cloud-cleanup/duplicate-scan/events'));
 assert.equal(eventCalls().length,0,'event history should not be fetched automatically');
 await p.get('duplicate-events-load').onclick();
 assert.equal(eventCalls().length,1);assert.match(eventCalls()[0],/duplicate-scan\/events\?limit=20/);
 assert.equal(p.get('duplicate-events-older').disabled,false);
 await p.get('duplicate-events-older').onclick();
 assert.equal(eventCalls().length,2);assert.match(eventCalls()[1],/before_sequence=9/);
 const html=p.get('duplicate-events-list').innerHTML;
 assert.ok(html.indexOf('#8')<html.indexOf('#9')&&html.indexOf('#9')<html.indexOf('#10'),'timeline should render oldest to newest');
 assert.ok(html.includes('&lt;img src=x&gt;'),'event evidence should be escaped');
 assert.ok(!html.includes('<img src=x>'));
 assert.equal(calls.some(url=>url.includes('/api/cloud/mutations')||url.includes('duplicate-scan?')),false,'history reads must not mutate or start a scan');
});

test('duplicate scan event history shows empty/error states and can retry',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});let calls=0;
 p.context.fetch=async()=>{calls++;if(calls===1)throw new Error('offline');return{ok:true,json:async()=>({events:[],next_before_sequence:null})}};
 await p.get('duplicate-events-load').onclick();
 assert.match(p.get('duplicate-events-status').textContent,/读取失败/);
 assert.notEqual(p.get('duplicate-events-load').disabled,true);
 await p.get('duplicate-events-load').onclick();
 assert.match(p.get('duplicate-events-list').innerHTML,/暂无扫描事件/);
 assert.match(p.get('duplicate-events-status').textContent,/已读取 0 条/);
});

test('duplicate scan event history ignores stale responses',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const pending=[];
 p.context.fetch=()=>new Promise(resolve=>pending.push(resolve));
 const first=p.get('duplicate-events-load').onclick();
 const second=p.get('duplicate-events-load').onclick();
 pending[1]({ok:true,json:async()=>({events:[{sequence:22,run_id:'new',status:'paused',phase:'paused',recorded_at_utc:'2026-10-30T02:00:00Z',evidence:{}}],next_before_sequence:null})});
 await second;
 pending[0]({ok:true,json:async()=>({events:[{sequence:11,run_id:'old',status:'running',phase:'hashing',recorded_at_utc:'2026-10-30T01:00:00Z',evidence:{}}],next_before_sequence:null})});
 await first;
 assert.ok(p.get('duplicate-events-list').innerHTML.includes('#22'));
 assert.ok(!p.get('duplicate-events-list').innerHTML.includes('#11'));
});

test('duplicate scan runs are loaded on demand, filtered, selected by run, and paged independently',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const calls=[];
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value.startsWith('/api/cloud-cleanup/duplicate-scan/runs?'))return{ok:true,json:async()=>({runs:[{start_sequence:50,run_id:'a'.repeat(32),status:'complete',phase:'complete',started_at_utc:'2026-10-30T02:00:00Z',updated_at_utc:'2026-10-30T02:01:00Z',processed_files:4,total_files:4,processed_bytes:40,total_bytes:40}],next_before_start_sequence:30,has_more:true})};if(value.includes('/runs/')&&value.endsWith('/events?limit=20'))return{ok:true,json:async()=>({run_id:'a'.repeat(32),events:[{sequence:50,status:'complete',phase:'complete',recorded_at_utc:'2026-10-30T02:01:00Z',evidence:{last_error:'<svg onload=x>'}}],next_before_sequence:40,has_more:true})};if(value.includes('before_sequence=40'))return{ok:true,json:async()=>({run_id:'a'.repeat(32),events:[{sequence:39,status:'running',phase:'hashing',recorded_at_utc:'2026-10-30T02:00:00Z',evidence:{processed_files:3}}],next_before_sequence:null,has_more:false})};throw new Error(`unexpected URL ${value}`)};
 assert.equal(calls.some(url=>url.includes('/runs?')),false,'history does not fetch until requested');
 await p.get('duplicate-runs-load').onclick();assert.match(calls.find(url=>url.includes('/runs?')),/limit=20/);assert.equal(p.get('duplicate-runs-older').disabled,false);
 p.get('duplicate-runs-status-filter').value='complete';p.get('duplicate-runs-from-utc').value='2026-10-30';await p.get('duplicate-runs-filter').onsubmit({preventDefault(){}});const filtered=calls.filter(url=>url.includes('/runs?')).at(-1);assert.match(filtered,/status=complete/);assert.match(filtered,/started_from_utc=2026-10-30/);
 await vm.runInContext(`loadDuplicateScanRunEvents('${'a'.repeat(32)}')`,p.context);await p.get('duplicate-run-events-older').onclick();const html=p.get('duplicate-run-events-list').innerHTML;assert.ok(html.indexOf('#39')<html.indexOf('#50'));assert.ok(html.includes('&lt;svg onload=x&gt;'));assert.ok(!html.includes('<svg'));
 assert.ok(calls.some(url=>url.includes('/runs/')&&url.includes('/events?limit=20')));assert.ok(calls.some(url=>url.includes('before_sequence=40')));assert.equal(p.get('duplicate-run-export').disabled,false);
});

test('duplicate scan run selection ignores stale event responses after a newer run is selected',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const pending=[];p.context.fetch=url=>String(url).includes('/runs/')?new Promise(resolve=>pending.push(resolve)):Promise.resolve({ok:true,json:async()=>({events:[],next_before_sequence:null,has_more:false})});
 const old=vm.runInContext(`loadDuplicateScanRunEvents('${'1'.repeat(32)}')`,p.context),fresh=vm.runInContext(`loadDuplicateScanRunEvents('${'2'.repeat(32)}')`,p.context);
 pending[1]({ok:true,json:async()=>({events:[{sequence:22,status:'paused',phase:'paused',recorded_at_utc:'2026-10-30T02:00:00Z',evidence:{}}],next_before_sequence:null,has_more:false})});await fresh;
 pending[0]({ok:true,json:async()=>({events:[{sequence:11,status:'running',phase:'hashing',recorded_at_utc:'2026-10-30T01:00:00Z',evidence:{}}],next_before_sequence:null,has_more:false})});await old;
 assert.ok(p.get('duplicate-run-events-list').innerHTML.includes('#22'));assert.ok(!p.get('duplicate-run-events-list').innerHTML.includes('#11'));
});

test('duplicate scan history exports filtered summaries and selected-run evidence only on explicit clicks',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});const calls=[],downloads=[];
 p.context.fetch=async (url)=>{const value=String(url);calls.push(value);if(value.includes('/runs/')&&value.endsWith('/export'))return{ok:true,blob:async()=>({}),headers:{get:()=>`attachment; filename="duplicate-scan-${'a'.repeat(32)}.json"`}};if(value.includes('/runs/export'))return{ok:true,blob:async()=>({}),headers:{get:()=>`attachment; filename="duplicate-scan-runs.json"`}};throw new Error(`unexpected URL ${value}`)};
 p.context.URL={createObjectURL:()=>`blob:${downloads.length}`,revokeObjectURL(){}};p.context.document.createElement=()=>({click(){downloads.push(this.download)},remove(){}});p.context.document.body={append(){}};
 assert.equal(calls.length,0,'exports are not automatic');p.get('duplicate-runs-status-filter').value='complete';p.get('duplicate-runs-from-utc').value='2026-10-30';p.get('duplicate-runs-through-utc').value='2026-10-31';await p.get('duplicate-runs-filter').onsubmit({preventDefault(){}});
 await p.get('duplicate-runs-export').onclick();assert.match(calls.find(url=>url.includes('/runs/export')),/runs\/export\?status=complete&started_from_utc=2026-10-30&started_through_utc=2026-10-31/);assert.equal(downloads[0],'duplicate-scan-runs.json');
 await vm.runInContext(`loadDuplicateScanRunEvents('${'a'.repeat(32)}')`,p.context);await p.get('duplicate-run-export').onclick();assert.ok(calls.some(url=>url.endsWith(`/runs/${'a'.repeat(32)}/export`)));assert.equal(downloads.length,2);assert.equal(p.get('duplicate-run-export-status').textContent,'导出已开始下载');
});

test('duplicate scan progress shows verified bytes and renders zero-byte work without invalid percentages',async()=>{
	let state={status:'running',phase:'hashing',processed_files:3,total_files:4,processed_bytes:1024,total_bytes:2048};
	const p=page('index_cloud.html',[],{'directory':'/'},async url=>String(url)==='/api/cloud-cleanup/duplicate-scan'?{ok:true,json:async()=>state}:{ok:true,json:async()=>({events:[],groups:[]})});
	await waitForPollIdle(p,'duplicateStatusInFlight');
	await vm.runInContext('duplicateStatus()',p.context);
	assert.match(p.get('duplicate-summary').textContent,/3 \/ 4 文件/);
	assert.match(p.get('duplicate-summary').textContent,/1\.00 KiB \/ 2\.00 KiB/);
	assert.match(p.get('duplicate-progress').textContent,/已校验字节：1\.00 KiB \/ 2\.00 KiB/);
	state={status:'running',phase:'hashing',processed_files:0,total_files:2,processed_bytes:0,total_bytes:0};
	await vm.runInContext('duplicateStatus()',p.context);
	assert.match(p.get('duplicate-progress').textContent,/已校验字节：0 B \/ 0 B/);
	assert.doesNotMatch(p.get('duplicate-progress').textContent,/NaN|Infinity/);
	state={status:'running',phase:'hashing',processed_files:1,total_files:2,processed_bytes:100,total_bytes:300,current_path:'/large/<img>.bin',current_bytes:50,current_size:200};
	await vm.runInContext('duplicateStatus()',p.context);
	assert.match(p.get('duplicate-progress').textContent,/当前文件：\/large\/<img>\.bin（50 B \/ 200 B）/);
	assert.match(p.get('duplicate-summary').textContent,/150 B \/ 300 B/,'overall progress should include in-file bytes');
	assert.ok(!p.get('duplicate-progress').innerHTML.includes('<img>'),'cloud path must be rendered as text, not HTML');
});

test('duplicate scan retry status distinguishes current deadline from the last actual wait',async()=>{
	let state={status:'failed',phase:'failed',retry_delay_ms:10000};
	const p=page('index_cloud.html',[],{'directory':'/'},async()=>({ok:true,json:async()=>state}));
	await waitForPollIdle(p,'duplicateStatusInFlight');
	await vm.runInContext('duplicateStatus()',p.context);
	assert.match(p.get('duplicate-progress').textContent,/最近实际等待时长：10000 ms/);
	assert.match(p.get('duplicate-progress').textContent,/当前重试计划：无/);
	assert.doesNotMatch(p.get('duplicate-progress').textContent,/下次重试：2026/);
	state={status:'running',phase:'backoff',retry_delay_ms:30000,retry_at_utc:new Date(Date.now()+30000).toISOString()};
	await vm.runInContext('duplicateStatus()',p.context);
	assert.match(p.get('duplicate-progress').textContent,/最近实际等待时长：30000 ms/);
	assert.match(p.get('duplicate-progress').textContent,/本机重试计划 UTC：/);
});

test('duplicate scan inventory drift shows refresh guidance and durable old/new identity evidence',async()=>{
	const state={status:'paused',phase:'inventory_changed',processed_files:4,total_files:8,processed_bytes:40,total_bytes:80,last_error:'检测到网盘文件信息变化；请刷新目录 /old、/new 后恢复重复扫描。'};
	const p=page('index_cloud.html',[],{'directory':'/'},async url=>String(url).includes('/duplicate-scan/events')?{ok:true,json:async()=>({events:[{sequence:12,run_id:'drift-run',status:'paused',phase:'inventory_changed',recorded_at_utc:'2026-10-31T01:02:03Z',evidence:{change_kind:'cloud_identity_changed',old_identity:{path:'/old/a.bin'},observed_identity:{path:'/new/a.bin'},provider_observed_at_utc:'2026-10-31T01:02:02Z',local_recorded_at_utc:'2026-10-31T01:02:03Z'}}],next_before_sequence:null})}:{ok:true,json:async()=>state});
	await waitForPollIdle(p,'duplicateStatusInFlight');
	await vm.runInContext('duplicateStatus()',p.context);
	assert.match(p.get('duplicate-progress').textContent,/网盘对象变化，等待缓存目录刷新/);
	assert.match(p.get('duplicate-progress').textContent,/\/old、\/new/);
	await vm.runInContext('loadDuplicateScanEvents(false)',p.context);
	assert.match(p.get('duplicate-events-list').innerHTML,/\/old\/a\.bin/);
	assert.match(p.get('duplicate-events-list').innerHTML,/\/new\/a\.bin/);
	assert.match(p.get('duplicate-events-list').innerHTML,/2026-10-31T01:02:02Z/);
	assert.match(p.get('duplicate-events-list').innerHTML,/2026-10-31T01:02:03Z/);
	assert.ok(!p.get('duplicate-events-list').innerHTML.includes('<img>'));
});

test('cloud browser switches to local cache explicitly, labels coverage and keeps cache read-only',async()=>{
	const html=fs.readFileSync(path.join(root,'index_cloud.html'),'utf8');
	assert.match(html,/<select[^>]*id="browse-source"[\s\S]*?<option value="live" selected/);
	const p=page('index_cloud.html',[],{'directory':'/cached','browse-source':'cache'});
	let calls=[];
	let cached={directory:'/cached',entries:[{fs_id:701,path:'/cached/file.txt',name:'file.txt',directory:false,size:5,provider_mtime:123},{fs_id:702,path:'/cached/full',name:'full',directory:true,size:0,directory_size_bytes:42,directory_size_complete:true},{fs_id:703,path:'/cached/partial',name:'partial',directory:true,size:0,directory_size_bytes:17,directory_size_complete:false}],metadata:{path:'/cached',status:'stale',observed_at_utc:'2026-10-30T00:00:00Z'},total:3,offset:0,next_offset:null};
	p.context.fetch=async url=>{
		calls.push(url);
		if(url.startsWith('/api/cloud-inventory?'))return{ok:true,json:async()=>cached};
		if(url.startsWith('/api/cloud-inventory/size?'))return{ok:true,json:async()=>({path:'/cached',bytes:5,files:1,directories:0,complete:false,observed_at_utc:'2026-10-30T00:00:00Z'})};
		if(url.startsWith('/api/cloud?'))return{ok:true,json:async()=>({directory:'/cached',entries:[{id:'701',fs_id:701,path:'/cached/file.txt',name:'file.txt',directory:false,size:5,provider_size:5,server_mtime:123}],total:1,offset:0})};
		return{ok:true,json:async()=>({status:'idle',entries:[],groups:[]})};
	};
	await vm.runInContext('browse()',p.context);
	assert.ok(calls.some(url=>url.startsWith('/api/cloud-inventory?dir=')));
	assert.ok(!calls.some(url=>url.startsWith('/api/cloud?')),'cached mode requested live provider data');
	assert.match(p.get('browse-source-status').textContent,/缓存不完整/);
	assert.match(p.get('browse-source-status').textContent,/2026-10-30T00:00:00Z/);
	assert.ok(!p.get('entries').innerHTML.includes('data-rename='));
	assert.ok(!p.get('entries').innerHTML.includes('data-move='));
	assert.ok(!p.get('entries').innerHTML.includes('data-delete='));
	assert.match(p.get('entries').innerHTML,/42 B/,'cached directory should display its locally aggregated size');
	assert.match(p.get('entries').innerHTML,/未统计/,'incomplete cached directory size must not look complete');
	cached={directory:'/cached',entries:[{fs_id:701,path:'/cached/file.txt',name:'file.txt',directory:false,size:5}],metadata:{path:'/cached',status:'failed',observed_at_utc:null},total:1,offset:0,next_offset:null};
	await vm.runInContext('browse()',p.context);
	assert.match(p.get('browse-source-status').textContent,/缓存不完整（上次扫描失败）/);
	assert.doesNotMatch(p.get('browse-source-status').textContent,/尚无缓存记录/);
	assert.match(p.get('entries').innerHTML,/file\.txt/,'failed coverage hid previously committed cache entries');
	assert.equal(p.get('mkdir').disabled,true,'cached mode left a remote create control enabled');
	assert.equal(p.get('empty-cleanup-card').hidden,true,'cached mode left empty-directory cleanup available');
	assert.equal(p.get('duplicate-cleanup-card').hidden,true,'cached mode left duplicate cleanup available');

	cached={directory:'/cached',entries:[],metadata:{path:'/cached',status:'complete',observed_at_utc:'2026-10-30T00:01:00Z'},total:0,offset:0,next_offset:null};
	await vm.runInContext('browse()',p.context);
	assert.match(p.get('entries').innerHTML,/缓存目录为空/);
	assert.match(p.get('browse-source-status').textContent,/此轮遍历完成（非远端原子快照）/);
	assert.match(p.get('browse-source-status').textContent,/2026-10-30T00:01:00Z/);

	cached={directory:'/cached',entries:[],metadata:{path:'/cached',status:'',observed_at_utc:null},total:0,offset:0,next_offset:null};
	await vm.runInContext('browse()',p.context);
	assert.match(p.get('entries').innerHTML,/尚无缓存记录/);
	assert.ok(!p.get('entries').innerHTML.includes('此目录为空'));

	const previousCalls=calls.length;
	vm.runInContext('offset=40',p.context);
	p.get('browse-source').value='live';
	assert.equal(typeof p.get('browse-source').onchange,'function');
	p.get('browse-source').onchange();
	await new Promise(resolve=>setImmediate(resolve));
	assert.ok(calls.slice(previousCalls).some(url=>url.startsWith('/api/cloud?dir=%2Fcached&offset=0')));
	assert.ok(p.get('entries').innerHTML.includes('data-rename="701"'));
	assert.equal(p.get('mkdir').disabled,false,'live mode did not restore the remote create control');
	assert.equal(p.get('empty-cleanup-card').hidden,false,'live mode did not restore empty-directory cleanup');
	assert.equal(p.get('duplicate-cleanup-card').hidden,false,'live mode did not restore duplicate cleanup');
});

test('incomplete cloud directories load only on request, page by generation and reuse existing explicit refresh flow',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live','incomplete-directories-reason':'','incomplete-directories-query':''});await new Promise(resolve=>setImmediate(resolve));const calls=[];let releaseStale;
 const entry={path:'/cache/<stale>',status:'stale',reason:'stale',cached_directory:true,observed_at_utc:'2026-11-02T03:00:00Z',last_run_id:'run-1'};
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value.startsWith('/api/cloud-inventory/incomplete-directories')){if(value.includes('before_path='))return await new Promise(resolve=>{releaseStale=()=>resolve({ok:true,json:async()=>({entries:[{...entry,path:'/cache/next'}],total:2,cache_generation:4,has_more:false})})});if(value.includes('q=different'))return{ok:true,json:async()=>({entries:[{...entry,path:'/new-query'}],total:1,cache_generation:4,has_more:false})};return{ok:true,json:async()=>({entries:[entry],total:2,cache_generation:4,next_before_path:entry.path,has_more:true})}}if(value.startsWith('/api/cloud-inventory?'))return{ok:true,json:async()=>({entries:[],total:0,metadata:{status:'stale',cache_generation:4},cache_generation:4})};return{ok:true,json:async()=>({status:'complete',inventory_coverage:{status:'incomplete',incomplete_directories:1,incomplete_reasons:{}}})}};
 assert.equal(calls.some(url=>url.includes('/incomplete-directories')),false);await p.get('incomplete-directories-load').onclick();assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,1);assert.ok(calls.every(url=>!url.startsWith('/api/cloud?')));assert.match(p.get('incomplete-directories-list').innerHTML,/&lt;stale&gt;/);assert.doesNotMatch(p.get('incomplete-directories-list').innerHTML,/<stale>/);assert.equal(!!p.get('incomplete-directories-next').disabled,false);
 p.get('incomplete-directories-next').onclick();await new Promise(resolve=>setImmediate(resolve));assert.ok(calls.at(-1).includes('before_path=%2Fcache%2F%3Cstale%3E'));assert.ok(calls.at(-1).includes('cache_generation=4'));
 p.get('incomplete-directories-query').value='different';p.get('incomplete-directories-filter').onsubmit({preventDefault(){}});await new Promise(resolve=>setImmediate(resolve));releaseStale();await new Promise(resolve=>setImmediate(resolve));assert.match(p.get('incomplete-directories-list').innerHTML,/new-query/);assert.doesNotMatch(p.get('incomplete-directories-list').innerHTML,/cache\/next/);
 const fill={closest:selector=>selector==='[data-incomplete-fill]'?({dataset:{incompleteFill:'/new-query'}}):null};p.get('incomplete-directories-list').onclick({target:fill});assert.equal(p.get('directory').value,'/new-query');assert.equal(calls.some(url=>url.startsWith('/api/cloud-inventory/refresh')),false);
 const open={closest:selector=>selector==='[data-incomplete-cache-open]'?({dataset:{incompleteCacheOpen:'/new-query'}}):null};p.get('incomplete-directories-list').onclick({target:open});await new Promise(resolve=>setImmediate(resolve));assert.equal(p.get('browse-source').value,'cache');assert.ok(calls.some(url=>url.startsWith('/api/cloud-inventory?')));assert.ok(calls.every(url=>!url.startsWith('/api/cloud?')));
});

test('completed scoped refresh reloads the loaded incomplete-path list from local cache once',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live','incomplete-directories-reason':'stale','incomplete-directories-query':'/old'});await new Promise(resolve=>setImmediate(resolve));const calls=[];
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value.startsWith('/api/cloud-inventory/refresh'))return{ok:true,json:async()=>({run_id:'refresh-1',root_path:'/docs',status:'complete',worker_active:false,pending_count:0})};if(value.startsWith('/api/cloud-inventory/incomplete-directories'))return{ok:true,json:async()=>({entries:[{path:'/docs/new',status:'stale',reason:'stale',cached_directory:true}],total:1,cache_generation:8,has_more:false})};return{ok:true,json:async()=>({status:'complete',run_id:'refresh-1',root_path:'/docs',worker_active:false,pending_count:0})}};
 await vm.runInContext("incompleteDirectoriesLoaded=true;incompleteDirectoriesBefore='/old-cursor';incompleteDirectoriesGeneration=7;activeDirectoryRefreshRun='refresh-1';activeDirectoryRefreshRoot='/docs'",p.context);
 await vm.runInContext('directoryRefreshStatus()',p.context);
 assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,1);
 assert.ok(calls.some(url=>url.startsWith('/api/cloud-inventory/incomplete-directories?')&&url.includes('reason=stale')&&url.includes('q=%2Fold')));
 assert.equal(calls.some(url=>url.startsWith('/api/cloud?')),false);
 assert.match(p.get('incomplete-directories-list').innerHTML,/\/docs\/new/);
 const count=calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length;await vm.runInContext('directoryRefreshStatus()',p.context);assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,count,'polling a completed run does not reload the list repeatedly');
});

test('successful remote mutation refreshes loaded incomplete-paths while failed mutation leaves that list untouched',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live','incomplete-directories-reason':'stale','incomplete-directories-query':'docs'});await new Promise(resolve=>setImmediate(resolve));const calls=[];p.context.confirm=()=>true;
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value==='/api/cloud/mutations')return{ok:true,json:async()=>({status:'confirmed',operation_id:'op-1'})};if(value.startsWith('/api/cloud-inventory/incomplete-directories'))return{ok:true,json:async()=>({entries:[],total:0,cache_generation:9,has_more:false})};if(value==='/api/cloud-inventory/scan')return{ok:true,json:async()=>({status:'complete',inventory_coverage:{status:'complete'}})};if(value.startsWith('/api/cloud?'))return{ok:true,json:async()=>({entries:[],total:0})};return{ok:true,json:async()=>({})}};
 await vm.runInContext('incompleteDirectoriesLoaded=true;incompleteDirectoriesBefore="/old";incompleteDirectoriesGeneration=8',p.context);await vm.runInContext("mutate('mkdir',null,{destination_directory:'/docs',new_name:'new'})",p.context);await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,1);assert.ok(calls.some(url=>url.startsWith('/api/cloud-inventory/incomplete-directories?')&&url.includes('reason=stale')&&url.includes('q=docs')));
 await waitForPollIdle(p,'scanStatusInFlight');calls.length=0;p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value==='/api/cloud/mutations')return{ok:false,status:502,text:async()=>'provider failure'};if(value.startsWith('/api/cloud?'))return{ok:true,json:async()=>({entries:[],total:0})};return{ok:true,json:async()=>({})}};await vm.runInContext("mutate('mkdir',null,{destination_directory:'/docs',new_name:'failed'})",p.context);await new Promise(resolve=>setImmediate(resolve));assert.equal(calls.some(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')),false);
});

test('completed full scan refreshes loaded incomplete-paths once and preserves filters',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live','incomplete-directories-reason':'stale','incomplete-directories-query':'/docs'});await waitForPollIdle(p,'scanStatusInFlight');const calls=[];let state={status:'running',run_id:'scan-7',worker_active:true,pending_count:1,inventory_coverage:{status:'incomplete',cache_generation:7}};
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value==='/api/cloud-inventory/scan')return{ok:true,json:async()=>state};if(value.startsWith('/api/cloud-inventory/incomplete-directories'))return{ok:true,json:async()=>({entries:[],total:0,cache_generation:8,has_more:false})};return{ok:true,json:async()=>({})}};
 await vm.runInContext('incompleteDirectoriesLoaded=true;incompleteDirectoriesBefore="/old-cursor";incompleteDirectoriesGeneration=7',p.context);await vm.runInContext('scanStatus()',p.context);assert.equal(calls.some(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')),false);
 state={status:'complete',run_id:'scan-7',worker_active:false,pending_count:0,inventory_coverage:{status:'incomplete',cache_generation:8}};await vm.runInContext('scanStatus()',p.context);
 assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,1);assert.ok(calls.some(url=>url.startsWith('/api/cloud-inventory/incomplete-directories?')&&url.includes('reason=stale')&&url.includes('q=%2Fdocs')));assert.equal(calls.some(url=>url.startsWith('/api/cloud?')),false);
 await vm.runInContext('scanStatus()',p.context);assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,1,'same completed run is handled once');
});

test('historical scan completion and paused or failed scans do not refresh an incomplete list',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await waitForPollIdle(p,'scanStatusInFlight');const calls=[];let state={status:'complete',run_id:'old-scan',worker_active:false,pending_count:0,inventory_coverage:{status:'incomplete'}};
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value==='/api/cloud-inventory/scan')return{ok:true,json:async()=>state};return{ok:true,json:async()=>({})}};vm.runInContext('incompleteDirectoriesLoaded=true',p.context);
 await vm.runInContext('scanStatus()',p.context);state={status:'paused',run_id:'paused-scan',worker_active:false,pending_count:2,inventory_coverage:{status:'incomplete'}};await vm.runInContext('scanStatus()',p.context);state={status:'failed',run_id:'failed-scan',worker_active:false,pending_count:2,inventory_coverage:{status:'incomplete'}};await vm.runInContext('scanStatus()',p.context);
 assert.equal(calls.some(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')),false);
});

test('quickly completed scan started from this page is still associated with its returned run ID',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live','incomplete-directories-reason':'failed','incomplete-directories-query':'archive'});await waitForPollIdle(p,'scanStatusInFlight');const calls=[];
 p.context.fetch=async(url,options={})=>{const value=String(url);calls.push(value);if(value==='/api/cloud-inventory/scan'&&options.method==='POST')return{ok:true,status:202,json:async()=>({status:'running',run_id:'fast-scan',worker_active:true,pending_count:1})};if(value==='/api/cloud-inventory/scan')return{ok:true,json:async()=>({status:'complete',run_id:'fast-scan',worker_active:false,pending_count:0,inventory_coverage:{status:'complete'}})};if(value.startsWith('/api/cloud-inventory/incomplete-directories'))return{ok:true,json:async()=>({entries:[],total:0,cache_generation:10,has_more:false})};return{ok:true,json:async()=>({})}};
 vm.runInContext('incompleteDirectoriesLoaded=true',p.context);await p.get('scan-start').onclick();await waitForPollIdle(p,'scanStatusInFlight');
 assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,1);assert.ok(calls.some(url=>url.startsWith('/api/cloud-inventory/incomplete-directories?')&&url.includes('reason=failed')&&url.includes('q=archive')));assert.equal(calls.some(url=>url.startsWith('/api/cloud?')),false);
});

test('completed full scan does not load an incomplete-path list the user has not opened',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await waitForPollIdle(p,'scanStatusInFlight');const calls=[];let state={status:'running',run_id:'unopened-scan',worker_active:true,pending_count:1,inventory_coverage:{status:'incomplete'}};
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value==='/api/cloud-inventory/scan')return{ok:true,json:async()=>state};return{ok:true,json:async()=>({})}};
 await vm.runInContext('scanStatus()',p.context);state={status:'complete',run_id:'unopened-scan',worker_active:false,pending_count:0,inventory_coverage:{status:'complete'}};await vm.runInContext('scanStatus()',p.context);
 assert.equal(calls.some(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')),false);
});

test('cache coverage reason counts provide local quick filters and include zero categories',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live','incomplete-directories-query':'/docs'});await waitForPollIdle(p,'scanStatusInFlight');const calls=[];
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value==='/api/cloud-inventory/scan')return{ok:true,json:async()=>({status:'complete',run_id:'scan-old',inventory_coverage:{status:'incomplete',cache_generation:9,incomplete_reasons:{scanning_directories:0,stale_directories:4,unknown_directories:1,failed_directories:0,missing_directory_status:2,orphan_directory_status:0,root_incomplete:1,path_mismatch:0,parent_not_complete:3}}})};if(value.startsWith('/api/cloud-inventory/incomplete-directories'))return{ok:true,json:async()=>({entries:[],total:0,cache_generation:9,has_more:false})};return{ok:true,json:async()=>({})}};
 await vm.runInContext('scanStatus()',p.context);const summary=p.get('incomplete-directories-summary');assert.match(summary.innerHTML,/4/);assert.match(summary.innerHTML,/0/);assert.match(summary.innerHTML,/data-incomplete-reason="stale"/);assert.match(summary.innerHTML,/可能重叠/);
 const stale={closest:selector=>selector==='[data-incomplete-reason]'?({dataset:{incompleteReason:'stale'}}):null};summary.onclick({target:stale});await new Promise(resolve=>setImmediate(resolve));assert.equal(p.get('incomplete-directories-reason').value,'stale');assert.ok(calls.some(url=>url.startsWith('/api/cloud-inventory/incomplete-directories?')&&url.includes('reason=stale')&&url.includes('q=%2Fdocs')));assert.equal(calls.some(url=>url.startsWith('/api/cloud?')),false);
});

test('incomplete-directory exports provide current-page and all-filtered JSON actions',()=>{
 const html=fs.readFileSync(path.join(root,'index_cloud.html'),'utf8');assert.match(html,/id="incomplete-directories-export-page"/);assert.match(html,/id="incomplete-directories-export-all"/);assert.match(html,/id="incomplete-directories-export-status"/);assert.match(html,/async function exportIncompleteDirectories/);
});

test('incomplete-directory JSON export whitelists evidence fields and pages one generation completely',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live','incomplete-directories-reason':'stale','incomplete-directories-query':'/docs'});await waitForPollIdle(p,'scanStatusInFlight');const calls=[],downloads=[];let exportedBlob=null;
 p.context.Blob=Blob;p.context.document.createElement=()=>({click(){downloads.push(this.download)},remove(){}});p.context.document.body={append(){}};p.context.URL.createObjectURL=blob=>{exportedBlob=blob;return'blob:cache'};p.context.URL.revokeObjectURL=()=>{};
 const entry=(path,run)=>({path,status:'stale',reason:'stale',cached_directory:true,observed_at_utc:'2026-11-02T00:00:00Z',last_run_id:run,account_fingerprint:'must-not-export',token:'must-not-export'});
 p.context.fetch=async(url)=>{const value=String(url);calls.push(value);if(value.startsWith('/api/cloud-inventory/incomplete-directories')){if(value.includes('before_path='))return{ok:true,json:async()=>({entries:[entry('/docs/b','run-b')],total:2,cache_generation:9,has_more:false})};return{ok:true,json:async()=>({entries:[entry('/docs/a','run-a')],total:2,cache_generation:9,next_before_path:'/docs/a',has_more:true})}}return{ok:true,json:async()=>({})}};
 await vm.runInContext('incompleteDirectoriesLoaded=true;incompleteDirectoriesGeneration=9;incompleteDirectoriesPageEntries=[{path:"/docs/page",reason:"stale",status:"stale",cached_directory:true,observed_at_utc:"2026-11-02T00:00:00Z",last_run_id:"page-run"}];incompleteDirectoriesTotal=2',p.context);await p.get('incomplete-directories-export-page').onclick();
 let payload=JSON.parse(await exportedBlob.text());assert.equal(payload.scope,'current_page');assert.equal(payload.exported_count,1);assert.equal(payload.total_count,2);assert.equal(payload.cache_generation,9);assert.equal(payload.filters.path,'/docs');assert.equal(payload.entries[0].last_run_id,'page-run');assert.equal('account_fingerprint'in payload.entries[0],false);assert.equal('token'in payload.entries[0],false);assert.equal(calls.length,0);
 exportedBlob=null;await p.get('incomplete-directories-export-all').onclick();payload=JSON.parse(await exportedBlob.text());assert.equal(payload.scope,'all_filtered');assert.deepEqual(payload.entries.map(x=>x.path),['/docs/a','/docs/b']);assert.equal(payload.exported_count,2);assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,2);assert.ok(calls.every(url=>url.includes('reason=stale')&&url.includes('q=%2Fdocs')&&url.includes('cache_generation=9')));assert.equal(calls.some(url=>url.startsWith('/api/cloud?')),false);assert.match(downloads[0],/cloud-cache-incomplete-page-/);assert.match(downloads[1],/cloud-cache-incomplete-filtered-/);
});

test('all-filtered cache export aborts on generation conflict or over-cap count without downloading partial JSON',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live','incomplete-directories-reason':'','incomplete-directories-query':''});await waitForPollIdle(p,'scanStatusInFlight');const calls=[],downloads=[];p.context.Blob=Blob;p.context.document.createElement=()=>({click(){downloads.push(this.download)},remove(){}});p.context.document.body={append(){}};p.context.URL.createObjectURL=()=> 'blob:should-not-download';p.context.URL.revokeObjectURL=()=>{};
 await vm.runInContext('incompleteDirectoriesLoaded=true;incompleteDirectoriesGeneration=5;incompleteDirectoriesPageEntries=[{}]',p.context);p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value.includes('before_path='))return{ok:false,status:409,text:async()=>JSON.stringify({code:'cache_generation_changed',cache_generation:6})};return{ok:true,json:async()=>({entries:[{}],total:2,cache_generation:5,has_more:true,next_before_path:'/first'})}};await p.get('incomplete-directories-export-all').onclick();assert.equal(downloads.length,0);assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,2);assert.match(p.get('incomplete-directories-export-status').textContent,/缓存代次已变化/);
 calls.length=0;p.context.fetch=async url=>{calls.push(String(url));return{ok:true,json:async()=>({entries:[],total:10001,cache_generation:5,has_more:true,next_before_path:'/x'})}};await p.get('incomplete-directories-export-all').onclick();assert.equal(downloads.length,0);assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,1);assert.match(p.get('incomplete-directories-export-status').textContent,/超过 10000 条导出上限/);
});

test('completed scan generation drift refreshes a loaded list after a missed run transition once',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live','incomplete-directories-reason':'stale','incomplete-directories-query':'docs'});await waitForPollIdle(p,'scanStatusInFlight');const calls=[];
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value==='/api/cloud-inventory/scan')return{ok:true,json:async()=>({status:'complete',run_id:'completed-while-suspended',worker_active:false,pending_count:0,inventory_coverage:{status:'complete',cache_generation:10}})};if(value.startsWith('/api/cloud-inventory/incomplete-directories'))return{ok:true,json:async()=>({entries:[],total:0,cache_generation:10,has_more:false})};return{ok:true,json:async()=>({})}};
 await vm.runInContext('incompleteDirectoriesLoaded=true;incompleteDirectoriesGeneration=9',p.context);await vm.runInContext('scanStatus()',p.context);assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,1);assert.ok(calls.some(url=>url.startsWith('/api/cloud-inventory/incomplete-directories?')&&url.includes('reason=stale')&&url.includes('q=docs')));await vm.runInContext('scanStatus()',p.context);assert.equal(calls.filter(url=>url.startsWith('/api/cloud-inventory/incomplete-directories')).length,1);assert.equal(calls.some(url=>url.startsWith('/api/cloud?')),false);
});

test('incomplete cloud directory pagination restarts at page one after cache generation changes',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live','incomplete-directories-reason':'','incomplete-directories-query':''});await new Promise(resolve=>setImmediate(resolve));const calls=[];let secondPage=true,resetAfterConflict=false;
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value.startsWith('/api/cloud-inventory/incomplete-directories')){if(value.includes('before_path=')){if(secondPage){secondPage=false;resetAfterConflict=true;return{ok:false,status:409,text:async()=>JSON.stringify({code:'cache_generation_changed',cache_generation:8})}}return{ok:true,json:async()=>({entries:[{path:'/new-cache',reason:'stale',status:'stale',cached_directory:true}],total:1,cache_generation:8,has_more:false})}}if(resetAfterConflict)return{ok:true,json:async()=>({entries:[{path:'/new-cache',reason:'stale',status:'stale',cached_directory:true}],total:1,cache_generation:8,has_more:false})};return{ok:true,json:async()=>({entries:[{path:'/first',reason:'stale',status:'stale',cached_directory:true}],total:2,cache_generation:7,next_before_path:'/first',has_more:true})}}return{ok:true,json:async()=>({})}};
 await p.get('incomplete-directories-load').onclick();await p.get('incomplete-directories-next').onclick();await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls.filter(x=>x.startsWith('/api/cloud-inventory/incomplete-directories')).length,3);assert.ok(calls[1].includes('cache_generation=7'));assert.ok(!calls[2].includes('before_path=')&&!calls[2].includes('cache_generation='));assert.match(p.get('incomplete-directories-list').innerHTML,/new-cache/);assert.match(p.get('incomplete-directories-status').textContent,/generation 8/);
});

test('cloud cache full-inventory search navigates to cached or fresh live parent listings explicitly',async()=>{
	const html=fs.readFileSync(path.join(root,'index_cloud.html'),'utf8');
	assert.match(html,/id="cache-search-form"/);
	const p=page('index_cloud.html',[],{'directory':'/','browse-source':'cache','cache-search-query':'AI报告'});
	const calls=[];
	p.context.fetch=async url=>{
		calls.push(String(url));
		if(String(url).startsWith('/api/cloud-inventory/search?'))return{ok:true,json:async()=>({query:'AI报告',entries:[{fs_id:701,path:'/资料/AI报告.pdf',name:'AI报告.pdf',directory:false,size:2048,provider_mtime:123},{fs_id:702,path:'/资料/方案',name:'方案',directory:true,size:0,directory_size_bytes:4096,directory_size_complete:true}],total:3,offset:0,next_offset:1,coverage:{status:'incomplete',incomplete_directories:2,scan_updated_at_utc:'2026-10-30T00:00:00Z'}})};
		if(String(url).startsWith('/api/cloud?'))return{ok:true,json:async()=>({directory:'/资料',entries:[{id:'701',fs_id:701,path:'/资料/AI报告.pdf',name:'AI报告.pdf',directory:false,size:2048,provider_size:2048,server_mtime:123}],total:1,offset:0})};
		if(String(url).startsWith('/api/cloud-inventory?')){const isResultParent=String(url).includes('dir=%2F%E8%B5%84%E6%96%99');return{ok:true,json:async()=>({directory:isResultParent?'/资料':'/',entries:[],metadata:{path:isResultParent?'/资料':'/',status:isResultParent?'stale':'complete',observed_at_utc:isResultParent?'2026-10-29T00:00:00Z':null},total:0,offset:0,next_offset:null})};}
		if(String(url).startsWith('/api/cloud-inventory/size?'))return{ok:true,json:async()=>({path:'/',bytes:0,files:0,directories:0,complete:true})};
		return{ok:true,json:async()=>({status:'idle',entries:[],groups:[]})};
	};
	await p.get('cache-search-form').onsubmit({preventDefault(){}});
	assert.ok(calls.some(url=>url==='/api/cloud-inventory/search?q=AI%E6%8A%A5%E5%91%8A&offset=0&limit=100'));
	assert.ok(!calls.some(url=>url.startsWith('/api/cloud?')),'cache search called the live provider');
	assert.match(p.get('browse-source-status').textContent,/缓存不完整/);
	assert.match(p.get('browse-source-status').textContent,/不代表网盘没有/);
	assert.match(p.get('entries').innerHTML,/AI报告\.pdf/);
	assert.match(p.get('entries').innerHTML,/data-cache-open="\/资料\/方案"/);
	assert.match(p.get('entries').innerHTML,/data-cache-parent="\/资料"/);
	assert.match(p.get('entries').innerHTML,/data-live-parent="\/资料"/);
	assert.ok(!/data-(rename|move|delete)=/.test(p.get('entries').innerHTML));
	assert.equal(p.get('cache-search-form').hidden,false);
	const beforeCacheNavigation=calls.length;
	p.get('entries').onclick({target:{closest:selector=>selector==='[data-cache-parent]'?{dataset:{cacheParent:'/资料'}}:null}});
	await new Promise(resolve=>setImmediate(resolve));
	assert.equal(p.get('browse-source').value,'cache');
	assert.equal(p.get('directory').value,'/资料');
	assert.equal(p.get('cache-search-query').value,'');
	assert.ok(calls.slice(beforeCacheNavigation).some(url=>url==='/api/cloud-inventory?dir=%2F%E8%B5%84%E6%96%99&offset=0&limit=100'),'cache navigation did not open the cached parent at offset zero');
	assert.ok(!calls.slice(beforeCacheNavigation).some(url=>url.startsWith('/api/cloud?')),'cache navigation called the live provider');
	assert.equal(p.get('cache-search-form').hidden,false,'cached directory browsing should keep cache search available');
	assert.match(p.get('browse-source-status').textContent,/缓存不完整/,'cached navigation hid stale directory coverage');
	assert.match(p.get('browse-source-status').textContent,/2026-10-29T00:00:00Z/,'cached navigation hid its observation time');
	assert.ok(!/data-(rename|move|delete)=/.test(p.get('entries').innerHTML),'cached navigation exposed remote mutation actions');
	await p.get('cache-search-form').onsubmit({preventDefault(){}});
	const beforeDirectoryOpen=calls.length;
	p.get('entries').onclick({target:{closest:selector=>selector==='[data-cache-open]'?{dataset:{cacheOpen:'/资料/方案'}}:null}});
	await new Promise(resolve=>setImmediate(resolve));
	assert.equal(p.get('browse-source').value,'cache');
	assert.equal(p.get('directory').value,'/资料/方案');
	assert.ok(calls.slice(beforeDirectoryOpen).some(url=>url==='/api/cloud-inventory?dir=%2F%E8%B5%84%E6%96%99%2F%E6%96%B9%E6%A1%88&offset=0&limit=100'),'directory hit did not open itself in the cached listing');
	assert.ok(!calls.slice(beforeDirectoryOpen).some(url=>url.startsWith('/api/cloud?')),'opening a directory hit called the live provider');
	await p.get('cache-search-form').onsubmit({preventDefault(){}});
	p.get('entries').onclick({target:{closest:selector=>selector==='[data-live-parent]'?{dataset:{liveParent:'/资料'}}:null}});
	await new Promise(resolve=>setImmediate(resolve));
	assert.equal(p.get('browse-source').value,'live');
	assert.equal(p.get('directory').value,'/资料');
	assert.ok(calls.some(url=>url.startsWith('/api/cloud?dir=%2F%E8%B5%84%E6%96%99&offset=0')),'live handoff did not refresh the parent listing from offset zero');
	assert.equal(p.get('entries').innerHTML.includes('data-rename="701"'),true,'live listing did not restore fresh operation controls');
	assert.equal(p.get('cache-search-form').hidden,true,'search remained available in live mode');
});

test('cached directory and search pagination reanchor once after inventory generation changes',async()=>{
	const p=page('index_cloud.html',[],{'directory':'/cached','browse-source':'cache'});
	const calls=[];let directoryGeneration=4,searchGeneration=4,conflictDirectory=true,conflictSearch=true;
	p.context.fetch=async url=>{
		calls.push(url);
		if(String(url).startsWith('/api/cloud-inventory/size?'))return{ok:true,json:async()=>({path:'/cached',bytes:0,files:0,directories:0,complete:true})};
		if(String(url).startsWith('/api/cloud-inventory/search?')){
			if(url.includes('offset=1')&&conflictSearch){conflictSearch=false;searchGeneration=5;return{ok:false,status:409,text:async()=>JSON.stringify({code:'cache_generation_changed',cache_generation:5})}}
			return{ok:true,json:async()=>({query:'needle',entries:[{fs_id:9,path:'/cached/needle',name:'needle',size:1}],total:2,offset:0,next_offset:1,cache_generation:searchGeneration,coverage:{status:'complete'}})};
		}
		if(String(url).startsWith('/api/cloud-inventory?')){
			if(url.includes('offset=1')&&conflictDirectory){conflictDirectory=false;directoryGeneration=5;return{ok:false,status:409,text:async()=>JSON.stringify({code:'cache_generation_changed',cache_generation:5})}}
			if(url.includes('cache_generation=4'))directoryGeneration=5;
			return{ok:true,json:async()=>({directory:'/cached',entries:[{fs_id:directoryGeneration,path:'/cached/item',name:`generation-${directoryGeneration}`,directory:false,size:1}],metadata:{path:'/cached',status:'complete'},total:2,offset:0,next_offset:1,cache_generation:directoryGeneration})};
		}
		return{ok:true,json:async()=>({status:'idle',entries:[]})};
	};
	await vm.runInContext('browse()',p.context);
	await p.get('next').onclick();
	await new Promise(resolve=>setImmediate(resolve));
	assert.ok(calls.some(url=>url.includes('offset=1&limit=100&cache_generation=4')),'directory page omitted its generation token');
	assert.match(p.get('directory-size').textContent,/\/cached：0 B/);
	assert.match(p.get('message').textContent,/缓存已更新/);
	assert.match(p.get('entries').innerHTML,/generation-5/);
	assert.ok(calls.some(url=>url.includes('dir=%2Fcached&offset=0&limit=100')),'directory conflict did not restart at offset zero');
	p.get('cache-search-query').value='needle';
	await p.get('cache-search-form').onsubmit({preventDefault(){}});
	await p.get('next').onclick();
	await new Promise(resolve=>setImmediate(resolve));
	assert.ok(calls.some(url=>url.includes('q=needle&offset=1&limit=100&cache_generation=4')),'search page omitted its generation token');
	assert.match(p.get('message').textContent,/缓存已更新/);
	assert.equal(calls.filter(url=>url.includes('q=needle&offset=0')).length,2,'search conflict did not re-read exactly one first page');
});

test('late local cache page from an old directory cannot replace the selected directory',async()=>{
	const p=page('index_cloud.html',[],{'directory':'/old','browse-source':'cache'});
	let resolveOld;const calls=[];
	p.context.fetch=url=>{
		calls.push(String(url));
		if(String(url).includes('dir=%2Fold'))return new Promise(resolve=>{resolveOld=()=>resolve({ok:true,json:async()=>({directory:'/old',entries:[{fs_id:1,path:'/old/file',name:'old-file',size:1}],metadata:{status:'complete'},total:1,offset:0,next_offset:null,cache_generation:1})})});
		if(String(url).includes('dir=%2Fnew'))return Promise.resolve({ok:true,json:async()=>({directory:'/new',entries:[{fs_id:2,path:'/new/file',name:'new-file',size:1}],metadata:{status:'complete'},total:1,offset:0,next_offset:null,cache_generation:2})});
		if(String(url).startsWith('/api/cloud-inventory/size?'))return Promise.resolve({ok:true,json:async()=>({path:'/new',bytes:1,files:1,directories:0,complete:true})});
		return Promise.resolve({ok:true,json:async()=>({status:'idle',entries:[]})});
	};
	const oldRequest=vm.runInContext('browse()',p.context);
	p.get('directory').value='/new';
	const newRequest=vm.runInContext('browse()',p.context);
	await newRequest;resolveOld();await oldRequest;
	assert.match(p.get('entries').innerHTML,/new-file/);
	assert.doesNotMatch(p.get('entries').innerHTML,/old-file/);
	assert.ok(calls.some(url=>url.includes('dir=%2Fnew')));
});

test('cloud page exposes persisted monitor and local download path settings',()=>{
	const cloudPage=fs.readFileSync(path.join(root,'index_cloud.html'),'utf8');
	assert.match(cloudPage,/id="path-form"/);
	assert.match(cloudPage,/id="queue-path"/);
	assert.match(cloudPage,/id="local-path"/);
	assert.match(cloudPage,/id="migrate-local-files"/);
	assert.match(cloudPage,/\/api\/settings\/local-migration/);
	assert.match(cloudPage,/核对内容/);
	assert.match(cloudPage,/\/api\/settings\/paths/);
	assert.match(cloudPage,/由 \.env 或启动参数显式设置的路径在网页中只读/);
	assert.match(cloudPage,/id="queue-path-impact"/);
	assert.match(cloudPage,/下一次新目录成功扫描/);
});

test('monitor path change lists affected tasks and requires explicit confirmation',async()=>{
 const settings={queue_dir:'/monitor',local_download_dir:'D:\\downloads',queue_editable:true,local_download_editable:true};
 const impact={current_queue_dir:'/monitor',target_queue_dir:'/new',count:1,offset:0,limit:50,has_more:false,tasks:[{fs_id:91,path:'/monitor/a<file>.txt',server_filename:'a<file>.txt',status:'pending',attempts:0,download_mode:'keep'}],digest:'preview-digest'};
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'},async url=>({ok:true,json:async()=>String(url)==='/api/settings/paths'?settings:{queue_change_impact:impact}}));await new Promise(resolve=>setImmediate(resolve));
 let confirmation='',posts=0,submitted=null;p.context.confirm=message=>{confirmation=message;return false};
 p.context.fetch=async(url,options={})=>{if(String(url).includes('/api/settings/paths?queue_dir='))return{ok:true,json:async()=>({queue_change_impact:impact})};if(String(url)==='/api/settings/paths'&&options.method==='POST'){posts++;submitted=JSON.parse(options.body);return{ok:true,json:async()=>({queue_dir:'/new',local_download_dir:'D:\\downloads'})}}return{ok:true,json:async()=>({})}};
 p.get('queue-path').value='/new';await p.get('path-form').onsubmit({preventDefault(){}});
 assert.match(p.get('queue-path-impact').innerHTML,/a&lt;file&gt;\.txt/);assert.match(confirmation,/源缺失失败/);assert.match(confirmation,/暂停调度/);assert.match(confirmation,/a<file>\.txt/);assert.equal(posts,0,'canceling queue impact confirmation must not save paths');
 p.context.confirm=()=>true;await p.get('path-form').onsubmit({preventDefault(){}});
 assert.equal(posts,1);assert.equal(submitted.queue_change_preview_digest,'preview-digest');assert.equal(p.get('queue-path').value,'/new');assert.match(p.get('path-status').textContent,/暂停调度.*成功扫描/);
});

test('stale monitor path impact is shown and requires a fresh confirmation',async()=>{
 const settings={queue_dir:'/monitor',local_download_dir:'D:\\downloads',queue_editable:true,local_download_editable:true};
 const first={current_queue_dir:'/monitor',target_queue_dir:'/new',count:1,offset:0,limit:50,has_more:false,tasks:[{fs_id:92,path:'/monitor/a.txt',server_filename:'a.txt',status:'pending',attempts:0,download_mode:'keep'}],digest:'first-digest'};
 const latest={...first,tasks:[{...first.tasks[0],fs_id:93,path:'/monitor/b.txt',server_filename:'b.txt'}],digest:'latest-digest'};
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'},async url=>({ok:true,json:async()=>String(url)==='/api/settings/paths'?settings:{queue_change_impact:first}}));await new Promise(resolve=>setImmediate(resolve));
 let previews=0,posts=0;const submitted=[];p.context.confirm=()=>true;
 p.context.fetch=async(url,options={})=>{if(String(url).includes('/api/settings/paths?queue_dir=')){previews++;return{ok:true,json:async()=>({queue_change_impact:previews===1?first:latest})}}if(String(url)==='/api/settings/paths'&&options.method==='POST'){posts++;submitted.push(JSON.parse(options.body));if(posts===1)return{ok:false,status:409,text:async()=>JSON.stringify({error:'队列在确认后发生变化，请核对最新受影响任务后再次确认',queue_change_impact:latest})};return{ok:true,json:async()=>({queue_dir:'/new',local_download_dir:'D:\\downloads'})}}return{ok:true,json:async()=>({})}};
 p.get('queue-path').value='/new';await p.get('path-form').onsubmit({preventDefault(){}});
 assert.equal(posts,1,'a stale confirmation must not be retried automatically');assert.match(p.get('queue-path-impact').innerHTML,/b\.txt/);assert.match(p.get('path-status').textContent,/再次点击/);
 await p.get('path-form').onsubmit({preventDefault(){}});assert.equal(posts,2);assert.deepEqual(submitted.map(x=>x.queue_change_preview_digest),['first-digest','latest-digest']);
});

test('monitor path impact preview pages large queues without changing snapshot digest',async()=>{
 const settings={queue_dir:'/monitor',local_download_dir:'D:\\downloads',queue_editable:true,local_download_editable:true};
 const page0={current_queue_dir:'/monitor',target_queue_dir:'/new',count:51,offset:0,limit:50,has_more:true,tasks:[{fs_id:101,path:'/monitor/first.txt',server_filename:'first.txt',status:'pending',attempts:0,download_mode:'keep'}],digest:'same-full-snapshot'};
 const page1={...page0,offset:50,has_more:false,tasks:[{fs_id:151,path:'/monitor/last.txt',server_filename:'last.txt',status:'retry_wait',attempts:2,download_mode:'keep'}]};
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'},async url=>({ok:true,json:async()=>String(url)==='/api/settings/paths'?settings:{queue_change_impact:page0}}));await new Promise(resolve=>setImmediate(resolve));
 const urls=[];p.context.fetch=async url=>{urls.push(String(url));return{ok:true,json:async()=>({queue_change_impact:String(url).includes('offset=50')?page1:page0})}};
 p.get('queue-path').value='/new';p.get('queue-path').onchange();await new Promise(resolve=>setImmediate(resolve));
 assert.match(p.get('queue-path-impact').innerHTML,/第 1–1 项 \/ 共 51 项/);assert.match(p.get('queue-path-impact').innerHTML,/下一页/);
 p.get('queue-path-impact').onclick({target:{closest:()=>({disabled:false,dataset:{queueImpactOffset:'50'}})}});await new Promise(resolve=>setImmediate(resolve));
 assert.ok(urls.some(url=>url.includes('offset=50')&&url.includes('limit=50')));assert.match(p.get('queue-path-impact').innerHTML,/第 51–51 项 \/ 共 51 项/);assert.match(p.get('queue-path-impact').innerHTML,/last\.txt/);assert.doesNotMatch(p.get('queue-path-impact').innerHTML,/first\.txt/);
});

test('changing local download path without migration confirms existing files stay in the old directory',async()=>{
	const initialFetch=async url=>({ok:true,json:async()=>String(url)==='/api/settings/paths'?{queue_dir:'/monitor',local_download_dir:'D:\\old-downloads',queue_editable:true,local_download_editable:true}:{}});
	const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'},initialFetch);await new Promise(resolve=>setImmediate(resolve));
	let confirmation='',posts=0;
	p.context.confirm=message=>{confirmation=message;return false;};
	p.context.fetch=async(url,options={})=>{
		if(String(url)==='/api/settings/paths'&&options.method==='POST'){posts++;return{ok:true,json:async()=>({queue_dir:'/monitor',local_download_dir:'D:\\new-downloads'})};}
		return{ok:true,json:async()=>({queue_dir:'/monitor',local_download_dir:'D:\\old-downloads'})};
	};
	p.get('queue-path').value='/monitor';p.get('local-path').value='D:\\new-downloads';
	await p.get('path-form').onsubmit({preventDefault(){}});
	assert.match(confirmation,/现有文件.*留在.*原目录/);
	assert.match(confirmation,/新下载.*新目录/);
	assert.equal(posts,0,'canceling the no-migration warning must not save the path');
	p.context.confirm=message=>{confirmation=message;return true;};
	await p.get('path-form').onsubmit({preventDefault(){}});
	assert.equal(posts,1,'confirming the no-migration warning should proceed with the path update');
	assert.equal(p.get('local-path').value,'D:\\new-downloads');
});

test('managed path form is locked during a migration and restored after failure',async()=>{
	const fetchImpl=async url=>{
		const value=String(url);
		if(value==='/api/settings/paths')return{ok:true,json:async()=>({queue_dir:'/monitor',local_download_dir:'D:\\downloads',queue_editable:true,local_download_editable:true})};
		if(value==='/api/settings/local-migration')return{ok:true,json:async()=>({status:'running',phase:'copying',processed_files:0,total_files:1,processed_bytes:0,total_bytes:1,started_at_utc:'2026-10-31T00:00:00Z',updated_at_utc:'2026-10-31T00:00:00Z'})};
		return{ok:true,json:async()=>({entries:[],total:0,offset:0,limit:10,next_offset:null})};
	};
	const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'},fetchImpl);await new Promise(resolve=>setImmediate(resolve));
	assert.equal(p.get('queue-path').disabled,true);
	assert.equal(p.get('local-path').disabled,true);
	assert.equal(p.get('migrate-local-files').disabled,true);
	assert.equal(p.get('save-paths').disabled,true);
	p.context.fetch=async url=>{
		if(String(url)==='/api/settings/local-migration')return{ok:true,json:async()=>({status:'failed',phase:'copying',processed_files:0,total_files:1,processed_bytes:0,total_bytes:1,started_at_utc:'2026-10-31T00:00:00Z',updated_at_utc:'2026-10-31T00:01:00Z',last_error:'test failure'})};
		if(String(url)==='/api/settings/paths')return{ok:true,json:async()=>({queue_dir:'/effective-monitor',local_download_dir:'D:\\effective-downloads',queue_editable:true,local_download_editable:true})};
		return{ok:true,json:async()=>({entries:[],total:0,offset:0,limit:10,next_offset:null})};
	};
	await vm.runInContext('pollMigration()',p.context);
	await new Promise(resolve=>setImmediate(resolve));
	assert.equal(p.get('queue-path').disabled,false);
	assert.equal(p.get('local-path').disabled,false);
	assert.equal(p.get('migrate-local-files').disabled,false);
	assert.equal(p.get('save-paths').disabled,false);
	assert.equal(p.get('queue-path').value,'/effective-monitor');
	assert.equal(p.get('local-path').value,'D:\\effective-downloads');
});

test('local migration evidence export downloads only after a click and shows server errors',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/', 'browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 let clicked=null,requestURL='';p.context.URL.createObjectURL=()=> 'blob:migration';p.context.URL.revokeObjectURL=()=>{};
 p.context.document.createElement=tag=>({tagName:tag,click(){clicked=this;},remove(){}});p.context.document.body={append(){}};
 const filename='local-migration-'+'a'.repeat(32)+'.json';p.context.fetch=async url=>{requestURL=String(url);return{ok:true,headers:{get:()=> 'attachment; filename="'+filename+'"'},blob:async()=>({size:10})};};
 const button=p.get('export-migration-evidence');await button.onclick();
 assert.equal(requestURL,'/api/settings/local-migration/export');assert.equal(clicked.download,filename);assert.equal(p.get('migration-export-status').textContent,'证据已开始下载');
 p.context.fetch=async()=>({ok:false,status:404,text:async()=> '尚无本地目录迁移记录。'});
 await button.onclick();assert.equal(p.get('migration-export-status').textContent,'尚无本地目录迁移记录。');
});

test('local migration history pages records, ignores stale responses and exports the selected ID',async()=>{
 const html=fs.readFileSync(path.join(root,'index_cloud.html'),'utf8');
 assert.match(html,/id="migration-history-refresh"/);assert.match(html,/id="migration-history-next"/);assert.match(html,/id="migration-history-prev"/);
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const id0='0'.repeat(31)+'1',id10='0'.repeat(31)+'2',calls=[];let releaseStale;
 const item=id=>({id,source_path:'<img src=x>',target_path:'D:\\new',status:'completed',phase:'complete',processed_files:1,total_files:1,processed_bytes:5,total_bytes:5,started_at_utc:'2026-10-30T01:00:00Z',updated_at_utc:'2026-10-30T01:01:00Z'});
 p.context.fetch=async url=>{
  const value=String(url);calls.push(value);
  if(value.includes('/api/settings/local-migrations?offset=10'))return await new Promise(resolve=>{releaseStale=()=>resolve({ok:true,json:async()=>({entries:[item(id10)],total:21,offset:10,limit:10,next_offset:20})})});
  if(value.includes('/api/settings/local-migrations?offset=0'))return{ok:true,json:async()=>({entries:[item(id0)],total:21,offset:0,limit:10,next_offset:10})};
  if(value.includes('/export'))return{ok:true,headers:{get:()=>`attachment; filename="local-migration-${id0}.json"`},blob:async()=>({size:10})};
  return{ok:true,json:async()=>({})};
 };
 const oldRequest=vm.runInContext('loadMigrationHistory(10)',p.context);const currentRequest=vm.runInContext('loadMigrationHistory(0)',p.context);await currentRequest;releaseStale();await oldRequest;
 assert.match(p.get('migration-history').innerHTML,new RegExp(id0));assert.doesNotMatch(p.get('migration-history').innerHTML,new RegExp(id10));assert.match(p.get('migration-history').innerHTML,/&lt;img src=x&gt;/);assert.doesNotMatch(p.get('migration-history').innerHTML,/<img src=x>/);assert.equal(p.get('migration-history-next').disabled,false);
 let clicked=null;p.context.URL.createObjectURL=()=> 'blob:selected';p.context.URL.revokeObjectURL=()=>{};p.context.document.createElement=()=>({click(){clicked=this},remove(){}});p.context.document.body={append(){}};
 p.get('migration-history').onclick({target:{closest:selector=>selector==='[data-export-migration]'?({dataset:{exportMigration:id0},disabled:false}):null}});await new Promise(resolve=>setImmediate(resolve));
 assert.ok(calls.includes(`/api/settings/local-migrations/${id0}/export`));assert.equal(clicked.download,`local-migration-${id0}.json`);
});

test('local migration history filters reset paging and remain active while paging or refreshing',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));const calls=[];
 p.context.fetch=async url=>{const value=String(url);calls.push(value);const offset=value.includes('offset=10')?10:0;return{ok:true,json:async()=>({entries:[],total:25,offset,limit:10,next_offset:offset===10?20:10})};};
 p.get('migration-history-status-filter').value='failed';p.get('migration-history-query').value='  D:\\NAS_%  ';
 p.get('migration-history-filter').onsubmit({preventDefault(){}});await new Promise(resolve=>setImmediate(resolve));
 assert.ok(calls.at(-1).includes('offset=0'));assert.ok(calls.at(-1).includes('status=failed'));assert.ok(calls.at(-1).includes('q=D%3A%5CNAS_%25'));
 p.get('migration-history-next').onclick();await new Promise(resolve=>setImmediate(resolve));assert.ok(calls.at(-1).includes('offset=10'));assert.ok(calls.at(-1).includes('status=failed'));assert.ok(calls.at(-1).includes('q=D%3A%5CNAS_%25'));
 p.get('migration-history-refresh').onclick();await new Promise(resolve=>setImmediate(resolve));assert.ok(calls.at(-1).includes('offset=10'));assert.ok(calls.at(-1).includes('status=failed'));
 p.get('migration-history-clear').onclick();await new Promise(resolve=>setImmediate(resolve));assert.ok(calls.at(-1).includes('offset=0'));assert.ok(!calls.at(-1).includes('status=')&&!calls.at(-1).includes('q='));assert.equal(p.get('migration-history-query').value,'');
});

test('local migration history date filter is UTC, retained while paging and included in summary export',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));const calls=[];
 p.context.fetch=async url=>{const value=String(url);calls.push(value);const offset=value.includes('offset=10')?10:0;return{ok:true,json:async()=>({entries:[],total:20,offset,limit:10,next_offset:offset===0?10:null})};};
 p.get('migration-history-status-filter').value='';p.get('migration-history-query').value='';p.get('migration-history-start-date-utc').value='2024-02-29';p.get('migration-history-end-date-utc').value='2024-03-01';
 p.get('migration-history-filter').onsubmit({preventDefault(){}});await new Promise(resolve=>setImmediate(resolve));
 assert.ok(calls.at(-1).includes('started_from_utc=2024-02-29'));assert.ok(calls.at(-1).includes('started_through_utc=2024-03-01'));
 p.get('migration-history-next').onclick();await new Promise(resolve=>setImmediate(resolve));assert.ok(calls.at(-1).includes('offset=10')&&calls.at(-1).includes('started_from_utc=2024-02-29')&&calls.at(-1).includes('started_through_utc=2024-03-01'));
 p.get('migration-history-refresh').onclick();await new Promise(resolve=>setImmediate(resolve));assert.ok(calls.at(-1).includes('started_from_utc=2024-02-29')&&calls.at(-1).includes('started_through_utc=2024-03-01'));
 p.get('migration-history-clear').onclick();await new Promise(resolve=>setImmediate(resolve));assert.ok(!calls.at(-1).includes('started_from_utc=')&&!calls.at(-1).includes('started_through_utc='));
});

test('local migration summary export uses current filters only after click and reports errors',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));const calls=[];let clicked=null;
 p.context.URL.createObjectURL=()=> 'blob:summary';p.context.URL.revokeObjectURL=()=>{};p.context.document.createElement=()=>({click(){clicked=this},remove(){}});p.context.document.body={append(){}};
 p.context.fetch=async url=>{calls.push(String(url));return{ok:true,headers:{get:()=> 'attachment; filename="local-migration-history.json"'},blob:async()=>({size:20})};};
 const button=p.get('migration-history-export');assert.equal(calls.length,0);p.get('migration-history-status-filter').value='failed';p.get('migration-history-query').value=' D:\\logs_% ';p.get('migration-history-start-date-utc').value='2024-02-29';p.get('migration-history-end-date-utc').value='2024-03-01';
 await button.onclick();assert.equal(calls[0],'/api/settings/local-migrations/export?status=failed&q=D%3A%5Clogs_%25&started_from_utc=2024-02-29&started_through_utc=2024-03-01');assert.equal(clicked.download,'local-migration-history.json');assert.equal(p.get('migration-summary-export-status').textContent,'摘要已开始下载');
 p.context.fetch=async url=>({ok:false,status:413,text:async()=> '筛选结果超过 5000 条，未生成部分导出文件。'});await button.onclick();assert.match(p.get('migration-summary-export-status').textContent,/超过 5000 条/);
});

test('local migration event timeline pages by migration ID, escapes evidence and ignores stale selection',async()=>{
 const p=page('index_cloud.html',[],{'directory':'/','browse-source':'live'});await new Promise(resolve=>setImmediate(resolve));
 const id1='a'.repeat(32),id2='b'.repeat(32),calls=[];let releaseStale;
 p.context.fetch=async url=>{const value=String(url);calls.push(value);if(value.includes(`/local-migrations/${id1}/events`))return await new Promise(resolve=>{releaseStale=()=>resolve({ok:true,json:async()=>({migration_id:id1,events:[{sequence:1,status:'failed',phase:'copying',recorded_at_utc:'2026-10-30T01:00:00Z',evidence:{note:'stale event'}}],next_before_sequence:0,has_more:false})})});if(value.includes(`/local-migrations/${id2}/events?limit=20&before_sequence=2`))return{ok:true,json:async()=>({migration_id:id2,events:[{sequence:1,status:'running',phase:'copying',recorded_at_utc:'2026-10-30T00:00:00Z',evidence:{note:'older'}}],next_before_sequence:0,has_more:false})};if(value.includes(`/local-migrations/${id2}/events`))return{ok:true,json:async()=>({migration_id:id2,events:[{sequence:3,status:'running',phase:'verifying',recorded_at_utc:'2026-10-30T03:00:00Z',evidence:{note:'<img src=x>'}},{sequence:2,status:'running',phase:'copying',recorded_at_utc:'2026-10-30T02:00:00Z',evidence:{processed_files:2}}],next_before_sequence:2,has_more:true})};return{ok:true,json:async()=>({})};};
 const click=(attribute,id)=>{const key={'data-toggle-migration-events':'toggleMigrationEvents','data-load-older-migration-events':'loadOlderMigrationEvents'}[attribute];p.get('migration-history').onclick({target:{closest:selector=>selector===`[${attribute}]`?({dataset:{[key]:id}}):null}})};
 click('data-toggle-migration-events',id1);click('data-toggle-migration-events',id2);await new Promise(resolve=>setImmediate(resolve));
 const panel=p.get(`migration-events-${id2}`);assert.match(panel.innerHTML,/&lt;img src=x&gt;/);assert.doesNotMatch(panel.innerHTML,/<img src=x>/);assert.ok(calls.some(url=>url===`/api/settings/local-migrations/${id2}/events?limit=20`));
 releaseStale();await new Promise(resolve=>setImmediate(resolve));assert.doesNotMatch(p.get(`migration-events-${id1}`).innerHTML,/stale event/);assert.match(panel.innerHTML,/verifying/);
 click('data-load-older-migration-events',id2);await new Promise(resolve=>setImmediate(resolve));assert.ok(calls.some(url=>url===`/api/settings/local-migrations/${id2}/events?limit=20&before_sequence=2`));assert.match(panel.innerHTML,/older/);
});

test('automatic monitor history loads read-only records and escapes remote paths',async()=>{
 const calls=[];const p=page('index_monitor_history.html',[],{},async url=>{calls.push(String(url));return{ok:true,json:async()=>({runs:[{run_id:'a'.repeat(32),status:'success',monitor_path:'/queue/<img>',started_at_utc:'2026-11-02T01:00:00Z',finished_at_utc:'2026-11-02T01:00:02Z',cloud_observed_at_utc:'2026-11-02T01:00:01Z'}],has_more:false,next_before_sequence:0})};});await new Promise(resolve=>setImmediate(resolve));
 assert.equal(calls.length,1);assert.ok(calls[0].startsWith('/api/monitor-scans?'));assert.match(p.get('scan-list').innerHTML,/&lt;img&gt;/);assert.doesNotMatch(p.get('scan-list').innerHTML,/<img>/);assert.match(p.get('scan-list').innerHTML,/百度目录响应观察 UTC/);
});

test('automatic monitor history events are fetched only when requested',async()=>{
 const calls=[];const p=page('index_monitor_history.html',[],{},async url=>{calls.push(String(url));return{ok:true,json:async()=>String(url).includes('/events')?{events:[{kind:'cloud_listed',recorded_at_utc:'2026-11-02T01:00:01Z',cloud_observed_at_utc:'2026-11-02T01:00:01Z'}],has_more:false}:{runs:[{run_id:'b'.repeat(32),status:'interrupted',monitor_path:'/queue',started_at_utc:'2026-11-02T01:00:00Z'}],has_more:false}};});await new Promise(resolve=>setImmediate(resolve));assert.equal(calls.length,1);
 const handler=p.listeners.get('scan-list:click');await handler({target:{closest:selector=>selector==='[data-more-events]'?null:({dataset:{events:'b'.repeat(32)}})}});await new Promise(resolve=>setImmediate(resolve));assert.equal(calls.length,2);assert.ok(calls[1].includes('/api/monitor-scans/events?run_id='));assert.match(p.get(`events-${'b'.repeat(32)}`).innerHTML,/cloud_listed/);
});
