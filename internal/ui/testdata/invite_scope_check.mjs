import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import {stripTypeScriptTypes} from 'node:module';
import {runInNewContext} from 'node:vm';

// The constructor is not used; strip its parameter property for Node's eraser.
const source=(await readFile(new URL('../web/src/store.ts',import.meta.url),'utf8')).replace(/^import .*;\n/gm,'').replace('constructor(readonly host: Host)','constructor(host: Host)');
const Store=runInNewContext(stripTypeScriptTypes(source).replace(/^export /gm,'')+'\nStore');
const store=Object.create(Store.prototype);
store.state={drafts:{}};
store.set=patch=>Object.assign(store.state,patch);
const conv='chat',topic='warehouse';
store.openInvite(conv);
assert.equal(store.state.invite.topic,'','Main flow never defaults to all topics');
store.state.drafts[conv]={text:'',topic};
for(const selected of [undefined,['inside','outside']]){
 store.openInvite(conv,selected);
 assert.equal(store.state.invite.topic,topic,'header, composer and selected-message entries capture the active topic');
 assert.equal(store.state.invite.selected,selected,'selection identities are retained for scoped filtering');
}
store.openInvite(conv,['inside','outside'],{who:'agent',label:'Since they left',topic:undefined});
assert.equal(store.state.invite.topic,topic,'a previous whole-chat invitation cannot erase the visible topic');
const captured=store.state.invite;
store.state.drafts[conv]={text:'',topic:'other'};
assert.equal(captured.topic,topic,'changing topics after opening never expands the invitation');
store.openInvite(conv,undefined,{who:'agent',topic:'old-scope'});
assert.equal(store.state.invite.topic,'other','bringing back an earlier participation starts in the visible topic');
assert.equal(store.state.invite.who,'agent');
console.log('Bring In captures the active topic and explicit Main flow PASS');
