import fs from 'node:fs/promises';
import assert from 'node:assert/strict';
import {prepare} from '../static/local-skins.mjs';
const [dir,expected]=process.argv.slice(2);
const raw=await fs.readFile(dir+'/skin.json'),m=JSON.parse(raw);
const files=[new File([raw],'skin.json')];for(const name of m.files)files.push(new File([await fs.readFile(dir+'/'+name)],name));
assert.equal((await prepare(files)).item.digest,expected,'native and browser digest bytes');
const invalid=async(change)=>{const bad=structuredClone(m);change(bad);await assert.rejects(prepare([new File([JSON.stringify(bad)],'skin.json'),...files.slice(1)]));};
await invalid(x=>delete x.id);await invalid(x=>x.id='default');await invalid(x=>x.id='comic');await invalid(x=>x.name=' COMIC ');await invalid(x=>x.document='absent.css');await invalid(x=>x.api=2);await invalid(x=>x.files.push(x.files[0]));await invalid(x=>x.files=['../entry.mjs']);await invalid(x=>x.entry='absent.mjs');await invalid(x=>x.files.push('foo.__proto__'));
// Classic and Zoom are agentnet-skins packages: their ids and names are not reserved.
const valid=async(change)=>{const ok=structuredClone(m);change(ok);return (await prepare([new File([JSON.stringify(ok)],'skin.json'),...files.slice(1)])).item;};
for(const [id,name] of [['classic','Classic'],['zoom','Zoom']])assert.deepEqual([(await valid(x=>{x.id=id;x.name=name;})).package_id,(await valid(x=>{x.id=id;x.name=name;})).name],[id,name]);
await assert.rejects(prepare(files.filter(x=>x.name!=='entry.mjs')));await assert.rejects(prepare([new File(['x'.repeat(16385)],'skin.json')]));
await assert.rejects(prepare([new File(['null'],'skin.json')]),/Invalid AgentNet skin manifest/);
console.log('local skin package validation and Go digest parity PASS');
