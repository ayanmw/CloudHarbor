import {mountBuiltWorkspace} from './built-workspace.mjs';
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {JSDOM} from 'jsdom';
const html=await readFile(new URL('../../internal/webui/app/index.html',import.meta.url),'utf8');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
function listing(directory='/',entries=[]){return {directory,queue_dir:'/queue',entries,offset:0,total:entries.length,next_offset:null};}
async function render(respond=async()=>({ok:true,json:async()=>listing()}),historyRespond=async()=>({ok:true,json:async()=>({records:[],total:0,next_cursor:null})}),options={}){
 const requests=[],dom=new JSDOM(html,{url:options.url||'http://localhost/intake',runScripts:'outside-only',pretendToBeVisual:true});
 dom.window.fetch=async(raw,init={})=>{requests.push({url:String(raw),method:init.method||'GET',body:init.body,hash:dom.window.location.hash});if(raw==='/api/oauth/status')return options.auth?options.auth():{ok:true,json:async()=>({state:'ready',ready:true})};return String(raw).startsWith('/api/intakes?')?historyRespond(String(raw),init):respond(String(raw),init);};
 await mountBuiltWorkspace(dom);await tick();await tick();return {dom,requests};
}
test('intake has a persistent directory table and parent navigation even for an empty directory',async()=>{
 const {dom,requests}=await render();try{const doc=dom.window.document;
 assert.match(doc.querySelector('h1').textContent,/添加网盘项目/);
 assert.ok(doc.querySelector('[data-testid=intake-browser] table'));assert.match(doc.querySelector('thead').textContent,/名称.*类型.*大小/);
 assert.match(doc.querySelector('[data-testid=intake-browser]').textContent,/此目录暂无项目/);
 assert.equal(doc.querySelector('[data-testid=intake-up]').disabled,true);
 assert.ok(requests.some(r=>r.url.startsWith('/api/cloud?')));assert.ok(requests.every(r=>r.method==='GET'));
 }finally{dom.window.close();}
});
test('recheck only verifies the selected batch and refreshes history while retaining unknown isolation',async()=>{
 const record=historyRecord();let finish;let reads=0;const {dom,requests}=await render(async raw=>raw.endsWith('/recheck')?new Promise(resolve=>finish=()=>resolve({ok:true,json:async()=>record})):{ok:true,json:async()=>listing()},async()=>{reads++;return historyResponse([record]);});try{const doc=dom.window.document;doc.querySelector('[data-testid=intake-record-details]').click();await tick();const button=doc.querySelector('[data-testid=intake-recheck]');button.click();button.click();await tick();assert.equal(requests.filter(r=>r.method==='POST').length,1);assert.match(requests.find(r=>r.method==='POST').url,/\/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\/recheck$/);finish();await tick();await tick();await tick();assert.ok(reads>=2);assert.match(doc.querySelector('[role=dialog]').textContent,/结果未知.*隔离/);assert.ok(!requests.some(r=>r.url.endsWith('/commit')));}finally{finish?.();dom.window.close();}
});
test('a late recheck response never reopens closed details or borrows a different batch identity',async()=>{
 let finish;const {dom}=await render(async raw=>raw.endsWith('/recheck')?new Promise(resolve=>finish=()=>resolve({ok:true,json:async()=>historyRecord()})):{ok:true,json:async()=>listing()},async()=>historyResponse([historyRecord(),historyRecord('b','/另一批')]));try{const doc=dom.window.document;doc.querySelector('[data-testid=intake-record-details]').click();await tick();doc.querySelector('[data-testid=intake-recheck]').click();await tick();doc.querySelector('[aria-label="关闭入队详情"]').click();await tick();finish();await tick();await tick();assert.equal(doc.querySelector('[role=dialog]'),null);}finally{finish?.();dom.window.close();}
});
test('commit completion refreshes the current directory and intake history without repeating a move',async()=>{
 let reads=0;const record=previewRecord();const {dom,requests}=await render(async raw=>({ok:true,json:async()=>raw.endsWith('/preview')?record:raw.endsWith('/commit')?{...record,state:'finished',items:record.items.map(i=>({...i,state:'moved'}))}:listing()}),async()=>{reads++;return historyResponse();});try{await prepare(dom);const initialReads=reads,initialDirectoryReads=requests.filter(r=>r.url.startsWith('/api/cloud?')).length,doc=dom.window.document;doc.querySelector('[data-testid=intake-confirm]').click();await tick();doc.querySelector('[data-testid=intake-commit]').click();await tick();await tick();await tick();assert.ok(reads>initialReads);assert.ok(requests.filter(r=>r.url.startsWith('/api/cloud?')).length>initialDirectoryReads);assert.equal(requests.filter(r=>r.url.endsWith('/commit')).length,1);}finally{dom.window.close();}
});
test('known unavailable account disables new previews and invalidates an existing confirmation on refresh',async()=>{
 let ready=true;const {dom,requests}=await render(async raw=>({ok:true,json:async()=>raw.endsWith('/preview')?previewRecord():listing()}),undefined,{auth:async()=>({ok:true,json:async()=>({ready,state:ready?'ready':'reauthorization_required'})})});try{await prepare(dom);ready=false;dom.window.document.querySelector('[data-testid=intake-auth-refresh]').click();await tick();await tick();const doc=dom.window.document;assert.match(doc.querySelector('[data-testid=intake-auth]').textContent,/重新授权/);assert.equal(doc.querySelector('[data-testid=intake-commit]'),null);assert.equal(doc.querySelector('[data-testid=intake-preview]').disabled,true);assert.ok(!requests.some(r=>r.url.endsWith('/commit')));}finally{dom.window.close();}
});
test('userscript handoff clears the fragment before requests and imports paths without preview or move',async()=>{
 const fragment='#nas-intake='+encodeURIComponent(JSON.stringify({version:1,paths:['/外部课程']}));const {dom,requests}=await render(undefined,undefined,{url:'http://localhost/intake'+fragment});try{assert.equal(dom.window.location.hash,'');assert.equal(dom.window.document.querySelector('[name=paths]').value,'/外部课程');assert.match(dom.window.document.querySelector('[data-testid=intake-selection-notice]').textContent,/尚未预检或移动/);assert.ok(requests.every(r=>r.hash===''&&r.method==='GET'));}finally{dom.window.close();}
});
test('invalid userscript handoff is cleared and never imported',async()=>{
 const {dom,requests}=await render(undefined,undefined,{url:'http://localhost/intake#nas-intake='+encodeURIComponent(JSON.stringify({version:1,paths:['/目录/../别处']}))});try{assert.equal(dom.window.location.hash,'');assert.equal(dom.window.document.querySelector('[name=paths]').value,'');assert.match(dom.window.document.querySelector('[data-testid=intake-selection-notice]').textContent,/无效/);assert.ok(requests.every(r=>r.method==='GET'));}finally{dom.window.close();}
});
test('failed recheck keeps the old evidence and never sends a commit or retries automatically',async()=>{
 const {dom,requests}=await render(async raw=>raw.endsWith('/recheck')?{ok:false,status:503,text:async()=>'核对暂不可用'}:{ok:true,json:async()=>listing()},async()=>historyResponse([historyRecord()]));try{const doc=dom.window.document;doc.querySelector('[data-testid=intake-record-details]').click();await tick();doc.querySelector('[data-testid=intake-recheck]').click();await tick();await tick();assert.match(doc.querySelector('[role=dialog]').textContent,/核对暂不可用.*不会重发移动/);assert.match(doc.querySelector('[role=dialog]').textContent,/9007199254740993/);assert.equal(requests.filter(r=>r.method==='POST').length,1);}finally{dom.window.close();}
});
test('known administrator session expiry blocks further preview requests',async()=>{
 const {dom,requests}=await render(undefined,undefined,{auth:async()=>({ok:false,status:401,text:async()=>'登录失效'})});try{const doc=dom.window.document,input=doc.querySelector('[name=paths]');input.value='/课程';input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));await tick();assert.equal(doc.querySelector('[data-testid=intake-preview]').disabled,true);assert.match(doc.querySelector('[data-testid=intake-auth]').textContent,/重新登录/);assert.ok(!requests.some(r=>r.method==='POST'));}finally{dom.window.close();}
});
test('a different batch commit must never appear as verification evidence for an earlier unknown batch',async()=>{
 const a=historyRecord(),b=previewRecord(['/课程'],{id:'b'.repeat(32)});
 const {dom}=await render(async raw=>({ok:true,json:async()=>raw.endsWith('/recheck')?a:raw.endsWith('/preview')?b:raw.endsWith('/commit')?{...b,state:'finished',items:b.items.map(i=>({...i,state:'moved'}))}:listing()}),async()=>historyResponse([a]));
 try{const doc=dom.window.document;doc.querySelector('[data-testid=intake-record-details]').click();await tick();doc.querySelector('[data-testid=intake-recheck]').click();await tick();await tick();doc.querySelector('[aria-label="关闭入队详情"]').click();await tick();await prepare(dom);doc.querySelector('[data-testid=intake-confirm]').click();await tick();doc.querySelector('[data-testid=intake-commit]').click();await tick();await tick();await tick();doc.querySelector('[data-testid=intake-record-details]').click();await tick();assert.match(doc.querySelector('[role=dialog]').textContent,/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/);assert.doesNotMatch(doc.querySelector('[role=dialog]').textContent,/移动结果已核对，后台将扫描/);}finally{dom.window.close();}
});
test('intake mounts inside the shared workspace layout with a styled main region',async()=>{
 const {dom}=await render();try{assert.ok(dom.window.document.querySelector('.workspace.event-workspace.intake-workspace > main.main'));}finally{dom.window.close();}
});
const historyResponse=(records=[],next=null)=>({ok:true,json:async()=>({records,total:records.length,next_cursor:next})});
const historyRecord=(id='a',path='/课程')=>previewRecord([path],{id:id.repeat(32),state:'finished',items:previewRecord([path]).items.map(item=>({...item,state:'unknown',error:'directory_completion_unconfirmed'}))});
test('intake history uses compact summaries and opens exact read-only batch evidence',async()=>{
 const record=historyRecord();const {dom}=await render(undefined,async()=>historyResponse([record]));try{const doc=dom.window.document,section=doc.querySelector('[data-testid=intake-history]');assert.equal(section.querySelectorAll('tbody tr').length,1);assert.match(section.textContent,/结果未知/);assert.match(section.textContent,/1 项/);section.querySelector('[data-testid=intake-record-details]').click();await tick();const dialog=doc.querySelector('[role=dialog]');assert.match(dialog.textContent,/9007199254740993/);assert.match(dialog.textContent,/缺少目录完成凭据/);assert.match(dialog.textContent,new RegExp(record.created.replace(/[.]/g,'\\.')));assert.doesNotMatch(dialog.textContent,/确认移动并加入队列|解除隔离|重新发送/);doc.querySelector('[aria-label="关闭入队详情"]').click();await tick();assert.equal(doc.querySelector('[role=dialog]'),null);}finally{dom.window.close();}
});
test('intake history passes opaque cursors unchanged and only commits successful page navigation',async()=>{
 const cursor='opaque_cursor-123';let fail=true;const {dom,requests}=await render(undefined,async raw=>{const query=new URL(raw,'http://localhost').searchParams;if(query.has('cursor'))return fail?{ok:false,status:503,text:async()=>'历史读取失败'}:historyResponse([historyRecord('b','/较早')]);return historyResponse([historyRecord('a','/原页')],cursor);});try{const doc=dom.window.document,next=()=>doc.querySelector('[data-testid=intake-history-next]').click();next();await tick();await tick();assert.match(doc.querySelector('[data-testid=intake-history]').textContent,/原页/);assert.equal(doc.querySelector('[data-testid=intake-history-prev]').disabled,true);fail=false;next();await tick();await tick();assert.match(doc.querySelector('[data-testid=intake-history]').textContent,/较早/);assert.equal(new URL(requests.at(-1).url,'http://localhost').searchParams.get('cursor'),cursor);doc.querySelector('[data-testid=intake-history-prev]').click();await tick();await tick();assert.match(doc.querySelector('[data-testid=intake-history]').textContent,/原页/);}finally{dom.window.close();}
});
test('failed history filter cannot mix the old cursor with new unresolved criteria',async()=>{
 const {dom}=await render(undefined,async raw=>new URL(raw,'http://localhost').searchParams.get('unresolved')==='1'?{ok:false,status:503,text:async()=>'待核对暂不可读'}:historyResponse([historyRecord('a','/原条件')],'older_cursor'));try{const doc=dom.window.document;doc.querySelector('[name=intake-unresolved]').click();await tick();await tick();assert.match(doc.querySelector('[data-testid=intake-history]').textContent,/原条件/);assert.equal(doc.querySelector('[data-testid=intake-history-next]').disabled,true);assert.match(doc.querySelector('[data-testid=intake-history-error]').textContent,/待核对暂不可读/);}finally{dom.window.close();}
});
test('late history results from an old filter cannot replace the newer filter',async()=>{
 let finish;let reads=0;const {dom}=await render(undefined,async raw=>{const unresolved=new URL(raw,'http://localhost').searchParams.get('unresolved')==='1';if(unresolved)return new Promise(resolve=>finish=()=>resolve(historyResponse([historyRecord('b','/旧筛选')])));return historyResponse([historyRecord('a',++reads===1?'/最初':'/新筛选')]);});try{const doc=dom.window.document,filter=doc.querySelector('[name=intake-unresolved]');filter.click();await tick();filter.click();await tick();await tick();finish();await tick();await tick();assert.match(doc.querySelector('[data-testid=intake-history]').textContent,/新筛选/);assert.doesNotMatch(doc.querySelector('[data-testid=intake-history]').textContent,/旧筛选/);}finally{finish?.();dom.window.close();}
});
function previewRecord(paths=['/课程'],extra={}){return {id:'a'.repeat(32),state:'preview',queue_dir:'/queue',download_mode:'delete',created:new Date().toISOString(),expires:new Date(Date.now()+120000).toISOString(),items:paths.map(path=>({source:{path,fs_id:9007199254740992},source_id:'9007199254740993',target:'/queue/'+path.split('/').at(-1),state:'ready'})),...extra};}
async function prepare(dom){const doc=dom.window.document,input=doc.querySelector('[name=paths]');input.value='/课程';input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));await tick();doc.querySelector('[data-testid=intake-preview]').click();await tick();await tick();}
test('preview shows fixed paths policy exact identity and requires explicit confirmation before a single commit',async()=>{
 let finish;const record=previewRecord();const {dom,requests}=await render(async(raw)=>raw.endsWith('/preview')?{ok:true,json:async()=>record}:raw.endsWith('/commit')?new Promise(resolve=>finish=()=>resolve({ok:true,json:async()=>({...record,state:'finished',items:record.items.map(i=>({...i,state:'moved'}))})})): {ok:true,json:async()=>listing()});
 try{await prepare(dom);const doc=dom.window.document,section=doc.querySelector('[data-testid=intake-preview-result]');assert.match(section.textContent,/9007199254740993/);assert.doesNotMatch(section.textContent,/9007199254740992/);assert.match(section.textContent,/\/queue\/课程/);assert.match(section.textContent,/删除网盘/);assert.equal(doc.querySelector('[data-testid=intake-commit]').disabled,true);
 const check=doc.querySelector('[data-testid=intake-confirm]');check.click();await tick();const button=doc.querySelector('[data-testid=intake-commit]');button.click();button.click();await tick();assert.equal(requests.filter(r=>r.url.endsWith('/commit')).length,1);assert.equal(doc.querySelector('[name=paths]').disabled,true);finish();await tick();await tick();assert.match(doc.querySelector('[data-testid=intake-result]').textContent,/已核对/);assert.equal(doc.querySelector('[data-testid=intake-commit]'),null);
 assert.deepEqual(JSON.parse(requests.find(r=>r.url.endsWith('/preview')).body),{paths:['/课程']});
 }finally{finish?.();dom.window.close();}
});
test('editing selection immediately discards a preview including a late preview response',async()=>{
 let finish;const {dom}=await render(async raw=>raw.endsWith('/preview')?new Promise(resolve=>finish=()=>resolve({ok:true,json:async()=>previewRecord()})):{ok:true,json:async()=>listing()});
 try{const doc=dom.window.document,input=doc.querySelector('[name=paths]');await prepare(dom);input.value='/其他';input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));await tick();finish();await tick();await tick();assert.equal(doc.querySelector('[data-testid=intake-commit]'),null);assert.match(doc.querySelector('[data-testid=intake-selection-notice]').textContent,/重新预检/);
 }finally{finish?.();dom.window.close();}
});
test('expired previews cannot be confirmed and never send a commit',async()=>{
 const {dom,requests}=await render(async raw=>({ok:true,json:async()=>raw.endsWith('/preview')?previewRecord(['/课程'],{expires:new Date(Date.now()-1000).toISOString()}):listing()}));
 try{await prepare(dom);const doc=dom.window.document;assert.match(doc.querySelector('[data-testid=intake-preview-result]').textContent,/已过期/);assert.equal(doc.querySelector('[data-testid=intake-commit]').disabled,true);assert.ok(!requests.some(r=>r.url.endsWith('/commit')));}finally{dom.window.close();}
});
test('interrupted commit consumes confirmation and directs verification without retrying the move',async()=>{
 const {dom,requests}=await render(async raw=>{if(raw.endsWith('/commit'))throw Error('连接中断');return {ok:true,json:async()=>raw.endsWith('/preview')?previewRecord():listing()};});
 try{await prepare(dom);const doc=dom.window.document;doc.querySelector('[data-testid=intake-confirm]').click();await tick();doc.querySelector('[data-testid=intake-commit]').click();await tick();await tick();assert.match(doc.querySelector('[data-testid=intake-result]').textContent,/连接中断.*核对.*不会.*重发/);assert.equal(doc.querySelector('[data-testid=intake-commit]'),null);assert.equal(requests.filter(r=>r.url.endsWith('/commit')).length,1);}finally{dom.window.close();}
});
test('a mismatched preview cannot confirm paths different from the submitted selection',async()=>{
 const {dom,requests}=await render(async raw=>({ok:true,json:async()=>raw.endsWith('/preview')?previewRecord(['/别的目录']):listing()}));try{await prepare(dom);assert.match(dom.window.document.querySelector('[data-testid=intake-action-error]').textContent,/预检.*不匹配/);assert.equal(dom.window.document.querySelector('[data-testid=intake-commit]'),null);assert.ok(!requests.some(r=>r.url.endsWith('/commit')));}finally{dom.window.close();}
});
test('an existing preview stays discarded after changing paths away and back',async()=>{
 const {dom}=await render(async raw=>({ok:true,json:async()=>raw.endsWith('/preview')?previewRecord():listing()}));try{await prepare(dom);const doc=dom.window.document,input=doc.querySelector('[name=paths]');for(const path of ['/别的','/课程']){input.value=path;input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));}await tick();assert.equal(doc.querySelector('[data-testid=intake-commit]'),null);assert.match(doc.querySelector('[data-testid=intake-selection-notice]').textContent,/重新预检/);}finally{dom.window.close();}
});
test('preview expires while displayed and confirmation is disabled without a submission',async()=>{
 const {dom,requests}=await render(async raw=>({ok:true,json:async()=>raw.endsWith('/preview')?previewRecord(['/课程'],{expires:new Date(Date.now()+100).toISOString()}):listing()}));try{await prepare(dom);await new Promise(resolve=>setTimeout(resolve,600));await tick();const doc=dom.window.document;assert.match(doc.querySelector('[data-testid=intake-preview-result]').textContent,/已过期/);assert.equal(doc.querySelector('[data-testid=intake-confirm]').disabled,true);assert.equal(doc.querySelector('[data-testid=intake-commit]').disabled,true);assert.ok(!requests.some(r=>r.url.endsWith('/commit')));}finally{dom.window.close();}
});
test('backend authentication-change rejection consumes the old confirmation and requires a new preview',async()=>{
 const {dom,requests}=await render(async raw=>raw.endsWith('/commit')?{ok:false,status:409,text:async()=>'入队预检已过期或认证已变化，请重新预检'}:{ok:true,json:async()=>raw.endsWith('/preview')?previewRecord():listing()});try{await prepare(dom);const doc=dom.window.document;doc.querySelector('[data-testid=intake-confirm]').click();await tick();doc.querySelector('[data-testid=intake-commit]').click();await tick();await tick();assert.match(doc.querySelector('[data-testid=intake-result]').textContent,/认证已变化.*重新预检/);assert.equal(doc.querySelector('[data-testid=intake-commit]'),null);assert.equal(requests.filter(r=>r.url.endsWith('/commit')).length,1);}finally{dom.window.close();}
});
test('intake separates directory type and file size and returns to the parent directory',async()=>{
 const dir={id:'9007199254740993',path:'/课程',name:'课程',directory:true,size:0,can_enqueue:true};
 const {dom,requests}=await render(async raw=>({ok:true,json:async()=>new URL(raw,'http://localhost').searchParams.get('dir')==='/'?listing('/',[dir,{...dir,id:'2',path:'/文件.txt',name:'文件.txt',directory:false,size:1024}]):listing('/课程')}));
 try{const doc=dom.window.document,rows=doc.querySelectorAll('tbody tr');assert.equal(rows.length,2);assert.equal(rows[0].querySelector('[data-testid=intake-size]').textContent,'—');assert.equal(rows[1].querySelector('[data-testid=intake-size]').textContent,'1 KiB');
 doc.querySelector('[data-testid=intake-open]').click();await tick();await tick();assert.match(doc.querySelector('[data-testid=intake-path]').textContent,/课程/);assert.equal(doc.querySelector('[data-testid=intake-up]').disabled,false);
 doc.querySelector('[data-testid=intake-up]').click();await tick();await tick();assert.equal(new URL(requests.at(-1).url,'http://localhost').searchParams.get('dir'),'/');
 }finally{dom.window.close();}
});
test('directory failure keeps the browser visible and never presents a successful empty directory',async()=>{
 const {dom}=await render(async()=>({ok:false,status:503,text:async()=>'网盘暂不可读'}));try{const doc=dom.window.document;assert.ok(doc.querySelector('[data-testid=intake-browser] table'));assert.match(doc.querySelector('[role=alert]').textContent,/网盘暂不可读/);assert.doesNotMatch(doc.body.textContent,/此目录暂无项目/);}finally{dom.window.close();}
});
test('a stale directory response cannot replace a later requested directory',async()=>{
 let finish;const {dom}=await render(async raw=>{const dir=new URL(raw,'http://localhost').searchParams.get('dir');if(dir==='/旧')return new Promise(resolve=>finish=()=>resolve({ok:true,json:async()=>listing('/旧')}));return {ok:true,json:async()=>listing(dir)};});
 try{const doc=dom.window.document,input=doc.querySelector('[name=directory]'),open=value=>{input.value=value;input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));doc.querySelector('[data-testid=intake-directory-form]').dispatchEvent(new dom.window.Event('submit',{bubbles:true,cancelable:true}));};
 open('/旧');await tick();open('/新');await tick();await tick();finish();await tick();await tick();assert.equal(doc.querySelector('[data-testid=intake-path]').textContent,'/新');
 }finally{finish?.();dom.window.close();}
});
test('intake selection survives directory changes and can be removed or cleared without remote writes',async()=>{
 const entry=(path,name,directory=true)=>({id:path,path,name,directory,size:0,can_enqueue:true});
 const {dom,requests}=await render(async raw=>{const dir=new URL(raw,'http://localhost').searchParams.get('dir');return {ok:true,json:async()=>listing(dir,dir==='/'?[entry('/课程','课程')]:[entry('/课程/资料.txt','资料.txt',false)])};});
 try{const doc=dom.window.document;doc.querySelector('[data-testid=intake-select]').click();await tick();doc.querySelector('[data-testid=intake-open]').click();await tick();await tick();doc.querySelector('[data-testid=intake-select]').click();await tick();
 assert.equal(doc.querySelectorAll('[data-testid=intake-selected-item]').length,2);assert.match(doc.querySelector('[name=paths]').value,/\/课程\n\/课程\/资料.txt/);
 doc.querySelector('[data-testid=intake-remove]').click();await tick();assert.equal(doc.querySelectorAll('[data-testid=intake-selected-item]').length,1);
 doc.querySelector('[data-testid=intake-clear]').click();await tick();assert.equal(doc.querySelector('[name=paths]').value,'');assert.equal(doc.querySelector('[data-testid=intake-select]').checked,false);assert.ok(requests.every(r=>r.method==='GET'));
 }finally{dom.window.close();}
});
test('manual paths synchronize directory checkboxes and reject more than twenty distinct selections',async()=>{
 const {dom}=await render(async()=>({ok:true,json:async()=>listing('/',[{id:'exact-string-id',path:'/课程',name:'课程',directory:true,size:0,can_enqueue:true}])}));
 try{const doc=dom.window.document,input=doc.querySelector('[name=paths]');const fill=value=>{input.value=value;input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));};
 fill('/课程\n/课程');await tick();assert.equal(doc.querySelectorAll('[data-testid=intake-selected-item]').length,1);assert.equal(doc.querySelector('[data-testid=intake-select]').checked,true);
 fill(Array.from({length:21},(_,i)=>'/'+i).join('\n'));await tick();assert.match(doc.querySelector('[data-testid=intake-selection-error]').textContent,/最多.*20/);assert.equal(doc.querySelector('[data-testid=intake-select]').disabled,true);
 fill('相对路径');await tick();assert.match(doc.querySelector('[data-testid=intake-selection-error]').textContent,/绝对/);
 }finally{dom.window.close();}
});
test('twenty selected paths cannot be expanded by browsing and disallowed items remain unavailable',async()=>{
 const {dom}=await render(async()=>({ok:true,json:async()=>listing('/',[{id:'x',path:'/新项目',name:'新项目',directory:true,size:0,can_enqueue:true},{id:'y',path:'/隔离',name:'隔离',directory:false,size:1,can_enqueue:false}])}));
 try{const doc=dom.window.document,input=doc.querySelector('[name=paths]');input.value=Array.from({length:20},(_,i)=>'/'+i).join('\n');input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));await tick();
 assert.equal(doc.querySelectorAll('[data-testid=intake-selected-item]').length,20);assert.ok([...doc.querySelectorAll('[data-testid=intake-select]')].every(c=>c.disabled));assert.match(doc.querySelector('tbody').textContent,/不可选/);
 doc.querySelector('[data-testid=intake-clear]').click();await tick();assert.equal(doc.querySelectorAll('[data-testid=intake-select]')[0].disabled,false);assert.equal(doc.querySelectorAll('[data-testid=intake-select]')[1].disabled,true);
 }finally{dom.window.close();}
});

test('download confirmation links retain the exact selected path without a second browser',async()=>{
 const {dom,requests}=await render(undefined,undefined,{url:'http://localhost/intake?path=%2F课程%2F第一期'});try{const doc=dom.window.document;assert.match(doc.querySelector('h1').textContent,/下载预检/);assert.equal(doc.querySelector('[data-testid=intake-browser]'),null);assert.equal(doc.querySelector('textarea').value,'/课程/第一期');assert.equal(doc.querySelector('[data-testid=intake-history]'),null);assert.ok(!requests.some(r=>r.url.startsWith('/api/cloud?')));}finally{dom.window.close();}
});
test('intake history has an independent menu and no download-selection form',async()=>{
 const {dom,requests}=await render(undefined,undefined,{url:'http://localhost/intake-history'});try{const doc=dom.window.document;assert.match(doc.querySelector('h1').textContent,/入队记录/);assert.equal(doc.querySelector('textarea'),null);assert.equal(doc.querySelector('[data-testid=intake-browser]'),null);assert.ok(requests.some(r=>r.url.startsWith('/api/intakes?')));assert.ok(!requests.some(r=>r.url.startsWith('/api/cloud?')));}finally{dom.window.close();}
});

test('multi-path download preflight keeps every selected path and never auto-submits',async()=>{const {dom,requests}=await render(undefined,undefined,{url:'http://localhost/intake?path=%2F一%20号&path=%2F二号'});try{assert.equal(dom.window.document.querySelector('textarea').value,'/一 号\n/二号');assert.ok(requests.every(r=>r.method==='GET'));assert.equal(dom.window.document.querySelector('[data-testid=intake-browser]'),null)}finally{dom.window.close()}});

test('invalid or oversized path selections are never split into different download intents',async()=>{for(const query of ['?path=%2Fa%0A%2Fb','?path=%2Fa&path=%2Fa','?'+Array.from({length:21},(_,i)=>'path=%2F'+i).join('&')]){const {dom,requests}=await render(undefined,undefined,{url:'http://localhost/intake'+query});try{assert.ok(dom.window.document.querySelector('[role=alert]'));assert.equal(dom.window.document.querySelector('textarea').value,'');assert.equal(dom.window.document.querySelector('[data-testid=intake-preview]').disabled,true);assert.ok(requests.every(r=>r.method==='GET'))}finally{dom.window.close()}}});

test('intake details distinguish receipt and local registration times without claiming download started',async()=>{
 const record=historyRecord();record.items[0]={...record.items[0],state:'moved',error:undefined,sync_confirmed:true,sync_confirmed_at_utc:'2026-11-05T01:02:03Z',queue_registered_at_utc:'2026-11-05T01:02:04Z'};
 const {dom}=await render(undefined,async()=>historyResponse([record]));
 try{const doc=dom.window.document;doc.querySelector('[data-testid=intake-record-details]').click();await tick();const text=doc.querySelector('[role=dialog]').textContent;assert.match(text,/2026-11-05T01:02:03Z/);assert.match(text,/2026-11-05T01:02:04Z/);assert.match(text,/不代表监控已发现/);}finally{dom.window.close();}
});
test('intake history rejects invalid stage timestamps and accepts old records without invented evidence',async()=>{
 const old=historyRecord();old.items[0]={...old.items[0],state:'moved',sync_confirmed:true,error:undefined};
 const {dom}=await render(undefined,async()=>historyResponse([old]));try{const doc=dom.window.document;doc.querySelector('[data-testid=intake-record-details]').click();await tick();assert.match(doc.querySelector('[role=dialog]').textContent,/历史时间未记录/);}finally{dom.window.close();}
 const bad=structuredClone(old);bad.items[0].queue_registered_at_utc='yesterday';
 const result=await render(undefined,async()=>historyResponse([bad]));try{assert.equal(result.dom.window.document.querySelector('[data-testid=intake-record-details]'),null);assert.match(result.dom.window.document.querySelector('[data-testid=intake-history-error]').textContent,/入队项目响应无效/);}finally{result.dom.window.close();}
});

test('intake task links retain exact object IDs and reject a different task identity',async()=>{
 const record=historyRecord();record.items[0]={...record.items[0],state:'moved',error:undefined,download_task:{task_id:record.items[0].source_id,status:'pending',observed_at_utc:'2026-11-05T01:02:04Z'}};
 const {dom}=await render(undefined,async()=>historyResponse([record]));try{const doc=dom.window.document;doc.querySelector('[data-testid=intake-record-details]').click();await tick();const dialog=doc.querySelector('[role=dialog]'),link=dialog.querySelector('a[href^="/?task="]');assert.equal(new URL(link.href).searchParams.get('task'),'9007199254740993');assert.match(dialog.textContent,/等待下载/);assert.match(dialog.textContent,/状态读取于/);}finally{dom.window.close();}
 record.items[0].download_task.task_id='123';const bad=await render(undefined,async()=>historyResponse([record]));try{assert.match(bad.dom.window.document.querySelector('[data-testid=intake-history-error]').textContent,/下载任务关联响应无效/);}finally{bad.dom.window.close();}
});

test('intake monitor evidence opens the exact scan and keeps historical observation separate from current state',async()=>{
 const record=historyRecord();record.items[0]={...record.items[0],state:'moved',error:undefined,download_task:{task_id:record.items[0].source_id,status:'pending',observed_at_utc:'2026-11-05T01:02:04Z',monitor_observation:{run_id:'b'.repeat(32),path:'/queue/历史路径',observed_at_utc:'2026-11-05T00:00:00Z'}}};
 const {dom}=await render(undefined,async()=>historyResponse([record]));try{const doc=dom.window.document;doc.querySelector('[data-testid=intake-record-details]').click();await tick();const dialog=doc.querySelector('[role=dialog]');assert.equal(new URL(dialog.querySelector('a[href^="/monitor-history"]').href).searchParams.get('run_id'),'b'.repeat(32));assert.match(dialog.textContent,/首次核对到：2026-11-05T00:00:00Z/);assert.match(dialog.textContent,/当时路径：\/queue\/历史路径/);assert.match(dialog.textContent,/不保证整轮扫描成功/);}finally{dom.window.close();}
 record.items[0].download_task.monitor_observation.run_id='bad';const bad=await render(undefined,async()=>historyResponse([record]));try{assert.match(bad.dom.window.document.querySelector('[data-testid=intake-history-error]').textContent,/监控观察响应无效/);}finally{bad.dom.window.close();}
});

test('intake pending task explains the current blocker and rejects unknown wait reasons',async()=>{
 const record=historyRecord();record.items[0]={...record.items[0],state:'moved',error:undefined,download_task:{task_id:record.items[0].source_id,status:'pending',observed_at_utc:'2026-11-05T01:02:04Z',wait_reason:'queue_paused'}};
 const {dom}=await render(undefined,async()=>historyResponse([record]));try{const doc=dom.window.document;doc.querySelector('[data-testid=intake-record-details]').click();await tick();assert.match(doc.querySelector('[role=dialog]').textContent,/下载队列已暂停；可到下载列表恢复/);}finally{dom.window.close();}
 record.items[0].download_task.wait_reason='invented';const bad=await render(undefined,async()=>historyResponse([record]));try{assert.match(bad.dom.window.document.querySelector('[data-testid=intake-history-error]').textContent,/下载等待原因响应无效/);}finally{bad.dom.window.close();}
});

test('intake file sizes use the shared readable IEC units while directories have no download size',async()=>{const entries=[{id:'1',path:'/small.bin',name:'small.bin',directory:false,size:1024,can_enqueue:true},{id:'2',path:'/large.bin',name:'large.bin',directory:false,size:2405652891274,can_enqueue:true},{id:'3',path:'/folder',name:'folder',directory:true,size:0,can_enqueue:true}];const {dom,requests}=await render(async()=>({ok:true,json:async()=>listing('/',entries)}));try{assert.deepEqual([...dom.window.document.querySelectorAll('[data-testid=intake-size]')].map(n=>n.textContent),['1 KiB','2.19 TiB','—']);assert.ok(requests.every(r=>r.method==='GET'));}finally{dom.window.close();}});


test('intake history explains its read-only role and does not link back to itself',async()=>{
 for(const path of ['/intake-history','/intake']){
 const {dom}=await render(undefined,undefined,{url:'http://localhost'+path});try{const d=dom.window.document,links=[...d.querySelectorAll('main a[href="/intake-history"]')];if(path==='/intake-history'){assert.equal(links.length,0);assert.match(d.querySelector('.page-header').textContent,/只读查看/);}else{assert.equal(links.length,1);assert.equal(links[0].textContent,'查看入队记录');}}finally{dom.window.close();}
 }
});
