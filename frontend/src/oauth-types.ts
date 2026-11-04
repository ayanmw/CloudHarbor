export type OAuthStatus={state:string;ready:boolean;has_refresh_token:boolean;can_refresh:boolean;account_bound:boolean;updated_at:string;expires_at?:string;next_attempt?:string;last_error:string};
const timestamp=(value:unknown):value is string=>typeof value==='string'&&/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(value)&&Number.isFinite(Date.parse(value));
export function validateOAuthStatus(value:OAuthStatus):OAuthStatus{
 if(!value||typeof value.state!=='string'||!value.state||typeof value.ready!=='boolean'||typeof value.has_refresh_token!=='boolean'||typeof value.can_refresh!=='boolean'||typeof value.account_bound!=='boolean'||!timestamp(value.updated_at)||(value.expires_at!==undefined&&!timestamp(value.expires_at))||(value.next_attempt!==undefined&&!timestamp(value.next_attempt))||(value.last_error!==undefined&&typeof value.last_error!=='string'))throw Error('账号状态响应无效');
 return {state:value.state,ready:value.ready,has_refresh_token:value.has_refresh_token,can_refresh:value.can_refresh,account_bound:value.account_bound,updated_at:value.updated_at,expires_at:value.expires_at,next_attempt:value.next_attempt,last_error:value.last_error||''};
}
export function oauthStatusLabel(value:OAuthStatus){
 if(value.ready)return value.state==='expiry_unknown'?'账号可用（到期时间未知）':value.next_attempt?'账号可用，续期等待重试':'账号可用';
 return ({unconfigured:'尚未授权',verification_required:'等待账号验证',account_mismatch:'账号不匹配',reauthorization_required:'需要重新授权',token_rejected:'凭证失效，等待续期',storage_error:'凭证保存失败',closed:'服务已停止'} as Record<string,string>)[value.state]||'账号暂不可用';
}
export type OAuthAppConfig={configured:boolean;editable:boolean;source:''|'env'|'queue';app_id_mask?:string;app_key_mask?:string;secret_key_mask?:string;sign_key_mask?:string};
export function validateOAuthAppConfig(value:OAuthAppConfig):OAuthAppConfig{
 if(!value||typeof value.configured!=='boolean'||typeof value.editable!=='boolean'||!['','env','queue'].includes(value.source)||(value.editable&&(value.configured||value.source!=='')))throw Error('应用配置状态响应无效');
 const result:OAuthAppConfig={configured:value.configured,editable:value.editable,source:value.source};
 for(const key of ['app_id_mask','app_key_mask','secret_key_mask','sign_key_mask'] as const){if(value.configured&&typeof value[key]!=='string')throw Error('应用配置掩码响应无效');if(typeof value[key]==='string')result[key]=value[key];}
 return result;
}
export type OAuthDeviceCode={device_code:string;user_code:string;verification_url:string;qrcode_url:string;expires_in:number;interval:number};
export function validateOAuthDeviceCode(value:OAuthDeviceCode):OAuthDeviceCode{
 if(!value||typeof value.device_code!=='string'||!value.device_code||value.device_code.length>4096||typeof value.user_code!=='string'||!value.user_code||typeof value.verification_url!=='string'||typeof value.qrcode_url!=='string'||!Number.isInteger(value.expires_in)||value.expires_in<1||value.expires_in>3600||!Number.isInteger(value.interval)||value.interval<1||value.interval>60)throw Error('设备码响应无效，请重新获取');
 return value;
}
export function safeBaiduAuthorizationURL(value:string){
 try{const url=new URL(value);return url.protocol==='https:'&&!url.username&&!url.password&&(url.hostname==='baidu.com'||url.hostname.endsWith('.baidu.com'))?url.href:'';}catch{return '';}
}
