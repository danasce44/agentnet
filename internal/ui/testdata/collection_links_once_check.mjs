import {test} from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import http from 'node:http';
import {createRequire} from 'node:module';
const require=createRequire(import.meta.url);
const source=process.env.AGENTNET_SOURCE||path.resolve(import.meta.dirname,'../../..');
const collection=process.env.AGENTNET_COLLECTION_SOURCE;
const loader=fs.readFileSync(process.env.AGENTNET_LOADER_SOURCE||path.join(source,'internal/ui/static/loader.js'),'utf8');
const start=loader.indexOf('    let linkSequence=0;'),end=loader.indexOf('    // An invitation or device link',start);
assert(start>=0&&end>start,'actual portable opener and document listener exist');
const actual=loader.slice(start,end);

test('actual loader and standalone anchors open one exact destination',{skip:!process.env.AGENTNET_CHROMIUM},async t=>{
 assert(collection,'AGENTNET_COLLECTION_SOURCE must name the candidate collection');
 const {chromium}=require(process.env.AGENTNET_PLAYWRIGHT||'playwright');
 const server=http.createServer((req,res)=>{
  const u=new URL(req.url,'http://127.0.0.1');
  if(u.pathname==='/'){res.setHeader('Content-Type','text/html');res.end('<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{margin:0}#skin{height:100vh}</style><div id="skin"></div>');return;}
  if(u.pathname==='/actual-loader'){res.setHeader('Content-Type','text/plain');res.end(actual);return;}
  if(u.pathname==='/conversation-links.mjs'){res.setHeader('Content-Type','text/javascript');res.end(fs.readFileSync(path.join(source,'internal/ui/static/conversation-links.mjs')));return;}
  const m=/^\/(classic|holonet|zoom)\/([^/]+)$/.exec(u.pathname);if(!m){res.statusCode=404;res.end();return;}
  try{
   let data=m[2]==='manifest.mjs'?'export default '+fs.readFileSync(path.join(collection,'skins',m[1],'skin.json'),'utf8'):fs.readFileSync(path.join(collection,'skins',m[1],'src',m[2]));
   if(m[2]==='entry.mjs'){
    const needle=m[1]==='zoom'?'\nstart();':'const ready = start();';assert(data.toString().includes(needle));
    data=data.toString().replace(needle,'globalThis.fixture={state,bodyOf,topicSelections};'+(m[1]==='zoom'?'':'const ready=Promise.resolve();'));
   }
   res.setHeader('Content-Type',m[2].endsWith('.css')?'text/css':'text/javascript');res.end(data);
  }catch(e){res.statusCode=404;res.end(String(e));}
 });
 let browser;
 try{
  await new Promise(r=>server.listen(0,'127.0.0.1',r));const origin='http://127.0.0.1:'+server.address().port;
  browser=await chromium.launch({headless:true,executablePath:process.env.AGENTNET_CHROMIUM});
  for(const skin of ['classic','holonet','zoom'])await t.test(skin+' actual loader click and fallback',async()=>{
   const context=await browser.newContext({viewport:{width:390,height:844},reducedMotion:'reduce'}),page=await context.newPage(),errors=[];
   page.setDefaultTimeout(4000);page.on('pageerror',e=>errors.push(e.message));
   await context.route('**/*',r=>new URL(r.request().url()).origin===origin?r.continue():r.abort());await page.goto(origin);
   try{
    await page.evaluate(async skin=>{
     const shadow=document.querySelector('#skin').attachShadow({mode:'open'}),style=new CSSStyleSheet();style.replaceSync(await(await fetch('/'+skin+'/style.css')).text());shadow.adoptedStyleSheets=[style];
     const app=document.createElement('div');app.className='skin-root';shadow.append(app);
     const realm='1'.repeat(32),conv='c'.repeat(64),topic='b'.repeat(32),workspace={id:'fixture',realm,state:'enrolled'};
     const peer={person:'briar',label:'Briar',address:'briar/phone',devices:[],state:'pinned'},me={person:'atlas',label:'Atlas',address:'atlas/desk',devices:[],state:'self'};
     const message={id:topic,dir:'in',from:peer.address,kind:'message',topic,body:'Exact named destination',at:'2026-10-11T09:02:32Z',author:{label:'Briar',about:'Fixture'},actions:[],can:[]};
     const view={id:conv,kind:'dm',peer,members:[me,peer],agents:[],guests:[],topics:[{id:topic,title:'Exact topic',state:'active'}],messages:[message]};
     const localGate=new Promise(resolve=>{window.releaseLocal=resolve;});
     const api=async(p,body)=>{if(body!==undefined)throw Error('Unexpected mutation '+p);if(p==='/api/dm?id='+conv){await localGate;return structuredClone(view);}if(p.startsWith('/api/typing?'))return {current:false,supported:false};throw Error('Unexpected read '+p);};
     let handler,kinds,portable;
     const host={version:1,workspace,api,listen(){return()=>{}},onOpen(fn,accepted){handler=fn;kinds=accepted;},openLink:value=>portable.open(value)};
     await(await import('/'+skin+'/entry.mjs')).mount(app,host);
     for(const child of app.children)child.hidden=true;
     fixture.state.overview={me:{address:me.address,fingerprint:'atlas-key',agent:false},person:me,people:[peer],threads:[],dms:[],review:[],peers:[]};
     const {parseLink,matchingWorkspace,checkTarget}=await import('/conversation-links.mjs');
     window.hostCalls=[];window.notices=[];window.pending=0;window.fallbackTargets=[];
     window.agentnet={workspace};
     const install=new Function('parseLink','matchingWorkspace','checkTarget','api','handler','kinds',`
      let openPortable;
      const shell={active:'fixture'},workspaces=null,mounting=Promise.resolve(),homeName='Comic';
      let openKinds=new Set(kinds),openHandler=handler;
      const hostFor=()=>({api}),switchTo=()=>{throw Error('Unexpected workspace switch');},notice=words=>notices.push(words);
      ${await(await fetch('/actual-loader')).text()}
      const real=openPortable;
      openPortable=async value=>{hostCalls.push(value);pending++;try{return await real(value);}finally{pending--;}};
      return {open:openPortable,fallback(fn){openHandler=fn;openKinds=new Set(['topic','thread']);}};
     `);
     portable=install(parseLink,matchingWorkspace,checkTarget,api,handler,kinds);
     const href='agentnet://open#realm='+realm+'&conv='+conv+'&topic='+topic;
     const panel=document.createElement('section');panel.id='fixture-messages';app.append(panel);
     panel.append(fixture.bodyOf({...message,body:'[Open exact topic]('+href+')\n\nhttps://example.invalid/external?payload='+('keep'.repeat(30))}));
     window.expected={href,conv,topic};
     window.fallback=()=>{
      app.hidden=true;hostCalls.length=0;notices.length=0;
      portable.fallback((id,kind,context)=>fallbackTargets.push({id,kind,context}));
      const plain=document.createElement('div');document.body.append(plain);const root=plain.attachShadow({mode:'open'}),a=document.createElement('a');a.id='fallback';a.href=href;a.textContent='Plain skin link';root.append(a);
     };
    },skin);
    await page.locator('#fixture-messages a[href^="agentnet://"]').click();await page.evaluate(()=>releaseLocal());
    await page.waitForFunction(()=>hostCalls.length>0&&pending===0&&fixture.state.dm===expected.conv);
    const result=await page.evaluate(()=>({calls:hostCalls.slice(),notices:notices.filter(Boolean),id:fixture.state.dm,topic:fixture.topicSelections[fixture.state.dm],expected}));
    console.log(JSON.stringify({skin,phase:'standalone',...result}));
    assert.deepEqual(result.calls,[result.expected.href]);assert.deepEqual(result.notices,[]);assert.equal(result.id,result.expected.conv);assert.equal(result.topic,result.expected.topic);
    const external=await page.evaluate(()=>{const a=document.querySelector('#skin').shadowRoot.querySelector('#fixture-messages a[href^="https://"]');let prevented;document.addEventListener('click',e=>{prevented=e.defaultPrevented;e.preventDefault();},{once:true});a.dispatchEvent(new MouseEvent('click',{bubbles:true,composed:true,cancelable:true}));return {prevented,calls:hostCalls.length,href:a.href,target:a.target};});
    assert.equal(external.prevented,false);assert.equal(external.calls,1);assert.equal(external.href,'https://example.invalid/external?payload='+('keep'.repeat(30)));assert.equal(external.target,'_blank');
    await page.evaluate(()=>fallback());await page.getByRole('link',{name:'Plain skin link',exact:true}).click();
    await page.waitForFunction(()=>hostCalls.length>0&&pending===0&&fallbackTargets.length>0);
    const fallback=await page.evaluate(()=>({calls:hostCalls,notices:notices.filter(Boolean),targets:fallbackTargets,expected}));
    assert.deepEqual(fallback.calls,[fallback.expected.href]);assert.deepEqual(fallback.notices,[]);assert.deepEqual(fallback.targets,[{id:fallback.expected.conv,kind:'topic',context:{topic:fallback.expected.topic}}]);assert.deepEqual(errors,[]);
   }finally{await context.close();}
  });
 }finally{if(browser)await browser.close();await new Promise(r=>server.close(r));}
});
