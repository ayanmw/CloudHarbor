import {mountBuiltWorkspace} from './built-workspace.mjs';
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {JSDOM,VirtualConsole} from 'jsdom';
const html=await readFile(new URL('../../internal/webui/app/index.html',import.meta.url),'utf8');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
async function render(respond, url='http://localhost/task-events',beforeMount,virtualConsole){
 const requests=[],dom=new JSDOM(html,{url,runScripts:'outside-only',pretendToBeVisual:true,...(virtualConsole?{virtualConsole}:{})});
 dom.window.fetch=async (url,init={})=>{requests.push({url:String(url),method:init.method||'GET'});if(respond)return respond(String(url));const q=new URL(url,'http://localhost').searchParams,asOf=q.get('as_of_sequence')||'9007199254740993',before=q.get('before_sequence');return {ok:true,json:async()=>({events:[{sequence:before?String(BigInt(before)-1n):asOf,job_id:'9007199254740995',kind:'job_completed',recorded_at_utc:'2026-11-03T03:00:00Z'}],as_of_sequence:asOf,total_matches:1,kind_counts:[{kind:'job_completed',count:1}],has_more:false,next_before_sequence:null})};};
 beforeMount?.(dom.window);await mountBuiltWorkspace(dom);await tick();await tick();return {dom,requests};
}
test('event workspace mounts a compact read-only table and preserves all filter controls',async()=>{
 const {dom,requests}=await render();try{
 const doc=dom.window.document;
 assert.match(doc.querySelector('h1').textContent,/任务事件/);
 assert.match(doc.title,/任务事件/);
 assert.equal(doc.querySelectorAll('tbody tr').length,1);
 assert.match(doc.querySelector('tbody').textContent,/9007199254740995/);
 for(const name of ['task_id','kind','operation_id','attempt_id','from_utc','through_utc','recorded_from_utc','recorded_before_utc'])assert.ok(doc.querySelector(`[name="${name}"]`),name);
 assert.ok(requests.every(r=>r.method==='GET' && !r.url.includes('task-workspace')));
 for(const link of doc.querySelectorAll('.sidebar nav a')){assert.ok(link.querySelector('svg'),'compact navigation needs an icon');assert.ok(link.getAttribute('aria-label'));}
 }finally{dom.window.close();}
});
test('event workspace reports a failed read without claiming the history is empty',async()=>{
 const {dom}=await render(async()=>({ok:false,status:503,text:async()=> '本机事件暂不可读'}));try{
 assert.match(dom.window.document.querySelector('[role="alert"]').textContent,/本机事件暂不可读/);
 assert.doesNotMatch(dom.window.document.body.textContent,/没有事件记录/);
 }finally{dom.window.close();}
});
test('event URL restores precise IDs UTC and submitted filters without forwarding action parameters',async()=>{
 const params=new URLSearchParams({task_id:'9007199254740995',attempt_id:'尝试 / &',recorded_from_utc:'2026-11-03T01:02:03.123456000Z',as_of_sequence:'9007199254740993',before_sequence:'9007199254740992',action:'delete',admin_key:'discard'});
 const {dom,requests}=await render(undefined,'http://localhost/task-events?'+params+'#discard');try{
 const first=new URL(requests[0].url,'http://localhost');
 assert.equal(first.searchParams.get('task_id'),'9007199254740995');
 assert.equal(first.searchParams.get('attempt_id'),'尝试 / &');
 assert.equal(first.searchParams.get('recorded_from_utc'),'2026-11-03T01:02:03.123456000Z');
 assert.equal(first.searchParams.get('before_sequence'),'9007199254740992');
 assert.equal(first.searchParams.get('action'),null);assert.equal(dom.window.location.hash,'');
 const doc=dom.window.document, input=doc.querySelector('[name=attempt_id]');
 assert.equal(doc.querySelector('[role=alert]'),null);
 input.value='draft';input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));
 [...doc.querySelectorAll('button')].find(b=>b.textContent.includes('刷新记录')).click();await tick();await tick();
 assert.equal(new URL(requests.at(-1).url,'http://localhost').searchParams.get('attempt_id'),'尝试 / &');
 doc.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();
 assert.equal(new URL(requests.at(-1).url,'http://localhost').searchParams.get('attempt_id'),'draft');
 dom.window.history.back();await new Promise(r=>setTimeout(r,30));await tick();
 assert.equal(doc.querySelector('[name=attempt_id]').value,'尝试 / &');
 assert.ok(requests.every(r=>r.method==='GET'));
 }finally{dom.window.close();}
});
test('invalid event URL conditions do not issue a request or silently reduce UTC precision',async()=>{
 for(const search of ['task_id=9223372036854775808','recorded_from_utc=2026-02-30T00%3A00%3A00Z','recorded_from_utc=2026-11-03T00%3A00%3A00.000000001Z','task_id=1&task_id=2','before_sequence=2','as_of_sequence=1&before_sequence=2','attempt_id='+encodeURIComponent('😀'.repeat(33))]){
 const {dom,requests}=await render(undefined,'http://localhost/task-events?'+search);try{
 assert.equal(requests.length,0,search);assert.ok(dom.window.document.querySelector('[role=alert]'),search);
 }finally{dom.window.close();}
 }
});
test('event paging fixes a snapshot, preserves rows on failure and explicit refresh starts a new snapshot',async()=>{
 let newest='9007199254740993',fail=false;
 const {dom,requests}=await render(async raw=>{
  if(fail)return {ok:false,status:503,text:async()=> '分页读取失败'};
  const q=new URL(raw,'http://localhost').searchParams,asOf=q.get('as_of_sequence')||newest,older=!!q.get('before_sequence');
  return {ok:true,json:async()=>({events:[{sequence:older?'9007199254740991':asOf,job_id:'1',kind:older?'job_registered':'job_completed',recorded_at_utc:'2026-11-03T03:00:00Z'}],as_of_sequence:asOf,total_matches:2,kind_counts:[],has_more:!older,next_before_sequence:older?null:asOf})};
 });try{
 const doc=dom.window.document;
 let next=doc.querySelector('[data-testid=event-next]');assert.ok(next);
 newest='9007199254740995';fail=true;next.click();await tick();await tick();
 assert.match(doc.querySelector('tbody').textContent,/9007199254740993/);
 assert.match(doc.querySelector('[role=alert]').textContent,/分页读取失败/);
 fail=false;doc.querySelector('[data-testid=event-next]').click();await tick();await tick();
 const q=new URL(requests.at(-1).url,'http://localhost').searchParams;
 assert.equal(q.get('as_of_sequence'),'9007199254740993');assert.equal(q.get('before_sequence'),'9007199254740993');
 assert.match(doc.querySelector('tbody').textContent,/9007199254740991/);
 doc.querySelector('[data-testid=event-prev]').click();await tick();await tick();
 assert.match(doc.querySelector('tbody').textContent,/9007199254740993/);
 [...doc.querySelectorAll('button')].find(b=>b.textContent.includes('刷新记录')).click();await tick();await tick();
 const fresh=new URL(requests.at(-1).url,'http://localhost').searchParams;
 assert.equal(fresh.get('as_of_sequence'),null);assert.equal(fresh.get('before_sequence'),null);
 assert.match(doc.querySelector('tbody').textContent,/9007199254740995/);
 }finally{dom.window.close();}
});
test('browser back isolates a late older-page response',async()=>{
 let finishOld,firstPages=0;
 const response=(older,kind)=>({ok:true,json:async()=>({events:[{sequence:older?'9':'10',job_id:'1',kind,recorded_at_utc:'2026-11-03T03:00:00Z'}],as_of_sequence:'10',total_matches:2,kind_counts:[],has_more:!older,next_before_sequence:older?null:'10'})});
 const {dom}=await render(async raw=>{
  if(new URL(raw,'http://localhost').searchParams.get('before_sequence'))return new Promise(resolve=>{finishOld=()=>resolve(response(true,'source_operation_unknown'));});
  return response(false,++firstPages===1?'job_completed':'job_registered');
 });try{
 const doc=dom.window.document;doc.querySelector('[data-testid=event-next]').click();await tick();
 dom.window.history.back();await new Promise(r=>setTimeout(r,30));await tick();
 assert.match(doc.querySelector('tbody').textContent,/任务登记/);
 finishOld();await tick();await tick();
 assert.match(doc.querySelector('tbody').textContent,/任务登记/);assert.doesNotMatch(doc.querySelector('tbody').textContent,/结果未知/);
 }finally{finishOld?.();dom.window.close();}
});
test('a non-advancing cursor is rejected without replacing the visible page',async()=>{
 const {dom}=await render(async raw=>{
 const older=new URL(raw,'http://localhost').searchParams.has('before_sequence');
 return {ok:true,json:async()=>({events:[{sequence:older?'9':'10',job_id:'1',kind:'job_completed'}],as_of_sequence:'10',total_matches:3,kind_counts:[],has_more:true,next_before_sequence:'10'})};
 });try{
 const doc=dom.window.document;doc.querySelector('[data-testid=event-next]').click();await tick();await tick();
 assert.match(doc.querySelector('[role=alert]').textContent,/分页边界无效/);assert.match(doc.querySelector('tbody').textContent,/10/);
 }finally{dom.window.close();}
});
test('event drawer preserves clock evidence and exact identity, escapes content and filters the selected correlation',async()=>{
 const {dom,requests}=await render(async()=>({ok:true,json:async()=>({events:[{sequence:'9007199254740993',job_id:'9007199254740995',kind:'job_completed',recorded_at_utc:'2026-11-03T03:00:00.123456Z',occurred_at_utc:'invalid',response_observed_at_utc:'2026-11-03T03:00:01Z',attempt_id:'尝试-&',operation_id:'operation-1',run_id:'run-1',evidence:{path:'<img src=x onerror=alert(1)>'}}],as_of_sequence:'9007199254740993',total_matches:1,kind_counts:[],has_more:false,next_before_sequence:null})}));try{
 const doc=dom.window.document;
 const open=doc.querySelector('[data-testid=event-details]');assert.ok(open);open.click();await tick();
 const drawer=doc.querySelector('[aria-labelledby=event-detail-title]');assert.ok(drawer);
 assert.match(drawer.textContent,/本机记录 UTC.*2026-11-03T03:00:00.123456Z/s);
 assert.match(drawer.textContent,/事件发生 UTC.*invalid/s);
 assert.match(drawer.textContent,/响应观察 UTC.*2026-11-03T03:00:01Z/s);
 assert.doesNotMatch(drawer.textContent,/云端观察 UTC/);
 assert.match(drawer.textContent,/9007199254740995/);
 assert.equal(drawer.querySelector('details').open,false);
 assert.match(drawer.querySelector('pre').textContent,/<img/);assert.equal(drawer.querySelector('img'),null);
 drawer.querySelector('[data-testid=filter-attempt]').click();await tick();await tick();
 assert.equal(doc.querySelector('[aria-labelledby=event-detail-title]'),null);
 assert.equal(new URL(requests.at(-1).url,'http://localhost').searchParams.get('attempt_id'),'尝试-&');
 assert.ok(requests.every(r=>r.method==='GET'));
 }finally{dom.window.close();}
});
test('export uses the submitted snapshot, excludes page cursors and ignores a late download after a new query',async()=>{
 let finishBlob, exportCalls=0;const downloads=[];
 const {dom,requests}=await render(async raw=>{
 if(raw.includes('/export?')){exportCalls++;return {ok:true,headers:{get:()=>''},blob:async()=>new Promise(resolve=>{finishBlob=()=>resolve({mock:true});})};}
 const q=new URL(raw,'http://localhost').searchParams;return {ok:true,json:async()=>({events:[],as_of_sequence:q.get('as_of_sequence')||'10',total_matches:0,kind_counts:[],has_more:false,next_before_sequence:null})};
 },'http://localhost/task-events?task_id=9007199254740993',w=>{w.URL.createObjectURL=()=> 'blob:mock';w.URL.revokeObjectURL=()=>{};w.HTMLAnchorElement.prototype.click=function(){downloads.push(this.download);};});try{
 const doc=dom.window.document,input=doc.querySelector('[name=task_id]');
 input.value='2';input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));
 const exportButton=doc.querySelector('[data-testid=event-export]');assert.ok(exportButton);exportButton.click();exportButton.click();await tick();
 const query=new URL(requests.find(r=>r.url.includes('/export?')).url,'http://localhost').searchParams;
 assert.equal(exportCalls,1);assert.equal(query.get('task_id'),'9007199254740993');assert.equal(query.get('as_of_sequence'),'10');assert.equal(query.get('before_sequence'),null);assert.equal(query.get('limit'),null);
 doc.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();
 finishBlob();await tick();await tick();assert.equal(downloads.length,0);assert.doesNotMatch(doc.body.textContent,/已开始下载/);
 }finally{finishBlob?.();dom.window.close();}
});
test('export reports server failure and a successful download independently of the readable table',async()=>{
 for(const fail of [true,false]){
 const downloads=[];const {dom}=await render(async raw=>raw.includes('/export?')?(fail?{ok:false,status:413,text:async()=> '记录过多，请缩小筛选范围'}:{ok:true,headers:{get:()=> 'attachment; filename="task-events-global-test.json"'},blob:async()=>({mock:true})}):{ok:true,json:async()=>({events:[],as_of_sequence:'0',total_matches:0,kind_counts:[],has_more:false,next_before_sequence:null})},undefined,w=>{w.URL.createObjectURL=()=> 'blob:mock';w.URL.revokeObjectURL=()=>{};w.HTMLAnchorElement.prototype.click=function(){downloads.push(this.download);};});try{
 const doc=dom.window.document;doc.querySelector('[data-testid=event-export]').click();await tick();await tick();
 if(fail){assert.match(doc.querySelector('[role=alert]').textContent,/导出失败.*缩小筛选范围/);assert.equal(downloads.length,0);}else{assert.deepEqual(downloads,['task-events-global-test.json']);assert.match(doc.body.textContent,/已开始下载/);}
 assert.equal(doc.querySelector('[data-testid=event-export]').disabled,false);
 }finally{dom.window.close();}
 }
});
test('event drawer keyboard focus includes the evidence summary in its focus loop',async()=>{
 const {dom}=await render(undefined,undefined,w=>{w.HTMLElement.prototype.getClientRects=()=>[{width:10,height:10}];});try{
 const doc=dom.window.document;doc.querySelector('[data-testid=event-details]').click();await tick();await tick();
 const panel=doc.querySelector('[aria-labelledby=event-detail-title]');
 assert.equal(doc.activeElement.getAttribute('aria-label'),'关闭事件详情');
 doc.activeElement.dispatchEvent(new dom.window.KeyboardEvent('keydown',{key:'Tab',shiftKey:true,bubbles:true,cancelable:true}));
 assert.equal(doc.activeElement,panel.querySelector('summary'));
 }finally{dom.window.close();}
});
test('read and export authentication expiry request login and do not start a download',async()=>{
 for(const atExport of [false,true]){
 let navigations=0,downloads=0;const console=new VirtualConsole();console.on('jsdomError',e=>{assert.match(e.message,/Not implemented: navigation/);navigations++;});
 const {dom}=await render(async raw=>!atExport||raw.includes('/export?')?{ok:false,status:401,text:async()=> 'unauthorized'}:{ok:true,json:async()=>({events:[],as_of_sequence:'0',total_matches:0,kind_counts:[],has_more:false,next_before_sequence:null})},undefined,w=>{w.HTMLAnchorElement.prototype.click=()=>downloads++;},console);try{
 const doc=dom.window.document;if(atExport){doc.querySelector('[data-testid=event-export]').click();await tick();await tick();}
 assert.match(doc.querySelector('[role=alert]').textContent,/请重新登录/);assert.equal(navigations,1);assert.equal(downloads,0);
 if(!atExport)assert.equal(doc.querySelector('[data-testid=event-export]').disabled,true);
 }finally{dom.window.close();}
 }
});
