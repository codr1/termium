# Quickstart

From your terminal to your first page in one command. Linux x86-64 and macOS Apple Silicon/Intel.

## Install and open

```bash
curl -fsSL https://termium.dev/install | bash
```

Setup supplies the private runtime, downloads and verifies Chromium and Vimium, configures your shell, and opens Termium. No development tools, separate browser installation, or sudo required. The installer checks platform compatibility before activation; see [installation](installation.md) for requirements and troubleshooting.

## Find your way around

- **Ctrl+L**: enter an address, then press **Enter**.
- **f**: show link hints, then type a link’s label.
- **j / k**: scroll down / up. Click a text field to type normally.
- **t**: open a tab. **J / K**: switch tabs. **x**: close the current tab.
- **F6**: toggle **Mouse keys**. Arrows move the pointer; **Enter** clicks.
- **F1**: Termium help. **?**: Vimium help.
- **Ctrl+Q**, then **Enter**: quit.

The top bar also has tabs, Back, Forward, Reload, an address field, and Menu. Mouse clicks, dragging, and scrolling work alongside keyboard navigation.

Kitty or sixel graphics are selected automatically, with an ASCII graphics fallback when neither is available. To try the fallback explicitly, use `termium --renderer tcell` in a new terminal.

## Come back anytime

In a new terminal, run this from any directory:

```bash
termium example.com
```

Termium remembers your chosen home page when you run `termium --set-homepage https://example.com`. Tabs and browsing profiles do not persist after quitting yet.

To update, close running Termium sessions and rerun the install command. Run `termium --doctor` if startup fails.

[More controls](getting-started.md) · [Terminal support](terminals.md) · [Installation details](installation.md)
