import * as wire from '../static/wire.mjs';
import {openIDB} from '../static/engine.mjs';

export async function dismissalCanonical({world,check,c,root,conv,keys,address,roster,pub,alicePub,aliceKeys,v}) {
 const source=await world();
 aliceKeys={...aliceKeys,box:v.challenge.alice_box,signPublic:await crypto.subtle.importKey("raw",alicePub.sign_key,{name:"Ed25519"},true,["verify"])};
 try {
  const phoneKeys=await wire.newKeys(),phoneAddress='browser/canonical-phone',phonePub=await wire.publicEntry(phoneKeys,phoneAddress),phoneFP=await wire.fingerprint(phonePub);
  const next=await wire.nextRoster(keys,address,roster,[...roster.devices,phonePub],await wire.joinConsent(phoneKeys,phoneAddress,roster.person,1,await wire.rosterHash(roster)),roster.label,[...await wire.rosterHumans(roster),phoneFP]);
  await wire.verifyNext(next,roster);
  const person=await source.e.personRecord([roster,next],'self',null);
  source.extraChains.set(roster.person,[roster,next].map(r=>JSON.parse(wire.rosterJSON(r))));source.e.me=person;
  await source.st.write([{s:'kv',k:'person',v:person}]);await source.e.pinDevices(person);
  const foreign=await wire.parseRoster(v.challenge.rosters[0]),oldKeys=await wire.newKeys(),oldAddress='alice/retired',oldPub=await wire.publicEntry(oldKeys,oldAddress),oldFP=await wire.fingerprint(oldPub);
  const joined=await wire.nextRoster(aliceKeys,alicePub.address,foreign,[...foreign.devices,oldPub],await wire.joinConsent(oldKeys,oldAddress,foreign.person,1,await wire.rosterHash(foreign)),foreign.label,[...await wire.rosterHumans(foreign),oldFP]);
  const left=await wire.nextRoster(aliceKeys,alicePub.address,joined,foreign.devices,null,foreign.label,await wire.rosterHumans(foreign));
  await wire.verifyNext(joined,foreign);await wire.verifyNext(left,joined);
  const joinedPerson=await source.e.personRecord([foreign,joined],'pinned',await source.st.get('persons',foreign.person));
  source.extraChains.set(foreign.person,[foreign,joined].map(r=>JSON.parse(wire.rosterJSON(r))));
  await source.st.write([{s:'persons',k:foreign.person,v:joinedPerson}]);await source.e.pinDevices(joinedPerson);
  await source.receive(c.proof);await source.receive(c.context);
  const pv=v.participations;
  for(const name of ['member-invite','member-accept','member-question'])await source.receive(pv[name]);
  const invite=wire.parseEvent(pv['member-invite'].inner.body),accepted=wire.parseEvent(pv['member-accept'].inner.body);
  const end=await wire.signEvent(aliceKeys,{conv,pid:invite.pid,type:'dismiss',prev:await wire.eventHash(accepted),ts:1700000210,author:invite.author});
  const inner=(from)=>({v:2,id:wire.newID(),lid:wire.newID(),from,to:address,ts:end.ts,kind:'message',conv,root:wire.rootJSON(root),pid:end.pid,sub:'event',origin:'ui',body:wire.eventJSON(end)});
  const canonical=inner(alicePub.address);await source.receive({envelope:await wire.seal(canonical,aliceKeys,pub)});
  const duplicate=inner(oldAddress);await source.receive({envelope:await wire.seal(duplicate,oldKeys,pub)});
  const stored=await source.st.get('inbox',duplicate.id);
  check(!!stored&&stored.fp===oldFP,'foreign current transport retained signed dismissal before retirement');
  const retired=await source.e.personRecord([foreign,joined,left],'pinned',joinedPerson);
  source.extraChains.set(foreign.person,[foreign,joined,left].map(r=>JSON.parse(wire.rosterJSON(r))));
  await source.st.write([{s:'persons',k:foreign.person,v:retired}]);
  if(source.e.historyRun)await source.e.historyRun;
  if(source.e.retrying)await source.e.retrying;
  source.e.groupSupport=async()=>{};source.e.sendGroupCopy=async()=>'';source.e.ctlSupport=async()=>[false,'legacy fixture'];
  const dev=person.devices.find(d=>d.address===phoneAddress),group=await source.e.groupRecord(conv);
  let rejected=false;try{await source.e.historyCopy(dev,group,source.e.itemOf(stored,false));}catch(e){rejected=!!e.historicalEndTransport;}
  check(rejected,'retired foreign transport remains inadmissible under unchanged source policy');
  const key='history-deferred/'+phoneFP+'/'+conv+'/'+oldFP+'/'+duplicate.lid;
  await source.st.write([{s:'kv',k:key,v:{key,tuple:conv+'/'+oldFP+'/'+duplicate.lid,conv,id:duplicate.id,dir:'inbox',why:'invalid'}}]);
  await source.e.runHistory();
  await source.st.write([{s:'kv',k:key,v:{key,tuple:conv+'/'+oldFP+'/'+duplicate.lid,conv,id:duplicate.id,dir:'inbox',why:'invalid'}}]);
  source.e.historySweeps?.delete(phoneFP);
  await source.e.reconcileHistory();
  const validCanonical=await source.st.get('inbox',canonical.id);
  const write=source.st.write.bind(source.st);let raced=false,conflicted=false,beforeCopies;
  const other=globalThis.indexedDB?await openIDB(source.name):source.st;
  source.st.write=async(ops,checks)=>{
   if(!raced&&ops.some(op=>op.s==='kv'&&op.k===key&&op.v===undefined)) {
    if(!checks?.some(c=>c.s==='inbox'&&c.k===canonical.id))throw Error('alias transaction lacks canonical guard');
    raced=true;beforeCopies=JSON.stringify(await source.st.all('outbox'));
    await (other===source.st?write:other.write.bind(other))([{s:'inbox',k:canonical.id,v:{...validCanonical,at:validCanonical.at+1}}]);
   }
   return write(ops,checks);
  };
  try{for(let page=0;page<12&&!raced;page++)await source.e.historyCatchupStep(dev,(await source.e.historyBook())[phoneAddress]);}catch(e){conflicted=e.message==='storage changed during verification';if(!conflicted)throw e;}
  finally{source.st.write=write;if(other!==source.st)other.close();}
  check(raced&&conflicted,'concurrent canonical source mutation aborts exact alias transaction: raced='+raced+' conflicted='+conflicted);
  check(!!await source.st.get('kv',key)&&JSON.stringify(await source.st.all('outbox'))===beforeCopies,'CAS rollback retains duplicate reference and inserts no partial canonical child');
  await write([{s:'inbox',k:canonical.id,v:validCanonical}]);source.e.historySweeps?.delete(phoneFP);
  await source.e.runHistory();
  const rows=(await source.st.all('outbox')).filter(r=>r.to===phoneAddress&&r.sub==='history'),items=rows.filter(r=>r.body).map(r=>wire.parseHistory(r.body));
  check(!await source.st.get('kv',key),'validated exact signed canonical copy clears obsolete duplicate deferred reference');
  check(items.some(h=>h.id===canonical.id)&&!items.some(h=>h.id===duplicate.id),'only independently valid original-author dismissal is copied');
  check(JSON.stringify(await source.st.get('inbox',duplicate.id))===JSON.stringify(stored),'duplicate original transport and immutable identity remain untouched');
  check(!items.some(h=>h.from_key===oldFP),'retired foreign transport gains no admission or rewritten history identity');
  const ledgerKey='history-copy/'+phoneFP+'/'+conv+'/'+(await wire.fingerprint(alicePub))+'/'+canonical.lid;
  const ledger=await source.st.get('kv',ledgerKey);
  await source.st.write([{s:'kv',k:ledgerKey,v:{...ledger,hash:'f'.repeat(64)}},{s:'kv',k:key,v:{key,tuple:conv+'/'+oldFP+'/'+duplicate.lid,conv,id:duplicate.id,dir:'inbox',why:'invalid'}}]);
  await source.e.runHistory();
  check(!!await source.st.get('kv',key),'conflicting canonical content ledger never retires duplicate bookkeeping');
  await source.st.write([{s:'kv',k:ledgerKey,v:ledger}]);await source.e.runHistory();
  check(!await source.st.get('kv',key),'restoring exact durable canonical coverage resumes held bookkeeping');
  const kept=await source.st.get('outbox',ledger.copy);
  await source.st.write([{s:'outbox',k:kept.id,v:{...kept,state:'archive_accepted',body:''}},{s:'kv',k:key,v:{key,tuple:conv+'/'+oldFP+'/'+duplicate.lid,conv,id:duplicate.id,dir:'inbox',why:'invalid'}}]);
  await source.e.runHistory();
  check(!await source.st.get('kv',key),'trimmed retained canonical ciphertext keeps exact content-ledger coverage');
  const badEvent=wire.parseEvent(wire.eventJSON(end));badEvent.sig[0]^=1;
  const bad={...stored,id:wire.newID(),lid:wire.newID(),body:wire.eventJSON(badEvent)};
  const badKey='history-deferred/'+phoneFP+'/'+conv+'/'+oldFP+'/'+bad.lid;
  await source.st.write([{s:'inbox',k:bad.id,v:bad},{s:'kv',k:badKey,v:{key:badKey,tuple:conv+'/'+oldFP+'/'+bad.lid,conv,id:bad.id,dir:'inbox',why:'invalid'}}]);
  await source.e.runHistory();
  check(!!await source.st.get('kv',badKey),'same event hash with altered signature never aliases valid signed dismissal');
  check(!(await source.st.all('outbox')).filter(r=>r.sub==='history'&&r.body).some(r=>wire.parseHistory(r.body).id===bad.id),'invalid signed duplicate creates no historical carrier');
  await source.reload();
  check(!await source.st.get('kv',key)&&!!await source.st.get('inbox',duplicate.id),'canonical bookkeeping survives actual storage restart');
 } finally {await source.close();}
}
