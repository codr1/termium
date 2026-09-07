const { test } = require('node:test');
const assert = require('node:assert/strict');
const puppeteer = require('puppeteer');
const { BrowserControls } = require('../dist/src/browser-controls');

// Allow cold Chromium startup on native CI; the cancellation assertion below
// retains its own 1.5-second bound, independent of launch/teardown time.
test('navigation aborts an in-flight capture without disabling resize or future captures', { timeout: 60000 }, async t => {
    const browser = await puppeteer.launch({ headless: true, defaultViewport: null, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
    t.after(() => browser.close());
    const page = await browser.newPage();
    const controls = new BrowserControls(async () => page);
    await controls.attach(page);
    await controls.setViewport(320, 201);
    // Observe the real CDP send so navigation starts during capture, rather
    // than relying on an arbitrary delay between two asynchronous RPCs.
    const target = page.target();
    const originalCreate = target.createCDPSession.bind(target);
    let started;
    const captureStarted = new Promise(resolve => { started = resolve; });
    target.createCDPSession = async (...args) => {
        const session = await originalCreate(...args);
        const send = session.send.bind(session);
        session.send = (method, ...params) => {
            const result = send(method, ...params);
            if (method === 'Page.captureScreenshot') started();
            return result;
        };
        return session;
    };
    const pending = controls.capture('png').catch(error => error);
    await captureStarted;
    await page.goto('data:text/html,<body style="background:lime">navigation fixture</body>');
    let timer;
    try {
        const result = await Promise.race([pending, new Promise((_, reject) => { timer = setTimeout(() => reject(Error('capture remained pending after navigation')), 1500); })]);
        if (result instanceof Error) assert.equal(result.code, 9);
    } finally { clearTimeout(timer); }
    target.createCDPSession = originalCreate;
    await controls.setViewport(400, 217);
    const png = await controls.capture('png');
    assert.equal(png.readUInt32BE(16), 400);
    assert.equal(png.readUInt32BE(20), 217);
});
