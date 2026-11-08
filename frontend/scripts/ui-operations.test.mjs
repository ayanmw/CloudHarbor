import {mountBuiltWorkspace} from './built-workspace.mjs';
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {JSDOM,VirtualConsole} from 'jsdom';
const html=await readFile(new URL('../../internal/webui/app/index.html',import.meta.url),'utf8');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
async function render(respond,url='http://localhost/operations',beforeMount,virtualConsole){
 const requests=[],dom=new JSDOM(html,{url,runScripts:'outside-only',pretendToBeVisual:true,...(virtualConsole?{virtualConsole}:{})});
 dom.window.fetch=async(raw,init={})=>{requests.push({url:String(raw),method:init.method||'GET'});return respond?respond(String(raw)):{ok:true,json:async()=>({operations:[{operation_id:'a'.repeat(32),kind:'move',status:'unknown',object_id:'9007199254740993',source_path:'/原目录',destination_path:'/目标目录',created_at_utc:'2026-11-03T03:00:00.123456Z'}],total:1,next_cursor:null})};};
 beforeMount?.(dom.window);await mountBuiltWorkspace(dom);await tick();await tick();return {dom,requests};
}
test('operation history mounts a compact read-only table and keeps unknown operations quarantined',async()=>{
 const {dom,requests}=await render();try{
 const doc=dom.window.document;assert.match(doc.title,/操作历史/);assert.match(doc.querySelector('h1').textContent,/操作历史/);assert.equal(doc.querySelectorAll('tbody tr').length,1);
 assert.match(doc.querySelector('tbody').textContent,/结果未知/);assert.match(doc.querySelector('tbody').textContent,/原目录/);assert.match(doc.querySelector('tbody').textContent,/目标目录/);
 assert.doesNotMatch(doc.querySelector('tbody').textContent,/重试|重新发送|解除隔离/);
 for(const name of ['status','kind','q','from','to'])assert.ok(doc.querySelector(`[name="${name}"]`),name);
 assert.ok(requests.every(r=>r.method==='GET'&&r.url.startsWith('/api/cloud/operations?')));
 assert.equal(new URL(requests[0].url,'http://localhost').searchParams.get('identity_format'),'string');assert.match(doc.querySelector('tbody').textContent,/9007199254740993/);
 for(const link of doc.querySelectorAll('.sidebar nav a')){assert.ok(link.querySelector('svg'));assert.ok(link.getAttribute('aria-label'));}
 }finally{dom.window.close();}
});
test('operation history failure is explicit rather than an empty successful history',async()=>{
 const {dom}=await render(async()=>({ok:false,status:503,text:async()=> '操作历史暂不可读'}));try{const doc=dom.window.document;assert.match(doc.querySelector('[role=alert]').textContent,/操作历史暂不可读/);assert.doesNotMatch(doc.body.textContent,/没有操作记录/);}finally{dom.window.close();}
});
test('operation filters preserve nanosecond UTC and keep draft searches out of refresh requests',async()=>{
 const {dom,requests}=await render();try{
 const doc=dom.window.document,set=(name,value)=>{const input=doc.querySelector(`[name="${name}"]`);input.value=value;input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));};
 set('q','  目录 😀  ');set('from','2026-11-03T03:00:00.123456789Z');set('to','2026-11-03T03:00:00.123456789Z');
 doc.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();
 const query=new URL(requests.at(-1).url,'http://localhost').searchParams;assert.equal(query.get('q'),'目录 😀');assert.equal(query.get('from'),'2026-11-03T03:00:00.123456789Z');assert.equal(query.get('to'),query.get('from'));
 set('q','未提交');[...doc.querySelectorAll('button')].find(b=>b.textContent.includes('刷新记录')).click();await tick();await tick();assert.equal(new URL(requests.at(-1).url,'http://localhost').searchParams.get('q'),'目录 😀');
 }finally{dom.window.close();}
});
test('invalid operation time and query inputs retain rows without issuing another request',async()=>{
 for(const [name,value] of [['q','😀'.repeat(129)],['from','2026-02-30T00:00:00Z'],['from','2026-11-03T24:00:00Z'],['from','2262-04-11T23:47:16.854775808Z'],['from','1677-09-21T00:12:43.145224191Z'],['from','2026-11-03T03:00:00.1234567891Z']]){
 const {dom,requests}=await render();try{const doc=dom.window.document,input=doc.querySelector(`[name="${name}"]`);input.value=value;input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));const before=requests.length;doc.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();assert.equal(requests.length,before,value);assert.ok(doc.querySelector('[role=alert]'));assert.equal(doc.querySelectorAll('tbody tr').length,1);}finally{dom.window.close();}
 }
});
test('operation filter accepts inclusive int64 time boundaries and 128 Unicode characters',async()=>{
 for(const boundary of ['1677-09-21T00:12:43.145224192Z','2262-04-11T23:47:16.854775807Z']){
 const {dom,requests}=await render();try{const doc=dom.window.document;for(const [name,value] of [['from',boundary],['to',boundary],['q','😀'.repeat(128)]]){const input=doc.querySelector(`[name="${name}"]`);input.value=value;input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));}doc.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();const query=new URL(requests.at(-1).url,'http://localhost').searchParams;assert.equal(query.get('from'),boundary);assert.equal(query.get('to'),boundary);assert.equal(Array.from(query.get('q')).length,128);assert.equal(doc.querySelector('[role=alert]'),null);}finally{dom.window.close();}
 }
});
test('operation URL restores precise combined boundaries and submitted conditions without forwarding actions',async()=>{
 const params=new URLSearchParams({q:'目录 & 😀',status:'unknown',before_created_at_utc:'2026-11-03T03:00:00.123456789Z',before_operation_id:'b'.repeat(32),operation_id:'c'.repeat(32),action:'delete',admin_key:'discard'});
 const {dom,requests}=await render(undefined,'http://localhost/operations?'+params+'#discard');try{
 const query=new URL(requests[0].url,'http://localhost').searchParams;assert.equal(query.get('q'),'目录 & 😀');assert.equal(query.get('before_created_at_utc'),'2026-11-03T03:00:00.123456789Z');assert.equal(query.get('before_operation_id'),'b'.repeat(32));assert.equal(query.get('operation_id'),null);assert.equal(query.get('action'),null);assert.equal(dom.window.location.hash,'');
 const doc=dom.window.document,input=doc.querySelector('[name=q]');input.value='新查询';input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));doc.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();assert.equal(new URL(requests.at(-1).url,'http://localhost').searchParams.get('before_operation_id'),null);
 dom.window.history.back();await new Promise(r=>setTimeout(r,30));await tick();assert.equal(doc.querySelector('[name=q]').value,'目录 & 😀');assert.ok(requests.every(r=>r.method==='GET'));
 }finally{dom.window.close();}
});
test('operation URL rejects incomplete or invalid cursors and duplicate filters before fetching',async()=>{
 for(const search of ['before_operation_id='+'a'.repeat(32),'before_created_at_utc=2026-11-03T00%3A00%3A00Z','status=unknown&status=confirmed','operation_id=bad','kind=invalid']){
 const {dom,requests}=await render(undefined,'http://localhost/operations?'+search);try{assert.equal(requests.length,0,search);assert.ok(dom.window.document.querySelector('[role=alert]'));}finally{dom.window.close();}
 }
});
test('operation keyset pagination preserves nanoseconds and same-time ID ordering after a failed read',async()=>{
 let fail=false;const time='2026-11-03T03:00:00.123456789Z';
 const {dom,requests}=await render(async raw=>{if(fail)return {ok:false,status:503,text:async()=> '分页失败'};const older=new URL(raw,'http://localhost').searchParams.has('before_operation_id'),id=(older?'b':'c').repeat(32);return {ok:true,json:async()=>({operations:[{operation_id:id,kind:'move',status:'unknown',source_path:older?'/较早':'/最新',destination_path:'/目标',created_at_utc:time}],total:2,next_cursor:older?null:{created_at_utc:time,operation_id:id}})};});try{
 const doc=dom.window.document,next=doc.querySelector('[data-testid=operation-next]');assert.ok(next);fail=true;next.click();await tick();await tick();assert.match(doc.querySelector('tbody').textContent,/最新/);fail=false;next.click();await tick();await tick();const query=new URL(requests.at(-1).url,'http://localhost').searchParams;assert.equal(query.get('before_created_at_utc'),time);assert.equal(query.get('before_operation_id'),'c'.repeat(32));assert.match(doc.querySelector('tbody').textContent,/较早/);doc.querySelector('[data-testid=operation-prev]').click();await tick();await tick();assert.match(doc.querySelector('tbody').textContent,/最新/);
 }finally{dom.window.close();}
});
test('operation browser back isolates a late older-page response',async()=>{
 let finishOld,firstReads=0;const time='2026-11-03T03:00:00.123456789Z';
 const response=(id,path,more)=>({ok:true,json:async()=>({operations:[{operation_id:id,kind:'move',status:'unknown',source_path:path,destination_path:'/目标',created_at_utc:time}],total:3,next_cursor:more?{created_at_utc:time,operation_id:id}:null})});
 const {dom}=await render(async raw=>{if(new URL(raw,'http://localhost').searchParams.has('before_operation_id'))return new Promise(resolve=>{finishOld=()=>resolve(response('b'.repeat(32),'/旧页',false));});return response('c'.repeat(32),++firstReads===1?'/首次':'/回退',true);});try{
 const doc=dom.window.document;doc.querySelector('[data-testid=operation-next]').click();await tick();dom.window.history.back();await new Promise(r=>setTimeout(r,30));await tick();assert.match(doc.querySelector('tbody').textContent,/回退/);finishOld();await tick();await tick();assert.match(doc.querySelector('tbody').textContent,/回退/);assert.doesNotMatch(doc.querySelector('tbody').textContent,/旧页/);
 }finally{finishOld?.();dom.window.close();}
});
test('an operation cursor not matching the last row is rejected while visible rows remain',async()=>{
 const time='2026-11-03T03:00:00.123456789Z';
 const {dom}=await render(async raw=>{const older=new URL(raw,'http://localhost').searchParams.has('before_operation_id');return {ok:true,json:async()=>({operations:[{operation_id:(older?'b':'c').repeat(32),kind:'move',status:'unknown',source_path:older?'/错误页':'/原页',destination_path:'/目标',created_at_utc:time}],total:3,next_cursor:{created_at_utc:time,operation_id:'c'.repeat(32)}})};});try{
 const doc=dom.window.document;doc.querySelector('[data-testid=operation-next]').click();await tick();await tick();assert.match(doc.querySelector('[role=alert]').textContent,/组合游标无效/);assert.match(doc.querySelector('tbody').textContent,/原页/);assert.doesNotMatch(doc.querySelector('tbody').textContent,/错误页/);
 }finally{dom.window.close();}
});
test('operation identity rendering never converts a rounded number into an exact ID',async()=>{
 for(const value of [9007199254740992,'0']){const {dom}=await render(async()=>({ok:true,json:async()=>({operations:[{operation_id:'a'.repeat(32),object_id:value,kind:'move',status:'unknown',source_path:'/原目录',destination_path:'/目标',created_at_utc:'2026-11-03T03:00:00Z'}],total:1,next_cursor:null})}));try{const text=dom.window.document.querySelector('tbody').textContent;if(typeof value==='number'){assert.match(text,/未提供精确对象ID/);assert.doesNotMatch(text,/9007199254740992/);}else assert.match(text,/未记录有效对象ID（0）/);}finally{dom.window.close();}}
});
test('operation drawer reads exact evidence text and submits event filters without writes',async()=>{
 const {dom,requests}=await render(async raw=>raw.includes('/events?')?{ok:true,json:async()=>({events:[{sequence:'9007199254740993',status:'unknown',recorded_at_utc:'2026-11-03T03:00:00.123456789Z',evidence_json:'{"fs_id":9007199254740993,"event":"unknown_cache_recovery","path":"<img src=x>"}'}],next_before_sequence:null})}:{ok:true,json:async()=>({operations:[{operation_id:'a'.repeat(32),object_id:'9007199254740993',kind:'move',status:'unknown',source_path:'/源',destination_path:'/目标',created_at_utc:'2026-11-03T03:00:00Z'}],total:1,next_cursor:null})});try{
 const doc=dom.window.document,open=doc.querySelector('[data-testid=operation-details]');assert.ok(open);open.click();await tick();await tick();const drawer=doc.querySelector('[aria-labelledby=operation-detail-title]');assert.ok(drawer);assert.match(drawer.textContent,/9007199254740993/);assert.match(drawer.textContent,/unknown 隔离仍保留/);assert.equal(drawer.querySelector('img'),null);assert.equal(drawer.querySelector('details').open,false);
 assert.match(drawer.querySelector('pre').textContent,/"fs_id":9007199254740993/);
 const input=drawer.querySelector('[name=event_status]');input.value='unknown';input.dispatchEvent(new dom.window.Event('change',{bubbles:true}));drawer.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();const query=new URL(requests.at(-1).url,'http://localhost').searchParams;assert.equal(query.get('status'),'unknown');assert.equal(query.get('identity_format'),'string');assert.ok(requests.every(r=>r.method==='GET'));
 }finally{dom.window.close();}
});
test('failed event filter does not allow an old cursor to page through the new condition',async()=>{
 const {dom}=await render(async raw=>{if(raw.includes('/events?')){const query=new URL(raw,'http://localhost').searchParams;if(query.get('status')==='failed')return {ok:false,status:503,text:async()=> '事件条件读取失败'};return {ok:true,json:async()=>({events:[{sequence:'100',status:'unknown',recorded_at_utc:'2026-11-03T03:00:00Z',evidence_json:'{}'}],next_before_sequence:'100'})};}return {ok:true,json:async()=>({operations:[{operation_id:'a'.repeat(32),kind:'move',status:'unknown',source_path:'/源',destination_path:'/目标',created_at_utc:'2026-11-03T03:00:00Z'}],total:1,next_cursor:null})};});try{
 const doc=dom.window.document;doc.querySelector('[data-testid=operation-details]').click();await tick();await tick();const drawer=doc.querySelector('[aria-labelledby=operation-detail-title]'),input=drawer.querySelector('[name=event_status]');input.value='failed';input.dispatchEvent(new dom.window.Event('change',{bubbles:true}));drawer.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();assert.match(drawer.textContent,/事件条件读取失败/);assert.match(drawer.querySelector('.timeline').textContent,/#100/);assert.equal(drawer.querySelector('[data-testid=operation-event-next]').disabled,true);
 }finally{dom.window.close();}
});
test('operation event paging preserves exact cursors and ignores a response from a closed drawer',async()=>{
 let finishOld,reads=0;
 const eventPage=(sequence,next)=>({ok:true,json:async()=>({events:[{sequence,status:'unknown',recorded_at_utc:'2026-11-03T03:00:00Z',evidence_json:'{}'}],next_before_sequence:next})});
 const {dom,requests}=await render(async raw=>{if(raw.includes('/events?')){if(++reads===1)return new Promise(resolve=>{finishOld=()=>resolve(eventPage('888',null));});const older=new URL(raw,'http://localhost').searchParams.has('before_sequence');return eventPage(older?'9007199254740991':'9007199254740993',older?null:'9007199254740993');}return {ok:true,json:async()=>({operations:[{operation_id:'a'.repeat(32),kind:'move',status:'unknown',source_path:'/源',destination_path:'/目标',created_at_utc:'2026-11-03T03:00:00Z'}],total:1,next_cursor:null})};});try{
 const doc=dom.window.document;doc.querySelector('[data-testid=operation-details]').click();await tick();doc.querySelector('[aria-label="关闭操作详情"]').click();await tick();doc.querySelector('[data-testid=operation-details]').click();await tick();await tick();doc.querySelector('[data-testid=operation-event-next]').click();await tick();await tick();assert.equal(new URL(requests.at(-1).url,'http://localhost').searchParams.get('before_sequence'),'9007199254740993');finishOld();await tick();await tick();const text=doc.querySelector('.timeline').textContent;assert.match(text,/9007199254740991/);assert.doesNotMatch(text,/#888/);
 }finally{finishOld?.();dom.window.close();}
});
test('deep-linked operation evidence never borrows another operations metadata',async()=>{
 const {dom}=await render(async raw=>raw.includes('/events?')?{ok:true,json:async()=>({events:[],next_before_sequence:null})}:{ok:true,json:async()=>({operations:[{operation_id:'a'.repeat(32),kind:'move',status:'confirmed',source_path:'/别的来源',destination_path:'/别的目标',created_at_utc:'2026-11-03T03:00:00Z'}],total:1,next_cursor:null})},'http://localhost/operations?operation_id='+'c'.repeat(32));try{const drawer=dom.window.document.querySelector('[aria-labelledby=operation-detail-title]');assert.ok(drawer);assert.match(drawer.textContent,/不在当前列表页/);assert.doesNotMatch(drawer.textContent,/别的来源|别的目标|创建 UTC/);}finally{dom.window.close();}
});
const exportList=()=>({ok:true,json:async()=>({operations:[{operation_id:'a'.repeat(32),object_id:'9007199254740993',kind:'move',status:'unknown',source_path:'/源',destination_path:'/目标',created_at_utc:'2026-11-03T03:00:00Z'}],total:1,next_cursor:null})});
const exportEvents=()=>({ok:true,json:async()=>({events:[],next_before_sequence:null})});
const captureDownloads=downloads=>w=>{w.URL.createObjectURL=()=> 'blob:mock';w.URL.revokeObjectURL=()=>{};w.HTMLAnchorElement.prototype.click=function(){downloads.push(this.download);};};
test('operation summary export binds submitted filters and ignores a late blob after a new search',async()=>{
 const downloads=[];let finishBlob,calls=0;
 const {dom,requests}=await render(async raw=>raw.includes('/export')?(calls++,{ok:true,headers:{get:()=>''},blob:async()=>new Promise(resolve=>{finishBlob=()=>resolve({mock:true});})}):exportList(),'http://localhost/operations?q=old',captureDownloads(downloads));try{
 const doc=dom.window.document,input=doc.querySelector('[name=q]');input.value='new';input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));const button=doc.querySelector('[data-testid=operation-summary-export]');assert.ok(button);button.click();button.click();await tick();assert.equal(calls,1);const query=new URL(requests.find(r=>r.url.includes('/export')).url,'http://localhost').searchParams;assert.equal(query.get('q'),'old');for(const key of ['limit','before_created_at_utc','before_operation_id','identity_format'])assert.equal(query.get(key),null);
 doc.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();finishBlob();await tick();await tick();assert.equal(downloads.length,0);assert.doesNotMatch(doc.body.textContent,/已开始下载/);
 }finally{finishBlob?.();dom.window.close();}
});
test('operation full and filtered evidence exports use separate fixed contexts',async()=>{
 const downloads=[];const {dom,requests}=await render(async raw=>raw.includes('/export')?{ok:true,blob:async()=>({mock:true})}:raw.includes('/events?')?exportEvents():exportList(),undefined,captureDownloads(downloads));try{
 const doc=dom.window.document;doc.querySelector('[data-testid=operation-details]').click();await tick();await tick();const drawer=doc.querySelector('[aria-labelledby=operation-detail-title]');
 const status=drawer.querySelector('[name=event_status]');status.value='unknown';status.dispatchEvent(new dom.window.Event('change',{bubbles:true}));drawer.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();
 drawer.querySelector('[data-testid=operation-full-export]').click();await tick();await tick();drawer.querySelector('[data-testid=operation-filtered-export]').click();await tick();await tick();
 assert.deepEqual(downloads,['remote-operation-'+'a'.repeat(32)+'.json','remote-operation-events-'+'a'.repeat(32)+'.json']);const urls=requests.filter(r=>r.url.includes('/export')).map(r=>r.url);assert.equal(urls[0],'/api/cloud/operations/'+'a'.repeat(32)+'/export');const query=new URL(urls[1],'http://localhost').searchParams;assert.equal(query.get('status'),'unknown');assert.equal(query.get('before_sequence'),null);assert.equal(query.get('identity_format'),null);
 }finally{dom.window.close();}
});
test('all operation export modes report server rejection in the visible context',async()=>{
 for(const mode of ['summary','full','filtered']){const downloads=[];const {dom}=await render(async raw=>raw.includes('/export')?{ok:false,status:413,text:async()=> '请缩小范围'}:raw.includes('/events?')?exportEvents():exportList(),undefined,captureDownloads(downloads));try{
 const doc=dom.window.document;if(mode!=='summary'){doc.querySelector('[data-testid=operation-details]').click();await tick();await tick();}doc.querySelector(`[data-testid=operation-${mode==='summary'?'summary':mode==='full'?'full':'filtered'}-export]`).click();await tick();await tick();const context=mode==='summary'?doc:doc.querySelector('[aria-labelledby=operation-detail-title]');assert.match(context.querySelector('[role=alert]').textContent,/导出失败.*缩小范围/);assert.equal(downloads.length,0);
 }finally{dom.window.close();}}
});
test('operation authorization expiry never falls back or downloads evidence',async()=>{
 for(const mode of ['list','events','summary','full','filtered']){let navigations=0;const downloads=[],console=new VirtualConsole();console.on('jsdomError',e=>{assert.match(e.message,/Not implemented: navigation/);navigations++;});
 const {dom,requests}=await render(async raw=>{if(mode==='list'||(mode==='events'&&raw.includes('/events?'))||(['summary','full','filtered'].includes(mode)&&raw.includes('/export')))return {ok:false,status:401,text:async()=> 'unauthorized'};return raw.includes('/events?')?exportEvents():exportList();},'http://localhost/operations'+(mode==='list'?'?operation_id='+'a'.repeat(32):''),captureDownloads(downloads),console);try{
 const doc=dom.window.document;if(mode==='events'||mode==='full'||mode==='filtered'){doc.querySelector('[data-testid=operation-details]').click();await tick();await tick();}if(['summary','full','filtered'].includes(mode)){doc.querySelector(`[data-testid=operation-${mode}-export]`).click();await tick();await tick();}
 assert.match(doc.querySelector('[role=alert]').textContent,/请重新登录/);assert.equal(navigations,1,mode);assert.equal(downloads.length,0);if(mode==='list'){assert.equal(requests.length,1);assert.equal(doc.querySelector('main').hasAttribute('inert'),false);}else if(mode==='summary')assert.equal(doc.querySelector('[data-testid=operation-summary-export]').disabled,true);else assert.equal(doc.querySelector('[data-testid=operation-full-export]').disabled,true);
 }finally{dom.window.close();}}
});
test('closing or changing operation event filters cancels a late evidence download',async()=>{
 for(const mode of ['full','filtered']){const downloads=[];let finishBlob,calls=0;
 const {dom}=await render(async raw=>raw.includes('/export')?(calls++,{ok:true,blob:async()=>new Promise(resolve=>{finishBlob=()=>resolve({mock:true});})}):raw.includes('/events?')?exportEvents():exportList(),undefined,captureDownloads(downloads));try{
 const doc=dom.window.document;doc.querySelector('[data-testid=operation-details]').click();await tick();await tick();const button=doc.querySelector(`[data-testid=operation-${mode}-export]`);button.click();button.click();await tick();assert.equal(calls,1);
 if(mode==='full'){doc.querySelector('[aria-label="关闭操作详情"]').click();await tick();}else{const drawer=doc.querySelector('[aria-labelledby=operation-detail-title]'),input=drawer.querySelector('[name=event_status]');input.value='unknown';input.dispatchEvent(new dom.window.Event('change',{bubbles:true}));drawer.querySelector('form').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));await tick();await tick();}
 finishBlob();await tick();await tick();assert.equal(downloads.length,0);assert.doesNotMatch(doc.body.textContent,/已开始下载/);
 }finally{finishBlob?.();dom.window.close();}}
});
test('operation summary export starts a named download on success',async()=>{
 const downloads=[];const {dom}=await render(async raw=>raw.includes('/export')?{ok:true,headers:{get:()=> 'attachment; filename="remote-operations-test.json"'},blob:async()=>({mock:true})}:exportList(),undefined,captureDownloads(downloads));try{const doc=dom.window.document;doc.querySelector('[data-testid=operation-summary-export]').click();await tick();await tick();assert.deepEqual(downloads,['remote-operations-test.json']);assert.match(doc.body.textContent,/导出时的实时记录/);}finally{dom.window.close();}
});


test('legacy cache observation is explicitly unverified',async()=>{
 const old='1970-01-21T18:19:51.158141465Z',now='2026-11-06T18:52:38.141465789Z';
 const evidence='{"event":"cache_refresh_reconciled"}';
 const {dom,requests}=await render(async url=>({ok:true,json:async()=>url.includes('/events?')?{events:[{sequence:'1',status:'confirmed',recorded_at_utc:now,response_observed_at_utc:old,evidence_json:evidence}],next_before_sequence:null}:{operations:[{operation_id:'a'.repeat(32),kind:'move',status:'confirmed',source_path:'/test/a',destination_path:'/test/b',created_at_utc:now}],total:1,next_cursor:null}}));
 try{const doc=dom.window.document;doc.querySelector('[data-testid=operation-details]').click();await tick();await tick();const row=doc.querySelector('.timeline li');assert.match(row.textContent,/历史时间单位未经核实/);assert.doesNotMatch(row.textContent,/响应观察 UTC：/);assert.ok(row.textContent.includes(old));assert.equal(row.querySelector('pre').textContent,evidence);assert.ok(requests.every(r=>r.method==='GET'));}finally{dom.window.close();}
});


test('new cache observations and other historical events keep their recorded UTC',async()=>{
 for(const [event,time] of [['cache_refresh_reconciled','2026-11-06T18:52:38.141465789Z'],['provider_confirmed','1970-01-21T18:19:51.158141465Z']]){
 const evidence=JSON.stringify({event,...(event==='cache_refresh_reconciled'?{response_observed_at_utc:time}:{})});
 const {dom}=await render(async url=>({ok:true,json:async()=>url.includes('/events?')?{events:[{sequence:'1',status:'confirmed',recorded_at_utc:'2026-11-06T18:52:38Z',response_observed_at_utc:time,evidence_json:evidence}],next_before_sequence:null}:{operations:[{operation_id:'a'.repeat(32),kind:'move',status:'confirmed',source_path:'/test/a',destination_path:'/test/b',created_at_utc:'2026-11-06T18:52:38Z'}],total:1,next_cursor:null}}));
 try{const doc=dom.window.document;doc.querySelector('[data-testid=operation-details]').click();await tick();await tick();const row=doc.querySelector('.timeline li');assert.ok(row.textContent.includes('响应观察 UTC：'+time));assert.doesNotMatch(row.textContent,/单位未经核实/);assert.equal(row.querySelector('pre').textContent,evidence);}finally{dom.window.close();}
 }
});
