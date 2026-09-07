# Installation

Termium now has a complete native-bundle installer. The first public release using it has not been published yet; the public download command will appear here when its artifacts are available.

## Run it

After installation, run this from any directory:

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

The installer puts the command at `~/.local/bin/termium` and sets up bash, zsh, and fish. Open a new terminal after running the contributor command if your current shell did not already have `~/.local/bin` on PATH. The eventual public one-line command also activates PATH in the original shell and launches the application.

## What setup does

- Checks the platform and verifies the archive's SHA-256. Missing or mismatched checksums stop installation.
- Stages the client, server, private Node runtime, and Linux browser libraries/fonts, with glibc supplied by the host.
- Downloads the exact Chromium and Vimium archives approved by the release build over HTTPS. Their SHA-256 checksums are pinned inside the verified app archive.
- Verifies downloads before extraction and caches them under the installation directory’s `downloads/` folder. Repeat installations reuse matching cached archives; corrupt cache entries download again.
- Completes browser and Vimium checks before activating the installation. Download, extraction, or validation failure preserves the previous working version.
- Starts a disposable browser, verifies input and screenshots, and checks Linux sandbox diagnostics before activation. Setup never disables the sandbox or requests sudo.
- Installs a versioned application and atomically switches the stable command to it.
- Preserves existing shell configuration and avoids duplicate setup blocks. It refuses to overwrite an unrelated `termium` command.

Each normal launch uses a private Unix socket and a separate temporary Chromium profile. Browsing sessions are not persisted between launches yet.

## Platforms and limits

| Platform | Bundle target |
| --- | --- |
| Linux x86-64 | glibc 2.36 or newer, with working unprivileged Chromium sandbox support |
| macOS Apple Silicon | Native ARM64 bundle; tested through the macOS CI runner |
| macOS Intel | Native AMD64 bundle; tested through the macOS CI runner |
| Linux ARM64 | Not packaged yet |
| Native Windows | Later work |

WSL2 runs the Linux build. Windows Terminal's graphics support does not imply a native Windows executable.

The installer tests the actual host rather than silently disabling security features when browser startup fails. Distribution policies can restrict sandbox namespaces, notably on Ubuntu. Ubuntu CI explicitly allows Chromium user namespaces through a targeted AppArmor profile on the disposable runner; installation does not change host policy. A green hosted-runner test is not certification of every stock distribution or macOS Gatekeeper configuration. Clean native-machine distribution testing remains part of release acceptance.

## Updates and storage

Rerun the installer to update. Close running Termium sessions first; concurrent installers and active sessions are protected by locks. Failed validation leaves the current installation selected. Older versions remain available on disk; automatic pruning and uninstall are not implemented yet.

Application files live under `${XDG_DATA_HOME:-~/.local/share}/termium`. `TERMIUM_HOME` can select another application directory; the command remains at `~/.local/bin/termium`. `TERMIUM_NO_MODIFY_PATH=1` opts out of shell integration for managed environments. All paths are user-owned; no system Node or browser installation is modified.

See [getting started](getting-started.md) for controls and [terminal support](terminals.md) for renderer choices.
