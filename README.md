# Termium

Browse the web inside your terminal.

[Website](https://termium.dev/) · [User guides](https://termium.dev/docs/) · [Installation](https://termium.dev/docs/installation/)

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

- The welcome page puts the key legend at your fingertips.
- Press **Ctrl+L**, type an address, and press **Enter**.
- Use the top bar for Back, Forward, Reload/Stop, and Menu.
- Press **f** for link hints, **j/k** to scroll, and **?** for Vimium help.
- Use **t**, **J/K**, and **x/X** to create, switch, close, and reopen tabs.
- Click a page field to focus it, then type normally.
- Press **F6** for keyboard pointer control; press it again to return to typing.
- Press **Ctrl+Q**, then **Enter** to quit.

Save a custom startup and new-tab page with `termium --set-homepage https://example.com`.

Read the [getting started guide](docs/getting-started.md) for controls and display options.

## Documentation

| Guide | What it covers |
| --- | --- |
| [Installation](docs/installation.md) | One-command setup, availability, and platform targets |
| [Getting started](docs/getting-started.md) | Opening pages, typing, dialogs, and quitting |
| [Terminal support](docs/terminals.md) | Graphics modes and compatibility |
| [Troubleshooting](docs/troubleshooting.md) | Display issues, startup failures, and bug reports |
| [Vimium and tabs](docs/vimium.md) | Bundled keyboard navigation, tabs, mouse coexistence, and limitations |

## What's next

1. **One-command setup:** no manual dependency installation, configuration edits, or separate server startup on supported Linux and macOS systems.
2. **Browser polish:** expand terminal compatibility testing, persistent browsing sessions, and clipboard integration around bundled Vimium and tabs.
3. **Windows:** add native installation and terminal support after Linux and macOS.

The [installation plan](docs/plans/one-command-install.md) and [browser UI plan](docs/plans/browser-ui.md) define the work and release checks. The development build includes the installer, tab UI, and Vimium; public release availability is tracked separately.

## Development

See [development](docs/development.md) for building, [testing](docs/testing.md) for the `npm test` suite and CI coverage, and [architecture](docs/architecture.md) for the rendering pipeline.

Found a bug? Follow the [reporting guide](docs/troubleshooting.md#report-a-problem) and [open an issue](https://github.com/codr1/termium/issues).

## License

The project's current declared license is CC BY-ND. An OSS-compatible license and a standalone license file are release prerequisites; this documentation update does not change the license.
