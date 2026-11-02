import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { JSDOM } from 'jsdom';
const root = new URL('../../internal/webui/', import.meta.url);

test('scan controls default to completion and only send a finite cap after explicit selection', async () => {
 const html=await readFile(new URL('index_cloud.html',root),'utf8');
 const code=await readFile(new URL('res/cloud_scan_limits.js',root),'utf8');
 const dom=new JSDOM(html,{url:'http://localhost/cloud',runScripts:'outside-only'});
 try{
  dom.window.eval(code);const api=dom.window.CloudScanLimits,doc=dom.window.document;
  assert.deepEqual(JSON.parse(JSON.stringify(api.request('scan'))),{request_limit_mode:'unlimited',request_budget:0});
  assert.equal(doc.querySelector('#scan-limit-fields').hidden,true);
  const mode=doc.querySelector('#scan-limit-mode');mode.value='limited';mode.dispatchEvent(new dom.window.Event('change'));
  doc.querySelector('#scan-budget').value='0';assert.throws(()=>api.request('scan'));
  doc.querySelector('#scan-budget').value='12';assert.equal(api.request('scan').request_budget,12);
  mode.value='unlimited';mode.dispatchEvent(new dom.window.Event('change'));
  assert.equal(api.request('scan').request_budget,0);
 }finally{dom.window.close()}
});

test('a finite paused checkpoint is labelled and does not overwrite the users later explicit unlimited choice',async()=>{
 const html=await readFile(new URL('index_cloud.html',root),'utf8');
 const code=await readFile(new URL('res/cloud_scan_limits.js',root),'utf8');
 const dom=new JSDOM(html,{url:'http://localhost/cloud',runScripts:'outside-only'});
 try{
  dom.window.eval(code);const api=dom.window.CloudScanLimits,doc=dom.window.document;
  api.observe('scan',{run_id:'old',status:'paused',pending_count:1,request_budget:10,requests_used:10});
  assert.equal(doc.querySelector('#scan-limit-mode').value,'limited');
  assert.match(doc.querySelector('#scan-limit-hint').textContent,/追加/);
  const mode=doc.querySelector('#scan-limit-mode');mode.value='unlimited';mode.dispatchEvent(new dom.window.Event('change'));
  api.observe('scan',{run_id:'old',status:'paused',pending_count:1,request_budget:10,requests_used:10});
  assert.equal(api.request('scan').request_limit_mode,'unlimited');
 }finally{dom.window.close()}
});

test('new runs and externally changed finite caps cannot inherit an unchosen unlimited mode',async()=>{
 const html=await readFile(new URL('index_cloud.html',root),'utf8');
 const code=await readFile(new URL('res/cloud_scan_limits.js',root),'utf8');
 const dom=new JSDOM(html,{url:'http://localhost/cloud',runScripts:'outside-only'});
 try{
  dom.window.eval(code);const api=dom.window.CloudScanLimits,doc=dom.window.document;
  for(const [kind,prefix] of [['scan','scan'],['refresh','directory-refresh']]){
   api.observe(kind,{run_id:'complete-a',status:'complete',pending_count:0,request_budget:10});
   api.observe(kind,{run_id:'finite-b',status:'paused',pending_count:5,request_budget:10,requests_used:10});
   assert.equal(api.request(kind).request_limit_mode,'limited');
   const mode=doc.querySelector('#'+prefix+'-limit-mode');mode.value='unlimited';mode.dispatchEvent(new dom.window.Event('change'));
   api.observe(kind,{run_id:'finite-b',status:'paused',pending_count:5,request_budget:10});
   assert.equal(api.request(kind).request_limit_mode,'unlimited','same-run user intent must survive polling');
   api.observe(kind,{run_id:'unlimited-c',status:'paused',pending_count:5,request_budget:0});
   api.observe(kind,{run_id:'unlimited-c',status:'paused',pending_count:5,request_budget:20});
   assert.equal(api.request(kind).request_limit_mode,'limited','a cap added elsewhere to the same run must be observed');
  }
 }finally{dom.window.close()}
});
