const { test } = require('node:test');
const assert = require('node:assert/strict');
const { spawnSync } = require('node:child_process');
const { EventEmitter } = require('node:events');
const { BrowserControls } = require('../dist/src/browser-controls');
const { InputEvent, InputKind } = require('../dist/generated/bc');

test('a rejected keyboard operation is surfaced once and does not poison the ordered input queue', async () => {
    const page = new EventEmitter();
    const cdp = { send: async () => {}, detach: async () => {} };
    const target = { createCDPSession: async () => cdp };
    page.target = () => target;
    let calls = 0;
    const failure = Error('Protocol error (Input.dispatchKeyEvent): Not attached to an active page');
    page.keyboard = { press: async () => { if (++calls === 1) throw failure; } };
    const controls = new BrowserControls(async () => page);
    await controls.attach(page);
    const key = InputEvent.fromPartial({ kind: InputKind.KEY_INPUT, key: 'PageDown' });
    const first = controls.input(key);
    const second = controls.input(key);
    await assert.rejects(first, error => error === failure);
    await second;
    assert.equal(calls, 2, 'failed input must not be silently replayed');
});

test('input tracing correlates concurrent operations and reports failures without text payloads', () => {
    const child = spawnSync(process.execPath, ['-e', `
        const {traceInput,inputTrace}=require('./server/dist/src/input-diagnostics');
        const event={kind:0,tabId:'tab',generation:7,text:'private typed contents',modifiers:0};
        Promise.allSettled([
            traceInput(event,async()=>{await new Promise(r=>setTimeout(r,10));inputTrace('cdp.result',{session:'one',ok:true});}),
            traceInput(event,async()=>{inputTrace('cdp.result',{session:'two',ok:false});throw Error('Session closed');})
        ]).then(results=>{if(results[1].status!=='rejected')process.exitCode=1;});
    `], { cwd: require('node:path').resolve(__dirname, '../..'), env: { ...process.env, TERMIUM_INPUT_TRACE: '1' }, encoding: 'utf8' });
    assert.equal(child.status, 0, child.stderr);
    assert.ok(!child.stdout.includes('private typed contents'));
    const lines = child.stdout.trim().split('\n').map(JSON.parse);
    for (const id of [1,2]) {
        const events = lines.filter(line => line.id === id);
        assert.deepEqual(events.map(line => line.stage), ['queued','cdp.result','result']);
        assert.equal(events[0].generation,7);
        assert.equal(events[1].session,id===1?'one':'two');
        assert.equal(events[2].ok,id===1);
    }
});
