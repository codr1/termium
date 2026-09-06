# Testing

After installing the [contributor toolchain and npm dependencies](development.md), run the whole automated suite with:

```bash
npm test
```

This rebuilds the generated protocol bindings, TypeScript server, and Go client; runs static checks and Go tests with the race detector; installs Puppeteer's matching Chrome; and runs integration tests against the actual server and browser. A missing browser or failed launch fails the run. Tests do not require a visible terminal window, GNU `timeout`, or a public website.

The first run needs internet access to download dependencies and Chrome. Later browser runs reuse [Puppeteer's cache](https://pptr.dev/guides/configuration). Linux needs Chrome's system libraries; CI installs them automatically. Prefer Node 24 LTS. An interrupted browser download can leave an incomplete cache directory; remove that specific incomplete browser version and rerun `npm run test:browser:install`.

## What runs

| Layer | What the tests establish |
| --- | --- |
| Build and static checks | Both languages compile against freshly generated protobuf bindings; Go vet and installer shell syntax checks pass. |
| Client unit tests | Supported flags, server discovery through symlinks, live/closed/stale listener detection, and existing Kitty encoding tests. The race detector runs on exercised Go code. |
| Executable tests | The built binary exits with the expected status and diagnostic for help, version, unknown flags, and invalid renderers. |
| Browser integration | Real gRPC calls produce redirects, Unicode form input, special keys, mouse clicks, and recoverable navigation failures. |
| Screenshot integration | PNG and JPEG decode correctly, contain the fixture's background pixels, match viewport dimensions, and can be cancelled and reopened. |
| Dialog integration | Prompt text, empty text, cancellation, and confirmation responses reach the page; closing the dialog stream terminates it. |
| Process lifecycle | Port conflicts fail startup, missing Chrome produces RPC errors, and SIGINT/SIGTERM shut down with live streams, an unanswered prompt, or a stalled browser launch. Cleanup detects and kills a leaked browser process. |

The browser tests use a local HTTP fixture and verify effects independently of the RPC response text. For example, typing must submit the expected Unicode string to the fixture; a screenshot must decode to the right pixels. Tests use OS-assigned loopback ports and temporary browser profiles. They never remove `/tmp/termium.sock` or connect to a running development server. Failed browser assertions include captured server logs. A transparent executable wrapper records Chrome's PID before executing the real browser. Cleanup has a deadline and kills both the server's process group and Chrome's separate process group if necessary; profiles are kept under the test's temporary directory.

## Faster development loops

```bash
# Unit tests, after generating protobuf bindings:
npm run test:go

# Browser and executable tests, after rebuilding changed code:
npm run test:integration

# TypeScript checks without emitting JavaScript:
npm run typecheck

# Repeat one integration scenario to investigate flakiness:
go test -tags=integration -race -count=10 -timeout=5m -v ./tests/integration -run 'TestBrowser/screenshots'
```

`npm test` always rebuilds to avoid passing against stale binaries. The shorter commands assume the relevant generated files and binaries already exist. Integration tests have an explicit build tag, so ordinary `go test ./...` does not launch Chromium. Use `npm test` for the complete automated check.

## Pull requests and releases

The [Test workflow](../.github/workflows/test.yml) runs on pull requests, pushes to `main`, and manual dispatch. It uses Node 24 and the Go version declared in `go.mod`. The matrix covers Linux x86-64, macOS Apple Silicon, and macOS Intel using [GitHub's native runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners). Browser download and extraction run as the runner user; only system package installation uses sudo. Failed test output is retained as a workflow artifact. The release workflow calls the same tests before building artifacts.

Configure the three test jobs as required checks in the repository's branch rules to enforce them before merging. Adding a workflow alone does not configure branch protection. Linux ARM64 browser coverage and Windows are still outstanding; neither is implied by a green matrix.

## What a green run does not prove

This is browser integration through the production protocol, not a full terminal UI test. It does not yet drive the client's interactive event loop or certify graphics on Ghostty, Kitty, iTerm2, or other terminals. Keep a short manual check for terminal rendering, resize, keyboard focus, dialogs, and clean exit on each supported terminal.

For the next UI work, put Vimium-style command routing behind a state machine with deterministic tests for modes, counts, prefixes, and focus. Add a pseudo-terminal test that launches the real client and sends key sequences. Keep visual terminal checks separate from browser behavior so a failure identifies the layer that broke.

One-command installation also needs its own release acceptance suite: install an actual artifact in a clean environment with no Go, Node, or `protoc`; launch and browse a local fixture; then test update and uninstall. Current build and shell syntax checks do **not** validate that deployment path. Track that work against the [installation plan](plans/one-command-install.md).

[Documentation home](README.md)
