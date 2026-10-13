// Real browser Engine projection/decision guards; transport is recorded only.
import assert from 'node:assert/strict';
import { Engine, memoryStore } from '../static/engine.mjs';
import * as wire from '../static/wire.mjs';
const host='owner/host', address='owner/browser', key='11111111-22222222-33333333-44444444', hostKey='55555555-66666666-77777777-88888888';
const id='1'.repeat(32), report='2'.repeat(32), token='3'.repeat(32), send='4'.repeat(32);
const store=memoryStore(), e=new Engine({store,base:'https://fixture.invalid',fetch:async()=>{throw Error('no network')}});
e.address=address;e.fp=key;e.me={state:'self',human_keys:[key],devices:[{address,fingerprint:key},{address:host,fingerprint:hostKey}]};
const n={token,harness:'codex',host,request:id,key,attempt:1,method:'item/commandExecution/requestApproval',thread:'thread',turn:'turn',item:'item',operation:'printf exact > result.md',cwd:'/owned/work',choices:['allow','deny','cancel'],expires:0};
const item={id,key,kind:'question',state:'running',blocker:'needs_human',attempt:1,actionable:false,native:n};
const notice={id:report,v:1,from:host,fp:hostKey,to:address,kind:'message',status:'review_notice',at:1,body:JSON.stringify({v:2,at:1,host,items:[item]}),read:false};
await store.write([{s:'pins',k:host,v:{address:host,fingerprint:hostKey,pending:null}},{s:'inbox',k:report,v:notice}]);
const shown=async(row=notice)=> (await e.activeReportItems([row]))[0]?.report?.items[0]?.native;
assert.deepEqual(await shown(),n,'current exact owner-human sees live unlimited callback without operator grant');
assert.equal(e.reportItems([notice])[0].report.items[0].native,undefined,'default projection strips private payload');
const calls=[];e.sendDeviceControl=async(...args)=>{calls.push(args);return {id:args.at(-1),state:'queued'}};
const action={host,id,key,action:'continue',expect:'running',attempt:1,report,send_id:send,text:'agentnet-native-response-v1\n'+JSON.stringify({token,choice:'allow'})};
assert.equal((await e.decide(action)).sent,send);
assert.equal(calls[0][4],wire.CapContinuation);
assert.equal(calls[0][5],send);assert.equal(JSON.parse(calls[0][3]).expect,'running');
await e.decide(action);assert.equal(calls[1][5],send,'ambiguous transport retry keeps original decision ID');
for(const change of [{token:'5'.repeat(32),choice:'allow'},{token,choice:'acceptForSession'}])await assert.rejects(()=>e.decide({...action,text:'agentnet-native-response-v1\n'+JSON.stringify(change)}));
await assert.rejects(()=>e.decide({...action,report:'6'.repeat(32)}));
await assert.rejects(()=>e.decide({...action,attempt:2}));
await assert.rejects(()=>e.decide({...action,send_id:''}));
for (const mutate of [
 row=>({...row,history:true}),row=>({...row,replica:true}),row=>({...row,fp:key}),
 row=>({...row,body:JSON.stringify({v:2,host,items:[{...item,native:{...n,expires:Date.now()-1}}]})}),
 row=>({...row,body:JSON.stringify({v:2,host,items:[{...item,native:{...n,host:'foreign/host'}}]})}),
 row=>({...row,body:JSON.stringify({v:2,host,items:[{...item,native:{...n,attempt:2}}]})}),
]) {
 const row=mutate(notice);assert.equal(await shown(row),undefined);
 await store.write([{s:'inbox',k:report,v:row}]);await assert.rejects(()=>e.decide(action));
}
await store.write([{s:'inbox',k:report,v:notice}]);
for (const mutate of [m=>({...m,human_keys:[]}),m=>({...m,state:'conflict'}),m=>({...m,devices:m.devices.filter(d=>d.address!==host)}),m=>({...m,devices:m.devices.filter(d=>d.address!==address)})]) {
 const old=e.me;e.me=mutate(old);assert.equal(await shown(),undefined);await assert.rejects(()=>e.decide(action));e.me=old;
}
for (const pin of [{address:host,fingerprint:hostKey,pending:{fingerprint:key}},{address:host,fingerprint:key,pending:null}]) {
 await store.write([{s:'pins',k:host,v:pin}]);assert.equal(await shown(),undefined);await assert.rejects(()=>e.decide(action));
}
await store.write([{s:'pins',k:host,v:{address:host,fingerprint:hostKey,pending:null}},{s:'kv',k:'revoked-device/'+host+'/'+hostKey,v:{at:1}}]);
assert.equal(await shown(),undefined);await assert.rejects(()=>e.decide(action));
// A current notice must never make an imported callback from that host active.
await store.write([{s:'kv',k:'revoked-device/'+host+'/'+hostKey,v:undefined}]);
const both=await e.activeReportItems([notice,{...notice,id:'7'.repeat(32),history:true}]);
assert.ok(both[0].report.items[0].native);assert.equal(both[1].report.items[0].native,undefined);
assert.equal(calls.length,2,'all invalid responses stay inert');
await store.write([{s:'kv',k:'revoked-device/'+address+'/'+key,v:{at:1}}]);
assert.equal(await shown(),undefined);await assert.rejects(()=>e.decide(action));
console.log('PASS native browser owner/key/role/revocation/private report/live lifetime/history/token/choice/stable ID');
