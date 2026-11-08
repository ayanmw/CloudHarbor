import {readFile} from 'node:fs/promises';
import {fileURLToPath} from 'node:url';
import {resolve,relative,isAbsolute} from 'node:path';
import {SourceTextModule} from 'node:vm';
const root=fileURLToPath(new URL('../../internal/webui/app/',import.meta.url));
// Every test executes the shipped module graph in its own browser realm.
export async function mountBuiltWorkspace(dom,{rejectModule,beforeModule}={}){
 const html=await readFile(resolve(root,'index.html'),'utf8');
 const entry=html.match(/src="\/app\/([^"]+\.js)"/)[1];
 const modules=new Map(),loaded=[];
 async function moduleFor(path){
  const absolute=resolve(root,path),within=relative(root,absolute);
  if(within.startsWith('..')||isAbsolute(within))throw Error('Module outside built app');
  if(modules.has(absolute))return modules.get(absolute);
  if(rejectModule?.(within))throw Error('Simulated module load failure');
  const promise=(async()=>{
   await beforeModule?.(within);
   loaded.push(within.replaceAll('\\','/'));
   const code=await readFile(absolute,'utf8');
   return new SourceTextModule(code,{context:dom.getInternalVMContext(),identifier:absolute,
    initializeImportMeta(meta){meta.url=new URL('/app/'+within.replaceAll('\\','/'),dom.window.location.origin).href;},
    importModuleDynamically:async(specifier,parent)=>{const child=await moduleFor(resolve(parent.identifier,'..',specifier));if(child.status==='unlinked')await child.link(linker);if(child.status==='linked')await child.evaluate();return child;}
   });
  })();
  modules.set(absolute,promise);
  // Failed browser fetches can be retried by a later explicit import.
  promise.catch(()=>modules.delete(absolute));
  return promise;
 }
 async function linker(specifier,parent){return moduleFor(resolve(parent.identifier,'..',specifier));}
 const main=await moduleFor(entry);await main.link(linker);await main.evaluate();
 return {loaded};
}
