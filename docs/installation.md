# Installation

**Status: the one-command release is planned. The current installer is not ready for a hands-off installation.**

## The intended experience

The supported command will be published here after release testing. For now, use [development setup](development.md) to try the project; the existing installer is not a supported route.

The planned command will detect your system, download and verify the right release, prepare everything Termium needs, and open Termium when setup completes. You will see progress while it downloads. After quitting, `termium` must work again in the same terminal as well as in newly opened terminals.

You will not need to install Node.js, npm, Go, Chromium, or development tools; edit a configuration file; choose a renderer; or start a background server yourself. Setup must not ask for a password or require a system package-manager command on supported systems.

Setup requires an internet connection and a supported operating system and terminal. Once setup completes, launching Termium must not require another download. Websites still need their usual network access.

## Platform targets

| Operating system | CPU | Installation status |
| --- | --- | --- |
| Linux | x86-64 / AMD64 | First-release target; Debian 12 is the first feasibility candidate, not yet supported |
| Linux | ARM64 | Targeted; browser packaging must be resolved before support is announced |
| macOS | Apple Silicon / ARM64 | First-release target; macOS 14 is the first feasibility candidate, not yet supported |
| macOS | Intel / x86-64 | First-release target; macOS 14 is the first feasibility candidate, not yet supported |
| Windows | To be confirmed | Later release |

See [terminal support](terminals.md) for graphics modes. No platform is certified for the new installer yet. Other Linux distributions require their own tests; compatibility will not be inferred from a Debian result. In particular, Ubuntu's browser sandbox restrictions need separate validation. A cross-compiled binary alone does not mean installation and browsing work on that platform.

## Updates and removal

The planned update path is to rerun the same installation command. Setup must preserve your settings and leave the previous installation usable if the update fails.

Automatic background updates and a removal command are not implemented. Their behavior and exact commands will be documented when available. Removal must make it clear whether saved browsing data is retained or deleted.

## Trying the project today

The current build is intended for development. Existing installations can use the [getting started guide](getting-started.md); contributors can follow [development setup](development.md).

The existing installer can stop on missing Node.js, missing browser files, or release packaging failures. These are installation gaps to fix in Termium, not steps in the intended user setup.

[Documentation home](README.md)
