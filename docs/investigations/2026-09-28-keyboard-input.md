# Scripted keyboard loss on ea

## Finding

The reproduced key loss occurs **before Termium reads terminal input**. Repeated,
short-lived `wtype` processes on the headless sway/Ghostty stage do not reliably
produce bytes at the terminal's PTY. This investigation does not identify whether
wtype, sway, or Ghostty is responsible for that loss.

The failing script creates and destroys a Wayland virtual keyboard for every tap:

```bash
for i in $(seq 1 96); do wtype j; sleep 0.1; done
```

Keeping one virtual keyboard alive, allowing 100 ms before typing and before exit,
delivered all requested keys in the measured runs:

```bash
wtype -s 100 -d 100 "$(printf 'j%.0s' {1..96})" -s 100
```

This is a measured workaround for this stage, not a guarantee of delivery on all
Wayland systems. A successful wtype exit is not proof that a terminal received a
key. Validate the PTY boundary when using synthetic input for tests or recordings.

## Evidence

Investigation used the installed v0.2.0 server and Chromium on ea. A separate
client built from release commit `4ba5020` added input logging without changing
input handling. The existing source checkout on ea was older than the installed
release and was not used as the release's source of truth.

| Run | Requested keys | Bytes/keys observed at PTY | Client RPC results | Page observation |
| --- | ---: | ---: | ---: | --- |
| Original per-tap wtype, server observer | 96 `j` | Not recorded | 26 successes | 26 keydowns, scrolling |
| Original per-tap wtype, raw PTY recording | 96 `j` | 35 | 35 successes | All received keys delivered |
| Persistent wtype, startup/drain delays | 96 `j` | 96 additional | 96 successes | Reached bottom of page |
| Persistent wtype, 2.2 s PageDown spacing | 1 Home + 14 PageDown | 15 additional key events | 15 successes | Continued scrolling to bottom |
| Patched harness key-script block | 96 `j` | 96 additional | 96 successes | No input errors |

The raw boundary was recorded with util-linux `script --log-in`, outside Termium.
The 35-key run logged exactly 35 receives, enqueues, dispatches, and successful
results. No client queue backlog or server rejection explains the other 61 keys:
they never appeared in the PTY input stream. The instrumented browser reported no
input failures in these runs. Diagnostic logging changes timing, so the precise
loss percentage is not a performance claim.

The original videos include an explicit hold after the key script finishes. A
stationary page during that hold is not evidence of continuing input being lost.
The reported stop times near 11–12 seconds are consistent with the end of these
short key scripts, but the original recordings do not establish exact injection
timestamps. A full-size frame from run C at 14 seconds reads “Ready” in the status
bar; it does not show the reported CDP error.

## Changes and validation

- On ea, `~/dev/termium-video/shots/take.sh` now compiles its entire key/wait script
  into one wtype invocation. Key spacing and explicit waits are retained. Backup:
  `take.sh.before-input-fix-20260928`. This external harness is not part of this repo.
- Client `--debug --logfile PATH` records input receive, queue, cancellation,
  dispatch and result metadata. Its child server enables structured input tracing,
  including tab/document generations and Puppeteer's keyboard CDP session ID.
  Typed and pasted text contents are not logged by these new trace points.
- A separately launched server can enable the same trace with
  `TERMIUM_INPUT_TRACE=1`; it writes JSON lines to stdout. Trace IDs correlate a
  server operation across asynchronous queue waits. Client and server trace IDs
  are not a shared wire identifier; match order and tab/generation metadata.
- `server/tests/input-video.integration.cjs` serves a local fixture, records a
  short WebM in Chromium, and plays it in a looping autoplay video. For 30 seconds
  it mixes Vimium `j`, ArrowDown, and PageDown through BrowserSession while PNG
  capture runs concurrently. It checks exact page key counts, actual scroll
  movement, ongoing video decoding and continued capture. No external media or
  website is needed. On ea: **290 keys, 368 PNG captures, scrollY 56,700**, pass.
- A unit test injects `Not attached to an active page` into a keyboard operation:
  the error must propagate, the following queued operation must run, and the
  failed mutation must not be automatically replayed.

The first video-test attempt counted keys from an ordinary page listener, after
Vimium's interception handlers, and failed even though scrolling worked. The
corrected observer is installed before navigation. That test-only correction and
the original failed log are preserved in the investigation evidence.

The full `npm test` suite (including race checks, browser integration, and website
checks), `npm run typecheck`, and the normal client rebuild passed locally on
Linux. The sustained-video test also passed separately on ea. A final real
Ghostty check on ea displayed Vimium hints on Hacker News after one `f`; the new
log recorded the complete operation, including its real keyboard session ID.
That initial investigation did not include native macOS validation; see the
subsequent release-validation finding below.

## macOS finding during release validation

Native CI subsequently exposed a separate PageDown behavior gap. A failing Intel
Mac run received all 12 requested keys; its last PageDown arrived at scrollY 400,
with 199,440 pixels of available scroll range, a visible page, BODY focus, and
ongoing video playback. `j` and ArrowDown advanced the page; PageDown did not.
The initial mixed-key test sometimes mistook residual ArrowDown animation for
PageDown progress, so its apparent success was not sufficient coverage.

CDP-injected keyboard events bypass native AppKit bindings. As with the existing
Command+A handling, unmodified PageDown and PageUp now carry Chromium's explicit
`scrollPageForward` and `scrollPageBackward` commands on macOS. These remain part
of the key dispatch, so a page's `preventDefault` can cancel them. Linux handling
and modified-key combinations are unchanged.

The test now checks PageDown from rest, PageUp back to the top, and page-level
cancellation before starting its mixed-key/video loop. Both native Mac CI jobs
are required release gates. Chromium's implementation of the scrolling commands
is in [editor_command.cc](https://github.com/chromium/chromium/blob/main/third_party/blink/renderer/core/editing/commands/editor_command.cc).

## Unresolved observation

`Not attached to an active page` was **not reproduced** here. No session-recovery
change is justified by the observed key loss. Keyboard dispatch uses Puppeteer's
primary page session; capture uses Termium's dedicated screenshot session, and
pointer/history use a separate control session. Reattaching the screenshot
session would not repair Puppeteer's keyboard session.

If that error recurs, the new trace distinguishes selection/readiness failures,
keyboard dispatch failures, and state-read failures after a delivered key. Capture
the trace before inferring that the keyboard session is permanently broken. Do
not replay a key with an uncertain result: it may have navigated or closed a tab.

## Evidence locations

On ea: `/tmp/termium-input-{live.jsonl,raw.log,traced-client.log}`,
`/tmp/termium-input-repro-baseline.jsonl`, and
`/tmp/termium-input-video-test{,-2}.log`. The live observer records synthetic test
keys and page state; it is investigation tooling, not shipped instrumentation.
Local copies and full-suite logs: `/tmp/termium-input-investigation/`.
