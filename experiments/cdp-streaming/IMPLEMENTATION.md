# cdp-streaming run.mjs — implementation notes (for review)

Standalone PoC, no Termium runtime edits. `node --check` passes; 236 lines after the
correction pass (all 11 items from /tmp/termium-streaming-corrections.txt applied). No
measurements or tests run yet.

## CLI and bounds

`--mode screenshot|stream --format png|jpeg --width N --height N --warmup S --seconds N --out DIR`
(defaults warmup=3, seconds=10). Bounds: integers, 384<=width<=3840 (marker row is 384px
wide), 64<=height<=2160, 0<seconds<=30, warmup integer **seconds** 0..10 in both modes.
`--out` must not exist at all (even if empty); created fresh otherwise. One browser per
invocation; `PUPPETEER_EXECUTABLE_PATH` honored via `executablePath`.

## Setup (mirrors server/src/server.ts launch)

- Launch: headless, pipe, enableExtensions, defaultViewport null, protocolTimeout 15000,
  args `--disable-blink-features=AutomationControlled` + the server's exact
  `--user-agent=...Chrome/120.0.0.0...` string (verbatim from server.ts:169-170).
- Sibling `fixture.html` served via node:http on 127.0.0.1 ephemeral port with pathname
  routing (query strings accepted) and a `/ready` 204 endpoint; navigate to
  `?scene=canvas` (the fixture only sets `window.fixtureReady` in the canvas branch).
- Dedicated target CDPSession (`page.createCDPSession()`) for all capture work. A second,
  browser-target session (`browser.target().createCDPSession()`) is used once, outside
  measurement, for `Browser.getVersion`, `SystemInfo.getInfo`, and
  `Browser.getBrowserCommandLine` (SystemInfo does not carry the command line; the exact
  string is reported in `envInfo.commandLine`). No explicit detach — browser.close owns it.
- `Emulation.setDeviceMetricsOverride {width,height,deviceScaleFactor:1,mobile:false}`,
  then `waitForFunction window.fixtureReady`; `window.fixture.originEpochMS` read via
  page.evaluate right after ready.

## Screenshot mode

Persistent CDP session, sequential `Page.captureScreenshot {format, quality:60 if jpeg,
fromSurface:true,captureBeyondViewport:false,optimizeForSpeed:true}`, no FPS cap; the
response is `{data}` and both warmup/measurement consume `res.data`. Warmup runs until a
monotonic deadline of `warmup*1000` ms with the same consume/hash path as measurement.
The window loop never starts a new RPC after the window ends; complete captures arriving
past the end are receipts with phase `late`, excluded from measurement stats (no synthetic
truncated captures). Per-receipt RPC latency measured before decode. Ten consecutive
capture failures abort the run instead of spinning quietly.

## Stream mode

Listener registered before `Page.startScreencast {format, quality:60 if jpeg,
maxWidth,maxHeight,everyNthFrame:1}`. Handler timestamps synchronously at receipt
(`performance.now` + `Date.now`, both before decode/hash), consumes the base64 Buffer,
SHA256-digests it, then acks immediately with the event's own `sessionId`. In-flight acks
are tracked in a Set (true outstanding state): promise added on send, deleted in
`.finally` of the already-caught promise; size bounded at 128 — exceeding records an
error that fails the run. Teardown: `stopScreencast`, then drain until the Set is empty
(handler stays active through the drain), only then `cdp.off()`. Warmup = first `warmup`
seconds of arrivals, counted separately; frames landing at/after the window end are
phase `late`.

## Measurement and outputs

Fixed monotonic window opens only after warmup. Measurement receipts are exactly those
with mono in [windowStart, windowEndPlanned). fps and bytesPerSec use the **requested**
`seconds` as denominator — a late-finishing capture never enlarges throughput; actual
stopped time is recorded separately as `windowActualMS`. The resource sampler (required
sibling resources.mjs) starts immediately before the window and stops immediately after;
its init/start/stop failures are recorded, not swallowed. CPU delta is snapshotted
immediately after the mode returns, **before** sampler stop; `process.cpuUsage` units are
microseconds (seconds = /1e6); the accounting interval (`accountingDurationMS`) is
recorded because it may cover a slight tail past windowEnd — disclosed alongside the
sampler's own boundary notes. No file writes during the window. At most one payload
sample per second (max 32) retained in memory from measurement receipts; images written
only after stop.

Outputs (all after stop/end): `result.json`, `receipts.json` (every receipt with relMS vs
window start, receiveEpochMS, bytes, digest, phase + rpcLatencyMS/metadata), and sample
images. `result.json` carries the schema check-samples.mjs expects:
`options.{format,width,height}`, `fixture.originEpochMS`,
`samples:[{file,receiveEpochMS,relMS,bytes,metadata}]`. Extras: counts by phase, fps,
bytesPerSec (measurement only), payloadChanges (digest transitions over measured receipts
only — encoded digests are not treated as unique visual frames), arrivalIntervalsMS and
screenshotRTTMS distributions (min/p50/p95/max), Node CPU seconds + accounting duration,
Node RSS at window start/end, envInfo (getVersion/SystemInfo/exact command line), sampler
result, bounded error log. `ok` is false — and exit code 1 — when there are zero
measurement frames **or any captured error**; never silent success.

## Safety bounds

Watchdog timer (warmup+seconds+30s budget): on fire it awaits `browser.close()` with a
10s grace period, then SIGKILLs only this launched browser process if still alive, and
exits 1 — no orphaned browser. The watchdog is cleared in finally **after** the normal
`browser.close()` completes, never before. Fatal errors propagate to stderr with exit 1.
No marker decoding here — that is the coordinator's offline check-samples.mjs job.
