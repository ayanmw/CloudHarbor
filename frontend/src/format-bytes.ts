// Byte identities stay exact; only the displayed value is rounded.
export function formatBytes(value:string|number|bigint|null|undefined):string {
 if(value===null||value===undefined)return '—';
 if(typeof value==='number'){if(!Number.isFinite(value)||value<0)return '—';value=BigInt(Math.round(value));}
 let n:bigint;try{if(typeof value==='string'&&!/^(0|[1-9][0-9]*)$/.test(value))return '—';n=BigInt(value);}catch{return '—';}
 if(n<0n)return '—';const units=['B','KiB','MiB','GiB','TiB','PiB','EiB'];let power=1n,index=0;
 while(index<units.length-1&&n>=power*1024n){power*=1024n;index++;}
 if(index===0)return n+' B';let rounded=(n*100n+power/2n)/power;
 if(rounded>=102400n&&index<units.length-1){power*=1024n;index++;rounded=(n*100n+power/2n)/power;}
 const fraction=(rounded%100n).toString().padStart(2,'0').replace(/0+$/,'');return (rounded/100n).toString()+(fraction?'.'+fraction:'')+' '+units[index];
}
