// Investigation-only tracing. Never enabled by the normal launcher.
import { AsyncLocalStorage } from 'async_hooks';
import * as fs from 'fs/promises';
import * as inspector from 'inspector';
import { performance } from 'perf_hooks';
import { Browser } from 'puppeteer';

const report = process.env.TERMIUM_PERF_REPORT;
const enabled = process.env.TERMIUM_CAPTURE_TRACE === '1' && !!report;
const scope = new AsyncLocalStorage<number>();
const spans: { name: string; id: number; start_us: number; duration_us: number }[] = [];
let sequence = 0;
let dropped = 0;
const now = () => (performance.timeOrigin + performance.now()) * 1000;

export function beginCapturePhase(name: string) {
    return enabled ? { name, id: scope.getStore() ?? 0, start_us: now() } : undefined;
}
export function endCapturePhase(start: ReturnType<typeof beginCapturePhase>) {
    if (!start) return;
    if (spans.length >= 100000) { dropped++; return; }
    spans.push({ ...start, duration_us: now() - start.start_us });
}
export function captureScope<T>(action: () => Promise<T>): Promise<T> {
    return enabled ? scope.run(++sequence, action) : action();
}

const delay = (ms: number) => new Promise<void>(resolve => setTimeout(resolve, Math.max(0, ms)));
async function json(suffix: string, data: unknown) {
    const file = report + suffix;
    await fs.writeFile(file + '.tmp', JSON.stringify(data));
    await fs.rename(file + '.tmp', file);
}
function post(session: inspector.Session, method: string): Promise<any> {
    return new Promise((resolve, reject) => session.post(method, {}, (error: Error | null, value: unknown) => error ? reject(error) : resolve(value)));
}

export function startCaptureDiagnostics(browser: Browser) {
    if (!enabled) return;
    void collect(browser).catch(async error => {
        await json('.server.done', { error: String(error) }).catch(() => {});
    });
}

async function collect(browser: Browser) {
    let control: { start: string; duration_ns: number };
    for (;;) {
        try { control = JSON.parse(await fs.readFile(report + '.start', 'utf8')); break; }
        catch (error) {
            if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
            if (!browser.connected) throw Error('Browser closed before trace window');
            await delay(25);
        }
    }
    const start = Date.parse(control.start);
    const duration = control.duration_ns / 1e6;
    if (!Number.isFinite(start) || !(duration >= 1000 && duration <= 600000)) throw Error('Invalid trace window');
    const cdp = await browser.target().createCDPSession();
    const v8 = new inspector.Session();
    v8.connect();
    let tracing = false;
    let profiling = false;
    try {
        const environment = {
            version: await cdp.send('Browser.getVersion'),
            system: await cdp.send('SystemInfo.getInfo'),
            commandLine: await cdp.send('Browser.getBrowserCommandLine'),
        };
        // No screenshot trace category: it embeds extra screenshots in the trace.
        const categories = ['devtools', 'renderer', 'renderer_host', 'cc', 'viz', 'gpu',
            'toplevel', 'toplevel.flow'];
        await cdp.send('Tracing.start', { transferMode: 'ReturnAsStream', traceConfig: {
            recordMode: 'recordUntilFull', traceBufferSizeInKb: 131072, includedCategories: categories,
        } });
        tracing = true;
        const clock: { id: string; before_us: number; after_us: number }[] = [];
        const sync = async (id: string) => {
            const before_us = now();
            await cdp.send('Tracing.recordClockSyncMarker', { syncId: id });
            clock.push({ id, before_us, after_us: now() });
        };
        await sync('termium-start');
        await post(v8, 'Profiler.enable');
        await delay(start - Date.now());
        await post(v8, 'Profiler.start');
        profiling = true;
        const actualStart = now();
        await delay(start + duration - Date.now());
        const actualEnd = now();
        const { profile } = await post(v8, 'Profiler.stop');
        profiling = false;
        await sync('termium-end');
        const complete = new Promise<any>(resolve => cdp.once('Tracing.tracingComplete', resolve));
        await cdp.send('Tracing.end');
        tracing = false;
        const info = await complete;
        if (!info.stream || info.dataLossOccurred) throw Error('Chromium trace missing or lost events');
        const output = await fs.open(report + '.chromium.json', 'w');
        try {
            for (;;) {
                const chunk = await cdp.send('IO.read', { handle: info.stream, size: 1024 * 1024 });
                await output.writeFile(chunk.base64Encoded ? Buffer.from(chunk.data, 'base64') : chunk.data);
                if (chunk.eof) break;
            }
        } finally {
            await output.close();
            await cdp.send('IO.close', { handle: info.stream });
        }
        await json('.node.cpuprofile', profile);
        await json('.server.timeline.json', { clock, actualStart, actualEnd, control, environment, categories, dropped, spans });
        await json('.server.done', { complete: true });
    } finally {
        if (profiling) await post(v8, 'Profiler.stop').catch(() => {});
        if (tracing) await cdp.send('Tracing.end').catch(() => {});
        v8.disconnect();
        await cdp.detach().catch(() => {});
    }
}
