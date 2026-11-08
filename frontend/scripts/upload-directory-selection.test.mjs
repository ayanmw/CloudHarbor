import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
const source=await readFile(new URL('../src/upload-directory-selection.ts',import.meta.url),'utf8');
const js=ts.transpile(source,{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ES2022});
const {enumerateUploadDirectory}=await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
const directory=(name,children=[])=>({kind:'directory',name,async *values(){yield* children}});
const file=(name,size=3)=>({kind:'file',name,async getFile(){return {name,size}}});

test('complete directory selection retains empty root and nested empty folders',async()=>{
 const result=await enumerateUploadDirectory(directory('课程',[file('a.txt'),directory('sub',[directory('empty')])]));
 assert.deepEqual(result.directories,['课程','课程/sub','课程/sub/empty']);assert.equal(result.files[0].path,'课程/a.txt');assert.equal(result.emptyDirectories,1);assert.equal(result.bytes,'3');
 const empty=await enumerateUploadDirectory(directory('空目录'));assert.equal(empty.files.length,0);assert.deepEqual(empty.directories,['空目录']);assert.equal(empty.emptyDirectories,1);
});
test('directory enumeration never returns a partial manifest on permission failure',async()=>{
 const broken={kind:'directory',name:'broken',async *values(){throw Error('permission denied')}};
 await assert.rejects(enumerateUploadDirectory(directory('root',[file('ok'),broken])),/permission denied/);
});
test('directory enumeration cancellation invalidates delayed file reads',async()=>{
 let release;const signal={aborted:false};const pending=enumerateUploadDirectory(directory('root',[{kind:'file',name:'late',getFile:()=>new Promise(r=>release=r)}]),signal);
 while(!release)await new Promise(r=>setImmediate(r));signal.aborted=true;release({name:'late',size:1});await assert.rejects(pending,/已取消/);
});
test('directory enumeration rejects oversized manifests and ambiguous paths',async()=>{
 await assert.rejects(enumerateUploadDirectory(directory('root',[file('a')]),undefined,()=>{},1),/10000/);
 await assert.rejects(enumerateUploadDirectory(directory('root',[file('same'),directory('same')])),/重复/);
 await assert.rejects(enumerateUploadDirectory(directory('root',[directory('..')])),/名称/);
 await assert.rejects(enumerateUploadDirectory(directory('root',[file('huge',Number.MAX_SAFE_INTEGER+1)])),/大小/);
});
