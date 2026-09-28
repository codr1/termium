# Demo testing follow-up — 2026-09-28

Source: the demo engine's corrected report and artifacts at
`ea:~/dev/termium-video/bugs/`. Investigation starts from released `v0.2.1`
(`1ff761c`). This document distinguishes observations from demonstrated causes.

## Keyboard input: previous diagnosis withdrawn

The engine reports 41 of 41 keys received and dispatched successfully in each
of three debug runs, and working Vimium hints on Hacker News and termium.dev.
Its separate `cat` test lost 22 of 25 keys without Termium when using short-lived
`wtype` processes. Use a primed, persistent virtual keyboard for future takes.
Keep the input diagnostics: they exposed the rig failure. This does not prove
every possible input race is absent, nor that the earlier patch fixed the rig.

## Capture: bounded recovery implemented, site cause still unconfirmed

The status-bar screenshot establishes an intermittent
`Page.captureScreenshot: Not attached to an active page` error. Three subsequent
debug runs did not reproduce it. Video sections are an observed correlation,
not proof that video causes a target replacement.

The existing capture code already detaches failed sessions. However, the client
surfaces the unknown RPC error and backs off for one second. The change retries
this exact screenshot error once using a fresh capture session. A successful
retry returns a frame normally; a second failure still reaches the caller.
Navigation, unrelated errors, and input are not retried by this change.
Deterministic tests cover recovery, persistent failure, navigation, input-session
isolation, session reuse, and listener/watchdog cleanup. A live SpaceX recurrence
has not yet been captured with this patch.

## Installer: cause unresolved; diagnostics improved

The demo report describes EWOULDBLOCK even after its process cleanup. At the time
of this investigation, `ea` has no Termium lock in `lslocks` and already runs
v0.2.1. A nonblocking exclusive-lock probe also acquired both `.install.lock`
and the current version's `.active` without changing their contents or deleting
them. The original blocking holder is no longer observable.

The code uses `flock`, not lock-file existence. The regression checks that a live
lease blocks installation and that releasing it permits installation with both
lock files still present. Errors now include the lock path and distinguish
contention from other locking failures. There is no proven fix for the reported
incident, and holder PID reporting remains follow-up work.

Do not assume orphaned Node servers held the session lock: no file-descriptor or
kernel-lock evidence was saved for that claim. Go opens this file close-on-exec,
and the launcher does not pass it through `ExtraFiles`.

## Terminal-close cleanup: reproduced on v0.2.1 and fixed locally

Six clean demo sessions were encouraging but insufficient. The new integration
test launches the actual client, lets it start its own Node server and Chromium,
waits for the fixture in the terminal, then separately exercises SIGHUP and
closing the PTY master. Both failed on the released source, leaving browser
processes behind. SIGHUP was missing from the client's shutdown signal handler.

Adding it routes terminal hangup through existing cleanup. Both scenarios pass
afterward, checking that the owned server and browser PIDs have exited. Tests
clean up only their recorded processes on failure. This is graceful hangup
coverage, not a guarantee of cleanup after SIGKILL or machine failure.

## Remaining UX/features

- xterm setup is now documented in [terminal guidance](terminals.md#xterm-setup).
  A visible fallback hint remains to implement. Do not infer real xterm from
  `TERM=xterm-256color`: many other terminals use that value. Identify xterm
  separately and show the hint only when automatic probing falls back to ASCII.
- Dark mode remains a feature request. Implement explicit
  `--color-scheme dark|light` first with tests of the page's actual media query.
  Terminal-background detection for `auto` needs bounded OSC 11 probing,
  fallback behavior, and input-parser tests before enabling it by default.
- Software WebGL is a separate performance investigation. No GPU configuration
  or performance claims change in this pass.

## Validation

Local Linux validation completed on 2026-09-28:

- The new hangup test failed against the released client in both scenarios,
  then passed after the handler change. Logs:
  `/tmp/termium-demo-hangup-{before,after}.log`.
- Capture tests: 12 passed, no skips (`/tmp/termium-demo-capture.log`).
- Focused installer tests passed (`/tmp/termium-demo-install.log`).
- `npm run typecheck` passed (`/tmp/termium-demo-typecheck.log`).
- Full `npm test` exited 0 without retries: build, static checks, server tests,
  Go race tests, real-browser integration (including both hangup scenarios under
  the race detector), and all five website tests.
  Log: `/tmp/termium-demo-full-test.log`.

Source review checked the retry bound, original-error propagation, input-session
isolation, signal routing, test-owned process cleanup, and lock-file semantics.
Native macOS validation and another SpaceX demo take remain outstanding.
No release, publishing, installation upgrade, or CEF changes were performed.
