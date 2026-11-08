import {mountBuiltWorkspace} from './built-workspace.mjs';
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {JSDOM,VirtualConsole} from 'jsdom';
const html=await readFile(new URL('../../internal/webui/app/index.html',import.meta.url),'utf8');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
async function render(failed=false,url='http://localhost/monitor-history',beforeMount,virtualConsole){
 const requests=[],dom=new JSDOM(html,{url,runScripts:'outside-only',pretendToBeVisual:true,...(virtualConsole?{virtualConsole}:{})});
 dom.window.fetch=async(url,init={})=>{requests.push({url:String(url),method:init.method||'GET'});if(typeof failed==='function')return failed(String(url));return failed?{ok:false,status:503,text:async()=> '监控历史暂不可读'}:{ok:true,json:async()=>({runs:[{sequence:'1',run_id:'a'.repeat(32),status:'interrupted',monitor_path:'/模拟监控目录',started_at_utc:'2026-11-03T03:00:00Z',recorded_at_utc:'2026-11-03T03:00:01Z'}],has_more:false,next_before_sequence:null})};};
 beforeMount?.(dom.window);await mountBuiltWorkspace(dom);await tick();await tick();return {dom,requests};
}
test('monitor history mounts its own compact table without treating interrupted scans as success',async()=>{
 const {dom,requests}=await render();try{
 const doc=dom.window.document;assert.match(doc.querySelector('h1').textContent,/监控历史/);
 assert.match(doc.title,/监控历史/);assert.equal(doc.querySelectorAll('tbody tr').length,1);
 assert.match(doc.querySelector('tbody').textContent,/中断/);assert.match(doc.querySelector('tbody').textContent,/模拟监控目录/);
 assert.ok(doc.querySelector('[name=status]'));assert.ok(doc.querySelector('[name=started_from_utc]'));assert.ok(doc.querySelector('[name=started_through_utc]'));
 assert.ok(requests.every(r=>r.method==='GET'&&r.url.startsWith('/api/monitor-scans?')));
 for(const link of doc.querySelectorAll('.sidebar nav a')){assert.ok(link.querySelector('svg'));assert.ok(link.getAttribute('aria-label'));}
 }finally{dom.window.close();}
});
test('monitor read failure is visible and does not pretend there were no scans',async()=>{
 const {dom}=await render(true);try{assert.match(dom.window.document.querySelector('[role=alert]').textContent,/监控历史暂不可读/);assert.doesNotMatch(dom.window.document.body.textContent,/没有扫描记录/);}finally{dom.window.close();}
});
test('monitor pages use exact string boundaries and preserve visible runs on failure',async()=>{
 let fail=false;
 const {dom,requests}=await render(async raw=>{if(fail)return {ok:false,status:503,text:async()=> '历史分页失败'};const older=new URL(raw,'http://localhost').searchParams.has('before_sequence');return {ok:true,json:async()=>({runs:[{sequence:older?'9007199254740991':'9007199254740993',run_id:(older?'b':'a').repeat(32),status:'success',monitor_path:older?'/较早目录':'/最新目录',started_at_utc:'2026-11-03T03:00:00Z'}],has_more:!older,next_before_sequence:older?null:'9007199254740993'})};});try{
 const doc=dom.window.document,next=doc.querySelector('[data-testid=monitor-next]');assert.ok(next);fail=true;next.click();await tick();await tick();assert.match(doc.querySelector('tbody').textContent,/最新目录/);
 fail=false;next.click();await tick();await tick();const query=new URL(requests.at(-1).url,'http://localhost').searchParams;assert.equal(query.get('before_sequence'),'9007199254740993');assert.equal(query.get('sequence_format'),'string');assert.match(doc.querySelector('tbody').textContent,/较早目录/);
 doc.querySelector('[data-testid=monitor-prev]').click();await tick();await tick();assert.match(doc.querySelector('tbody').textContent,/最新目录/);
 }finally{dom.window.close();}
});
test('monitor URL restores submitted filters and exact cursor while browser back remains read only',async()=>{
 const params=new URLSearchParams({status:'interrupted',started_from_utc:'2026-11-01',started_through_utc:'2026-11-03',before_sequence:'9007199254740993',run_id:'a'.repeat(32),action:'delete',admin_key:'discard'});
 const {dom,requests}=await render(false,'http://localhost/monitor-history?'+params+'#discard');try{
 const first=new URL(requests[0].url,'http://localhost').searchParams,doc=dom.window.document;
 assert.equal(first.get('status'),'interrupted');assert.equal(first.get('before_sequence'),'9007199254740993');assert.equal(first.get('run_id'),null);assert.equal(first.get('action'),null);assert.equal(dom.window.location.hash,'');assert.equal(new URL(dom.window.location.href).searchParams.get('run_id'),'a'.repeat(32));
 const input=doc.querySelector('[name=started_from_utc]');input.value='2026-11-02';input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));
 [...doc.querySelectorAll('button')].find(b=>b.textContent.includes('刷新记录')).click();await tick();await tick();
 assert.equal(new URL(requests.at(-1).url,'http://localhost').searchParams.get('started_from_utc'),'2026-11-01');
 doc.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();
 assert.equal(new URL(requests.at(-1).url,'http://localhost').searchParams.get('started_from_utc'),'2026-11-02');
 dom.window.history.back();await new Promise(r=>setTimeout(r,30));await tick();assert.equal(doc.querySelector('[name=started_from_utc]').value,'2026-11-01');assert.ok(requests.every(r=>r.method==='GET'));
 }finally{dom.window.close();}
});
test('invalid monitor URL values do not issue requests',async()=>{
 for(const search of ['status=bogus','started_from_utc=2026-02-30','started_from_utc=2026-11-03&started_through_utc=2026-11-01','before_sequence=9223372036854775808','before_sequence=0','run_id=bad','status=failed&status=success']){
 const {dom,requests}=await render(false,'http://localhost/monitor-history?'+search);try{assert.equal(requests.length,0,search);assert.ok(dom.window.document.querySelector('[role=alert]'),search);}finally{dom.window.close();}
 }
});
test('monitor browser back rejects a late response from the abandoned page',async()=>{
 let finishOld,firstReads=0;
 const result=(older,path)=>({ok:true,json:async()=>({runs:[{sequence:older?'9':'10',run_id:(older?'b':'a').repeat(32),status:'success',monitor_path:path,started_at_utc:'2026-11-03T03:00:00Z'}],has_more:!older,next_before_sequence:older?null:'10'})});
 const {dom}=await render(async raw=>{if(new URL(raw,'http://localhost').searchParams.has('before_sequence'))return new Promise(resolve=>{finishOld=()=>resolve(result(true,'/旧响应'));});return result(false,++firstReads===1?'/首次读取':'/回退读回');});try{
 const doc=dom.window.document;doc.querySelector('[data-testid=monitor-next]').click();await tick();
 dom.window.history.back();await new Promise(r=>setTimeout(r,30));await tick();assert.match(doc.querySelector('tbody').textContent,/回退读回/);
 finishOld();await tick();await tick();assert.match(doc.querySelector('tbody').textContent,/回退读回/);assert.doesNotMatch(doc.querySelector('tbody').textContent,/旧响应/);
 }finally{finishOld?.();dom.window.close();}
});
test('monitor run drawer separates clocks and pages exact events without treating missing metadata as success',async()=>{
 const run='a'.repeat(32);let fail=false;
 const {dom,requests}=await render(async raw=>{
 if(raw.includes('/events?')){if(fail)return {ok:false,status:503,text:async()=> '事件读取失败'};const older=new URL(raw,'http://localhost').searchParams.has('before_sequence');return {ok:true,json:async()=>({events:[{sequence:older?'9':'9007199254740993',run_id:run,kind:older?'scan_started':'scan_interrupted',recorded_at_utc:'2026-11-03T03:00:00.123456Z',cloud_observed_at_utc:'2026-11-03T03:00:01Z',error_code:'<unsafe>'}],has_more:!older,next_before_sequence:older?null:'9007199254740993'})};}
 return {ok:true,json:async()=>({runs:[{sequence:'10',run_id:run,status:'interrupted',monitor_path:'/目录',started_at_utc:'2026-11-03T03:00:00Z',recorded_at_utc:'2026-11-03T03:00:02Z'}],has_more:false,next_before_sequence:null})};
 });try{
 const doc=dom.window.document,open=doc.querySelector('[data-testid=monitor-details]');assert.ok(open);open.click();await tick();await tick();
 let drawer=doc.querySelector('[aria-labelledby=monitor-detail-title]');assert.ok(drawer);assert.match(drawer.textContent,/未判定成功/);assert.match(drawer.textContent,/本机开始 UTC.*2026-11-03T03:00:00Z/s);assert.match(drawer.textContent,/本机结束 UTC.*未记录/s);
 assert.match(drawer.textContent,/9007199254740993/);assert.equal(drawer.querySelector('unsafe'),null);
 fail=true;drawer.querySelector('[data-testid=monitor-event-next]').click();await tick();await tick();assert.match(drawer.textContent,/9007199254740993/);
 fail=false;drawer.querySelector('[data-testid=monitor-event-next]').click();await tick();await tick();assert.match(drawer.textContent,/scan_started/);
 const query=new URL(requests.at(-1).url,'http://localhost').searchParams;assert.equal(query.get('run_id'),run);assert.equal(query.get('sequence_format'),'string');assert.equal(query.get('before_sequence'),'9007199254740993');
 assert.equal(new URL(dom.window.location.href).searchParams.get('run_id'),run);
 doc.querySelector('[aria-label="关闭扫描详情"]').click();await tick();assert.equal(new URL(dom.window.location.href).searchParams.get('run_id'),null);
 }finally{dom.window.close();}
});
test('a deep-linked run outside the page never borrows another runs metadata',async()=>{
 const run='b'.repeat(32);const {dom}=await render(async raw=>raw.includes('/events?')?{ok:true,json:async()=>({events:[{sequence:'1',run_id:run,kind:'scan_started',recorded_at_utc:'2026-11-03T03:00:00Z'}],has_more:false,next_before_sequence:null})}:{ok:true,json:async()=>({runs:[{sequence:'1',run_id:'a'.repeat(32),status:'success',monitor_path:'/别的目录',started_at_utc:'2026-11-03T03:00:00Z'}],has_more:false,next_before_sequence:null})},'http://localhost/monitor-history?run_id='+run);try{
 const drawer=dom.window.document.querySelector('[aria-labelledby=monitor-detail-title]');assert.ok(drawer);assert.match(drawer.textContent,/未读取目录和起止元数据/);assert.doesNotMatch(drawer.textContent,/别的目录|本机开始 UTC/);assert.match(drawer.textContent,/scan_started/);
 }finally{dom.window.close();}
});
test('reopening a monitor run isolates a late event response from its old drawer',async()=>{
 let finishOld,calls=0;const run='a'.repeat(32),page=kind=>({ok:true,json:async()=>({events:[{sequence:'1',run_id:run,kind}],has_more:false,next_before_sequence:null})});
 const {dom}=await render(async raw=>{if(raw.includes('/events?')){if(++calls===1)return new Promise(resolve=>{finishOld=()=>resolve(page('old-event'));});return page('new-event');}return {ok:true,json:async()=>({runs:[{sequence:'1',run_id:run,status:'success',monitor_path:'/目录',started_at_utc:'2026-11-03T03:00:00Z'}],has_more:false,next_before_sequence:null})};});try{
 const doc=dom.window.document;doc.querySelector('[data-testid=monitor-details]').click();await tick();doc.querySelector('[aria-label="关闭扫描详情"]').click();await tick();doc.querySelector('[data-testid=monitor-details]').click();await tick();await tick();
 finishOld();await tick();await tick();const drawer=doc.querySelector('[aria-labelledby=monitor-detail-title]');assert.match(drawer.textContent,/new-event/);assert.doesNotMatch(drawer.textContent,/old-event/);
 }finally{finishOld?.();dom.window.close();}
});
test('monitor export binds the selected run and closing its drawer cancels a late download',async()=>{
 let finishBlob,exportCalls=0;const downloads=[],run='a'.repeat(32);
 const {dom,requests}=await render(async raw=>{if(raw.includes('/export')){exportCalls++;return {ok:true,blob:async()=>new Promise(resolve=>{finishBlob=()=>resolve({mock:true});})};}if(raw.includes('/events?'))return {ok:true,json:async()=>({events:[],has_more:false,next_before_sequence:null})};return {ok:true,json:async()=>({runs:[{sequence:'1',run_id:run,status:'success',monitor_path:'/目录',started_at_utc:'2026-11-03T03:00:00Z'}],has_more:false,next_before_sequence:null})};},undefined,w=>{w.URL.createObjectURL=()=> 'blob:mock';w.URL.revokeObjectURL=()=>{};w.HTMLAnchorElement.prototype.click=function(){downloads.push(this.download);};});try{
 const doc=dom.window.document;doc.querySelector('[data-testid=monitor-details]').click();await tick();await tick();const button=doc.querySelector('[data-testid=monitor-export]');assert.ok(button);button.click();button.click();await tick();assert.equal(exportCalls,1);assert.ok(requests.some(r=>r.url==='/api/monitor-scans/'+run+'/export'));
 doc.querySelector('[aria-label="关闭扫描详情"]').click();await tick();finishBlob();await tick();await tick();assert.equal(downloads.length,0);
 }finally{finishBlob?.();dom.window.close();}
});
test('monitor export reports success and server rejection in its visible drawer',async()=>{
 for(const fail of [false,true]){const run='a'.repeat(32),downloads=[];
 const {dom}=await render(async raw=>raw.includes('/export')?(fail?{ok:false,status:413,text:async()=> '事件过多'}:{ok:true,blob:async()=>({mock:true})}):raw.includes('/events?')?{ok:true,json:async()=>({events:[],has_more:false,next_before_sequence:null})}:{ok:true,json:async()=>({runs:[{sequence:'1',run_id:run,status:'success',monitor_path:'/目录'}],has_more:false,next_before_sequence:null})},undefined,w=>{w.URL.createObjectURL=()=> 'blob:mock';w.URL.revokeObjectURL=()=>{};w.HTMLAnchorElement.prototype.click=function(){downloads.push(this.download);};});try{
 const doc=dom.window.document;doc.querySelector('[data-testid=monitor-details]').click();await tick();await tick();doc.querySelector('[data-testid=monitor-export]').click();await tick();await tick();
 if(fail){assert.match(doc.querySelector('[role=alert]').textContent,/导出失败.*事件过多/);assert.equal(downloads.length,0);}else{assert.deepEqual(downloads,['queue-monitor-scan-'+run+'.json']);assert.match(doc.body.textContent,/已开始下载/);}
 }finally{dom.window.close();}}
});
test('monitor authorization expiry stops detail fallbacks and does not download evidence',async()=>{
 for(const mode of ['list','events','export']){const run='a'.repeat(32);let navigations=0,downloads=0;const console=new VirtualConsole();console.on('jsdomError',e=>{assert.match(e.message,/Not implemented: navigation/);navigations++;});
 const {dom,requests}=await render(async raw=>{if(mode==='list'||(mode==='events'&&raw.includes('/events?'))||(mode==='export'&&raw.includes('/export')))return {ok:false,status:401,text:async()=> 'unauthorized'};if(raw.includes('/events?'))return {ok:true,json:async()=>({events:[],has_more:false,next_before_sequence:null})};return {ok:true,json:async()=>({runs:[{sequence:'1',run_id:run,status:'success',monitor_path:'/目录'}],has_more:false,next_before_sequence:null})};},'http://localhost/monitor-history?run_id='+run,w=>{w.HTMLAnchorElement.prototype.click=()=>downloads++;},console);try{
 const doc=dom.window.document;if(mode==='export'){doc.querySelector('[data-testid=monitor-export]').click();await tick();await tick();}
 assert.match(doc.querySelector('[role=alert]').textContent,/请重新登录/);assert.equal(navigations,1,mode);assert.equal(downloads,0);
 if(mode==='list'){assert.equal(requests.length,1);assert.equal(doc.querySelector('main').hasAttribute('inert'),false);}else assert.equal(doc.querySelector('[data-testid=monitor-export]').disabled,true);
 }finally{dom.window.close();}}
});


test('monitor event titles use Chinese labels while preserving exact event codes',async()=>{
 const kinds=['completed','cloud_listed','started','failed','interrupted','future_kind'],labels=['本轮巡检完成','已读取网盘目录','开始巡检','本轮巡检失败','进程中断（未判定成功）','future_kind'],run='a'.repeat(32);
 const {dom,requests}=await render(async raw=>({ok:true,json:async()=>raw.includes('/events?')?{events:kinds.map((kind,i)=>({run_id:run,sequence:String(6-i),kind,recorded_at_utc:'2026-11-06T20:00:00Z'})),has_more:false,next_before_sequence:null}:{runs:[{sequence:'1',run_id:run,status:'success',monitor_path:'/test',started_at_utc:'2026-11-06T20:00:00Z'}],has_more:false,next_before_sequence:null}}));
 try{const d=dom.window.document;d.querySelector('[data-testid=monitor-details]').click();await tick();await tick();const rows=[...d.querySelectorAll('.timeline li')];assert.equal(rows.length,6);rows.forEach((row,i)=>{assert.equal(row.querySelector('strong').textContent,labels[i]);assert.match(row.querySelector('details').textContent,/事件代码/);assert.ok(row.querySelector('details').textContent.includes(kinds[i]));});assert.ok(requests.every(r=>r.method==='GET'));}finally{dom.window.close();}
});
