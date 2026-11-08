import {mountBuiltWorkspace} from './built-workspace.mjs';
import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {JSDOM,VirtualConsole} from 'jsdom';
import {displaySizeListing,sizeSnapshot} from './cloud-display-size-fixture.mjs';
const html=await readFile(new URL('../../internal/webui/app/index.html',import.meta.url),'utf8');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
const row=i=>({id:String(i+1),fs_id:String(i+1),path:'/f'+i,name:'f'+i,directory:false,size:'10',provider_size:'10',server_mtime:'1'});
function listing(params,total=101,token=sizeSnapshot){const offset=Number(params.get('offset')||0);const value=displaySizeListing({directory:'/',queue_dir:'/queue',entries:Array.from({length:Math.min(100,Math.max(0,total-offset))},(_,i)=>row(offset+i)),total,offset,next_offset:offset+100<total?offset+100:null,...(offset+100<total?{next_boundary:{fingerprint:'b'.repeat(64)}}:{}),observed_at_utc:'2026-11-03T01:00:00Z'},params.get('desc')==='1',token);value.order.field=params.get('order')||'size';value.display_size.mixed=params.get('mixed')!=='0';return value}
async function render(respond,search='?order=size',prepare=()=>{}){
 const dom=new JSDOM(html,{url:'http://localhost/cloud/files'+search,runScripts:'outside-only',pretendToBeVisual:true,virtualConsole:new VirtualConsole()}),requests=[];
 prepare(dom);
 dom.window.fetch=async(raw,init={})=>{const url=new URL(String(raw),'http://localhost');requests.push({url,init});return respond(url,init)};
 await mountBuiltWorkspace(dom);await tick();await tick();return {dom,requests};
}

test('display size uses complete-directory endpoint and carries immutable snapshot across pages, but refresh creates anew',async()=>{
 const {dom,requests}=await render(async url=>({ok:true,json:async()=>listing(url.searchParams)}));
 try{const d=dom.window.document;assert.equal(requests[0].url.pathname,'/api/cloud-display-size');assert.equal(requests[0].url.searchParams.get('snapshot'),null);assert.equal(new URL(dom.window.location.href).searchParams.get('size_snapshot'),sizeSnapshot);assert.match(d.querySelector('[data-testid=cloud-display-size-basis]').textContent,/代次 9007199254740993/);d.querySelector('[data-testid=cloud-files-next]').click();await tick();await tick();assert.equal(requests.at(-1).url.searchParams.get('snapshot'),sizeSnapshot);assert.equal(requests.at(-1).url.searchParams.get('offset'),'100');assert.equal(d.querySelectorAll('tbody tr').length,1);d.querySelector('[data-testid=cloud-files-refresh]').click();await tick();await tick();assert.equal(requests.at(-1).url.searchParams.get('offset'),'0');assert.equal(requests.at(-1).url.searchParams.get('snapshot'),null);assert.ok(requests.every(r=>!r.init.method||r.init.method==='GET'));}finally{dom.window.close()}
});

test('restoring a size page reuses the URL snapshot and recovers once from expiry',async()=>{
 const {dom,requests}=await render(async url=>url.searchParams.has('snapshot')?{ok:false,status:409,json:async()=>({code:'display_size_snapshot_changed'})}:{ok:true,json:async()=>listing(url.searchParams,1,'c'.repeat(32))},'?order=size&offset=100&boundary='+'b'.repeat(64)+'&size_snapshot='+sizeSnapshot);
 try{const d=dom.window.document;assert.equal(requests[0].url.searchParams.get('snapshot'),sizeSnapshot);assert.equal(requests.length,2);assert.equal(requests[1].url.pathname,'/api/cloud-display-size');assert.equal(requests[1].url.searchParams.get('snapshot'),null);assert.equal(requests[1].url.searchParams.get('offset'),'0');assert.match(d.body.textContent,/大小排序快照已失效.*第一页/);assert.equal(new URL(dom.window.location.href).searchParams.get('size_snapshot'),'c'.repeat(32));assert.equal(dom.window.history.length,1);}finally{dom.window.close()}
});

test('a malformed size snapshot cannot replace the confirmed list or sort marker',async()=>{
 let bad=false;const {dom}=await render(async url=>({ok:true,json:async()=>{const value=listing(url.searchParams,1);if(bad)value.display_size.cache_generation=9007199254740992;return value}}));
 try{const d=dom.window.document;assert.equal(d.querySelectorAll('tbody tr').length,1);bad=true;d.querySelector('[data-testid=cloud-files-refresh]').click();await tick();await tick();assert.match(d.querySelector('[role=alert]').textContent,/大小排序.*无效/);assert.equal(d.querySelectorAll('tbody tr').length,1);assert.equal(d.querySelector('[data-testid=cloud-select-page]').disabled,true);}finally{dom.window.close()}
});

test('size snapshot expiry never loops and a failed rebuild preserves historical rows',async()=>{
 let failed=false;const {dom,requests}=await render(async url=>failed?{ok:false,status:409,json:async()=>({code:'display_size_snapshot_changed',message:'快照读取暂不可用'})}:{ok:true,json:async()=>listing(url.searchParams)});
 try{const d=dom.window.document;failed=true;d.querySelector('[data-testid=cloud-files-next]').click();await tick();await tick();await tick();assert.equal(requests.length,3);assert.equal(requests.at(-1).url.searchParams.get('snapshot'),null);assert.match(d.querySelector('[role=alert]').textContent,/快照读取暂不可用.*历史结果/);assert.doesNotMatch(d.querySelector('[data-testid=cloud-page-notice]')?.textContent||d.body.textContent,/已重新读取第一页/);assert.equal(new URL(dom.window.location.href).searchParams.get('offset'),null);assert.equal(d.querySelectorAll('tbody tr').length,100);assert.equal(d.querySelector('[data-testid=cloud-header-sort-size]').closest('th').getAttribute('aria-sort'),'ascending');}finally{dom.window.close()}
});

test('size page without a URL snapshot rebuilds page one with an explicit notice',async()=>{
 const {dom,requests}=await render(async url=>({ok:true,json:async()=>listing(url.searchParams,0)}),'?order=size&offset=100&boundary='+'b'.repeat(64));
 try{assert.equal(requests.length,1);assert.equal(requests[0].url.searchParams.get('offset'),'0');assert.equal(requests[0].url.searchParams.get('snapshot'),null);assert.match(dom.window.document.body.textContent,/依据缺失.*第一页/)}finally{dom.window.close()}
});

test('snapshot identity, UTC, totals and unknown counts are checked before publishing a size page',async()=>{
 const corruptions=[v=>v.display_size.snapshot_id='invalid',v=>v.display_size.remote_read_started_at_utc='2026-02-30T00:00:00Z',v=>v.display_size.expires_at_utc=v.display_size.created_at_utc,v=>v.display_size.unknown_directories=-1,v=>v.display_size.unknown_directories=2,v=>v.total=2,v=>v.next_offset=100,v=>v.observed_at_utc='2026-11-03T02:00:00Z'];
 for(const corrupt of corruptions){const {dom}=await render(async url=>({ok:true,json:async()=>{const value=listing(url.searchParams,1);corrupt(value);return value}}));try{assert.match(dom.window.document.querySelector('[role=alert]').textContent,/大小排序.*无效/);assert.equal(dom.window.document.querySelector('[data-testid=cloud-display-size-basis]'),null);assert.equal(dom.window.document.querySelector('[data-testid=cloud-header-sort-size]').closest('th').getAttribute('aria-sort'),null);assert.equal(new URL(dom.window.location.href).searchParams.get('size_snapshot'),null)}finally{dom.window.close()}}
});

test('a response cannot swap the requested snapshot while restoring a history page',async()=>{
 const {dom}=await render(async url=>({ok:true,json:async()=>listing(url.searchParams,1,'c'.repeat(32))}),'?order=size&size_snapshot='+sizeSnapshot);
 try{assert.match(dom.window.document.querySelector('[role=alert]').textContent,/大小排序.*无效/);assert.equal(dom.window.document.querySelector('[data-testid=cloud-display-size-basis]'),null);assert.equal(new URL(dom.window.location.href).searchParams.get('size_snapshot'),sizeSnapshot)}finally{dom.window.close()}
});

test('cancelled size creation ignores its late snapshot and authentication response',async()=>{
 for(const lateStatus of [200,401,409]){let finish;const {dom,requests}=await render(async()=>new Promise(resolve=>finish=resolve));try{const d=dom.window.document;assert.match(d.body.textContent,/当前目录完整清单.*取消/);d.querySelector('[data-testid=cloud-files-cancel-read]').click();await tick();assert.equal(requests[0].init.signal.aborted,true);finish({ok:lateStatus===200,status:lateStatus,json:async()=>lateStatus===200?listing(new URLSearchParams(),1):{code:'display_size_snapshot_changed'}});await tick();await tick();assert.equal(requests.length,1);assert.equal(d.querySelector('[data-testid=cloud-display-size-basis]'),null);assert.match(d.querySelector('[role=alert]').textContent,/已取消/);assert.equal(d.querySelector('[data-testid=cloud-files-refresh]').disabled,false);assert.equal(new URL(dom.window.location.href).searchParams.get('size_snapshot'),null)}finally{finish?.({ok:false,status:503});dom.window.close()}}
});

test('known directory size is displayed but remote rename still receives raw provider size',async()=>{
 let preview;const {dom}=await render(async(url,init)=>{if(init.method==='POST'){preview=JSON.parse(init.body);return {ok:true,json:async()=>({operation_id:'d'.repeat(32),status:'confirmed',object_id:'1',source_path:'/folder',destination_path:'/renamed'})}}const value=listing(url.searchParams,3);value.entries=[{...row(0),path:'/folder',name:'folder',directory:true,size:'5000',provider_size:'0',size_complete:true},{...row(1),path:'/zero',name:'zero',directory:true,size:'0',provider_size:'0',size_complete:true},{...row(2),path:'/unknown',name:'unknown',directory:true,size:'0',provider_size:'0',size_complete:false}];value.display_size.unknown_directories=1;return {ok:true,json:async()=>value}});
 try{const d=dom.window.document;assert.match(d.querySelector('[data-testid=cloud-display-size-basis]').textContent,/未知大小置后（1 个目录）/);const rows=d.querySelectorAll('tbody tr');assert.match(rows[0].querySelector('[data-testid=cloud-file-size]').textContent,/4.88 KiB/);assert.match(rows[1].querySelector('[data-testid=cloud-file-size]').textContent,/0 B/);assert.equal(rows[2].querySelector('[data-testid=cloud-file-size]').textContent.trim(),'—');rows[0].querySelector('[data-testid=cloud-file-info]').click();await tick();assert.match(d.querySelector('[data-testid=cloud-file-info-panel]').textContent,/排序快照.*9007199254740993/);d.querySelector('[data-testid=cloud-file-info-close]').click();await tick();rows[0].querySelector('[data-testid=cloud-file-rename]').click();await tick();const input=d.querySelector('[data-testid=cloud-action-name]');input.value='renamed';input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));await tick();d.querySelector('[data-testid=cloud-action-ack]').click();await tick();d.querySelector('[data-testid=cloud-action-submit]').click();await tick();await tick();assert.equal(preview.expected.size,'0');assert.equal(preview.expected.fs_id,'1');}finally{dom.window.close()}
});

test('returning to an idle size view rebuilds its remote snapshot',async()=>{
 const {dom,requests}=await render(async url=>({ok:true,json:async()=>listing(url.searchParams,1)}));
 try{dom.window.dispatchEvent(new dom.window.Event('blur'));dom.window.dispatchEvent(new dom.window.Event('focus'));await tick();await tick();assert.equal(requests.length,2);assert.equal(requests.at(-1).url.searchParams.get('snapshot'),null);}finally{dom.window.close()}
});

test('changing order or directory discards the previous snapshot and all orders retain snapshot evidence',async()=>{
 const {dom,requests}=await render(async url=>{const value=listing(url.searchParams,0);value.directory=url.searchParams.get('dir')||'/';if(url.searchParams.get('order')!=='size'){value.order={field:url.searchParams.get('order')||'name',descending:url.searchParams.get('desc')==='1'}}return {ok:true,json:async()=>value}});
 try{const d=dom.window.document;d.querySelector('[data-testid=cloud-header-sort-size]').click();await tick();await tick();assert.equal(requests.at(-1).url.searchParams.get('desc'),'1');assert.equal(requests.at(-1).url.searchParams.get('snapshot'),null);const input=d.querySelector('[data-testid=cloud-files-path]');input.value='/next';input.dispatchEvent(new dom.window.Event('input',{bubbles:true}));await tick();d.querySelector('[data-testid=cloud-files-open]').click();await tick();await tick();assert.equal(requests.at(-1).url.searchParams.get('dir'),'/next');assert.equal(requests.at(-1).url.searchParams.get('snapshot'),null);d.querySelector('[data-testid=cloud-header-sort-name]').click();await tick();await tick();assert.equal(requests.at(-1).url.pathname,'/api/cloud-display-size');assert.equal(requests.at(-1).url.searchParams.get('snapshot'),null);assert.equal(new URL(dom.window.location.href).searchParams.get('size_snapshot'),sizeSnapshot);assert.ok(d.querySelector('[data-testid=cloud-display-size-basis]'));}finally{dom.window.close()}
});

test('history restores a later size page without creating another snapshot',async()=>{
 const {dom,requests}=await render(async url=>({ok:true,json:async()=>listing(url.searchParams)}));
 try{dom.window.history.replaceState(dom.window.history.state,'','/cloud/files?order=size&offset=100&boundary='+'b'.repeat(64)+'&size_snapshot='+sizeSnapshot);dom.window.dispatchEvent(new dom.window.PopStateEvent('popstate'));await tick();await tick();assert.equal(requests.at(-1).url.searchParams.get('snapshot'),sizeSnapshot);assert.equal(requests.at(-1).url.searchParams.get('offset'),'100');assert.equal(dom.window.document.querySelectorAll('tbody tr').length,1);}finally{dom.window.close()}
});

test('size recovery retains search location and clears the recovery notice on a fresh read',async()=>{
 const saved='/cloud/files?source=cache&q=f',location=new URLSearchParams({order:'size',size_snapshot:sizeSnapshot,focus_id:'1',focus_path:'/f0',focus_directory:'0',return_search:saved});
 const {dom}=await render(async url=>url.searchParams.has('snapshot')?{ok:false,status:409,json:async()=>({code:'display_size_snapshot_changed'})}:{ok:true,json:async()=>listing(url.searchParams,1)},'?'+location);
 try{const d=dom.window.document;assert.match(d.body.textContent,/大小排序快照已失效/);assert.ok(d.querySelector('.cloud-row-located'));assert.equal(d.querySelector('[data-testid=cloud-search-return]').getAttribute('href'),saved);assert.equal(new URL(dom.window.location.href).searchParams.get('return_search'),saved);d.querySelector('[data-testid=cloud-files-refresh]').click();await tick();await tick();assert.doesNotMatch(d.body.textContent,/大小排序快照已失效/);assert.ok(d.querySelector('.cloud-row-located'));}finally{dom.window.close()}
});

test('missing size page proof cannot announce successful recovery when the first read fails',async()=>{
 const {dom}=await render(async()=>({ok:false,status:503,text:async()=> '暂不可读'}),'?order=size&offset=100');
 try{assert.match(dom.window.document.querySelector('[role=alert]').textContent,/暂不可读/);assert.doesNotMatch(dom.window.document.body.textContent,/已重新读取第一页/);}finally{dom.window.close()}
});

test('size pagination identifies a snapshot read rather than claiming another remote full read',async()=>{
 let finish;const {dom}=await render(async url=>url.searchParams.has('snapshot')?new Promise(resolve=>finish=resolve):{ok:true,json:async()=>listing(url.searchParams)});
 try{const d=dom.window.document;d.querySelector('[data-testid=cloud-files-next]').click();await tick();assert.match(d.body.textContent,/正在读取大小排序快照分页/);assert.doesNotMatch(d.body.textContent,/正在读取当前目录完整清单/);}finally{finish?.({ok:false,status:503,text:async()=> 'closed'});dom.window.close()}
});

test('mixed ordering defaults on and toggling any order rebuilds the first page with persisted preference',async()=>{
 const {dom,requests}=await render(async url=>{const value=listing(url.searchParams,0);value.order.field=url.searchParams.get('order')||'name';value.display_size.mixed=url.searchParams.get('mixed')!=='0';return {ok:true,json:async()=>value}});
 try{const d=dom.window.document,control=d.querySelector('[data-testid=cloud-files-mixed]');assert.ok(control);assert.equal(control.checked,true);control.click();await tick();await tick();assert.equal(requests.at(-1).url.searchParams.get('mixed'),'0');assert.equal(requests.at(-1).url.searchParams.get('snapshot'),null);assert.equal(requests.at(-1).url.searchParams.get('offset'),'0');assert.equal(new URL(dom.window.location.href).searchParams.get('mixed'),'0');assert.equal(dom.window.localStorage.getItem('nas-downloader.cloud-mixed-order.v1'),'0');d.querySelector('[data-testid=cloud-header-sort-name]').click();await tick();await tick();assert.equal(requests.at(-1).url.searchParams.get('order'),'name');assert.equal(requests.at(-1).url.searchParams.get('mixed'),'0');d.querySelector('[data-testid=cloud-header-sort-time]').click();await tick();await tick();assert.equal(requests.at(-1).url.searchParams.get('order'),'time');assert.equal(requests.at(-1).url.searchParams.get('mixed'),'0');assert.match(d.querySelector('[data-testid=cloud-display-size-basis]').textContent,/文件夹在前.*文件在后/);}finally{dom.window.close()}
});


test('mixed preference restores locally while explicit history mode takes precedence',async()=>{
 for(const [search,want] of [['?order=name',false],['?order=time&mixed=1',true]]){
  const {dom,requests}=await render(async url=>({ok:true,json:async()=>listing(url.searchParams,0)}),search,dom=>dom.window.localStorage.setItem('nas-downloader.cloud-mixed-order.v1','0'));
  try{assert.equal(dom.window.document.querySelector('[data-testid=cloud-files-mixed]').checked,want);assert.equal(requests[0].url.searchParams.get('mixed'),want?'1':'0');assert.equal(new URL(dom.window.location.href).searchParams.get('mixed'),want?'1':'0')}finally{dom.window.close()}
 }
});

test('blocked preference storage keeps default mixed sorting and permits a page-only change',async()=>{
 const {dom,requests}=await render(async url=>({ok:true,json:async()=>listing(url.searchParams,0)}),'?order=name',dom=>{dom.window.Storage.prototype.getItem=()=>{throw Error('blocked')};dom.window.Storage.prototype.setItem=()=>{throw Error('blocked')}});
 try{const d=dom.window.document,control=d.querySelector('[data-testid=cloud-files-mixed]');assert.equal(control.checked,true);control.click();await tick();await tick();assert.equal(requests.at(-1).url.searchParams.get('mixed'),'0');assert.match(d.body.textContent,/浏览器未保存偏好/);assert.equal(d.querySelector('[role=alert]'),null);assert.match(d.querySelector('[data-testid=cloud-display-size-basis]').textContent,/文件夹在前/)}finally{dom.window.close()}
});

test('mismatched grouping response leaves old ordering evidence and allows a retry',async()=>{
 let fail=true;const {dom}=await render(async url=>{const value=listing(url.searchParams,0);if(fail)value.display_size.mixed=true;return {ok:true,json:async()=>value}});
 try{const d=dom.window.document;d.querySelector('[data-testid=cloud-files-mixed]').click();await tick();await tick();assert.match(d.querySelector('[role=alert]').textContent,/快照.*无效/);assert.match(d.querySelector('[data-testid=cloud-display-size-basis]').textContent,/文件夹与文件混合/);assert.ok(d.querySelector('[data-testid=cloud-sort-pending]'));fail=false;d.querySelector('[data-testid=cloud-files-refresh]').click();await tick();await tick();assert.equal(d.querySelector('[role=alert]'),null);assert.match(d.querySelector('[data-testid=cloud-display-size-basis]').textContent,/文件夹在前/);assert.equal(d.querySelector('[data-testid=cloud-sort-pending]'),null)}finally{dom.window.close()}
});
