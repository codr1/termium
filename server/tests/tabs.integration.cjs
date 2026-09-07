const { test } = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const puppeteer = require('puppeteer');
const { BrowserSession } = require('../dist/src/browser-session');
const { InputEvent, InputKind, NavigationAction: A, NavigationRequest } = require('../dist/generated/bc');

async function waitFor(fn, message) {
    const end = Date.now() + 6000;
    do { const result = await fn(); if (result) return result; await new Promise(r => setTimeout(r, 15)); } while (Date.now() < end);
    throw Error(message);
}

test('bundled Vimium and real Chromium tabs share input, selection and capture', { timeout: 60000 }, async t => {
    const frameServer = http.createServer((_req,res) => { res.setHeader('content-type','text/html'); res.end(`<input><button onclick="document.title='frame clicked'">Frame button</button>`); });
    await new Promise(r => frameServer.listen(0, '127.0.0.1', r));
    t.after(() => frameServer.close());
    const fixture = http.createServer((req, res) => {
        res.setHeader('content-type', 'text/html');
        if (req.url === '/stalled') return; // Navigation must leave Stop usable.
        if (req.url === '/frames') {
            res.end(`<iframe src="http://127.0.0.1:${frameServer.address().port}/"></iframe><div id="shadow"></div><script>document.querySelector('#shadow').attachShadow({mode:'open'}).innerHTML='<input>'</script>`);
            return;
        }
        res.end(`<!doctype html><title>Fixture</title><style>body{margin:20px;height:2000px;background:#335577}a,input,button{display:block;margin-bottom:20px}</style><a href="/destination">Destination</a><input><button onclick="document.title='clicked'">Click me</button>`);
    });
    await new Promise(r => fixture.listen(0, '127.0.0.1', r));
    t.after(() => fixture.close());
    const browser = await puppeteer.launch({ headless: true, pipe: true, enableExtensions: true });
    t.after(() => browser.close());
    const session = new BrowserSession(async () => browser, page => { page.on('pageerror', error => t.diagnostic('Page error: '+error.message)); });
    const command = (action, fields = {}) => session.command(NavigationRequest.fromPartial({ action, ...fields }));
    const send = async (fields) => {
        const s = await session.state();
        return session.input(InputEvent.fromPartial({ generation: s.generation, tabId: s.activeTabId, ...fields }));
    };
    const text = async value => { try { return await send({ kind: InputKind.TEXT_INPUT, text: value }); } catch (error) { error.message = `While typing ${JSON.stringify(value)}: ${error.message}`; throw error; } };
    const key = key => send({ kind: InputKind.KEY_INPUT, key });
    const url = `http://127.0.0.1:${fixture.address().port}`;
    await session.ensurePage();
    await command(A.NAVIGATE, { url });
    await waitFor(async () => (await session.state()).url === url+'/' && !(await session.state()).loading, 'initial navigation');
    await session.setViewport(800, 600);
    let page = await session.ensurePage();
    const first = await session.state();
    await text('f');
    await page.waitForSelector('.vimiumHintMarker', { visible: true, timeout: 5000 });
    const labels = await page.$$eval('.vimiumHintMarker', es => es.map(e => e.textContent));
    assert.equal(labels.length, 3);
    const hints = await session.capture('png');
    assert.equal(hints.tabId, first.activeTabId);
    assert.equal(hints.state.activeTabId, first.activeTabId);
    assert.deepEqual([...hints.data.subarray(0,8)], [137,80,78,71,13,10,26,10]);
    await text(labels[2].toLowerCase());
    await page.waitForFunction(() => document.title === 'clicked');
    await page.click('input');
    await text('fjgg');
    await send({kind:InputKind.PASTE_INPUT,text:' literal f'});
    assert.equal(await page.$eval('input', e => e.value), 'fjgg literal f');
    await key('Escape');
    await text('j'); await page.waitForFunction(() => scrollY > 0);
    await text('gg'); await page.waitForFunction(() => scrollY === 0);

    // Same URL, different page state: URL matching must never determine identity.
    await command(A.NEW_TAB, { url });
    const second = await waitFor(async () => { const s = await session.state(); return s.url === url+'/' && !s.loading && s; }, 'second navigation');
    assert.equal(second.tabs.length, 2);
    assert.notEqual(second.activeTabId, first.activeTabId);
    page = await session.ensurePage();
    await page.evaluate(() => { document.body.style.background = '#771122'; });
    const screenshot = await session.capture('png');
    assert.equal(screenshot.state.activeTabId, second.activeTabId);
    assert.equal(screenshot.data.readUInt32BE(16), 800);
    assert.equal(screenshot.data.readUInt32BE(20), 600);

    await text('F');
    await page.waitForSelector('.vimiumHintMarker', { visible: true });
    const destinationHint = await page.$eval('.vimiumHintMarker', e => e.textContent.toLowerCase());
    await text(destinationHint);
    const background = await waitFor(async () => { const s = await session.state(); return s.tabs.length===3 && s; }, 'F did not create a background tab');
    assert.equal(background.activeTabId, second.activeTabId);
    await text('J');
    await waitFor(async () => (await session.state()).activeTabId===first.activeTabId, 'Vimium J did not select first tab');
    assert.equal(await (await session.ensurePage()).$eval('input', e => e.value), 'fjgg literal f');
    await assert.rejects(session.input(InputEvent.fromPartial({ tabId: second.activeTabId, generation: second.generation, kind: InputKind.TEXT_INPUT, text:'WRONG' })), /changed/);
    const selected = await command(A.SELECT_TAB, {tabId:second.activeTabId});
    assert.equal(selected.activeTabId,second.activeTabId);
    assert.equal(await (await session.ensurePage()).evaluate(() => getComputedStyle(document.body).backgroundColor),'rgb(119, 17, 34)');
    // A canceled unsaved-changes dialog keeps the selected tab alive.
    page = await session.ensurePage();
    await page.evaluate(() => { onbeforeunload = e => { e.preventDefault(); e.returnValue = ''; }; });
    const dismissed = new Promise(resolve => page.once('dialog', async dialog => {
        assert.equal(dialog.type(), 'beforeunload'); await dialog.dismiss(); resolve();
    }));
    await command(A.CLOSE_TAB);
    await dismissed;
    assert.equal((await session.state()).activeTabId, second.activeTabId);
    await page.evaluate(() => { onbeforeunload = null; });
    await text('x');
    await waitFor(async () => !(await session.state()).tabs.some(t=>t.id===second.activeTabId),'x did not close tab');
    await text('X');
    await waitFor(async () => (await session.state()).tabs.length===3,'X did not restore tab');

    // Browser UI commands must survive an idle MV3 worker being stopped.
    await command(A.CLOSE_TAB);
    const closedCount = (await session.state()).tabs.length;
    const workerCDP = await (await session.ensurePage()).createCDPSession();
    await workerCDP.send('ServiceWorker.enable');
    await workerCDP.send('ServiceWorker.stopAllWorkers');
    await workerCDP.detach();
    await command(A.REOPEN_TAB);
    await waitFor(async () => (await session.state()).tabs.length === closedCount+1, 'restore after worker stop');
    const welcome = await command(A.NEW_TAB);
    page = await session.ensurePage();
    assert.match(welcome.url,/\/pages\/termium.html$/);
    await text('t');
    await waitFor(async () => (await session.state()).tabs.length===5,'Vimium t on offline welcome page');
    await command(A.NAVIGATE, {url:url+'/frames'});
    await waitFor(async () => !(await session.state()).loading, 'frame fixture load');
    page = await session.ensurePage();
    const child = page.frames().find(f => f !== page.mainFrame() && f.url().startsWith('http:'));
    assert.ok(child, 'cross-origin frame missing');
    await child.click('input');
    await text('fjgg');
    assert.equal(await child.$eval('input', e => e.value), 'fjgg');
    await key('Escape');
    await text('f');
    await child.waitForSelector('.vimiumHintMarker', {visible:true,timeout:3000});
    const frameHint = await child.$$eval('.vimiumHintMarker', es => es.at(-1).textContent.toLowerCase());
    await text(frameHint);
    await child.waitForFunction(() => document.title === 'frame clicked');
    await page.evaluate(() => document.querySelector('#shadow').shadowRoot.querySelector('input').focus());
    await text('fjgg');
    assert.equal(await page.evaluate(() => document.querySelector('#shadow').shadowRoot.querySelector('input').value), 'fjgg');
    await key('Escape');
    const started = Date.now();
    await command(A.NEW_TAB, {url:url+'/stalled'});
    assert.ok(Date.now()-started < 3000, 'new tab monopolized input while loading');
    await command(A.STOP);
    assert.equal((await session.state()).loading,false);
    // Reproduce a late key acknowledgement with stale Puppeteer isClosed.
    // The real x closes its real tab; only acknowledgement timing is injected.
    await command(A.NEW_TAB);
    const closingPage = await session.ensurePage();
    const closedId = (await session.state()).activeTabId;
    const originalType = closingPage.keyboard.type.bind(closingPage.keyboard);
    const originalClosed = closingPage.isClosed.bind(closingPage);
    closingPage.keyboard.type = async value => {
        await originalType(value);
        await waitFor(() => originalClosed(), 'fault-injection tab did not close');
        throw Error('Protocol error (Input.dispatchKeyEvent): Target closed');
    };
    closingPage.isClosed = () => false;
    await text('x');
    closingPage.isClosed = originalClosed;
    assert.ok(!(await session.state()).tabs.some(tab => tab.id === closedId));
    // Closing every tab leaves a usable welcome page instead of a dead session.
    for (const p of await browser.pages()) await p.close();
    const replacement = await session.state();
    assert.equal(replacement.tabs.length,1);
    assert.match(replacement.url,/\/pages\/termium.html$/);
    await text('t');
    await waitFor(async () => (await session.state()).tabs.length===2,'replacement welcome input');
});
