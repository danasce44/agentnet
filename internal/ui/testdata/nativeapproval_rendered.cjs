// Rebuilt Comic over public Host v1: inert owned native requests, no model/OS.
const fs=require('node:fs'),path=require('node:path'),http=require('node:http'),assert=require('node:assert/strict');
const {chromium}=require(process.env.AGENTNET_PLAYWRIGHT);
const assets=path.resolve(__dirname,'../static'), evidence=process.env.AGENTNET_SCREENSHOTS;
assert(evidence);fs.mkdirSync(evidence,{recursive:true});
const boot=String.raw`
const platform=new URL(location.href).searchParams.get('platform');
const address='owner/browser', hostAddress=platform==='daemon'?address:'owner/host', key='11111111-22222222-33333333-44444444';
const me={person:'a'.repeat(64),label:'Owner',address,fingerprint:key,state:'self',human_keys:[key],devices:[{address,fingerprint:key},{address:'owner/host',fingerprint:'55555555-66666666-77777777-88888888'}]};
const base={token:'3'.repeat(32),harness:'codex',host:hostAddress,request:'1'.repeat(32),key,attempt:1,method:'item/commandExecution/requestApproval',thread:'owned-thread',turn:'owned-turn',item:'item',operation:'printf exact > /owned/work/result.md',cwd:'/owned/work',choices:['allow','deny','cancel'],expires:0};
const overview={version:'fixture',seq:1,me:{address,fingerprint:key},person:me,persons:true,agents:true,files:false,controls:false,role:'person',people:[],review:[],links:[],reminders:[],threads:[],topics:[],dms:[],quarantine:[],directory:{current:true,members:[]}};
window.fixture={requests:[],effects:{},lose:false};let opened,changed,counter=3;
window.setNative=(extra={})=>{
const native={...base,token:(++counter).toString(16).padStart(32,'0'),...extra};
overview.review=[{id:'2'.repeat(32),peer:hostAddress,kind:'message',notice:true,why:'Native input',excerpt:'1 request reported',at:new Date().toISOString(),report:{host:hostAddress,at:new Date().toISOString(),items:[{id:base.request,key,kind:'question',state:'running',blocker:'needs_human',attempt:1,actionable:false,native}]}}];
changed?.({type:'change',seq:++overview.seq});
};
window.endNative=()=>{overview.review=[];changed?.({type:'change',seq:++overview.seq});};
const host={version:1,platform,workspace:{id:'default',name:'Owned fixture',endpoint:location.origin,address,realm:'',state:'enrolled'},skins:[],onSkinsChange(){return()=>{}},onOpen(fn){opened=fn},listen(fn){changed=fn;return()=>{}},stage:async()=>{throw Error('no files')},file:async()=>{throw Error('no files')},api:async(p,body)=>{
if(p==='/api/act'||p==='/api/operator/decide'){
 fixture.requests.push({path:p,body});fixture.effects[body.send_id]=(fixture.effects[body.send_id]||0)+(!fixture.effects[body.send_id]?1:0);
 if(fixture.lose){fixture.lose=false;throw Error('Synthetic lost response; retry the exact response');}
 return {note:'Response stored for the current callback'};
}
if(p.startsWith('/api/overview'))return structuredClone(overview);
if(p.startsWith('/api/typing/status'))return {send:false,scopes:[]};
if(p.startsWith('/api/agents'))return {host:hostAddress,agents:[],sessions:[],local:platform==='daemon'};
return {};
}};
window.setNative();
const baseURL='/assets/skins/comic/',manifest=await(await fetch(baseURL+'skin.json')).json();
if(manifest.document){const css=new CSSStyleSheet();css.replaceSync(await(await fetch(baseURL+manifest.document)).text());document.adoptedStyleSheets=[css];}
const shadow=document.querySelector('#skin').attachShadow({mode:'open'}),css=new CSSStyleSheet();css.replaceSync(await(await fetch(baseURL+manifest.style)).text());shadow.adoptedStyleSheets=[css];
const root=document.createElement('div');root.className='skin-root';shadow.append(root);
await(await import(baseURL+manifest.entry)).mount(root,host);
window.openNative=()=>opened('','review');window.ready=true;
`;
const server=http.createServer((req,res)=>{
 const u=new URL(req.url,'http://127.0.0.1');
 if(u.pathname==='/'){res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0}#skin{height:100dvh}</style><div id="skin"></div><script type="module" src="/boot.mjs"></script>');return;}
 if(u.pathname==='/boot.mjs'){res.setHeader('Content-Type','text/javascript');res.end(boot);return;}
 if(u.pathname.startsWith('/assets/')){const f=path.resolve(assets,'.'+u.pathname.slice(7));if(f.startsWith(assets+path.sep)&&fs.existsSync(f)&&fs.statSync(f).isFile()){res.setHeader('Content-Type',f.endsWith('.mjs')?'text/javascript':f.endsWith('.css')?'text/css':f.endsWith('.json')?'application/json':'application/octet-stream');res.end(fs.readFileSync(f));return;}}
 res.statusCode=404;res.end('No fixture route');
});
(async()=>{
 let browser;const errors=[];
 try{
 await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port;
 browser=await chromium.launch({headless:true,executablePath:process.env.AGENTNET_CHROMIUM||'/usr/bin/chromium'});
 for(const platform of ['daemon','browser'])for(const width of [1280,390]){
 const ctx=await browser.newContext({viewport:{width,height:900},hasTouch:width<500,reducedMotion:'reduce'});
 await ctx.route('**/*',r=>new URL(r.request().url()).origin===origin?r.continue():r.abort());
 const page=await ctx.newPage();page.setDefaultTimeout(10000);page.on('pageerror',e=>errors.push(e.stack));
 try{
 await page.goto(origin+'/?platform='+platform);await page.waitForFunction(()=>window.ready);await page.evaluate(()=>openNative());
 const card=()=>page.getByRole('region',{name:'Codex native request'});
 await card().waitFor();await card().getByText('printf exact > /owned/work/result.md',{exact:true}).waitFor();
 assert.equal(await page.getByText(/didn.t let this device decide/).count(),0);
 await card().getByRole('button',{name:'Deny this action',exact:true}).click();
 await page.waitForFunction(()=>fixture.requests.length===1);
 let request=(await page.evaluate(()=>fixture.requests))[0];
 assert.equal(request.path,platform==='daemon'?'/api/act':'/api/operator/decide');
 assert.equal(request.body.attempt,1);assert.equal(request.body.id,'1'.repeat(32));
 assert.equal(JSON.parse((request.body.body||request.body.text).split('\n')[1]).choice,'deny');
 if(platform==='browser'){assert.equal(request.body.expect,'running');assert.equal(request.body.report,'2'.repeat(32));}
 assert.equal(await page.getByRole('dialog').count(),0,'no additional confirmation');
 await card().getByText(/Response saved\./).waitFor();
 await page.evaluate(()=>{fixture.lose=true;setNative()});
 await card().getByRole('button',{name:'Allow this action',exact:true}).click();
 await card().getByRole('button',{name:'Retry response',exact:true}).waitFor();
 await card().getByRole('button',{name:'Retry response',exact:true}).click();
 await page.waitForFunction(()=>fixture.requests.length===3);
 const tries=(await page.evaluate(()=>fixture.requests)).slice(1);assert.deepEqual(tries[0],tries[1]);
 assert.equal(await page.evaluate(()=>Object.values(fixture.effects).reduce((a,b)=>a+b,0)),2,'same-ID retry adds no operation');
 await page.evaluate(()=>setNative({method:'item/tool/requestUserInput',operation:'',choices:['submit','cancel'],cancel_run:true,questions:[{id:'choice',question:'Choose exact option',options:[{label:'Blue',description:'Exact offered option'},{label:'Red',description:'Other option'}]},{id:'secret',question:'Private native input',isSecret:true}]}));
 await card().getByRole('button',{name:'Send native answer',exact:true}).waitFor();
 assert.ok(await card().getByRole('button',{name:'Send native answer',exact:true}).isDisabled());
 await card().getByLabel('Choose exact option').selectOption('Blue');await card().getByLabel('Private native input').fill('isolated-fixture-secret');
 assert.equal(await card().getByLabel('Private native input').getAttribute('type'),'password');
 await card().getByRole('button',{name:'Send native answer',exact:true}).click();await page.waitForFunction(()=>fixture.requests.length===4);
 request=(await page.evaluate(()=>fixture.requests))[3];assert.deepEqual(JSON.parse((request.body.body||request.body.text).split('\n')[1]).answers,{choice:'Blue',secret:'isolated-fixture-secret'});
 assert.equal((await card().innerText()).includes('isolated-fixture-secret'),false);
 await page.evaluate(()=>setNative({method:'mcpServer/elicitation/request',operation:'Fixture MCP form',choices:['submit','deny','cancel'],fields:[{id:'ok',title:'Proceed',type:'boolean',required:true},{id:'tag',title:'Tag',type:'string',required:true,enum:['exact','other']}]}));
 await card().getByLabel('Proceed').selectOption('false');await card().getByLabel('Tag').selectOption('exact');
 await card().getByRole('button',{name:'Send native answer',exact:true}).click();await page.waitForFunction(()=>fixture.requests.length===5);
 request=(await page.evaluate(()=>fixture.requests))[4];assert.deepEqual(JSON.parse((request.body.body||request.body.text).split('\n')[1]).values,{ok:false,tag:'exact'});
 await page.evaluate(()=>setNative({method:'item/permissions/requestApproval',operation:'Native turn permissions',choices:['allow','deny','cancel'],cancel_run:true}));
 await card().getByRole('button',{name:'Cancel',exact:true}).click();await page.waitForFunction(()=>fixture.requests.length===6);
 request=(await page.evaluate(()=>fixture.requests))[5];assert.equal(JSON.parse((request.body.body||request.body.text).split('\n')[1]).choice,'cancel');
 await page.evaluate(()=>setNative({expires:Date.now()+800}));
 await card().getByRole('button',{name:'Allow this action',exact:true}).waitFor();
 await page.waitForFunction(()=>document.querySelector('#skin').shadowRoot.querySelector('section[aria-label="Codex native request"]').textContent.includes('has expired'));
 assert.ok(await card().getByRole('button',{name:'Allow this action',exact:true}).isDisabled());
 await page.evaluate(()=>setNative({expires:Date.now()+2147483647+60000}));await card().getByRole('button',{name:'Allow this action',exact:true}).waitFor();assert.equal(await card().getByRole('button',{name:'Allow this action',exact:true}).isDisabled(),false,'long configured deadline is not an overflowing immediate timer');
 await page.evaluate(()=>setNative({expires:0}));await card().getByRole('button',{name:'Allow this action',exact:true}).waitFor();assert.equal(await card().getByRole('button',{name:'Allow this action',exact:true}).isDisabled(),false);
 const box=await card().boundingBox();assert.ok(box.x>=0&&box.x+box.width<=width+1,'native card fits requesting device');
 await page.screenshot({path:path.join(evidence,platform+'-'+width+'-native.png')});
 await page.evaluate(()=>endNative());await card().waitFor({state:'detached'});
 }finally{await ctx.close()}
 }
 assert.deepEqual(errors,[]);console.log('NATIVE RENDERED PASS: local/remote 1280/390 exact operation deny/allow/input/MCP/cancel/unlimited/expiry/end/stable retry');
 }finally{if(browser)await browser.close();server.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
