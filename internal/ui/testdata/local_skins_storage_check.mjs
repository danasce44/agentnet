// Storage failures with the real package manager/gallery. The browser companion
// additionally corrupts Chromium's on-disk index and checks persistent bytes.
import assert from 'node:assert/strict';
import * as local from '../static/local-skins.mjs';
import { gallery, LATEST, RAW } from '../static/skin-gallery.mjs';
import { createHash } from 'node:crypto';

const origin = 'http://127.0.0.1:8123', prefix = 'agentnet-skin-package-v1-';
const ready = origin + '/local-skins/ready', cat = 'agentnet-skin-catalog-v1';
globalThis.location = { origin };
Object.defineProperty(globalThis, 'navigator', { value: { serviceWorker: {}, locks: { request: async (_, fn) => fn() } }, configurable: true });
const sha = bytes => createHash('sha256').update(bytes).digest('hex');
const pkg = (id, version = 1) => [new File([JSON.stringify({ api: 1, id, name: id, entry: 'entry.mjs', files: ['entry.mjs', 'style.css'], style: 'style.css' })], 'skin.json'),
  new File(['export const version = ' + version + ';'], 'entry.mjs'), new File(['h1 { color: black; }'], 'style.css')];
const key = request => typeof request === 'string' ? request : request.url || request.href;
class Cache {
  responses = new Map(); damaged = false; writes = 0;
  async match(request) {
    if (this.damaged) throw new DOMException('Unexpected internal error.', 'UnknownError');
    return this.responses.get(key(request))?.clone();
  }
  async keys() { return [...this.responses.keys()].map(url => new Request(url)); }
  async put(request, response) {
    this.writes++;
    if (this.writeError && (!this.failAfter || this.writes >= this.failAfter)) throw this.writeError;
    this.responses.set(key(request), response.clone());
  }
  async delete(request) { return this.responses.delete(key(request)); }
}
function world() {
  const store = new Map(), deleted = [], created = [];
  globalThis.caches = {
    async open(name) {
      if (!store.has(name)) {
        const cache = new Cache(); if(this.failName===name){cache.writeError=this.writeError;cache.failAfter=this.failAfter;}
        store.set(name, cache);created.push({name,cache});
      }
      if(this.openError)throw this.openError;
      return store.get(name);
    },
    async has(name) { return store.has(name); },
    async keys() { return [...store.keys()]; },
    async delete(name) {
      deleted.push(name);
      if (this.deleteError) throw this.deleteError;
      return store.delete(name);
    },
  };
  return { store, deleted, created };
}
{ // Same-digest repair is idempotent; a duplicate checks bytes without rewrites.
  const w = await seed(); w.store.get(prefix+w.damaged.digest).damaged = true;
  assert.deepEqual(await local.install(pkg('damaged')), w.damaged);
  const c=w.store.get(prefix+w.damaged.digest), writes=c.writes, deletions=w.deleted.length;
  assert.deepEqual(await local.install(pkg('damaged')), w.damaged);
  assert.equal(c.writes,writes);assert.equal(w.deleted.length,deletions);
  assert.deepEqual(await snapshot(w.store,prefix+w.healthy.digest),w.before);
}
async function snapshot(store, name) {
  const c = store.get(name);
  return Promise.all([...c.responses].map(async ([url, r]) => [url, [...new Uint8Array(await r.clone().arrayBuffer())]]));
}
const assetURL = (item, name) => origin + '/local-skins/' + item.digest + '/' + name;
async function seed() {
  const w = world(), healthy = await local.install(pkg('healthy')), damaged = await local.install(pkg('damaged'));
  return { ...w, healthy, damaged, before: await snapshot(w.store, prefix + healthy.digest) };
}

{ // The observed Chromium failure no longer blocks a verified gallery update.
  const w = await seed(); w.store.get(prefix + w.damaged.digest).damaged = true;
  const files = pkg('damaged', 2), prepared = await local.prepare(files), published = {};
  for (const f of files) published[f.name] = new Uint8Array(await f.arrayBuffer());
  const skin = { id: 'damaged', name: 'damaged', version: '2.0.0', api: 1, digest: prepared.item.digest,
    files: Object.entries(published).map(([path, bytes]) => ({ path, size: bytes.length, sha256: sha(bytes) })) };
  const g = gallery({ fetcher: async url => {
    if (url === LATEST) return Response.json({ tag_name: 'v2.0.0' });
    if (url === RAW + 'v2.0.0/published/catalog.json') return Response.json({ catalog: 1, skins: [skin] });
    return new Response(published[url.slice((RAW + 'v2.0.0/published/damaged/').length)]);
  } });
  assert.equal((await g.list()).skins[0].state, 'install', 'unreadable package is offered for repair');
  assert.equal(await g.install('damaged'), w.damaged.id);
  assert.equal((await g.list()).skins[0].state, 'installed');
  assert.deepEqual(w.deleted, [prefix+w.damaged.digest], 'verified new bytes replace only the chosen unreadable package');
  assert.equal(w.store.has(prefix+w.damaged.digest), false, 'chosen unreadable old snapshot does not permanently consume quota');
  assert.deepEqual(await snapshot(w.store, prefix + w.healthy.digest), w.before, 'healthy bytes are exact');
  assert.equal((await local.catalog()).find(x => x.id === w.damaged.id).digest, prepared.item.digest);
}
{ // Parent negative: explicit removal of a corrupt chosen ID keeps unrelated bytes.
  const w=await seed();w.store.get(prefix+w.damaged.digest).damaged=true;
  const healthyCatalog=await w.store.get(cat).match(origin+'/local-skin-catalog/'+encodeURIComponent(w.healthy.id)).then(r=>r.text());
  await local.remove(w.damaged.id);
  assert.equal(w.store.has(prefix+w.damaged.digest),false);
  assert.equal(w.store.get(cat).responses.has(origin+'/local-skin-catalog/'+encodeURIComponent(w.damaged.id)),false);
  assert.deepEqual(w.deleted,[prefix+w.damaged.digest]);
  assert.deepEqual(await snapshot(w.store,prefix+w.healthy.digest),w.before);
  assert.equal(await w.store.get(cat).match(origin+'/local-skin-catalog/'+encodeURIComponent(w.healthy.id)).then(r=>r.text()),healthyCatalog);
  console.log('explicit corrupt chosen removal PASS');
}
{ // Parent negative: four tiny corrupt skins permit a verified NEW digest update.
  const w=world(),healthy=await local.install(pkg('healthy')),before=await snapshot(w.store,prefix+healthy.digest), damaged=[];
  for(let i=0;i<4;i++)damaged.push(await local.install(pkg('damaged-'+i)));
  for(const item of damaged)w.store.get(prefix+item.digest).damaged=true;
  const next=await local.install(pkg('damaged-0',2));
  assert.notEqual(next.digest,damaged[0].digest);
  assert.deepEqual(w.deleted,[prefix+damaged[0].digest]);
  for(const item of damaged.slice(1))assert.equal(w.store.get(prefix+item.digest).damaged,true);
  assert.deepEqual(await snapshot(w.store,prefix+healthy.digest),before);
  assert.equal((await local.catalog()).find(x=>x.id===next.id).digest,next.digest);
  console.log('four corrupt skins verified new-digest update PASS');
}
{ // Removing another chosen skin ignores unrelated corruption and preserves its entry.
  const w=await seed();w.store.get(prefix+w.damaged.digest).damaged=true;
  const before=await w.store.get(cat).match(origin+'/local-skin-catalog/'+encodeURIComponent(w.damaged.id)).then(r=>r.text());
  await local.remove(w.healthy.id);
  assert.deepEqual(w.deleted,[prefix+w.healthy.digest]);
  assert.equal(w.store.get(prefix+w.damaged.digest).damaged,true);
  assert.equal(await w.store.get(cat).match(origin+'/local-skin-catalog/'+encodeURIComponent(w.damaged.id)).then(r=>r.text()),before);
}
{ // Failed exact delete preserves the catalog recovery/removal action.
  const w=await seed();w.store.get(prefix+w.damaged.digest).damaged=true;
  const before=await snapshot(w.store,cat);caches.deleteError=new DOMException('unavailable','InvalidStateError');
  await assert.rejects(local.remove(w.damaged.id),/storage/i);
  assert.deepEqual(await snapshot(w.store,cat),before);
  assert.equal(w.store.get(prefix+w.damaged.digest).damaged,true);
  assert.deepEqual(await snapshot(w.store,prefix+w.healthy.digest),w.before);
}
{ // Catalog deletion failure retains its exact entry; explicit removal retry converges.
  const w=await seed(), c=w.store.get(cat), before=await snapshot(w.store,cat), original=c.delete.bind(c);
  c.delete=async()=>{throw new DOMException('unavailable','InvalidStateError');};
  await assert.rejects(local.remove(w.damaged.id),/storage/i);
  assert.deepEqual(await snapshot(w.store,cat),before);
  assert.deepEqual(await snapshot(w.store,prefix+w.healthy.digest),w.before);
  c.delete=original;await local.remove(w.damaged.id);
  assert.equal(c.responses.has(origin+'/local-skin-catalog/'+encodeURIComponent(w.damaged.id)),false);
}
{ // Malformed ready metadata alone cannot erase usable old bytes on a failed update.
  const w=await seed(), c=w.store.get(prefix+w.damaged.digest);c.responses.set(ready,new Response('{'));
  const before=await snapshot(w.store,prefix+w.damaged.digest), metadata=await snapshot(w.store,cat);
  w.store.get(cat).writeError=new DOMException('unavailable','InvalidStateError');
  await assert.rejects(local.install(pkg('damaged',2)),/storage/i);
  assert.deepEqual(await snapshot(w.store,prefix+w.damaged.digest),before);
  assert.deepEqual(await snapshot(w.store,cat),metadata);
}
{ // Missing or malformed ready metadata repair retains verified files on failure.
  for(const marker of ['missing','malformed'])for(const error of ['QuotaExceededError','InvalidStateError']){
    const w=await seed(), name=prefix+w.damaged.digest, c=w.store.get(name);
    if(marker==='missing')c.responses.delete(ready);else c.responses.set(ready,new Response('{'));
    const before=await snapshot(w.store,name), metadata=await snapshot(w.store,cat), writes=c.writes;
    c.writeError=new DOMException('unavailable',error);caches.failName=name;caches.writeError=c.writeError;
    await assert.rejects(local.install(pkg('damaged')),/storage/i);
    assert.deepEqual(w.deleted,[],marker+': no deletion');
    assert.equal(w.store.get(name),c,marker+': original cache retained');
    assert.equal(c.writes,writes+1,marker+': only ready write attempted');
    assert.deepEqual(await snapshot(w.store,name),before);
    assert.deepEqual(await snapshot(w.store,cat),metadata);
    assert.deepEqual(await snapshot(w.store,prefix+w.healthy.digest),w.before);
    c.writeError=null;caches.failName='';assert.deepEqual(await local.install(pkg('damaged')),w.damaged);
  }
  console.log('missing/malformed ready marker failed-write preservation and explicit retry PASS');
}
{ // A ready marker does not excuse absent or changed target bytes on reinstall.
  for (const corrupt of ['missing', 'changed', 'metadata']) {
    const w = await seed(), c = w.store.get(prefix + w.damaged.digest), asset = assetURL(w.damaged, 'entry.mjs');
    if (corrupt === 'missing') c.responses.delete(asset);
    else if (corrupt === 'changed') c.responses.set(asset, new Response('corrupt bytes'));
    else c.responses.set(ready, new Response('{'));
    if(corrupt!=='changed')assert.equal((await local.catalog()).some(x=>x.id===w.damaged.id),false,'incomplete snapshot is offered for reinstall');
    assert.deepEqual(await local.install(pkg('damaged')), w.damaged, corrupt + ': original digest restored');
    assert.equal(await w.store.get(prefix + w.damaged.digest).match(asset).then(r => r.text()), 'export const version = 1;');
    assert.deepEqual(await snapshot(w.store, prefix + w.healthy.digest), w.before);
  }
}
{ // Failed update keeps the previous usable version/catalog; retry is explicit.
  const w = await seed(), files = pkg('damaged', 2), next = (await local.prepare(files)).item;
  caches.failName = prefix + next.digest; caches.writeError = new DOMException('full', 'QuotaExceededError');
  await assert.rejects(local.install(files), /Skin.*storage|Skin not stored/i);
  assert.equal((await local.catalog()).find(x => x.id === w.damaged.id).digest, w.damaged.digest);
  assert.deepEqual(await snapshot(w.store, prefix + w.healthy.digest), w.before);
  assert.equal(w.created.filter(x=>x.name===prefix+next.digest).reduce((n,x)=>n+x.cache.writes,0), 1, 'no automatic full-cache write retry');
  caches.failName = '';
  assert.deepEqual(await local.install(files), next, 'one subsequent explicit retry succeeds');
}
{ // Storage unavailability is not evidence for deleting a readable snapshot.
  const w = await seed(), c = w.store.get(prefix + w.healthy.digest);
  c.match = async () => { throw new DOMException('unavailable', 'InvalidStateError'); };
  await assert.rejects(local.install(pkg('newskin')), /storage/i);
  assert.deepEqual(w.deleted, []);
  assert.deepEqual(await snapshot(w.store, prefix + w.healthy.digest), w.before);
}
{ // Backend-wide open failure, even UnknownError, cannot erase a package.
  const w=await seed();caches.openError=new DOMException('unavailable','UnknownError');
  await assert.rejects(local.install(pkg('newskin')),/storage/i);
  assert.deepEqual(w.deleted,[]);
}
{ // Partial rebuild failure never publishes ready/catalog; the old version stays.
  const w=await seed(), files=pkg('damaged',2), next=(await local.prepare(files)).item;
  caches.failName=prefix+next.digest;caches.writeError=new DOMException('full','QuotaExceededError');caches.failAfter=2;
  await assert.rejects(local.install(files),/storage/i);
  const partial=w.created.find(x=>x.name===prefix+next.digest).cache;
  assert.equal(partial.writes,2);assert.equal(partial.responses.has(ready),false);
  assert.equal(w.store.has(prefix+next.digest),false);
  assert.equal((await local.catalog()).find(x=>x.id===w.damaged.id).digest,w.damaged.digest);
  assert.deepEqual(await snapshot(w.store,prefix+w.healthy.digest),w.before);
}
{ // A catalog-write failure keeps old publication; its durable new snapshot can retry.
  const w=await seed(), files=pkg('damaged',2), next=(await local.prepare(files)).item;
  const c=w.store.get(cat), before=await snapshot(w.store,cat);
  c.writeError=new DOMException('unavailable','InvalidStateError');
  await assert.rejects(local.install(files),/storage/i);
  assert.deepEqual(await snapshot(w.store,cat),before);
  assert.deepEqual(await snapshot(w.store,prefix+w.healthy.digest),w.before);
  const stored=w.store.get(prefix+next.digest), writes=stored.writes;
  c.writeError=null;assert.deepEqual(await local.install(files),next);assert.equal(stored.writes,writes);
}
{ // Invalid metadata cannot turn the 64 MiB cap into free space.
  const w=await seed();
  for(let i=0;i<4;i++) {
    const digest=String(i+1).repeat(64),id='local:unknown-'+i;
    const c=await caches.open(prefix+digest);c.responses.set(ready,Response.json({id,size:-1}));
    w.store.get(cat).responses.set(origin+'/local-skin-catalog/'+encodeURIComponent(id),Response.json({local:true,id,digest,size:-1}));
  }
  await assert.rejects(local.install(pkg('newskin')),/64 MiB limit/);
  assert.deepEqual(w.deleted,[]);
  assert.deepEqual(await snapshot(w.store,prefix+w.healthy.digest),w.before);
}
{ // A failed exact-cache deletion aborts; it never clears other storage.
  const w = await seed(); w.store.get(prefix + w.damaged.digest).damaged = true;
  caches.deleteError = new DOMException('unavailable', 'InvalidStateError');
  await assert.rejects(local.install(pkg('damaged')), /storage/i);
  assert.deepEqual(w.deleted, [prefix + w.damaged.digest]);
  assert.deepEqual(await snapshot(w.store, prefix + w.healthy.digest), w.before);
}
{ // Corrupt catalog cannot be replaced at the expense of healthy skin records.
  const w = await seed(); w.store.get(cat).damaged = true;
  await assert.rejects(local.install(pkg('newskin')), /storage/i);
  assert.deepEqual(w.deleted, []);
  assert.deepEqual(await snapshot(w.store, prefix + w.healthy.digest), w.before);
}
{ // Orphans/incomplete snapshots are bounded; unrelated cache names stay put.
  const w = await seed();
  const incomplete = prefix + 'a'.repeat(64); await caches.open(incomplete);
  await caches.open('agentnet-skin-package-v1-unrelated'); await caches.open('other-application');
  await local.install(pkg('newskin'));
  assert.deepEqual(w.deleted, [incomplete]);
  assert(w.store.has('agentnet-skin-package-v1-unrelated')); assert(w.store.has('other-application'));
}
console.log('local skin storage check PASS: exact-cache repair, verified bytes, rollback, bounded failures, healthy packages');
