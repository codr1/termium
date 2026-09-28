const { test } = require('node:test');
const assert = require('node:assert/strict');
const http = require('node:http');
const puppeteer = require('puppeteer');
const { BrowserSession } = require('../dist/src/browser-session');
const { InputEvent, InputKind, NavigationAction, NavigationRequest } = require('../dist/generated/bc');
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

// No external website, network media, codecs on PATH, or Wayland input injector.
// Record a short local WebM in Chromium and loop it through a real video decoder.
const fixtureHTML = `<!doctype html><title>Video input regression</title>
<style>body{margin:0;height:200000px;background:linear-gradient(#246,#d86)}
video{position:fixed;right:0;top:0;width:80%;height:80%;pointer-events:none}</style>
<video autoplay muted loop playsinline></video><script>
window.fixtureReady = (async () => {
 const canvas = document.createElement('canvas'); canvas.width=640; canvas.height=360;
 const ctx=canvas.getContext('2d'); let n=0;
 const draw=setInterval(()=>{ctx.fillStyle=n++%2?'#d60':'#06d';ctx.fillRect(0,0,640,360);},33);
 const stream=canvas.captureStream(30), chunks=[];
 const recorder=new MediaRecorder(stream,{mimeType:'video/webm;codecs=vp8'});
 recorder.ondataavailable=e=>chunks.push(e.data);
 const stopped=new Promise(resolve=>recorder.onstop=resolve);
 recorder.start(); await new Promise(resolve=>setTimeout(resolve,1100)); recorder.stop(); await stopped;
 clearInterval(draw); stream.getTracks().forEach(t=>t.stop());
 const video=document.querySelector('video'); video.src=URL.createObjectURL(new Blob(chunks,{type:'video/webm'}));
 await video.play();
})();</script>`;

test('sustained keyboard input scrolls for 30 seconds while autoplay video and PNG capture continue', { timeout: 90000 }, async t => {
    const fixture = http.createServer((_req, res) => {
        res.setHeader('content-type', 'text/html'); res.end(fixtureHTML);
    });
    await new Promise(resolve => fixture.listen(0, '127.0.0.1', resolve));
    t.after(() => new Promise(resolve => fixture.close(resolve)));
    const browser = await puppeteer.launch({ headless: true, pipe: true, enableExtensions: true, defaultViewport: null });
    t.after(() => browser.close());
    const session = new BrowserSession(async () => browser, undefined, () => 'about:blank');
    const page = await session.ensurePage();
    // Observe before Vimium installs its handlers: it may stop propagation of
    // handled keys before ordinary page scripts can count them.
    await page.evaluateOnNewDocument(() => {
        window.receivedKeys = 0;
        window.recentKeys = [];
        addEventListener('keydown', event => {
            window.receivedKeys++;
            window.recentKeys.push({ key: event.key, repeat: event.repeat, y: scrollY });
            if (window.recentKeys.length > 8) window.recentKeys.shift();
        }, true);
    });
    const url = `http://127.0.0.1:${fixture.address().port}`;
    // Wait for a committed fixture before waiting for its media promise.
    await session.command(NavigationRequest.fromPartial({ action: NavigationAction.NAVIGATE, url }));
    await page.waitForFunction(() => !!window.fixtureReady, { timeout: 15000 });
    await page.evaluate(() => window.fixtureReady);
    await session.setViewport(1215, 560);
    const state = await session.state();
    const sendKey = key => session.input(InputEvent.fromPartial({
        kind: InputKind.KEY_INPUT, key, tabId: state.activeTabId, generation: state.generation,
    }));
    await sendKey('ArrowDown');
    await page.waitForFunction(() => scrollY > 0, { timeout: 3000 });
    // Native smooth scrolling need not move equal distances for opposite
    // keystrokes, especially when the second interrupts an animation. Test
    // direction from a known position, not an exact round trip to pixel zero.
    // An instant scroll also cancels the preceding animation before each check.
    await page.evaluate(() => scrollTo({ top: 500, behavior: 'instant' }));
    assert.equal(await page.evaluate(() => scrollY), 500);
    await sendKey('ArrowUp');
    await page.waitForFunction(() => scrollY < 500, { timeout: 3000 });
    // PageDown must work without borrowing residual motion from a preceding
    // ArrowDown. An explicit CDP command must still respect page cancellation.
    await page.evaluate(() => scrollTo({ top: 0, behavior: 'instant' }));
    assert.equal(await page.evaluate(() => scrollY), 0);
    await page.evaluate(() => addEventListener('keydown', event => event.preventDefault(), { once: true }));
    await sendKey('PageDown');
    await sleep(200);
    assert.equal(await page.evaluate(() => scrollY), 0, 'PageDown ignored preventDefault');
    await sendKey('PageDown');
    await page.waitForFunction(() => scrollY > 100, { timeout: 3000 });
    await page.evaluate(() => scrollTo({ top: 1000, behavior: 'instant' }));
    assert.equal(await page.evaluate(() => scrollY), 1000);
    await sendKey('PageUp');
    await page.waitForFunction(() => scrollY < 1000, { timeout: 3000 });
    await page.evaluate(() => {
        scrollTo({ top: 0, behavior: 'instant' });
        window.receivedKeys = 0; window.recentKeys = [];
    });
    const captureErrors = [];
    let running = true, captures = 0;
    const capturing = (async () => {
        while (running) {
            try { await session.capture('png'); captures++; }
            catch (error) { captureErrors.push(error.message); }
            await sleep(40);
        }
    })();
    let sent = 0, lastY = 0, previousCaptures = 0, previousVideoFrames = 0;
    const observe = () => page.evaluate(() => ({ y: scrollY, keys: receivedKeys,
        videoFrames: document.querySelector('video').getVideoPlaybackQuality().totalVideoFrames }));
    const checkProgress = (observed, event) => {
        assert.ok(observed.y > lastY,
            `scroll stalled: input=${JSON.stringify(event)} sent=${sent} previousY=${lastY} observed=${JSON.stringify(observed)}`);
        assert.ok(captures > previousCaptures, 'capture stopped during input');
        assert.ok(observed.videoFrames > previousVideoFrames, 'video stopped during input');
        lastY = observed.y;
        previousCaptures = captures; previousVideoFrames = observed.videoFrames;
    };
    try {
        // Test each mode for ten seconds. Vimium's RAF-driven instant scrolls
        // can cancel native smooth scrolls; a separate visible delta per
        // rapidly interleaved key is not a valid input-delivery requirement.
        for (const event of [
            { kind: InputKind.TEXT_INPUT, text: 'j' },
            { kind: InputKind.KEY_INPUT, key: 'ArrowDown' },
            { kind: InputKind.KEY_INPUT, key: 'PageDown' },
        ]) {
            const started = Date.now(), initialSent = sent;
            lastY = (await observe()).y;
            while (Date.now() - started < 10000) {
                await session.input(InputEvent.fromPartial({ ...event, tabId: state.activeTabId, generation: state.generation }));
                sent++;
                await sleep(100);
                const observed = await observe();
                assert.equal(observed.keys, sent, 'a key was lost or duplicated');
                if ((sent - initialSent) % 10 === 0) checkProgress(observed, event);
            }
            assert.ok(sent - initialSent >= 10, 'fewer than ten keys completed in an input phase');
            await sleep(300); // Finish the previous animation before switching modes.
            if ((sent - initialSent) % 10 !== 0) checkProgress(await observe(), event);
            t.diagnostic(`input=${JSON.stringify(event)} completed ten seconds; sent=${sent}; scrollY=${lastY}`);
        }
    } finally {
        running = false;
        await capturing;
    }
    assert.deepEqual(captureErrors, []);
    assert.ok(sent >= 30, `only ${sent} inputs completed in 30 seconds`);
    t.diagnostic(`${sent} keys delivered; ${captures} PNG captures; scrollY=${lastY}`);
});
