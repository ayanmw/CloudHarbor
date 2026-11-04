import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import ts from 'typescript';
const source=await readFile(new URL('../src/format-bytes.ts',import.meta.url),'utf8');
const js=ts.transpile(source,{target:ts.ScriptTarget.ES2022,module:ts.ModuleKind.ES2022});
const {formatBytes}=await import('data:text/javascript;base64,'+Buffer.from(js).toString('base64'));
test('readable bytes use exact integer arithmetic with IEC rounding',()=>{
 for(const [input,expected] of [['0','0 B'],['1023','1023 B'],['1024','1 KiB'],['2405652891274','2.19 TiB'],['9223372036854775807','8 EiB'],['1048575','1 MiB'],['-1','—'],['invalid','—']])assert.equal(formatBytes(input),expected);
 assert.equal(formatBytes(1234.5),'1.21 KiB');
});
