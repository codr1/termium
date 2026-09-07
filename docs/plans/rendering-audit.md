# Graphics audit and implementation priorities

Audit date: 2026-09-06. Reviewed base: `78b62dae6c8e062649e7799062d6b587f3c261f1`.

Status: historical findings against the base above. Both Kitty and Sixel rendering exist. The audit included source review, isolated probes using the pinned Sixel fork, and a PTY input probe. It did not certify visual behavior across real terminal emulators.

## Implementation update

The rendering branch now separates preparation from UI painting, keeps only the newest pending frames, suppresses routine Kitty responses on every chunk, preserves the Sixel cursor, and explicitly invalidates stale or hidden graphics. It adds paced unary capture, stream backpressure, bounded image sizes, and recovery from failed capture requests. Captures use an independent CDP session that is detached on navigation, avoiding a stranded screenshot without disrupting input/history. The local encoder fixes palette indices, raster dimensions, output errors, and cache ownership; websafe is the default.

Tests cover current terminal screen state through a VT emulator, capture/navigation cancellation against Chromium, flow control, palette pixels, band boundaries, exact cache comparisons, unchanged-frame byte budgets, and overlay restoration. These do not certify actual graphics emulators. Font/DPI recalibration, terminal response demultiplexing for delayed probes, multiplexer passthrough, partial terminal updates, and help styling remain separate work. A synchronous terminal write can still block the UI; pacing reduces backlog, not the latency of a single blocked write.

## Correctness before Vimium

1. **Kitty replies can become keyboard input.** `client/kitty_renderer.go` uses `q=1` for frame transmission and image deletion. This allows error replies. A PTY probe against the reviewed tcell dependency showed a Kitty error response becoming ordinary keys, including `G` and `i`. Those can become navigation commands once Vimium is enabled. Suppress all routine graphics replies with `q=2`; separately handle deliberate capability-query responses before keyboard dispatch. Test fragmented replies and typing around probes. See the [Kitty response rules](https://sw.kovidgoyal.net/kitty/graphics-protocol/#suppressing-responses-from-the-terminal).
2. **Sixel saves the cursor after positioning it.** The three output paths in `client/main.go` restore the viewport origin instead of the cursor position tcell believes it owns. Use one checked save → position → transmit → restore path. Test the emitted sequence ordering and the next local editor update.
3. **Stale Kitty graphics are not explicitly removed.** `displayFrame` rejects older-generation or mismatched-size frames by clearing text cells. That does not reliably remove the image placement. Invalidate graphics explicitly on document changes, invalid frames, resize, and stream failure. See [Kitty graphics and terminal actions](https://sw.kovidgoyal.net/kitty/graphics-protocol/#interaction-with-other-terminal-actions).
4. **The Plan9 palette loses white.** The pinned Sixel fork adds one to a `uint8` palette index; index 255 wraps to zero and gets skipped. A pixel round-trip probe reproduced transparent output for pure white. Widen before adding and test every palette entry, including transparency and the last palette slot.
5. **Sixel output errors are ignored by the dependency.** An always-failing writer still produced a successful `Encode` result. Propagate writer and flush failures. Encoding into a checked intermediate buffer can separate encoder behavior from terminal output, but the dependency still needs correction.
6. **The band compositor declares padded raster height.** `NumBands*6` can exceed the viewport height by five pixels. Emit the actual height. Test partial final bands of one through five rows; whether padded background currently damages the border depends on the emulator.

## Why Sixel is slow

The current UI redraws the latest screenshot after every event, including mouse movement, keyboard input, operation acknowledgments, and state polls. Expensive decoding, quantization, encoding, and output happen in that same event loop. This couples input responsiveness to image throughput.

The websafe band cache avoids some encoding but still transmits the complete image. It even recomposes the image when no bands changed, and a rolling refresh forces one band dirty on subsequent frames. A cache hit therefore does not mean less terminal work.

An isolated probe of one synthetic 800×600 gradient, using the actual pinned encoder and memory output, measured:

| Path | Approximate time | Encoded bytes |
| --- | ---: | ---: |
| Adaptive full frame | 328–342 ms | 86,947 |
| Websafe first frame | 179 ms | 68,534 |
| Websafe warmed cache | 27–30 ms | 68,534 |
| Cached bands, unchanged pixels | 0.55–0.73 ms | 68,435 on every frame |

These are diagnostic measurements, not reproducible performance guarantees or end-to-end frame rates. They exclude browser capture, transport, terminal writes, and terminal rendering. At 24 frames per second, the unchanged cached output alone is about 1.64 MB/s.

Improve this in the following order:

1. Track browser-image damage separately from toolbar/help/cursor damage. Do not resend a screenshot just because the user moved the pointer or edited the address.
2. Skip unchanged images, with explicit invalidation when resize, overlays, or terminal redraw erase the displayed image. Include document identity in this decision.
3. Prepare images in a bounded worker, retaining only the newest pending frame. Keep terminal output under one owner; do not let encoding workers write escape sequences.
4. Respect gRPC writable backpressure. `server/src/server.ts` currently logs a failed `write` readiness check and keeps capturing. Stop producing until drain or cancellation. Throttle hidden views and adapt capture/presentation pacing to observed throughput.
5. Measure capture, decode, quantization, encode, queue delay, output bytes, and input-to-paint latency separately. Include cold and warm cache results, scrolling, text, photographs, and animation.
6. Investigate partial terminal updates only after full-frame correctness. Sixel palette stability, background behavior, six-row alignment, and repaint after overlays need emulator tests.

Do not parallelize the existing fixed-palette encoder without addressing its global, unsynchronized, unbounded color cache. The cache is also not keyed by palette. A bounded or direct lookup for websafe quantization is preferable to accumulating every input color.

## Compatibility and resource gaps

- **Cell pixel geometry is only calibrated at startup.** Resize reconciles rows and columns but does not remeasure font size or DPI changes. Pixel-only changes can leave screenshot placement and mouse coordinates inconsistent. Runtime queries need safe response routing. See [xterm window operations](https://invisible-island.net/xterm/ctlseqs/ctlseqs.html).
- **Transport limits and allowed viewport size disagree.** The Go client retains gRPC's default 4 MiB receive limit, while a detailed PNG at an allowed large viewport can exceed it. A stream receive error ends the screenshot loop. Define bounded pixel and encoded-byte budgets, compatible message limits, visible errors, and controlled stream recovery.
- **Detection is not compatibility certification.** Probe timeouts and response parsing need testing with delayed replies, user typing, SSH, and multiplexers. tmux/screen graphics passthrough is not implemented.
- **Sixel currently receives JPEG quality 60 screenshots.** This loses quality before palette quantization. Benchmark alternative bounded capture formats for text fidelity and stable change detection before choosing another default.
- **Current automated terminal integration uses tcell mode.** Kitty tests inspect protocol strings; Sixel needs pixel round-trips and output tests. Neither establishes emulator-level visual correctness.

Required regression coverage: terminal replies cannot become keys; cursor preservation; stale-image deletion; palette round-trips; exact final-band height; unchanged-frame byte budgets; slow-writer responsiveness; cancellation during backpressure; pixel-size changes; oversized-frame handling; and help/menu restoration on Kitty and Sixel. Maintain a small Linux/macOS acceptance matrix with actual graphics-capable terminals.

## Help, menus, and dependencies

Herdr uses Ratatui and Crossterm. Its panel appearance comes from styled terminal cells, borders, layout, and a shared palette; see its [dependencies](https://github.com/herdrdev/herdr/blob/master/Cargo.toml) and [panel widgets](https://github.com/herdrdev/herdr/blob/master/src/ui/widgets.rs). Termium can use the same visual principles with its Go UI.

Dependencies are welcome when they improve the result. Lip Gloss is a candidate for reusable styling and layout. It returns styled terminal text, so integration with tcell requires translating supported styling into cells; printing its ANSI output behind tcell's back would violate cursor ownership. Bubbles components would additionally need input/view adaptation. These integrations need a small tested prototype before selecting versions or adding dependencies.

Do not run a second Bubble Tea terminal loop alongside tcell. A full move to Bubble Tea/Ultraviolet would be a deliberate renderer migration with graphics acceptance tests. Ultraviolet documents Kitty **keyboard** support; that is distinct from Kitty graphics support. Bubble Tea's [image-support issue](https://github.com/charmbracelet/bubbletea/issues/163) remains open at audit time, so ordinary styled view strings should not be assumed to preserve arbitrary graphics commands. See [Ultraviolet's architecture](https://github.com/charmbracelet/ultraviolet).

The help design should use a consistent palette, clear title, grouped shortcuts, highlighted key labels, visible focus, scrolling or filtering on smaller screens, and a persistent close hint. Show only implemented commands. Support both keyboard and mouse interaction. Render help locally, independently of browser capture, and restore the current browser image correctly when help closes.

[Documentation home](../README.md)
