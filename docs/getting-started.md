# Getting started

New here? Start with [Quickstart](quickstart.md). This guide covers everyday browsing in more detail.

## Open Termium

For an existing working installation:

```bash
termium
```

In a new terminal after [installation](installation.md), `termium` works from any directory. Termium opens its welcome page with an ASCII browser logo, navigation keys, and links to the project. The tab and navigation bars stay at the top. To open a particular page immediately:

```bash
termium example.com
```

```text
1 Example Domain ×                                   [≡]  [+]
[Back] [Forward] [Reload]  https://example.com              [Menu]
                         Webpage
────────────────────────────────────────────────────────────────
Ready · Ctrl+L: address · F10: menu
```

The bar shows the current URL, including redirects. Back and Forward reflect Chromium's history; Reload becomes Stop during navigation. On narrow terminals the buttons become compact, then secondary controls move into Menu.

## Choose your home page

By default, startup and new tabs open the Termium welcome page. It opens the compact browser page at `termium.dev/welcome`, separate from the public product website, when that page is available. A bundled copy keeps the key legend usable offline. Once you type, click, or scroll, an arriving website response cannot take you away from the page.

Save your own home page once:

```bash
termium --set-homepage https://example.com
```

This applies to startup, new tabs, and **Menu → Home** / **Alt+Home**. Use `termium --set-homepage default` to restore Termium's page, `about:termium` for the bundled page without a website request, or `about:blank` for a blank page. Custom websites open directly and have ordinary browser network-error behavior.

`termium example.com` opens that address once without changing your saved home page. `--homepage URL` overrides the home page for one session; `TERMIUM_HOMEPAGE` can supply it through the environment. Precedence is flag, environment, saved setting, default.

Settings are saved in `~/.config/termium/settings.json` on Linux (respecting `XDG_CONFIG_HOME`) and `~/Library/Application Support/termium/settings.json` on macOS. The command creates the file for you. For an explicitly shared `--tcp` server, configure its home page using the server's `--homepage` option.

## Visit a website

Press **Ctrl+L** or click the address field. Its contents are selected, so start typing to replace them. Enter an address such as `example.com`, then press **Enter**. Termium adds `https://` when you omit the scheme; use `http://` explicitly for a local HTTP service. The field accepts URLs, not search queries.

**Escape** cancels editing. Left/Right, Home/End, Backspace/Delete, Ctrl+A (select all), and Ctrl+U (clear) work in the address field. Unicode text and bracketed terminal paste are supported. Tab and Shift+Tab move among toolbar controls; Enter activates the focused control.

| Shortcut | Action |
| --- | --- |
| Ctrl+L | Edit address |
| Ctrl+T / Ctrl+W | New tab / close tab |
| Alt+Left / Alt+Right | Back / Forward |
| Alt+Home | Open your home page |
| Ctrl+R or F5 | Reload / Stop |
| F10 | Open or close Menu |
| F1 | Shortcut help |
| F6 | Toggle mouse keys |
| Ctrl+Q | Quit confirmation |
| Escape three times rapidly | Emergency exit, including during dialogs |

Use Menu when a terminal intercepts a shortcut. Menu supports mouse clicks, Up/Down, Tab/Shift+Tab, Enter, and Escape. It contains tab controls, navigation, address, mouse keys, help, and Quit.

## Interact with a page

Click a field and type normally. Page keys include arrows, Tab/Shift+Tab, Enter, Backspace/Delete, Home/End, Page Up/Down, and modifier combinations that your terminal can report. The application shortcuts above remain reserved. Outside text fields, bundled [Vimium](vimium.md) handles navigation: **f** shows link hints, **j/k** scroll, **t** opens a tab, **J/K** switch tabs, and **x/X** close/reopen a tab. Press **?** for Vimium help or **F1** for Termium help.

Mouse support includes left/right/middle buttons, hover, double/triple clicks, held-button dragging, wheel scrolling, and back/forward buttons when reported by the terminal. Shift+wheel scrolls horizontally. Drags remain captured until release, including a release outside the page area. Pointer accuracy is limited to the center of a terminal cell.

Use your terminal's paste command. Bracketed paste inserts literal text; pasted text cannot activate Termium shortcuts. Middle-click opens links in tabs; select them from the top row or all-tabs picker. Host clipboard integration and native browser file choosers remain limited; see [Vimium limitations](vimium.md#help-and-limits).

## Mouse keys

Press **F6** or choose **Menu → Mouse keys**. A visible cursor and bottom-row instructions indicate this explicit mode. To right-click, move the pointer with arrows or **h/j/k/l**, then press **r**. The bindings below apply while mouse keys are on.

| Key | Action |
| --- | --- |
| Arrows or h/j/k/l | Move one cell |
| Shift+arrow | Move five cells |
| Enter | Left-click |
| Space | Hold/release the left button for dragging |
| r | Right-click |
| m | Middle-click |
| u / d | Scroll up / down |
| Page Up / Page Down | Scroll approximately half a viewport |
| Escape or F6 | Leave mouse keys mode and release held buttons |

Leave mouse keys mode before typing into a page field. Outside this mode, arrows go to the webpage normally.

## Respond to dialogs

Website alerts, confirmations, and prompts appear inside the terminal. Prompts start with their text field focused and the default selected. Type a replacement, use Tab/Shift+Tab to select a button, then Enter, or click a button. Escape dismisses an alert or cancels a confirmation/prompt. Resize and Ctrl+Q remain available.

Graphics temporarily pause behind menus and dialogs so they cannot cover local controls. They resume when the overlay closes.

## Quit

Press **Ctrl+Q** or choose **Menu → Quit**. Press Enter/Y or click Quit to exit; Escape/N or Stay cancels. This closes the managed browser session and does not yet ask the webpage about unsaved changes. Rapid triple-Escape is an immediate emergency exit.

## Display options

Automatic selection probes for Kitty and sixel support with bounded timeouts, then falls back to ASCII graphics. Manual overrides are available:

```bash
termium --renderer kitty
termium --renderer sixel
termium --renderer tcell
```

Run one of these alternatives. ASCII graphics approximate the screenshot using colored blocks; it is not a readable-text browser mode. See [terminal support](terminals.md). Run `termium --help` for all options. A splash appears only when explicitly requested with `--splash path/to/image.jpg`.

[Documentation home](README.md)
