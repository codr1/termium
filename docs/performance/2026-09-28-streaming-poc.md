# Standalone CDP streaming experiment — 2026-09-28

**Streaming substantially increases capture-only throughput on ea, but is not a free latency or CPU improvement.** The strongest result is 4K PNG: 29.3–29.9 frames/s streamed versus 9.9–10.0 from sequential screenshots. At 4K, streaming consumes more CPU per second and somewhat more CPU per delivered frame. JPEG also delivers older sampled content. No Termium integration is implemented or implied by these results.

The existing full-pipeline results were first [recorded as the baseline](2026-09-28-baseline.md), with hashes of the retained evidence. This experiment uses its **own fresh screenshot controls**, because a capture-only toy cannot be compared directly to Termium's renderer/PTY FPS.

## Repeated results

A = sequential screenshot; B = screencast. Each condition ran A1 → B1 → B2 → A2, with a fresh browser each time, three seconds of warmup and ten measured seconds. Ranges below are the two run results, not confidence intervals. Ratio ranges use B1/A1 and B2/A2.

| Viewport / format | Screenshot frames/s | Stream frames/s | Stream / screenshot |
| --- | ---: | ---: | ---: |
| 1280×720 JPEG | 19.3–20.2 | 30.0 / 30.0 | 1.49–1.55× |
| 1280×720 PNG | 15.9–18.0 | 30.0 / 30.0 | 1.67–1.89× |
| 3840×2160 JPEG | 7.4–7.5 | 16.8–16.9 | 2.24–2.28× |
| 3840×2160 PNG | 9.9–10.0 | 29.3–29.9 | 2.93–3.02× |

The fixture targets 30 updates/s. Hitting 30 is a workload ceiling, not a measured maximum streaming capacity. The toy has no 24 FPS cap, no Go renderer, no gRPC bridge and no terminal writes. There are 3,211 measured arrivals across the sixteen trials. Every measured adjacent payload digest changed; that is evidence of changing encoded payloads, not an assertion that every visual frame was individually decoded.

### CPU and sampled content age

CPU is **Node plus its Chromium process tree**, as average cores (`100% = one core`). It includes the small resource sampler, base64 decode/hash sink and animated page. It is not Go-only CPU or GPU utilization. CPU accounting covers a small tail around the exact throughput window.

Content age is the median of ten retained images per run: Node receipt time minus the time represented by an encoded 30 Hz frame marker. Logical ticks precede actual drawing, usually by up to one tick and possibly longer under stalls. This is not input latency or exact compositor presentation age; ten samples do not establish a full-frame latency distribution.

| Viewport / format | Screenshot CPU cores | Stream CPU cores | Screenshot sample-age median | Stream sample-age median |
| --- | ---: | ---: | ---: | ---: |
| 720p JPEG | 0.53–0.54 | 0.67–0.72 | 31–44 ms | 32–33 ms |
| 720p PNG | 0.55–0.56 | 0.68–0.70 | 47 / 47 ms | 19–22 ms |
| 4K JPEG | 1.13 / 1.13 | 2.93–2.99 | 122–126 ms | 195–197 ms |
| 4K PNG | 0.94–0.97 | 3.30–3.45 | 75–103 ms | 93–106 ms |

- **4K PNG:** about triple throughput. Content age does not improve consistently across the two pairings, while CPU rises substantially. The pipeline can deliver frames more frequently without making each frame correspondingly younger.
- **4K JPEG:** more than double throughput, but consistently older sampled frames and roughly 2.6× the CPU per second. It is not an unconditional replacement for screenshots.
- Approximate process-tree CPU per delivered 4K frame is 150–153 ms for JPEG screenshots versus 173–178 ms streamed; PNG is 94–98 ms versus 110–118 ms. This amortizes the animated page and sampler too; it is not a profiler's attribution to one encoder call. At 720p the corresponding per-frame CPU estimate decreases with streaming.
- Average encoded payload sizes are similar between modes: about 0.40 MB JPEG / 0.44 MB PNG at 720p, 3.63 MB JPEG / 1.50 MB PNG at 4K. The throughput win is not explained by silently sending smaller images or markedly smaller payloads.
- Sampled peak summed process RSS was roughly 1.58–1.62 GiB at 720p. At 4K, screenshot JPEG was 1.85–1.90 GiB versus streaming 2.12–2.16 GiB; PNG was 1.78–1.93 GiB versus 1.98–2.00 GiB. **Summed RSS double-counts shared pages and is not physical memory/PSS.** These numbers also include retained image samples and fresh-browser variation.

## What the toy does

[Code and runnable commands](../../experiments/cdp-streaming/README.md). It uses the installed Puppeteer package directly, launches one headless browser and serves a local seeded canvas fixture. No application runtime files were edited.

Screenshot mode reuses one target CDP session and calls `Page.captureScreenshot` sequentially, with `fromSurface: true`, `captureBeyondViewport: false`, `optimizeForSpeed: true`, and JPEG quality 60 when appropriate. Streaming registers the event handler before `Page.startScreencast`, requests the same format/dimensions/quality and `everyNthFrame: 1`, consumes each event, then acknowledges its supplied `sessionId`. Both paths base64-decode and SHA256-hash the received payload. The sink keeps at most one image per second for later validation; it does no image decoding or disk writes in the measurement window.

The fixture derives from Termium's canvas workload and adds a 384×12 black/white row containing a sync word and logical frame counter. After each measured run, a separate browser decodes the ten saved images, checks the marker/contrast and exact dimensions, and computes sampled logical content age. **All 160 images validated and all ten sampled frame counters in each run were distinct.** This checks sampled content progression and resolution, not complete pixel equality, legibility on arbitrary sites, or every delivered frame.

The screenshot and screencast methods use different internal Chromium paths. Source for the measured browser shows that screencasting consumes compositor frames through a video consumer and [encodes on worker threads before delivering events](https://github.com/chromium/chromium/blob/4999cc1efed37c4d91dc4ce6ec4b0a50e2a9a8cb/content/browser/devtools/protocol/page_handler.cc#L1797). Its fast-encoder selection matches the screenshot setting in this build, and the desktop path requests ARGB rather than the Android I420 path. It still performs image encoding and sends encoded payloads through CDP. Source and measurements are consistent with useful overlap/concurrency; this experiment did not collect native traces to assign the speedup to individual functions or prove where the extra 4K CPU is spent.

## Judgment and next boundary

The standalone proof is successful: the existing screenshot request/reply cadence is not the maximum frame-delivery rate available from this Chromium instance. **PNG streaming merits a bounded follow-up**, particularly for Kitty's inexpensive passthrough path.

It is not ready for Termium integration. The next useful experiment would control production/freshness near the application's 24 FPS target and exercise a slow consumer, retaining a bounded latest-frame policy instead of building a queue. Simply discarding an already-encoded event at the Node sink does not necessarily save browser encoding CPU. JPEG's ~195 ms sampled age and the 4K CPU increase should be addressed before choosing a default. Navigation, resize, target changes, static pages, pause/resume and shutdown are also outside this toy's success claim.

Sixel's separate ~342 ms preparation cost at 4K remains; faster capture alone does not remove it. GPU acceleration, capped capture resolution/upscaling and Sixel emission improvements remain separate experiments. No expected gains are added together, and no application FPS improvement is claimed here.

## Evidence and validation

- Source: `5d73357`, isolated `experiment/cdp-streaming` worktree. A later docs/analysis commit adds the report; measured runtime scripts are unchanged. The original working checkout and its user edits were left alone.
- Host: ea (`192.168.1.100`), native Linux, Ryzen AI 9 HX 370; the same browser executable used for the recorded baseline. Browser revision/environment are saved in every result. All sixteen runs reported software compositing; no acceleration flags changed and no unrelated process was stopped.
- Driver: `/tmp/termium-streaming-evidence/run-ea.sh`, also on ea under `/tmp/termium-stream-poc-PG0Y9M`. It runs JPEG then PNG at 720p, then JPEG/PNG at 4K, each in A-B-B-A order. No profiler, build or test suite ran alongside the timing windows. Host load averages are preserved in `run.log`; small differences are not attributed to a cause from load averages.
- Raw results, receipts and images: `/tmp/termium-streaming-evidence/results` locally; `/tmp/termium-stream-poc-PG0Y9M/results` on ea. [Summary data](2026-09-28-streaming-summary.json) and [source/result hashes](2026-09-28-streaming-manifest.json) are retained with this report. `experiments/cdp-streaming/analyze.py RESULTS_ROOT` reproduces the aggregate comparisons after all runs finish.
- All sixteen measured invocations and their image validations exited successfully, with no errors or retries. Two preliminary two-second PNG smoke runs are excluded from the tables. Syntax checks, sampler lifecycle and CLI rejection checks passed; independent source review found no remaining blocker. This validates the standalone Linux experiment, not a release or integration.
- Cleanup check found zero remaining browser processes matching the sixteen measured user-data directories. No push, PR, merge, deployment or release was performed. Work stops at this experiment boundary.
