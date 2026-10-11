import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import {collectiveHref,parseCollective} from '../skins/shared/collective-mentions.mjs';
import {rawLinkParts} from '../skins/shared/link-text.mjs';

// Exercise the real skin reload functions without a browser or rebuilt assets.
for(const skin of ['classic','zoom']){
 const source=fs.readFileSync(new URL('../skins/'+skin+'/src/entry.mjs',import.meta.url),'utf8');
 const extract=re=>{const found=source.match(re);assert(found,'source function must exist');return found[0];};
 const code=[extract(/^const mentionAt = .*;$/m),extract(/^const validMentions = [\s\S]*?mentionAt\(text, s\)\);/m),extract(/^const mentionAgentPIDs=.*;$/m),extract(/^function selectedAgentPIDs\([\s\S]*?^\}/m),extract(/^function restoreDraft\([\s\S]*?^\}/m)].join('\n');
 if(skin==='zoom'){
  const context={parseCollective,rawLinkParts};vm.createContext(context);
  vm.runInContext([extract(/^const mentionRef = .*;$/m),extract(/^const mentionPlain = .*;$/m),extract(/^function messagePreview\([\s\S]*?^\}/m)].join('\n'),context);
  const thousand=collectiveHref(Array.from({length:1000},(_,i)=>({kind:'person',id:'human-'+i})));
  assert.equal(context.messagePreview('[@everyone]('+thousand+') https://example.com'),'@everyone https://example.com','durable Zoom preview stays compact with 1000 frozen refs');
 }
 const refs=[{kind:'person',id:'human-a'},{kind:'agent',id:'agent-a'},{kind:'agent',id:'agent-b'}];
 const href=collectiveHref(refs);
 const collective={start:0,name:'everyone',ref:{kind:'collective',id:href.slice('agentnet:collective/'.length)}};
 const check=(mentions,agents,legacy,expected)=>{
  const draft={text:'@everyone Check',kind:'question',mentions,...(legacy?{agent:legacy}:{})};
  const body={value:''};let selected;
  const state={dm:'group',drafts:{test:draft}};
  const context={state,parseCollective,$:()=>body,renderPending(){},grow(){},setKind(){},setAnswering(){},setDMReply(){},announce(){},setDMAgent(a){selected=a;},t:{messages:[],agents}};
  vm.runInNewContext(code+'\nrestoreDraft("test",t);',context);
  assert.equal(selected?.pid||null,expected,skin+' restores exact selected participation');
  return {state,selected};
 };
 const active=check([collective],[{pid:'agent-a',can_ask:true}],null,'agent-a');
 assert.deepEqual(JSON.parse(JSON.stringify(active.state.mentions)),[collective],'reload preserves frozen snapshot without legacy fallback');
 const removed=check([collective],[],null,'agent-a');
 assert.equal(removed.selected.can_ask,undefined,'removed target remains an exact unavailable placeholder, never replaced');
 check([{...collective,ref:{kind:'collective',id:collectiveHref([{kind:'person',id:'human-a'}]).slice('agentnet:collective/'.length)}}],[],null,null);
 const malformed=check([{...collective,ref:{kind:'collective',id:'v1/a.agent-a,bad'}}],[],null,null);
 assert.equal(malformed.state.mentions.length,0,'malformed snapshot is inert');
 check([],[], 'agent-c','agent-c');
}
console.log('PASS collective draft reload: real Classic/Zoom functions retain frozen refs, unavailable targets, legacy fallback; invalid snapshots stay inert');
