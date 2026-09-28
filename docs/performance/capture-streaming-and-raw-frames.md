# Capture streaming and raw frames

Recorded 2026-09-28. **Research only; the application still uses client-paced screenshot requests.** The standalone streaming toy is not part of the installed capture pipeline. Resume this investigation after the release.

## What crosses the browser connection

`Page.startScreencast` delivers a sequence of complete, independently encoded PNG or JPEG images. Each `Page.screencastFrame` event contains base64 image data, metadata, and a session ID that the consumer acknowledges with `Page.screencastFrameAck`. It is not a video codec, changed-pixel stream, or raw framebuffer API.

The same event can travel over a CDP WebSocket connection, but **our application and measured toy launch Puppeteer with `pipe: true`**. They use a local pipe, not a WebSocket. That changes transport, not the encoded-image payload. The earlier conversational WebSocket explanation described CDP's message contents, not our actual launch configuration.

```text
Chromium pixels -> PNG/JPEG encode -> base64 in CDP message
  -> Node base64 decode -> binary image bytes
  -> application gRPC -> Go client
       Kitty default: PNG bytes -> Kitty protocol -> terminal decodes PNG
       Sixel default: JPEG decode -> pixels -> palette conversion -> Sixel
```

The toy stops in Node after receiving, base64-decoding and hashing image bytes; it does not run gRPC or either terminal renderer. Kitty PNG passthrough avoids application pixel decoding, not PNG encoding in Chromium or PNG decoding in the terminal. Sixel requires pixels and therefore pays the intermediate image decode. The different defaults reflect previous pipeline measurements, not a requirement that Sixel use JPEG or Kitty use PNG.

## Recorded baseline and experiment

- [Whole-pipeline baseline and evidence hashes](2026-09-28-baseline.md).
- [Standalone streaming results, CPU and sampled frame-age caveats](2026-09-28-streaming-poc.md).
- [Toy and reproduction commands](../../experiments/cdp-streaming/README.md).
- [Viewport scaling from 720p to 4K](2026-09-27-viewport-scaling.md).

On ea, two screenshot/stream/stream/screenshot sequences per resolution (one per format) found 4K PNG delivery of 29.3–29.9 frames/s versus 9.9–10.0 from screenshots; 4K JPEG delivered 16.8–16.9 versus 7.4–7.5. The 30 Hz fixture limits these results. CPU increased, and sampled JPEG content was older. These are capture-only results, not Termium or visible-terminal FPS. The report records all four resolution/format comparisons and their limits.

## Can we select streaming FPS?

The measured browser's `Page.startScreencast` has no direct FPS argument. It supports `everyNthFrame`, which skips candidate frames before image encoding. This is a sampling fraction, not a fixed rate: every second candidate from a 30 Hz source is roughly 15 FPS, but candidate arrival rates vary.

Acknowledgements release Chromium's in-flight allowance. Pacing them is an experiment worth trying near Termium's 24 FPS target, not a proven smooth rate limiter. It may increase frame age or cause bursts. Receiving every event and dropping extras at the consumer bounds our work but does not recover CPU already spent encoding those images.

Protocol capabilities vary by Chromium build. The current upstream schema also lists `maxFramesInFlight` and `sendLastFrame`, and a separate recording API with a `frameRate` option. They were not present in the saved `startScreencast` schema used by this experiment, and were not tested. Inspect the actual installed browser protocol before using newer controls. A recording API is not interchangeable with live per-frame delivery.

## Can we obtain raw pixels instead?

The tested CDP screenshot/screencast path has no raw RGB/RGBA/BGRA or shared-memory framebuffer output. Locality alone does not eliminate compression, base64, or copying. Chromium has pixels internally before image encoding, but exposing that internal surface requires a different capture interface or a maintained browser modification.

| Candidate | Raw access | Work and limits |
| --- | --- | --- |
| Electron offscreen rendering | `paint` events with a bitmap, dirty region, and explicit frame-rate control; `NativeImage.toBitmap()` copies raw bytes | Small standalone prototype is practical. Changing the production browser host requires separate extension, navigation, packaging and platform validation. Raw access is not automatically zero-copy. |
| Chromium Embedded Framework (CEF) | `CefRenderHandler::OnPaint` supplies a complete width × height × 4 BGRA image plus dirty rectangles; windowless rendering has a rate setting | Native embedding and distribution work. Callback buffer lifetime and ownership must be respected. Dirty rectangles could help select Sixel bands, but this has not been implemented or measured. |
| Stock Chromium extension capture | `tabCapture` -> `MediaStreamTrackProcessor` -> unencoded `VideoFrame`; `copyTo()` can produce RGBA bytes | Activation/permission requirements, headless operation and efficient export to the client need proving. Conversion/readback may remain; RGBA output does not prove the source avoided chroma subsampling. |
| Custom Chromium capture interface | Export pixels before the existing image encoder, potentially through shared memory | A browser patch, build and update obligation; no ready-made CDP switch or measured implementation. |

A raw Sixel path could be `browser pixels -> bounded reusable local buffer -> palette conversion -> Sixel`. It removes intermediate PNG/JPEG encoding and decoding, but does not remove browser rendering, possible GPU readback, pixel-format conversion, transfer, or Sixel preparation. GPU shared textures are not directly consumable by today's CPU Sixel encoder.

At 3840 × 2160, four-byte pixels occupy 33,177,600 bytes per frame (31.64 MiB), or about 796 MB/s at 24 FPS before extra copies. This is memory traffic, not a claim that local bandwidth is saturated. Reusing bounded buffers and measuring actual copies matters. The existing four frame arenas are client-owned buffers, not automatically memory shared with Chromium.

## Resume after release

1. Extend the existing toy with acknowledgement pacing near 24 FPS, `everyNthFrame: 2` as a comparison, and a deliberately slow consumer. Measure delivered rate, CPU, sampled frame age, drops and queue bounds on ea. Do not infer CPU savings from discarded events alone.
2. Build a separate Electron offscreen-bitmap toy to test removal of intermediate codecs. Measure delivery into a real CPU buffer, including `toBitmap()` copying, using the same fixture/resolutions. Record browser differences; this is an architectural comparison, not a single-variable CDP benchmark. Do not migrate Termium as part of the toy.
3. Separately investigate capped capture resolution/upscaling and Sixel hot functions. The measured ~342 ms Sixel preparation at 4K remains even if capture improves.

Before choosing any production stream, verify navigation, resize, target replacement, idle pages, pause/resume, shutdown, and bounded latest-frame ownership. Do not add expected gains together or claim a universal speedup. Native browser/Firefox surveys and partial terminal updates remain later work.

## Primary sources

- [CDP Page schema](https://github.com/ChromeDevTools/devtools-protocol/blob/master/pdl/domains/Page.pdl) — upstream evolves; the experiment's saved browser schema is authoritative for its tested controls.
- [Measured Chromium image-encoding path](https://github.com/chromium/chromium/blob/4999cc1efed37c4d91dc4ce6ec4b0a50e2a9a8cb/content/browser/devtools/protocol/page_handler.cc#L1797).
- [Electron offscreen rendering](https://www.electronjs.org/docs/latest/tutorial/offscreen-rendering/) and [NativeImage raw bitmap API](https://www.electronjs.org/docs/latest/api/native-image#imagetobitmapoptions).
- [CEF render callbacks](https://cef-builds.spotifycdn.com/docs/147.0/classCefRenderHandler.html) and [windowless frame rate](https://cef-builds.spotifycdn.com/docs/151.2/structcef__browser__settings__t.html).
- [Chrome tab capture](https://developer.chrome.com/docs/extensions/reference/api/tabCapture), [unencoded track processing](https://developer.chrome.com/docs/capabilities/web-apis/mediastreamtrack-insertable-media-processing), and [WebCodecs pixel copying](https://www.w3.org/TR/webcodecs/#dom-videoframe-copyto).
