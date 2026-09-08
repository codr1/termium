# Terminal support

Termium displays browser screenshots inside the terminal. Image quality and responsiveness depend on the terminal's graphics support.

## Display modes

| Mode | Intended use | Manual override |
| --- | --- | --- |
| Kitty graphics | Terminals implementing the Kitty graphics protocol, including Kitty and Ghostty | `termium --renderer kitty` |
| Sixel graphics | Terminals configured to support sixel, including compatible xterm builds | `termium --renderer sixel` |
| ASCII graphics | Displays the screenshot with character blocks; useful when graphics are unavailable | `termium --renderer tcell` |

ASCII graphics approximate the screenshot using colored blocks. This is not a text or accessibility view of the webpage. Small text may be difficult to read.

The default is `auto`: bounded probes check Kitty support and sixel device attributes, then fall back to ASCII graphics. ASCII mode skips graphics calibration entirely. If graphics do not appear, use a manual override. Menus and dialogs temporarily pause graphics output to keep local controls visible.

## Compatibility status

Kitty and Ghostty are primary graphics targets. A release-tested matrix of terminal versions, Linux distributions, and macOS versions has not been published yet. The names above describe intended protocol compatibility, not certification of every version.

Run Termium directly in a terminal while diagnosing display problems. SSH, tmux, and screen introduce additional graphics and input behavior that still needs testing. Windows terminal support will follow the Linux and macOS releases.

## Sixel performance

Sixel defaults to the fixed `websafe` palette for speed. To request it explicitly:

```bash
termium --renderer sixel --palette websafe
```

Use `--palette adaptive` for image-specific color selection at a higher CPU cost, or `--palette plan9` for another fixed palette.

Screenshots are prepared off the input loop. Unchanged images are not retransmitted, and editing the address or moving the pointer with mouse keys does not resend browser pixels. Capture slows to measured preparation/output throughput and pauses behind menus and dialogs. Terminal output remains serialized; a slow terminal or SSH connection can still stall a write.

The Sixel palette trades color fidelity for speed. Performance depends on page content, window size, and the terminal; there is no guaranteed frame rate.

## Comparing graphics performance

Both renderers use the same capture scheduler, preparation worker, and terminal writer. Capture defaults differ for a reason: Kitty can consume PNG directly, while Sixel already needs decoded pixels and normally uses faster JPEG capture. JPEG can lower capture time yet increase Kitty’s terminal traffic after decoding and recompression. PNG preserves fine text and original screenshot colors; Sixel still quantizes them to its palette.

In builds supporting `--capture-format` (check `termium --help`), compare the same page and viewport with an explicit source:

```bash
termium --renderer sixel --capture-format png --timings 2>sixel-timings.log
termium --renderer kitty --capture-format png --timings 2>kitty-timings.log
```

Repeat both with `--capture-format jpeg` to compare the other source. Omit the flag, or use `--capture-format auto`, for the normal defaults. Use the same terminal emulator when it supports both protocols; comparing foot with Ghostty also measures differences between those terminals. Keep the browser pixel dimensions equal, as reported in the logs.

`--timings` reports the same fields for both renderers: capture/RPC duration, queue wait, preparation time, source/payload byte counts, image-write duration, and frame age. Preparation includes all graphics encoding and framing. No renderer performs a per-frame disk sync. Redirect stderr to a file; logging still has a cost. A completed write only means the terminal accepted the bytes, not that it painted them. These measurements do not establish visible FPS or keypress-to-paint latency.

Normal use is local: network bandwidth is not the target of these comparisons. Byte counts help explain CPU, copying, and terminal processing costs; larger payloads are acceptable when they improve responsiveness. Current logs do not establish that local transport is a bottleneck.

## Performance and profiling switches

All of these are optional. Run `termium --help` to check which switches your installed build supports.

| Switch | What to try | Scope or cost |
| --- | --- | --- |
| `--renderer auto\|kitty\|sixel\|tcell` | Select automatic graphics, Kitty, Sixel, or ASCII graphics. | Default: `auto`. Comparing different terminals also measures their implementation differences. |
| `--capture-format auto\|png\|jpeg` | Compare source image encoding independently of the renderer. | Default: `auto` selects PNG for Kitty and JPEG otherwise. PNG is lossless; JPEG can reduce capture work but adds decode/recompression work for Kitty. |
| `--palette websafe\|plan9\|adaptive` | Compare fixed palettes with image-specific color selection. | Sixel only; default: `websafe`. Adaptive selection costs more CPU. |
| `--timings` | Record capture/RPC, queue, preparation, write, byte-count, and frame-age diagnostics. | Writes stderr; redirect it to a file. Logging adds overhead and does not measure screen paint. |
| `--cpuprofile client.prof` | Record a Go client CPU profile. | Covers the client, not Node, Chromium, or the terminal. Profiling adds overhead. |
| `--trace client.trace` | Record a Go execution trace for scheduling, blocking, and GC investigation. | Client only. Prefer a separate run from CPU profiling and ordinary timing comparisons. |
| `--splash NONE` | Skip the splash when repeating a scenario. | Keep startup measurements separate from steady browsing. |
| `--save-screenshots` | Save captured source images to inspect quality and dimensions. | Writes files in the working directory; use a separate diagnostic run, not a performance baseline. |

For example, run these separately, navigate through the same scenario, then quit normally with `Ctrl+Q` and confirm to finish each recording:

```bash
termium --renderer sixel --capture-format jpeg --palette websafe --splash NONE --timings 2>sixel-timings.log
termium --renderer kitty --capture-format png --splash NONE --cpuprofile kitty.prof
termium --renderer kitty --capture-format png --splash NONE --trace kitty.trace
```

Contributors with Go installed can inspect the recordings using `go tool pprof -top kitty.prof` and `go tool trace kitty.trace`. Rebuild the development client with `npm run build:client` after `npm test` before profiling: the test suite leaves a race-instrumented binary. Use `./client/termium` for that checkout; the installed `termium` command is a separate copy.

JPEG quality, Chromium's fast-encoding setting, and the 24 FPS ceiling are implementation settings, not user-facing switches. There is currently no CLI control for FPS, JPEG quality, compression level, or resolution; resize the terminal and check logged browser pixel dimensions for size comparisons. Adaptive pacing may run below the ceiling.

See [testing](testing.md#comparing-capture-and-renderer-preparation) for repeatable benchmarks and [the planned full local run](testing.md#full-local-performance-run-planned).

Automatic selection and graceful fallback are requirements for the [one-command release](plans/one-command-install.md). Normal setup should not require renderer flags.

[Documentation home](README.md)
