const { test } = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const puppeteer = require('puppeteer');
const { BrowserSession } = require('../dist/src/browser-session');
const { NavigationRequest, NavigationAction: A } = require('../dist/generated/bc');

async function fixture(t) {
 let slowResponse, requested;
 const slowRequest = new Promise(resolve => { requested = resolve; });
 const server = http.createServer((req,res) => {
  if (req.url==='/slow' || req.url==='/timeout') {slowResponse=res;requested();return;}
  res.setHeader('content-type','text/html');
  if (req.url==='/published') {res.end('<meta name="termium-welcome" content="1"><title>Hosted welcome</title><h1>Hosted welcome</h1>');return;}
  if (req.url==='/unbranded') {res.end('<h1>Domain parking</h1>');return;}
  if (req.url==='/custom') {res.end('<title>My own home</title><input aria-label="Search"><a href="/next">Next</a>');return;}
  const files={'/':'index.html','/assets/welcome.css':'assets/welcome.css','/assets/mark.svg':'assets/mark.svg'};
  if (!files[req.url]) {res.writeHead(404);res.end();return;}
  if (req.url.endsWith('.css'))res.setHeader('content-type','text/css');
  if (req.url.endsWith('.svg'))res.setHeader('content-type','image/svg+xml');
  res.end(fs.readFileSync(path.join(__dirname,'../../site',files[req.url])));
 });
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 t.after(()=>{server.closeAllConnections();server.close();});
 return {url:`http://127.0.0.1:${server.address().port}`,slowRequest,finishSlow:()=>slowResponse.end('<meta name="termium-welcome" content="1"><h1>Late response</h1>')};
}
async function browserFor(t) {
 const browser=await puppeteer.launch({headless:true,pipe:true,enableExtensions:true});
 t.after(()=>browser.close());return browser;
}

test('welcome shares website assets, fits terminal viewports, and keeps offline control', {timeout:30000},async t=>{
 const f=await fixture(t), browser=await browserFor(t);
 const session=new BrowserSession(async()=>browser);
 const home=()=>session.command(NavigationRequest.fromPartial({action:A.HOME}));
 await home();
 let page=await session.ensurePage();
 assert.equal(await page.$eval('.ascii-mark',e=>e.getAttribute('role')),'img');
 assert.equal(await page.$$eval('.key-grid>div',es=>es.length),6);
 // Verify the actual page layout, not CSS text or a screenshot's existence.
 for (const [width,height] of [[624,320],[984,640],[320,600],[280,600]]) {
  await session.setViewport(width,height);
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),`horizontal overflow at ${width}×${height}`);
  assert.ok(await page.$eval('h1',e=>e.getBoundingClientRect().bottom>0));
  if (width>=624) assert.ok(await page.$eval('footer>p',e=>e.getBoundingClientRect().bottom<=innerHeight),'core help fell below terminal viewport');
  // Exercise wider fallback glyphs, as found on machines with different fonts.
  const style = await page.addStyleTag({content:'h1,dd {font-family:monospace!important}'});
  assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),`fallback-font overflow at ${width}×${height}`);
  await style.evaluate(e=>e.remove());
 }
 const website=url=>session.vimium.evaluate(url=>globalThis.chrome.storage.local.set({termiumWebsite:url}),url);
 const settled=()=>page.evaluate(async()=>{await import('./termium.js');});
 await website(f.url+'/unbranded');await home();await settled();
 assert.match(page.url(),/^chrome-extension:/,'parking page replaced welcome');
 await website(f.url+'/slow');await home();await f.slowRequest;
 await page.keyboard.press('j');f.finishSlow();await settled();
 assert.match(page.url(),/^chrome-extension:/,'late website stole input focus');
 await website(f.url+'/timeout');await home();await settled();
 assert.match(page.url(),/^chrome-extension:/,'network timeout replaced offline welcome');
 await website(f.url+'/published');await home();
 await page.waitForFunction(()=>location.pathname==='/published',{timeout:5000});
 assert.equal(page.url(),f.url+'/published');
 assert.equal(await page.evaluate(()=>typeof globalThis.chrome?.tabs),'undefined','remote page gained extension privileges');
});

test('custom homepage controls Home, new tabs and last-tab replacement', {timeout:30000},async t=>{
 const f=await fixture(t),browser=await browserFor(t);
 const session=new BrowserSession(async()=>browser,()=>{},()=>f.url+'/custom');
 const command=action=>session.command(NavigationRequest.fromPartial({action}));
 await command(A.HOME);
 let page=await session.ensurePage();await page.waitForSelector('input');
 assert.equal(page.url(),f.url+'/custom');
 await command(A.NEW_TAB);page=await session.ensurePage();await page.waitForSelector('input');
 assert.equal(page.url(),f.url+'/custom');assert.equal((await session.state()).tabs.length,2);
 // Vimium's own t uses the same custom destination.
 const existing = new Set((await browser.pages()).map(p=>p.target()));
 await page.keyboard.press('Escape');
 await session.vimium.waitForPage(page);await page.keyboard.type('t');
 await browser.waitForTarget(t=>t.type()==='page' && t.url()===f.url+'/custom' && !existing.has(t));
 for(const p of await browser.pages())await p.close();
 await session.state();page=await session.ensurePage();await page.waitForSelector('input');
 assert.equal(page.url(),f.url+'/custom');
});
