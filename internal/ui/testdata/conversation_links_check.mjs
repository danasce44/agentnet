import assert from 'node:assert/strict';
import {parseLink,linkTo,matchingWorkspace,checkTarget} from '../static/conversation-links.mjs';
const realm='a'.repeat(32),conv='b'.repeat(64),topic='c'.repeat(32),thread='d'.repeat(32);
const main=linkTo(realm,{conv,topic:'main'}),named=linkTo(realm,{conv,topic}),legacy=linkTo(realm,{thread});
assert.deepEqual(parseLink(main),{realm,conv,topic:'main'});assert.deepEqual(parseLink(named.slice(15)),{realm,conv,topic});
assert.deepEqual(parseLink(legacy),{realm,thread});
for(const link of [main+'&realm='+realm,main+'&unknown=1',main.replace('realm','%72ealm'),main.replace('open#','open/?x=1#'),main.replace('agentnet:','https:'),main.replace('open#','evil#'),main+'&thread='+thread,legacy+'&topic=main',legacy+'&conv='+conv,main.replace('topic=main','topic='),main.replace('topic=main','topic=../main'),main.replace('realm='+realm,'realm='+realm.toUpperCase()),main+'#extra',main+' ',main.replace('open#','user@open#'),'x'.repeat(257)])assert.throws(()=>parseLink(link),/invalid/);
const a={id:'default',realm,state:'enrolled'},b={...a,id:'1'.repeat(32)};
assert.equal(matchingWorkspace(parseLink(main),[b,{...a,realm:'f'.repeat(32)}]).id,b.id,'different device-local alias resolves the same realm');
assert.throws(()=>matchingWorkspace(parseLink(main),[a,b]),/intended enrollment/);
assert.throws(()=>matchingWorkspace(parseLink(main),[{...a,state:'disconnected'}]),/not enrolled/);
const calls=[],api=async(path,body)=>{calls.push({path,body});assert.equal(body,undefined);return path.includes('/thread')?{id:thread,topic:{id:thread},messages:[{id:thread}]}:{id:conv,role:'member',topics:[{id:topic,title:'Renamed topic'}]};};
await checkTarget(parseLink(main),api);await checkTarget(parseLink(named),api);await checkTarget(parseLink(legacy),api);
assert.deepEqual(calls.map(x=>x.path),['/api/dm?id='+conv,'/api/dm?id='+conv,'/api/thread?id='+thread],'reads exact local targets only');
for(const view of [{id:'e'.repeat(64)},{id:conv,frozen:'removed'},{id:conv,topics:[]}])await assert.rejects(checkTarget(parseLink(named),async()=>view),/not available/);
await assert.rejects(checkTarget(parseLink(legacy),async()=>({id:'e'.repeat(32),topic:{id:thread},messages:[{id:thread}]})),/root/);
await assert.rejects(checkTarget(parseLink(legacy),async()=>({id:thread,topic:{id:thread},messages:[{id:thread,deleted:true}]})),/root/);
for(const role of ['visitor','human_guest']){
 const scoped={id:conv,role,frozen:'',topics:[{id:topic}],messages:[]};
 await assert.rejects(checkTarget(parseLink(main),async()=>scoped),/Main flow/);
 await checkTarget(parseLink(named),async()=>scoped);
 await checkTarget(parseLink(main),async()=>({...scoped,main_topic:{id:'main',state:'active'}}));
}
console.log('conversation links check PASS: strict portable targets, aliases, ambiguity, exact accessible reads, erased roots/topics, no action calls');
