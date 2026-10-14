// Real persisted CacheStorage: damage one Chromium index, recover via the
// production manager/gallery, and compare healthy skin and non-skin bytes.
// This component fixture is not the parent's retained Android APK/profile gate.
const fs = require('node:fs/promises'), path = require('node:path'), http = require('node:http');
const assert = require('node:assert/strict'), {createHash} = require('node:crypto');
const {chromium} = require(process.env.AGENTNET_PLAYWRIGHT);
const [profile] = process.argv.slice(2), prefix = 'agentnet-skin-package-v1-';
const server = http.createServer(async (req, res) => {
  try {
    if(req.url === '/') { res.setHeader('Content-Type','text/html');res.end('<!doctype html><title>Skin storage fixture</title>');return; }
    const file=path.basename(req.url);
    if(!['local-skins.mjs','skin-choice.mjs','skin-gallery.mjs','sw.js'].includes(file)){res.writeHead(404).end();return;}
    res.setHeader('Content-Type','text/javascript');res.end(await fs.readFile(path.join(__dirname,'../static',file)));
  }catch(e){res.writeHead(500).end(String(e));}
});
const pkg = (id, version=1) => [
  {name:'skin.json',body:JSON.stringify({api:1,id,name:id,entry:'entry.mjs',files:['entry.mjs','style.css'],style:'style.css'})},
  {name:'entry.mjs',body:'export const version = '+version+';'}, {name:'style.css',body:'h1 { color: black; }'},
];
let context;
async function snapshot(page, healthy) {
  return page.evaluate(async healthy=>{
    const cache=await caches.open('agentnet-skin-package-v1-'+healthy.digest), files=[];
    for(const key of await cache.keys())files.push([key.url,[...new Uint8Array(await (await cache.match(key)).arrayBuffer())]]);
    const catalog=await caches.open('agentnet-skin-catalog-v1');
    const catalogBytes=[...new Uint8Array(await (await catalog.match('/local-skin-catalog/'+encodeURIComponent(healthy.id))).arrayBuffer())];
    const db=await new Promise((resolve,reject)=>{const r=indexedDB.open('saved-data-fixture');r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
    const records=await new Promise((resolve,reject)=>{const tx=db.transaction('saved'),r=tx.objectStore('saved').getAll();r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});db.close();
    return {files,catalogBytes,localStorage:Object.fromEntries(Object.entries(localStorage)),records};
  },healthy);
}
async function corruptIndex(profile, digest) {
  const storage=path.join(profile,'Default','Service Worker','CacheStorage');
  for(const sub of await fs.readdir(storage)) {
    const directory=path.join(storage,sub), index=await fs.readFile(path.join(directory,'index.txt'));
    const name=Buffer.from(prefix+digest), at=index.indexOf(name);
    if(at<0)continue;
    const uuid=index.subarray(at+name.length+2,at+name.length+2+36).toString();
    assert.match(uuid,/^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/);
    await fs.writeFile(path.join(directory,uuid,'index'),'wrong file structure');return;
  }
  assert.fail('exact damaged cache directory identified');
}
async function showManager(page) {
  await page.evaluate(async()=>{
    window.localSkins=await import('/local-skins.mjs');
    window.stopManager?.();document.getElementById('manager')?.remove();
    const root=document.createElement('section');root.id='manager';document.body.append(root);
    window.stopManager=localSkins.manager(root,{changed:async()=>{}});
  });
  await page.getByRole('button',{name:'Remove healthy',exact:true}).waitFor();
}
(async()=>{
  assert(profile,'owned persistent profile directory is required');
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const origin='http://127.0.0.1:'+server.address().port;
  const launch=()=>chromium.launchPersistentContext(profile,{headless:true,executablePath:process.env.AGENTNET_CHROMIUM||'/usr/bin/chromium'});
  context=await launch();let page=await context.newPage();await page.goto(origin);
  const install=(input)=>page.evaluate(async input=>{
    window.localSkins=await import('/local-skins.mjs');
    return localSkins.install(input.map(f=>new File([f.body],f.name)));
  },input);
  const healthy=await install(pkg('healthy')), damaged=await install(pkg('damaged'));
  await page.evaluate(async({healthy,damaged})=>{
    localStorage.setItem('agentnet.skin',healthy.id);
    localStorage.setItem('agentnet.skin.trusted.'+healthy.id,healthy.digest);
    localStorage.setItem('agentnet.skin.trusted.'+damaged.id,damaged.digest);
    localStorage.setItem('agentnet.messenger.drafts.default',JSON.stringify({chat:{text:'Retained draft'}}));
    const db=await new Promise((resolve,reject)=>{const r=indexedDB.open('saved-data-fixture',1);r.onupgradeneeded=()=>r.result.createObjectStore('saved',{keyPath:'id'});r.onsuccess=()=>resolve(r.result);r.onerror=()=>reject(r.error);});
    await new Promise((resolve,reject)=>{const tx=db.transaction('saved','readwrite'),s=tx.objectStore('saved');
      s.put({id:'identity',value:{address:'fixture/phone',key:new Uint8Array([1,2,3,255])}});
      s.put({id:'chat',value:[{id:'original-message',body:'Saved chat',files:['file']}]});
      s.put({id:'file',value:new Uint8Array([0,128,255,7])});
      s.put({id:'queued',value:{id:'queued-original-id',bytes:new Uint8Array([5,4,3,2,1])}});
      tx.oncomplete=resolve;tx.onerror=()=>reject(tx.error);
    });db.close();await localSkins.activate();
  },{healthy,damaged});
  const before=await snapshot(page,healthy);
  await context.close();context=null;
  // The per-cache protobuf index records the ASCII cache name followed by its
  // UUID directory. Assert that exact binding before damaging the owned fixture.
  await corruptIndex(profile,damaged.digest);
  context=await launch();page=await context.newPage();await page.goto(origin);
  const observed=await page.evaluate(async digest=>{
    const c=await caches.open('agentnet-skin-package-v1-'+digest);
    try{await c.match('/local-skins/ready');return 'unexpected success';}catch(e){return e.name+': '+e.message;}
  },damaged.digest);
  assert.match(observed,/UnknownError.*Unexpected internal error/,'actual corrupt backend signature');
  assert.deepEqual(await snapshot(page,healthy),before,'cold persisted non-skin and healthy bytes unchanged');
  const other=await install(pkg('another'));
  assert(await page.evaluate(d=>caches.has('agentnet-skin-package-v1-'+d),damaged.digest),'unrelated install retains unreadable snapshot');
  assert.equal((await page.evaluate(()=>localSkins.catalog())).some(x=>x.id===damaged.id),false,'unreadable package is not advertised as installed');
  assert.deepEqual(await snapshot(page,healthy),before);
  await showManager(page);
  const damagedRow=page.locator('#manager .item').filter({has:page.getByRole('button',{name:'Remove damaged',exact:true})});
  assert.match(await damagedRow.textContent(),/Needs repair/,'unreadable chosen entry remains reachable');
  page.once('dialog',dialog=>dialog.accept());
  await page.getByRole('button',{name:'Remove another',exact:true}).click();
  await page.getByText('Removed from this browser.',{exact:true}).waitFor();
  assert.equal(await page.evaluate(d=>caches.has('agentnet-skin-package-v1-'+d),other.digest),false);
  assert(await page.evaluate(d=>caches.has('agentnet-skin-package-v1-'+d),damaged.digest),'unrelated corrupt package retained after explicit removal');
  assert.deepEqual(await snapshot(page,healthy),before);
  // Supply original verified bytes through the production gallery. Its list
  // offers Install for the unreadable latest digest instead of an unusable Use.
  const input=pkg('damaged'), files=Object.fromEntries(input.map(f=>[f.name,Buffer.from(f.body)]));
  const skin={id:'damaged',name:'damaged',version:'1.0.0',api:1,digest:damaged.digest,
    files:Object.entries(files).map(([path,b])=>({path,size:b.length,sha256:createHash('sha256').update(b).digest('hex')}))};
  const repaired=await page.evaluate(async({input,skin})=>{
    const {gallery,LATEST,RAW}=await import('/skin-gallery.mjs');
    const g=gallery({fetcher:async url=>{
      if(url===LATEST)return Response.json({tag_name:'v1.0.0'});
      if(url===RAW+'v1.0.0/published/catalog.json')return Response.json({catalog:1,skins:[skin]});
      return new Response(input.find(f=>url.endsWith('/'+f.name))?.body);
    }});
    const before=(await g.list()).skins[0].state,id=await g.install('damaged'),after=(await g.list()).skins[0].state;
    return {before,id,after};
  },{input,skin});
  assert.deepEqual(repaired,{before:'install',id:damaged.id,after:'installed'});
  assert.deepEqual(await snapshot(page,healthy),before,'repair preserves exact stored values and healthy catalog/trust/selection');
  await context.setOffline(true);
  assert.equal(await page.evaluate(async digest=>(await fetch('/local-skins/'+digest+'/entry.mjs')).text(),damaged.digest),'export const version = 1;','repaired bytes served offline by the actual worker');
  assert.equal(await page.evaluate(async digest=>(await fetch('/local-skins/'+digest+'/entry.mjs')).text(),healthy.digest),'export const version = 1;','healthy package remains usable offline');
  await context.setOffline(false);
  const removebad=await install(pkg('removebad'));
  await context.close();context=null;
  await corruptIndex(profile,removebad.digest);
  context=await launch();page=await context.newPage();await page.goto(origin);
  assert.deepEqual(await snapshot(page,healthy),before,'second cold start preserves exact stored data');
  await showManager(page);
  await page.getByRole('button',{name:'Remove removebad',exact:true}).waitFor();
  const corruptRow=page.locator('#manager .item').filter({has:page.getByRole('button',{name:'Remove removebad',exact:true})});
  assert.match(await corruptRow.textContent(),/Needs repair/);
  page.once('dialog',dialog=>dialog.accept());
  await page.getByRole('button',{name:'Remove removebad',exact:true}).click();
  await page.getByText('Removed from this browser.',{exact:true}).waitFor();
  assert.equal(await page.evaluate(d=>caches.has('agentnet-skin-package-v1-'+d),removebad.digest),false,'actual damaged cache removed by existing manager');
  assert.equal((await page.evaluate(()=>localSkins.catalog())).length,2);
  assert.deepEqual(await snapshot(page,healthy),before,'manager removal preserves exact stored data/healthy catalog/trust/selection');
  console.log('local skin cache recovery PASS: actual corrupt Chromium disk index, unrelated install/removal, reachable repair entries, verified gallery repair, exact persisted data/healthy catalog/trust/selection, offline fetch, cold reload, explicit corrupt-cache removal');
})().catch(e=>{console.error(e);process.exitCode=1;}).finally(async()=>{if(context)await context.close();await new Promise(resolve=>server.close(resolve));});
