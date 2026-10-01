# Developer tools, clipboard and extensions

These features are on the development branch `main`; they are not in the latest
published release yet. Updating from source makes them available. See
[development setup](development.md) and [release availability](installation.md).

## Find the tools in Menu

Click **Menu** in the top bar or press **F10**. Use Up/Down (or Tab/Shift+Tab)
and Enter, or click an item. Escape closes the menu. On short terminals, keep
moving down to reveal the remaining entries.

| Menu item | What it does | Shortcut |
| --- | --- | --- |
| Developer tools | Opens Chromium's inspector for the current page | F12 |
| Copy selected text | Sends the page's selection to the terminal clipboard | F8 or Ctrl+C in page focus |
| Extensions | Opens Chromium's extension manager to inspect extensions and options | Menu only |
| Mouse keys | Enables keyboard movement, clicking and selection by dragging | F6 |
| Shortcut help | Shows Termium's inline key reference | F1 |

Paste uses your terminal's paste command; there is no menu action that reads the
host clipboard. Loading an unpacked extension uses `--extension` at launch;
**Extensions** opens the manager, not a terminal file picker. Sound is still
being investigated and has no playback or mute control in Menu yet.

## Inspect a page

Press **F12**, or choose **Developer tools** from the F10 menu. Termium opens
Chromium's built-in DevTools in a separate Termium tab, initially showing
Elements. Use its panels to inspect HTML/CSS, run console commands, examine
network requests, and debug scripts. Mouse and keyboard input work in the tools.
Vimium does not run on this browser-internal page.

Switch between the page and its tools using the tab bar or F10 → All tabs.
Ctrl+W closes the tools without closing the inspected page. Closing the inspected
page also removes its tools. Termium keeps one inspector at a time; opening tools
for another page replaces the previous inspector. No debugging TCP port is opened.
Breakpoints pause the inspected page, so its frames and input can stop until you
resume execution in DevTools.

## Select, copy and paste

Select webpage text by dragging with the mouse, using mouse keys (F6, Space to
hold/release a drag), or using the page's keyboard selection controls. Press
**F8** or **Ctrl+C** while page input is focused, or choose **Copy selected text**
in F10. Text selections in inputs, textareas, focused frames and open shadow DOM
are supported. Password fields are excluded. A selection may contain up to 64 KiB
of UTF-8 text; empty selections leave the clipboard alone.

Copy uses the terminal's OSC 52 clipboard support. Your terminal must allow it;
“Copy sent to terminal clipboard” confirms sending the request, not permission
from the terminal. It also works over SSH when the local terminal supports OSC 52.
Copy is plain text, not HTML or images, and does not grant webpages clipboard-read
permission. Ctrl+C in the address editor retains its local editor behavior.

Paste with your **terminal's paste shortcut** (commonly Ctrl+Shift+V on Linux or
Cmd+V on macOS). Termium forwards bracketed paste as literal text to the focused
page input, including Unicode and newlines, without interpreting it as Vimium
commands. A bare Ctrl+V key is not a portable request to read the host clipboard.
Clipboard access from a website's own JavaScript remains subject to Chromium's
permissions; this feature is the explicit Termium copy action.

## Load extensions

Pass an unpacked Chromium extension directory containing `manifest.json`:

```sh
termium --extension /path/to/extension https://example.com
termium --extension /path/to/first --extension "/path/with spaces/second"
```

Directories are resolved from your current working directory. Requested
extensions load before the initial page; Vimium remains installed. Invalid or
incompatible extensions produce a browser-startup error rather than being skipped.
Only load extensions you trust with the pages you browse.

Choose **Extensions** from F10 to view Chromium's extension manager, including
extension details and options. Loading is session-specific: supply the arguments
again on the next launch. This does not install CRX files or implement Chrome Web
Store installation, toolbar action buttons, or profile persistence. With a remote
Termium server, configure `--extension` on that server instead of the client.
