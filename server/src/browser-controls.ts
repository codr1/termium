import * as grpc from '@grpc/grpc-js';
import { inputTrace, inputTraceEnabled } from './input-diagnostics';
import { beginCapturePhase, endCapturePhase } from './capture-diagnostics';
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
    private captureCdp?: Promise<CDPSession>;
    private captureTarget?: Target;
    private captureLive?: CDPSession;
    private inputTail: Promise<unknown> = Promise.resolve();
    private held = 0;
    private x = 0; private y = 0;
    private loading = false;
    private error = '';
    private navigation = 0;
    private viewport = { width: 800, height: 600 };
    // Successful overrides belong to a control session, not a screenshot session.
    private appliedViewports = new WeakMap<CDPSession, { width: number; height: number }>();

    constructor(private readonly ensurePage: () => Promise<Page>) { }

    isLoading() { return this.loading; }
    async resetInput() { await this.releasePointer(); }

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
            const apply = target.createCDPSession().then(async session => {
                try {
                    await this.applyViewport(session);
                    return session;
                } catch (error) {
                    void session.detach().catch(() => { });
                    throw error;
                }
            });
            this.cdp = apply;
            void apply.catch(() => {
                // An obsolete initialization must not invalidate its replacement.
                if (this.cdp === apply) { this.target = undefined; this.cdp = undefined; }
            });
        }
        return this.cdp;
    }

    private async applyViewport(session: CDPSession) {
        const desired = this.viewport;
        const applied = this.appliedViewports.get(session);
        if (applied?.width === desired.width && applied?.height === desired.height) return;
        // Failure leaves the browser's size uncertain, including the old size.
        this.appliedViewports.delete(session);
        await session.send('Emulation.setDeviceMetricsOverride', {
            ...desired, deviceScaleFactor: 1, mobile: false,
        });
        this.appliedViewports.set(session, desired);
    }

    // Capture keeps a dedicated session, isolated from input/history. A
    // healthy session is reused by consecutive captures and disposed only on
    // target replacement or failure.
    private captureSession(): Promise<CDPSession> {
        const target = this.page!.target();
        // Back/forward cache activation swaps Puppeteer's primary target. A
        // session attached to the old target cannot control the restored page,
        // so scope the reusable session to the current target identity.
        if (this.captureTarget !== target || !this.captureCdp) {
            if (this.captureCdp) void this.captureCdp.then(session => session.detach()).catch(() => { });
            this.captureTarget = target;
            const created = target.createCDPSession();
            this.captureCdp = created;
            // One two-handler chain settles both ways on its own, so no derived
            // promise rejects unhandled while `created` still rejects for the
            // caller. Handlers compare promise references so a late settle from
            // an obsolete creation cannot claim or clear a replacement session.
            void created.then(
                session => { if (this.captureCdp === created) this.captureLive = session; },
                () => {
                    if (this.captureCdp === created) {
                        this.captureCdp = undefined;
                        this.captureTarget = undefined;
                        this.captureLive = undefined;
                    }
                },
            );
        }
        return this.captureCdp;
    }

    // Detaching rejects the session's pending CDP call, bounding a stalled
    // capture. Clear tracking only while this is still the live session so
    // late cleanup from an old attempt cannot drop a replacement.
    private abortCapture(session: CDPSession) {
        void session.detach().catch(() => { });
        if (this.captureLive === session) {
            this.captureCdp = undefined;
            this.captureTarget = undefined;
            this.captureLive = undefined;
        }
    }

    async setViewport(width: number, height: number) {
        await this.ensurePage();
        if (width < 1 || height < 1 || width > 16384 || height > 16384 || width * height > 16 * 1024 * 1024) fail(grpc.status.INVALID_ARGUMENT, 'Invalid viewport size');
        this.viewport = { width, height };
        await this.applyViewport(await this.session());
    }

    async capture(format: 'png' | 'jpeg'): Promise<Buffer> {
        try {
            return await this.captureOnce(format);
        } catch (error) {
            // A target can briefly lose its active compositor without a main
            // frame navigation. captureOnce has already discarded that session.
            // Retry this read once on a fresh session; persistent failures and
            // unrelated errors must still reach the caller.
            if (!(error instanceof Error) ||
                !error.message.includes('Protocol error (Page.captureScreenshot): Not attached to an active page')) throw error;
            return this.captureOnce(format);
        }
    }

    private async captureOnce(format: 'png' | 'jpeg'): Promise<Buffer> {
        const page = await this.ensurePage();
        // Render committed content even while images/scripts keep load pending.
        // Document changes are handled by aborting the capture session below.
        const generation = this.generation;
        // A navigation can strand a capture waiting for the old compositor, so
        // capture keeps its own session: detaching aborts its pending CDP call
        // without interrupting the input/history session or leaving a live RPC
        // behind a timer race. Successful captures keep the session; only an
        // aborted or failed attempt disposes it for the next frame.
        const session = await this.captureSession();
        const abort = () => this.abortCapture(session);
        const navigated = (frame: Frame) => { if (frame === page.mainFrame()) abort(); };
        page.on('framenavigated', navigated);
        page.on('close', abort);
        const timer = setTimeout(abort, 3000);
        try {
            if (generation !== this.generation) fail(grpc.status.FAILED_PRECONDITION, 'Page changed during capture');
            const cdpPhase = beginCapturePhase('cdp.screenshot');
            let result;
            try { result = await session.send('Page.captureScreenshot', {
                format, ...(format === 'jpeg' ? { quality: 60 } : {}),
                captureBeyondViewport: false, fromSurface: true, optimizeForSpeed: true,
            }); } finally { endCapturePhase(cdpPhase); }
            const base64Phase = beginCapturePhase('node.base64_decode');
            try { return Buffer.from(result.data, 'base64'); }
            finally { endCapturePhase(base64Phase); }
        } catch (error) {
            // A failed or aborted attempt must not be reused; the next capture
            // attaches a fresh session for the current target.
            this.abortCapture(session);
            if (generation !== this.generation) fail(grpc.status.FAILED_PRECONDITION, 'Page changed during capture');
            throw error;
        } finally {
            clearTimeout(timer);
            page.off('framenavigated', navigated);
            page.off('close', abort);
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
            tabs: [], activeTabId: '', vimiumStatus: '',
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
        // This is Puppeteer's keyboard session, separate from our capture and
        // pointer/history sessions. Read its ID only for diagnostics.
        const sessionId = inputTraceEnabled ? (page as Page & { _client?: () => CDPSession })._client?.().id() : undefined;
        inputTrace('dispatch.begin', { keyboardSession: sessionId, generation: this.generation, kind: event.kind });
        try {
            for (const key of modifiers) await page.keyboard.down(key);
            switch (event.kind) {
                case InputKind.TEXT_INPUT: await page.keyboard.type(event.text); break;
                case InputKind.PASTE_INPUT: await page.keyboard.sendCharacter(event.text); break;
                case InputKind.KEY_INPUT: {
                    // CDP bypasses AppKit's native key bindings on macOS. Supply
                    // the browser command with the key event, so page handlers
                    // can still cancel it via preventDefault (no JS scrolling).
                    let commands: string[] | undefined;
                    if (process.platform === 'darwin') {
                        if (event.modifiers === 4 && event.key.toLowerCase() === 'a') commands = ['selectAll'];
                        if (event.modifiers === 0 && event.key === 'PageDown') commands = ['scrollPageForward'];
                        if (event.modifiers === 0 && event.key === 'PageUp') commands = ['scrollPageBackward'];
                    }
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
                    if (inputTraceEnabled) inputTrace('pointer.session', { session: cdp.id() });
                    const base = { x: event.x, y: event.y, modifiers: event.modifiers };
                    // Chromium needs the active button as well as the held mask
                    // to treat this as a drag (including text selection).
                    const button = this.held & 1 ? 'left' : this.held & 2 ? 'right' : this.held & 4 ? 'middle' : 'none';
                    await cdp.send('Input.dispatchMouseEvent', { ...base, type: 'mouseMoved', buttons: this.held, button });
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
            inputTrace('dispatch.result', { keyboardSession: sessionId, ok: true });
        } catch (error) {
            inputTrace('dispatch.result', { keyboardSession: sessionId, ok: false, error: (error as Error).message });
            throw error;
        } finally {
            for (const key of modifiers.reverse()) await page.keyboard.up(key).catch(() => { });
        }
    }
}
