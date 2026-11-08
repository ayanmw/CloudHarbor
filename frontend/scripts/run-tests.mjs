import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const result=spawnSync(process.execPath,['--experimental-vm-modules',fileURLToPath(new URL('./test.mjs',import.meta.url))],{stdio:'inherit'});
if(result.error)throw result.error;
process.exit(result.status??1);
