import * as grpc from '@grpc/grpc-js';
import { Page, CDPSession, KeyInput, Target, Frame } from 'puppeteer';
import { BrowserState, InputEvent, InputKind, NavigationAction, NavigationRequest } from '../generated/bc';

function fail(code: grpc.status, message: string): never {
    throw Object.assign(new Error(message), { code });
}

// One session, one ordered input pipeline. UI focus stays in the client; page
// keys are always delivered to Chromium, without guessing DOM focus from polls.
export class BrowserControls {
    generation = 0;
    private page?: Page;
    private cdp?: Promise<CDPSession>;
    private target?: Target;
    private inputTail: Promise<unknown> = Promise.resolve();
    private held = 0;
    private x = 0; private y = 0;
    private loading = false;
    private error = '';
    private navigation = 0;
    private viewport = { width: 800, height: 600 };

    constructor(private readonly ensurePage: () => Promise<Page>) { }

    async attach(page: Page) {
        this.page = page;
        this.target = undefined;
        this.generation++;
        page.on('framenavigated', frame => {
            if (frame === page.mainFrame()) this.generation++;
        });
        page.on('request', request => {
            if (request.isNavigationRequest() && request.frame() === page.mainFrame()) this.loading = true;
        });
        page.on('load', () => { this.loading = false; });
        page.on('requestfailed', request => { if (request.isNavigationRequest() && request.frame() === page.mainFrame()) this.loading = false; });
        await this.session();
    }

    private session(): Promise<CDPSession> {
        const target = this.page!.target();
        // Back/forward cache activation swaps Puppeteer's primary target. A
        // session attached to the old target cannot control the restored page.
        if (this.target !== target || !this.cdp) {
            if (this.cdp) void this.cdp.then(session => session.detach()).catch(() => { });
            this.target = target;
            this.cdp = target.createCDPSession();
            const apply = this.cdp.then(async session => { await this.applyViewport(session); return session; });
            this.cdp = apply;
            void this.cdp.catch(() => { this.target = undefined; });
        }
        return this.cdp;
    }

    private async applyViewport(session: CDPSession) {
        await session.send('Emulation.setDeviceMetricsOverride', {
            ...this.viewport, deviceScaleFactor: 1, mobile: false,
        });
    }

    async setViewport(width: number, height: number) {
        await this.ensurePage();
        if (width < 1 || height < 1 || width > 16384 || height > 16384 || width * height > 16 * 1024 * 1024) fail(grpc.status.INVALID_ARGUMENT, 'Invalid viewport size');
        this.viewport = { width, height };
        await this.applyViewport(await this.session());
    }

    async prepareCapture() {
        await this.ensurePage();
        await this.session(); // Reapply desktop dimensions after a target swap.
    }

    async capture(format: 'png' | 'jpeg'): Promise<Buffer> {
        const page = await this.ensurePage();
        // Render committed content even while images/scripts keep load pending.
        // Document changes are handled by aborting the capture session below.
        const generation = this.generation;
        // A navigation can strand a capture waiting for the old compositor.
        // Give capture its own session: detaching aborts its pending CDP call
        // without interrupting the input/history session or leaving a live RPC
        // behind a timer race. The viewport queue waits for that rejection.
        const session = await page.target().createCDPSession();
        const abort = () => { void session.detach().catch(() => {}); };
        const navigated = (frame: Frame) => { if (frame === page.mainFrame()) abort(); };
        page.on('framenavigated', navigated);
        const timer = setTimeout(abort, 3000);
        try {
            if (generation !== this.generation) fail(grpc.status.FAILED_PRECONDITION, 'Page changed during capture');
            const result = await session.send('Page.captureScreenshot', {
                format, ...(format === 'jpeg' ? { quality: 60 } : {}),
                captureBeyondViewport: false, fromSurface: true,
            });
            return Buffer.from(result.data, 'base64');
        } catch (error) {
            if (generation !== this.generation) fail(grpc.status.FAILED_PRECONDITION, 'Page changed during capture');
            throw error;
        } finally {
            clearTimeout(timer);
            page.off('framenavigated', navigated);
            await session.detach().catch(() => {});
        }
    }

    private async history() {
        for (let attempt = 0; ; attempt++) {
            try { return await (await this.session()).send('Page.getNavigationHistory'); }
            catch (error) {
                // Chromium target activation and Puppeteer's target update are
                // separate events. Retry only this read during that transition.
                if (attempt >= 9 || !/Not attached to an active page|Session closed|Target closed/.test((error as Error).message)) throw error;
                this.target = undefined;
                await new Promise(resolve => setTimeout(resolve, 20));
            }
        }
    }

    async state(): Promise<BrowserState> {
        const page = await this.ensurePage();
        const history = await this.history();
        return {
            url: page.url(), title: history.entries[history.currentIndex]?.title || '',
            canBack: history.currentIndex > 0,
            canForward: history.currentIndex < history.entries.length - 1,
            loading: this.loading, generation: this.generation, error: this.error,
        };
    }

    command(request: NavigationRequest): Promise<BrowserState> {
        const work = this.inputTail.then(() => this.navigate(request));
        this.inputTail = work.catch(() => { });
        return work;
    }

    private async navigate(request: NavigationRequest): Promise<BrowserState> {
        const page = await this.ensurePage();
        if (request.action === NavigationAction.STOP) {
            this.navigation++;
            await (await this.session()).send('Page.stopLoading');
            this.loading = false;
            this.error = '';
            return this.state();
        }
        if (this.loading) fail(grpc.status.FAILED_PRECONDITION, 'Stop the current navigation first');
        if (request.action === NavigationAction.NAVIGATE) {
            let url: URL;
            try { url = new URL(request.url); } catch { fail(grpc.status.INVALID_ARGUMENT, 'Enter a valid URL'); }
            if (!['http:', 'https:'].includes(url!.protocol) && request.url !== 'about:blank') {
                fail(grpc.status.INVALID_ARGUMENT, 'Use an HTTP or HTTPS URL');
            }
        }
        const state = await this.state();
        if ((request.action === NavigationAction.BACK && !state.canBack) ||
            (request.action === NavigationAction.FORWARD && !state.canForward)) return state;
        const options = { waitUntil: 'load' as const, timeout: 15000 };
        const actions: Partial<Record<NavigationAction, () => Promise<unknown>>> = {
            [NavigationAction.NAVIGATE]: () => page.goto(request.url, options),
            [NavigationAction.BACK]: () => page.goBack(options),
            [NavigationAction.FORWARD]: () => page.goForward(options),
            [NavigationAction.RELOAD]: () => page.reload(options),
        };
        const action = actions[request.action];
        if (!action) fail(grpc.status.INVALID_ARGUMENT, 'Unknown navigation action');
        const token = ++this.navigation;
        this.error = '';
        this.loading = true;
        // Start navigation without occupying the input worker until network idle.
        // Stop and browser dialogs must remain usable while the request is pending.
        void action().catch(error => {
            if (token === this.navigation) this.error = (error as Error).message;
        }).finally(() => {
            if (token === this.navigation) this.loading = false;
        });
        return this.state();
    }

    input(event: InputEvent): Promise<void> {
        const work = this.inputTail.then(() => this.dispatch(event));
        this.inputTail = work.catch(() => { });
        return work;
    }

    private async releasePointer() {
        const cdp = await this.session();
        for (const [mask, button] of [[1, 'left'], [2, 'right'], [4, 'middle']] as const) {
            if (this.held & mask) { this.held &= ~mask; await cdp.send('Input.dispatchMouseEvent', { type: 'mouseReleased', x: this.x, y: this.y, button, buttons: this.held, clickCount: 1 }); }
        }
    }

    private async dispatch(event: InputEvent) {
        const page = await this.ensurePage();
        if (event.text.length > 1024 * 1024 || event.modifiers > 15 || event.clickCount > 3) fail(grpc.status.INVALID_ARGUMENT, 'Invalid input event');
        if (event.kind === InputKind.RESET_INPUT) {
            await this.releasePointer();
            this.held = 0;
            return;
        }
        if (event.generation && event.generation !== this.generation) {
            // Release any drag captured by the previous document.
            await this.releasePointer();
            this.held = 0;
            fail(grpc.status.FAILED_PRECONDITION, 'Page changed; input was cancelled');
        }
        const modifiers: KeyInput[] = [];
        for (const [mask, key] of [[1, 'Alt'], [2, 'Control'], [4, 'Meta'], [8, 'Shift']] as const) {
            if (event.modifiers & mask) modifiers.push(key);
        }
        try {
            for (const key of modifiers) await page.keyboard.down(key);
            switch (event.kind) {
                case InputKind.TEXT_INPUT: await page.keyboard.type(event.text); break;
                case InputKind.PASTE_INPUT: await page.keyboard.sendCharacter(event.text); break;
                case InputKind.KEY_INPUT: {
                    // Headless Chromium on macOS needs the editing command in
                    // addition to the Command+A key event (no AppKit menu exists).
                    const commands = process.platform === 'darwin' && event.modifiers === 4 && event.key.toLowerCase() === 'a' ? ['selectAll'] : undefined;
                    await page.keyboard.press(event.key as KeyInput, { commands });
                    break;
                }
                case InputKind.POINTER_INPUT:
                case InputKind.WHEEL_INPUT: {
                    const viewport = this.viewport;
                    if (!viewport || event.x < 0 || event.y < 0 || event.x >= viewport.width || event.y >= viewport.height || event.buttons > 7) {
                        fail(grpc.status.INVALID_ARGUMENT, 'Pointer is outside the viewport');
                    }
                    const cdp = await this.session(); this.x = event.x; this.y = event.y;
                    const base = { x: event.x, y: event.y, modifiers: event.modifiers };
                    await cdp.send('Input.dispatchMouseEvent', { ...base, type: 'mouseMoved', buttons: this.held });
                    if (event.kind === InputKind.WHEEL_INPUT) {
                        await cdp.send('Input.dispatchMouseEvent', { ...base, type: 'mouseWheel', buttons: this.held, deltaX: event.deltaX, deltaY: event.deltaY });
                        break;
                    }
                    for (const [mask, button] of [[1, 'left'], [2, 'right'], [4, 'middle']] as const) {
                        if ((this.held & mask) && !(event.buttons & mask)) {
                            await cdp.send('Input.dispatchMouseEvent', { ...base, type: 'mouseReleased', button, buttons: this.held & ~mask, clickCount: Math.max(1, event.clickCount) });
                            this.held &= ~mask;
                        }
                    }
                    for (const [mask, button] of [[1, 'left'], [2, 'right'], [4, 'middle']] as const) {
                        if (!(this.held & mask) && (event.buttons & mask)) {
                            await cdp.send('Input.dispatchMouseEvent', { ...base, type: 'mousePressed', button, buttons: this.held | mask, clickCount: Math.max(1, event.clickCount) });
                            this.held |= mask;
                        }
                    }
                    break;
                }
                default: fail(grpc.status.INVALID_ARGUMENT, 'Unknown input kind');
            }
        } finally {
            for (const key of modifiers.reverse()) await page.keyboard.up(key).catch(() => { });
        }
    }
}
