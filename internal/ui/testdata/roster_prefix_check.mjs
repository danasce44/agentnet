import assert from 'node:assert/strict';
import * as wire from '../static/wire.mjs';
import {Engine, memoryStore} from '../static/engine.mjs';

// A valid older chain prefix is missing freshness, not a conflicting identity.
// This isolates the two reads straddling an ordinary own-device roster update.
const keys=await wire.newKeys(),address='owner/phone',first=await wire.newRoster(keys,address,'Owner');
const next=await wire.nextRoster(keys,address,first,first.devices,null,'Updated label');
await wire.verifyFirst(first);await wire.verifyNext(next,first);
const store=memoryStore(),engine=new Engine({store,base:'https://fixture.invalid',fetch:async()=>{throw Error('unexpected network');}});
engine.keys=keys;engine.address=address;engine.fp=await wire.fingerprint(await wire.publicEntry(keys,address));
const pinned=await engine.personRecord([first,next],'self',null);engine.me=pinned;
await store.write([{s:'kv',k:'person',v:pinned}]);
engine.pinChain=async()=>pinned;
engine.chain=async()=>[first];
const root={creator:{person:first.person,roster:await wire.rosterHash(first)}};
const resolved=await engine.groupResolver(root,[],null,{rosters:{}},[]);
assert.equal(await wire.rosterHash(resolved.resolve(first.person,root.creator.roster)),root.creator.roster);
assert.deepEqual(await store.get('kv','person'),pinned,'historical prefix never rolls back current own identity');
const newRoot={creator:{person:first.person,roster:pinned.hash}};
await assert.rejects(engine.groupResolver(newRoot,[],null,{rosters:{}},[]),e=>e.reason==='proof_pending','a missing newer step waits for proof');
const fork=await wire.nextRoster(keys,address,first,first.devices,null,'Different branch');
engine.chain=async()=>[first,fork];
await assert.rejects(engine.groupResolver(newRoot,[],null,{rosters:{}},[]),e=>e.reason==='identity_conflict','actual verified fork remains blocked');
console.log('roster prefix PASS: older verified prefix, missing fresh proof, actual fork and pinned state preservation');
