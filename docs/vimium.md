# Vimium and tabs

Setup installs upstream **Vimium 2.4.2** alongside Chromium. There is no extension to install or configure. Vimium handles webpage navigation; Termium supplies the tab strip, address bar, terminal help, and mouse keys.

## Start here

On a webpage, press **f** to show labels on links, buttons, and fields. Type a label to activate it. **Escape** cancels. Click or hint a text field and type normally; press Escape to return to navigation. Pasted text is inserted literally.

| Keys | Action |
| --- | --- |
| f / F | Activate a hint here / open a link in a background tab |
| h / j / k / l | Scroll left / down / up / right |
| d / u | Scroll down / up half a page |
| gg / G | Top / bottom |
| H / L | Back / forward |
| /, then Enter | Find text |
| n / N | Next / previous match |
| o / O | Search or open an address here / in a new tab |
| t | New tab |
| J / K | Previous / next tab |
| x / X | Close / reopen a tab |
| i | Pass keys to the page until Escape |
| ? | Vimium's full shortcut help |

These are upstream bindings. Counts such as `3j` are supported. Vimium's search box (`o`) supports searching; Termium's **Ctrl+L** address bar accepts URLs.

## Tabs

The top row shows numbered titles, a highlighted selected tab, close targets, an all-tabs picker **[≡]**, and **[+]**. Click a title to select it. The selected tab stays visible when space is tight; the picker lists every tab. **Ctrl+T** opens a tab and **Ctrl+W** closes the selected tab, including on pages where Vimium cannot run. Menu includes reopening closed tabs.

Each tab keeps its own Chromium page, form contents, scroll position, and history. Background links stay in the background. Only the selected tab is captured for terminal rendering. Closing the last tab opens your home page. The default welcome page has a bundled offline copy; [you can save your own home page](getting-started.md#choose-your-home-page). Reopening uses Chromium's session restore; unsaved form recovery is not guaranteed. Tabs and browsing data do not persist after quitting Termium.

## Mouse and mouse keys

Mouse clicks, dragging, scrolling, and OS Mouse Keys continue to work. **F6** toggles Termium's mouse keys; it is a toggle, not a key to hold down. In mouse keys mode, h/j/k/l and arrows move the pointer, Enter clicks, Space holds/releases the left button, and u/d scroll. Escape or F6 leaves mouse keys mode. See [getting started](getting-started.md) for the complete pointer controls.

## Help and limits

**F1** opens terminal help and **F10** opens Menu. These remain usable on browser-owned pages. Termium reserves Ctrl+L, Ctrl+T, Ctrl+W, Ctrl+Q, Ctrl+R, Alt+Left/Right, Alt+Home, F1, F5, F6, and F10. A terminal may intercept keys before Termium receives them.

Vimium cannot inject into Chromium's protected pages, including `chrome://` pages. Termium shows an unavailable status there; use Ctrl+L or Menu. The bundled welcome page supports Vimium. Native file choosers, host clipboard integration, persistent profiles, and full popup-window behavior are not supported commitments yet. Vimium clipboard commands target Chromium's environment, which may differ from the terminal host over SSH or WSL.

Smooth scrolling is disabled and hints use static, high-contrast styling to reduce graphics work. The webpage and Vimium overlays still travel through Chromium screenshots and the selected Kitty, sixel, or character renderer. Character mode cannot make screenshot text as readable as a graphics terminal.

[Upstream provenance and license](../third_party/vimium/TERMIUM.md) · [Architecture](plans/browser-ui.md) · [Documentation home](README.md)
