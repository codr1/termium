# Installation

Paste this one line into bash, zsh, or fish on Linux x86-64 or macOS:

```bash
curl -fsSL https://termium.dev/install | bash
```

Setup installs the private runtime, downloads verified Chromium and Vimium, checks browser startup, and opens Termium. It also makes `termium` available in new terminals. No development tools, manual browser installation, or sudo required.

This is an early release. Check the [platform requirements](#platforms-and-limits) below. [Release notes and native archives](https://github.com/codr1/termium/releases) are on GitHub.

For unattended installation without opening the browser:

```bash
bash -o pipefail -c 'curl -fsSL https://termium.dev/install | bash -s -- --no-launch'
```

To inspect the installer before running it, download it from `https://termium.dev/install`; its source is [scripts/install.sh](../scripts/install.sh).

## Run it

In a new terminal after installation, run this from any directory:

```bash
termium
termium example.com
termium --renderer sixel example.com
```

Options come before the optional address. `--url` remains supported. The server and browser start automatically. Use `termium --doctor` to check the installed runtime, browser sandbox on Linux, input, and screenshot capture without opening the terminal UI.

## Install the development version

From a checkout with the [contributor toolchain](development.md) installed, one command builds, packages, validates, and installs it:

```bash
npm run install:local
```

This is a source-build command for contributors. A release recipient needs no Node, npm, Go, protoc, or Chromium: the installer supplies those runtime dependencies automatically. The platform archive includes Node and the Linux browser libraries/fonts; Chromium and Vimium download during installation. Subsequent launches use the installed copy, independent of the checkout or current directory.

The installer puts the command at `~/.local/bin/termium` and sets up bash, zsh, and fish. Open a new terminal after running the contributor command if your current shell did not already have `~/.local/bin` on PATH. The public installer opens the application immediately using the stable launcher. It cannot change an already-open shell’s PATH; if `termium` is not found there, use `~/.local/bin/termium` or open a new terminal.

## What setup does

- Checks the platform and verifies the archive's SHA-256. Missing or mismatched checksums stop installation.
- Stages the client, server, private Node runtime, and Linux browser libraries/fonts, with glibc supplied by the host.
- Downloads the exact Chromium and Vimium archives approved by the release build over HTTPS. Their SHA-256 checksums are pinned inside the verified app archive.
- Verifies downloads before extraction and caches them under the installation directory’s `downloads/` folder. Repeat installations reuse matching cached archives; corrupt cache entries download again.
- Completes browser and Vimium checks before activating the installation. Download, extraction, or validation failure preserves the previous working version.
- Starts a disposable browser, verifies input and screenshots, and checks Linux sandbox diagnostics before activation. Setup never disables the sandbox or requests sudo.
- Installs a versioned application and atomically switches the stable command to it.
- Opens Termium with real terminal input after setup. Piped or redirected output skips launch; `--no-launch` also suppresses it.
- Preserves existing shell configuration and avoids duplicate setup blocks. It refuses to overwrite an unrelated `termium` command.

Each normal launch uses a private Unix socket and a separate temporary Chromium profile. Browsing sessions are not persisted between launches yet.

## Platforms and limits

| Platform | Bundle target |
| --- | --- |
| Linux x86-64 | glibc with working unprivileged Chromium sandbox support; setup checks the required minimum |
| macOS Apple Silicon | Native ARM64 bundle; tested through the macOS CI runner |
| macOS Intel | Native AMD64 bundle; tested through the macOS CI runner |
| Linux ARM64 | Not packaged yet |
| Native Windows | Later work |

WSL2 runs the Linux build. Windows Terminal's graphics support does not imply a native Windows executable.

The installer tests the actual host rather than silently disabling security features when browser startup fails. Distribution policies can restrict sandbox namespaces, notably on Ubuntu. Ubuntu CI explicitly allows Chromium user namespaces through a targeted AppArmor profile on the disposable runner; installation does not change host policy. A green hosted-runner test is not certification of every stock distribution or macOS Gatekeeper configuration. Clean native-machine distribution testing remains part of release acceptance.

## Updates and storage

Rerun the installer to update. Close running Termium sessions first; concurrent installers and active sessions are protected by locks. Failed validation leaves the current installation selected. Older versions remain available on disk; automatic pruning and uninstall are not implemented yet.

Application files live under `${XDG_DATA_HOME:-~/.local/share}/termium`. `TERMIUM_HOME` can select another application directory; the command remains at `~/.local/bin/termium`. `TERMIUM_NO_MODIFY_PATH=1` opts out of shell integration for managed environments. All paths are user-owned; no system Node or browser installation is modified.

See [Quickstart](quickstart.md) for your first page and [getting started](getting-started.md) for controls and [terminal support](terminals.md) for renderer choices.
