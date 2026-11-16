import {test} from 'node:test';
import assert from 'node:assert/strict';
import {JSDOM,VirtualConsole} from 'jsdom';
import {mountBuiltWorkspace} from './built-workspace.mjs';

const config={root:'D:/Shared',revision:'a'.repeat(64),saved_at_utc:'2026-11-17T01:00:00Z',root_identity:'root-id'};
const entry=(name,directory=false)=>({name,path:name,id:name,directory,size:directory?'0':'1024',modified_at_utc:'2026-11-17T01:00:00Z',revision:'b'.repeat(64)});
const listing=(entries=[],extra={})=>({path:'',entries,total:entries.length,offset:0,limit:50,excluded:0,observed_at_utc:'2026-11-17T01:00:00Z',stale:false,root_revision:config.revision,...extra});
const response=(body,status=200)=>({ok:status<400,status,json:async()=>body,text:async()=>typeof body==='string'?body:JSON.stringify(body)});
async function until(fn){for(let i=0;i<100;i++){if(fn())return;await new Promise(r=>setTimeout(r,5));}assert.ok(fn(),'UI did not reach expected state');}
function browser(handle){const d=new JSDOM('<div id="app"></div>',{url:'http://localhost/library',runScripts:'outside-only',pretendToBeVisual:true,virtualConsole:new VirtualConsole()});d.window.fetch=async(u,o={})=>{const p=new URL(u,d.window.location.href);if(p.pathname.startsWith('/api/library/'))return handle(p,o);return response({items:[],tasks:[],runs:[],total:0,active:0});};return d;}

test('local library starts unconfigured and never automatically publishes a directory',async()=>{
 const calls=[];const dom=browser(async(p,o)=>{calls.push([p.pathname,o.method||'GET']);return response({root:'',revision:'',root_identity:'',saved_at_utc:''});});
 try{await mountBuiltWorkspace(dom);await until(()=>dom.window.document.querySelector('[data-testid=library-root-input]'));const doc=dom.window.document;assert.match(doc.querySelector('h1').textContent,/本站文件/);assert.match(doc.body.textContent,/只读/);assert.ok(doc.querySelector('a[href="/library"]'));assert.deepEqual(calls,[['/api/library/config','GET']]);}finally{dom.window.close();}
});
test('local library renders compact rows, readable size and safe observed-version downloads',async()=>{
 const dom=browser(async(p)=>p.pathname.endsWith('/config')?response(config):response(listing([entry('资料',true),entry('中文.txt')])));
 try{await mountBuiltWorkspace(dom);await until(()=>dom.window.document.querySelector('[data-testid=library-table]'));const doc=dom.window.document;assert.equal(doc.querySelectorAll('[data-testid=library-table] th').length,4);const a=doc.querySelector('[data-testid=library-download]');assert.ok(a);const u=new URL(a.href);assert.equal(u.pathname,'/api/library/content');assert.equal(u.searchParams.get('path'),'中文.txt');assert.equal(u.searchParams.get('revision'),'b'.repeat(64));assert.match(doc.body.textContent,/KiB|KB/);assert.equal(doc.querySelector('[data-testid=library-up]').disabled,true);}finally{dom.window.close();}
});
test('failed refresh displays the historical observation but disables downloads',async()=>{
 let fail=false;const dom=browser(async(p)=>p.pathname.endsWith('/config')?response(config):response(listing([entry('保留.txt')],fail?{stale:true,error:'目录离线',error_at_utc:'2026-11-17T02:00:00Z'}:{}),fail?503:200));
 try{await mountBuiltWorkspace(dom);await until(()=>dom.window.document.querySelector('[data-testid=library-download]'));fail=true;dom.window.document.querySelector('[data-testid=library-refresh]').click();await until(()=>dom.window.document.body.textContent.includes('历史观察'));assert.match(dom.window.document.body.textContent,/保留.txt/);assert.equal(dom.window.document.querySelector('a[data-testid=library-download]'),null);assert.equal(dom.window.document.querySelector('button[data-testid=library-download]').disabled,true);}finally{dom.window.close();}
});
test('late directory response cannot replace a newer navigation result',async()=>{
 let release;const dom=browser(async(p)=>{if(p.pathname.endsWith('/config'))return response(config);if(p.searchParams.get('path')==='child')return response(listing([entry('new.txt')],{path:'child'}));return new Promise(r=>release=()=>r(response(listing([entry('old.txt')]))));});
 try{await mountBuiltWorkspace(dom);await until(()=>release);dom.window.history.pushState(null,'','/library?path=child');dom.window.dispatchEvent(new dom.window.PopStateEvent('popstate'));await until(()=>dom.window.document.body.textContent.includes('new.txt'));release();await new Promise(r=>setTimeout(r,20));assert.ok(!dom.window.document.body.textContent.includes('old.txt'));assert.equal(dom.window.document.querySelector('[data-testid=library-up]').disabled,false);}finally{release?.();dom.window.close();}
});

test('library settings Escape closes the disclosure and returns focus',async()=>{
 const dom=browser(async(p)=>p.pathname.endsWith('/config')?response(config):response(listing([])));
 try{await mountBuiltWorkspace(dom);await until(()=>dom.window.document.querySelector('.library-root-settings'));const doc=dom.window.document,details=doc.querySelector('.library-root-settings'),summary=details.querySelector('summary');summary.click();assert.equal(details.open,true);const input=doc.querySelector('#library-root-change');input.focus();input.dispatchEvent(new dom.window.KeyboardEvent('keydown',{key:'Escape',bubbles:true}));assert.equal(details.open,false);assert.equal(doc.activeElement,summary);}finally{dom.window.close();}
});
