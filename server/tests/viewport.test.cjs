const { test } = require('node:test');
const assert = require('node:assert/strict');
const { EventEmitter } = require('node:events');
const { BrowserControls } = require('../dist/src/browser-controls');

function makePage() {
    const page = new EventEmitter();
    const frame = {};
    page.mainFrame = () => frame;
    page.url = () => 'about:blank';
    return page;
}

// A control session that records successful viewport overrides. Setting
// failNext makes the next send reject, so tests can script a failed resize or
// initialization deterministically without timers or sleeps.
function makeControlSession() {
    const session = {
        detaches: 0,
        overrides: [],
        failNext: false,
        async send(method, params) {
            assert.equal(method, 'Emulation.setDeviceMetricsOverride');
            if (this.failNext) { this.failNext = false; throw new Error('Target closed'); }
            this.overrides.push(params);
            return {};
        },
        async detach() { this.detaches++; },
    };
    return session;
}

function override(width, height) {
    return { width, height, deviceScaleFactor: 1, mobile: false };
}

test('viewport overrides are cached per control session', async () => {
    const page = makePage();
    const session = makeControlSession();
    const target = { createCDPSession: () => Promise.resolve(session) };
    page.target = () => target;
    const controls = new BrowserControls(async () => page);

    await controls.attach(page);
    assert.deepEqual(session.overrides, [override(800, 600)], 'attach did not apply the initial viewport exactly once');

    for (let i = 0; i < 3; i++) await controls.setViewport(800, 600);
    assert.equal(session.overrides.length, 1, 'repeated equal setViewport added overrides');

    await controls.setViewport(400, 217);
    assert.deepEqual(session.overrides.at(-1), override(400, 217));
    await controls.setViewport(400, 217);
    assert.equal(session.overrides.length, 2, 'an applied size was resent');

    await controls.setViewport(800, 600);
    assert.deepEqual(session.overrides.at(-1), override(800, 600));
    assert.equal(session.overrides.length, 3, 'reverting to an earlier size must resend the override');
});

test('a replaced target receives one initial override at unchanged dimensions', async () => {
    const page = makePage();
    const oldSession = makeControlSession();
    const newSession = makeControlSession();
    const targetA = { createCDPSession: () => Promise.resolve(oldSession) };
    const targetB = { createCDPSession: () => Promise.resolve(newSession) };
    page.target = () => targetA;
    const controls = new BrowserControls(async () => page);

    await controls.attach(page);
    assert.equal(oldSession.overrides.length, 1);

    // Back/forward cache activation swaps the primary target at the same size.
    page.target = () => targetB;
    await controls.setViewport(800, 600);
    assert.equal(oldSession.detaches, 1, 'the old control session was not disposed');
    assert.deepEqual(newSession.overrides, [override(800, 600)], 'replacement did not receive exactly one initial override');

    await controls.setViewport(800, 600);
    assert.equal(newSession.overrides.length, 1, 'the replacement cached nothing after its first apply');
});

test('a failed resize is retryable and invalidates the previous cached size', async () => {
    const page = makePage();
    const session = makeControlSession();
    const target = { createCDPSession: () => Promise.resolve(session) };
    page.target = () => target;
    const controls = new BrowserControls(async () => page);

    await controls.attach(page);
    assert.equal(session.overrides.length, 1);

    // A failed resize is retryable with the same dimensions.
    session.failNext = true;
    const error = await controls.setViewport(400, 217).catch(e => e);
    assert.ok(error instanceof Error, 'a failed resize must reject');
    assert.equal(session.detaches, 0, 'a failed resize detached the control session');
    await controls.setViewport(400, 217);
    assert.deepEqual(session.overrides.at(-1), override(400, 217));
    await controls.setViewport(400, 217);
    assert.equal(session.overrides.length, 2, 'the retry did not restore the cache');

    // A failure at different dimensions invalidates even an applied size: the
    // same 400x217 must be sent again instead of matching the stale cache.
    session.failNext = true;
    const error2 = await controls.setViewport(640, 360).catch(e => e);
    assert.ok(error2 instanceof Error, 'a failed resize must reject');
    await controls.setViewport(400, 217);
    assert.deepEqual(session.overrides.at(-1), override(400, 217));
    assert.equal(session.overrides.length, 3, 'the applied size was not resent after a failure');
});

test('a failed initial override detaches its session and stays recoverable', async () => {
    const page = makePage();
    let creations = 0;
    const dead = makeControlSession();
    const healthy = makeControlSession();
    const target = { createCDPSession() { return Promise.resolve(++creations === 1 ? dead : healthy); } };
    page.target = () => target;
    const controls = new BrowserControls(async () => page);

    dead.failNext = true;
    const error = await controls.attach(page).catch(e => e);
    assert.ok(error instanceof Error, 'a failed initial override must reject attach');
    assert.equal(dead.detaches, 1, 'the failed initialization did not detach its session');

    // The next setViewport recreates the control session and succeeds.
    await controls.setViewport(800, 600);
    assert.equal(creations, 2, 'recovery reused the rejected initialization promise');
    assert.deepEqual(healthy.overrides, [override(800, 600)], 'the new session did not receive exactly one override');

    await controls.setViewport(800, 600);
    assert.equal(healthy.overrides.length, 1, 'the recovered session did not cache the size');
});

test('a rejected attach is not poisoned for later viewport changes', async () => {
    const page = makePage();
    let creations = 0;
    const healthy = makeControlSession();
    const target = { createCDPSession() { if (++creations === 1) return Promise.reject(new Error('Target closed')); return Promise.resolve(healthy); } };
    page.target = () => target;
    const controls = new BrowserControls(async () => page);

    const error = await controls.attach(page).catch(e => e);
    assert.ok(error instanceof Error && /Target closed/.test(error.message), 'attach must reject with the creation failure');

    await controls.setViewport(800, 600);
    assert.equal(creations, 2, 'recovery reused the rejected initialization promise');
    assert.deepEqual(healthy.overrides, [override(800, 600)]);
});

test('a late failure from an obsolete initialization cannot clear its replacement', async () => {
    // The server serializes viewport changes, so this test overlaps attach and
    // setViewport on purpose: it is the only way to land one initialization's
    // failure after a replacement session exists, without arbitrary sleeps.
    const page = makePage();
    let creations = 0;
    let resolveOldCreation;
    const oldCreation = new Promise(resolve => { resolveOldCreation = resolve; });
    const stale = makeControlSession();
    stale.failNext = true; // the old target is already gone by the time it settles
    const healthy = makeControlSession();
    const targetA = { createCDPSession() { creations++; return oldCreation.then(() => stale); } };
    const targetB = { createCDPSession() { creations++; return Promise.resolve(healthy); } };
    page.target = () => targetA;
    const controls = new BrowserControls(async () => page);

    // The first initialization is held pending on the old target.
    const attaching = controls.attach(page).catch(error => error);
    page.target = () => targetB; // BFCache-style swap while it is in flight
    await controls.setViewport(800, 600);
    assert.equal(creations, 2);
    assert.deepEqual(healthy.overrides, [override(800, 600)], 'the replacement did not initialize with one override');

    // Now the obsolete initialization settles as a failure after its replacement is live.
    resolveOldCreation();
    const error = await attaching;
    assert.ok(error instanceof Error, 'the obsolete initialization must reject the pending attach');
    assert.equal(stale.detaches, 1, 'the obsolete session was not disposed');

    // The late failure must not have cleared the replacement: same dimensions reuse it.
    await controls.setViewport(800, 600);
    assert.equal(creations, 2, 'the late failure forced a new control session');
    assert.equal(healthy.overrides.length, 1, 'the late failure invalidated the replacement cache');
});

test('capture-session failure and recreation do not invalidate the control viewport', async t => {
    t.mock.timers.enable({ apis: ['setTimeout'] });
    const page = makePage();
    let creations = 0;
    const control = makeControlSession();
    function captureSession(dead) {
        return {
            detaches: 0,
            async send(method) {
                assert.equal(method, 'Page.captureScreenshot');
                if (dead) throw new Error('Session closed');
                return { data: Buffer.from('frame').toString('base64') };
            },
            async detach() { this.detaches++; },
        };
    }
    const deadCapture = captureSession(true);
    const liveCapture = captureSession(false);
    const target = { createCDPSession() { creations++; if (creations === 1) return Promise.resolve(control); if (creations === 2) return Promise.resolve(deadCapture); return Promise.resolve(liveCapture); } };
    page.target = () => target;
    const controls = new BrowserControls(async () => page);

    await controls.attach(page);
    assert.equal(control.overrides.length, 1);
    const baseline = page.listenerCount('framenavigated');

    const error = await controls.capture('png').catch(e => e);
    assert.ok(error instanceof Error, 'the first capture must fail');
    assert.equal(deadCapture.detaches, 1, 'the failed capture session was not disposed');

    assert.ok((await controls.capture('png')).length > 0, 'a fresh capture did not recover');
    assert.equal(creations, 3);

    // The healthy control viewport survives the capture churn.
    await controls.setViewport(800, 600);
    assert.equal(control.overrides.length, 1, 'capture failure invalidated the cached control viewport');
    assert.equal(liveCapture.detaches, 0, 'a healthy capture session was detached');
    assert.equal(page.listenerCount('framenavigated'), baseline, 'capture leaked a navigation listener');
});
