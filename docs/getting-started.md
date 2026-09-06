# Getting started

This guide describes the current development build. For installation availability, see [installation](installation.md).

## Open Termium

For an existing working installation:

```bash
termium
```

To install a contributor build from a source checkout, use `npm run install:local`; afterward, `termium` works from any directory. Termium opens a blank page with its navigation bar at the top. To open a particular page immediately:

```bash
termium example.com
```

```text
[Back] [Forward] [Reload]  https://example.com              [Menu]
────────────────────────────────────────────────────────────────
                         Webpage
────────────────────────────────────────────────────────────────
Ready · Ctrl+L: address · F10: menu
```

The bar shows the current URL, including redirects. Back and Forward reflect Chromium's history; Reload becomes Stop during navigation. On narrow terminals the buttons become compact, then secondary controls move into Menu.

## Visit a website

Press **Ctrl+L** or click the address field. Its contents are selected, so start typing to replace them. Enter an address such as `example.com`, then press **Enter**. Termium adds `https://` when you omit the scheme; use `http://` explicitly for a local HTTP service. The field accepts URLs, not search queries.

**Escape** cancels editing. Left/Right, Home/End, Backspace/Delete, Ctrl+A (select all), and Ctrl+U (clear) work in the address field. Unicode text and bracketed terminal paste are supported. Tab and Shift+Tab move among toolbar controls; Enter activates the focused control.

| Shortcut | Action |
| --- | --- |
| Ctrl+L | Edit address |
| Alt+Left / Alt+Right | Back / Forward |
| Ctrl+R or F5 | Reload / Stop |
| F10 | Open or close Menu |
| F1 | Shortcut help |
| F6 | Toggle keyboard pointer |
| Ctrl+Q | Quit confirmation |
| Escape three times rapidly | Emergency exit, including during dialogs |

Use Menu when a terminal intercepts a shortcut. Menu supports mouse clicks, Up/Down, Tab/Shift+Tab, Enter, and Escape. It contains navigation, address, keyboard pointer, help, and Quit.

## Interact with a page

Click a field and type normally. Page keys include arrows, Tab/Shift+Tab, Enter, Backspace/Delete, Home/End, Page Up/Down, and modifier combinations that your terminal can report. The application shortcuts above remain reserved. Printable letters pass through to the page; native Vimium hinting, find, modes, and tab commands are [future work](vimium.md).

Mouse support includes left/right/middle buttons, hover, double/triple clicks, held-button dragging, wheel scrolling, and back/forward buttons when reported by the terminal. Shift+wheel scrolls horizontally. Drags remain captured until release, including a release outside the page area. Pointer accuracy is limited to the center of a terminal cell.

Use your terminal's paste command. Bracketed paste inserts literal text; pasted text cannot activate Termium shortcuts. Termium does not yet provide clipboard copy commands or handle native browser file choosers and additional windows. Middle-click links may create a Chromium tab that this single-page UI cannot select yet.

## Use a keyboard as a mouse

Press **F6** or choose **Menu → Keyboard pointer**. A visible cursor and bottom-row instructions indicate this explicit mode.

| Keys in pointer mode | Action |
| --- | --- |
| Arrows or h/j/k/l | Move one cell |
| Shift+arrow | Move five cells |
| Enter | Left-click |
| Space | Hold/release the left button for dragging |
| r / m | Right-click / middle-click |
| u / d | Scroll up / down |
| Page Up / Page Down | Scroll approximately half a viewport |
| Escape or F6 | Leave pointer mode and release held buttons |

Leave pointer mode before typing into a page field. Outside this mode, arrows go to the webpage normally.

## Respond to dialogs

Website alerts, confirmations, and prompts appear inside the terminal. Prompts start with their text field focused and the default selected. Type a replacement, use Tab/Shift+Tab to select a button, then Enter, or click a button. Escape dismisses an alert or cancels a confirmation/prompt. Resize and Ctrl+Q remain available.

Graphics temporarily pause behind menus and dialogs so they cannot cover local controls. They resume when the overlay closes.

## Quit

Press **Ctrl+Q** or choose **Menu → Quit**. Press Enter/Y or click Quit to exit; Escape/N or Stay cancels. This closes the managed browser session and does not yet ask the webpage about unsaved changes. Rapid triple-Escape is an immediate emergency exit.

## Display options

Automatic selection probes for Kitty and sixel support with bounded timeouts, then falls back to character display. Manual overrides are available:

```bash
termium --renderer kitty
termium --renderer sixel
termium --renderer tcell
```

Run one of these alternatives. Character display approximates the screenshot using colored blocks; it is not a readable-text browser mode. See [terminal support](terminals.md). Run `termium --help` for all options. A splash appears only when explicitly requested with `--splash path/to/image.jpg`.

[Documentation home](README.md)
