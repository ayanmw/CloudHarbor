export type CloudFileEntry={id:string;path:string;name:string;directory:boolean;fs_id:string;size:string;provider_size:string;size_complete?:boolean;server_mtime:string;md5?:string};
export type CloudFileAction={operation:'mkdir'|'rename'|'move'|'copy'|'delete';directory:string;item?:CloudFileEntry;observedAt:string};
export type CloudActionResult={status:string;refresh_required?:boolean;operation_id?:string;message:string;source:string;target:string};

export type CloudFileInfoSnapshot={item:CloudFileEntry;source:'live'|'cache'|'snapshot';observedAt:string;cacheGeneration:string};
