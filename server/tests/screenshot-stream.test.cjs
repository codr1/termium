const { test } = require('node:test');
const assert = require('node:assert/strict');
const { EventEmitter, once } = require('node:events');
const { setTimeout: delay } = require('node:timers/promises');
const { streamScreenshots } = require('../dist/src/screenshot-stream');

class Sink extends EventEmitter {
    constructor() { super(); this.on('error', error => { this.error = error; this.emit('close'); }); }
    request = { fps: 60, format: 'png' };
    cancelled = false;
    writes = 0;
    write() { this.writes++; this.emit('written'); return false; }
    destroy(error) { this.error = error; this.emit('close'); }
}

test('backpressure suspends capture until drain; cancellation stops production', { timeout: 2000 }, async t => {
    const sink = new Sink();
    t.after(() => sink.emit('cancelled'));
    let captures = 0;
    streamScreenshots(sink, async () => { captures++; return { data: Buffer.alloc(1), generation: 1 }; });
    await once(sink, 'written');
    await delay(70);
    assert.equal(captures, 1, 'capture continued while writer was blocked');
    sink.emit('drain');
    await once(sink, 'written');
    assert.equal(captures, 2);
    sink.emit('cancelled');
    sink.emit('drain');
    await delay(70);
    assert.equal(captures, 2, 'drain restarted cancelled stream');
});

test('a slow capture stays exclusive and its result is discarded after cancellation', { timeout: 2000 }, async t => {
    const sink = new Sink();
    t.after(() => sink.emit('cancelled'));
    let finish, cancelled, captures = 0;
    const started = new EventEmitter();
    streamScreenshots(sink, (_, isCancelled) => {
        captures++;
        cancelled = isCancelled;
        const result = new Promise(resolve => { finish = resolve; });
        started.emit('started');
        return result;
    });
    await once(started, 'started');
    await delay(70);
    assert.equal(captures, 1);
    sink.emit('cancelled');
    assert.equal(cancelled(), true);
    finish({ data: Buffer.alloc(1), generation: 1 });
    await delay(30);
    assert.equal(sink.writes, 0);
});

test('unsupported formats and excessive capture rates fail before capture', () => {
    for (const request of [{ fps: -1, format: '' }, { fps: 61, format: '' }, { fps: 24, format: 'raw' }]) {
        const sink = new Sink();
        sink.request = request;
        streamScreenshots(sink, () => { throw Error('must not capture'); });
        assert.equal(sink.error.code, 3);
    }
});

test('ordinary loading and document changes do not consume the fatal error budget', { timeout: 2000 }, async t => {
    const sink = new Sink();
    t.after(() => sink.emit('cancelled'));
    let captures = 0;
    streamScreenshots(sink, async () => {
        if (++captures <= 30) throw Object.assign(new Error('temporary navigation'), { code: captures % 2 ? 14 : 9 });
        return { data: Buffer.alloc(1), generation: 2 };
    });
    await once(sink, 'written');
    assert.equal(sink.error, undefined);
    assert.equal(captures, 31);
});
