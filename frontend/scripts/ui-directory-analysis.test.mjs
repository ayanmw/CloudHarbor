import {mountBuiltWorkspace} from './built-workspace.mjs';
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {JSDOM,VirtualConsole} from 'jsdom';
const html=await readFile(new URL('../../internal/webui/app/index.html',import.meta.url),'utf8');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
test('duplicate parent-directory links prefill scope but never create a report automatically',async()=>{
 const query=new URLSearchParams({left:'/保留 & 原件',right:'/副本',action:'delete'});
 const {dom,requests}=await render('?'+query,async()=>({ok:true,json:async()=>({reports:[],has_more:false,next_before:''})}));
 try{const d=dom.window.document;assert.equal(d.querySelector('[data-testid=analysis-cloud-left]').value,'/保留 & 原件');assert.equal(d.querySelector('[data-testid=analysis-cloud-right]').value,'/副本');assert.ok(requests.every(r=>r.method==='GET'));assert.ok(requests.every(r=>!r.url.includes('delete')));}finally{dom.window.close()}
});
const run='a'.repeat(32),id='c'.repeat(32),at='2026-11-06T12:00:00Z';
const summary={files:'2',directories:'0',bytes:'9007199254740996',unverified_files:'0',listing_complete:true,content_complete:true,tree_digest:'a'.repeat(64),content_digest:'b'.repeat(64)};
const header={id,source_run:run,source_config:'b'.repeat(32),source_scope:'account-app',source_mode:'both',source_event_sequence:'12',source_recorded_at_utc:at,created_at_utc:at,left_root:'D:\\fixture',right_root:'/fixture',historical:true,row_count:'3',group_count:'1',summary:{version:'directory-comparison-v1',relation:'different',left:summary,right:{...summary,files:'3',bytes:'9007199254740999'}}};
const entry=(path,hash='a',size='3')=>({path,id:'entry-'+path,directory:false,size,sha256:hash.repeat(64),observed_at_utc:at});
const allRows=[{path:'a',kind:'same',left:entry('a'),right:entry('a')},{path:'b',kind:'only_right',left:null,right:entry('b','b')},{path:'c',kind:'content_conflict',left:entry('c','c','9007199254740993'),right:entry('c','d','9007199254740993')}];
const group={ordinal:1,key:'a'.repeat(64)+':3',sha256:'a'.repeat(64),size:'3',redundant_bytes_left:'0',redundant_bytes_right:'0',member_count:'2',capacity_basis:'identity_deduplicated_logical_bytes_upper_bound',members:[{side:'left',entry:entry('a')},{side:'right',entry:entry('a')}],members_have_more:false};
const page=(url,overrides={})=>{const q=new URL(url,'http://localhost').searchParams,kind=q.get('kind')||'';const rows=allRows.filter(r=>!kind||r.kind===kind);return {analysis:header,rows,offset:Number(q.get('offset')||0),kind,matched:String(rows.length),has_more:false,groups:[group],group_offset:Number(q.get('group_offset')||0),groups_have_more:false,...overrides}};
test('cloud directory comparison is explicit and labels both sources as historical cloud cache',async()=>{
 const cached={...header,source_kind:'cloud_cache',cache_generation:'7',source_run:'',source_config:'',source_mode:'',source_event_sequence:'',left_root:'/left',right_root:'/right'};
 const {dom,requests}=await render('',async(url,init)=>({ok:true,json:async()=>init.method==='POST'?cached:url==='/api/directory-analyses'?{reports:[],has_more:false,next_before:''}:page(url,{analysis:cached})}));
 try {const d=dom.window.document;for(const [side,path]of [['left','/left'],['right','/right']]){const input=d.querySelector('[data-testid=analysis-cloud-'+side+']');assert.ok(input);input.value=path;input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));}await tick();d.querySelector('[data-testid=analysis-cloud-create]').click();await tick();await tick();await tick();assert.equal(requests.filter(r=>r.method==='POST').length,1);assert.equal(requests.find(r=>r.method==='POST').url,'/api/directory-analyses');assert.match(d.querySelector('[data-testid=directory-analysis-report]').textContent,/缓存代次 7/);assert.match(d.body.textContent,/网盘（左侧）/);assert.doesNotMatch(d.querySelector('[data-testid=analysis-rows] thead').textContent,/本地/);} finally {dom.window.close()}
});
async function render(search,respond){const requests=[],dom=new JSDOM(html,{url:'http://localhost/directory-analysis'+search,runScripts:'outside-only',pretendToBeVisual:true,virtualConsole:new VirtualConsole()});dom.window.fetch=async(raw,init={})=>{const url=String(raw);requests.push({url,method:init.method||'GET'});if(url.startsWith('/api/directory-analyses')||url.endsWith('/analysis'))return respond(url,init);return {ok:true,json:async()=>({authentication:{ready:true},counts:{},queue_pause:{paused:false},queue_healthy:true})}};await mountBuiltWorkspace(dom);await tick();await tick();return {dom,requests}}
test('directory analysis creates an explicit local report without starting synchronization or cloud mutations',async()=>{const {dom,requests}=await render('?run='+run,async(url,init)=>({ok:true,json:async()=>init.method==='POST'?header:url==='/api/directory-analyses'?{reports:[],has_more:false,next_before:'',limit:50}:page(url)}));try{const d=dom.window.document;assert.ok(requests.every(r=>r.method==='GET'));d.querySelector('[data-testid=analysis-create]').click();await tick();await tick();await tick();assert.equal(requests.filter(r=>r.method==='POST').length,1);assert.ok(requests.find(r=>r.method==='POST').url.endsWith('/api/sync/runs/'+run+'/analysis'));assert.match(d.querySelector('[data-testid=directory-analysis-report]').textContent,/历史|快照/);assert.match(d.body.textContent,/8 PiB/);assert.match(d.querySelector('[data-testid=analysis-rows]').textContent,/同路径内容不同/);assert.ok(requests.every(r=>!r.url.includes('/api/cloud/mutations')&&!r.url.includes('/api/uploads')&&!/\/configs\/.*\/runs/.test(r.url)));}finally{dom.window.close()}});
test('a mismatched filtered report retains the old evidence and disables export',async()=>{let invalid=false;const {dom}=await render('?report='+id,async url=>({ok:true,json:async()=>page(url,invalid?{analysis:{...header,id:'d'.repeat(32)}}:{})}));try{const d=dom.window.document;invalid=true;const select=d.querySelector('[data-testid=analysis-kind]');select.value='content_conflict';select.dispatchEvent(new dom.window.Event('change',{bubbles:true}));select.closest('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();assert.match(d.body.textContent,/证据无效/);assert.match(d.body.textContent,/保留/);assert.equal(d.querySelector('a[href*="export=1"]'),null);assert.equal(d.querySelector('[data-testid=analysis-rows] tbody tr').textContent.includes('同路径同内容'),true);}finally{dom.window.close()}});
test('late directory analysis responses after pagehide cannot publish or redirect',async()=>{let resolve;const {dom}=await render('?report='+id,async()=>new Promise(r=>resolve=r));try{dom.window.dispatchEvent(new dom.window.Event('pagehide'));resolve({ok:false,status:401});await tick();await tick();assert.equal(dom.window.document.querySelector('[data-testid=directory-analysis-report]'),null);assert.equal(dom.window.location.pathname,'/directory-analysis');}finally{dom.window.close()}});
test('report path order accepts supplementary Unicode names in Go UTF8 order',async()=>{const paths=['\ue000','\u{10000}'];const {dom}=await render('?report='+id,async url=>({ok:true,json:async()=>page(url,{analysis:{...header,row_count:'2'},matched:'2',rows:paths.map(path=>({path,kind:'same',left:entry(path),right:entry(path)}))})}));try{assert.equal(dom.window.document.querySelectorAll('[data-testid=analysis-rows] tbody tr').length,2);assert.doesNotMatch(dom.window.document.body.textContent,/证据无效/);}finally{dom.window.close()}});


test('opening saved analysis focuses and reveals its report while history remains keyboard reachable',async()=>{
 const {dom,requests}=await render('',async url=>({ok:true,json:async()=>url==='/api/directory-analyses'?{reports:[header],has_more:false,next_before:''}:page(url)}));
 try{const d=dom.window.document,scrolled=[];dom.window.HTMLElement.prototype.scrollIntoView=function(){scrolled.push(this)};const history=d.querySelector('[role="region"][aria-label="分析报告历史，可横向滚动"]');assert.ok(history);assert.equal(history.tabIndex,0);[...history.querySelectorAll('button')].find(x=>x.textContent==='查看报告').click();await tick();await tick();await tick();const report=d.querySelector('[data-testid=directory-analysis-report]');assert.ok(report);assert.equal(d.activeElement,report);assert.deepEqual(scrolled,[report]);assert.equal(report.querySelectorAll('.table-wrap[role=region][tabindex="0"]').length,3,'all report tables need keyboard scroll access');assert.ok(requests.every(r=>r.method==='GET'));}finally{dom.window.close()}
});

test('opening saved analysis never moves focus for a response received after leaving',async()=>{
 let resolve;const {dom}=await render('',async url=>url==='/api/directory-analyses'?{ok:true,json:async()=>({reports:[header],has_more:false,next_before:''})}:new Promise(r=>resolve=r));
 try{let scrolls=0;dom.window.HTMLElement.prototype.scrollIntoView=()=>scrolls++;[...dom.window.document.querySelectorAll('button')].find(x=>x.textContent==='查看报告').click();await tick();dom.window.dispatchEvent(new dom.window.Event('pagehide'));resolve({ok:true,json:async()=>page('/api/directory-analyses/'+id)});await tick();await tick();await tick();assert.equal(scrolls,0);assert.equal(dom.window.document.querySelector('[data-testid=directory-analysis-report]'),null);}finally{dom.window.close()}
});


const historySection=d=>d.querySelector('[aria-label="分析报告历史，可横向滚动"]').closest('section');
const historyButton=(d,text)=>[...historySection(d).querySelectorAll('button')].find(b=>b.textContent===text);
test('analysis history distinguishes unread detail mode and successful empty results',async()=>{
 const {dom,requests}=await render('?report='+id,async url=>({ok:true,json:async()=>url==='/api/directory-analyses'?{reports:[],has_more:false,next_before:''}:page(url)}));
 try{const d=dom.window.document;assert.match(historySection(d).textContent,/尚未读取历史列表/);assert.equal(requests.filter(r=>r.url==='/api/directory-analyses').length,0);historyButton(d,'读取最新记录').click();await tick();await tick();assert.match(historySection(d).textContent,/尚无保存的分析报告/);assert.match(historySection(d).textContent,/列表读取 UTC/);}finally{dom.window.close()}
});
test('analysis history reports first read failure locally without claiming empty history',async()=>{
 const {dom}=await render('',async()=>({ok:false,status:503,text:async()=>'历史服务暂不可用'}));
 try{const section=historySection(dom.window.document);assert.match(section.querySelector('[role=alert]').textContent,/历史服务暂不可用/);assert.match(section.textContent,/历史列表读取失败/);assert.doesNotMatch(section.textContent,/尚无保存|尚无已读取/);}finally{dom.window.close()}
});
test('analysis history retains rows and observation time after refresh failure',async()=>{
 let fail=false;const {dom}=await render('',async()=>fail?{ok:false,status:503,text:async()=>'历史刷新失败'}:{ok:true,json:async()=>({reports:[header],has_more:false,next_before:''})});
 try{const d=dom.window.document,section=historySection(d),observed=section.querySelector('[data-testid=analysis-history-observed]').textContent;fail=true;historyButton(d,'读取最新记录').click();await tick();await tick();assert.equal(section.querySelectorAll('tbody button').length,1);assert.equal(section.querySelector('[data-testid=analysis-history-observed]').textContent,observed);assert.match(section.querySelector('[role=alert]').textContent,/历史刷新失败/);assert.match(section.textContent,/保留上次读取/);}finally{dom.window.close()}
});
test('analysis history keeps prior rows visible during a refresh without flashing an empty state',async()=>{
 let pending=false,resolve;const {dom}=await render('',async()=>pending?new Promise(r=>resolve=r):{ok:true,json:async()=>({reports:[header],has_more:false,next_before:''})});
 try{const d=dom.window.document,section=historySection(d);pending=true;historyButton(d,'读取最新记录').click();await tick();assert.equal(section.querySelectorAll('tbody button').length,1);assert.match(section.textContent,/正在重新读取历史/);assert.doesNotMatch(section.textContent,/尚无保存/);resolve({ok:true,json:async()=>({reports:[header],has_more:false,next_before:''})});await tick();await tick();assert.doesNotMatch(section.textContent,/正在重新读取历史/);}finally{dom.window.close()}
});
test('empty older history page never claims there are no saved analysis reports',async()=>{
 const {dom}=await render('',async url=>({ok:true,json:async()=>url.includes('?before=')?{reports:[],has_more:false,next_before:''}:{reports:[header],has_more:true,next_before:'f'.repeat(32)}}));
 try{const d=dom.window.document;historyButton(d,'更早报告').click();await tick();await tick();assert.match(historySection(d).textContent,/这一页没有分析报告/);assert.doesNotMatch(historySection(d).textContent,/尚无保存的分析报告/);assert.equal(historyButton(d,'回到最新').disabled,false);}finally{dom.window.close()}
});


test('analysis history explains an unavailable local service with a recovery action',async()=>{
 const {dom}=await render('',async()=>{throw new TypeError('Failed to fetch')});
 try{const message=historySection(dom.window.document).querySelector('[role=alert]').textContent;assert.match(message,/无法连接管理服务/);assert.match(message,/确认程序正在运行/);assert.match(message,/网络连接/);assert.match(message,/重试/);assert.doesNotMatch(message,/Failed to fetch/);}finally{dom.window.close()}
});
