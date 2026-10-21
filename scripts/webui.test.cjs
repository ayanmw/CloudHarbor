const {test} = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '..', 'internal', 'webui');

function page(name, tasks = []) {
    const nodes = new Map();
    const get = id => {
        if (!nodes.has(id)) nodes.set(id, {style:{}, textContent:'', innerHTML:'', querySelector:()=>get(id+' span')});
        return nodes.get(id);
    };
    const listeners = new Map();
    const sockets = [];
    class WebSocket {
        static OPEN = 1;
        constructor() { sockets.push(this); }
    }
    const context = vm.createContext({URL, AbortController, console, WebSocket, location:{protocol:'http:',host:'localhost'},
        document:{getElementById:get, querySelectorAll:()=>[], addEventListener:(event,fn)=>listeners.set(event,fn)},
        setTimeout:()=>0, setInterval:()=>0,
        fetch:async url=>({ok:true,json:async()=>url.includes('status') && !url.includes('oauth') ? {tasks} : {authentication:{state:'unconfigured',ready:false},state:'unconfigured',ready:false}})
    });
    vm.runInContext(fs.readFileSync(path.join(root, 'res', 'shared.js'), 'utf8'), context);
    const html = fs.readFileSync(path.join(root, name), 'utf8');
    for (const match of html.matchAll(/<script>([\s\S]*?)<\/script>/g)) vm.runInContext(match[1], context);
    return {context,get,listeners,sockets};
}

test('WebSocket page separates connection status and account readiness and escapes cloud text',async()=>{
    const p = page('index_ws.html');
    p.listeners.get('DOMContentLoaded')();
    p.sockets[0].onopen();
    await new Promise(resolve=>setImmediate(resolve));
    assert.equal(p.get('token-status-text').textContent,'尚未授权');
    p.context.payload = {tasks:[{status:'failed',path:'<img src=x onerror=alert(1)>',local_path:'<svg onload=alert(2)>',error:'<script>alert(3)</script>',size:3,progress:0}]};
    vm.runInContext('renderTasks(payload)',p.context);
    const html = p.get('tasks-list').innerHTML;
    assert.ok(html.includes('&lt;img'));
    assert.ok(html.includes('&lt;svg'));
    assert.ok(html.includes('&lt;script&gt;'));
    assert.ok(!html.includes('<img') && !html.includes('<script>'));
});
test('poll page escapes cloud text',async()=>{
    const p=page('index_poll.html',[{status:'failed',path:'<img src=x>',error:'<svg>',size:1}]);
    await new Promise(resolve=>setImmediate(resolve));
    assert.ok(p.get('tasks-list').innerHTML.includes('&lt;img'));
    assert.ok(!p.get('tasks-list').innerHTML.includes('<svg>'));
});
test('OAuth page only accepts HTTPS Baidu authorization links',()=>{
    const p=page('index_oauth.html');
    for(const url of ['javascript:alert(1)','http://openapi.baidu.com/device','https://baidu.com.evil.example/','https://user:password@baidu.com/']){
        p.context.inputURL=url;
        assert.equal(vm.runInContext('safeBaiduURL(inputURL)',p.context),'');
    }
    assert.equal(vm.runInContext("safeBaiduURL('https://openapi.baidu.com/device')",p.context),'https://openapi.baidu.com/device');
});
