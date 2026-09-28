# CPU Hot Functions — Kitty Go Client and Node V8 (2026-09-27)

Analysis-only record for the six bounded ea profiling runs. No implementation or
benchmark work in this stage. Companion to `2026-09-27-sixel-profiling.md`.

## Inputs and instrumentation labels

| Run | Window | Profile analyzed | Instrumentation on during run |
|---|---|---|---|
| kittycpu-720 (1280x720, kitty) | 30s | `client.json.cpuprofile` (Go pprof proto) | Go CPU profiler + phase timeline |
| kittycpu-2160 (3840x2160, kitty) | 30s | `client.json.cpuprofile` (Go pprof proto) | Go CPU profiler + phase timeline |
| kitty-720 / kitty-2160 | 10s each | `client.json.node.cpuprofile` (V8 inspector JSON) | Go CPU/timeline + Node inspector + Chromium tracing |
| sixel-720 / sixel-2160 | 10s each | `client.json.node.cpuprofile` (V8 inspector JSON) | Go CPU/timeline + Node inspector + Chromium tracing |

All paths under `/tmp/termium-pipeline-profile/results/<run>/canvas-<renderer>-1/`.
Both Go profiles carry Build ID `6f7ada07ad71e53d32715491b9a7c0d46bdd0c39` (same binary).

Weighting: Go pprof samples are 100 Hz, so flat seconds below are already weighted
sample totals. Node self time is the sum of per-sample `timeDeltas` attributed to each
leaf callFrame — not total-window divided by sample count.

## Go CPU hot functions (`go tool pprof -top`)

### kittycpu-720 — total samples 1.16s over a 30s window (~3.9% of one core)

| flat | % of profile | function |
|---|---|---|
| 0.28s | 24.1% | `encoding/base64.(*Encoding).Encode` |
| 0.23s | 19.8% | `internal/runtime/syscall/linux.Syscall6` |
| 0.05s (cum 0.12s) | 4.3% (10.3%) | `tcell/v2.(*tScreen).drawCell` |
| 0.04s | 3.5% | `tcell/v2.(*CellBuffer).GetContent` |
| 0.04s | 3.5% | `rivo/uniseg.propertySearch[go.shape.[3]int]` (inline) |
| 0.04s | 3.5% | `runtime.memmove` |
| 0.03s | 2.6% | `runtime.futex` |

### kittycpu-2160 — total samples 2.0s over a 30s window (~6.7% of one core)

| flat | % of profile | function |
|---|---|---|
| 0.38s | 19.0% | `internal/runtime/syscall/linux.Syscall6` |
| 0.28s (cum 0.77s) | 14.0% (38.5%) | `tcell/v2.(*tScreen).drawCell` |
| 0.28s | 14.0% | `tcell/v2.(*CellBuffer).GetContent` |
| 0.23s | 11.5% | `encoding/base64.(*Encoding).Encode` |
| 0.11s | 5.5% | `runtime.memmove` |
| 0.07s (cum 0.12s) | 3.5% (6.0%) | `tcell/v2.(*CellBuffer).Dirty` |
| 0.07s | 3.5% | `internal/runtime/maps.memHashAES` |

Reading:

- Frame transport is the top flat cost at both resolutions: base64 encoding of the
  capture payload plus syscall write/read (gRPC over unix socket, terminal writes).
- The tcell cell-drawing cluster (`drawCell`, `GetContent`, `Dirty`, uniseg grapheme
  work) is text-UI rendering through the `SetContent`/`Show` paths in
  `client/main.go`, `client/navigation_ui.go`, and `client/ui_layout.go` — not the
  kitty graphics frame path. It grows with terminal size and becomes the largest
  application-level cost at 4K (38.5% cumulative of all Go samples).
- The pprof totals (1.16s / 2.0s per 30s) are far below the run.log CPU headlines for
  these runs (53% / 110%) because they measure different scopes: the harness headline
  covers the whole process tree (Go + Node + Chromium), while pprof samples the Go
  process only. They must differ; cite each with its scope label. The relative
  hot-function breakdown within each profile is unaffected.

## Node V8 self time (timeDeltas-weighted, per-sample)

Coverage check — `endTime - startTime` vs summed `timeDeltas`:

| Run | Window | Sum(timeDeltas) | Samples |
|---|---|---|---|
| kitty-720 | 10.000s | 9.999s | 9459 |
| kitty-2160 | 10.000s | 10.000s | 9461 |
| sixel-720 | 10.000s | 10.000s | 9459 |
| sixel-2160 | 10.001s | 10.000s | 9469 |

Special frames, reported separately from named functions (percentages of window):

| Run | (idle) | (program) | (garbage collector)* | Named total |
|---|---|---|---|---|
| kitty-720 | 9514.7 ms (95.15%) | 42.2 ms (0.42%) | 163.5 ms (1.64%) | 278.4 ms (2.78%) |
| kitty-2160 | 9249.3 ms (92.49%) | 64.0 ms (0.64%) | 329.1 ms (3.29%) | 357.6 ms (3.58%) |
| sixel-720 | 9491.6 ms (94.91%) | 53.8 ms (0.54%) | 161.5 ms (1.61%) | 292.9 ms (2.93%) |
| sixel-2160 | 9726.2 ms (97.25%) | 22.2 ms (0.22%) | 78.1 ms (0.78%) | 173.9 ms (1.74%) |

\* TimeDeltas-weighted samples attributed to the collector. This is not a GC-pause measurement or exact thread-CPU accounting; weighted V8 samples include elapsed intervals between samples.

Top named functions per run:

| kitty-720 | ms | kitty-2160 | ms |
|---|---|---|---|
| onMessage [Connection.js] | 39.0 | onMessage [Connection.js] | 81.6 |
| post [node:inspector] | 26.8 | post [node:inspector] | 25.8 |
| concat [node:buffer] | 13.8 | utf8Slice | 21.1 |
| base64Write | 12.7 | finish [binary-encoding.js] | 19.0 |
| nextTick [task_queues] | 10.6 | _copyActual [node:buffer] | 16.9 |
| indexOfString | 7.4 | PuppeteerError [Errors.js] | 12.7 |

| sixel-720 | ms | sixel-2160 | ms |
|---|---|---|---|
| onMessage [Connection.js] | 44.4 | onMessage [Connection.js] | 33.4 |
| post [node:inspector] | 27.2 | post [node:inspector] | 18.8 |
| concat [node:buffer] | 10.6 | utf8Slice | 9.8 |
| base64Write | 9.5 | base64Write | 7.5 |
| _copyActual [node:buffer] | 8.4 | _copyActual [node:buffer] | 7.5 |
| nextTick [task_queues] | 7.3 | indexOfString | 6.4 |

Notes:

- `post [node:inspector]` (~19–27 ms per 10s) is the cost of the inspector itself in
  these instrumented runs; it is not present in uninstrumented operation.
- Named time is dominated by CDP message dispatch (`onMessage`) and buffer/base64
  handling of already-encoded frame payloads — consistent with encoding happening
  upstream, before Node receives data.
- The collector receives more sampled time in Kitty 4K than Sixel 4K (3.29% vs 0.78%). Kitty processed more frames and more total payload bytes in this window, but these samples do not establish the cause. Its individual PNG frames were smaller than the JPEG frames, not larger.

## Synthesis with the native-trace finding

The Chromium trace places most of the 4K post-encode interval in the browser task that returns the screenshot: approximately 44.8 ms JPEG / 19.5 ms PNG of task CPU **outside** image encoding. These profiles show little Node activity during the window. Go CPU is largely base64, syscalls and tcell text-UI work; separately, the capture goroutine has an RPC outstanding for about 98% of wall time. See [the whole-pipeline report](2026-09-27-pipeline-profiling.md) for the task attribution and its limits.

The Go samples total only 116 / 200 sampling ticks; small function differences are not precise rankings. Flat CPU in syscalls does not include all time blocked in I/O. Saved pprof tables: `/tmp/termium-pipeline-profile/results/pprof/kitty-{720,2160}-{top,cum}.txt`. Node aggregation script: `/tmp/termium-pipeline-profile/analyze_node.py`; full weighted function/URL tables: each traced run's `node-analysis.json`.
