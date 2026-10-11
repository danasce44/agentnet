import assert from 'node:assert/strict';
import {collectiveOptions,collectiveHref,parseCollective,collectivePlain} from '../skins/shared/collective-mentions.mjs';

const person={kind:'person',id:'human-a',name:'Alex'},guest={kind:'guest',id:'guest-a',person:'human-a',name:'Alex'};
const agent={kind:'agent',id:'pid-a',name:'Codex',agentID:'agent-a',host:'owner/laptop',hostKey:'key-a'};
const lookalike={...agent,id:'pid-b',host:'owner/other',hostKey:'key-b'};
const members=[person,guest,agent,lookalike];
const team={id:'team-a',name:'Reviewers',listed:true,members:['human-a','outside'],agents:[{id:agent.agentID,host:agent.host,host_key:agent.hostKey}]};
const view={current:true,teams:[team]};
const choices=collectiveOptions(members,view);
assert.equal(choices[0].name,'everyone');assert.equal(choices[0].targets.length,3,'human is addressed once even with an active guest participation');
assert.deepEqual(choices[1].targets.map(p=>p.id),['human-a','pid-a']);
assert.match(choices[1].sub,/1 outside this chat/);
assert.deepEqual(collectiveOptions(members,{...view,current:false}).map(c=>c.id),['everyone']);
for(const flag of ['archived','conflict'])assert.equal(collectiveOptions(members,{current:true,teams:[{...team,[flag]:true}]}).length,1);
assert.equal(collectiveOptions(members,{current:true,teams:[{...team,listed:false}]}).length,1);
assert.equal(collectiveOptions([lookalike],view).length,1,'same name/id on another host must not be addressed by the tag');
assert.equal(collectiveOptions([{...agent,hostKey:'changed'}],view).length,1,'changed host key must not inherit the tag');
assert.equal(collectiveOptions([],view).length,0,'a tag never invites its members');
const captured=structuredClone(choices[1].targets);
team.agents=[];team.members=[];
assert.deepEqual(choices[1].targets,captured,'editing the shared list must not rewrite already expanded recipients');
console.log('PASS collective mentions: exact host/key, current chat, duplicate humans, stale/archived/conflicting lists, captured targets');

// A thousand frozen recipients keep one visible token, without hiding names.
const thousand=Array.from({length:1000},(_,i)=>({kind:i%2?'agent':'person',id:i.toString(16).padStart(32,'0')}));
const href=collectiveHref(thousand),signed='[@everyone]('+href+') Check inventory';
assert.equal(parseCollective(href).length,1000);
assert.equal(collectivePlain(signed),'@everyone Check inventory');
assert.equal((signed.match(/\[@/g)||[]).length,1);
assert.deepEqual(parseCollective(collectiveHref([...thousand,...thousand])),thousand,'overlaps retain each exact reference once');
for(const bad of ['agentnet:collective/v2/p.good','agentnet:collective/v1/','agentnet:collective/v1/a.good,p.good,b.bad','agentnet:collective/v1/a.good,','agentnet:collective/v1/a.bad/name','agentnet:collective/v1/p.'+'x'.repeat(65),'agentnet:collective/v1/'+Array(4097).fill('a.good').join(',')])assert.equal(parseCollective(bad),null,bad.slice(0,80));
assert.equal(collectiveHref([{kind:'task',id:'admin'}]),null);
assert.equal(collectivePlain('[@everyone](agentnet:collective/v1/a.bad/name)'),'[@everyone](agentnet:collective/v1/a.bad/name)','invalid metadata stays inert text');
console.log('PASS compact snapshot: 1000 refs, bounded validation, exact dedup, one visible tag');
