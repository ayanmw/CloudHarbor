export type CleanupEntry={fs_id:string;path:string;name:string;directory:boolean;size:string;provider_mtime:string;content_md5?:string;content_verified_at_utc?:string};
export type CleanupCandidate={kind:'duplicate'|'empty';entry:CleanupEntry;retained?:CleanupEntry;sha256?:string;observedAt:string};
