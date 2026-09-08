// Reproducible capture fixtures for comparing both renderer preparation paths.
// This measures Chromium capture, not terminal presentation or visible FPS.
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import puppeteer from 'puppeteer';

const output = path.resolve('dist/capture-benchmark');
await fs.mkdir(output, { recursive: true });
const browser = await puppeteer.launch({ headless: true });
const report = { platform: `${os.platform()}/${os.arch()}`, cpu: os.cpus()[0]?.model, chromium: await browser.version(), viewport: [1280, 720], results: [] };
try {
  const page = await browser.newPage();
  await page.setViewport({ width: 1280, height: 720, deviceScaleFactor: 1 });
  const session = await page.createCDPSession();
  for (const scene of ['text', 'canvas']) {
    await page.setContent('<body style="background:#101722;color:white;font:18px monospace"><h1>Termium capture benchmark</h1>' +
      Array.from({ length: 80 }, (_, i) => `<p>Line ${i}: Chromium, tabs, keyboard navigation, and terminal graphics.</p>`).join('') +
      `<canvas width="1280" height="720" style="position:fixed;inset:0;display:${scene === 'canvas' ? 'block' : 'none'}"></canvas>`);
    if (scene === 'canvas') await page.evaluate(() => {
      const canvas = document.querySelector('canvas');
      const context = canvas.getContext('2d');
      const pixels = context.createImageData(canvas.width, canvas.height);
      let seed = 1234567;
      for (let i = 0; i < pixels.data.length; i += 4) {
        seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
        pixels.data[i] = seed & 255;
        pixels.data[i + 1] = (seed >>> 8) & 255;
        pixels.data[i + 2] = (seed >>> 16) & 255;
        pixels.data[i + 3] = 255;
      }
      context.putImageData(pixels, 0, 0);
    });
    const samples = Object.fromEntries(['jpeg', 'png', 'fast-png'].map(format => [format, []]));
    const sizes = {};
    // Interleave formats and discard warmup, rather than timing one cold path.
    for (let i = 0; i < 15; i++) for (const format of Object.keys(samples)) {
      const start = performance.now();
      const result = await session.send('Page.captureScreenshot', {
        format: format === 'jpeg' ? 'jpeg' : 'png',
        ...(format === 'jpeg' ? { quality: 60 } : {}),
        optimizeForSpeed: format !== 'png', captureBeyondViewport: false, fromSurface: true,
      });
      const data = Buffer.from(result.data, 'base64');
      if (i >= 3) samples[format].push(performance.now() - start);
      sizes[format] = data.length;
      if (i === 14) await fs.writeFile(path.join(output, `${scene}-${format}.${format === 'jpeg' ? 'jpg' : 'png'}`), data);
    }
    for (const [format, timings] of Object.entries(samples)) {
      timings.sort((a, b) => a - b);
      const result = { scene, format, median_ms: timings[Math.floor(timings.length / 2)], p95_ms: timings[Math.ceil(timings.length * .95) - 1], bytes: sizes[format] };
      report.results.push(result);
      console.log(`${scene} ${format}: ${result.median_ms.toFixed(1)} ms median, ${result.bytes} bytes`);
    }
  }
} finally {
  await browser.close();
}
await fs.writeFile(path.join(output, 'capture.json'), JSON.stringify(report, null, 2) + '\n');
console.log(`Captures and measurements: ${output}`);
