import { Engine, memoryStore, openIDB } from '../static/engine.mjs';
import { projectDeviceThreads, readThreadRows, readChatView } from '../static/chatprojection.mjs';
let checks=0;const check=(yes,why)=>{if(!yes)throw Error(why);checks++;};
const browser=typeof window!=='undefined',name='chat-projection-'+Date.now(),store=browser?await openIDB(name):memoryStore();
const engine=new Engine({store,base:'https://synthetic.invalid',fetch:async()=>{throw Error('No network during saved chat read');}});
engine.address='owner/desk';engine.fp='a'.repeat(64);
const peer='other/desk',id='1'.repeat(32),answer='2'.repeat(32),conv='3'.repeat(64);
await store.write([
 {s:'outbox',k:id,v:{v:1,id,to:peer,kind:'question',body:'selected question',at:1000,state:'delivered'}},
 {s:'inbox',k:answer,v:{v:1,id:answer,from:peer,fp:'b'.repeat(64),kind:'answer',body:'selected answer',reply_to:id,at:2000,read:true}},
 {s:'outbox',k:'c',v:{v:3,id:'c',control:true,aside:true,sub:'revision',to:peer,at:3000,ref:{id,fingerprint:engine.fp},body:'{"rev":1,"text":"edited question"}'}},
 {s:'inbox',k:'dm',v:{v:2,id:'dm',conv,from:peer,at:1000,body:'selected DM'}},
]);
// Forty MiB of unrelated history must not be materialized by a selected read.
const payload='x'.repeat(2048);
for(let start=0;start<20000;start+=500){
 const ops=[];
 for(let i=start;i<start+500;i++)ops.push({s:i%2?'inbox':'outbox',k:'unrelated-'+i,v:{v:i%2?2:1,id:'unrelated-'+i,conv:i%2?'other-conv':undefined,from:'another/device',to:'another/device',at:i,kind:'message',body:payload}});
 await store.write(ops);
}
const all=store.all.bind(store);store.all=async s=>{if(['inbox','outbox'].includes(s))throw Error('Unscoped '+s+' read');return all(s);};
const started=performance.now(),view=await engine.thread(id),threadMS=performance.now()-started;
check(view.peer===peer&&view.messages.length===2,'selected thread ignores unrelated history');
check(view.messages.find(m=>m.id===id).edited&&view.messages.find(m=>m.id===id).text==='edited question','selected thread applies its same-peer revision');
const rows=await readThreadRows(store,id);check(rows.inbox.length===1&&rows.outbox.length===2,'thread index includes same-peer controls');
const dmStart=performance.now(),dm=await readChatView(store,{me:null,fingerprint:engine.fp},conv),dmMS=performance.now()-dmStart;
check(dm.inbox.length===1&&dm.outbox.length===0&&dm.inbox[0].id==='dm','DM query loads only selected message rows');
// A metadata change updates the peer index in the same transaction; original
// transport endpoints still win over old copied metadata.
await store.write([{s:'inbox',k:answer,v:{...await store.get('inbox',answer),device_history:{peer:'wrong/device',direction:'out'}}}]);
check((await readThreadRows(store,id)).inbox.length===1,'native original ignores stale peer metadata');
await store.write([{s:'inbox',k:'imported',v:{v:1,id:'imported',from:'owner/phone',history:true,replica:true,at:4000,kind:'question',device_history:{peer,direction:'out'}}}]);
check((await store.byThreadPeer('inbox',peer)).some(m=>m.id==='imported'),'imported own-history uses its verified projected peer');
const chain=Array.from({length:30000},(_,i)=>({v:1,id:String(i),from:peer,at:i,reply_to:i?String(i-1):'',kind:'message'}));
const chainStart=performance.now(),groups=projectDeviceThreads({inbox:chain,outbox:[]},{address:engine.address,fingerprint:engine.fp,erased:()=>false}),chainMS=performance.now()-chainStart;
check(groups.length===1&&groups[0].length===chain.length,'long reply chain grouped once without copying each bucket');
store.close();if(browser)await new Promise((resolve,reject)=>{const r=indexedDB.deleteDatabase(name);r.onsuccess=resolve;r.onerror=()=>reject(r.error);});
const result={ok:true,checks,unrelated:20000,thread_ms:threadMS,dm_ms:dmMS,chain_ms:chainMS};
if(browser)window.chatProjectionResult=result;else console.log(JSON.stringify(result));
