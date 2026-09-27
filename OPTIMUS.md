# OPTIMUS.md

The current backlog is below, followed by implemented changes and historical investigations. Historical timings describe their original probes, not current performance guarantees.

## Capture design rule: prioritize the steady-state path

Agreed 2026-09-16: frame delivery is best effort. A brief stale or transitional frame, including an image of the previously selected tab, is an acceptable tradeoff for lower steady-state overhead. We do not require a transactionally consistent screenshot and browser-state snapshot for every frame.

Design one dedicated capture pipeline around cached active-page state, a reusable capture session, and viewport updates only when needed. The target steady-state browser interaction is `Page.captureScreenshot`; tab enumeration, history reads, and reconciliation should be driven by state changes or separate bounded refresh work, not repeated validation around each screenshot. Cheap local identity checks are fine, but do not add browser round-trips solely to guarantee frame freshness.

Prefer the newest available frame, keep queues bounded, and do not retry obsolete frames to guarantee their delivery. Handle closed targets, stalled captures, and session replacement on the recovery path. Keep complete terminal payloads and serialized terminal writes, and retain input-target validation at input dispatch. Frame metadata must retain honest provenance; it need not be synchronized with the latest toolbar metadata.

Evaluate the redesign by steady-state command counts, latency, CPU, and responsiveness. During navigation/tab races, test eventual recovery and usable input rather than requiring every transitional frame to be discarded. This is a design decision, not an implemented optimization or a proposal to change the local transport protocol.

## Current performance backlog

Reviewed 2026-09-16, updated 2026-09-27 (reusable capture session implemented). The remaining unchecked items are unfinished; their candidate optimizations have no measured benefit yet. Keep the current Go/Node/Puppeteer/Chromium stack as the baseline. Older proposals below are historical notes, not an implementation queue.

- [ ] **Run the full local performance comparison.** Start on the weaker laptop using the [profiling procedure](docs/testing.md#full-local-performance-run-planned). Separate capture, Node/CDP, local RPC, Go preparation/GC, terminal writes, and visible presentation. Compare input latency and long-frame tails as well as frame delivery.
- [x] **Reusable, dedicated capture CDP session.** Landed with optimization 1 via PR #22 after all six Linux/macOS test and package checks passed. Earlier local measurements (A = 92f8e2f vs B = 7a649f3) remain recorded below. The steady-state path reuses one capture session across consecutive captures instead of attaching and detaching per frame; see [Implemented: reusable dedicated capture CDP session](#implemented-reusable-dedicated-capture-cdp-session).
- [x] **Profile Sixel allocation and palette costs (optimization 3).** Bounded CPU/allocation profiling and a fixed-palette storage-reuse experiment are complete. The candidate saved synthetic encoder allocations but slowed the live production canvas workload in both pairings, so it was backed out. A later identical-JPEG replay was 11.9–15.1% faster; the live-pipeline discrepancy remains unexplained. See [the rejected experiment](#investigated-optimization-3--fixed-palette-scratch-reuse-rejected). Broader real-terminal profiling remains open; no runtime change from this experiment is retained.
- [x] **Avoid reapplying an unchanged viewport on every capture.** Implemented; validated on WSL2 and measured on native Linux ea on 2026-09-27 (A = e663a5e vs B = 41f94b0); its own cross-platform CI still pending. The steady-state path caches successfully applied dimensions per control CDP session, distinct from the desired viewport, and skips `Emulation.setDeviceMetricsOverride` when they match; see [Implemented: optimization 2 — skip unchanged viewport updates](#implemented-optimization-2--skip-unchanged-viewport-updates).
- [x] **Reuse four frame arenas (optimization 3 follow-up).** Shared Sixel/Kitty/ASCII ownership path implemented and Linux-validated; bounded ea measurements show lower allocation traffic, a Sixel preparation benefit, higher retained memory and an unexplained small Kitty throughput decrease. See [the arena record](#implemented-four-reusable-frame-arenas). Native CI/package qualification remains pending.
- [ ] **Investigate partial terminal image updates.** Unchanged-image reuse and Sixel band-encoding caches already exist. Updating only changed regions on screen is separate unfinished work. Validate palette/background behavior, cursor placement, scrolling, resize, overlays, and recovery from dropped or failed output across supported terminals before claiming a gain.
- [x] **Remove per-frame tab/history polling (optimization 1).** Landed via PR #22 with all six Linux/macOS test/package checks green on its exact head; locally validated earlier on the stacked `capture-metadata-fast-path` branch. Capture reuses the retained selected tab and omits optional screenshot metadata; the existing independent client state poll and explicit commands/input reconcile state. Warmed captures issue two CDP commands with viewport application still retained. Sixel improved in the short ea comparison. See [the implementation record](#implemented-optimization-1--remove-per-frame-tabhistory-polling).

### Numbered optimization priorities

User-selected order (2026-09-27): **1.** Remove per-frame tab/history polling — landed via PR #22, with all six Linux/macOS test/package checks green on its exact head. **2.** Skip unchanged viewport updates — implemented and locally validated/measured 2026-09-27; performance qualifications below, its own cross-platform CI pending. **3.** Profile Sixel quantization/allocations — investigation complete; buffer-reuse candidate rejected and backed out after production slowdown. **4.** Investigate partial terminal image updates — deferred by user until after release work. These numbers supersede the future-work order, not the historical unit names in earlier briefs. PR #22's CI covers session reuse and optimization 1; it does not cover optimization 2. The initial optimization 3 palette-index candidate remains backed out. Its subsequent four-arena follow-up is implemented and locally validated/measured; the shared path is retained at the user’s direction, with the small Kitty throughput difference explicitly unresolved. See the arena record below.

### Client audit follow-ups

Source-reviewed 2026-09-16 against the current tree. These are investigation candidates, not measured speedups. The external audit also described the screenshot stream and `prepareCapture`, which were removed in PR #18; tests calling an API did not establish a production consumer.

- [x] **Replace band CRCs with direct pixel comparison (first small change).** The websafe path hashes every band and then compares pixels before trusting matching hashes. Compare each band's pixels directly against the last successfully prepared image and remove CRC bookkeeping. Preserve exact equality, short final bands, resize behavior, and recovery after failed preparation. `bytes.Equal` can stop at the first difference, so the audit's three full scans per changed frame is not a measured traffic estimate. Remove the unused column arrays, frame counter, and band-manager methods alongside this change; column arrays alone occupy about 675 KiB at 1920 × 1080.
- [x] **Keep maximum capacity for the normalized band buffer.** Encoding a short final band currently reallocates the buffer, then a subsequent full band reallocates it again. Retain a six-row backing image and pass a bounded view. Test alternating heights and stale-pixel exclusion; measure allocations on heights not divisible by six.
- [x] **Encode band pixels without generating and stripping complete Sixel documents.** Each dirty band previously wrote a header and a full palette through go-sixel, then `stripSixelWrapper` scanned the result and discarded that framing. A completely changed 1080-row websafe frame generated 180 discarded palettes, or 38,880 color definitions, before its final palette. Implemented as a band-only encoder entry point in the forked go-sixel module; see [Implemented: band-only Sixel encoding](#implemented-band-only-sixel-encoding). Caching the final fixed-palette bytes is separate unfinished work below. Preserve palette indices, short bands, writer errors, and decoded pixels. Websafe has 216 colors, not 256.
- [ ] **Cache the final fixed-palette Sixel bytes.** `ComposeFullSixel` regenerates the shared header and full palette definitions on every frame composition. Cache fixed-palette definitions by palette. Keep the dimension-dependent raster header separate, or include dimensions in its cache key. Adaptive palettes need separate handling because changed color-register meanings must invalidate the cached bytes.
- [ ] **Extend band reuse to Plan9.** Both Plan9 and websafe have fixed palettes. Adaptive palettes need a separate design because changes to color-register meanings invalidate cached band data. Compare partial-change and full-change workloads; retain the Plan9 white/register regression coverage.
- [ ] **Reuse decoded RGBA images when eligible.** Avoid the extra copy for an already suitable tightly packed, origin-zero RGBA decode, preserving immutable published frame ownership. Go's JPEG decoder normally returns YCbCr, so this does not remove the default JPEG-to-RGBA conversion. Measure PNG and JPEG paths separately.
- [ ] **Reuse Kitty payload bytes across generation changes.** Identical encoded image data can share prepared graphics, but must carry the new generation, state, and capture metadata and still trigger required image placement. Removing the generation guard alone is unsafe because `reuse()` retains the old generation. Byte equality does not solve different PNG encodings of identical pixels; decoding solely for dedup needs separate evidence.

Lower-priority cleanup: remove the custom `min` definition in favor of the builtin (its callers are live), the unused `lastScreenshotTime`, and duplicate integer formatting where useful. Shared URL parsing must preserve the distinct Home/new-tab/navigation allowances. Parallel modifier key-downs require ordering and failure-cleanup tests; do not apply `Promise.all` as an assumed safe speedup.

Server audit clarification: viewport overrides use the persistent control session, not the temporary capture session. Session reuse and avoiding repeated viewport overrides are independent changes. The session-wide desired viewport is needed to propagate sizing across tabs; it is not automatically redundant with each tab's control state. The existing capture backlog above covers these changes and the already measured command count.

### Verified capture protocol overhead

On 2026-09-16, an ad hoc trace wrapped Puppeteer's connection `_rawSend` around three sequential `BrowserSession.capture('png')` calls after warmup. The real headless Chromium browser used a pipe, Vimium enabled, one static data-URL page, and a 640 × 360 viewport. Every capture issued this sequence:

```text
Target.getTargets                   # capture: initial snapshot
Emulation.setDeviceMetricsOverride  # unconditional viewport application
Target.attachToTarget               # dedicated capture session
Page.captureScreenshot
Target.detachFromTarget
Target.getTargets                   # capture: post-image validation
Target.getTargets                   # readState: initial snapshot
Page.getNavigationHistory
Target.getTargets                   # readState: final validation
```

The second and third screenshot buffers were byte-for-byte equal to their predecessors. All nine commands still ran before the client could perform image reuse. This verifies the steady-state command count for that scenario, not command latency or an FPS improvement; initialization, retries, multiple windows, and concurrent input can add work. The current capture microbenchmark measures neither these tab/history reads nor the viewport/session churn.

This nine-command sequence is the pre-Unit-1 baseline (A = 92f8e2f). As of B = 7a649f3 the steady-state path issues seven commands per warmed capture — only the attach/detach pair above no longer runs; see [Implemented: reusable dedicated capture CDP session](#implemented-reusable-dedicated-capture-cdp-session).

When an item is completed, record its implementation, correctness checks, and measured results below. Keep unmeasured ideas labelled as such.

## Performance harness: baseline before client optimization

The [benchmark harness](docs/benchmarking.md) exercises the actual client/server/browser pipeline on local idle, patch, scroll, and seeded-canvas scenes. It saves repeated runs and comparisons of capture/write rates, latency distributions, reuse/pending drops, process CPU/RSS, Go allocation/GC costs, and payload sizes. Drained-PTY and real-terminal modes are distinct; neither claims visible presentation FPS. The band-only benchmark isolates one-pixel and full-image changes with warm caches.

The CRC/band cleanup above is paused until baseline measurements are available. No band-encoding optimization has been applied as part of the harness. Keep full laptop/terminal profiling and server-phase attribution on the backlog; harness smoke runs are validation, not a performance study.

Validation on 2026-09-16: the full regression suite and typecheck passed. Linux smoke runs completed all four scenes with both graphics protocols, with no capture/preparation/write errors; idle scenes made zero graphics writes after warmup. Comparison of a report with itself produced zero deltas. Foreground-terminal launch and cleanup were also exercised inside a PTY, and the harness test binary cross-compiled for macOS ARM64. Native Mac execution and visible terminal presentation remain separate checks; CI now includes a short native harness smoke run without performance thresholds.

## Implemented: remove unused capture paths

Recorded 2026-09-16. Removed the uncalled `BrowserControls.prepareCapture()` helper and the legacy `StreamScreenshots` RPC, handler, implementation, and stream-specific tests. `ScreenshotRequest` reserves the retired `fps` field number and name; its format field keeps its existing wire number. The client continues to use unary `CaptureScreenshot`, and the used `StreamDialogs` RPC remains.

Image-format, fixture-pixel, resize, and terminal integration checks now exercise unary capture. A deterministic test holds an in-flight capture inside the real server, verifies cancellation followed by successful capture, and separately verifies server shutdown terminates the pending request. A fresh `npm test` passed, including protocol regeneration, static checks, Go race tests, real Chromium integration, terminal graphics decoding, and website tests.

This removes unused surface area and aligns coverage with the shipped client. It does not change the steady-state capture path or claim an FPS improvement; the capture/session/metadata optimizations above remain unfinished.

## Implemented: shared graphics preparation and comparable timings

Recorded 2026-09-08. See [frame preparation](client/frame_pipeline.go), [Kitty encoding](client/kitty_renderer.go), and [benchmark instructions](docs/testing.md#comparing-capture-and-renderer-preparation).

Sixel encoding already ran in the preparation worker, but Kitty’s Base64 encoding and chunk framing ran inside the UI’s display call. Kitty also had separate statistics, extra timing log lines, and a per-frame `stderr.Sync()` when timings were enabled. These differences distorted comparisons and added avoidable work before the UI could handle its next event.

Both protocols now publish complete immutable graphics payloads from the same preparation worker. The same UI-owned writer positions the cursor, writes the payload, and restores it. Terminal writes remain serialized with UI drawing to preserve escape-sequence ordering. The bounded queues, unchanged-image reuse, overlay behavior, and capture pacing remain shared. Kitty-specific statistics and disk sync are removed; common timings split capture/RPC, queue, preparation, and output costs, and report source/payload byte counts and pixel dimensions.

Capture format is independently selectable with `--capture-format jpeg` or `png`, and Chromium uses `optimizeForSpeed`. Defaults stay PNG for Kitty and JPEG for Sixel/ASCII graphics. Forcing a common JPEG default was rejected after measuring the extra Kitty pixel conversion and terminal traffic. Kitty’s PNG path still avoids image decoding/re-encoding; the explicit JPEG comparison path decodes once and sends opaque RGB compressed with zlib, rather than encoding another PNG. Base64 is required by the Kitty protocol and adds roughly one third to the binary payload. Sixel instead requires palette selection and its own band/run-length representation; sharing scheduling does not eliminate those protocol requirements.

### Local performance priorities

The current performance work assumes the existing Go client, Node/Puppeteer controller, and Chromium stack. Normal sessions communicate over a local Unix socket; external network bandwidth is not a constraint for this investigation. Prioritize input responsiveness, sustained frame delivery, CPU usage, memory/GC costs, and terminal rendering on the weaker laptop.

Screenshot bytes crossing the local RPC boundary and graphics bytes written to the terminal are different measurements. Larger buffers can increase copying, allocation, encoding, terminal parsing, and blocked-write costs even on one machine. Record their sizes to explain those costs, but do not treat smaller payloads as the objective or infer that the socket/PTY is saturated. Prefer less CPU work and better latency when larger local transfers are harmless. The current timings combine capture and RPC duration; they do not isolate IPC overhead.

### Measurements and limits

On a Linux workstation with an AMD Ryzen 9 9900X3D, at 1280 × 720, the interleaved Chromium capture probe measured these medians (12 samples per format after warmup):

| Fixture | JPEG capture | Normal PNG capture | Fast PNG capture |
| --- | ---: | ---: | ---: |
| Text | 32.4 ms | 63.1 ms | 38.0 ms |
| Seeded canvas stress case | 23.3 ms | 150.7 ms | 59.9 ms |

The corresponding full-frame preparation benchmark used the same saved captures for both renderers, without race instrumentation. These are mean times per operation from one benchmark run, not latency percentiles. Sixel used the websafe palette. Payload sizes include protocol framing, excluding the cursor-position wrapper.

| Fixture | Source | Renderer | Preparation | Payload bytes | Allocations/frame |
| --- | --- | --- | ---: | ---: | ---: |
| Text | JPEG | Kitty | 15.89 ms | 622,886 | 26 |
| Text | JPEG | Sixel | 17.73 ms | 286,574 | 191,133 |
| Text | Fast PNG | Kitty | 0.094 ms | 208,037 | 7 |
| Text | Fast PNG | Sixel | 13.66 ms | 135,928 | 69,761 |
| Seeded canvas | JPEG | Kitty | 19.80 ms | 3,698,455 | 23 |
| Seeded canvas | JPEG | Sixel | 61.03 ms | 3,049,533 | 1,421,750 |
| Seeded canvas | Fast PNG | Kitty | 1.01 ms | 2,762,919 | 7 |
| Seeded canvas | Fast PNG | Sixel | 78.88 ms | 4,065,450 | 1,735,393 |

Preserved evidence: [capture results and machine/browser metadata](docs/performance/2026-09-08/capture.json), [preparation output including allocated bytes](docs/performance/2026-09-08/preparation.txt), and [commands and source revision](docs/performance/2026-09-08/metadata.json). Fixture generation lives in `scripts/benchmark-capture.mjs`; original screenshot files are not archived. Future runs should retain their captures as well as their measurements.

The rationale for PNG passthrough is primarily avoiding client decode/recompression work and preserving screenshot quality. Its smaller output in these fixtures is additional evidence, not a network-bandwidth requirement. Fast PNG reduced measured capture time relative to normal PNG, with larger text captures. JPEG remains useful for Sixel, especially in the canvas stress case. These findings support the current defaults as a baseline; only a full local performance run can settle the best settings for a particular machine and terminal.

The Sixel allocation counts warrant profiling to locate allocation and GC costs. The counts alone do not establish that GC is the bottleneck. These measurements exclude terminal I/O and paint, so they establish neither a laptop FPS gain nor an inherent protocol ranking. Capture and preparation overlap; adding their times and taking the reciprocal would not measure application FPS.

An informal laptop comparison found Sixel in foot felt faster than Kitty graphics in Ghostty. That observation is the trigger for the investigation, not a controlled benchmark. No end-to-end CPU profile, per-stage IPC attribution, physical paint measurement, or sustained laptop run has been recorded here yet.

Regression tests independently decode Kitty chunks and compressed pixels, check size limits, image replacement, cursor restoration, and payload ownership, and feed identical JPEG pixels to both renderers. Executable integration tests decode actual terminal output and verify source selection. These correctness tests do not measure real Ghostty/foot painting speed or physical display latency. The planned run focuses on local sessions; remote-link tuning is outside its scope.

## Planned: full profiling run on the current stack

Status: planned, not executed. Use the [profiling procedure and metrics](docs/testing.md#full-local-performance-run-planned) and the [available switches](docs/terminals.md#performance-and-profiling-switches). Keep measured findings above separate from future hypotheses.

The next run will compare normal defaults and matched PNG/JPEG sources at equal browser pixel dimensions, starting on the weaker laptop. It will exercise idle pages, local UI-only changes, scrolling/text, image-heavy content, and bounded animation. Attribute time and CPU across Chromium capture, Node/CDP handling, local RPC, Go preparation/GC, terminal writes, and terminal paint. Measure input latency and long-frame tails as well as sustained delivery. Record the current 24 FPS ceiling and adaptive pacing so intentional waiting is not reported as a renderer bottleneck.

Keep the current stack and defaults as the baseline. Change one setting at a time and retain a change only when repeatable measurements show a benefit without losing acceptable image quality, navigation behavior, or terminal correctness.

## Implemented: skip redundant browser-image output

Recorded 2026-09-07. See [frame preparation](client/frame_pipeline.go), [presentation](client/main.go), and [regression tests](client/frame_pipeline_test.go).

### What was being resent

The browser screenshot was being sent to the terminal again when only the local interface needed repainting: editing an address, moving the Mouse keys cursor, or refreshing help/status controls. The Sixel band cache reduced encoding work but still assembled and wrote a complete image. Even unchanged browser frames could keep producing graphics bytes.

### What changed

- Browser-image updates and local UI redraws are tracked separately. Redrawing the address bar or local cursor does not by itself retransmit the browser image.
- Preparation reuses an existing frame when its screenshot bytes, document identity, and browser metadata are unchanged. For Sixel/character rendering, an exact decoded-pixel comparison also catches identical images with different compressed bytes. Reuse avoids another quantization/encoding pass.
- The presenter remembers the frame already displayed. The same frame does not trigger another graphics write, while tcell can still update toolbar text, focus, and cursor cells.
- Kitty passes Chromium’s PNG bytes through directly. Sixel caches encoded bands for reuse and encodes changed bands as needed.
- Resizing, changing documents, opening/closing overlays, and failed output invalidate the displayed image when necessary. Closing help restores the browser image; skipping redundant output must never leave missing or stale graphics behind.
- A bounded worker keeps only the newest pending capture. Capture pacing follows the slower of preparation and terminal output, and pauses while an overlay hides the browser.

### Evidence and limits

`TestImageDamageIndependentOfChromeAndRestoredAfterOverlay` draws an image, then performs twenty local UI redraws and requires zero additional graphics bytes. It also requires image restoration after help closes and rejects repainting an obsolete document. Other frame-pipeline tests cover exact frame/pixel reuse, changing metadata, band encoding, and queue/cancellation behavior.

The older synthetic Sixel probe below measured roughly 68 KB resent per unchanged frame. The fix removes that redundant full-image traffic for an unchanged displayed frame; it is not a measured whole-browser FPS multiplier. Browser capture and RPC transfer can still occur while watching for changes. A changed page still needs new graphics output, and changed browser metadata may produce a new presentation frame even when the pixel buffer is reused. Partial-image terminal updates and end-to-end latency measurements remain follow-up work.

## Implemented: avoid repeated stale-input recovery RPCs

When a tab or document changes, queued input stamped for the old document is cancelled rather than replayed. Previously, each queued event could make another failing write and state-refresh read. The input dispatcher now uses the first successful recovery read to cancel the remaining obsolete queue locally. A regression test checks that thirty-one stale events require only one rejected write and one recovery read, while fresh input still reaches Chromium. This is separate from the graphics optimization above.

## Implemented: exact Sixel band comparison and stable band buffers

Recorded 2026-09-17. Status: source review complete; validated on this Linux machine on 2026-09-17 — the focused Sixel/band regression tests passed, `go vet ./client` and the TypeScript typecheck were clean, and the full `npm test` suite (build, lint, server tests, Go tests with race detector, browser integration, website) passed. This is Linux-only validation on this machine, not Mac validation. See [frame preparation](client/frame_pipeline.go), [band encoding](client/sixel_band_encoder.go), and the Sixel regression tests in [the frame pipeline test file](client/frame_pipeline_test.go).

### Change 1: exact per-band comparison, no hashing

The websafe band path previously hashed each incoming band with CRC-32 and compared it against the hash retained in that band's cache entry; a matching hash still required a pixel-by-pixel confirmation against the last successfully prepared image before reuse. The new code removes all hashing and compares each band's pixels directly against the last successfully prepared frame, reusing the cached encoding only on exact equality.

The comparison is staged: per-band results are accumulated first, and cache entries are updated only after every band has been encoded or reused successfully. Staging is what makes hash-free lazy comparison safe — with hashing removed, the per-band pixel comparison against the last good frame is the only reuse decision, so a failed preparation must not leave any cache entry holding an encoding from the failed attempt. The old design's retained hashes plus pixel confirmation masked the same latent hazard; no corruption was observed or reproduced in it. Staging now guards against it by construction.

Invariants preserved: exact equality (a hash can reject equality, never prove it — direct comparison keeps that property), short final bands, resize behavior (geometry changes rebuild the band manager and encoder with empty caches), and recovery after failed preparation. `bytes.Equal` stops at the first differing byte, so an unchanged band costs a single scan instead of re-encoding; whole-frame equality already has fast paths in `prepare`.

### Change 2: stable maximum-capacity band buffer

The normalized band image keeps its full six-row height and capacity for every encode. A short final band is encoded through a bounded view (`SubImage`) of that buffer, so encoding it no longer reallocates the backing array, and a subsequent full-height band does not reallocate again. The encoder reads pixels only through accessors that respect the view's bounds, so stale rows below the band are never read; the code comment now states that invariant instead of claiming the toolchain would clear them (it does not).

### Evidence

Controlled microbenchmark on this machine: AMD Ryzen 9 9900X3D under WSL2, Go 1.27.1, five repetitions per configuration (`-count=5`), `BenchmarkSixelBandChanges`. This is a CPU cost of the band-change path, not an FPS measurement; concurrent user workloads on this machine make end-to-end numbers unreliable, so claims below are limited to these controlled runs and the saved evidence files.

| Workload | Before (ns/op) | Change 1 only (ns/op) | Both changes (ns/op) |
|---|---:|---:|---:|
| One pixel changed per frame | 381,620–392,004 | 183,903–195,793 | 168,528–174,261 |
| All pixels changed per frame | 14,522,456–14,642,873 | 14,716,620–15,618,818 | 14,202,040–14,579,612 |

- One-pixel workload (microbenchmark only): the median drops from 385,905 ns/op to 190,531 with change 1 alone (−50.6%) and to 171,992 with both changes (−55.4%); the incremental saving of about 19 µs from adding change 2 is unattributed — no explanation is claimed for it.
- All-pixels workload: change 1's range lies above the before range, while both changes' range overlaps it; no cause is asserted for these observations. Allocations drop from ~2,976,958 B/op to ~2,919,487 B/op (−57,471 B/op) and 30/3089 allocs/op to 30/3085 allocs/op.
- End-to-end browser runs were drained-PTY runs (`Display: false` in the saved results; no real terminal rendering). They were inconclusive in either direction: the canvas client CPU range was 89.29–92.99% of one core before and 100.38–106.65% with change 1 only (nonoverlapping), while the both-changes run was load-contaminated (writes/s 5.30–15.03; canvas CPU 50.88–96.85%), overlapping the before range and falling below change 1's entire range. No end-to-end speedup is claimed.

Saved evidence: `/tmp/termium-sixel-before.txt`, `/tmp/termium-sixel-after-1.txt`, `/tmp/termium-sixel-after.txt`; comparison outputs `/tmp/termium-compare-change1.txt` and `/tmp/termium-compare-after.txt`; per-run JSON under `dist/performance/sixel-{before,change1,after}/result.json`.

### Commands

```sh
go test ./client -run '^$' -bench '^BenchmarkSixelBandChanges$' -benchmem -count=5
npm run benchmark -- --label sixel-before --renderer sixel --out dist/performance/sixel-before
```

The end-to-end harness (see [benchmarking](docs/benchmarking.md)) supports the scenes `idle`, `patch`, `scroll`, and `canvas`; the saved runs used all four, three repeats, 30 s measured per run, on the default drained PTY. It is load-sensitive on this machine.

## Implemented: band-only Sixel encoding

Recorded 2026-09-17. Status: source review complete (two test defects found and fixed); validated on this Linux machine on 2026-09-17 — the focused Sixel/band regression tests passed, `go vet ./client` and the TypeScript typecheck were clean, and the full `npm test` suite (build, lint, server tests, Go tests with race detector, browser integration, website) passed. This is Linux-only validation on this machine, not Mac validation. See [band encoding](client/sixel_band_encoder.go), the pixel-only entry point in [the forked go-sixel module](third_party/go-sixel/sixel.go), and the Sixel regression tests in [the frame pipeline test file](client/frame_pipeline_test.go).

### What changed

The websafe band path previously called `Encoder.Encode` for every dirty band, generating a complete Sixel document — header, full 216-color palette definitions, pixel data, terminator — which `stripSixelWrapper` then scanned and discarded down to the pixels. The forked go-sixel module (vendored under `third_party/go-sixel`, its own Go module) now exposes `EncodePixelData`, which runs the same setup and pixel-writing path without header, palette, or terminator; `Encoder.Encode` shares those helpers. `BandEncoder.EncodeBand` returns an owned copy of that pixel-only output, so the per-band framing is no longer generated at all instead of being generated and stripped.

Commits on branch `sixel-band-only-encoder`: 9303f91 (fork entry point), 0054a32 (client switch to pixel-only encoding; `stripSixelWrapper` removed), c68b231 (review fixes: failure-recovery disarming and error-path encoder configuration in tests).

### Review and validation

Source review found no production defects but two test defects, both fixed in c68b231 before validation: the writer-failure recovery path never disabled a latched failure (`tw.failed`/`tw.remaining`), and the error-propagation tests' replacement encoders accidentally ran adaptive palettes instead of the websafe configuration under test.

Validation on this Linux machine at c68b231: focused Sixel/band regression tests passed; `go vet ./client` and TypeScript typecheck were clean; one complete `npm test` run (build, lint, server tests, Go tests with race detector, browser integration, website) passed. Native macOS validation has not been performed for this change.

### A/B measurements: bab0139 (A) vs c68b231 (B)

Interleaved comparison in isolated detached worktrees (`/tmp/termium-ab-A` at bab0139, main @ PR #19 merge; `/tmp/termium-ab-B` at c68b231), benchmark source verified byte-identical between them, compilation caches warmed before measuring, identical toolchain (Go 1.27.1) and settings, runs sequential in the order A → B → B → A. The untracked generated `client/pb` package was copied from the main tree into both worktrees; the `.proto` sources are identical across A..B, so compilation is valid in both:

```sh
go test ./client -run '^$' -bench '^BenchmarkSixelBandChanges$' -benchmem -count=5
```

Medians of five repetitions; ranges over all five:

**one-pixel (ns/op)**

| Run | Median | Range | B/op | allocs/op |
|---|---:|---:|---:|---:|
| A1 (bab0139) | 172,938 | 171,514–192,865 | 64,372 | 30 |
| B1 (c68b231) | 162,852 | 160,561–163,590 | 61,052 | 28 |
| B2 (c68b231) | 162,574 | 159,513–171,199 | 61,052 | 28 |
| A2 (bab0139) | 187,122 | 179,112–248,049 | 64,372 | 30 |

**all-pixels (ns/op)**

| Run | Median | Range | B/op | allocs/op |
|---|---:|---:|---:|---:|
| A1 (bab0139) | 14,283,307 | 14,257,460–16,449,373 | ~2,919,487 (±5) | 3,085 |
| B1 (c68b231) | 13,277,627 | 13,214,266–13,304,980 | ~2,337,389 (±4) | 2,723 |
| B2 (c68b231) | 13,245,916 | 13,229,937–13,287,481 | ~2,337,386 (±2) | 2,723 |
| A2 (bab0139) | 14,517,451 | 14,285,828–15,008,506 | ~2,919,488 (±4) | 3,085 |

- Timing: B is faster than A in both directions — one-pixel −10,086 ns (−5.8%) for A1→B1 and −24,548 ns (−13.1% vs A2) for B2→A2; all-pixels −1,005,680 ns ≈ −1.01 ms (−7.0%) and −1,271,535 ns ≈ −1.27 ms (−8.8%). The improvement persisted across both run orders in this experiment. Its measured magnitude varied; these runs do not establish the cause of that variation or eliminate all environmental effects.
- Memory: The one-pixel workload allocated 3,320 fewer bytes and used two fewer allocations per operation. The all-pixels workload allocated approximately 582 KB fewer bytes and used 362 fewer allocations per operation—about 20% and 12% reductions, respectively.
- End-to-end FPS has not been measured for this change; these are controlled microbenchmark numbers only.

Concurrent-workload caveat: user workloads ran on this machine throughout — the 1-min load average fell monotonically across the sequence (4.36 at start → 3.29 after A1 → 2.71 after B1 → 2.43 after B2 → 2.03 after A2), with codebase-memory-mcp (~23% CPU), claude/codex agent workloads, and hashd running; user Termium sessions were left untouched. No cause is inferred from load averages; persistence across both orders is the basis for the timing claim.

Saved evidence: raw outputs `/tmp/termium-ab-compare/A1.txt`, `B1.txt`, `B2.txt`, `A2.txt`; earlier single-revision runs (load-contaminated, not used for the A/B claim) at `/tmp/termium-sixel-after.txt` (d781982), `/tmp/termium-bandonly-after-c68b231.txt`, and `-run2.txt`.

## Implemented: reusable dedicated capture CDP session

Recorded 2026-09-27. Status: implemented and locally validated/measured on Linux/WSL2 (A = 92f8e2f, B = 7a649f3); cross-platform CI and performance acceptance still pending — the no-regression acceptance gate is not marked passed. See [BrowserControls.capture](server/src/browser-controls.ts) and the capture-session handling in [browser-session](server/src/browser-session.ts).

### What changed

The steady-state capture path previously created a dedicated CDP session for every frame (`Target.attachToTarget`) and detached it afterwards (`Target.detachFromTarget`). It now reuses one capture CDPSession across consecutive captures, recreating it only when the target identity changes or the session fails or is aborted. One in-flight capture and the capture/resize queue are preserved; failure and abort dispose of the session so a replacement starts clean.

### Validation

Source review is complete; eight focused tests, TypeScript typecheck, and one full `npm test` run all exited 0 on this Linux/WSL2 machine at B = 7a649f3 — the review itself has no command exit. The normal client was restored afterwards. Evidence: `/tmp/termium-validate-7a649f/` (this spelling is intentional). No native macOS validation and no new CI were added in this stage.

### Production trace: nine commands down to seven

A probe of the real production `BrowserSession` path measured four frames per PNG/JPEG format per revision, each after five warmup captures, at 640 × 360 with dimensions/formats checked and static consecutive payloads verified equal. A issued exactly nine CDP commands per warmed capture; B issued seven — only the attach/detach pair was eliminated. The unchanged remainder is four `Target.getTargets` calls plus viewport override, navigation-history read, and screenshot. The trace establishes command counts only: it is separate from timings and does not by itself establish speed. Probe script `/tmp/termium-capture-unit1-measurement/codex-trace.cjs`; outputs under the same root at `trace/A.json`, `trace/B.json`.

### Paired harness A-B-B-A

Sequential runs in isolated worktrees, order A1 → B1 → B2 → A2: eight configurations each (idle/patch/scroll/canvas × Sixel JPEG / Kitty PNG), 30 s measurement with 5 s warmup, one repeat per suite, drained PTY with no real terminal, matching source fixture/runtime/settings, normal binaries. All 32 runs completed; both `benchmark:compare` comparisons exited 0. Exact commands and exit markers: `/tmp/termium-capture-unit1-measurement/logs/harness-driver.sh` and `exits.txt`. Raw per-run data in `{A1,B1,B2,A2}/result.json`; comparison outputs in `logs/compare-A1-B1.log`, `logs/compare-A2-B2.log`; extracted metrics in `extracted-metrics.{json,txt}`. The table values below come from `codex-summary.json` checked against the result files.

Table values are medians across the two per-run summaries of each revision (A1/A2, B1/B2), not pooled per-frame percentiles. Total CPU is the sum of Node + Chromium + Go CPU percentages, where 100% equals one core; changes are given in percentage points and relative %.

| Scene/renderer | Capture p50 A → B (ms) | Total CPU A → B (% / Δpp / Δ%) | Writes/s A → B |
| --- | ---: | ---: | ---: |
| idle/Sixel JPEG | 36.07 → 35.30 | 44.4 → 40.8 (−3.6 pp, −8.2%) | 0 → 0 |
| idle/Kitty PNG | 37.58 → 36.86 | 50.1 → 46.0 (−4.1 pp, −8.2%) | 0 → 0 |
| patch/Sixel JPEG | 36.09 → 35.30 | 47.6 → 44.0 (−3.6 pp, −7.6%) | 4 → 4 |
| patch/Kitty PNG | 37.93 → 37.09 | 49.8 → 46.6 (−3.2 pp, −6.4%) | 4 → 4 |
| scroll/Sixel JPEG | 34.23 → 33.95 | 78.5 → 74.7 (−3.8 pp, −4.8%) | 20 → 20 |
| scroll/Kitty PNG | 33.98 → 33.71 | 50.1 → 47.1 (−3.1 pp, −6.2%) | 20 → 20 |
| canvas/Sixel JPEG | 65.49 → 65.43 | 137.0 → 134.4 (−2.6 pp, −1.9%) | 15.1 → 15.1 |
| canvas/Kitty PNG | 65.53 → 65.42 | 56.0 → 55.2 (−0.7 pp, −1.3%) | 15.0 → 15.4 |

### Results and limitations

- CPU: total CPU is lower on B in both pair orders across all eight configurations; the Node (server) component is lower in all sixteen paired comparisons as well (smallest delta 0.035 pp, canvas/Kitty A2→B2). This is a measured CPU benefit, not an FPS claim.
- Median capture p50 improves on idle and patch (and by ~0.3 ms on scroll); observed canvas p50 summary differences were below 0.2 ms, but these runs do not establish equivalence. No visible-FPS gain is demonstrated and no universal no-slowdown claim is made.
- Tails are mixed: patch/Sixel capture p95 rose +0.610 ms (A1→B1) and +0.185 ms (A2→B2); patch/Kitty rose +0.024/+3.814 ms, while its frame-age p95 improved ~6–7 ms in both pairs. Two runs per revision provide only limited between-run evidence — not proof of neutrality; these tails are recorded as mixed, not dismissed as noise, and the tail-latency tradeoff remains open for acceptance.
- Write rates are largely unchanged: patch is fixture-limited at 4 changes/s, and capture rate is scheduler-capped near 24 captures/s. Writes/s are not visible FPS. RSS results are mixed: server canvas/Kitty peaked at 122.5 MiB (A1) vs 145.4 (B1), and 139.8 (B2) vs 143.4 (A2); these short runs establish neither a leak nor a memory reduction.
- No error counters are reported in this record; that absence does not imply recovery paths never fired. Concurrent user workloads were recorded around each suite, but load averages are context, not causal evidence, and no environmental cause is inferred for any delta.

This stage completes local evidence and documentation only — it is not approval to merge. Remaining before landing: tail-latency acceptance/targeted qualification, cross-platform CI, integration with current main, then the decision to land. Unit 2 (viewport caching) and Unit 3 (metadata redesign) remain untouched; their backlog items stay open above.

### Short follow-up on ea (2026-09-27)

At the user's suggestion, repeated only the patch workload on `ea` (AMD Ryzen AI 9 HX 370, native Linux amd64), with the same A = `92f8e2f` and B = `7a649f3`. Used isolated temporary checkouts, copied the previously validated normal client/server builds and dependencies, and verified client and `browser-controls.js` hashes against the source-machine artifacts. Both revisions used the same Chromium build as the earlier experiment and ea's Node runtime. This was a separate within-machine comparison, not a comparison of absolute timings between machines; no rebuild or full correctness-suite rerun was performed on ea.

Sequential A1 → B1 → B2 → A2, patch only, both default renderers/formats, 30 s measurement + 5 s warmup, one repeat per suite: eight measurements in about five minutes, excluding setup. All four suites and both compatibility comparisons exited 0; all results were complete with no reported error counters. Existing checkout and user workloads were untouched. Load averages recorded before suites ranged from 0.10 to 0.40; this is context, not proof of an otherwise idle machine or an explanation of individual timings.

Values below are medians across two per-run summaries, not pooled frame percentiles. CPU sums Node, Chromium and Go percentages (100% = one core).

| Patch renderer | Capture p50 A → B (ms) | Capture p95 A → B (ms) | Total CPU A → B | Writes/s A → B |
| --- | ---: | ---: | ---: | ---: |
| Sixel/JPEG | 37.44 → 37.13 | 54.97 → 53.41 | 48.88% → 47.00% | 4 → 4 |
| Kitty/PNG | 39.84 → 38.83 | 52.87 → 52.47 | 51.88% → 48.31% | 4 → 4 |

Combined CPU decreased in both pair orders (summary reductions about 3.8% for Sixel and 6.9% for Kitty, relative). Sixel capture p95 improved by 1.512 and 1.605 ms; Kitty p95 changed by +2.192 and −3.002 ms. Other tails remain mixed: Sixel capture p99 changed by −5.222/+1.100 ms and frame-age p95 by −4.730/+3.252 ms; Kitty p99 changed by +1.584/−1.329 ms and frame-age p95 by +1.841/−0.601 ms. These short runs do not establish equivalence, but the earlier patch p95 increases did not reproduce consistently on this second machine. The combined evidence supports a CPU benefit with largely unchanged delivery; it does not support claiming every latency metric improves.

No further benchmark repetitions are planned for this unit before the next review/CI checkpoint. This is bounded additional performance qualification, not merge approval or native macOS coverage. The next gates remain cross-platform CI and review against current main, with the documented mixed tails considered in acceptance. Unit 2 and Unit 3 remain unstarted.

Raw evidence is preserved on ea under `/tmp/termium-unit1-ea-Lc3622/` and copied locally to `/tmp/termium-unit1-ea-results/`: `{A1,B1,B2,A2}/result.json` and per-run artifacts, suite logs, `driver.sh`, `conditions.log`, and `exits.txt`. Local analysis adds `codex-summary.json`, `compare-A1-B1.txt`, and `compare-A2-B2.txt`. The benchmark ran directly from the prebuilt production harness after setup, with `--scenes patch --renderer both --duration 30s --warmup 5s --repeats 1`; each run used a fresh output directory.

## Implemented: optimization 1 — remove per-frame tab/history polling

Recorded 2026-09-27. Candidate branch `capture-metadata-fast-path`, based on `129dfc0`: server/test commit `0fcf1ae`, client/test commit `05d856c`. This stacks on the locally validated capture-session reuse change; neither stack is published or merged under this task. Functional validation is green; **Kitty performance acceptance remains open**. No native macOS validation or new CI run is claimed.

### Change and behavior

[BrowserSession.capture](server/src/browser-session.ts) uses existing `records`/`selected` state instead of querying the browser around every screenshot. Cold start or a missing/closed selection still uses discovery. Local document-generation observation advances the epoch, and the chosen tab/epoch is frozen before asynchronous work. A transition may deliver a briefly stale image; it cannot be relabelled with a later selection's epoch. Viewport application, capture cancellation, queue limits, and fresh input-target validation remain intact.

Screenshots omit the optional `state` field. The dedicated client already calls GetBrowserState independently on a 250 ms schedule, including while images are unchanged or capture is paused. Commands and input still reconcile immediately. This avoids new server timers, a second polling loop, a metadata cache, or protocol fields. The schedule is not a hard convergence guarantee when RPCs are slow/failing; a bare capture-only caller must explicitly refresh state to discover an external live-tab selection. This dedicated-client contract is deliberate. [KeyboardHandler](client/keyboard.go) also fences older-started polls after accepted input acknowledgements, protecting same-generation metadata as navigation acknowledgements already did.

### Review and validation

Coordinator source review and two focused peer reviews found no confirmed production defects. Focused server and client ordering tests, the new actual-client metadata integration, typecheck, and the full `npm test` suite all passed at `05d856c` on Linux/WSL2; the normal client was restored. The new PTY test changes only document.title, observes the real toolbar updating without test-side GetBrowserState calls or input acknowledgements, and verifies decoded screenshot pixels remained identical. It passed both alone and in the full race-enabled integration run. Evidence: `/tmp/termium-opt1-validation-05d856c/`; peer reports with coordinator qualifications: `/tmp/termium-opt1-review-{a,b}.md`.

### Command trace, separately from timing

On ea, four warmed PNG and four warmed JPEG samples per revision measured **7 → 2 commands per capture**. The remaining commands are Emulation.setDeviceMetricsOverride and Page.captureScreenshot; skipping unchanged viewport updates is optimization 2, deliberately deferred. Format, dimensions and static frame equality were checked.

A separate controlled workload of **24 captures plus four explicit state reads** used **180 → 60 total CDP commands**, for each format. Target.getTargets fell from 104 to 8, history reads from 28 to 4; the 24 viewport and 24 screenshot commands remained. These are counts for that workload, **not commands/second**. This confirms work was removed rather than merely relocated. Production-harness timing below includes the real client's independent metadata polling.

### Short paired comparison on ea

A = `7a649f3` (runtime, client, protocol and benchmark sources identical to base `129dfc0`; the intervening commits only document prior measurements), B = `05d856c`. Used separate clean checkouts and corresponding prebuilt normal binaries/server output on ea (Ryzen AI 9 HX 370, Linux amd64), with matching runtime, Chromium, fixtures and options within the comparison. No unrelated processes were stopped. Recorded pre-suite load averages ranged from 0.32 to 0.75; no cause is inferred from them.

Sequential A1 → B1 → B2 → A2; idle and patch with both default renderers/formats (Sixel/JPEG, Kitty/PNG), 15 s measured + 5 s warmup, one repeat per suite. All 16 configurations completed; all four suites and both compatibility comparisons exited 0, with no reported error counters. This was a drained-PTY comparison, not real-terminal or visible-FPS measurement. No scroll/canvas timing claim is made for this change.

The table gives medians across the two per-run summaries of each revision, not pooled frame percentiles. Total CPU sums Node, Chromium and Go, with 100% representing one core.

| Workload / renderer | Capture p50 A → B (ms) | Capture p95 A → B (ms) | Total CPU A → B (% of one core) | Captures/s A → B | Writes/s A → B |
| --- | ---: | ---: | ---: | ---: | ---: |
| idle / sixel | 37.39 → 35.33 | 46.24 → 43.78 | 44.77 → 41.30 | 23.17 → 23.50 | 0.00 → 0.00 |
| idle / kitty | 40.53 → 41.69 | 48.56 → 49.17 | 50.34 → 50.93 | 22.40 → 22.23 | 0.00 → 0.00 |
| patch / sixel | 38.27 → 36.27 | 54.72 → 53.10 | 48.90 → 45.81 | 22.43 → 22.87 | 4.00 → 4.00 |
| patch / kitty | 39.15 → 39.46 | 53.21 → 54.41 | 49.38 → 51.24 | 22.17 → 22.00 | 4.00 → 4.00 |

Sixel capture p50 and combined CPU improved in both pair orders for both workloads. The summary medians are about 2 ms lower and combined CPU about 6–8% lower. Tails were not uniformly better: patch/Sixel capture p95 changed by +0.464/−3.690 ms across the pairs, and frame-age p95 by +1.952/−8.117 ms.

Kitty is inconclusive in this short sample. Idle capture p50 changed by +6.957/−4.646 ms; total CPU by +7.9%/−5.1% relative across the pairs. Patch p50 changed by +0.625/−0.012 ms; total CPU by +7.8%/−0.1%. Kitty patch p95 changed by +3.932/−1.519 ms, while frame-age p95 changed by +0.724/−0.211 ms. The summary values therefore do not establish a Kitty benefit or absence of slowdown. RSS is recorded per process in raw results; no memory reduction or leak conclusion is drawn. Write rates stayed at the workload limits (idle zero, patch four per second), which is not a throughput-capacity claim.

**Disposition:** implementation and functional validation complete; retain the candidate for review, but do not claim universal speedup or mark the no-regression acceptance gate passed. Stop further benchmarking at this boundary. Before landing, resolve Kitty performance acceptance and run cross-platform CI against the chosen combined branch. Optimizations 2–4 remain unstarted.

Raw trace/timing evidence lives on ea at `/tmp/termium-opt1-ea-56TbT2/`, copied locally to `/tmp/termium-opt1-ea-results/`: `trace-{A,B}.json`, `trace.cjs`, `{A1,B1,B2,A2}/result.json` and per-run artifacts, `driver.sh`, `conditions.log`, `exits.txt`, and suite logs. Local analysis adds `codex-summary.json`, `table.md`, and `compare-A1-B1.txt` / `compare-A2-B2.txt`. The production harness ran with `--scenes idle,patch --renderer both --duration 15s --warmup 5s --repeats 1`, using fresh outputs each time. Review raw results before drawing stronger conclusions.

## Implemented: optimization 2 — skip unchanged viewport updates

Recorded 2026-09-27. Source commit `41f94b0` on branch `viewport-cache`, base `e663a5e` (the PR #22 merge). Validated locally on WSL2 and measured on native Linux ea; no native macOS validation or new CI run is claimed for this change, and its own cross-platform CI remains pending.

### Change and behavior

[BrowserControls](server/src/browser-controls.ts) now tracks successfully applied dimensions per control CDP session in a `WeakMap`, kept distinct from the session-wide desired viewport. A warmed capture whose desired size matches the cached applied size sends no `Emulation.setDeviceMetricsOverride`; [BrowserSession.capture](server/src/browser-session.ts) still propagates the desired viewport to the active tab's controls before every capture, so resize, a tab selected after a resize, and new or replaced targets each receive one initial override at unchanged dimensions. A failed apply deletes the cached entry first — failure leaves the browser's size uncertain, so the next attempt resends; initialization failure detaches its session and rethrows while staying retryable, and cleanup of an obsolete initialization is identity-guarded so a late rejection cannot clear its replacement. Capture-session recreation does not disturb the control-session cache because the key is the control session, not the capture session.

### Review and validation

Direct source review plus the coordinator's independent review found no confirmed production defects (`/tmp/termium-opt2-review.md`, including the coordinator adjudication). Seven viewport unit tests cover caching per control session, one initial override for a replaced target at unchanged dimensions, failed-resize retry with invalidation of the previous cached size, failed-initialization detach and recovery, rejected-attach non-poisoning, a late obsolete-init failure not clearing its replacement, and capture-session recreation leaving the control cache intact. Real-browser integration covers resize/tab/reload geometry: the new test resizes, discovers an external tab via state(), propagates to an old tab without explicit resize, and survives reload (1144 ms). Full `npm test` and typecheck passed; the normal executable was restored afterwards. All validation steps exited 0 with no retries: `/tmp/termium-opt2-validation-41f94b0/`. A mutation check in a disposable copy removed the cache invalidation on failure and produced the expected regression failure ("the applied size was not resent after a failure"): `/tmp/termium-opt2-mutation-result.json`.

### Production trace: two commands down to one per warmed capture

On ea, four warmed PNG and four warmed JPEG samples per revision at 640 × 360 after five warmup captures. Baseline A = `e663a5e` issued **2 commands** per sample (`Emulation.setDeviceMetricsOverride` + `Page.captureScreenshot`); candidate B = `41f94b0` issued **1** — `Page.captureScreenshot` only, with valid geometry and consecutive static buffers verified identical within each revision. The trace stores byte sizes, not hashes or bytes, so no cross-role content identity is claimed; PNG and JPEG sizes matched across roles (17,847 / 11,798). A controlled workload of 24 captures plus four explicit state reads used **60 → 36 commands per format**. These are command counts for that workload, not rates or measured latency.

### Paired ABBA comparison on ea

A = `e663a5e`, B = `41f94b0` on ea (AMD Ryzen AI 9 HX 370, Linux amd64), with matching fixtures, toolchain, runtime and options; idle and patch with both default renderers/formats (Sixel/JPEG, Kitty/PNG), 15 s measured + 5 s warmup, one repeat per suite — 16 configurations total on a drained PTY. Binaries were freshly built locally and transferred; the host package manager and user workloads were untouched, and the shared existing benchmark harness is unchanged by this source diff. All four suites exited 0 with no reported error counters; environment metadata is in the raw result JSON and conditions in `conditions.log`. No load-based causal claims are made.

Table values are medians of the two per-run summaries of each revision (A1/A2, B1/B2), not pooled frame percentiles. CPU units are percentages of one core, summed Node + Chromium + Go.

| Workload / renderer | Capture p50 A → B (ms) | Capture p95 A → B (ms) | Summed CPU A → B (% of one core) |
| --- | ---: | ---: | ---: |
| idle / Sixel JPEG | 35.75 → 35.37 | 43.95 → 43.91 | 42.12 → 41.50 |
| idle / Kitty PNG | 38.12 → 37.34 | 47.82 → 46.28 | 48.51 → 48.33 |
| patch / Sixel JPEG | 36.37 → 35.69 | 53.81 → 53.98 | 45.13 → 44.51 |
| patch / Kitty PNG | 38.30 → 37.34 | 51.92 → 52.89 | 47.81 → 47.53 |

### Results and limitations

- Small p50 improvements: the summary medians improve by about 0.4–1.0 ms; Kitty capture p50 improved in both pair orders (all four kitty pairs negative).
- Summed CPU directions are mixed in every workload — one pair up, one down per workload. These summary reductions do not establish a consistent CPU benefit, and no stable CPU reduction is claimed.
- Patch/Kitty tail: capture p95 increased +0.758/+1.172 ms in both orders and Node CPU rose +0.730/+0.729 percentage points; its frame-age p95 decreased 0.283/1.123 ms and frame-age p99 improved in both orders. Sixel tails are mixed.
- Capture p99: Kitty improved in both pair orders (idle −1.196/−1.322 ms; patch −0.334/−0.758 ms); Sixel capture p99 is mixed (idle −1.21/+0.857; patch −4.387/+2.006).
- Capture rates ranged 22.5–23.6/s, near the ~24 scheduler cap and not visible FPS: Kitty capture rate increased in both pair orders on idle and patch, while Sixel rates were flat/mixed; no blanket throughput claim is made from the overlapping range. Patch writes stay at 4/s due to the workload; idle stays at 0.
- This is not a universal speedup, not proven no-slowdown, and not a visible-FPS gain (drained PTY). These measurements do not establish which phase dominates capture time; no causal attribution is made.
- Recommendation: record this bounded tradeoff for the next review/CI checkpoint rather than launching more timing runs.

Raw evidence is preserved on ea under `/tmp/termium-opt2-ea-WCNx20/` and copied locally to `/tmp/termium-opt2-ea-results/`: `{A1,B1,B2,A2}/result.json` and per-run artifacts, `trace-{A,B}.json`, compare outputs, `qwen-summary.json` / `coordinator-summary.json`, `conditions.log`, and `exits.txt`. The production harness ran with `--scenes idle,patch --renderer both --duration 15s --warmup 5s --repeats 1`, using fresh outputs each time.

## Implemented: four reusable frame arenas

**2026-09-27. Source `3a5525a`, parent `bb76b52`, branch `frame-arena-pool`; final source at `42ffd50` is byte-identical to `3a5525a`.** This is a separate follow-up to optimization 3; the earlier palette-index reuse candidate remains backed out. Four rotating byte arenas now hold decoded RGBA pixels and final terminal payloads. Each starts with 10 MiB on first use, doubles when required, and resets its allocation cursor without clearing reused bytes. All exposed pixels and payload bytes are overwritten before publication. Growth retains capacity; it does not shrink after a resize.

There is still **one pending display frame**, replaced by newer work. Explicit leases cover the preparation worker's previous frame, pending publication and the UI's current frame (including terminal writes and later redraws). A slot is reusable only after its last owner releases it. If all four slots are occupied, preparation drops the incoming frame instead of waiting or allocating a fifth slot. Unchanged pixels/metadata reuse share a lease. Cancellation, pause, supersession, resize, failure and shutdown release their respective owners. Sixel cached band strings retain independent storage across frames; they are committed only after composition succeeds. No `unsafe`, finalizers or Go experimental arena API is involved.

Sixel composition writes directly into frame storage, removing the intermediate full-document string/copy. Kitty framing also writes into frame storage. Image decoding, band quantization and the encoder's many small allocations remain unchanged: this is not an allocation-free renderer.

### Review and validation

An independent source review found no concrete defects in ownership, output bounds, error/shutdown paths or band cache lifetimes. Focused tests passed with `-race`, including a blocked terminal write while other frames rotate, pending supersession, exhausted-pool recovery, metadata/generation reuse, failed decoding/composition, cancellation/pause, transparent pixels, dimension changes, overlays and resize. Deliberately removing pending-frame release caused the test to exhaust the pool; removing allocation slice-cap bounds caused the neighboring-slice sentinel test to fail. These mutations ran through temporary Go overlays, not edits to the tested production tree.

`npm run typecheck` and one full `npm test` invocation passed: build, lint, server tests, Go race/shuffle tests, real Chromium/Go integration and website tests. The previously intermittent `TestShutdownDuringBrowserLaunch` passed in this run; its earlier failure is not erased or explained by this result. After strengthening the final slice-cap test assertion, the focused arena race suite passed again. The normal non-race client was restored and rebuilt at the source commit. Validation is Linux/WSL2 only; this change and optimization 2 still need their own native macOS/Linux CI and package qualification before release. No PR, push or release was performed.

### Bounded comparison on ea

A = `bb76b52` (runtime identical to `3ff7241`), B = `3a5525a`. Both executables were built with Go 1.27.1-X:nodwarf5 and ran sequentially on ea, native Linux amd64/Ryzen AI 9 HX 370. Server build, browser, Node, lockfile, fixture and dimensions match. The benchmark source was identical; baseline-only test shims make `release()` a no-op and `close()` clear `last`, preserving the old heap-owned benchmark behavior. GC controls were unset. No tests, builds or profiles overlapped timing, and no user workload was killed.

Frozen 1280 × 720 production-fixture captures were replayed without Chromium running during timing. A1 → B1 → B2 → A2, three samples per invocation; commands and input hashes are preserved with the artifacts. These measurements force full preparation and exclude RPC, browser capture, asynchronous output and previous-frame retention. Initial arena allocation is included and amortized over each benchmark's iteration count; B/op is not a warmed steady-state allocation claim.

| Replay workload / pair | Median ms/op A → B | Median bytes/op A → B |
| --- | ---: | ---: |
| sixel JPEG A1/B1 | 51.866 → 49.366 | 22,242,136 → 11,491,930 |
| sixel JPEG A2/B2 | 50.050 → 48.124 | 22,242,130 → 11,328,615 |
| kitty PNG A1/B1 | 0.305 → 0.196 | 603,441 → 12,379 |
| kitty PNG A2/B2 | 0.320 → 0.195 | 603,442 → 13,316 |

The production harness then ran canvas with default capture format/palette, each renderer separately in A1 → B1 → B2 → A2 order, 5 s warmup and 15 s measurement. It includes capture, RPC, preparation, pacing and drained-PTY output. Writes/s are **not visible-terminal FPS**.

```sh
benchmark --scenes canvas --renderer <sixel|kitty> --duration 15s --warmup 5s --repeats 1 --label <run> --out <fresh-directory>
```

| Renderer / pair | Prepare p50 A → B (ms) | Prepare p95 A → B (ms) | Writes/s A → B | Summed CPU A → B (% one core) |
| --- | ---: | ---: | ---: | ---: |
| sixel A1/B1 | 45.153 → 43.150 | 60.180 → 57.930 | 15.333 → 15.333 | 131.291 → 124.783 |
| sixel A2/B2 | 44.142 → 42.241 | 58.009 → 54.401 | 15.467 → 16.800 | 129.552 → 125.999 |
| kitty A1/B1 | 0.523 → 0.494 | 0.924 → 0.643 | 15.600 → 15.533 | 55.073 → 56.824 |
| kitty A2/B2 | 0.516 → 0.509 | 0.913 → 0.618 | 15.800 → 15.400 | 55.524 → 56.132 |

| Renderer / pair | Allocated MB/prepared frame A → B | GC cycles/prepared frame A → B | Sampled client peak RSS MiB A → B |
| --- | ---: | ---: | ---: |
| sixel A1/B1 | 24.688 → 10.580 | 1.039 → 0.200 | 77.64 → 132.52 |
| sixel A2/B2 | 24.472 → 10.538 | 1.017 → 0.198 | 78.86 → 128.50 |
| kitty A1/B1 | 1.676 → 0.588 | 0.214 → 0.013 | 41.49 → 88.92 |
| kitty A2/B2 | 1.471 → 0.579 | 0.173 → 0.013 | 41.71 → 88.87 |

Sixel preparation p50 improved 4.3–4.4% in both live pairings, allocated bytes per preparation fell about 57%, and client CPU time per written frame fell from 52.6–54.0 ms to 47.5–50.0 ms. Sixel output rate was equal in one pairing and 8.6% higher in the other. These are benefits measured on Sixel; it does not establish a universal FPS gain or prove that GC caused the speed difference. The tradeoff is higher retained memory: roughly 50–55 MiB more sampled client RSS. The pool can retain more after larger frames.

Kitty initially reduced allocated bytes per preparation by 61–65%, improved preparation tails and client CPU per frame, but writes/s fell 0.4% and 2.5% while summed browser/server/client CPU rose slightly. A single bounded Kitty-only confirmation was therefore run; its results follow below. All original 16 invocations passed without retries, reported no error/arena-exhaustion counters, and retained matching fixture/server hashes.

### Kitty confirmation and shared-path decision

One further Kitty-only A3 → B3 → B4 → A4 sequence used the same binaries/options and fresh artifact directories on ea. All four invocations passed without retries or error/exhaustion counters.

| Pair | Prepare p50 A → B (ms) | Prepare p95 A → B (ms) | Writes/s A → B | Client CPU ms/write A → B |
| --- | ---: | ---: | ---: | ---: |
| A3/B3 | 0.433 → 0.490 | 0.814 → 0.617 | 16.000 → 15.800 | 2.833 → 2.489 |
| A4/B4 | 0.528 → 0.510 | 0.914 → 0.653 | 15.667 → 15.400 | 3.021 → 2.597 |

Across all four Kitty pairings, B wrote 1–6 fewer frames per 15 s (0.4–2.5% lower throughput). Every captured frame was prepared and written; there were no discarded frames or exhausted arenas. Median capture latency was approximately 65 ms versus roughly 0.5 ms preparation. Preparation tails and client CPU time per frame improved in every pairing, while browser/server CPU and capture timing varied. This does **not** establish the cause of the throughput difference. Browser scheduling, pipeline timing, GC and cache behavior are hypotheses, not findings; no profiler or causal experiment was run here. There is no demonstrated Kitty end-to-end speedup. Retained Kitty client RSS rose from roughly 41–44 MiB to 89–101 MiB; ASCII performance was not measured.

A temporary local renderer-specific rollback (`542595c`) was independently reviewed and passed another full suite, but the user explicitly rejected separate allocation paths. It was reverted by `42ffd50` before any further timing or publication. `git diff 3a5525a 42ffd50 -- client` is empty. **The retained implementation uses the same arena ownership/allocation path for Sixel, Kitty and ASCII graphics.** Native renderer encoding remains format-specific. The final normal executable was rebuilt. This preserves the measured shared implementation, including its unresolved small Kitty throughput difference; it does not relabel those measurements as a win. The user accepted retaining the unified path and treating this small difference as a non-blocking follow-up. Future real-terminal measurements may investigate it; no additional profiling or timing rounds are required for this unit, and no universal speedup is claimed.

Evidence: `/tmp/termium-arena-validation/` (review, focused races, full-suite logs, expected-failure mutations and normal builds); `/tmp/termium-arena-results/` (raw replay outputs, all production reports, conditions, executable/source hashes, benchmark source and baseline compatibility shim, driver scripts, derived `summary.json` and `confirmation-summary.json`). Remote originals: `ea:/tmp/termium-arena-ea-8d37H1/`. No performance run was on the busy development workstation. Measurements used drained output, not a real graphical terminal. All twenty timing invocations exited 0; no further rounds or profiler runs were performed.

**Stop boundary:** this arena unit is implemented, reviewed, validated and measured locally/on ea; nothing is pushed. Remaining release work is native Linux/macOS CI and package/installer-update qualification of the chosen revision, terminal smoke tests (including WSL2), and follow-up on the previously intermittent shutdown test. Optimization 4, encoder small-allocation work, and broader profiling remain deferred; they are not automatic release blockers.

## Investigated: optimization 3 — fixed-palette scratch reuse (rejected)

**2026-09-27. Decision: do not ship this candidate.** Profiling and a bounded experiment are complete. The allocation reduction did not meet the user's no-repeatable-slowdown acceptance criterion: the production canvas workload became slower in both A/B pairings. Source `2461a11` and regression tests `944b384` are preserved in local history; `848c8ff` backs both out. Runtime and tests after the backout match parent `3ff7241` exactly. No PR, push or release was created. Optimization 4 remains deferred; this result is not a reason to continue experimenting indefinitely before a release.

### Profiles and candidate

A dedicated test executable built with Go 1.27.1-X:nodwarf5 ran on ea (native Linux amd64, Ryzen AI 9 HX 370). CPU and allocation profiles were separate: CPU used ordinary memory sampling; allocation profiles used `-test.memprofilerate=1`, whose instrumented timings are not baseline measurements. Both profiled cases are the existing synthetic 1920 × 1081 `BenchmarkSixelBandChanges` workloads, excluding screenshot capture, image decoding and terminal rendering.

- One-pixel CPU: `memeqbody` accounts for 76.59% of sampled CPU, mostly comparing unchanged bands. Scratch allocation is not the dominant cost in this case.
- All-pixels CPU: `writePixelData` is 30.57% flat / 51.75% cumulative, `cachedDraw` 23.41% / 41.56%. Pixel accessor/bounds work contributes beneath those callers. These process-wide sample shares include startup; cumulative percentages overlap and must not be added.
- All-pixels allocated bytes: `image.NewPaletted` is 78.96% of the process profile, motivating retained palette-index storage. The 15.52% attributed to `image.NewRGBA` is benchmark fixture setup/calibration before `ResetTimer`, not repeated frame preparation.
- Allocation objects differ: `writePixelData` is 36.46%, `RGBA.SubImage` 20.83%, `NewPaletted` 20.83%. Removing the largest byte allocation is not the same as removing most allocation objects or most CPU work.

The candidate retained one encoder-owned high-water palette-index buffer for origin-zero WebSafe/Plan9 images. It updated the active length, stride, bounds and palette; non-zero origins kept fresh allocation to preserve source-point-zero clipping behavior. Adaptive quantization and borrowed paletted inputs were unchanged. Neither CPU pixel loop was rewritten.

### Correctness and validation record

Qwen's independent production-source review found no defects. New tests compared reused/fresh output for both entry points, palette changes, full/short bands, resizing, padded source stride, dithering, transparency and adaptive interludes. Independent known-pixel decoding covered WebSafe and Plan9 white; input-ownership checks protected borrowed paletted images. Four temporary source mutations were rejected by those tests: missing palette update, missing stride update, reuse at non-zero origins, and adoption of caller-owned storage.

Focused Sixel tests passed. Typecheck initially failed because generated TypeScript protocol files were absent in the isolated worktree; after `npm run build:proto`, it passed. Full `npm test` passed build/lint, 23 server tests, Go race/shuffle tests and five Node integration tests, then **failed** Go integration `TestShutdownDuringBrowserLaunch`: cleanup reported an owned browser process still present after shutdown. That test starts the Node server and a blocked browser executable, without Sixel encoding. The same test passed once on the unchanged parent. One candidate integration-suite rerun passed, and four website tests passed separately. This does not establish the cause or erase the initial failure; shutdown process cleanup remains a release follow-up. Normal client executables were restored after testing and again after backout. Validation was Linux/WSL2; this candidate did not run native macOS CI.

### Matched A–B–B–A measurements

A = `3ff7241`, B = `944b384`. Same compiler, benchmark source, browser, Node, lockfile, server build, fixture and options; fresh isolated ea checkouts. No tests/builds overlapped timing. Microbenchmarks ran A1 → B1 → B2 → A2, each with five samples:

```sh
./<role>-test -test.run '^$' -test.bench '^BenchmarkSixelBandChanges$' -test.benchmem -test.count=5
```

| Workload / pair | Median ns/op A → B | Change | Median B/op A → B | Allocations/op A → B |
| --- | ---: | ---: | ---: | ---: |
| one-pixel A1/B1 | 208,552 → 210,059 | +0.72% | 61,052 → 58,908 | 28 → 26 |
| one-pixel A2/B2 | 225,698 → 214,071 | −5.15% | 61,052 → 58,908 | 28 → 26 |
| all-pixels A1/B1 | 13,985,892 → 13,592,071 | −2.82% | 2,337,387 → 106,123 | 2,723 → 2,361 |
| all-pixels A2/B2 | 14,243,103 → 13,595,189 | −4.55% | 2,337,390 → 106,123 | 2,723 → 2,361 |

This is approximately 95.5% fewer allocated bytes in the synthetic all-pixels encoder workload, **not** 95.5% less application memory. One-pixel timing is mixed. Full sample ranges and raw values are preserved in the evidence.

A separate production-harness A1 → B1 → B2 → A2 sequence used canvas/Sixel with default JPEG capture and WebSafe palette, 15 s measurement, 5 s warmup, one repeat, and a drained PTY:

```sh
benchmark --scenes canvas --renderer sixel --duration 15s --warmup 5s --repeats 1 --label opt3-<run> --out <fresh-directory>
```

| Pair | Prepare p50 A → B (ms) | Prepare p95 A → B (ms) | Frame-age p95 A → B (ms) | Writes/s A → B | Summed CPU A → B (% of one core) |
| --- | ---: | ---: | ---: | ---: | ---: |
| A1/B1 | 43.29 → 51.26 | 55.81 → 65.21 | 123.85 → 132.68 | 16.33 → 14.53 | 130.48 → 133.38 |
| A2/B2 | 48.02 → 55.56 | 63.79 → 67.76 | 126.13 → 130.73 | 15.40 → 13.80 | 131.60 → 132.99 |

All eight micro/production invocations exited 0, without timing retries; production reported no error counters. The synthetic win did not transfer to this busier workload: preparation p50 worsened 15.7–18.4%, output rate fell about 10–11%, and frame-age tails and CPU rose in both pairings. These short runs do not establish the mechanism or eliminate environmental effects. No GC, machine-load or cache-locality explanation is claimed. Nevertheless, the consistent adverse production result is sufficient to reject this candidate for release under the agreed bar. No visible-terminal FPS claim is made from drained-PTY writes.

### Follow-up: identical canvas JPEG replay on ea

Later on 2026-09-27, at the user's request, the same A/B executables replayed **one identical frozen production-fixture JPEG** on ea. The earlier live experiment also ran on ea; moving from the local workstation is not the difference between these experiments. The production HTML's RAF callbacks were queued, its readiness and one animation callback executed, then animation left frozen. Capture happened once at 1280 × 720, JPEG quality 60; Chromium was closed before timing. Input SHA-256: `a6492fb48b1f3317eaeba4f73ebb697088861d8109b91570104be5b026423e05` (403,209 bytes).

The existing `BenchmarkRendererPreparation/canvas/jpeg.jpg/sixel` ran A1 → B1 → B2 → A2, five samples each, using the original matched Go executables and default GC settings. This benchmark forces full changed-frame preparation with `p.last = nil`, while retaining encoder/compression scratch. It includes JPEG decoding, RGBA conversion and Sixel preparation; it omits the live browser, RPC, asynchronous terminal output and previous-frame retention. No profiler or GC trace was run in this follow-up.

| Run | Median ms/op | Range ms/op | Median allocated B/op | Median allocations/op |
| --- | ---: | ---: | ---: | ---: |
| A1 | 50.074 | 49.156–51.665 | 22,242,097 | 1,405,427 |
| B1 | 44.097 | 43.259–44.844 | 21,239,253 | 1,405,186 |
| B2 | 43.182 | 42.882–48.542 | 21,237,450 | 1,405,186 |
| A2 | 50.852 | 49.831–51.003 | 22,244,784 | 1,405,427 |

The candidate was **11.9% / 15.1% faster** in the two replay pairings, with approximately 4.5% fewer allocated bytes and 241 fewer allocation objects per operation. All output lengths were 2,984,411 bytes; this metric is length equality, not a byte-for-byte output comparison. All four runs passed without skips or retries. Raw data/capture hashes/conditions: `/tmp/termium-opt3-replay-results/`; remote binaries and evidence: `/tmp/termium-opt3-replay-ea-qr4hnJ/`.

This contradicts a general claim that buffer reuse inherently slows preparation. It does not explain the earlier live-pipeline regression: retained-frame lifetimes, concurrency/output work, live samples and environmental differences are still unisolated. The candidate remains backed out pending resolution; neither result is discarded, and no machine-load or GC cause is established. Earlier production memory counters recorded roughly one GC cycle and 0.06 ms accumulated GC pause per delivered frame in both versions. That does not establish expensive pauses as the source of the extra 7.5–8 ms, but does not measure all concurrent GC or allocator CPU work either. An arena is a possible later experiment only after identifying relevant allocation/GC costs; none was implemented here.

### Evidence and remaining work

- Profiles, exact profiled executable, compiler provenance, baseline and pprof tables: `/tmp/termium-opt3-evidence/`; ea profiling copy `/tmp/termium-opt3-ea-stageA/`. The first driver used incorrect standalone-test flags and exited 2 before measuring; the corrected `-test.*` commands succeeded. Both statuses are retained.
- Validation, original failure, retry, mutation evidence and independent source review: `/tmp/termium-opt3-validation/`.
- Comparisons: `/tmp/termium-opt3-comparison/`, including raw `micro-{A1,B1,B2,A2}.txt`, `canvas-*/result.json`, conditions, exits and `coordinator-summary.json`. Exact A/B test executables and runtime checkouts remain on ea at `/tmp/termium-opt3-ea-n3IylT/`.
- Local `/tmp` paths are evidence from this investigation, not permanent release assets. Preserve them before cleanup; the decision and key measurements are recorded here.

Release work now takes priority: resolve the intermittent shutdown-cleanup failure, obtain native test/package CI for optimization 2, and qualify install/update of the chosen release archives on advertised platforms. The release candidate keeps optimization 2; it does not include scratch-buffer reuse. A future optimization investigation should profile a representative busy frame through the production preparation path before selecting another change. Pixel conversion/writing and unchanged-band comparison remain candidates, not promised improvements.

## Historical investigation and backlog

The remaining sections preserve earlier measurements and proposals. References to the old fixed ticker, debug screenshot writes, repeated full-image transmission, or dialog compositing describe the pre-refactor implementation; consult the implemented record above and current code before treating them as open work.

## Performance Investigation Results (COMPLETED)

### Sixel Encoding Performance Crisis

**Problem**: go-sixel library (github.com/mattn/go-sixel) taking 3+ seconds per frame at 1870x1020

**Tested Solutions & Results**:
1. ✅ **Screenshot flag**: Removed disk writes - helped overall performance
2. ✅ **JPEG instead of PNG**: Reduced server encoding time  
3. ❌ **Encoder reuse**: Cached encoder - NO IMPROVEMENT (still 3+ seconds)
4. ❌ **Disable dithering**: Set Dither=false - NO IMPROVEMENT
5. ⚠️ **Resolution reduction**: 980x500 - Improved to ~850ms (still 25x too slow)
6. ❌ **img2sixel subprocess**: ~110-150ms but display corruption, not viable

**Root Cause**: The go-sixel library has fundamental performance issues. At 980x500 (1/4 pixels), it takes 850ms when we need <33ms for 30 FPS. The library is ~25-100x too slow.

**Next Step**: Profile go-sixel with pprof to identify the exact bottleneck, then either:
- Fix the library if it's a simple issue
- Write CGO wrapper around libsixel 
- Switch protocols (Kitty/iTerm2)

## Critical Performance Issues (Current State)

### The Murder Scene
1. **1-second ticker** (client/main.go:427) - Hardcoded 1 FPS cap
2. **Debug screenshots to disk** (client/main.go:467-490) - Writing TWO PNG files per frame
3. **PNG encode/decode overhead** - Full compression/decompression cycle every frame
4. **Excessive buffer allocations** - Creating new RGBA buffers repeatedly

## Quick Wins (Phase 1)

### 1. Add Screenshot Debug Flag
- Add `--save-screenshots` CLI flag
- Only save debug images when flag is set
- Expected impact: 5-10x performance improvement

### 2. Fix Screenshot Interval
```go
// Current: ticker := time.NewTicker(1000 * time.Millisecond)  // 1 FPS
// Target:  ticker := time.NewTicker(33 * time.Millisecond)    // 30 FPS
```
- Consider adaptive timing to prevent queue buildup
- Add frame skipping if processing can't keep up

### 3. Remove Redundant Operations
- Skip clearDrawingArea() except on first draw/resize
- Cache scaled images when dimensions unchanged
- Reuse RGBA buffers instead of allocating new ones

## Network/Protocol Optimizations (Phase 2)

### 4. Replace PNG with Raw Transfer
- Send raw RGBA bytes instead of PNG
- Add light compression (lz4/snappy)
- Implement proper buffering

### 5. Delta/Dirty Rectangle Tracking
- Only send changed regions
- Implement frame diffing
- Add sequence numbers for dropped frames

### 6. Streaming Instead of Polling
- Replace polling with push-based updates
- Consider WebSockets or Server-Sent Events
- Implement backpressure handling

## Rendering Pipeline (Phase 3)

### 7. Bypass tcell for Sixel Viewport
- Direct stdout writing for sixel area
- Keep tcell only for UI controls
- Eliminate double-buffering overhead

### 8. Optimize Sixel Rendering
- Pre-calculate sixel encoding
- Cache sixel output for static regions
- Implement proper cursor management

### 9. Smart Scaling
- Scale on server side if possible
- Use hardware acceleration if available
- Implement multi-resolution support

## Advanced Optimizations (Phase 4)

### 10. Video Encoding
- Use Puppeteer's screencast API
- Implement H.264/VP9 encoding
- Client-side video decoding

### 11. Adaptive Quality
- Detect terminal performance
- Adjust quality/FPS dynamically
- Implement progressive rendering

### 12. Frame Interpolation
- Predict intermediate frames
- Smooth motion during lag
- Implement motion vectors

## Measurement & Profiling

### 13. Add Performance Metrics
- FPS counter
- Frame time histogram
- Network latency tracking
- Terminal render time estimation

### 14. Profiling Points
- Time each pipeline stage
- Identify bottlenecks
- Add debug overlay option

## Terminal-Specific Optimizations

### 15. Windows Terminal Preview Specific
- Test sixel implementation limits
- Find optimal image dimensions
- Determine maximum sustainable FPS

### 16. Multi-Terminal Support
- Detect terminal capabilities
- Fall back gracefully
- Optimize for each terminal type

## Architecture Decisions

### Option A: "Quick & Dirty" (Few hours)
- Items 1-3 above
- Expected result: 10-15 FPS
- Minimal code changes

### Option B: "Smart Streaming" (1-2 days)
- Items 1-6 above
- Expected result: 20-30 FPS
- Moderate complexity

### Option C: "Video Stream" (Several days)
- Items 1-12 above
- Expected result: 30+ FPS
- Significant rework

## Implementation Order (Recommended)

1. **Immediate**: Add screenshot flag (#1) and fix ticker (#2)
2. **Measure**: Add FPS counter (#13) to establish baseline
3. **Quick wins**: Items #3-5 based on measurements
4. **Architecture**: Choose Option A/B/C based on results
5. **Optimize**: Implement chosen path
6. **Polish**: Terminal-specific optimizations

## Sixel Color Caching Optimizations (IN PROGRESS)

### Problem
- Adaptive palette regenerates every frame, causing cache misses
- palette.Index() still taking significant time even with per-frame caching
- Current performance: 400-600ms per frame with adaptive palette

### Proposed Solution: Global Cache with Partial Invalidation
1. **Global persistent cache** - Keep cache across frames instead of recreating
2. **Modulo-based invalidation** - Invalidate pixels where `(x + y*width) % 30 == frame % 30`
3. **Palette stability detection** - Measure how much palette changed between frames
4. **Adaptive invalidation rate**:
   - Stable palette: 1/30th per frame (1 second full refresh at 30 FPS)
   - Large change: 1/10th per frame (333ms full refresh)
   - Gradually return to 1/30th as palette stabilizes

### Implementation Details
- Cache key: RGB color (uint32) -> palette index (uint8)
- Invalidation ensures every pixel refreshes within 1 second at target 30 FPS
- Spatially distributed invalidation prevents visible artifacts
- Never fully clear cache to avoid cold start penalty

### Expected Impact
- Current: 400-600ms per frame (can't hit 2 FPS)
- With 97% cache hits: ~20-30ms per frame (enables 30 FPS)

### Web-Optimized Palettes (High Priority)
Since we're rendering web content, we should optimize for modern web color systems:

1. **Tailwind Palette Set**:
   - Pre-compute 256 or 1024 palettes based on Tailwind CSS colors
   - Each palette optimized for different color combinations
   - Cover common website themes (light/dark, brand colors, etc.)
   - Tailwind uses: 22 color families × 11 shades = ~220 colors that cover most modern UIs

2. **Material Design Palette**:
   - Google's Material Design color system
   - Systematic shades (50, 100, 200...900) 
   - Optimized for UI components

3. **Dynamic Palette Selection**:
   - Analyze page's dominant colors
   - Select best matching pre-computed palette
   - Could store palette library in ~100MB file

This approach would give near-perfect color matching for modern web UIs since websites actually use these exact design systems.

## Multi-Client Browser Control (Future Enhancement)

### The Wild Idea
Support multiple clients connecting to the same browser instance for collaborative browsing or "Twitch Plays Browser" chaos mode.

### Architecture Options

1. **Master/Viewer Mode**:
   - First client becomes "master" with full control
   - Additional clients are "viewers" (receive screenshots, no input)
   - Could add role switching or voting system
   
2. **Chaos Mode** (The Fun One):
   - All clients can control simultaneously
   - Last input wins (mouse clicks, keyboard)
   - Watch the mayhem as people fight over control
   - Perfect for demos, teaching, or just entertainment

3. **Queue Mode**:
   - Clients take turns controlling (time-based or action-based)
   - Others watch and wait their turn
   - Like passing the controller in a video game

### Implementation Considerations
- Server would need to:
  - Accept multiple gRPC connections
  - Fan out screenshot streams to all clients
  - Handle conflicting inputs (or embrace the chaos)
  - Track client roles/permissions
  
- Use cases:
  - Remote tech support (watch and guide)
  - Collaborative shopping/browsing
  - Educational demos
  - Party games ("Can 10 people order pizza together?")
  - Stream viewer participation

### Why This Could Be Amazing
- Imagine 5 people trying to fill out a form together
- Browser-based party games
- "Twitch Plays Pokemon" but for web browsing
- Ultimate test of your website's UX (if 10 people can use it...)

## Band-Level Cache Self-Healing (Future Enhancement)

### Rolling Ground Truth Update
Implement a self-healing mechanism to prevent cache drift and accumulated artifacts:

```go
// On each frame, force refresh one band based on frame number
bandToRefresh := frameNumber % numBands
sixelBands[bandToRefresh].ForceInvalidate()
```

### Benefits
- **Gradual refresh**: Every band gets ground truth update every N frames
- **No visible impact**: Only one band per frame (imperceptible)
- **Self-correcting**: Fixes any cache corruption or drift
- **Prevents accumulation**: JPEG artifacts don't build up over time

### Implementation Notes
- At 30 FPS with 139 bands: Full refresh every ~4.6 seconds
- Could make interval configurable (every 30, 60, or 139 frames)
- Consider skipping if band was already dirty (optimization)
- Could use spatial pattern instead of sequential (less noticeable)

**TODO**: Implement after band-level caching is working and tested.

## Dialog Rendering Flicker Fix (Future Enhancement)

### Problem
When dialogs appear over the browser view, there's visible flicker because:
1. Browser screenshot is drawn to screen
2. Dialog is drawn on top
3. Show() is called
4. Next frame repeats, causing dialog to disappear/reappear

### Option B: Proper Frame Compositing
Restructure the rendering pipeline to composite all elements before display:

**Current Architecture:**
- Screenshot arrives → Decode → Draw to screen → Show()
- Dialog event → Draw dialog over existing → Show() again
- Multiple Show() calls per frame cause flicker

**Proposed Architecture:**
1. **Separate render from display**: 
   - Maintain off-screen composition buffer
   - Draw browser content to buffer
   - Draw UI elements (borders, status) to buffer  
   - Draw dialog (if active) to buffer
   - Single Show() call per frame

2. **Decouple update rates**:
   - Screenshots arrive at 24 FPS
   - Display refreshes at consistent rate (30-60 FPS)
   - Interpolate or repeat frames as needed

3. **Implementation Requirements**:
   - Create composition manager
   - Buffer all drawing operations
   - Synchronize Show() calls to display rate
   - Handle partial updates efficiently

### Benefits
- Zero flicker for dialogs and UI elements
- Smoother overall rendering
- Better control over frame timing
- Foundation for future UI overlays

### Complexity
- Moderate refactoring of display pipeline
- Need to manage additional buffers
- Synchronization between screenshot and display threads

**Feasibility**: YES - Current architecture can be adapted. Main work is decoupling the screenshot receive loop from the display loop and adding a composition layer.

## Notes

- Windows Terminal Preview sixel may have hard FPS limits
- Local-only use case simplifies network optimizations
- Target: 30+ FPS if terminal allows
- Consider Ghostty migration path for future
