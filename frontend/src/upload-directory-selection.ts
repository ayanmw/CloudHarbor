export type UploadDirectoryHandle={kind:'directory';name:string;values:()=>AsyncIterable<UploadDirectoryHandle|UploadFileHandle>};
type UploadFileHandle={kind:'file';name:string;getFile:()=>Promise<File>};
export type DirectorySelection={files:{file:File;path:string}[];directories:string[];emptyDirectories:number;bytes:string};

export async function enumerateUploadDirectory(root:UploadDirectoryHandle,signal?:Pick<AbortSignal,'aborted'>,progress:(files:number,directories:number)=>void=()=>{},limit=10000):Promise<DirectorySelection>{
 const result:DirectorySelection={files:[],directories:[],emptyDirectories:0,bytes:'0'},seen=new Set<string>();let bytes=0n;
 const check=()=>{if(signal?.aborted)throw Error('目录选择已取消');};
 const add=(name:string,parent:string)=>{check();if(!name||name==='.'||name==='..'||/[\/\\\x00-\x1f\x7f]/.test(name))throw Error('目录或文件名称无效');const path=parent?parent+'/'+name:name;if(seen.has(path))throw Error('目录中存在重复路径');if(seen.size>=limit)throw Error('每批最多10000个文件和目录');seen.add(path);return path;};
 async function visit(handle:UploadDirectoryHandle,parent:string){
  const path=add(handle.name,parent);result.directories.push(path);progress(result.files.length,result.directories.length);let children=0;
  for await(const entry of handle.values()){
   check();children++;
   if(entry.kind==='directory')await visit(entry,path);
   else if(entry.kind==='file'){const relative=add(entry.name,path),file=await entry.getFile();check();if(!Number.isSafeInteger(file.size)||file.size<0)throw Error('文件大小无法精确读取');result.files.push({file,path:relative});bytes+=BigInt(file.size);progress(result.files.length,result.directories.length);}
   else throw Error('无法识别目录条目');
  }
  check();if(!children)result.emptyDirectories++;
 }
 await visit(root,'');check();result.bytes=bytes.toString();return result;
}
