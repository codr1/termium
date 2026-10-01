# Troubleshooting

This guide applies to the current development build. See [installation](installation.md) for release availability.

## Check the installation

Run `termium --doctor`. It checks the packaged runtime, browser sandbox on Linux, input, screenshots, bundled Vimium, and tab navigation. A contributor installation includes its own Node and Chromium; use `npm run install:local` to update it after source changes.

## Pressing f does nothing

Leave a text field with Escape, and leave mouse keys mode with F6 if its cursor is visible. Try an ordinary HTTP/HTTPS webpage with visible links. Protected browser pages cannot load Vimium. Run `termium --doctor` to check that the installed bundle includes the extension; an older installed executable will not gain new features from a source rebuild alone.

## The terminal shows no page or displays escape characters

Try one of the manual display options in [terminal support](terminals.md). For example:

```bash
termium --renderer tcell --splash NONE
```

If you use tmux, screen, or SSH, also try launching directly in the local terminal. Include both results when reporting the issue. Character mode skips terminal graphics queries; automatic detection uses bounded timeouts.

## The page feels slow

For sixel, try `--palette websafe`. A smaller terminal window can also reduce rendering work. Report the terminal dimensions, selected renderer, and whether the slowdown happens on a static page, while typing, or during navigation.

## Keyboard shortcuts behave unexpectedly

Arrow keys normally go to the page. If they move a local cursor, press F6 or Escape to leave mouse keys mode. Termium reserves Ctrl+L, Ctrl+T, Ctrl+W, Ctrl+Q, Ctrl+R, Alt+Left/Right, Alt+Home, F1, F5, F6, F8, F10, and F12, plus Ctrl+C while page input is focused; other supported page keys pass through. Terminals may intercept modifiers or mouse buttons before Termium receives them. Consult the [current controls](getting-started.md) and [Vimium navigation](vimium.md).

## Copy, paste, or developer tools are missing

These tools are on `main` and are not in the latest published release yet. A
source checkout does not update an installed executable: rebuild/install from
source as described in [development](development.md). Open **F10 → Developer
tools**, **Copy selected text**, or **Extensions** if your terminal intercepts
the function keys. Keep moving down in a short menu to reveal additional items.

If copy says “No text selected”, select text inside the webpage first. If it says
“Copy sent to terminal clipboard” but the clipboard does not change, check your
terminal's OSC 52 permissions. That message confirms sending, not acceptance.
Paste with the terminal's paste shortcut, not bare Ctrl+V. Extensions must be
unpacked directories supplied with `--extension`; the menu opens their manager.
See [Browser tools](browser-tools.md) for the full behavior and limits.

## Termium stops responding

Try Ctrl+Q, then Enter. Rapidly pressing Escape three times also exits through modal dialogs. If the application cannot process input, open another terminal and identify the affected Termium process before stopping it; avoid stopping unrelated browser processes.

If the shell's display remains garbled after Termium exits, run `reset` in that terminal. Report whether the problem occurred during startup, navigation, a website dialog, or shutdown.

## A second window controls the same page

Normal launches use separate managed browser sessions and private sockets. If two terminals control the same browser, check whether you explicitly selected the same shared `--tcp` server. Run `termium` without that option for an independent session.

## Installation fails inside Docker

If setup stops with `Target closed` and `browser check failed` inside a container, Chromium's sandbox is blocked. Docker's default seccomp profile blocks the namespaces the sandbox needs. Start the container with `--security-opt seccomp=unconfined` and run the installer again as a normal user.

## Report a problem

Include:

- The output of `termium --version` or `./client/termium --version`.
- Your operating system, CPU architecture, terminal name, and terminal version.
- Whether you are using SSH or a terminal multiplexer.
- What you did, what you expected, and what happened.
- The error message and selected renderer, if known.

For an existing working installation, collect a client log with:

```bash
termium --debug --logfile termium-debug.log
```

Debug logs may contain URLs and typed text. Remove private information before attaching them to a [GitHub issue](https://github.com/codr1/termium/issues).

[Documentation home](README.md)
