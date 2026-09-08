# Testing

After installing the [contributor toolchain and npm dependencies](development.md), run the whole automated suite with:

```bash
npm test
```

This rebuilds the generated protocol bindings, TypeScript server, and Go client (with race instrumentation for terminal integration); runs static checks and Go tests with the race detector; installs Puppeteer's matching Chrome; and runs integration tests against the actual server and browser. A missing browser or failed launch fails the run. Tests do not require a visible terminal window, GNU `timeout`, or a public website.

The first run needs internet access to download dependencies and Chrome. Later browser runs reuse [Puppeteer's cache](https://pptr.dev/guides/configuration). Linux needs Chrome's system libraries; CI installs them automatically. Use the toolchain declared in the project manifests. An interrupted browser download can leave an incomplete cache directory; remove that specific incomplete browser version and rerun `npm run test:browser:install`.

## What runs

| Layer | What the tests establish |
| --- | --- |
| Build and static checks | Both languages compile against freshly generated protobuf bindings; Go vet and installer shell syntax checks pass. |
| Client unit tests | Flags, server discovery/listeners, Kitty encoding, simulated tcell viewport colors/clipping, Unicode editing, mouse capture, modal routing, ordered input, stale-input recovery without replay, and concurrent frame ownership. |
| Graphics regression tests | Palette pixel round-trips, exact Sixel dimensions, allocation limits, output errors, unchanged-frame reuse, overlay restoration, splash/stale-image deletion, bounded pending work, and capture pacing. |
| Server flow-control tests | Slow captures stay exclusive, writable backpressure pauses production, cancellation prevents late writes, and navigation/watchdog cancellation preserves the input session and releases listeners. |
| Executable tests | The built binary exits with the expected status and diagnostic for help, version, unknown flags, and invalid renderers. |
| Home page | Saved-setting precedence, one-off URLs, shared website layout, custom Home/new-tab destinations, parked-site rejection, timeout fallback, user-interaction cancellation, and keeping hosted pages outside extension privileges. |
| Vimium and tabs | The actual bundled extension produces hints, preserves literal form input, scrolls, opens background links, switches duplicate-URL tabs, closes/restores tabs, rejects stale input, and renders the selected page. |
| Browser integration | Real gRPC calls produce redirects, Unicode form input, special keys, mouse clicks, and recoverable navigation failures. |
| Navigation and input | History/redirect state, reload, stopping a stalled navigation and recovering, literal paste, modifiers, held-button dragging, right-click, wheel input, and rejection of stale-document input. |
| Terminal integration | The actual client runs in a pseudo-terminal and submits Unicode form input through SGR mouse events and bracketed paste, edits an address, uses Back, resizes during a prompt, submits Unicode prompt text, verifies the new browser viewport, and quits. |
| Graphics terminal output | The real client runs in a pseudo-terminal with default and explicit capture formats. Tests decode emitted Kitty PNG, compressed RGB, and Sixel back to fixture pixels, verify viewport dimensions and saved source formats, and require clean keyboard exit. |
| Screenshot integration | PNG and JPEG decode correctly, contain the fixture's background pixels, match viewport dimensions, and can be cancelled and reopened. A committed page can be captured while a stalled subresource prevents the load event. |
| Dialog integration | Prompt text, empty text, cancellation, and confirmation responses reach the page; closing the dialog stream terminates it. |
| Process lifecycle | Port conflicts fail startup, missing Chrome produces RPC errors, and SIGINT/SIGTERM shut down with live streams, an unanswered prompt, or a stalled browser launch. Cleanup detects and kills a leaked browser process. |

The browser tests use a local HTTP fixture and verify effects independently of the RPC response text. For example, typing must submit the expected Unicode string to the fixture; a screenshot must decode to the right pixels. Tests use OS-assigned loopback ports and temporary browser profiles. They never remove `/tmp/termium.sock` or connect to a running development server. Failed browser assertions include captured server logs. A transparent executable wrapper records Chrome's PID before executing the real browser. Cleanup has a deadline and kills both the server's process group and Chrome's separate process group if necessary; profiles are kept under the test's temporary directory.

Dependency installation tests cover HTTPS downloads, checksum verification, cache reuse and corruption recovery, cancelled/failed downloads, archive traversal, escaping symlink chains and existing symlink parents, exact matching against reviewed Vimium source, and preserving an installed version when an upgrade fails. The package test uses the actual upstream archives with no development tools or browser on PATH.

## Faster development loops

```bash
# Unit tests, after generating protobuf bindings:
npm run test:go

# Server flow-control tests, after rebuilding the server:
npm run test:server

# Repeatable preparation microbenchmarks (exclude terminal I/O):
go test ./client -run '^$' -bench 'Benchmark(Websafe|Unchanged)Preparation' -benchmem

# Browser and executable tests, after rebuilding changed code:
npm run test:integration

# TypeScript checks without emitting JavaScript:
npm run typecheck

# Repeat one integration scenario to investigate flakiness:
go test -tags=integration -race -count=10 -timeout=5m -v ./tests/integration -run 'TestBrowser/screenshots'
```

`npm test` always rebuilds to avoid passing against stale binaries. The shorter commands assume the relevant generated files and binaries already exist. Integration tests have an explicit build tag, so ordinary `go test ./...` does not launch Chromium. Use `npm test` for the complete automated check.

## Comparing capture and renderer preparation

```bash
# Generate identical text and image-heavy fixtures with real Chromium:
npm run benchmark:capture

# Feed those captures into both renderers, reporting CPU time, allocations,
# and final terminal payload bytes:
TERMIUM_BENCH_CAPTURE_DIR="$PWD/dist/capture-benchmark" \
  go test ./client -run '^$' -bench BenchmarkRendererPreparation -benchmem
```

The capture benchmark interleaves JPEG, normal PNG, and fast PNG at the same viewport, discards warmup captures, and records machine/browser metadata in `dist/capture-benchmark/capture.json`. The preparation benchmark forces full-frame work while retaining encoder scratch. It excludes terminal I/O, unchanged-frame reuse, and the browser capture stage. Do not add the stage times and label that visible FPS: stages overlap, and terminal painting is outside these measurements. The seeded canvas is a stress case, not a representative average webpage. Run benchmarks without race instrumentation or other test workloads.

For actual terminal comparisons, use [the same capture format, page, and viewport](terminals.md#comparing-graphics-performance). Record payload bytes as well as CPU time; reducing one can increase the other.

## Full local performance run (planned)

This run has not yet been performed. The existing capture/preparation measurements and their rationale are recorded in [OPTIMUS](../OPTIMUS.md#implemented-shared-graphics-preparation-and-comparable-timings). The baseline is the current Go client, Node/Puppeteer controller, and Chromium running on one machine. Local sessions use the private Unix socket; remote network throughput is outside this run's scope.

1. **Record the environment.** Keep the source revision, build identity, OS, CPU/GPU, terminal and browser identities, display scaling, browser pixel dimensions, power mode, and whether the laptop is on battery or mains. Keep power and thermal conditions comparable. Run a normal build without race instrumentation: `npm run build:client` replaces the test binary after `npm test`. Use `./client/termium` to profile the checkout, not an older installed copy.
2. **Define repeatable workloads.** Use local fixtures for idle text, address editing/mouse keys without page changes, sustained scrolling, image-heavy pages, and bounded animation. Keep fixture content, input sequence, and duration fixed. Measure startup separately. Warm up each steady-state scenario, then record at least three runs of 30 seconds in alternating configuration order; retain individual results rather than only their average.
3. **Compare settings independently.** Start with normal defaults, then compare both renderers with PNG and both with JPEG. Keep viewport pixels equal. Test Sixel palettes separately. Compare protocols within the same terminal where supported; otherwise label foot-versus-Ghostty results as comparisons of the complete configurations. Record image quality alongside timing. See [the switch reference](terminals.md#performance-and-profiling-switches).
4. **Record a low-overhead baseline, then profile.** Measure ordinary browsing without debug logs, screenshot saving, or profilers first. Repeat with `--timings`, then separate client CPU-profile and execution-trace runs. Collect Node, Chromium, and terminal CPU/memory data separately; a Go client profile cannot attribute their costs. Compare instrumented runs with the baseline to expose measurement overhead.
5. **Separate the stages.** Current `capture` timing includes the whole screenshot RPC. Add correlated server-side phase measurements for queueing, CDP capture, Base64 handling, and response serialization before attributing any remainder to local IPC. Do not subtract unrelated benchmark medians. Profile preparation allocations, GC, blocked writes, and terminal CPU to test whether the observed Sixel allocation counts or larger local payloads actually limit throughput.
6. **Measure delivery and responsiveness.** Count captured, reused, superseded, and written frames separately. Report median and tail latency, sustained changed-frame delivery, CPU time, allocated bytes, peak memory, and GC behavior. A static page should not need repeated writes. Current logs do not expose all these counters; instrument missing measurements for the run. Account for the built-in 24 FPS ceiling and adaptive waits. Establish visible presentation and input-to-paint timing with a validated terminal-specific or external observation method; do not label successful writes as displayed frames.
7. **Preserve evidence and conclusions.** Archive captures, raw logs, profiles/traces, environment metadata, scenario definitions, and exact commands per configuration. Report variability and quality differences. Separate measured bottlenecks from hypotheses and retest a single change against the same baseline before drawing a conclusion.

Larger local transfers are acceptable when they reduce CPU work or improve responsiveness. Track capture bytes and terminal payload bytes separately as diagnostic evidence for copying, allocation, encoding, and terminal processing. Byte count by itself does not establish transport saturation or determine the winning configuration.

## Pull requests and releases

The [Test workflow](../.github/workflows/test.yml) runs on pull requests, pushes to `main`, and manual dispatch. It uses the toolchain versions declared in the workflow and `go.mod`. The matrix covers Linux x86-64, macOS Apple Silicon, and macOS Intel using [GitHub's native runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners). Browser download and extraction run as the runner user. Linux CI uses sudo for system packages and a targeted AppArmor allowance for downloaded Chrome executables on the disposable runner; Chromium's namespace and seccomp sandboxes stay enabled. This allowance follows [Chromium's documented approach](https://chromium.googlesource.com/chromium/src/+/main/docs/security/apparmor-userns-restrictions.md) and is never applied by the installer. Failed test output is retained as a workflow artifact. The release workflow calls the same tests before building artifacts.

Configure the three test jobs as required checks in the repository's branch rules to enforce them before merging. Adding a workflow alone does not configure branch protection. Linux ARM64 browser coverage and Windows are still outstanding; neither is implied by a green matrix.

## What a green run does not prove

The suite exercises the interactive client through a pseudo-terminal, reads its current screen through vt10x, and checks tcell cells in a simulation. Historical ANSI output is retained for failure diagnostics but does not establish UI readiness. It does not certify graphics on Ghostty, Kitty, iTerm2, or other real terminal emulators. Keep a manual check for image placement, menus over graphics, modifier delivery, and restored terminal settings on each supported terminal. The bundled Vimium tests cover core commands, not every upstream binding or every website. Keep manual checks for hint readability and find/help overlays on each graphics terminal.

Release packaging has a separate automated acceptance suite:

```bash
npm run build:bundle
npm run test:installation
```

It installs an actual archive into a fresh home with only bootstrap commands on PATH, rejects an incorrect checksum, repeats installation, checks command discovery in a new shell, and launches the installed client directly from the piped installer in a PTY. The real browser opens a local page using automatic renderer selection and quits from keyboard input. Focused bootstrap tests cover terminal attachment, redirected output, `--no-launch`, failed validation, and truncated installer downloads. Unit tests cover failed upgrades, active sessions, concurrent installers, foreign commands, read-only profiles, escaping symlinks, and paths containing spaces and quotes.

These checks do not substitute for clean native OS installation tests, macOS distribution/signing checks, or a real graphics-emulator matrix. See [installation](installation.md) for current platform limits.

[Documentation home](README.md)

## Public website

`npm run test:website` builds the static site and runs Chromium checks for all public routes, internal links and anchors, phone/desktop overflow, no-JavaScript navigation, clipboard behavior, and Vimium under the site content security policy. It also checks that the browser welcome page stays out of public links and the sitemap. The full `npm test` runs these checks after preparing the server and browser.
