import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {JSDOM,VirtualConsole} from 'jsdom';
const html=await readFile(new URL('../../internal/webui/app/index.html',import.meta.url),'utf8'),asset=html.match(/src="\/app\/(assets\/[^"]+\.js)"/)[1],bundle=await readFile(new URL('../../internal/webui/app/'+asset,import.meta.url),'utf8');
const tick=()=>new Promise(resolve=>setImmediate(resolve));
async function render(path){const dom=new JSDOM(html,{url:'http://localhost'+path,runScripts:'outside-only',virtualConsole:new VirtualConsole()});dom.window.fetch=async()=>({ok:true,json:async()=>({directory:'/',entries:[],offset:0,total:null,observed_at_utc:'2026-11-04T00:00:00Z',batches:[],records:[],items:[],counts:{},root_count:0,root:null,total:0,authentication:{ready:true},queue_pause:{paused:false},queue_healthy:true})});dom.window.eval(bundle);await tick();await tick();return dom;}
test('workspaces share one permanent header with module navigation and contextual left menus',async()=>{
 for(const [path,module,entry] of [['/cloud/files','cloud','/cloud/cache'],['/uploads','transfers','/'],['/intake-history','history','/operations'],['/settings/paths','settings','/oauth'],['/','transfers','/uploads']]){
 const dom=await render(path);try{const d=dom.window.document;assert.equal(d.querySelectorAll('.app-header').length,1,path);assert.equal(d.querySelector('.app-header [aria-current=page]').dataset.module,module);assert.ok(d.querySelector('.sidebar a[href="'+entry+'"]'));assert.equal(d.querySelectorAll('.sidebar .brand').length,0);assert.ok(d.querySelector('.app-header [aria-label="退出登录"]'));}finally{dom.window.close()}}
});
test('cloud management shortcuts are explicitly bordered icon navigation with current state',async()=>{const dom=await render('/cloud/files');try{const d=dom.window.document,nav=d.querySelector('[aria-label="网盘管理快捷导航"]');assert.ok(nav);assert.equal(nav.querySelectorAll('a').length,5);for(const a of nav.querySelectorAll('a')){assert.ok(a.classList.contains('button'));assert.ok(a.classList.contains('secondary'));assert.ok(a.querySelector('svg'))};assert.equal(nav.querySelector('[aria-current=page]').getAttribute('href'),'/cloud/files')}finally{dom.window.close()}});

test('failed logout stays in the workspace and reports the failure',async()=>{const dom=await render('/cloud/files');try{dom.window.fetch=async()=>({ok:false,status:503});dom.window.document.querySelector('.app-header [aria-label="退出登录"]').click();await tick();await tick();assert.equal(dom.window.location.pathname,'/cloud/files');assert.match(dom.window.document.querySelector('[role=alert]').textContent,/退出登录失败/)}finally{dom.window.close()}});
