# Vimium-style navigation and browser controls

**Vimium command coverage remains planned.** The development build now has a top address bar, history controls, Reload/Stop, Menu, and explicit keyboard pointer control. Page keys currently pass through directly; `h/j/k/l` move a pointer only after F6. The full normal/insert/hint/find and tab model below is not implemented. See [getting started](getting-started.md) for today's controls.

Termium will provide built-in keyboard navigation using familiar Vimium shortcuts. It will be ready when Termium opens, without installing a browser extension. The first UI release has a defined core command set; broader command coverage follows later.

## Keyboard browsing

The planned first-release shortcuts are inspired by [Vimium's default bindings](https://github.com/philc/vimium#keyboard-bindings), with Termium-specific application controls:

| Keys | Intended action |
| --- | --- |
| `h` / `j` / `k` / `l` | Scroll left / down / up / right |
| `d` / `u` | Scroll half a page down / up |
| `gg` / `G` | Go to the top / bottom |
| `f` / `F` | Follow a labeled link here / in another tab |
| `H` / `L` | Go back / forward |
| `r` | Reload |
| `/`, then `n` / `N` | Find text and move between matches |
| `o` / `O` | Enter a URL here / in a new foreground tab |
| Ctrl+L | Edit the current URL |
| `i` / `Escape` | Enter / leave insert mode |
| `?` | Show shortcut help |
| `t` / `x` / `X` | Open a tab / close it / reopen the last closed URL |
| `J` / `K` | Select the previous / next tab |
| F10 | Open the menu |
| Ctrl+Q | Quit Termium |

These are required for the first UI release. Counts such as `3j` repeat scrolling, history movement, and tab selection. `F` opens navigable links in background tabs; use `f` for buttons and input fields. Reopening a tab restores its URL, not unsaved forms. The address field initially accepts URLs only; it does not search bookmarks, history, or a search engine.

Marks, visual selection, clipboard commands, bookmarks/history search, custom mappings, site exclusions, and browser-window management follow in later releases. They are not part of the first-release command list.

When a text field has focus, typing must enter text normally. Escape must cancel the active mode or UI without unexpectedly closing Termium.

In normal mode, unbound letters are ignored. Press `i` to pass keys through to a website, and Escape to return to normal mode. Ctrl+L, F10, and Ctrl+Q remain reserved for Termium. Quit respects a website's unsaved-changes confirmation; canceling that confirmation keeps the session open.

## Browser controls

The current development interface has a compact bar above the page:

```text
[Back] [Forward] [Reload]  [ https://example.com             ] [Menu]
```

This establishes the navigation controls; the full Vimium release remains subject to the command matrix below.

- The address field shows the current page and is reachable by keyboard or mouse.
- Back and Forward are available when the page has a history entry in that direction.
- The current menu contains navigation, address, pointer mode, shortcut help, and Quit. New tab, Close tab, and Reopen tab remain planned; keybinding settings follow customization.
- Page content keeps the remaining space; narrow windows keep essential controls reachable.

Keyboard navigation and the visible controls must stay in sync. Following a link or moving through history must update the displayed address.

## Compatibility expectations

“As full as possible” means expanding coverage of Vimium's commands and naming exceptions. This is a native Termium implementation; importing Vimium configuration files is not currently promised. Ordinary page/frame hints and text search are required. Closed shadow roots, specialized editors, browser-owned pages, native file dialogs, clipboard access, and multiple windows need additional investigation. Limitations in these areas cannot waive a required core command on an ordinary webpage.

The [implementation plan](plans/browser-ui.md) records the technical work. Keyboard navigation is part of the same one-command Termium installation.

[Documentation home](README.md)
