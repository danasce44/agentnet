// A collective is one visible tag with a frozen, signed-text snapshot of
// exact advisory references. Only an explicit local selection creates jobs;
// received links, names and later shared-list changes grant nothing.
export const collectivePrefix = 'agentnet:collective/v1/';
const kinds = {p:'person',g:'guest',a:'agent'};
const letters = {person:'p',guest:'g',agent:'a'};
const maxRefs = 4096, maxPayload = 280000;
export function parseCollective(value) {
  if(typeof value!=='string'||!value.startsWith(collectivePrefix))return null;
  const payload=value.slice(collectivePrefix.length);
  if(!payload||payload.length>maxPayload)return null;
  const parts=payload.split(',');if(parts.length>maxRefs)return null;
  const refs=[],seen=new Set();
  for(const part of parts){
    const m=/^([pga])\.([A-Za-z0-9_-]{1,64})$/.exec(part);
    if(!m)return null;
    if(!seen.has(part)){seen.add(part);refs.push({kind:kinds[m[1]],id:m[2]});}
  }
  return refs;
}
export function collectiveHref(refs) {
  if(!Array.isArray(refs)||!refs.length||refs.length>maxRefs)return null;
  const parts=[];
  for(const ref of refs){if(!letters[ref?.kind]||typeof ref.id!=='string'||!/^([A-Za-z0-9_-]{1,64})$/.test(ref.id))return null;parts.push(letters[ref.kind]+'.'+ref.id);}
  const href=collectivePrefix+[...new Set(parts)].join(',');
  return parseCollective(href)?href:null;
}
export const collectiveRef = /\[@([^\[\]\r\n]{1,80})\]\((agentnet:collective\/v1\/[^\s()]{1,280000})\)/g;
export function collectivePlain(text) {
  return String(text||'').replace(collectiveRef,(all,name,href)=>parseCollective(href)?'@'+name:all);
}
export function collectiveOptions(people, view) {
  const unique = list => { const seen=new Set(); return list.filter(p=>{ const key=p.kind==='agent'?'agent/'+p.id:'person/'+(p.person||p.id); if(seen.has(key))return false; seen.add(key); return true; }); };
  const all = unique(people), out = [];
  const option = (id, name, targets, skipped=0) => ({
    key: 'collective/'+id, kind: 'collective', id, name: String(name||'').replace(/[\[\]\r\n]/g,'').trim().slice(0,80), seed: id, targets,
    sub: targets.length+' here'+(skipped?' · '+skipped+' outside this chat':'')+' · exact recipients',
  });
  if (all.length) out.push(option('everyone', 'everyone', all));
  if (!view?.current) return out;
  for (const team of view.teams || []) {
    if (!team.listed || team.conflict || team.archived) continue;
    const people = new Set(team.members || []), agents = team.agents || [];
    const targets = unique(all.filter(p => p.kind === 'agent'
      ? agents.some(a => a.id === p.agentID && a.host === p.host && a.host_key === p.hostKey)
      : people.has(p.person || p.id)));
    if (targets.length) out.push(option(team.id, team.name, targets, Math.max(0,people.size+agents.length-targets.length)));
  }
  return out.filter(c=>c.name);
}
