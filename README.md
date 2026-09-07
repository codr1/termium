# Termium

Browse the web inside your terminal.

[Website](https://termium.dev/) · [Quickstart](https://termium.dev/docs/quickstart/) · [User guides](https://termium.dev/docs/) · [Installation](https://termium.dev/docs/installation/)

Termium renders a real Chromium browser in your terminal, with keyboard and mouse interaction. It supports Kitty and sixel graphics, plus a character-based display mode.

## Quickstart

Paste this into your terminal on Linux x86-64 or macOS (Apple Silicon or Intel):

```bash
bash -o pipefail -c 'curl -fsSL https://termium.dev/install | bash' && export PATH="$HOME/.local/bin:$PATH" && "$HOME/.local/bin/termium" --first-run
```

Setup downloads Termium, Chromium, and Vimium, verifies them, and opens the browser. No Node, Go, browser installation, or sudo required. The permanent `termium` command works from any directory.

This is an early release. Linux requires glibc and working Chromium sandbox support; the installer checks compatibility; native Windows and Linux ARM64 packages are not available yet.

See [installation](docs/installation.md) for platform requirements, updates, and storage.

## Browse

Run `termium` from any directory, or `termium example.com` to open a page directly.

- The welcome page puts the key legend at your fingertips.
- Press **Ctrl+L**, type an address, and press **Enter**.
- Use the top bar for Back, Forward, Reload/Stop, and Menu.
- Press **f** for link hints, **j/k** to scroll, and **?** for Vimium help.
- Use **t**, **J/K**, and **x/X** to create, switch, close, and reopen tabs.
- Click a page field to focus it, then type normally.
- Press **F6** for mouse keys; press it again to return to typing.
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

1. **Distribution polish:** expand clean-machine testing and automate compatible dependency updates.
2. **Browser polish:** expand terminal compatibility testing, persistent browsing sessions, and clipboard integration around bundled Vimium and tabs.
3. **Windows:** add native installation and terminal support after Linux and macOS.

The [installation plan](docs/plans/one-command-install.md) and [browser UI plan](docs/plans/browser-ui.md) define the work and release checks. See [GitHub Releases](https://github.com/codr1/termium/releases) for native packages and release notes.

## Development

See [development](docs/development.md) for building, [testing](docs/testing.md) for the `npm test` suite and CI coverage, and [architecture](docs/architecture.md) for the rendering pipeline.

Found a bug? Follow the [reporting guide](docs/troubleshooting.md#report-a-problem) and [open an issue](https://github.com/codr1/termium/issues).

## License

[MIT](LICENSE). Third-party components retain their own licenses, included with their sources and release packages.
