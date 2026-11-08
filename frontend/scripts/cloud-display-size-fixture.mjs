export const sizeSnapshot='a'.repeat(32);
export function orderedListing(base,field='name',descending=false,mixed=true,token=sizeSnapshot){
 return {...base,total:base.total??base.entries.length,order:{field,descending},display_size:{mixed,snapshot_id:token,cache_generation:'9007199254740993',unknown_directories:0,remote_read_started_at_utc:'2026-11-03T00:59:00Z',remote_observed_at_utc:base.observed_at_utc||'2026-11-03T01:00:00Z',cache_read_at_utc:'2026-11-03T01:00:01Z',created_at_utc:'2026-11-03T01:00:02Z',expires_at_utc:'2026-11-03T01:15:02Z',read_at_utc:'2026-11-03T01:00:03Z'},observed_at_utc:base.observed_at_utc||'2026-11-03T01:00:00Z'};
}
export const displaySizeListing=(base,descending=false,token=sizeSnapshot)=>orderedListing(base,'size',descending,true,token);
