# Sixel profiling — production canvas workload (2026-09-27)

Bounded CPU and allocation profiles of the current client on the live canvas/Sixel pipeline, at 1280×720 and 3840×2160. This is a diagnostic record of where time and allocations go in one build; it makes **no speedup claim** and contains no A/B comparison. Profiling hooks are confined to the investigation branch; no rendering optimization was implemented. `TERMIUM_PROFILE` requires this diagnostic build, not the released client.

## Method

- Binary: `/tmp/termium-viewport-profile/termium-profile` (sha256 `93be38e1a699b19b23caa277434aa5fb7574d28698247d475df950cbec79f3eb`, Build ID `38d54292a0f8488466eb3c643a86168113fd62bb`), built from worktree `/tmp/termium-profile-worktree` at `2beb236` (`vcs.modified=true`, includes the window-profiling recorder), Go 1.27.1-X:nodwarf5, linux/amd64, CGO_ENABLED=1.
- Machine: ea (native Linux amd64, Ryzen AI 9 HX 370). Fresh isolated runtime `/tmp/termium-profile-ea-mTna4j`; Chromium via `PUPPETEER_EXECUTABLE_PATH=/tmp/termium-unit1-ea-Lc3622/chrome/chrome-linux64/chrome`; `TERM=xterm-256color`.
- Four sequential runs, all exit 0:

```bash
# On ea, using the preserved diagnostic runtime and harness; fresh outputs.
cd /tmp/termium-profile-ea-mTna4j/runtime
export PUPPETEER_EXECUTABLE_PATH=/tmp/termium-unit1-ea-Lc3622/chrome/chrome-linux64/chrome
export TERM=xterm-256color
profile_out=$(mktemp -d /tmp/termium-profile-repeat-XXXXXX)
while read -r label mode cols rows; do
  TERMIUM_PROFILE="$mode" /tmp/termium-series-ea-cf0nQA/benchmark \
    --scenes canvas --renderer sixel --duration 30s --warmup 5s \
    --repeats 1 --cols "$cols" --rows "$rows" \
    --label "$label" --out "$profile_out/$label" || break
done <<'RUNS'
cpu720 cpu 162 49
cpu4k cpu 482 139
memory720 memory 162 49
memory4k memory 482 139
RUNS
```

- CPU mode: `runtime/pprof` StartCPUProfile/StopCPUProfile around the 30 s window. Memory mode: forced `runtime.GC()` + heap snapshot before the window, second GC + snapshot after; the after-snapshot doubles as a live inuse view.
- Boundary timestamps (memory720 `client.log`): before-snapshot completion log `18:06:36.081977Z`, window start `18:06:36.082001Z`, window freeze `18:07:06.082473Z`, after-snapshot completion log `18:07:06.085517Z`. These log gaps are about 24 µs and 3 ms; the 4K gaps are about 40 µs and 6.7 ms. They are not exact allocation-sampling boundaries: profiles are sampled and written over time. GC includes brief stop-the-world pauses; the pipeline is otherwise not quiesced during snapshots.
- pprof reports were generated locally with the matching binary:

```bash
profile_binary=/tmp/termium-viewport-profile/termium-profile
profile_results=/tmp/termium-viewport-profile/results
cpu_profile="$profile_results/cpu4k/canvas-sixel-1/client.json.cpuprofile"
heap_profile="$profile_results/memory4k/canvas-sixel-1/client.json"
go tool pprof -top -nodecount=15 "$profile_binary" "$cpu_profile"
go tool pprof -top -cum -nodecount=20 "$profile_binary" "$cpu_profile"
go tool pprof -list writePixelData "$profile_binary" "$cpu_profile"
go tool pprof -list processSOS "$profile_binary" "$cpu_profile"
for sample in alloc_space alloc_objects; do
  go tool pprof -top -nodecount=15 -sample_index="$sample" \
    -base "$heap_profile.heap.before" "$profile_binary" "$heap_profile.heap.after"
done
go tool pprof -top -nodecount=15 -sample_index=inuse_space "$profile_binary" "$heap_profile.heap.after"
```

Artifacts: raw profiles and `client.json` under `/tmp/termium-viewport-profile/results/{cpu720,cpu4k,memory720,memory4k}/canvas-sixel-1/`; all sixteen text reports in `/tmp/termium-viewport-profile/results/pprof/` (`{cpu720,cpu4k}-{top,cum,gc}.txt`, `{cpu720,cpu4k}-list-writePixelData.txt`, `{cpu720,cpu4k}-list-processSOS.txt`, `memory{720,4k}-{alloc_space-delta,alloc_objects-delta,inuse_space-end}.txt`). Driver, conditions and exit records: `/tmp/termium-viewport-profile/results/{driver.sh,driver.log,conditions.log,exits.txt,binaries.sha256}`.

## CPU: 720p vs 4K

30 s windows; total samples 23,310 ms (77.70% of wall) at 720p and 25,500 ms (85.00%) at 4K. Flat and cumulative columns are shown separately; **cumulative percentages overlap and must not be summed.**

| Function | 720p flat / cum | 4K flat / cum |
| --- | ---: | ---: |
| `go-sixel.(*Encoder).writePixelData` | 8,400 ms (36.04%) / 14,010 ms (**60.10%**) | 8,470 ms (33.22%) / 14,720 ms (**57.73%**) |
| `runtime.mallocgcTinySC2` | 1,930 ms (8.28%) / 2,810 ms (12.05%) | 1,880 ms (7.37%) / 2,670 ms (10.47%) |
| `image/jpeg.(*decoder).processSOS` | 790 ms (3.39%) / 4,790 ms (**20.55%**) | 890 ms (3.49%) / 5,780 ms (**22.67%**) |
| `image/jpeg.(*decoder).decodeHuffman` | 1,220 ms (5.23%) / 1,760 ms (7.55%) | 1,220 ms (4.78%) / 1,960 ms (7.69%) |
| `image/jpeg.(*decoder).reconstructBlock` | 1,110 ms (4.76%) / 1,600 ms (6.86%) | 1,200 ms (4.71%) / 1,950 ms (7.65%) |
| `image/internal/imageutil.DrawYCbCr` | 1,080 ms (4.63%) flat | 1,180 ms (4.63%) flat |
| `runtime.memmove` | 1,010 ms (4.33%) flat | 980 ms (3.84%) flat |
| `bytes.(*Buffer).Write` | 710 ms (3.05%) / 1,590 ms (6.82%) | 950 ms (3.73%) / 1,880 ms (7.37%) |
| `go-sixel.(*Encoder).cachedDraw` | 660 ms (2.83%) / 1,330 ms (5.71%) | 900 ms (3.53%) / 1,820 ms (7.14%) |
| `go-sixel.(*checkedWriter).Write` | 510 ms (2.19%) / 2,100 ms (9.01%) | 750 ms (2.94%) / 2,640 ms (10.35%) |
| `internal/runtime/syscall/linux.Syscall6` | 630 ms (2.70%) flat | 780 ms (3.06%) flat |

Shape is the same at both resolutions: Sixel emission (`writePixelData`) dominates, JPEG decode (`processSOS` subtree) is second, and small-object allocation traffic (`mallocgcTinySC2`, see GC section) is a visible third component. 4K spends more total CPU (85% vs 78% of wall) with the same relative shape.

## Source-line hotspots (`-list`)

`writePixelData` (`third_party/go-sixel/sixel.go`), flat/cum per run:

| Line | Code | 720p | 4K |
| ---: | --- | ---: | ---: |
| 397 | `ch := e.buf[width*n+x]` (per-color full-width scan) | 3.45 s flat | 3.40 s flat |
| 395 | `for x := 0; x < width; x++` (same loop overhead) | 2.59 s flat | 2.66 s flat |
| 405 | `w.Write([]byte{s})` | 270 ms / 2.42 s cum | 340 ms / 2.59 s cum |
| 423 | `w.Write([]byte{0x21, c1, c2, s})` (DECGRI) | 160 ms / 1.50 s cum | 230 ms / 1.69 s cum |
| 426 | `w.Write([]byte{0x21, byte(0x30+cnt), s})` (DECGRI) | 150 ms / 790 ms cum | 110 ms / 870 ms cum |
| 376 | `e.buf[width*int(idx)+x] \|= 1 << uint(p)` (bit-set pass) | 710 ms flat | 690 ms flat |
| 368 | `rgba.RGBAAt(x, y).A != 0` | — / 330 ms cum | — / 590 ms cum |

Two distinct hotspots inside the function: (a) the per-color full-width scan of `e.buf` (lines 395–397, ~6 s flat combined per run), and (b) the many small `w.Write([]byte{...})` calls with fresh slice literals (lines 405/423/426, ~4.7–5.1 s cumulative). Both are observations, not proven fixes.

`image/jpeg.(*decoder).processSOS` (`scan.go`): flat 790 ms / cum 4.79 s at 720p and 890 ms / cum 5.78 s at 4K; line 312 `d.reconstructBlock(...)` accounts for 1.61 s cumulative at 720p, with Huffman decode (`decodeHuffman`) another major child.

## Allocations (sampled heap-profile attribution)

Sizes below use MiB (the pprof text formatter labels these binary units `MB`). `alloc_space` delta between snapshots: 4,780.99 MiB total (720p) and 6,010.44 MiB (4K). Top flat nodes:

| Node | 720p | 4K |
| --- | ---: | ---: |
| `go-sixel.(*Encoder).writePixelData` | 1,746.53 MiB (36.53%) | 2,288.53 MiB (38.08%) |
| `bytes.(*Buffer).String` (inline) | 1,457.74 MiB (30.49%) | 1,893.36 MiB (31.50%) |
| `image.NewYCbCr` | 639.77 MiB (13.38%) | 830.70 MiB (13.82%) |
| `image.NewPaletted` | 466.58 MiB (9.76%) | 570.64 MiB (9.49%) |

Cumulative context (overlapping, not summable): `encodeInto` 3,680.35 MiB (76.98%) / 4,755.55 MiB (79.12%); `EncodePixelData` 2,215.61 MiB (46.34%) / 2,859.18 MiB (47.57%); `jpeg.Decode` 648.38 MiB (13.56%) / 831.72 MiB (13.84%).

`alloc_objects` delta: **115,832,943** sampled objects (720p) and **151,312,890** (4K), with `writePixelData` flat at 114,460,371 (**98.82%**) and 149,981,425 (**99.12%**).

> **Caveat — sampled attribution, not exact counts.** `alloc_objects` is heap-profile sampling (roughly one sample per `MemProfileRate` = 524,288 bytes), with tiny-allocation packing semantics; it is **not** `runtime.MemStats.Mallocs`. The runtime aggregate for the same 4K window reports `go_memory.allocations = 859,949,810`, versus the pprof delta estimate of 151,312,890 — different measurement semantics. Read "≈99% of sampled allocation objects in `writePixelData`" as a share of sampled attribution; it is not an exact call count or exact objects/frame figure from pprof.

Runtime aggregates over the window (quoted separately from pprof): 720p allocated_bytes delta 5,032,359,944, allocations 670,944,734; 4K allocated_bytes delta 6,231,705,920, allocations 859,949,810.

## Live heap: arena retention vs RSS

End-of-window `inuse_space` (Go live heap at the post-GC after-snapshot): **50.93 MiB total** at 720p and **393.79 MiB** at 4K. Dominant node is frame-arena retention: `main.(*frameArenaPool).acquire` 40 MiB (78.53%) at 720p; `main.(*frameArena).alloc` 320 MiB (81.26%) at 4K — i.e., ~40 MiB of ~51 MiB and ~320 MiB of ~394 MiB live heap is the four-slot arena pool holding decoded RGBA pixels and final terminal payloads.

This is **not** process RSS: the 4K client peaked around 777 MiB RSS in earlier sweep evidence, versus 393.79 MiB Go live heap here. `inuse_space` measures Go-heap objects reachable after a forced GC; it excludes unused runtime heap pages, stack memory, allocator metadata and non-Go memory. Reachable backing arrays count in full, including their unused capacity. Chromium is a separate process. The recorder's `HeapAlloc` at window freeze was higher (71.49 MiB / 554.48 MiB) than this later post-GC sampled snapshot; the two observations differ in timing, GC state and measurement method, so they do not identify exactly which objects disappeared.

The arenas cover decoded RGBA pixels and final payloads only. They do **not** cover third-party encoder temporaries: per-frame `image.NewYCbCr`/`NewPaletted`, JPEG decode buffers, and go-sixel's own small allocations (including the many short-lived `[]byte{...}` slices in emission) are allocated outside the arena and churn through the normal allocator.

## GC vs allocator

- `runtime.mallocgcTinySC2` at 8.28% / 7.37% flat is **allocator** overhead — the tiny-size-class malloc path serving the high rate of small allocations (chiefly `writePixelData`'s per-write slice literals). It is not garbage collection.
- GC-related CPU stacks account for approximately 2.02% of sampled CPU at 720p and 0.78% at 4K. These are samples matching `runtime\.(gc|scan|sweep|mark)|runtime\.\(\*(gc|sweep)` anywhere in the stack, saved as the `*-gc.txt` reports; this filter is an attribution aid, not exhaustive runtime accounting. Separately, recorder totals are 95 cycles / 7.12 ms accumulated pause at 720p and 15 cycles / 1.16 ms at 4K. Pause time measures stop-the-world delays, not all GC CPU. Both observations support prioritizing encoding/allocator work over GC tuning.

## Validation and limitations

The coordinator reviewed the instrumentation, with an independent lifecycle review. `go test -race ./client ./internal/perf ./cmd/benchmark` and `go vet ./client` passed locally; the normal symbol-bearing binary was then built. Logs and build provenance are under `/tmp/termium-viewport-profile/validation-*`; the exact source delta is `stage1.diff` there (SHA-256 `0ad46f55d79020d9c7cdfb8ec0742c2f08a072a6bdc94e4d2d9e7928835d3deb`). After measurement, only documentation and a sampling-boundary comment were corrected; runtime behavior is unchanged. All four ea runs completed with the expected dimensions and no recorded error/drop counters. Profiles parse with the matching binary. No native macOS validation was performed.

- One machine, one synthetic canvas fixture, one build, with the actual client/browser pipeline and drained PTY output. These profiles sample only the Go client. They do not explain Chromium's capture latency or measure real-terminal rendering. Real-terminal and browser-side profiling remain open; there is no A/B pairing or speedup/regression claim.
- Throughput figures from the profiled runs are diagnostic context only: 477 captures / 1,423,692,767 written bytes at 720p; 70 captures (69 prepared/written) / 1,826,535,722 bytes at 4K. The harness's external CPU/RSS sampling runs until `client.json` exists and therefore includes profile finalization; only the in-process recorder totals exclude boundary work.
- pprof is sampled: CPU at the default rate, heap objects per the caveat above. Snapshot completion timestamps bracket the recorder window, but do not establish exact allocation boundaries; the pipeline was not quiesced.
- Cumulative percentages overlap across call paths and are never summed in this document.

## Next candidates (observations, not proven fixes)

1. **Small-byte-slice allocations and writer calls in Sixel emission** — `writePixelData` holds ≈99% of sampled allocation objects and ~4.7–5.1 s cumulative CPU in the per-write `w.Write([]byte{...})` paths; batching or reusing output buffers targets both the allocator traffic (`mallocgcTinySC2`) and writer-call overhead at once.
2. **Full-width per-color scan** (lines 395–397) is a second observed CPU hotspot (~6 s flat combined per run); restructuring it is unproven.
3. GC itself looks small — not the target. Any arena extension must be scoped with the caveat that current arenas do not cover third-party encoder temporaries.
