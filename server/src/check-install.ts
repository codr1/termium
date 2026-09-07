import puppeteer from 'puppeteer';
import * as fs from 'node:fs/promises';
import * as os from 'node:os';
import * as path from 'node:path';
import { BrowserSession } from './browser-session';
import { InputEvent, InputKind, NavigationAction, NavigationRequest } from '../generated/bc';
import { BrowserControls } from './browser-controls';

// A disposable profile checks the actual packaged browser. No user browsing
// data, external website, or running Termium server participates in validation.
async function check() {
    const temporary = await fs.mkdtemp(path.join(os.tmpdir(), 'termium-check-'));
    const abort = new AbortController();
    let browser: Awaited<ReturnType<typeof puppeteer.launch>> | undefined;
    const timer = setTimeout(() => {
        abort.abort();
        browser?.process()?.kill('SIGKILL');
        console.error('Browser validation timed out');
        process.exitCode = 1;
    }, 30000);
    try {
        browser = await puppeteer.launch({ headless: true, pipe: true, enableExtensions: true, signal: abort.signal, dumpio: process.env.TERMIUM_CHECK_DEBUG === '1', args: ['--lang=en-US'],
            env: { ...process.env, XDG_DATA_HOME: path.join(temporary, 'data'), XDG_CONFIG_HOME: path.join(temporary, 'config'), XDG_CACHE_HOME: path.join(temporary, 'cache') } });
        let page = await browser.newPage();
        page.setDefaultTimeout(5000);
        if (process.platform === 'linux') {
            await page.goto('chrome://sandbox');
            const status = await page.evaluate(() => document.body.innerText);
            if (!/adequately sandboxed/i.test(status) || !/Seccomp-BPF sandbox\s+Yes/.test(status)) {
                throw new Error(`Browser sandbox validation failed:\n${status}`);
            }
            console.log('✓ Browser sandbox active');
            await page.close();
            page = await browser.newPage();
            page.setDefaultTimeout(5000);
        }
        const controls = new BrowserControls(async () => page);
        await controls.attach(page);
        await controls.setViewport(320, 201);
        await page.setContent('<body style="background:lime"><input><button onclick="document.title=document.querySelector(\'input\').value">Check</button>');
        await page.type('input', 'Termium ✓');
        await page.click('button');
        if (await page.title() !== 'Termium ✓') throw new Error('Browser input check failed');
        const screenshot = await controls.capture('png');
        if (!Buffer.from(screenshot).subarray(0, 8).equals(Buffer.from([137,80,78,71,13,10,26,10]))) throw new Error('Browser screenshot check failed');
        console.log('✓ Runtime, browser, input, and screenshots ready');
        const session = new BrowserSession(async () => browser!);
        await session.command(NavigationRequest.fromPartial({ action: NavigationAction.NEW_TAB }));
        const before = await session.state();
        await session.input(InputEvent.fromPartial({ kind: InputKind.TEXT_INPUT, text: 't', tabId: before.activeTabId, generation: before.generation }));
        const deadline = Date.now() + 5000;
        let after = await session.state();
        while (after.tabs.length !== before.tabs.length + 1 && Date.now() < deadline) {
            await new Promise(resolve => setTimeout(resolve, 20));
            after = await session.state();
        }
        if (after.tabs.length !== before.tabs.length + 1 || after.activeTabId === before.activeTabId) throw Error('Vimium tab navigation check failed');
        await session.capture('png');
        console.log('✓ Bundled Vimium and tab navigation ready');
    } finally {
        try { await browser?.close(); } finally { clearTimeout(timer); await fs.rm(temporary, { recursive: true, force: true }); }
    }
}
void check().catch(error => { console.error((error as Error).message); process.exitCode = 1; });
