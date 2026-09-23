const { test } = require('node:test');
const assert = require('node:assert/strict');
const { EventEmitter } = require('node:events');
const { BrowserControls } = require('../dist/src/browser-controls');

function makePage() {
    const page = new EventEmitter();
    const frame = {};
    page.mainFrame = () => frame;
    page.url = () => 'about:blank';
    return { page, frame };
}

for (const trigger of ['navigation', 'watchdog']) {
    test(`${trigger} releases a stuck capture and preserves the input session`, async t => {
        t.mock.timers.enable({ apis: ['setTimeout'] });
        const page = new EventEmitter();
        const frame = {};
        page.mainFrame = () => frame;
        page.url = () => 'about:blank';
        let sessionCount = 0, inputDetached = false, completeCapture = false;
        let started;
        const captureStarted = new Promise(resolve => { started = resolve; });
        const target = {
            async createCDPSession() {
                if (++sessionCount === 1) return {
                    async send(method) {
                        assert.equal(inputDetached, false, 'capture closed the input session');
                        if (method === 'Page.getNavigationHistory') return { entries: [], currentIndex: 0 };
                        assert.equal(method, 'Emulation.setDeviceMetricsOverride');
                    },
                    async detach() { inputDetached = true; },
                };
                let rejectCapture;
                return {
                    send(method) {
                        assert.equal(method, 'Page.captureScreenshot');
                        if (completeCapture) return Promise.resolve({ data: Buffer.from('next frame').toString('base64') });
                        const pending = new Promise((_, reject) => { rejectCapture = reject; });
                        started();
                        return pending;
                    },
                    async detach() { rejectCapture?.(new Error('Session closed')); },
                };
            },
        };
        page.target = () => target;
        const controls = new BrowserControls(async () => page);
        await controls.attach(page);
        const baseline = page.listenerCount('framenavigated');
        const pending = controls.capture('png').catch(error => error);
        await captureStarted;
        if (trigger === 'navigation') page.emit('framenavigated', frame);
        else t.mock.timers.tick(3000);
        const error = await pending;
        assert.ok(error instanceof Error);
        if (trigger === 'navigation') assert.equal(error.code, 9);
        assert.equal(page.listenerCount('framenavigated'), baseline, 'capture leaked a navigation listener');
        await controls.setViewport(400, 217);
        await controls.state();
        completeCapture = true;
        assert.equal((await controls.capture('png')).toString(), 'next frame');
        assert.equal(inputDetached, false);
        assert.equal(sessionCount, 3, 'the aborted capture session was not reused');
        assert.equal(page.listenerCount('framenavigated'), baseline);
    });
}

test('steady-state captures reuse one dedicated session without detaching', async t => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const { page } = makePage();
    let creations = 0;
    const sessions = [];
    const target = {
        createCDPSession() {
            creations++;
            const session = {
                detaches: 0,
                async send(method) {
                    if (method === 'Emulation.setDeviceMetricsOverride') return {};
                    assert.equal(method, 'Page.captureScreenshot');
                    return { data: Buffer.from('frame').toString('base64') };
                },
                async detach() { this.detaches++; },
            };
            sessions.push(session);
            return Promise.resolve(session);
        },
    };
    page.target = () => target;
    const controls = new BrowserControls(async () => page);
    await controls.attach(page);
    assert.equal(creations, 1, 'attach created the input session');
    const baseline = page.listenerCount('framenavigated');
    for (let i = 0; i < 3; i++) {
        assert.ok((await controls.capture('png')).length > 0);
    }
    assert.equal(creations, 2, 'steady-state captures reattached the CDP session');
    assert.equal(sessions[1].detaches, 0, 'a healthy capture session was detached between frames');
    assert.equal(page.listenerCount('framenavigated'), baseline, 'capture leaked a navigation listener');
    t.mock.timers.tick(10_000);
    assert.equal(sessions[1].detaches, 0, 'a successful capture left an armed watchdog');
});

test('target replacement disposes the old capture session and reuses a new one', async () => {
    const { page } = makePage();
    let creations = 0;
    function makeTarget() {
        return {
            createCDPSession() {
                creations++;
                const session = {
                    detaches: 0,
                    screenshots: 0,
                    async send(method) {
                        if (method === 'Emulation.setDeviceMetricsOverride') return {};
                        assert.equal(method, 'Page.captureScreenshot');
                        this.screenshots++;
                        return { data: Buffer.from('frame').toString('base64') };
                    },
                    async detach() { this.detaches++; },
                };
                sessions.push(session);
                return Promise.resolve(session);
            },
        };
    }
    const sessions = [];
    let target = makeTarget();
    page.target = () => target;
    const controls = new BrowserControls(async () => page);
    await controls.attach(page);
    assert.ok((await controls.capture('png')).length > 0);
    assert.equal(creations, 2);
    // Back/forward cache activation swaps Puppeteer's primary target.
    target = makeTarget();
    assert.ok((await controls.capture('png')).length > 0);
    assert.equal(sessions[1].detaches, 1, 'old-target capture session was not disposed');
    assert.equal(creations, 3, 'replacement target kept the old capture session');
    assert.equal(sessions[2].screenshots, 1);
    // The replacement is reused by the next capture.
    await controls.capture('png');
    assert.equal(creations, 3, 'steady-state captures reattached the CDP session');
    assert.equal(sessions[2].screenshots, 2);
});

test('a failed capture-session attach does not poison later captures', async () => {
    const { page } = makePage();
    let creations = 0;
    const target = {
        createCDPSession() {
            creations++;
            if (creations === 2) return Promise.reject(new Error('Target closed'));
            const session = {
                detaches: 0,
                async send(method) {
                    if (method === 'Emulation.setDeviceMetricsOverride') return {};
                    assert.equal(method, 'Page.captureScreenshot');
                    return { data: Buffer.from('frame').toString('base64') };
                },
                async detach() { this.detaches++; },
            };
            return Promise.resolve(session);
        },
    };
    page.target = () => target;
    const controls = new BrowserControls(async () => page);
    await controls.attach(page);
    assert.equal(creations, 1);
    const error = await controls.capture('png').catch(e => e);
    assert.ok(error instanceof Error, 'failed attach must reject the capture');
    assert.equal(creations, 2);
    assert.ok((await controls.capture('png')).length > 0, 'later capture did not recover from failed attach');
    assert.equal(creations, 3, 'recovery reused the rejected session promise');
});

test('a late rejection from an aborted capture cannot clear its replacement', async t => {
    // The server serializes captures, so this test overlaps two attempts on
    // purpose: it is the only way to land one attempt's rejection after a
    // replacement session exists, without arbitrary sleeps.
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const { page } = makePage();
    let creations = 0;
    const sessions = [];
    let started;
    const captureStarted = new Promise(resolve => { started = resolve; });
    let rejectFirstCapture;
    const target = {
        createCDPSession() {
            creations++;
            if (creations === 1) return Promise.resolve({
                async send(method) { assert.equal(method, 'Emulation.setDeviceMetricsOverride'); return {}; },
                async detach() {},
            });
            const session = {
                detaches: 0,
                async send(method) {
                    assert.equal(method, 'Page.captureScreenshot');
                    if (creations === 2) {
                        started();
                        return new Promise((_, reject) => { rejectFirstCapture = reject; });
                    }
                    return { data: Buffer.from('frame').toString('base64') };
                },
                async detach() { this.detaches++; },
            };
            sessions.push(session);
            return Promise.resolve(session);
        },
    };
    page.target = () => target;
    const controls = new BrowserControls(async () => page);
    await controls.attach(page);
    const pending = controls.capture('png').catch(error => error);
    await captureStarted;
    t.mock.timers.tick(3000);
    assert.equal(sessions[0].detaches, 1, 'watchdog did not detach the stalled session');
    // The replacement attaches and succeeds while the old rejection is held.
    const second = controls.capture('png').catch(error => error);
    assert.ok((await second).length > 0, 'replacement capture failed');
    assert.equal(creations, 3);
    // Now the old attempt's rejection lands after its replacement is live.
    rejectFirstCapture(new Error('Session closed'));
    const first = await pending;
    assert.ok(first instanceof Error);
    assert.equal(sessions[1].detaches, 0, 'late rejection detached the replacement session');
    // The replacement is still tracked: the next capture reuses it.
    await controls.capture('png');
    assert.equal(creations, 3, 'late rejection cleared the replacement tracking');
});

test('target replacement during attach disposes the obsolete capture session', async () => {
    const { page } = makePage();
    let creations = 0;
    function makeSession(failSend) {
        return {
            detaches: 0,
            async send(method) {
                if (method === 'Emulation.setDeviceMetricsOverride') return {};
                assert.equal(method, 'Page.captureScreenshot');
                if (failSend) throw new Error('Target closed');
                return { data: Buffer.from('frame').toString('base64') };
            },
            async detach() { this.detaches++; },
        };
    }
    let attachRequested;
    const attachRequest = new Promise(resolve => { attachRequested = resolve; });
    let resolveAttach;
    const targetA = {
        createCDPSession() {
            creations++;
            if (creations === 1) return Promise.resolve(makeSession(false)); // input session
            attachRequested();
            return new Promise(resolve => { resolveAttach = resolve; });      // held capture attach
        },
    };
    const targetB = { createCDPSession() { creations++; return Promise.resolve(makeSession(false)); } };
    page.target = () => targetA;
    const controls = new BrowserControls(async () => page);
    await controls.attach(page);
    const pending = controls.capture('png').catch(error => error);
    await attachRequest; // the first capture is awaiting its attach on A
    page.target = () => targetB; // BFCache-style swap while the attach is in flight
    const obsolete = makeSession(true);
    resolveAttach(obsolete);
    const error = await pending;
    assert.ok(error instanceof Error, 'capture through an obsolete session must fail');
    assert.equal(obsolete.detaches, 1, 'obsolete capture session was not disposed');
    // The next capture attaches to the replacement target and succeeds.
    assert.ok((await controls.capture('png')).length > 0);
    assert.equal(creations, 3);
    // And it is reused by the one after that.
    await controls.capture('png');
    assert.equal(creations, 3, 'steady-state captures reattached the CDP session');
});

test('a target closed between captures disposes the retained session on next use', async () => {
    const { page } = makePage();
    let creations = 0;
    const sessions = [];
    const target = {
        createCDPSession() {
            creations++;
            if (creations === 1) return Promise.resolve({
                async send(method) { assert.equal(method, 'Emulation.setDeviceMetricsOverride'); return {}; },
                async detach() {},
            });
            const session = {
                detaches: 0,
                dead: false,
                async send(method) {
                    if (this.dead) throw new Error('Target closed');
                    assert.equal(method, 'Page.captureScreenshot');
                    return { data: Buffer.from('frame').toString('base64') };
                },
                async detach() { this.detaches++; },
            };
            sessions.push(session);
            return Promise.resolve(session);
        },
    };
    page.target = () => target;
    const controls = new BrowserControls(async () => page);
    await controls.attach(page);
    assert.ok((await controls.capture('png')).length > 0);
    assert.equal(creations, 2);
    // The target dies between captures: the retained session's next send fails.
    sessions[1].dead = true;
    const error = await controls.capture('png').catch(e => e);
    assert.ok(error instanceof Error && /Target closed/.test(error.message), 'stale session must fail its capture');
    // The failed attempt disposed the retained session...
    assert.equal(sessions[1].detaches, 1, 'closed session was not detached on failure');
    // ...and the next capture attaches a fresh one that succeeds.
    assert.ok((await controls.capture('png')).length > 0);
    assert.equal(creations, 3, 'recovery did not attach a new session after closure');
});
