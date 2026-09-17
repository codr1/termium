# Comparing performance

Run from a checkout with the [development dependencies](development.md) installed. The harness builds a normal client, starts the actual Go client/Node server/Chromium stack, and serves local pages. It uses the normal private Unix socket and capture pacing. It does not replace capture with a microbenchmark or change the 24 FPS ceiling.

Run `npm ci` once after checkout or dependency updates. The compiler is included in the npm development dependencies; missing Go protobuf generators are installed into a project-local cache on the first build. No separate `protoc` package or PATH customization is required.

Start with a short smoke run:

```bash
npm run benchmark -- --label smoke --renderer sixel --scenes patch --warmup 2s --duration 5s --repeats 1
```

For an implementation comparison, take a baseline before changing code:

```bash
npm run benchmark -- --label before --out dist/performance/before
# Change implementation, then run the same command/settings:
npm run benchmark -- --label after --out dist/performance/after
npm run benchmark:compare -- dist/performance/before/result.json dist/performance/after/result.json
```

Defaults are Kitty and Sixel, four scenes, three repeats, five seconds of warmup after readiness, and thirty measured seconds per run. Allow about fifteen minutes plus startup. Renderer order reverses on alternate repeats. Results retain each run; comparison reports medians and ranges, not a significance claim. Output directories must be new, so a baseline cannot be accidentally overwritten. Interrupted/failed suites are marked incomplete and cannot be compared as completed suites.

Keep the same machine, power source/mode, terminal, viewport, browser/runtime, and background workload. Use `--note 'AC power; performance mode; ...'` to record conditions. Alternate before/after suites when practical to expose thermal drift. A small difference within the repeat-to-repeat range is inconclusive.

## What the numbers mean

| Measurement | Meaning |
| --- | --- |
| Writes/s | Successful complete graphics writes by the client during the window. This is delivered output, **not visible terminal FPS**. |
| Captures/s | Successful production screenshot RPC completions, including images later reused or superseded. |
| Capture p50/p95/p99 | Whole screenshot RPC latency, including server queueing and its current CDP/metadata work. |
| Preparation and queue latency | Client preparation time and time from RPC completion to preparation starting. |
| Write latency and intervals | Time spent writing a graphics payload, and spacing between successful writes. Long intervals reveal stalls that averages can hide. |
| Frame age | Time from the screenshot RPC starting to the graphics write completing. It is not browser-event-to-visible-paint latency. |
| Reuse and pending drops | Prepared graphics reused; raw or prepared frames replaced in bounded pending slots. Drops can be intentional and beneficial. These are event counts, not a conservation equation across window boundaries. |
| CPU by role | Client, Node server, and Chromium descendants sampled separately; optional terminal process. **100% means one fully used logical core**, so totals can exceed 100%. |
| Peak summed RSS | Largest sampled sum of resident memory per role. Shared mappings can be counted more than once; this is not unique physical memory. |
| Go memory/GC | Allocated bytes and objects, GC cycle count, cumulative stop-the-world pause time, and ending live heap. Includes the opt-in instrumentation's allocations. |
| Source/graphics bytes | Captured image bytes and successfully written graphics payload bytes. Terminal control/UI bytes are excluded. |
| Errors | Capture, preparation, and graphics-write failures remain in the raw counters. Check them when interpreting throughput. |

An idle page should have captures and reuse but few or no graphics writes after warmup. Zero idle writes/s is desirable. Empty latency distributions have `samples: 0`; their numeric zero fields are not measured zero-latency operations. Writes can also include replacement metadata or image restoration; they are not proof of unique pixel changes.

CPU/RSS sampling runs once per second. Linux CPU totals use `/proc` clock ticks (with the tick rate read from `getconf CLK_TCK`); macOS uses fractional CPU times from `ps`. Process ancestry and RSS come from `ps` on both systems. If a Linux process exits between the two reads, its last sample falls back to the coarser `ps` time. Short-lived processes missed between samples are absent, and exited processes contribute their last observed total. CPU resource windows have their own saved duration/timestamps and can differ slightly from the client's measurement window. Terminal CPU is separate from Termium's total. RSS is sampled, not an exact instantaneous peak.

## Workloads and settings

- `idle`: fixed text, to measure steady-state capture overhead and deduplication.
- `patch`: a small fixed rectangle changes color at a target 30 Hz, exercising partial image changes.
- `scroll`: a long text page scrolls on a fixed schedule.
- `canvas`: a seeded noise texture moves at a target 30 Hz, stressing changed-image encoding. It is not a representative average web page.

Page animation uses wall-clock time and `requestAnimationFrame`, so an overloaded browser can skip animation steps. Input-to-paint timing and scripted mouse keys/Vimium interaction are separate future measurements.

Select a smaller matrix or one setting at a time:

```bash
npm run benchmark -- --label sixel-png --renderer sixel --capture-format png --scenes patch,scroll
npm run benchmark -- --label plan9 --renderer sixel --palette plan9 --scenes patch,canvas
```

`auto` keeps the shipped capture defaults: PNG for Kitty, JPEG for Sixel. Force the same source format when isolating renderer differences. The comparison command matches actual renderer, format, scene, and viewport, and rejects mismatched runtimes, fixtures, palette, duration, warmup, or output mode. It is intended for before/after implementations with identical settings, not for ranking different image-quality configurations. All raw JSON remains available for deliberate cross-configuration analysis.

The default drained PTY is 162 columns × 49 rows with the client's fallback 8 × 16 pixel cells, normally giving a 1280 × 720 browser viewport after UI borders. `--rows`/`--cols` change it; the report records the actual browser dimensions. The PTY reader discards output continuously without parsing or storing graphics, avoiding terminal rendering as a confounder.

## Include the real terminal

Run in the terminal you want to measure and select a protocol it supports:

```bash
npm run benchmark -- --label foot-sixel --renderer sixel --display
npm run benchmark -- --label ghostty-kitty --renderer kitty --display
```

This connects the client directly to your terminal, using its real geometry and calibration. Do not type into or resize it during a run. `--rows`/`--cols` apply only to the drained PTY. The harness ends each run and restores terminal settings. The output mode is saved so a drained-PTY result cannot silently be compared against a real terminal.

To include terminal CPU/RSS, supply its process ID with `--terminal-pid PID`. Find it in your OS process monitor. This samples that process, not arbitrary children or other terminal applications. If one process serves multiple windows, its usage includes those windows; leave them idle. Record the terminal version, scaling, and display/GPU details in `--note`.

Neither mode measures when the terminal compositor actually paints the image. Establish visible FPS or input-to-paint latency separately using terminal-specific instrumentation or an external recording. A successful write can still be buffered downstream.

## Saved evidence and lower-level probes

Each output directory contains `result.json`, per-run client metrics, process samples, and client logs. Reports include the source revision, dirty-worktree status/diff hash, client/server entry-point and lockfile hashes, fixture hash, OS/CPU, Go/Node/Chromium identities, configuration, and notes. Untracked source contents are not covered by the Git diff hash; commit comparison candidates for durable reproduction. No screenshot payloads are saved by the harness.

The metrics collector is off in normal runs. The harness enables it through `TERMIUM_PERF_REPORT`, keeps bounded samples in memory, and writes once after the measurement window. Both candidates must use this instrumentation. Do not combine comparative runs with `--timings`, screenshot saving, CPU profiling, tracing, race builds, or other test workloads. The harness rejects race-instrumented client binaries. Use separate profiling runs to explain a measured difference; a Go profile alone cannot explain Node, Chromium, or terminal CPU.

For faster isolated comparisons, keep the existing [capture/preparation probes](testing.md#comparing-capture-and-renderer-preparation). The new band benchmark alternates real changed frames with a warm cache:

```bash
go test ./client -run '^$' -bench '^BenchmarkSixelBandChanges$' -benchmem -count=5
```

It covers a one-pixel change in a short final band and a full image change at 1920 × 1081. It excludes image decoding, browser work, transport, and terminal writes. Do not convert its operation time into end-to-end FPS.

Native Linux and Mac CI run a short harness smoke check after the regression suite and save its artifacts. This checks that capture, reports, process sampling, and cleanup work on each platform. Shared CI runner timings are not performance baselines, and no speed threshold is enforced there.
