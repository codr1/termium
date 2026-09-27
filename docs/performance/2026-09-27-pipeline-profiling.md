# Whole-pipeline profiling on ea — 2026-09-27

The Sixel-only profile was insufficient to explain the pipeline. This follow-up measures both renderers, Go work versus wait, Node, and Chromium's screenshot stages. **At 4K, Sixel is primarily preparation/pacing-limited; Kitty is capture-limited.** Neither conclusion comes from adding stage durations: capture and preparation overlap.

[Interactive timeline: both renderers at both resolutions](2026-09-27-pipeline-timeline.html). [Earlier Sixel CPU/allocation profiles](2026-09-27-sixel-profiling.md).

## Work versus waiting

Four traced 10-second canvas runs, drained PTY, with the existing 24 FPS ceiling and adaptive pacing unchanged. Means below describe completed spans strictly before the end of the window; spans synthetically closed at that boundary are excluded from duration means. Occupancy clips all spans, including those crossing boundaries. Writes/s are harness output, **not visible terminal FPS**.

| Pipeline | Writes/s | Capture RPC mean | Preparation mean | PTY write mean | Preparation working | Preparation waiting |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Sixel, 1280×720 | 15.4 | 59.0 ms | 46.2 ms | 4.2 ms | 71.5% | 28.5% |
| Kitty, 1280×720 | 15.3 | 64.1 ms | 0.44 ms | 1.38 ms | 0.67% | 99.33% |
| Sixel, 3840×2160 | 2.3 | 135.0 ms | 342.4 ms | 25.9 ms | 80.1% | 19.9% |
| Kitty, 3840×2160 | 9.8 | 100.7 ms | 1.16 ms | 3.40 ms | 1.14% | 98.86% |

“Working” means elapsed time inside preparation, not CPU utilization. “Waiting” means the preparation goroutine is inside its pending-frame receive/select; it is not proof of exact OS scheduler state. CPU profiles supply the separate execution-cost view.

- **Sixel 720p:** capture RPC occupies 91.3% of the window; preparation overlaps it for 62.8% of the window. Preparation waits during RPC for 28.5%. This is a pipeline, not capture → prepare → write serialized on one goroutine.
- **Sixel 4K:** capture RPC occupies 31.1%, intentional capture-loop pacing 68.9%, preparation 80.1%. Capture/preparation overlap is 11.2%. Nearly all of the preparer's 19.9% waiting occurs while a capture is outstanding. The loop currently targets `max(1/24 s, 1.25 × max(preparation cost, output cost))`, using a decaying maximum for cost. Its deliberate headroom explains why preparation is not continuously busy. This is an opportunity to experiment with scheduling later, not evidence that all pacing is wasted or safe to remove.
- **Kitty:** the capture RPC occupies 97.9% / 98.9% of the window. PNG passthrough takes little preparation work, so the preparer waits almost continuously for screenshots. Additional 30-second Kitty CPU/timeline runs without Chromium tracing reproduced this shape: preparation 0.70% / 1.05%, waiting 99.29% / 98.95%; writes 15.50 / 9.97 per second.
- Queue and publish-to-output delay are small in this workload. At 4K, median raw-frame queue delay is about 0.01 ms in both paths. This does not measure terminal paint delay.

The lanes' percentages overlap and must not be summed. “An RPC is outstanding” also does not mean Chromium spends that entire interval on CPU.

## Where capture time goes

The trace matches 153 / 152 / 23 / 97 complete captures for Sixel 720p / Kitty 720p / Sixel 4K / Kitty 4K respectively, with zero unmatched complete captures inside the server trace window. These are **per-capture means from the same matched set**, so each column's components add to its CDP total (rounding aside).

| CDP screenshot component | Sixel 720p, JPEG | Kitty 720p, PNG | Sixel 4K, JPEG | Kitty 4K, PNG |
| --- | ---: | ---: | ---: | ---: |
| Node send → forced-redraw event | 0.32 ms | 0.46 ms | 0.55 ms | 0.48 ms |
| Forced redraw → browser surface-copy request | 28.37 ms | 31.99 ms | 27.02 ms | 26.91 ms |
| Surface-copy request → image encoder begins | 16.05 ms | 19.81 ms | 17.31 ms | 17.88 ms |
| `EncodeBitmapAsJpeg` / `EncodeBitmapAsPngFast` | 6.14 ms | 4.12 ms | 38.52 ms | 31.70 ms |
| Encoder ends → Node receives CDP result | 6.85 ms | 6.37 ms | 47.68 ms | 21.15 ms |
| **Whole CDP screenshot call** | **57.73 ms** | **62.74 ms** | **131.07 ms** | **98.12 ms** |

The two intervals before encoding include presentation scheduling, interprocess delivery, copy work, and waiting. They are not pure CPU, nor isolated GPU-readback bandwidth measurements. The GPU-process `CopyOutputRequest` asynchronous lifetime averages 14.32 / 17.25 / 12.02 / 13.22 ms; this too is a request lifetime, not an individual readback function's CPU time. The short synchronous `CopyOutput` submission scope must not be substituted for completion.

### A substantial cost after image encoding

The browser's enclosing `ThreadControllerImpl::RunTask` is identified by its posted source `copy_output_request_mojom_traits.cc:SendResult`. It begins only 0.03–0.04 ms before image encoding. After the encoder returns, the same browser task continues:

| Measurement | Sixel 4K | Kitty 4K |
| --- | ---: | ---: |
| Task wall time remaining after encoding | 44.83 ms | 19.50 ms |
| Enclosing task thread-CPU minus encoder thread-CPU | 44.78 ms | 19.47 ms |
| Task ends → Node receives result | 2.85 ms | 1.65 ms |

The CPU subtraction includes the tiny pre-encode portion, so it is an approximation of post-encode CPU, not an instruction-level attribution. Still, this shows that **most of the post-encode interval is browser-side CPU execution**, not Node or Go waiting for network bandwidth.

Source inspection identifies a plausible response-construction path: [`PageHandler::ScreenshotCaptured`](https://github.com/chromium/chromium/blob/4999cc1efed37c4d91dc4ce6ec4b0a50e2a9a8cb/content/browser/devtools/protocol/page_handler.cc#L1828) invokes `sendSuccess(Binary::fromVector(...))` after the encoder; [`DevToolsSession::SendProtocolResponse` and `DispatchProtocolMessageToClient`](https://github.com/chromium/chromium/blob/4999cc1efed37c4d91dc4ce6ec4b0a50e2a9a8cb/content/browser/devtools/devtools_session.cc#L427) serialize the result and convert CBOR to JSON for a JSON client. **Serialization/base64/copies/cleanup are candidates within the measured task remainder, not individually profiled culprits.** No symbolized native stack sample isolates their shares yet. Screenshot payloads averaged 3.63 MB JPEG versus 1.50 MB PNG at 4K in this canvas scene; image complexity and format matter.

### Node/Go bridge costs

Measured mean Node `Buffer.from(data, 'base64')` is 0.30 ms for 4K JPEG / 0.19 ms for PNG. Protobuf encoding is 0.72 / 0.46 ms. Viewport admission, target selection and cached viewport checks total about 0.04 ms per frame. Mean Go RPC duration minus the matched server handler is 2.46 / 1.52 ms; that residual includes transport, client decode, scheduling and instrumentation, and must not be labeled pure IPC latency. It excludes protobuf work inside the handler.

These costs do not support blaming the Node↔Go bridge for the roughly 100 ms capture time. Replacing Node alone would also retain Chromium's own screenshot encoding and CDP response costs.

## CPU hot functions and opportunities

The earlier **30-second production Sixel CPU profiles** remain the stronger samples for client execution cost. Cumulative percentages overlap:

| Hot function / source | 720p / 4K share of sampled Go CPU | Concrete opportunity |
| --- | ---: | --- |
| `go-sixel.(*Encoder).writePixelData` | 60.1% / 57.7% cumulative | Reduce emission work, not merely the number of retained frame allocations. |
| Full-width per-color scan, `sixel.go:395–397` | ~6 s flat in each 30 s profile | Investigate skipping unused color ranges or reducing scanned width while preserving exact output/decoding. |
| Tiny `w.Write([]byte{…})` sites, lines 405/423/426 | ~4.7–5.1 s cumulative combined | Reuse small scratch or batch emission; avoid fresh tiny slices and excessive interface/writer calls. These costs are inside `writePixelData`, not additional to its 60%. |
| JPEG `(*decoder).processSOS` subtree | 20.6% / 22.7% cumulative | Compare the complete decode/preparation/capture path for format choices; a faster capture encoder alone does not establish a faster pipeline. |
| `runtime.mallocgcTinySC2` | 8.3% / 7.4% flat | Tiny-allocation overhead is material. This is allocation CPU, not garbage collection. |

GC-related stacks were roughly 2.0% / 0.8% of sampled CPU. The four arenas are reused across frames; they do not currently cover all third-party encoder temporaries. A fourth-arena scratchpad remains deferred. Eliminating or batching tiny writes at their source is a narrower candidate than changing frame ownership again.

Chromium has real CPU work too. Across the 10-second 4K Kitty trace, `EncodeBitmapAsPngFast` accounts for about 3.11 seconds of thread CPU, `LayerTreeHost::DoUpdateLayers` about 1.20 seconds, and `SoftwareRenderer::DoDrawQuad` about 0.86 seconds. These are named trace scopes, **not a complete symbolized native flame graph**; parent and child scopes overlap. The screenshot-response task remainder adds the cost identified above.

### Kitty client and Node hot functions

[Full weighted CPU tables](2026-09-27-cpu-hot-functions.md). The additional 30-second Go profiles contain 1.16 s / 2.00 s of CPU samples at 720p / 4K: approximately **3.9% / 6.7% of one core** for the Go process, not the whole browser process tree.

| Kitty Go function | 720p | 4K |
| --- | ---: | ---: |
| `encoding/base64.(*Encoding).Encode`, flat | 0.28 s | 0.23 s |
| `internal/runtime/syscall/linux.Syscall6`, flat | 0.23 s | 0.38 s |
| `tcell.(*tScreen).drawCell`, cumulative | 0.12 s | 0.77 s |
| `tcell.(*CellBuffer).GetContent`, flat | 0.04 s | 0.28 s |
| `runtime.memmove`, flat | 0.04 s | 0.11 s |

The 4K `drawCell` subtree is 38.5% of this small Go CPU total, about 2.6% of one core over the run. Cell count therefore does affect text-UI redraw work, even though the graphics protocols are governed by image pixels. Avoiding unnecessary text-UI redraws could save CPU; it is not the primary FPS opportunity while capture takes ~100 ms and preparation ~1 ms. These profiles have only 116 / 200 sampling ticks, so tiny differences should not be ranked confidently.

Node V8 profiles attribute 92.5–97.3% of their roughly 10-second windows to `(idle)`. Named functions total 174–358 ms: `Connection.onMessage` contributes 33–82 ms, followed by buffer conversion/copy and protobuf-writer functions. Inspector `post` itself contributes 19–27 ms and is diagnostic overhead. Collector-attributed weighted samples are 78–329 ms; these are neither pause totals nor precise thread-CPU measurements. The Node profile corroborates low activity, rather than an image encoder hidden in the server runtime.

## Environment finding

All four actual Termium traced runs report **software compositing and software rasterization**, with an ANGLE SwiftShader renderer. The browser command line includes headless operation; no acceleration flags were changed in this experiment. A fast host CPU is not proof that this browser workload is GPU-accelerated. This does not establish what a user's Ghostty/foot renderer does or what another Chromium configuration would achieve.

## Next bounded experiments

1. **Sixel emission:** batch tiny writes/reuse emission scratch, then measure production canvas and replay separately. Preserve output equivalence and writer-failure handling. Retain only a repeatable benefit; the previous palette-buffer experiment is not revived by this profile.
2. **Capture/streaming:** compare a bounded screencast prototype to screenshots with identical viewport/format/content, recording freshness, frame drops and actual writes. The forced-redraw/presentation interval gives a reason to try it. Streaming does not automatically eliminate encoding or CDP response construction.
3. **Capture response and environment:** investigate the browser task's remaining CPU with native symbols or narrower trace instrumentation; separately test a supported accelerated headless configuration. A same-language client is not demonstrated to solve these costs. Existing capture-format switches allow a smaller controlled experiment before architecture changes.
4. **Lower capture resolution + upscale:** potentially reduces capture encoding, response bytes, decoding and Sixel preparation together. Evaluate text legibility and terminal scaling cost, not just screenshot time. This is still a quality/performance experiment, not implemented behavior.

Adaptive pacing is an additional scheduling candidate: 4K Sixel preparation is idle about one fifth of the time. Test freshness, input responsiveness and wasted/dropped work before reducing headroom. No expected speedups from these candidates should be added together.

## Method, validation and artifacts

- Host: ea (`192.168.1.100`), native Linux amd64, Ryzen AI 9 HX 370. All performance runs were there; no user processes were stopped. Local machine used only for builds, tests and analysis.
- Diagnostic source: profiling worktree at `3f4d125` plus saved diagnostic changes (now committed as `3d7ca24`); runtime code base includes `2beb236`. Exact client SHA256: `972f58ec8190cf9b3c86f23054e47144c396b9606c08d5dfcafd40e2ff625bce`. Copied runtime Git metadata is **not** source provenance. The patch, new helper sources and executable are preserved separately.
- Fresh runtime: `/tmp/termium-pipeline-ea-fGpsxF/runtime`. Driver: `/tmp/termium-pipeline-profile/run-ea.sh`. Results exist both on ea at `/tmp/termium-pipeline-ea-fGpsxF/results` and locally at `/tmp/termium-pipeline-profile/results`.
- Four runs with `TERMIUM_PROFILE=cpu TERMIUM_TIMELINE=1 TERMIUM_CAPTURE_TRACE=1`, `--scenes canvas --duration 10s --warmup 5s --repeats 1`; 162×49 or 482×139 PTY cells produced exactly 1280×720 or 3840×2160 browser pixels. Sixel used JPEG quality 60 and websafe palette; Kitty used PNG passthrough. Two additional 30-second Kitty CPU/timeline runs omitted Chromium/Node tracing. All six completed successfully, with no capture/prepare/write error counters or dropped diagnostic events.
- Server tracing uses its own browser CDP session. It records `devtools,renderer,renderer_host,cc,viz,gpu,toplevel,toplevel.flow`, without screenshot-image trace embedding. Chromium reports no trace loss. Node V8 profiling and Go CPU sampling run over bounded windows. Reports are withheld until trace/profile finalization; no benchmark is left running.
- Go spans are clipped to the client's saved window. Native timestamps are mapped with bracketed clock-sync markers; the narrowest marker bracket supplies the offset (58–133 µs half-width). End markers are consistent within their wider brackets. Analysis uses fully contained server captures, avoiding trace setup and end boundaries. Node CPU profiling starts a few tens of milliseconds after the requested window start; its actual interval is recorded.
- CPU/trace collection perturbs execution. These runs diagnose stage ownership; they are **not** A/B evidence or an uninstrumented speed claim. Aggregate harness process CPU includes trace finalization and is not used to attribute capture cost.
- Validation: server build/typecheck and server tests passed; full Go client tests with race detector passed, including timeline sealing and completion-barrier checks. Independent static review found no blocker in timing hooks/lifecycle. This is Linux-only diagnostic validation, not release qualification. No render optimization, public deployment, PR, merge or release was performed.
- Durable [analysis scripts and summarized evidence](2026-09-27-pipeline-analysis/) are included beside this report. Full analysis scripts and detailed JSON summaries: `/tmp/termium-pipeline-profile/analyze_{pipeline,chrome,response}.py`; per-run `pipeline-analysis.json`, `chrome-analysis.json`, `response-analysis.json`. Raw logs and exact binary hashes are preserved. The interactive timeline embeds the measured spans so it remains usable without `/tmp` artifacts. Very large raw browser traces remain outside Git.
