# Troubleshooting

This guide applies to the current development build. The [one-command installer](installation.md) is still planned.

## Installation reports missing Node.js or Chrome

The current installer does not manage all dependencies. It also does not download Chrome automatically on first launch, despite that claim in older documentation.

These are known installation gaps. Contributors can use the [development instructions](development.md); the planned release must handle setup automatically.

## The terminal shows no page or displays escape characters

Try one of the manual display options in [terminal support](terminals.md). For example:

```bash
termium --renderer tcell --splash NONE
```

If you use tmux, screen, or SSH, also try launching directly in the local terminal. Include both results when reporting the issue. Character mode skips terminal graphics queries; automatic detection uses bounded timeouts.

## The page feels slow

For sixel, try `--palette websafe`. A smaller terminal window can also reduce rendering work. Report the terminal dimensions, selected renderer, and whether the slowdown happens on a static page, while typing, or during navigation.

## Keyboard shortcuts behave unexpectedly

Arrow keys normally go to the page. If they move a local cursor, press F6 or Escape to leave keyboard pointer mode. Termium reserves Ctrl+L, Ctrl+Q, Ctrl+R, Alt+Left/Right, F1, F5, F6, and F10; other supported page keys pass through. Terminals may intercept modifiers or mouse buttons before Termium receives them. Consult the [current controls](getting-started.md) and [upcoming Vimium navigation](vimium.md).

## Termium stops responding

Try Ctrl+Q, then Enter. Rapidly pressing Escape three times also exits through modal dialogs. If the application cannot process input, open another terminal and identify the affected Termium process before stopping it; avoid stopping unrelated browser processes.

If the shell's display remains garbled after Termium exits, run `reset` in that terminal. Report whether the problem occurred during startup, navigation, a website dialog, or shutdown.

## A second window controls the same page

The current build shares one local browser server. Multiple independent Termium sessions are not yet isolated. Use one session at a time while this is being fixed.

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
