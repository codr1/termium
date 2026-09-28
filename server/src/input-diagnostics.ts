import { AsyncLocalStorage } from 'node:async_hooks';
import { InputEvent } from '../generated/bc';

// Opt-in metadata only: never log typed text, paste contents, or page URLs.
export const inputTraceEnabled = process.env.TERMIUM_INPUT_TRACE === '1';
const context = new AsyncLocalStorage<number>();
let sequence = 0;

export function inputTrace(stage: string, fields: Record<string, unknown> = {}) {
    if (inputTraceEnabled) console.log(JSON.stringify({
        scope: 'termium.input', time: new Date().toISOString(), id: context.getStore(), stage, ...fields,
    }));
}

export function traceInput<T>(event: InputEvent, action: () => Promise<T>): Promise<T> {
    if (!inputTraceEnabled) return action();
    return context.run(++sequence, async () => {
        const started = performance.now();
        inputTrace('queued', { kind: event.kind, tab: event.tabId, generation: event.generation,
            textBytes: Buffer.byteLength(event.text), modifiers: event.modifiers });
        try {
            const result = await action();
            inputTrace('result', { ok: true, elapsedMs: performance.now() - started });
            return result;
        } catch (error) {
            inputTrace('result', { ok: false, elapsedMs: performance.now() - started, error: (error as Error).message });
            throw error;
        }
    });
}
