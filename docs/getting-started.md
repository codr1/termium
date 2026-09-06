# Getting started

This guide describes the current development build. For installation availability, see [installation](installation.md).

## Open Termium

For an existing working installation:

```bash
termium
```

From a source checkout, use `./client/termium` instead. Press **Enter** at the splash screen. The current build opens a demonstration website; use the address prompt to choose another page.

To skip the splash screen:

```bash
termium --splash NONE
```

## Visit a website

1. Press **Ctrl+L** to open the address prompt. It contains the current address.
2. Press **Ctrl+U** to clear it.
3. Type an address such as `example.com`.
4. Press **Enter** to navigate.

Termium adds `https://` if you omit the scheme. Use an explicit `http://` for a local HTTP service. Press **Escape** to cancel address editing.

| While editing an address | Action |
| --- | --- |
| Left / Right | Move within the address |
| Home / End | Move to the beginning or end |
| Backspace / Delete | Remove the preceding or following character |
| Ctrl+U | Clear the address |
| Enter | Open the address |
| Escape | Cancel editing |

The current address editor has limitations with non-ASCII text. A persistent address bar is part of the [planned UI](vimium.md).

## Interact with a page

Click a link to follow it. Click a text field and type to enter text. **Tab**, **Enter**, and **Backspace** are forwarded to the page.

In normal browsing mode, the arrow keys currently move Termium's local cursor; they do not behave as webpage arrow keys. Mouse-wheel scrolling, full modifier-key handling, clipboard integration, and native Vimium-style navigation are not implemented.

## Respond to dialogs

Termium shows website alerts, confirmations, and prompts inside the terminal. Use **Tab** to change focus and **Enter** to activate the selected action. **Escape** dismisses an alert or cancels a confirmation.

## Quit

In normal browsing mode, press **Escape** to open the exit confirmation. Press **Enter** to confirm, or **Escape** to cancel. If you are editing an address, the first Escape cancels editing.

The current build also contains a rapid triple-Escape shortcut, but dialog handling can intercept those keys. Do not rely on it as a universal emergency exit. See [troubleshooting](troubleshooting.md#termium-stops-responding) if the application stops responding.

The next UI phase will reserve Escape for canceling browser and keyboard-navigation modes and use Ctrl+Q or Menu → Quit to exit. Those bindings are planned; use the Escape confirmation flow in the current build.

## Display options

Termium defaults to automatic renderer selection. Manual options are available for troubleshooting:

```bash
termium --renderer kitty
termium --renderer sixel
termium --renderer tcell
```

These are alternatives; run one command. See [terminal support](terminals.md) for details. To list the current command-line options, run `termium --help`.

[Documentation home](README.md)
