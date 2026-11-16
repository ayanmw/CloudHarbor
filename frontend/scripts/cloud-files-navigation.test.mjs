import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
const source=await readFile(new URL('../src/cloud-files-route.ts',import.meta.url),'utf8');
const js=ts.transpile(source,{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ES2022});
const {readCloudFilesRoute,cloudFilesRouteURL,cloudSearchUTCSeconds,cloudSearchUTCInput,validCloudSearchExtra,validCloudWorkspaceReturn}=await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
const origin='/cloud/files?dir=%2Fdocs&source=cache&q=lesson&scope=%2Fdocs&kind=file&offset=50&generation=7';
const locationQuery=(extra={})=>'?'+new URLSearchParams({dir:'/docs',focus_id:'9007199254740993',focus_path:'/docs/lesson',focus_directory:'0',return_search:origin,...extra});
test('live location roundtrips only a validated local search return',()=>{const parsed=readCloudFilesRoute(locationQuery());assert.equal(parsed.error,'');assert.equal(parsed.route.location.id,'9007199254740993');assert.equal(parsed.route.location.returnSearch,origin);assert.equal(cloudFilesRouteURL(parsed.route),'/cloud/files'+locationQuery());});
test('malformed and nested location returns never become navigation targets',()=>{
 for(const extra of [{return_search:'https://evil.test/cloud/files?source=cache&q=x'},{return_search:'//evil.test/'},{return_search:'/cloud/files?source=live&q=x'},{return_search:origin+'&return_search='+encodeURIComponent(origin)},{focus_id:'0'},{focus_id:'9223372036854775808'},{focus_path:'/other/lesson'},{focus_directory:'true'},{source:'cache',q:'lesson'}])assert.ok(readCloudFilesRoute(locationQuery(extra)).error,JSON.stringify(extra));
 for(const suffix of ['&focus_id=1','&return_search='+encodeURIComponent(origin)])assert.ok(readCloudFilesRoute(locationQuery()+suffix).error);
 assert.ok(readCloudFilesRoute('?return_search='+encodeURIComponent(origin)).error);
});
test('extra search filters retain exact seconds in URLs and nested location returns',()=>{
 const url=origin.replace('&offset','&extensions=pdf%2Cmp4&modified_from=100&modified_before=200&offset'),parsed=readCloudFilesRoute(url.slice('/cloud/files'.length));assert.equal(parsed.error,'');assert.deepEqual(parsed.route.extra,{extensions:'pdf,mp4',modifiedFrom:'100',modifiedBefore:'200'});assert.equal(cloudFilesRouteURL(parsed.route),url);const located=readCloudFilesRoute(locationQuery({return_search:url}));assert.equal(located.error,'');assert.equal(located.route.location.returnSearch,url);
 for(const suffix of ['&extensions=','&extensions=PDF','&extensions=pdf,pdf','&modified_from=0','&modified_from=01','&modified_before=200&modified_before=200','&modified_from=200&modified_before=100','&kind=directory&extensions=pdf'])assert.ok(readCloudFilesRoute('?source=cache&q=x'+suffix).error,suffix);
});
test('UTC controls roundtrip seconds without local timezone or invalid date normalization',()=>{
 assert.equal(cloudSearchUTCSeconds('1970-01-01T00:01:40'),'100');assert.equal(cloudSearchUTCInput('100'),'1970-01-01T00:01:40');assert.equal(cloudSearchUTCSeconds('2026-11-05T00:00'),String(Date.UTC(2026,10,5)/1000));assert.equal(cloudSearchUTCSeconds('2026-02-30T00:00'),null);assert.equal(cloudSearchUTCSeconds('2026-11-05T24:00'),null);assert.equal(cloudSearchUTCSeconds(''), '');assert.equal(cloudSearchUTCSeconds(cloudSearchUTCInput('253402300800')),'253402300800');assert.equal(validCloudSearchExtra({extensions:'pdf',modifiedFrom:'200',modifiedBefore:'100'},'all'),false);
});

test('display size snapshot survives first-page and later-page routes only for live size ordering',()=>{
 const token='a'.repeat(32),boundary='b'.repeat(64);
 for(const offset of [0,100]){
  const parsed=readCloudFilesRoute('?order=size&size_snapshot='+token+(offset?'&offset=100&boundary='+boundary:''));
  assert.equal(parsed.error,'');assert.equal(parsed.route.sizeSnapshot,token);
  assert.equal(readCloudFilesRoute(new URL(cloudFilesRouteURL(parsed.route),'http://localhost').search).route.sizeSnapshot,token);
 }
 for(const suffix of ['?order=type&size_snapshot='+token,'?source=cache&order=size&size_snapshot='+token,'?order=size&size_snapshot=','?order=size&size_snapshot='+token.toUpperCase(),'?order=size&size_snapshot='+token+'&size_snapshot='+token])assert.ok(readCloudFilesRoute(suffix).error,suffix);
});

test('mixed mode and snapshot roundtrip for every live order, never cache search',()=>{
 for(const order of ['name','size','time'])for(const mixed of ['0','1']){const parsed=readCloudFilesRoute('?order='+order+'&mixed='+mixed+'&size_snapshot='+'a'.repeat(32));assert.equal(parsed.error,'');assert.equal(parsed.route.mixed,mixed==='1');assert.equal(readCloudFilesRoute(new URL(cloudFilesRouteURL(parsed.route),'http://localhost').search).route.mixed,mixed==='1')}
 for(const query of ['?mixed=2','?mixed=','?mixed=0&mixed=1','?source=cache&q=x&mixed=1'])assert.ok(readCloudFilesRoute(query).error,query);
});

test('workspace return accepts canonical internal browsing only',()=>{
 for(const value of ['/cloud/files','/cloud/files?dir=%2Fdocs&order=size&mixed=0','/cloud/files?source=cache&q=lesson&scope=%2Fdocs&kind=file&offset=50&generation=7'])assert.equal(validCloudWorkspaceReturn(value),cloudFilesRouteURL(readCloudFilesRoute(value.slice('/cloud/files'.length)).route));
 for(const value of ['https://evil.test/cloud/files','//evil.test/cloud/files','/cloud/files/other','/cloud/files#x','/cloud/files?return_files=x','/cloud/files?dir=%2Fa&dir=%2Fb','/cloud/files?dir=%2F..','/cloud/files?'+ 'x'.repeat(32768)])assert.equal(validCloudWorkspaceReturn(value),'',value.slice(0,90));
 assert.equal(validCloudWorkspaceReturn('/cloud/files?delete=1'),'/cloud/files');
});
