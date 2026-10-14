// Portable destinations contain only stable identifiers. This is navigation
// over already admitted local data, never an invitation or a history fetch.
const id = /^[0-9a-f]{32}$/, conv = /^[0-9a-f]{64}$/;
const invalid = () => { throw new Error('This AgentNet link is invalid.'); };
export function parseLink(value) {
  if(typeof value!=='string'||value.length>256)invalid();
  const fragment=value.startsWith('agentnet://open#')?value.slice(16):value.startsWith('#')?value.slice(1):null;
  if(!fragment||!/^[a-z0-9=&]+$/.test(fragment))invalid();
  const fields={};
  for(const part of fragment.split('&')){
    const pieces=part.split('=');
    if(pieces.length!==2||!['realm','conv','topic','thread'].includes(pieces[0])||Object.hasOwn(fields,pieces[0]))invalid();
    fields[pieces[0]]=pieces[1];
  }
  if(!id.test(fields.realm))invalid();
  if(fields.thread!==undefined){
    if(Object.keys(fields).length!==2||!id.test(fields.thread))invalid();
  }else if(Object.keys(fields).length!==3||!conv.test(fields.conv)||!(fields.topic==='main'||id.test(fields.topic)))invalid();
  return Object.freeze(fields);
}
export function linkTo(realm, target) {
  if(!id.test(realm))throw new Error('This workspace has no recorded identity for portable links yet.');
  if(!target||Object.keys(target).some(k=>!['conv','topic','thread'].includes(k))||(target.thread!==undefined?Object.keys(target).length!==1:Object.keys(target).length!==2))invalid();
  const value=target?.thread!==undefined
    ? 'agentnet://open#realm='+realm+'&thread='+target.thread
    : 'agentnet://open#realm='+realm+'&conv='+target?.conv+'&topic='+target?.topic;
  parseLink(value);return value;
}
export function matchingWorkspace(target, list) {
  const matches=list.filter(w=>w.state==='enrolled'&&w.realm===target.realm);
  if(matches.length!==1)throw new Error(matches.length?'Choose the intended enrollment of this workspace before opening its link.':'This link belongs to a workspace that is not enrolled on this device.');
  return matches[0];
}
export async function checkTarget(target, api) {
  if(target.thread){
    const view=await api('/api/thread?id='+target.thread);
    if(view?.id!==target.thread||view.topic?.id!==target.thread||!(view.messages||[]).some(m=>(m.id===target.thread||m.lid===target.thread)&&!m.deleted))throw new Error('That conversation root is not available on this device.');
    return;
  }
  const view=await api('/api/dm?id='+target.conv);
  if(view?.id!==target.conv||view.frozen)throw new Error('That conversation is not available on this device.');
  if(target.topic!=='main'&&!(view.topics||[]).some(t=>t.id===target.topic))throw new Error('That exact topic is not available on this device.');
  // Scoped readers with no visible Main data cannot substitute another topic.
  if(target.topic==='main'&&(view.role==='visitor'||view.role==='human_guest')&&!view.main_topic)throw new Error('Main flow is not available in this conversation on this device.');
}
