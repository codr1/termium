#!/usr/bin/env node
// Standalone CDP capture-path proof of concept (no Termium runtime code).
// Modes: sequential Page.captureScreenshot vs Page.startScreencast streaming.
// Serves the sibling fixture.html on loopback; one browser per invocation.
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {fileURLToPath} from 'node:url';
import puppeteer from 'puppeteer';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const round3 = (x) => Math.round(x * 1000) / 1000;
function fail(msg, code = 2) { console.error(`run.mjs: ${msg}`); process.exit(code); }

// ---------- CLI ----------
const args = {};
for (let i = 2; i < process.argv.length; i += 2) {
  const k = process.argv[i], v = process.argv[i + 1];
  if (!k || !k.startsWith('--') || v === undefined) fail(`bad CLI near: ${process.argv.slice(i).join(' ')}`);
  args[k.slice(2)] = v;
}
const mode = args.mode, format = args.format, outDir = args.out;
const width = Number(args.width), height = Number(args.height);
const warmup = Number(args.warmup ?? '3'), seconds = Number(args.seconds ?? '10');
if (!['screenshot', 'stream'].includes(mode)) fail('--mode screenshot|stream required');
if (!['png', 'jpeg'].includes(format)) fail('--format png|jpeg required');
// width>=384: the fixture marker row is 384px wide and must fit.
if (!Number.isInteger(width) || !Number.isInteger(height) || width < 384 || width > 3840 || height < 64 || height > 2160) fail('bounds: integers, 384<=width<=3840, 64<=height<=2160');
if (!Number.isInteger(warmup) || warmup < 0 || warmup > 10) fail('--warmup must be integer seconds 0..10 (both modes)');
if (!(seconds > 0 && seconds <= 30)) fail('bounds: 0<seconds<=30');
if (!outDir) fail('--out NEW_DIRECTORY required');
if (fs.existsSync(outDir)) fail(`refusing to use existing ${outDir}; pass a new directory`);
fs.mkdirSync(outDir, {recursive: true});

// ---------- fixture over loopback http (pathname routing; query accepted) ----------
const fixturePath = path.join(HERE, 'fixture.html');
if (!fs.existsSync(fixturePath)) fail(`sibling fixture missing: ${fixturePath}`, 1);
const fixtureHTML = fs.readFileSync(fixturePath);
const httpServer = http.createServer((req, res) => {
  const pathname = new URL(req.url, 'http://localhost').pathname;
  if (pathname === '/' || pathname === '/fixture.html') { res.writeHead(200, {'content-type': 'text/html'}); res.end(fixtureHTML); }
  else if (pathname === '/ready') { res.writeHead(204); res.end(); }
  else { res.writeHead(404); res.end(); }
});
await new Promise((resolve) => httpServer.listen(0, '127.0.0.1', resolve));
const url = `http://127.0.0.1:${httpServer.address().port}/fixture.html?scene=canvas`;

// ---------- browser (same launch args as the Termium server) ----------
let browser;
try {
  browser = await puppeteer.launch({
    headless: true, pipe: true, enableExtensions: true, defaultViewport: null, protocolTimeout: 15000,
    executablePath: process.env.PUPPETEER_EXECUTABLE_PATH || undefined,
    args: [
      '--disable-blink-features=AutomationControlled',
      '--user-agent=Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    ]
  });
} catch (e) { fail(`browser launch failed: ${e.message}`, 1); }

// Watchdog: bounded budget; close with a grace period, then kill only our launched process.
const watchdog = setTimeout(() => {
  console.error('run.mjs: watchdog timeout; closing browser with bounded grace');
  (async () => {
    try { await Promise.race([browser.close(), sleep(10 * 1000)]); } catch {}
    const proc = browser.process();
    if (proc && !proc.killed) { try { proc.kill('SIGKILL'); } catch {} }
    process.exit(1);
  })();
}, (warmup + seconds + 30) * 1000);

let exitCode = 0;
try {
  const page = await browser.newPage();
  await page.goto(url, {waitUntil: 'load', timeout: 15000});
  const cdp = await page.createCDPSession(); // dedicated target CDP session for capture work
  await cdp.send('Emulation.setDeviceMetricsOverride', {width, height, deviceScaleFactor: 1, mobile: false});
  await page.waitForFunction(() => Boolean(window.fixtureReady), {timeout: 15000});

  // One-time environment info outside the measurement window. SystemInfo needs a browser-target session.
  const browserSession = await browser.target().createCDPSession();
  const [browserGetVersion, systemInfo, commandLineRes, originEpochMS] = await Promise.all([
    browserSession.send('Browser.getVersion'),
    browserSession.send('SystemInfo.getInfo'),
    browserSession.send('Browser.getBrowserCommandLine'), // SystemInfo does not carry the command line
    page.evaluate(() => window.fixture?.originEpochMS ?? null)
  ]);

  // ---------- receipts, sampling, errors ----------
  const receipts = [];   // mono = monotonic ms; relative time computed at write-out against windowStart
  const samples = [];    // at most one payload per second (max 32), measurement phase only
  let lastSampleMono = -Infinity;
  const errors = [];
  function noteError(where, e) { if (errors.length < 64) errors.push({where, message: String(e?.message ?? e)}); }
  let windowOpen = false, windowStart = 0, windowEndPlanned = Infinity, windowEnd = 0;
  const openWindow = () => { windowOpen = true; windowStart = performance.now(); windowEndPlanned = windowStart + seconds * 1000; };
  function addReceipt(phase, mono, receiveEpochMS, buf, extra = {}) {
    receipts.push({phase, mono, receiveEpochMS, bytes: buf.length, digest: crypto.createHash('sha256').update(buf).digest('hex'), ...extra});
    if (phase === 'measurement' && samples.length < 32 && mono - lastSampleMono >= 1000) {
      lastSampleMono = mono;
      const file = `sample-${String(samples.length + 1).padStart(3, '0')}.${format}`;
      samples.push({file, buf, receiveEpochMS, relMS: round3(mono - windowStart), bytes: buf.length, metadata: extra.metadata ?? null});
    }
  }

  // Required Linux resource sampler (sibling resources.mjs); failures are recorded and fail the run.
  let sampler = null;
  try {
    const mod = await import('./resources.mjs');
    if (typeof mod.createSampler !== 'function') throw new Error('resources.mjs does not export createSampler');
    sampler = await mod.createSampler([process.pid, browser.process().pid]);
  } catch (e) { noteError('sampler-init', e); }
  let samplerRunning = false;
  const samplerStart = async () => { if (!sampler) return; try { await sampler.start(); samplerRunning = true; } catch (e) { noteError('sampler-start', e); } };
  const samplerStop = async () => { if (!sampler || !samplerRunning) return null; try { return await sampler.stop(); } catch (e) { noteError('sampler-stop', e); return null; } };

  // ---------- mode: screenshot (persistent session, sequential RPCs, no FPS cap) ----------
  async function runScreenshot() {
    const opts = {format, fromSurface: true, captureBeyondViewport: false, optimizeForSpeed: true};
    if (format === 'jpeg') opts.quality = 60;
    let consecutiveErrors = 0; // repeated failures abort instead of spinning quietly
    const warmupDeadline = performance.now() + warmup * 1000; // warmup is seconds in both modes
    while (performance.now() < warmupDeadline) {
      let res;
      try { res = await cdp.send('Page.captureScreenshot', opts); }
      catch (e) { noteError('warmup-capture', e); if (++consecutiveErrors >= 10) throw new Error(`repeated capture failures (${consecutiveErrors} in a row)`); continue; }
      consecutiveErrors = 0;
      const mono = performance.now(), receiveEpochMS = Date.now(); // timestamps before decode/hash
      addReceipt('warmup', mono, receiveEpochMS, Buffer.from(res.data, 'base64'));
    }
    await samplerStart(); // immediately before the measurement window
    openWindow();
    const cpuBefore = process.cpuUsage(), rssBefore = process.memoryUsage().rss, cpuAccountStartMS = performance.now();
    while (true) {
      const t0 = performance.now();
      if (t0 - windowStart >= seconds * 1000) break; // never start new work after the window
      let res;
      try { res = await cdp.send('Page.captureScreenshot', opts); }
      catch (e) { noteError('capture', e); if (++consecutiveErrors >= 10) throw new Error(`repeated capture failures (${consecutiveErrors} in a row)`); continue; }
      consecutiveErrors = 0;
      const mono = performance.now(), receiveEpochMS = Date.now(); // timestamps before decode/hash
      const buf = Buffer.from(res.data, 'base64');
      // Complete captures arriving past the window end are late: recorded, excluded from measurement stats.
      addReceipt(mono < windowEndPlanned ? 'measurement' : 'late', mono, receiveEpochMS, buf, {rpcLatencyMS: round3(mono - t0)});
    }
    windowEnd = performance.now();
    return {cpuBefore, rssBefore, cpuAccountStartMS};
  }

  // ---------- mode: stream (screencast with per-frame ack) ----------
  async function runStream() {
    const opts = {format, maxWidth: width, maxHeight: height, everyNthFrame: 1};
    if (format === 'jpeg') opts.quality = 60;
    const pendingAcks = new Set(); // true in-flight ack state, bounded at 128
    const onFrame = (event) => {
      try {
        const mono = performance.now(), receiveEpochMS = Date.now(); // synchronous timestamps at receipt
        if (pendingAcks.size >= 128) { noteError('ack-overflow', new Error('more than 128 outstanding screencast acks')); return; }
        const buf = Buffer.from(event.data, 'base64'); // consume payload before acking
        const phase = !windowOpen ? 'warmup' : (mono < windowEndPlanned ? 'measurement' : 'late');
        addReceipt(phase, mono, receiveEpochMS, buf, {metadata: event.metadata ?? null});
        const base = cdp.send('Page.screencastFrameAck', {sessionId: event.sessionId}).catch((e) => noteError('ack', e));
        const tracked = base.finally(() => pendingAcks.delete(tracked)); // removed on settle of the already-caught promise
        pendingAcks.add(tracked);
      } catch (e) { noteError('frame-handler', e); }
    };
    cdp.on('Page.screencastFrame', onFrame); // listener registered before startScreencast
    await cdp.send('Page.startScreencast', opts);
    if (warmup > 0) await sleep(warmup * 1000); // warmup arrivals counted separately
    await samplerStart(); // immediately before the measurement window
    openWindow();
    const cpuBefore = process.cpuUsage(), rssBefore = process.memoryUsage().rss, cpuAccountStartMS = performance.now();
    while (performance.now() < windowEndPlanned) await sleep(25);
    try { await cdp.send('Page.stopScreencast'); } catch (e) { noteError('stop-screencast', e); }
    // Handler stays active until the stop command returns and outstanding acks drain.
    while (pendingAcks.size > 0) { await Promise.allSettled([...pendingAcks]); if (pendingAcks.size === 0) break; await sleep(10); }
    cdp.off('Page.screencastFrame', onFrame);
    windowEnd = performance.now();
    return {cpuBefore, rssBefore, cpuAccountStartMS};
  }

  const {cpuBefore, rssBefore, cpuAccountStartMS} = mode === 'screenshot' ? await runScreenshot() : await runStream();
  // CPU delta immediately after the mode returns (before sampler stop); process.cpuUsage units are microseconds.
  const cpuDelta = process.cpuUsage(cpuBefore);
  const cpuAccountingDurationMS = round3(performance.now() - cpuAccountStartMS); // may cover a slight tail past windowEnd
  const samplerResult = await samplerStop(); // immediately after the measurement window

  // ---------- stats and outputs (all writes happen after stop/end) ----------
  const meas = receipts.filter((r) => r.phase === 'measurement'); // mono in [windowStart, windowEndPlanned)
  const countOf = (p) => receipts.filter((r) => r.phase === p).length;
  let payloadChanges = 0; // measured receipts only, never warmup+late
  for (let i = 1; i < meas.length; i++) if (meas[i].digest !== meas[i - 1].digest) payloadChanges++;
  const dist = (vals) => {
    if (!vals.length) return null;
    const s = [...vals].sort((a, b) => a - b);
    const q = (p) => { const i = (s.length - 1) * p, lo = Math.floor(i), hi = Math.ceil(i); return round3(s[lo] + (s[hi] - s[lo]) * (i - lo)); };
    return {min: round3(s[0]), p50: q(0.5), p95: q(0.95), max: round3(s[s.length - 1])};
  };
  const intervals = []; for (let i = 1; i < meas.length; i++) intervals.push(meas[i].mono - meas[i - 1].mono);
  const windowActualMS = round3(windowEnd - windowStart); // actual stopped time, separate from the requested denominator
  const measBytes = meas.reduce((n, r) => n + r.bytes, 0);
  const ok = meas.length > 0 && errors.length === 0; // any captured error or zero frames fails the run
  const result = {
    ok,
    options: {mode, format, width, height, warmup, seconds},
    fixture: {originEpochMS},
    windowActualMS,
    counts: {warmup: countOf('warmup'), measurement: meas.length, late: countOf('late')},
    fps: round3(meas.length / seconds), // exact requested-seconds denominator; a late finish never enlarges it
    bytesPerSec: Math.round(measBytes / seconds),
    payloadChanges,
    arrivalIntervalsMS: dist(intervals),
    screenshotRTTMS: dist(meas.map((r) => r.rpcLatencyMS).filter((v) => v != null)),
    nodeCPUSeconds: {user: round3(cpuDelta.user / 1e6), system: round3(cpuDelta.system / 1e6), accountingDurationMS: cpuAccountingDurationMS},
    nodeRSSBytes: {windowStart: rssBefore, windowEnd: process.memoryUsage().rss},
    envInfo: {browserGetVersion, systemInfo, commandLine: commandLineRes.arguments}, // exact browser command line
    sampler: samplerResult ?? null,
    errors,
    samples: samples.map(({buf, ...rec}) => rec)
  };
  for (const s of samples) fs.writeFileSync(path.join(outDir, s.file), s.buf); // sample images after stop/end
  const relReceipts = receipts.map(({mono, ...r}) => ({...r, relMS: round3(mono - windowStart)}));
  fs.writeFileSync(path.join(outDir, 'result.json'), JSON.stringify(result, null, 2));
  fs.writeFileSync(path.join(outDir, 'receipts.json'), JSON.stringify(relReceipts, null, 2));
  console.log(JSON.stringify({mode,format,width,height,fps:result.fps,frames:meas.length,ok,outDir}));
  if (!ok) { console.error(`run.mjs: run failed (measurement frames=${meas.length}, errors=${errors.length}); not reporting success`); exitCode = 1; }
} catch (e) {
  console.error(`run.mjs fatal: ${e?.stack ?? e}`);
  exitCode = 1;
} finally {
  try { await browser.close(); } catch {} // watchdog stays armed until cleanup completes
  httpServer.close();
  clearTimeout(watchdog); // cleared only after cleanup, never before the normal close
  process.exit(exitCode);
}
