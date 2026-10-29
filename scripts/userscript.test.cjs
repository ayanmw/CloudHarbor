const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const vm=require('node:vm');
const path=require('node:path');
const script=fs.readFileSync(path.join(__dirname,'../tampermonkey_baidupan_nas_queue.user.js'),'utf8');

function fixture({legacy,vue,pathname='/disk/main',confirm=true,address}={}) {
    const buttons=[],alerts=[],opened=[],menus=[],confirmations=[];
    const window={location:{pathname},yunData:{FILEINFO:[{path:'/整页未选中'}]},confirm:message=>{confirmations.push(message);return confirm;},alert:message=>alerts.push(message),prompt:()=>null};
    if(legacy!==undefined)window.require=()=>({instanceForSystem:{list:{getSelected:()=>legacy}}});
    const roots=vue || {};
    const document={body:{appendChild:node=>buttons.push(node)},getElementById:()=>null,querySelectorAll:selector=>roots[selector] || [],createElement:()=>({style:{},addEventListener(event,fn){this[event]=fn;}})};
    window.document=document;
    const context=vm.createContext({window,unsafeWindow:window,document:{...document,querySelectorAll:()=>[]},URL,console,setInterval:()=>0,GM_getValue:()=>address,GM_setValue:()=>{},GM_registerMenuCommand:(label,fn)=>menus.push(fn),GM_openInTab:(url,options)=>opened.push({url,options}),fetch:()=>{throw Error('userscript must not call API');}});
    vm.runInContext(script,context);
    return {buttons,alerts,opened,menus,confirmations,context};
}

test('legacy selection hands off only paths without credentials or automatic API calls',()=>{
    const f=fixture({legacy:[{path:'/已选择/中文 a.zip',fs_id:3400000000000000123,access_token:'must-not-leak'}]});
    assert.equal(f.buttons.length,1);f.buttons[0].click();
    assert.equal(f.opened.length,1);
    const url=new URL(f.opened[0].url);
    assert.equal(url.origin,'http://127.0.0.1:18089');assert.equal(url.pathname,'/intake');assert.equal(url.search,'');
    assert.deepEqual(JSON.parse(decodeURIComponent(url.hash.slice('#nas-intake='.length))),{version:1,paths:['/已选择/中文 a.zip']});
    assert.ok(!url.href.includes('must-not-leak'));assert.ok(f.confirmations[0].includes('127.0.0.1:18089'));
});

test('Vue selected lists work while ambiguity and unselected FILEINFO fail closed',()=>{
    for(const selector of ['.wp-s-core-pan','.nd-main-list','.nd-new-main-list']) {
        const f=fixture({vue:{[selector]:[{__vue__:{selectedList:[{path:'/已选目录'}]}}]}});f.buttons[0].click();assert.equal(f.opened.length,1);
    }
    for(const options of [{},{legacy:[]},{legacy:[{server_filename:'没有完整路径'}]},{legacy:[{path:'/a'}],vue:{'.wp-s-core-pan':[{__vue__:{selectedList:[{path:'/b'}]}}]}},{legacy:[{path:'/a'}],pathname:'/s/share'},{legacy:[{path:'/a'}],pathname:'/disk/recyclebin'}]) {
        const f=fixture(options);if(f.buttons.length)f.buttons[0].click();assert.equal(f.opened.length,0);if(f.buttons.length)assert.equal(f.alerts.length,1);
    }
});

test('selection bounds, unsafe addresses and cancelled confirmation never open management',()=>{
    for(const paths of [['/a','/a'],['relative'],['/a/../b'],['/a\nb'],['/a '],['/'],Array.from({length:21},(_,i)=>'/'+i)]) {
        const f=fixture({legacy:paths.map(path=>({path}))});f.buttons[0].click();assert.equal(f.opened.length,0);
    }
    for(const address of ['http://remote.example','https://user:pass@host','https://host/?token=x','javascript:alert(1)','https://host/other','http://127.0.0.1.evil.example']) {
        const f=fixture({legacy:[{path:'/a'}],address});f.buttons[0].click();assert.equal(f.opened.length,0);
    }
    const f=fixture({legacy:[{path:'/a'}],confirm:false});f.buttons[0].click();assert.equal(f.opened.length,0);
});
