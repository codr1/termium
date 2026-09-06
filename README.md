# Termium

Browse the web inside your terminal.

Termium renders a real Chromium browser in your terminal, with keyboard and mouse interaction. It supports Kitty and sixel graphics, plus a character-based display mode.

## Install

Our target is one command on Linux and macOS: paste it, let setup finish, and start browsing. Termium will handle its browser, runtime, and terminal configuration automatically.

**The one-command release is not ready yet.** The existing installer has known packaging failures. Until a release passes installation testing, use the development instructions linked below. A supported install command will appear here when available.

See [installation status](docs/installation.md) for platform targets and what the installer will do. Contributors can [build the current development version](docs/development.md).

## Browse

For an existing working installation, run `termium`. From a source checkout, run `./client/termium`.

- Press **Enter** to continue past the current splash screen.
- Press **Ctrl+L**, then **Ctrl+U**, type an address, and press **Enter**.
- Click a page field to focus it, then type normally.
- Press **Escape** in normal browsing mode to open the exit confirmation, then **Enter** to quit.

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
2. **Browser UI and keyboard navigation:** implement familiar Vimium shortcuts natively with broad command coverage, plus an address bar, back/forward controls, and a menu under consideration.
3. **Windows:** add native installation and terminal support after Linux and macOS.

The [installation plan](docs/plans/one-command-install.md) and [browser UI plan](docs/plans/browser-ui.md) define the work and release checks. These are planned capabilities, not features of the current build.

## Development

See [development](docs/development.md) for building and testing, and [architecture](docs/architecture.md) for the rendering pipeline.

Found a bug? Follow the [reporting guide](docs/troubleshooting.md#report-a-problem) and [open an issue](https://github.com/codr1/termium/issues).

## License

The project's current declared license is CC BY-ND. An OSS-compatible license and a standalone license file are release prerequisites; this documentation update does not change the license.
