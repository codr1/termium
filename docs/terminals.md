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

For profiling, `--timings` reports preparation time, capture/queue time, image-write time, and frame age on stderr. These are pipeline measurements, not keypress-to-paint measurements.

The tradeoff is color fidelity. Performance depends on page content, window size, and the terminal; there is no guaranteed frame rate.

Automatic selection and graceful fallback are requirements for the [one-command release](plans/one-command-install.md). Normal setup should not require renderer flags.

[Documentation home](README.md)
