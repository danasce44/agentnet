const assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path');
const {chromium}=require(process.env.AGENTNET_PLAYWRIGHT);
(async()=>{
 const browser=await chromium.launch({executablePath:process.env.AGENTNET_CHROMIUM||'/usr/bin/chromium',headless:true,args:['--no-sandbox']});
 try {for(const width of [390,1280]) {
  const context=await browser.newContext({viewport:{width,height:1000},reducedMotion:'reduce'});
  const page=await context.newPage(),errors=[];
  page.on('pageerror',error=>errors.push(String(error)));
  page.setDefaultTimeout(7000);
  try {
  await page.goto(process.argv[2]);
  await page.getByRole('navigation',{name:'Main',exact:true}).getByRole('button',{name:width<700?'You':'Settings',exact:true}).click();
  await page.getByRole('button',{name:/^Your devices/}).click();
  const remove=page.getByRole('button',{name:'Remove Phone',exact:true});
  await remove.waitFor();
  const card=page.locator('li').filter({has:remove});
  assert.equal(await card.count(),1,'exact device row');
  assert.ok((await card.innerText()).includes('One of your devices'),'quiet primary subtitle');
  assert.equal(await card.getByRole('progressbar').count(),0,'background progress hidden when collapsed');
  assert.ok(!/pending history|waiting for context|need attention|waiting to send|on the server|stored on the device/.test(await card.innerText()),'technical counts absent from collapsed row');
  const details=card.getByRole('button',{name:'Details',exact:true});
  await details.click();
  await card.getByText('phone-key',{exact:true}).waitFor();
  const text=await card.innerText();
  for(const value of ['owner/phone','7 pending history items','6 blocked history copies','8 waiting to send','212 on the server, waiting for the device','19 history items accepted for background import']) assert.ok(text.includes(value),value+' retained under Details');
  assert.ok(!text.includes('waiting for context')&&!text.includes('need attention'),'no invented reason or approval claim');
  const progress=card.getByRole('progressbar',{name:'Copying your chats to Phone',exact:true});
  assert.equal(await progress.getAttribute('aria-valuenow'),'3');
  assert.equal(await progress.getAttribute('aria-valuemax'),'9');
  assert.equal(await remove.isVisible(),true,'Remove action remains available');
  assert.equal(await card.evaluate(e=>e.scrollWidth>e.clientWidth+1),false,'expanded row fits width '+width);
  if(process.env.AGENTNET_SCREENSHOTS){fs.mkdirSync(process.env.AGENTNET_SCREENSHOTS,{recursive:true});await page.screenshot({path:path.join(process.env.AGENTNET_SCREENSHOTS,'devices-details-'+width+'.png')});}
  await details.click();
  assert.equal(await card.getByRole('progressbar').count(),0,'technical progress collapses again');
  assert.ok(!/pending history|blocked history|waiting to send/.test(await card.innerText()),'counts collapse again');
  assert.deepEqual(errors,[],'page errors');
  console.log('Device Details',width,'PASS');
  } catch(error) {
   if(process.env.AGENTNET_SCREENSHOTS){fs.mkdirSync(process.env.AGENTNET_SCREENSHOTS,{recursive:true});await page.screenshot({path:path.join(process.env.AGENTNET_SCREENSHOTS,'devices-details-failure-'+width+'.png')});}
   console.error(await page.evaluate(()=>document.querySelector('#skin')?.shadowRoot?.querySelector('.skin-root')?.innerText||document.body.innerText));
   throw error;
  } finally {await context.close();}
 }}finally{await browser.close();}
})().catch(error=>{console.error(error);process.exit(1)});
