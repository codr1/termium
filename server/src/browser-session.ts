import { Browser, CDPSession, Page } from 'puppeteer';
import * as grpc from '@grpc/grpc-js';
import { BrowserState, InputEvent, InputKind, NavigationAction, NavigationRequest, Screenshot } from '../generated/bc';
import { BrowserControls } from './browser-controls';
import { Vimium } from './vimium';
import { inputTrace, traceInput } from './input-diagnostics';
import { beginCapturePhase, endCapturePhase } from './capture-diagnostics';

type Tab = { id: string; page: Page; controls: BrowserControls; window: number };
type Snapshot = { active: Tab; tabs: Tab[]; generation: number; titles: Map<string, string> };
function stale(): never {
    const metadata = new grpc.Metadata();
    metadata.set('termium-reason', 'stale-target');
    throw Object.assign(Error('Tab or page changed; input was cancelled'), { code: grpc.status.FAILED_PRECONDITION, metadata });
}

// The Chromium tab target is stable across page-target swaps (including BFCache).
// Puppeteer 25.10 exposes it internally as _tabId. Keep that dependency here,
// guard it against actual CDP metadata, and exercise it in native integration CI.
function tabId(page: Page): string {
    const id = (page as unknown as { _tabId?: string })._tabId;
    if (!id) throw Error('Unsupported Puppeteer tab identity; use the packaged runtime');
    return id;
}

export class BrowserSession {
    private browser!: Browser;
    private cdp!: CDPSession;
    private vimium!: Vimium;
    private initializing?: Promise<void>;
    private refreshing?: Promise<Snapshot>;
    private records = new Map<string, Tab>();
    private inspector?: Tab & { owner: string; targetId: string };
    private inspectorSelected = false;
    private selected = '';
    private documentGeneration = -1;
    private epoch = 0;
    private tail: Promise<unknown> = Promise.resolve();
    private vimiumStatus = 'Vimium';

    constructor(private readonly openBrowser: () => Promise<Browser>,
        private readonly onPage: (page: Page, id: string) => void = () => {},
        private readonly homepage: () => string = () => 'about:termium') { }

    private async init() {
        if (!this.initializing) this.initializing = (async () => {
            this.browser = await this.openBrowser();
            this.cdp = await this.browser.target().createCDPSession();
            this.vimium = new Vimium(this.browser, this.homepage());
            await this.vimium.install();
        })();
        await this.initializing;
    }

    private async snapshot(): Promise<Snapshot> {
        await this.init();
        // A caller arriving after a tab mutation must not reuse a read that
        // began before that mutation. Share only the next fresh snapshot.
        if (this.refreshing) await this.refreshing;
        if (!this.refreshing) this.refreshing = this.refresh().finally(() => { this.refreshing = undefined; });
        return this.refreshing;
    }

    private async refresh(): Promise<Snapshot> {
        let pages = await this.browser.pages();
        const replacement = !pages.length;
        if (replacement) {
            const page = await this.browser.newPage();
            pages = [page];
        }
        const { targetInfos } = await this.cdp.send('Target.getTargets', { filter: [{ type: 'tab', exclude: false }] });
        const byId = new Map(pages.filter(p => !p.isClosed()).map(p => [tabId(p), p]));
        const infos = targetInfos.filter(info => byId.has(info.targetId));
        if (!infos.length) throw Error('Browser tab list is changing; retry');
        for (const info of infos) {
            if (typeof info.embedderData?.tabActive !== 'boolean' || !Number.isInteger(info.embedderData?.tabStripIndex)) {
                throw Error('This browser lacks tab metadata; use Termium’s packaged Chromium');
            }
            if (!this.records.has(info.targetId)) {
                const page = byId.get(info.targetId)!;
                const controls = new BrowserControls(async () => page);
                const { windowId } = await this.cdp.send('Browser.getWindowForTarget', { targetId: info.targetId });
                const tab = { id: info.targetId, page, controls, window: windowId };
                this.onPage(page, tab.id);
                await controls.attach(page);
                this.records.set(tab.id, tab);
                page.on('framenavigated', frame => this.vimium.forget(frame));
                page.on('close', () => {
                    this.records.delete(tab.id);
                    if (this.selected === tab.id) { this.selected = ''; this.epoch++; }
                });
            }
        }
        infos.sort((a, b) => this.records.get(a.targetId)!.window - this.records.get(b.targetId)!.window ||
            a.embedderData.tabStripIndex - b.embedderData.tabStripIndex);
        let candidates = infos.filter(info => info.embedderData.tabActive);
        if (candidates.length > 1) {
            const window = await this.vimium.evaluate(async () => (await (globalThis as any).chrome.windows.getLastFocused()).id, undefined);
            candidates = candidates.filter(info => this.records.get(info.targetId)!.window === window);
        }
        if (candidates.length !== 1) throw Error('Browser is switching tabs; retry');
        let active = this.records.get(candidates[0].targetId)!;
        if (this.inspector && (this.inspector.page.isClosed() || !byId.has(this.inspector.owner))) {
            const old = this.inspector;
            this.inspector = undefined;
            this.inspectorSelected = false;
            this.records.delete(old.id);
            if (!old.page.isClosed()) await old.page.close().catch(() => {});
        }
        if (this.inspectorSelected && this.inspector) active = this.inspector;
        if (replacement) await this.openHome(active);
        if (this.selected !== active.id || this.documentGeneration !== active.controls.generation) {
            const old = this.records.get(this.selected);
            this.selected = active.id;
            this.documentGeneration = active.controls.generation;
            this.vimiumStatus = 'Vimium';
            this.epoch++;
            if (old && old !== active) await old.controls.resetInput().catch(() => {});
        }
        const tabs = infos.map(info => this.records.get(info.targetId)!);
        const titles = new Map(infos.map(info => [info.targetId, info.title]));
        if (this.inspector) { tabs.push(this.inspector); titles.set(this.inspector.id, 'Developer tools'); }
        return { active, tabs, generation: this.epoch, titles };
    }

    async ensurePage() { return (await this.snapshot()).active.page; }
    async state(): Promise<BrowserState> {
        for (let attempt = 0; ; attempt++) {
            try { return await this.readState(); }
            catch (error) {
                // Tab closure/activation can precede Puppeteer's target update.
                // Retry only the read, never the input or command that caused it.
                if (attempt >= 20 || !/Target closed|Session closed|No target with given id|Not attached to an active page|tab list is changing|switching tabs/.test((error as Error).message)) throw error;
                await new Promise(resolve => setTimeout(resolve, 10));
            }
        }
    }
    private async readState(): Promise<BrowserState> {
        const s = await this.snapshot();
        const state = await s.active.controls.state();
        const after = await this.snapshot();
        if (after.generation !== s.generation || after.active.id !== s.active.id) throw Error('Browser is switching tabs; retry');
        return { ...state, generation: s.generation, activeTabId: s.active.id, vimiumStatus: this.vimiumStatus,
            tabs: s.tabs.map(tab => ({ id: tab.id, title: s.titles.get(tab.id) || 'New tab', url: tab.page.url(),
                active: tab.id === s.active.id, loading: tab.controls.isLoading() })) };
    }

    private enqueue<T>(action: () => Promise<T>): Promise<T> {
        const work = this.tail.then(action);
        this.tail = work.catch(() => {});
        return work;
    }

    command(request: NavigationRequest): Promise<BrowserState> {
        return this.enqueue(async () => {
            const s = await this.snapshot();
            const targeted = request.tabId ? s.tabs.find(t => t.id === request.tabId) : s.active;
            if (!targeted) stale();
            if (request.generation && request.generation !== s.generation) stale();
            switch (request.action) {
                case NavigationAction.DEVTOOLS:
                    if (targeted !== s.active) stale();
                    await this.openInspector(targeted);
                    break;
                case NavigationAction.EXTENSIONS: {
                    this.inspectorSelected = false;
                    const page = await this.browser.newPage();
                    await page.goto('chrome://extensions/', { waitUntil: 'domcontentloaded' });
                    await page.bringToFront();
                    break;
                }
                case NavigationAction.HOME:
                    if (targeted !== s.active) stale();
                    await this.openHome(s.active);
                    break;
                case NavigationAction.NEW_TAB: {
                    if (request.url) {
                        let url: URL;
                        try { url = new URL(request.url); } catch { throw Object.assign(Error('Enter a valid HTTP or HTTPS address'), { code: grpc.status.INVALID_ARGUMENT }); }
                        if (!['http:', 'https:'].includes(url.protocol)) throw Object.assign(Error('Use an HTTP or HTTPS address'), { code: grpc.status.INVALID_ARGUMENT });
                    }
                    this.inspectorSelected = false;
                    const p = await this.browser.newPage();
                    await p.bringToFront();
                    const created = await this.snapshot();
                    const ownTab = created.tabs.find(tab => tab.id === tabId(p));
                    if (!ownTab) throw Error('New tab disappeared before navigation');
                    if (request.url) await ownTab.controls.command({ ...request, action: NavigationAction.NAVIGATE, generation: 0 });
                    else await this.openHome(ownTab);
                    break;
                }
                case NavigationAction.SELECT_TAB:
                    this.inspectorSelected = targeted === this.inspector;
                    if (!this.inspectorSelected) await targeted.page.bringToFront();
                    break;
                case NavigationAction.CLOSE_TAB:
                    if (targeted === this.inspector) {
                        await this.inspector.page.close();
                        this.records.delete(targeted.id);
                        this.inspector = undefined;
                        this.inspectorSelected = false;
                        break;
                    }
                    // Page.close with runBeforeUnload keeps cancellation usable.
                    await targeted!.page.close({ runBeforeUnload: true });
                    break;
                case NavigationAction.REOPEN_TAB:
                    await this.vimium.evaluate(async () => {
                        const chrome = (globalThis as any).chrome;
                        const recent = await chrome.sessions.getRecentlyClosed({ maxResults: 1 });
                        if (recent.length) await chrome.sessions.restore(recent[0].tab?.sessionId ?? recent[0].window?.sessionId);
                    }, undefined);
                    break;
                default:
                    if (targeted !== s.active) stale();
                    await s.active.controls.command({ ...request, generation: 0 });
            }
            return this.state();
        });
    }

    private async openInspector(owner: Tab) {
        if (owner === this.inspector) return;
        if (this.inspector && this.inspector.owner !== owner.id) {
            await this.inspector.page.close();
            this.records.delete(this.inspector.id);
            this.inspector = undefined;
        }
        if (!this.inspector || this.inspector.page.isClosed()) {
            const { targetId } = await this.cdp.send('Target.openDevTools', { targetId: owner.id, panelId: 'elements' });
            // Puppeteer categorizes the built-in DevTools frontend as "other";
            // it has no normal tab-strip metadata. Keep it as an explicit tab.
            const target = await this.browser.waitForTarget(t => (t as any)._targetId === targetId, { timeout: 3000 });
            const page = await target.page();
            if (!page) throw Error('Chromium did not expose its Developer Tools page');
            const controls = new BrowserControls(async () => page);
            await controls.attach(page);
            this.inspector = { id: `devtools:${targetId}`, targetId, owner: owner.id, page, controls, window: owner.window };
            this.records.set(this.inspector.id, this.inspector);
        }
        this.inspectorSelected = true;
    }

    getSelection(request: NavigationRequest) {
        return this.enqueue(async () => {
            const s = await this.snapshot();
            if ((request.tabId && request.tabId !== s.active.id) || (request.generation && request.generation !== s.generation)) stale();
            let text = '';
            let frame = s.active.page.mainFrame();
            for (let depth = 0; depth < 32; depth++) {
                // Follow the focused element chain rather than document.hasFocus:
                // a headless page can have a valid selection without OS focus.
                const handle = await frame.evaluateHandle(() => {
                    let element = document.activeElement;
                    while (element?.shadowRoot?.activeElement) element = element.shadowRoot.activeElement;
                    return element;
                });
                try {
                    const element = handle.asElement();
                    const child = element ? await element.contentFrame() : null;
                    if (child) { frame = child; continue; }
                    text = await frame.evaluate(element => {
                        if (element instanceof HTMLInputElement || element instanceof HTMLTextAreaElement) {
                            if (element instanceof HTMLInputElement && element.type === 'password') return '';
                            return element.value.substring(element.selectionStart ?? 0, element.selectionEnd ?? 0);
                        }
                        return window.getSelection()?.toString() ?? '';
                    }, handle);
                    break;
                } finally { await handle.dispose(); }
            }
            if (Buffer.byteLength(text, 'utf8') > 64 * 1024) throw Error('Selection exceeds the 64 KiB clipboard limit');
            const after = await this.snapshot();
            if (after.generation !== s.generation || after.active.id !== s.active.id) stale();
            return { text, state: await this.state() };
        });
    }

    private async openHome(tab: Tab) {
        await tab.controls.resetInput();
        if (this.vimium.home === this.vimium.welcome) {
            await tab.page.goto(this.vimium.home, { waitUntil: 'domcontentloaded', timeout: 5000 });
        } else {
            await tab.controls.command({ action: NavigationAction.NAVIGATE, url: this.vimium.home, tabId: tab.id, generation: 0 });
        }
    }

    input(event: InputEvent): Promise<BrowserState> {
        return traceInput(event, () => this.enqueue(async () => {
            inputTrace('dequeued');
            const s = await this.snapshot();
            inputTrace('selection', { tab: s.active.id, generation: s.generation });
            if ((event.tabId && event.tabId !== s.active.id) || (event.generation && event.generation !== s.generation)) {
                const old = this.records.get(event.tabId);
                if (old) await old.controls.resetInput();
                stale();
            }
            if ([InputKind.KEY_INPUT, InputKind.TEXT_INPUT].includes(event.kind)) {
                inputTrace('vimium.begin');
                try { this.vimiumStatus = await this.vimium.waitForPage(s.active.page); inputTrace('vimium.ready'); }
                catch (error) { this.vimiumStatus = (error as Error).message; throw error; }
            }
            try { await s.active.controls.input({ ...event, generation: 0 }); }
            catch (error) {
                // x may close its own target before the key-up acknowledges.
                // Never retry that input on the replacement tab.
                if (!s.active.page.isClosed()) {
                    if (!/Target closed|Session closed/.test((error as Error).message)) throw error;
                    // CDP can report closure before Puppeteer updates isClosed.
                    // Confirm the tab disappeared using a fresh read; never
                    // replay the event or mask an error on a surviving tab.
                    const after = await this.state();
                    if (after.tabs.some(tab => tab.id === s.active.id)) throw error;
                    return after;
                }
            }
            return this.state();
        }));
    }

    private viewport = { width: 800, height: 600 };
    async setViewport(width: number, height: number) {
        const s = await this.snapshot();
        await s.active.controls.setViewport(width, height);
        this.viewport = { width, height };
    }

    private captureSelection(): Pick<Snapshot, 'active' | 'generation'> | undefined {
        const active = this.records.get(this.selected);
        if (!active || active.page.isClosed()) return undefined;
        // Navigation can precede the next state poll. Observe its local epoch
        // without querying Chromium, just as refresh() does for this document.
        if (this.documentGeneration !== active.controls.generation) {
            this.documentGeneration = active.controls.generation;
            this.vimiumStatus = 'Vimium';
            this.epoch++;
        }
        return { active, generation: this.epoch };
    }

    async capture(format: 'png' | 'jpeg'): Promise<Screenshot> {
        // The client independently polls state, including while images are
        // unchanged or capture is paused. Only cold/closed selection needs a
        // discovery read here. Freeze provenance before any capture awaits.
        const selection = beginCapturePhase('server.selection');
        const s = this.captureSelection() ?? await this.snapshot();
        endCapturePhase(selection);
        const viewport = beginCapturePhase('server.viewport');
        try { await s.active.controls.setViewport(this.viewport.width, this.viewport.height); }
        finally { endCapturePhase(viewport); }
        let data: Buffer;
        try { data = await s.active.controls.capture(format); }
        catch (error) {
            if (s.active.page.isClosed() || /Target closed|Session closed/.test((error as Error).message)) stale();
            throw error;
        }
        // A transitional frame is acceptable; never stamp it with a later
        // selection's epoch or replay toolbar metadata through screenshots.
        return { data, generation: s.generation, tabId: s.active.id };
    }
}
