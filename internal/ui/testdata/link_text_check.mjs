import assert from 'node:assert/strict';
import { rawLinkParts, shortLinkLabel, previewText, localRunningDetail, requestResultText } from '../skins/shared/link-text.mjs';
const url='https://example.test/'+('long/'.repeat(25))+'?q=x%2Fy&u=%E2%9C%93#frag';
for(const source of [url,'See '+url+'.','<'+url+'>','**'+url+'**','- '+url,'> '+url]) {
 const parts=rawLinkParts(source).filter(p=>typeof p!=='string');
 assert.equal(parts.length,1,source);assert.equal(parts[0].href,url);assert.equal(parts[0].label,'example.test/…');
}
for(const source of ['`'+url+'`','~~~\n'+url+'\n~~~','[Guide]('+url+')','    '+url]) assert.deepEqual(rawLinkParts(source),[source]);
assert.equal(shortLinkLabel('https://short.test','https://short.test'),'https://short.test');
assert.equal(shortLinkLabel('Descriptive label '+url,url),'Descriptive label '+url);
const credentials=url.replace('example.test','misleading.test@example.test');
assert.equal(shortLinkLabel(credentials,credentials),'misleading.test@example.test/…');
assert.equal(rawLinkParts(url+' and '+url).filter(p=>typeof p!=='string').length,2);
assert.equal(rawLinkParts('`'+url+'` then '+url).filter(p=>typeof p!=='string').length,1);
// Markdown tables must compact raw URLs too; explicit labels and code stay literal.
const table='| Link | Code |\n|---|---|\n| '+url+' | `'+url+'` |';
const tableLinks=rawLinkParts(table).filter(p=>typeof p!=='string');
assert.equal(tableLinks.length,1);assert.equal(tableLinks[0].href,url);assert.equal(tableLinks[0].label,'example.test/…');
console.log('raw link parser vectors passed');

for (const [source,expected] of [
 ['**Stock:** Ready','Stock: Ready\n'],['# Heading\n\n- *One*\n- Two','Heading\n\nOne\nTwo\n'],
 ['\\*literal\\* and `code_*`','*literal* and code_*\n'],['[Guide](https://example.test)','Guide\n'],
 ['<script>alert(1)</script>','<script>alert(1)</script>'],['A &amp; B','A & B\n'],
 ['```\na_* &amp;\n```','a_* &amp;\n'],['ordinary text','ordinary text'],
]) assert.equal(previewText(source),expected,source);
const source='**Stock:** Ready';previewText(source);assert.equal(source,'**Stock:** Ready','preview preserves original copy text');
const clause='Running since 2026-10-11T09:02:32Z. Stop is available; nothing stops automatically unless you set a time limit.';
const oldTZ=process.env.TZ;process.env.TZ='Europe/Kyiv';
try {
 const local=localRunningDetail(clause,true);
 assert(!local.includes('2026-10-11T09:02:32Z'));assert.match(local,/12:02/);
 assert.equal(localRunningDetail(clause,false),clause,'non-running text is untouched');
 const prefix='Seems stuck: no harness output or progress for 5 minutes. ';
 assert.equal(localRunningDetail(prefix+clause,true),prefix+local);
 for(const text of [clause.replace('2026-10-11','2026-02-31'),clause.replace('09:02:32Z','99:02:32Z'),clause+' Other text.','User says '+clause]) assert.equal(localRunningDetail(text,true),text,'unknown/invalid detail stays literal');
 assert.equal(clause,'Running since 2026-10-11T09:02:32Z. Stop is available; nothing stops automatically unless you set a time limit.');
} finally {if(oldTZ===undefined)delete process.env.TZ;else process.env.TZ=oldTZ;}
console.log('preview and local running-detail vectors passed');

// Display projections use the native trailer grammar and never alter stored bytes.
for(const [source,expected] of [
 ['Need a choice\nemotion: curious','Need a choice'],
 ['Need a choice\nreaction: -👍\ntopic: done\nemotion: waiting','Need a choice'],
 ['Need a choice\ntopic: done\nreaction: 🇺🇦','Need a choice'],
 ['Need a choice\n REACTION : 👩🏽‍💻 \r\n EMOTION : calm \r\n','Need a choice'],
 ['Need a choice\nemotion: \u0085curious\u0085','Need a choice'],
 ['Need a choice\nemotion: \uFEFFcurious','Need a choice\nemotion: \uFEFFcurious'],
 ['emotion: curious','emotion: curious'],
 ['Need a choice\nemotion: Calm','Need a choice\nemotion: Calm'],
 ['Need a choice\nemotion: too long and invalid','Need a choice\nemotion: too long and invalid'],
 ['Need a choice\nreaction: 👍👍','Need a choice\nreaction: 👍👍'],
 ['Need a choice\nreaction: $','Need a choice\nreaction: $'],
 ['Need a choice\nreaction: 👍\nMore literal prose','Need a choice\nreaction: 👍\nMore literal prose'],
 ['Need a choice\n\x60emotion: curious\x60','Need a choice\n\x60emotion: curious\x60'],
 ['Need a choice\n\n    emotion: curious','Need a choice\n\n    emotion: curious'],
 ['Need a choice\n\n~~~\nemotion: curious\n~~~','Need a choice\n\n~~~\nemotion: curious\n~~~'],
 ['Need a choice\n\n~~~\nemotion: curious','Need a choice\n\n~~~\nemotion: curious'],
 ['Need a choice\n> emotion: curious','Need a choice\n> emotion: curious'],
 ['Need a choice\n[emotion: curious](https://example.test)','Need a choice\n[emotion: curious](https://example.test)'],
]) { const original=source;assert.equal(requestResultText(source),expected,source);assert.equal(source,original); }
console.log('request result trailer projection vectors passed');
