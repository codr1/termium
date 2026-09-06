const { test } = require('node:test');
const assert = require('node:assert/strict');
const { EventEmitter } = require('node:events');
const { BrowserControls } = require('../dist/src/browser-controls');

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
        assert.equal(page.listenerCount('framenavigated'), baseline);
    });
}
