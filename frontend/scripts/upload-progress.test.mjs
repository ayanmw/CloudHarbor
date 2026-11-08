import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
const source=await readFile(new URL('../src/upload-progress.ts',import.meta.url),'utf8');
const js=ts.transpile(source,{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ES2022});
const {uploadSummary,uploadItemProgress,validUploadItem}=await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
const item=(state,size='100',sent='0')=>({id:'a'.repeat(32),path:'folder/file',size,sent,state,updated_at_utc:'2026-11-05T00:00:00Z',error:'',actual_path:''});
test('upload summary preserves exact capacity and excludes unknown from completed',()=>{
 const summary=uploadSummary([item('confirmed','9007199254740993','9007199254740993'),item('unknown','2','2'),item('uploading','10','3'),item('queued'),item('receiving'),item('failed'),item('cancelled')]);
 assert.equal(summary.totalBytes,'9007199254741405');assert.equal(summary.confirmedBytes,'9007199254740993');assert.equal(summary.confirmed,1);assert.equal(summary.active,1);assert.equal(summary.waiting,2);assert.equal(summary.failed,1);assert.equal(summary.unknown,1);assert.equal(summary.cancelled,1);
});
test('stage percentages use exact integers and never mark finalization or zero-byte unknown complete',()=>{
 assert.equal(uploadItemProgress(item('uploading','9007199254740993','4503599627370496')).percent,'49.9');
 assert.equal(uploadItemProgress(item('uploading','100','10')).percent,'10');assert.equal(uploadItemProgress(item('confirmed','0')).percent,'100');
 assert.equal(uploadItemProgress(item('unknown','0')).percent,null);assert.equal(uploadItemProgress(item('finalizing','10','10')).percent,null);
});
test('upload evidence rejects malformed bytes and oversize acknowledgements',()=>{
 for(const invalid of [item('uploading','10','11'),item('uploading','10','oops'),item('uploading','9223372036854775808'),item('invented')])assert.equal(validUploadItem(invalid),false);
 assert.equal(validUploadItem(item('uploading','9223372036854775807','9007199254740993')),true);
});

test('paused uploads have their own count and cannot carry a pending active-stage pause flag',()=>{
 const paused=item('paused','10','2');const summary=uploadSummary([paused,item('queued')]);assert.equal(summary.paused,1);assert.equal(summary.waiting,1);assert.equal(uploadItemProgress(paused).percent,null);assert.match(uploadItemProgress(paused).description,/继续前核对会话和文件，未知请求保持隔离/);assert.equal(validUploadItem({...paused,pause_requested:true}),false);assert.equal(validUploadItem({...item('uploading'),pause_requested:true}),true);
});
