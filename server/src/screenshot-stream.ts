import * as grpc from '@grpc/grpc-js';
import { Screenshot, ScreenshotRequest } from '../generated/bc';

// Legacy streaming clients still get bounded production: write(false) stops
// capture until drain. Never queue captures behind a previous unfinished one.
export function streamScreenshots(
    call: grpc.ServerWritableStream<ScreenshotRequest, Screenshot>,
    capture: (format: string, cancelled: () => boolean) => Promise<Screenshot>,
) {
    const fps = call.request.fps || 10;
    if (fps < 1 || fps > 60 || !['', 'jpeg', 'png'].includes(call.request.format)) {
        call.destroy(Object.assign(new Error('Use 1–60 FPS and png or jpeg'), { code: grpc.status.INVALID_ARGUMENT }));
        return;
    }
    let busy = false;
    let blocked = false;
    let stopped = false;
    let errors = 0;
    const timer = setInterval(async () => {
        if (busy || blocked || stopped) return;
        busy = true;
        try {
            const frame = await capture(call.request.format, () => stopped || call.cancelled);
            if (!stopped && !call.cancelled) {
                blocked = !call.write(frame);
                errors = 0;
            }
        } catch (error) {
            const code = (error as { code?: grpc.status }).code;
            if (code === grpc.status.UNAVAILABLE || code === grpc.status.FAILED_PRECONDITION ||
                (code === grpc.status.RESOURCE_EXHAUSTED && (error as Error).message === 'Capture is busy')) return;
            if (!stopped && ++errors > 10) { stop(); call.destroy(error as Error); }
        } finally { busy = false; }
    }, 1000 / fps);
    const stop = () => { stopped = true; clearInterval(timer); };
    call.on('drain', () => { blocked = false; });
    call.on('cancelled', stop);
    call.on('error', stop);
    call.on('close', stop);
    call.on('end', stop);
}
