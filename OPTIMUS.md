# OPTIMUS.md

Implemented changes are recorded first, followed by historical investigations and ideas. Historical timings describe their original probes, not current performance guarantees.

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
