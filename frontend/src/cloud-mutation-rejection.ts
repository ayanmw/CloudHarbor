// Only the explicit server admission contract proves that no write started.
// A bare 409, malformed envelope, or an accompanying operation receipt does not.
export function cloudMutationRejection(status:number,value:any):string|null {
 if(!value||typeof value!=='object'||Array.isArray(value)||value.outcome!=='not_started'||'operation' in value||'operation_id' in value)return null;
 const codes=status===400?['invalid_request']:status===409?['identity_changed','conflict','authentication_waiting']:[];
 if(!codes.includes(value.code)||typeof value.message!=='string'||!value.message.trim()||value.message.length>2048)return null;
 return value.message;
}
