const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '..', 'internal', 'webui');

function page(name, tasks = []) {
    const nodes = new Map();
    const get = id => {
        if (!nodes.has(id)) nodes.set(id, {style:{}, textContent:'', innerHTML:'', addEventListener:(event,fn)=>listeners.set(id+':'+event,fn),querySelector:()=>get(id+' span')});
        return nodes.get(id);
    };
    const listeners = new Map();
    const sockets = [];
    class WebSocket {
        static OPEN = 1;
        constructor() { this.sent=[]; sockets.push(this); }
        send(message) { this.sent.push(message); }
    }
    const context = vm.createContext({URL, URLSearchParams, AbortController, console, WebSocket, location:{protocol:'http:',host:'localhost'},
        document:{getElementById:get, querySelectorAll:()=>[], addEventListener:(event,fn)=>listeners.set(event,fn)},
        setTimeout:()=>0, setInterval:()=>0,
        fetch:async url=>({ok:true,json:async()=>url.includes('status') && !url.includes('oauth') ? {tasks} : {authentication:{state:'unconfigured',ready:false},state:'unconfigured',ready:false}})
    });
    vm.runInContext(fs.readFileSync(path.join(root, 'res', 'shared.js'), 'utf8'), context);
    const html = fs.readFileSync(path.join(root, name), 'utf8');
    for (const match of html.matchAll(/<script>([\s\S]*?)<\/script>/g)) vm.runInContext(match[1], context);
    if(html.includes('/res/intake.js'))vm.runInContext(fs.readFileSync(path.join(root,'res','intake.js'),'utf8'),context);
    return {context,get,listeners,sockets};
}

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
 assert.ok(html.includes('job_registered'));assert.ok(!html.includes('task_cancelled'),'older page should replace the current page');
 assert.equal(vm.runInContext("taskEventPanels.get('123').events.length",p.context),1,'only the current page should remain in memory');
 const olderEvent=html.slice(html.indexOf('job_registered'),html.indexOf('</article>',html.indexOf('job_registered')));
 assert.ok(!olderEvent.includes('云端扫描观察时间'),'missing cloud observation time must stay absent');
 assert.ok(html.includes('返回较新事件'));
 await vm.runInContext("loadNewerTaskEvents('123')",p.context);
 html=vm.runInContext('taskEventsControl(task)',p.context);
 assert.equal(calls.length,3);assert.ok(!calls[2].includes('before_sequence='));
 assert.ok(html.includes('task_cancelled'));assert.ok(!html.includes('job_registered'));
 assert.equal(vm.runInContext("taskEventPanels.get('123').events.length",p.context),1,'newer navigation must also retain only one page');
 await vm.runInContext("toggleTaskEvents('456')",p.context);
 assert.equal(vm.runInContext('taskEventPanels.size',p.context),1,'only one task timeline should stay expanded in memory');
 assert.equal(vm.runInContext("taskEventPanels.has('123')",p.context),false);
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
 p.context.payload={tasks:[],total_tasks:250,status_counts:{downloading:2,done:240,failed:8},offset:0,limit:100,has_more:true,next_offset:100};
 vm.runInContext('renderTasks(payload)',p.context);
 assert.equal(p.get('total-tasks').textContent,250);
 assert.equal(p.get('done-tasks').textContent,240);
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

test('malformed, extra-field and noncanonical handoffs never become input or requests',()=>{
 for(const value of ['%bad',encodeURIComponent(JSON.stringify({version:2,paths:['/a']})),encodeURIComponent(JSON.stringify({version:1,paths:['/a'],token:'secret'})),encodeURIComponent(JSON.stringify({version:1,paths:['/a/../b']})),encodeURIComponent(JSON.stringify({version:1,paths:['/a ']})),encodeURIComponent(JSON.stringify({version:1,paths:['/a','/a']})),'x'.repeat(60001)]) {
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
