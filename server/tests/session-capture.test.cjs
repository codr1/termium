const { test } = require('node:test');
const assert = require('node:assert/strict');
const grpc = require('@grpc/grpc-js');
const { BrowserSession } = require('../dist/src/browser-session');

function tab(id) {
    return {
        id, window: 1,
        page: { isClosed: () => false },
        controls: {
            generation: 3,
            setViewport: async () => {},
            capture: async format => Buffer.from(`${id}:${format}`),
        },
    };
}

function sessionWith(active) {
    const session = new BrowserSession(async () => { throw Error('unexpected initialization'); });
    session.records.set(active.id, active);
    session.selected = active.id;
    session.documentGeneration = active.controls.generation;
    session.epoch = 8;
    session.snapshot = async () => { throw Error('unexpected tab discovery'); };
    session.state = async () => { throw Error('unexpected metadata read'); };
    return session;
}

test('warm capture uses retained selection without tab or history reads', async () => {
    const active = tab('selected');
    const session = sessionWith(active);
    const sizes = [];
    session.viewport = { width: 640, height: 360 };
    active.controls.setViewport = async (width, height) => sizes.push([width, height]);
    for (const format of ['png', 'jpeg', 'png']) {
        const frame = await session.capture(format);
        assert.equal(frame.data.toString(), `selected:${format}`);
        assert.equal(frame.tabId, active.id);
        assert.equal(frame.generation, 8);
        assert.equal(frame.state, undefined);
    }
    assert.deepEqual(sizes, [[640, 360], [640, 360], [640, 360]], 'viewport caching is a separate change');
});

test('cold, missing and closed selections delegate to discovery and keep its provenance', async t => {
    for (const reason of ['cold', 'missing', 'closed']) await t.test(reason, async () => {
        const old = tab('old');
        const session = sessionWith(old);
        if (reason === 'cold') session.selected = '';
        if (reason === 'missing') session.records.delete(old.id);
        if (reason === 'closed') old.page.isClosed = () => true;
        const recovered = tab('recovered');
        let discoveries = 0;
        session.snapshot = async () => {
            discoveries++;
            // A later state read may advance global state before this caller
            // resumes. Capture must use the discovery result's own identity.
            session.epoch = 12;
            session.selected = 'later';
            return { active: recovered, generation: 10 };
        };
        const frame = await session.capture('png');
        assert.equal(discoveries, 1);
        assert.equal(frame.tabId, 'recovered');
        assert.equal(frame.generation, 10);
        assert.equal(frame.data.toString(), 'recovered:png');
    });
});

test('a local document change advances capture epoch once and resets Vimium status', async () => {
    const active = tab('selected');
    const session = sessionWith(active);
    session.vimiumStatus = 'Previous document unavailable';
    active.controls.generation++;
    assert.equal((await session.capture('png')).generation, 9);
    assert.equal((await session.capture('png')).generation, 9);
    assert.equal(session.documentGeneration, active.controls.generation);
    assert.equal(session.vimiumStatus, 'Vimium');
});

test('selection changing during viewport work cannot relabel a transitional frame', async () => {
    const old = tab('old');
    const next = tab('next');
    const session = sessionWith(old);
    let release;
    const held = new Promise(resolve => { release = resolve; });
    old.controls.setViewport = () => held;
    const pending = session.capture('png');
    session.records.set(next.id, next);
    session.selected = next.id;
    session.epoch = 9;
    release();
    const frame = await pending;
    assert.equal(frame.tabId, 'old');
    assert.equal(frame.generation, 8);
    assert.equal(frame.data.toString(), 'old:png');
    assert.equal(frame.state, undefined);
    assert.equal((await session.capture('png')).tabId, 'next');
});

test('a failed retained session preserves stale-target recovery without replaying capture', async () => {
    const active = tab('selected');
    const session = sessionWith(active);
    let attempts = 0;
    active.controls.capture = async () => {
        attempts++;
        if (attempts === 1) throw Error('Session closed');
        return Buffer.from('recovered');
    };
    await assert.rejects(session.capture('png'), error =>
        error.code === grpc.status.FAILED_PRECONDITION &&
        error.metadata.get('termium-reason')[0] === 'stale-target');
    assert.equal(attempts, 1, 'never retry an obsolete screenshot within the request');
    assert.equal((await session.capture('png')).data.toString(), 'recovered');
});
