# Termium

Browse the web inside your terminal.

Termium renders a real Chromium browser in your terminal, with keyboard and mouse interaction. It supports Kitty and sixel graphics, plus a character-based display mode.

## Install

The native installer packages Termium, Node, Chromium, and Linux browser libraries/fonts. It verifies the release, checks browser startup, and installs a permanent `termium` command. The first public release using this installer has not been published yet.

Contributors with the [build toolchain](docs/development.md) can build and install the current checkout with:

```bash
npm run install:local
```

See [installation](docs/installation.md) for platform requirements, updates, and storage.

## Browse

Run `termium` from any directory, or `termium example.com` to open a page directly.

- Press **Ctrl+L**, type an address, and press **Enter**.
- Use the top bar for Back, Forward, Reload/Stop, and Menu.
- Click a page field to focus it, then type normally.
- Press **F6** for keyboard pointer control; press it again to return to typing.
- Press **Ctrl+Q**, then **Enter** to quit.

Read the [getting started guide](docs/getting-started.md) for controls and display options.

## Documentation

| Guide | What it covers |
| --- | --- |
| [Installation](docs/installation.md) | One-command setup, availability, and platform targets |
| [Getting started](docs/getting-started.md) | Opening pages, typing, dialogs, and quitting |
| [Terminal support](docs/terminals.md) | Graphics modes and compatibility |
| [Troubleshooting](docs/troubleshooting.md) | Display issues, startup failures, and bug reports |
| [Upcoming browser UI](docs/vimium.md) | Planned native Vimium-style navigation and browser controls |

## What's next

1. **One-command setup:** no manual dependency installation, configuration edits, or separate server startup on supported Linux and macOS systems.
2. **Browser UI and keyboard navigation:** implement familiar Vimium shortcuts natively with broad command coverage, building on the current address bar, back/forward controls, and menu.
3. **Windows:** add native installation and terminal support after Linux and macOS.

The [installation plan](docs/plans/one-command-install.md) and [browser UI plan](docs/plans/browser-ui.md) define the work and release checks. These are planned capabilities, not features of the current build.

## Development

See [development](docs/development.md) for building, [testing](docs/testing.md) for the `npm test` suite and CI coverage, and [architecture](docs/architecture.md) for the rendering pipeline.

Found a bug? Follow the [reporting guide](docs/troubleshooting.md#report-a-problem) and [open an issue](https://github.com/codr1/termium/issues).

## License

The project's current declared license is CC BY-ND. An OSS-compatible license and a standalone license file are release prerequisites; this documentation update does not change the license.
