# OPTIMUS.md

This is a list of optimizations we want to consider. We will be deleting them one at a time as we implement them.

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

## Notes

- Windows Terminal Preview sixel may have hard FPS limits
- Local-only use case simplifies network optimizations
- Target: 30+ FPS if terminal allows
- Consider Ghostty migration path for future