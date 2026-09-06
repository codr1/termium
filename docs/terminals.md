# Terminal support

Termium displays browser screenshots inside the terminal. Image quality and responsiveness depend on the terminal's graphics support.

## Display modes

| Mode | Intended use | Manual override |
| --- | --- | --- |
| Kitty graphics | Terminals implementing the Kitty graphics protocol, including Kitty and Ghostty | `termium --renderer kitty` |
| Sixel graphics | Terminals configured to support sixel, including compatible xterm builds | `termium --renderer sixel` |
| Character display | Displays the screenshot with character blocks; useful when graphics are unavailable | `termium --renderer tcell` |

Character display is an image approximation, not a text or accessibility view of the webpage. Small text may be difficult to read.

The default is `auto`: bounded probes check Kitty support and sixel device attributes, then fall back to character display. Character mode skips graphics calibration entirely. If graphics do not appear, use a manual override. Menus and dialogs temporarily pause graphics output to keep local controls visible.

## Compatibility status

Kitty and Ghostty are primary graphics targets. A release-tested matrix of terminal versions, Linux distributions, and macOS versions has not been published yet. The names above describe intended protocol compatibility, not certification of every version.

Run Termium directly in a terminal while diagnosing display problems. SSH, tmux, and screen introduce additional graphics and input behavior that still needs testing. Windows terminal support will follow the Linux and macOS releases.

## Sixel performance

The default palette is `adaptive`. For a potentially faster sixel display with a fixed color palette, try:

```bash
termium --renderer sixel --palette websafe
```

The tradeoff is color fidelity. Performance depends on page content, window size, and the terminal; there is no guaranteed frame rate.

Automatic selection and graceful fallback are requirements for the [one-command release](plans/one-command-install.md). Normal setup should not require renderer flags.

[Documentation home](README.md)
