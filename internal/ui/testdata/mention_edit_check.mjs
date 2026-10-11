import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
for(const skin of ['comic','classic','zoom']){
 const file=skin==='comic'?'../web/src/features/Composer.mentions.ts':'../skins/'+skin+'/src/entry.mjs';
 const source=fs.readFileSync(new URL(file,import.meta.url),'utf8');
 const name=skin==='comic'?'shift':'shiftMentions';
 let code=source.match(new RegExp('(?:export )?function '+name+'\\([\\s\\S]*?^\\}', 'm'))?.[0];
 assert(code,skin+' edit helper exists');
 if(skin==='comic')code=code.replace(/^export function[^\n]+/, 'function shift(spans, prev, cur, caret) {').replaceAll('caret!','caret').replace('const kept: Span[] = [], dropped: Span[] = [];','const kept = [], dropped = [];');
 const mentionAt=(text,s)=>text.slice(s.start,s.start+s.name.length+1)==='@'+s.name;
 const context={mentionAt,intact:mentionAt};vm.createContext(context);vm.runInContext(code,context);
 const token='@Brin’s Analyst ',prev=token+token;
 const refs=[{start:0,name:'Brin’s Analyst',id:'agent-a',ref:{kind:'agent',id:'agent-a'}},{start:token.length,name:'Brin’s Analyst',id:'agent-b',ref:{kind:'agent',id:'agent-b'}}];
 const ids=result=>Array.from(result.kept,s=>s.id);
 assert.deepEqual(ids(context[name](refs,prev,token)),[],skin+' unknown/IME equal-label deletion drops ambiguous exact PIDs');
 assert.deepEqual(ids(context[name]([refs[0]],token,prev)),[],skin+' unknown equal-label insertion cannot guess original PID placement');
 const surround='@Other '+prev+'; @Tail ',remaining='@Other '+token+'; @Tail ';
 const outer=[{start:0,name:'Other',id:'before'},...refs.map(s=>({...s,start:s.start+7})),{start:9+prev.length,name:'Tail',id:'after'}];
 assert.deepEqual(ids(context[name](outer,surround,remaining)),['before','after'],skin+' unknown ambiguous edit preserves only unambiguous surrounding refs');
 assert.deepEqual(ids(context[name](refs,prev,prev)),['agent-a','agent-b'],skin+' no-op preserves exact PIDs');
 assert.deepEqual(ids(context[name](refs,prev,token,0)),['agent-b'],skin+' deleting first identical label preserves second exact PID');
 assert.deepEqual(ids(context[name](refs,prev,token,token.length)),['agent-a'],skin+' deleting second identical label preserves first exact PID');
 assert.deepEqual(ids(context[name](refs,prev,prev+'x',prev.length+1)),['agent-a','agent-b'],skin+' inserting after exact tags preserves both');
 assert.deepEqual(ids(context[name](refs,prev,'x'+prev,1)),['agent-a','agent-b'],skin+' inserting before tags shifts both intact refs');
 assert.deepEqual(ids(context[name](refs,prev,'replacement')),[],skin+' whole replacement drops exact refs without caret hint');
}
console.log('PASS exact mention edits: first/second identical labels preserve correct PID, boundary insertions and full replacement');
