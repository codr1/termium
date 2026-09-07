import path from 'node:path';
import { Browser, Frame, Page, Realm } from 'puppeteer';

export class Vimium {
    id = '';
    welcome = '';
    home = '';
    private installed = false;
    private ready = new WeakSet<Frame>();
    constructor(private readonly browser: Browser, private readonly homepage = 'about:termium') { }

    async install() {
        // Explicitly await installation: launch's extension-array path in our
        // pinned Puppeteer can return before the install promises settle.
        this.id = await this.browser.installExtension(path.resolve(__dirname, '../extensions/vimium'));
        this.welcome = `chrome-extension://${this.id}/pages/termium.html`;
        const builtin = this.homepage === 'about:termium';
        // Preserve the original default for installations that saved it before /welcome.
        const hosted = ['https://termium.dev', 'https://termium.dev/',
            'https://termium.dev/welcome', 'https://termium.dev/welcome/'].includes(this.homepage);
        if (!builtin && !hosted && this.homepage !== 'about:blank') {
            const url = new URL(this.homepage);
            if (!['http:', 'https:'].includes(url.protocol)) throw Error('Home page must use HTTP or HTTPS');
        }
        this.home = builtin || hosted ? this.welcome : this.homepage;
        await this.evaluate(async ({ home, website }: {home:string;website:string}) => {
            await (globalThis as any).chrome.storage.local.set({ termiumWebsite: website });
            const settings = (globalThis as any).Settings;
            await settings.onLoaded();
            for (const [key, value] of Object.entries({
                smoothScroll: false, hideUpdateNotifications: true,
                newTabDestination: 'customUrl', newTabCustomUrl: home,
                openVomnibarOnNewTabPage: false,
                userDefinedLinkHintCss: '.vimiumHintMarker { background: #ffe58a !important; border: 1px solid #473a12 !important; box-shadow: none !important; } .vimiumHintMarker span { color: #161616 !important; font-size: 14px !important; font-weight: bold !important; }',
            })) await settings.set(key, value);
        }, { home: this.home, website: hosted ? 'https://termium.dev/welcome/' : '' });
        this.installed = true;
    }

    async evaluate<T, A>(fn: (arg: A) => T | Promise<T>, arg: A): Promise<T> {
        // MV3 workers can disappear while idle. Reacquire a live target for
        // each call; never blindly replay a mutation after an uncertain result.
        const workerURL = `chrome-extension://${this.id}/background_scripts/main.js`;
        if (this.installed && !this.browser.targets().some(t => t.type() === 'service_worker' && t.url() === workerURL)) {
            const page = (await this.browser.pages())[0];
            if (page) {
                const cdp = await page.createCDPSession();
                try { await cdp.send('ServiceWorker.enable'); await cdp.send('ServiceWorker.startWorker', { scopeURL: `chrome-extension://${this.id}/` }); }
                finally { await cdp.detach(); }
            }
        }
        const target = await this.browser.waitForTarget(t => t.type() === 'service_worker' &&
            t.url() === `chrome-extension://${this.id}/background_scripts/main.js`, { timeout: 3000 });
        const cdp = await target.createCDPSession();
        // A fresh CDP session also works after MV3 restart; Puppeteer's worker
        // wrapper can miss Inspector.workerScriptLoaded when attaching late.
        const timer = setTimeout(() => { void cdp.detach().catch(() => {}); }, 5000);
        try {
            const deadline = Date.now() + 3000;
            for (;;) {
                const ready = await cdp.send('Runtime.evaluate', { expression: 'typeof globalThis.Settings !== "undefined"', returnByValue: true });
                if (ready.result.value) break;
                if (Date.now() >= deadline) throw Error('Vimium background initialization timed out');
                await new Promise(resolve => setTimeout(resolve, 10));
            }
            const result = await cdp.send('Runtime.evaluate', {
                expression: `(${fn.toString()})(${JSON.stringify(arg) ?? 'undefined'})`,
                awaitPromise: true, returnByValue: true,
            });
            if (result.exceptionDetails) throw Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text);
            return result.result.value as T;
        } finally { clearTimeout(timer); await cdp.detach().catch(() => {}); }
    }

    forget(frame: Frame) { this.ready.delete(frame); }

    async waitForPage(page: Page): Promise<string> {
        const url = page.url();
        if (!/^https?:/.test(url) && !url.startsWith(`chrome-extension://${this.id}/`)) {
            return 'Vimium unavailable on this browser page · Ctrl+L to navigate';
        }
        // Check the receiving frame, including a focused cross-origin frame.
        // This is readiness only. Vimium classifies every actual event itself.
        for (const frame of page.frames()) {
            if (this.ready.has(frame)) continue;
            // Vimium's HUD, find box and help are extension UI frames, not
            // ordinary content-script frames with a NormalMode instance.
            if (frame !== page.mainFrame() && frame.url().startsWith(`chrome-extension://${this.id}/`)) continue;
            let realm: Realm | Frame | undefined;
            if (frame.url().startsWith(`chrome-extension://${this.id}/`)) realm = frame;
            else {
                for (const candidate of frame.extensionRealms()) {
                    if ((await candidate.extension())?.id === this.id) { realm = candidate; break; }
                }
            }
            if (!realm) {
                if (frame !== page.mainFrame()) continue;
                // A commit precedes context creation. A bounded wait prevents
                // the first key from slipping past a not-yet-loaded extension.
                const started = Date.now();
                while (!realm && Date.now() - started < 2000) {
                    await new Promise(resolve => setTimeout(resolve, 10));
                    for (const candidate of frame.extensionRealms()) {
                        if ((await candidate.extension())?.id === this.id) { realm = candidate; break; }
                    }
                }
                if (!realm) throw Error('Vimium is not ready. Reload the page or use Ctrl+L.');
            }
            await realm.waitForFunction(() => {
                const root = globalThis as any;
                return root.frameId != null && root.handlerStack?.stack.some((h: any) => h._name.startsWith('mode-normal-'));
            }, { timeout: 2000 });
            // A storage response is an initialization fence after NormalMode
            // has requested its keymap. No page-world function is invoked.
            await realm.evaluate(async () => {
                const root = globalThis as any;
                await root.chrome.storage.session.get('normalModeKeyStateMapping');
            });
            this.ready.add(frame);
        }
        return 'Vimium';
    }
}
