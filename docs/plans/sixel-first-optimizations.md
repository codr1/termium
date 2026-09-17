# Agent briefing: first Sixel optimizations

## Assignment

Implement a small, measured improvement to the existing Sixel preparation path. The user prioritizes speed and wants before/after evidence from the new benchmark harness. Start with redundant band CRC work and unused bookkeeping, then eliminate short-band buffer reallocations. Keep those two changes independently reviewable and measurable. Review, fix findings, and repeat until clean.

Read `CLAUDE.md`, [OPTIMUS](../../OPTIMUS.md), [benchmarking](../benchmarking.md), and [testing](../testing.md) first. Before implementation, tell the user which change you are starting and where its baseline is saved.

## Starting state — preserve this work

At handoff, the checkout is `/home/vess/dev/termium`, on `main`, with substantial **uncommitted** work implementing the benchmark harness, metrics, documentation, CI smoke checks, and project-local protobuf tools. These changes are prerequisites, not changes to discard. No Sixel encoding optimization has landed in this working tree.

Inspect `git status` and the diff before editing. A fresh worktree created solely from `HEAD` will omit the new harness. Preserve the current prerequisite work in your branch/worktree strategy and clearly identify it separately from your optimization commits. Do not blanket-stage unrelated files. In particular, `docs/plans/native-browser-control-survey.md` is a separate existing artifact.

The user may be running a terminal baseline in this same checkout. Check for an active benchmark before rebuilding binaries or changing its inputs. Do not kill their session, overwrite results, or mix concurrent benchmarks with tests. Wait for it to finish or coordinate a separate checkout containing the same prerequisite changes.

Build through npm scripts. `npm ci` supplies pinned `protoc`; `scripts/generate-proto.mjs` installs missing Go generators into the project-local cache. No `/tmp/termium-build-tools` PATH workaround should be necessary. Initial dependency/tool setup requires network access. Follow the environment's actual permission rules when it is needed.

The existing harness and full suite passed locally on Linux. Mac compile checks are not native execution evidence. CI has native Linux, Mac ARM, and Mac Intel checks and a short benchmark smoke run without speed thresholds.

## First change: exact band comparison and cleanup

Inspect:

- `client/frame_pipeline.go`: `framePreparer.prepare`, `encode`, and `reuse`.
- `client/sixel_bands.go`: band layout, hashes, cache strings, and unused fields/methods.
- `client/frame_pipeline_test.go`: pixel round trips, hash-collision regression, output limits, and frame ownership.

The websafe path currently hashes every band with CRC32 and then checks actual pixels before trusting matching hashes. Replace that decision with direct exact comparison of each band's pixels against the corresponding last successfully prepared image. A missing cache or incompatible prior image requires encoding. Keep the full-frame unchanged-image fast path.

Remove `HashBand`, `crcTable`, the band `Hash`, and `DetectDirtyBands` once their responsibilities are replaced. Remove the unused `SixelColumn`/`Columns` allocation, `FrameNumber`, `GetDirtyBandCount`, `ComposeSixelOutput`, and `MarkAllDirty` after reconfirming their callers. Simplify any dirty/count state made redundant by this change; avoid unrelated cleanup.

**Cache validity on failure matters.** `p.last` advances only after successful preparation. If some band strings are replaced and a later band or output-size check fails, those strings may no longer correspond to `p.last.Image`. Do not let an exact comparison against the old image then reuse those newer strings. Stage cache updates until success, invalidate the affected cache on error, or use another simple approach with an explicit invariant. No new full-image copy is needed just to preserve this relationship.

The production decoder currently supplies normalized RGBA images. Keep comparison offsets consistent with that contract and the short final band; do not expand this task into a general image library.

## Second change: retain the normalized band's backing storage

Inspect `client/sixel_band_encoder.go`, especially `NewBandEncoder` and `EncodeBand`.

The encoder preallocates a six-row RGBA buffer but reallocates it when the last band is shorter. Encoding another full band then reallocates again. Keep storage for the maximum band height and pass a correctly bounded view to the underlying encoder.

A short band must not expose stale rows from a prior full band. This matters because the encoder iterates in six-row groups. Verify short-to-full and full-to-short transitions, including image heights with remainders one through five. Do not mutate previously published frame pixels or cached output through reused storage.

Measure this separately after the first change. The default end-to-end viewport has a height divisible by six; it cannot alone demonstrate the short-band improvement. The existing band microbenchmark uses a non-divisible height and is appropriate evidence for that allocation reduction.

## Required correctness coverage

Use tests that check output behavior, not just the new implementation's bookkeeping:

- Decode Sixel and verify unchanged rows and changed pixels across first, middle, and short final bands; include one-pixel changes and repeated A → B → A transitions.
- Cover resize/geometry changes and successful recovery after a deliberately induced preparation error following partial band work.
- Exercise full/short buffer reuse without stale pixels leaking past the logical bounds.
- Preserve exact dimensions, palette/register correctness, output-size limits, writer failures, frame immutability, metadata freshness, and unchanged-frame reuse.

Replace the hash-collision-specific test with appropriate exact-comparison/cache regression coverage. Do not retain a hash solely for that test. The same test file also uses `hash/crc32` to construct PNG headers; that separate use remains valid.

Keep the existing PNG/JPEG preparation tests and actual-client Sixel output decoding. Do not relax correctness checks to obtain better timings. The user's tolerance for a transitional browser frame is not permission to corrupt a Sixel payload or indefinitely reuse wrong cached pixels.

## Measurements before editing

Use the same machine, Go/Node/Chromium versions, power conditions, fixture, and configuration for both candidates. Do not run benchmarks under the race detector or concurrently with builds/tests. Save fresh output directories; never overwrite a baseline. Capture the precise source state, including the uncommitted prerequisite diff if necessary.

After a normal build, save the band microbenchmark:

```bash
npm run build
go test ./client -run '^$' -bench '^BenchmarkSixelBandChanges$' -benchmem -count=5 > /tmp/termium-sixel-before.txt
```

Its one-pixel and all-pixels cases alternate changed images with a warm band cache at 1920 × 1081. It excludes decoding, RPC, browser work, and terminal presentation. It is not an FPS measurement. If you add a workload needed to expose a missed case, run the identical workload against both implementations.

Save an end-to-end baseline before changing implementation:

```bash
npm run benchmark -- --label sixel-before --renderer sixel --out dist/performance/sixel-before
```

Repeat the same measurements after each change, using distinct output names. For the combined result:

```bash
go test ./client -run '^$' -bench '^BenchmarkSixelBandChanges$' -benchmem -count=5 > /tmp/termium-sixel-after.txt
npm run benchmark -- --label sixel-after --renderer sixel --out dist/performance/sixel-after
npm run benchmark:compare -- dist/performance/sixel-before/result.json dist/performance/sixel-after/result.json
```

The harness defaults to three repeats, five seconds of warmup, and thirty measured seconds for each of four local scenes. It records capture/write rates, latency distributions, CPU per process role, sampled RSS, Go allocations/GC, reuse, pending drops, and byte counts. Review errors as well as throughput. Compare variability rather than declaring every numerical difference a win.

Use `--display` only in a suitable real terminal and keep that evidence separate from drained-PTY results. Completed graphics writes are not visible FPS. A 24 FPS ceiling or capture bottleneck may hide a preparation improvement in total throughput; report a repeatable CPU/allocation/latency improvement honestly even if writes/s does not change. Do not claim a gain when results are within noise.

## Scope boundaries

Keep the server capture/CDP redesign, Kitty dedup, JPEG conversion, Plan9 band caching, adaptive palette changes, band-only encoder API, and partial terminal updates for separate work. The larger band-only encoder opportunity is already documented in OPTIMUS. This pass keeps the current palette, output protocol, frame pacing, and terminal write ownership.

Do not change dependencies, capture defaults, image quality, benchmark fixtures, or measurement definitions between candidates to manufacture an apparent improvement. `bytes.Equal` short-circuits; do not repeat the external audit's claim of three complete pixel scans on every changed frame. Column storage savings occur on cache creation/resize, not every frame.

## Completion and handoff

Run meaningful focused tests while developing, then `npm test` and `npm run typecheck` on the final change. Restore a normal executable with `npm run build:client` before further benchmarking. Perform a fresh review of the final diff, fix findings, and rerun the affected checks until clean. Include native CI results when available; do not represent a local Linux run as Mac validation.

Update OPTIMUS with what was changed, the cache/buffer invariants, exact benchmark commands and environment, measured results and variability, and any unresolved limitations. Preserve the raw evidence and identify its paths. Keep dependency versions in manifests and machine-generated reports rather than duplicating them in human-facing guidance.

Prepare the optimization as a focused PR or reviewable patch, clearly separating the existing harness/tooling prerequisites. Report remaining findings, validation, and measured tradeoffs to the coordinating agent/user. This briefing does not authorize merging or publishing a release.
