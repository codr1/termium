import { Browser, CDPSession, Page } from 'puppeteer';
import * as grpc from '@grpc/grpc-js';
import { BrowserState, InputEvent, InputKind, NavigationAction, NavigationRequest, Screenshot } from '../generated/bc';
import { BrowserControls } from './browser-controls';
import { Vimium } from './vimium';

type Tab = { id: string; page: Page; controls: BrowserControls; window: number };
type Snapshot = { active: Tab; tabs: Tab[]; generation: number; titles: Map<string, string> };
function stale(): never {
    throw Object.assign(Error('Tab or page changed; input was cancelled'), { code: grpc.status.FAILED_PRECONDITION });
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
    private selected = '';
    private documentGeneration = -1;
    private epoch = 0;
    private tail: Promise<unknown> = Promise.resolve();
    private vimiumStatus = 'Vimium';

    constructor(private readonly openBrowser: () => Promise<Browser>,
        private readonly onPage: (page: Page, id: string) => void = () => {}) { }

    private async init() {
        if (!this.initializing) this.initializing = (async () => {
            this.browser = await this.openBrowser();
            this.cdp = await this.browser.target().createCDPSession();
            this.vimium = new Vimium(this.browser);
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
        if (!pages.length) {
            const page = await this.browser.newPage();
            await page.goto(this.vimium.welcome);
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
        const active = this.records.get(candidates[0].targetId)!;
        if (this.selected !== active.id || this.documentGeneration !== active.controls.generation) {
            const old = this.records.get(this.selected);
            this.selected = active.id;
            this.documentGeneration = active.controls.generation;
            this.vimiumStatus = 'Vimium';
            this.epoch++;
            if (old && old !== active) await old.controls.resetInput().catch(() => {});
        }
        return { active, tabs: infos.map(info => this.records.get(info.targetId)!), generation: this.epoch,
            titles: new Map(infos.map(info => [info.targetId, info.title])) };
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
                case NavigationAction.NEW_TAB: {
                    if (request.url) {
                        let url: URL;
                        try { url = new URL(request.url); } catch { throw Object.assign(Error('Enter a valid HTTP or HTTPS address'), { code: grpc.status.INVALID_ARGUMENT }); }
                        if (!['http:', 'https:'].includes(url.protocol)) throw Object.assign(Error('Use an HTTP or HTTPS address'), { code: grpc.status.INVALID_ARGUMENT });
                    }
                    const p = await this.browser.newPage();
                    await p.bringToFront();
                    const created = await this.snapshot();
                    const ownTab = created.tabs.find(tab => tab.id === tabId(p));
                    if (!ownTab) throw Error('New tab disappeared before navigation');
                    if (request.url) await ownTab.controls.command({ ...request, action: NavigationAction.NAVIGATE, generation: 0 });
                    else await p.goto(this.vimium.welcome, { waitUntil: 'domcontentloaded', timeout: 5000 });
                    break;
                }
                case NavigationAction.SELECT_TAB: await targeted!.page.bringToFront(); break;
                case NavigationAction.CLOSE_TAB:
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

    input(event: InputEvent): Promise<BrowserState> {
        return this.enqueue(async () => {
            const s = await this.snapshot();
            if ((event.tabId && event.tabId !== s.active.id) || (event.generation && event.generation !== s.generation)) {
                const old = this.records.get(event.tabId);
                if (old) await old.controls.resetInput();
                stale();
            }
            if ([InputKind.KEY_INPUT, InputKind.TEXT_INPUT].includes(event.kind)) {
                try { this.vimiumStatus = await this.vimium.waitForPage(s.active.page); }
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
        });
    }

    private viewport = { width: 800, height: 600 };
    async setViewport(width: number, height: number) {
        const s = await this.snapshot();
        await s.active.controls.setViewport(width, height);
        this.viewport = { width, height };
    }
    async capture(format: 'png' | 'jpeg'): Promise<Screenshot> {
        const s = await this.snapshot();
        await s.active.controls.setViewport(this.viewport.width, this.viewport.height);
        let data: Buffer;
        try { data = await s.active.controls.capture(format); }
        catch (error) {
            if (s.active.page.isClosed() || /Target closed|Session closed/.test((error as Error).message)) stale();
            throw error;
        }
        const after = await this.snapshot();
        if (after.generation !== s.generation || after.active.id !== s.active.id) stale();
        const state = await this.state();
        if (state.generation !== s.generation) stale();
        return { data, generation: s.generation, tabId: s.active.id, state };
    }
}
