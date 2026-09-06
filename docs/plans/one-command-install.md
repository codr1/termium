# One-command installation plan

Status: requirements for implementation. The existing installer does not meet this contract.

## Product requirement

One pasted command must take a user on a supported Linux or macOS system from no Termium installation to a usable browser. No follow-up setup commands, dependency installation, password prompts, configuration edits, or renderer selection.

The reference is the familiar shell-installer entry point demonstrated by [Claude Code's native installation](https://code.claude.com/docs/en/quickstart). This requirement concerns Termium setup; browsing a site still involves normal user interaction.

The proposed entry point below is an implementation specification only. It depends on a replacement installer and a new `--first-run` launcher option; do not run or publish it as an available installation method:

```bash
bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/codr1/termium/main/scripts/install.sh | bash' && export PATH="$HOME/.local/bin:$PATH" && "$HOME/.local/bin/termium" --first-run
```

A shorter project-owned URL can be added once a domain is chosen and controlled. The command remains one pasted line: dependency setup, parent-shell PATH activation, and launch all happen without another user step. The shell tail is required; shortening this to a bare pipe would break repeat launches in the original shell.

## Installer responsibilities

1. Detect OS, architecture, minimum OS/libc compatibility, available disk space, and unsupported environments before changing an installation.
2. Download the matching release over HTTPS with visible progress and bounded retry behavior.
3. Require a matching checksum for every executable artifact. Never silently skip verification.
4. Provision the client, server, private runtime, browser, and required browser resources. Include Linux libraries and fonts where necessary; do not turn missing dependencies into user package-manager tasks.
5. Test that the packaged browser starts with the intended sandbox and that client/server versions agree.
6. Install as the current user, in a writable location, without sudo or modifications to a system Node/browser installation.
7. Install a stable launcher at `$HOME/.local/bin/termium` and persist PATH integration, preserving existing shell configuration and avoiding duplicate managed blocks. Cover bash, zsh, and fish with their native syntax.
8. Detect terminal graphics and select a usable fallback with bounded probes.
9. Return success only after installation and validation complete. The parent-shell tail activates PATH and invokes the launcher. `--first-run` opens the TUI without the mandatory splash confirmation when stdin and stdout are terminals; otherwise it reports installation status and exits without opening a TUI.

A piped installer cannot change its parent shell's environment. The `export` in the proposed command runs in that parent after the pipeline succeeds. The final launcher inherits terminal input from the parent, not the installer pipe. `pipefail` ensures a failed download cannot be treated as installation success. The installer must buffer and validate complete payloads before mutation and never launch a TUI itself.

Use `$HOME/.local/bin` for the stable launcher even when `TERMIUM_HOME` relocates application files. Fail without overwriting an unrelated launcher. Existing shell aliases/functions named `termium` and read-only shell startup files must be detected by the shell integration feasibility tests and handled before advertising that environment as supported. Do not replace the user's shell or silently create a nested interactive shell.

The proposed syntax targets bash, zsh, and fish. Fish provides an [export compatibility function](https://fishshell.com/docs/current/cmds/export.html) that accepts quoted PATH values. Before publishing support, verify minimum shell versions, default startup files, custom zsh configuration directories, command caches, and both login/non-login startup. Repeating the command must not duplicate installed files or managed configuration blocks; the literal current-shell PATH prepend may contain a repeated directory without changing command resolution.

Review verification: the exact proposed command was exercised with a mocked download/installer/launcher in an isolated Ubuntu 24.04 container using bash 5.2.21, zsh 5.9, and fish 3.7.0. All 12 scenarios passed: fresh PATH without the launcher directory, an existing cached binary, repeat invocation, and failed download for each shell. This proves the shell sequencing and same-shell lookup mechanism. It does not validate a real release, persistent startup-file integration, interactive TUI attachment, native macOS, or browser startup.

## Feasibility gate before installer implementation

Do the smallest browser packaging experiment first. The initial candidates are Debian 12 AMD64 and macOS 14 on ARM64 and AMD64. These are test targets, not supported platforms or a claim that the full release archive already exists.

For each candidate, record the OS image/version, architecture, kernel and libc where relevant, browser/runtime revisions, artifact hashes, missing libraries, and sandbox results. Start from a fresh native VM or machine with a normal user account, no system Chrome, and no development tools. Build artifacts elsewhere. Copy the candidate bundle into a user-owned directory, start the browser, navigate to a local test page, exercise input, take a screenshot, and close it.

For Linux, prove that sandboxing is active, not merely that `--no-sandbox` was omitted: capture Chromium sandbox diagnostics and process restrictions appropriate to the pinned browser. Package necessary shared libraries and fonts, then repeat on the clean image. No root-owned helper, system browser, host security-policy edits, or package installation may be added to make the test pass. A container using a host kernel does not qualify as a clean native OS test.

[Chromium documents Ubuntu restrictions on sandbox namespaces](https://chromium.googlesource.com/chromium/src/+/main/docs/security/apparmor-userns-restrictions.md). Ubuntu 24.04 is a separate negative/compatibility test: expect it to remain outside the supported matrix unless a sandboxed user-only bundle succeeds with stock policy. Never weaken the sandbox to satisfy the install requirement.

| Candidate | Evidence status | Promotion rule |
| --- | --- | --- |
| Debian 12 AMD64 | Not tested with a self-contained sandboxed bundle | Native clean-image experiment and installation checks pass |
| macOS 14 ARM64 / AMD64 | Not tested with the proposed bundle | Each architecture independently passes native launch and distribution checks |
| Ubuntu 24.04 AMD64 | Known host-policy risk; not tested here | Stock-policy, user-only sandboxed launch and installation checks pass |
| Linux ARM64 | Browser distribution unresolved | Select a maintained browser source, then run the same native checks |

If a candidate fails, keep it unsupported and record the specific blocker. Do not call the feasibility gate complete or the Linux installation promise delivered until at least one Linux candidate passes. Choose build images, browser source, and minimum OS versions from that evidence before implementing the full release pipeline. Runtime and native macOS feasibility remain open release blockers in this documentation-only change.

## Packaging direction

Keep the Go client and Puppeteer server for this release. Prefer a complete per-platform release containing their runtime dependencies so users never run npm or a browser installer.

Build and validate native artifacts for Linux AMD64, macOS ARM64, and macOS AMD64. Linux ARM64 needs a deliberate browser distribution: the default Puppeteer Chrome download [does not provide that target](https://pptr.dev/troubleshooting). Bundle a compatible browser and test it before announcing support. Windows follows later.

Publish only the exact OS versions and architectures promoted through the feasibility gate and full installer tests. Bundling shared libraries does not remove kernel, libc, sandbox, or OS compatibility requirements. Unsupported hosts must fail early with a clear explanation.

Bundle dependency notices and use a repeatable browser/runtime update process. Browser provisioning must support the DOM helpers and browser-control APIs needed for upcoming [native Vimium-style navigation](browser-ui.md). The default experience does not depend on installing a Vimium extension.

## Updates and recovery

Rerunning the same command should update an installation. Download and verify into a separate staging directory, validate using a disposable browser profile, then switch versions atomically. Validation must never open or migrate the user's profile. A failed download, failed validation, or interrupted update must leave the old installation usable.

Keep settings and browser data separate from versioned application files. Do not activate an update while a session using that installation is running: return a clear busy result and leave it unchanged. Before the first real launch of a new browser revision, snapshot the closed profile and version its settings; on migration failure restore the snapshot before selecting the old application version. Retain the pre-upgrade snapshot until successful launch is confirmed. This is recovery from a failed upgrade, not a promise to downgrade profiles after subsequent browsing. Define retention and removal behavior before exposing update or uninstall commands. Never delete browsing data as a side effect of updating.

Concurrent installers must not corrupt one another. Clean up staging files on errors and signals. Preserve user-owned files and unrelated PATH entries.

## Current implementation gaps

| Area | Evidence in the current code | Required change |
| --- | --- | --- |
| Runtime | `scripts/install.sh` requires an existing Node installation | Package and resolve a private runtime |
| Browser | Bundle builds skip downloading; startup only launches Puppeteer | Include or explicitly provision the tested browser before declaring success |
| Server bundle | Runtime installation calls an omitted `scripts/preinstall.sh` | Separate build and runtime packaging |
| Integrity | Missing checksums are treated as optional | Fail closed on absent or mismatched checksums |
| Upgrade | Server files are removed before replacement is fully installed | Stage, validate, and atomically activate a complete version |
| Shell setup | Installer prints manual PATH instructions, including shell-incompatible syntax for fish | Install automatic, idempotent shell integration |
| Startup | Client waits at a splash; terminal calibration can block | Direct first launch and bounded capability detection |
| Session safety | Shared socket and disabled Chromium sandbox | Private session endpoints and sandboxed launch |
| Release validation | Only a tag-triggered build exists | Exercise the published archive and installer on clean target systems |

## Acceptance criteria

- [ ] One command works without Node, npm, Go, protoc, or Chromium preinstalled.
- [ ] Setup needs no root privileges, interactive confirmations, or manual shell edits on supported systems.
- [ ] The native feasibility gate has passed for every advertised OS/architecture; publish its evidence with the release checklist.
- [ ] Browser libraries and fonts work on clean supported Linux installations with the sandbox verified active.
- [ ] Native Apple Silicon and Intel macOS installations pass; required signing/distribution behavior is validated without manual OS bypass steps.
- [ ] The complete one-line command launches a usable TUI; a non-interactive run terminates successfully without waiting for keyboard input.
- [ ] In each target shell with a stock PATH lacking `$HOME/.local/bin`, install, browse, quit, then run `termium` in the same shell without another setup action. Also test an existing cached command path.
- [ ] A new login and non-login shell resolve `termium`; repeat setup preserves startup behavior and does not duplicate managed configuration blocks.
- [ ] Spaces in home paths and custom install locations work.
- [ ] Cold setup shows progress; subsequent launches work without a network download.
- [ ] Navigation, typing, click, screenshot delivery, and terminal restoration pass after installing the release archive.
- [ ] Missing or altered artifacts, low disk space, interrupted downloads, failed browser startup, and failed profile migration do not destroy a previous installation or browsing data.
- [ ] The runtime uses the browser sandbox and local sessions do not share control accidentally.
- [ ] Each claimed architecture passes native installation and browser tests.

Only after these checks pass should the user-facing installation page describe the one-command path as available.

[Documentation home](../README.md)
