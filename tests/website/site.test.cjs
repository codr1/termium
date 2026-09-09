const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const http = require('node:http');
const puppeteer = require('puppeteer');

const root = path.resolve(__dirname, '../../dist/website');
async function fixture(t) {
  const server = http.createServer((req, res) => {
    const route = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
    let file = path.resolve(root, '.' + route);
    if (!file.startsWith(root + path.sep) && file !== root) { res.writeHead(403); res.end(); return; }
    if (fs.existsSync(file) && fs.statSync(file).isDirectory()) file = path.join(file, 'index.html');
    if (!fs.existsSync(file)) { file = path.join(root, '404.html'); res.statusCode = 404; }
    res.setHeader('content-type', ({ '.html': 'text/html', '.css': 'text/css', '.mjs': 'text/javascript', '.js': 'text/javascript', '.svg': 'image/svg+xml', '.woff2': 'font/woff2' })[path.extname(file)] || 'text/plain');
    res.setHeader('content-security-policy', "default-src 'self'; style-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'");
    res.end(fs.readFileSync(file));
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(() => { server.closeAllConnections(); server.close(); });
  const browser = await puppeteer.launch({ headless: true, pipe: true, enableExtensions: true });
  t.after(() => browser.close());
  return { browser, origin: `http://127.0.0.1:${server.address().port}` };
}

test('public site is complete, links resolve, and the welcome page stays separate', { timeout: 30000 }, async t => {
  const { browser, origin } = await fixture(t);
  const page = await browser.newPage();
  const queue = ['/'];
  const visited = new Set();
  const anchors = [];
  while (queue.length) {
    const route = queue.shift();
    if (visited.has(route)) continue;
    visited.add(route);
    const response = await page.goto(origin + route);
    assert.equal(response.status(), 200, route);
    await page.evaluate(() => document.fonts.ready);
    const loadedFonts = await page.evaluate(() => [...document.fonts].filter(f => f.status === 'loaded').map(f => f.family));
    for (const family of ['Inter', 'JetBrains Mono']) assert.ok(loadedFonts.includes(family), `${family} did not load under the site CSP on ${route}`);
    assert.equal(await page.$$eval('h1', es => es.length), 1, route);
    assert.equal(await page.$('meta[name="termium-welcome"]'), null, `Marketing content must never become an offline welcome: ${route}`);
    const links = await page.$$eval('a[href]', es => es.map(e => e.getAttribute('href')));
    for (const href of links) {
      const url = new URL(href, origin + route);
      assert.ok(!([origin, 'https://termium.dev'].includes(url.origin) && /^\/(welcome|home)(\/|$)/.test(url.pathname)), `Public link to private welcome: ${href}`);
      if (url.origin !== origin) continue;
      queue.push(url.pathname);
      if (url.hash) anchors.push([url.pathname, decodeURIComponent(url.hash.slice(1))]);
    }
  }
  assert.equal(visited.size, 11, 'Expected product page, docs index, and nine guides');
  for (const [route, id] of anchors) {
    await page.goto(origin + route);
    assert.ok(await page.evaluate(id => !!document.getElementById(id), id), `Broken fragment ${route}#${id}`);
  }
  const welcome = await page.goto(origin + '/welcome/');
  assert.equal(welcome.status(), 200);
  assert.equal(await page.$eval('meta[name="termium-welcome"]', e => e.content), '1');
  assert.equal(await page.$eval('meta[name="robots"]', e => e.content), 'noindex, nofollow');
  assert.ok(!fs.readFileSync(path.join(root, 'sitemap.xml'), 'utf8').includes('/welcome'));
  assert.ok(fs.readFileSync(path.join(root, 'robots.txt'), 'utf8').includes('Disallow: /welcome'));
  assert.equal((await page.goto(origin + '/missing-page')).status(), 404);
  assert.equal(await page.$eval('h1', e => e.textContent).then(s => s.includes('wrong turn')), true);
});

test('pages fit phone and desktop widths and work without JavaScript', { timeout: 30000 }, async t => {
  const { browser, origin } = await fixture(t);
  const page = await browser.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  const pages = ['/', '/welcome/', '/docs/', ...fs.readdirSync(path.join(root, 'docs'), { withFileTypes: true }).filter(e => e.isDirectory()).map(e => `/docs/${e.name}/`)];
  for (const route of pages) for (const width of [320, 390, 580, 768, 900, 1280]) {
    await page.setViewport({ width, height: 800 });
    await page.goto(origin + route);
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `Overflow at ${width}px on ${route}`);
  }
  await page.setJavaScriptEnabled(false);
  await page.goto(origin + '/');
  await page.click('a[href="/docs/installation/"]');
  await page.waitForSelector('.docs-content h1');
  assert.equal(new URL(page.url()).pathname, '/docs/installation/');
  assert.ok(await page.$('pre code.language-bash'), 'Installation commands require JavaScript');
  assert.deepEqual(errors, []);
});

test('copy controls work under the site CSP and Vimium can navigate the public site', { timeout: 30000 }, async t => {
  const { browser, origin } = await fixture(t);
  const page = await browser.newPage();
  await browser.defaultBrowserContext().overridePermissions(origin, ['clipboard-read', 'clipboard-write', 'clipboard-sanitized-write']);
  await page.goto(origin + '/docs/installation/');
  const command = await page.$eval('pre code.language-bash', e => e.textContent);
  await page.click('.copy-button');
  await page.waitForFunction(() => document.querySelector('.copy-button').textContent === 'Copied');
  assert.equal(await page.evaluate(() => navigator.clipboard.readText()), command);
  assert.equal(await page.$eval('.copy-button', e => e.textContent), 'Copied');
  await page.evaluate(() => { navigator.clipboard.writeText = async () => { throw Error('Clipboard denied'); }; });
  await page.click('.copy-button');
  await page.waitForFunction(() => document.querySelector('.copy-button').textContent.startsWith('Selected'));
  assert.equal(await page.evaluate(() => getSelection().toString()), command.trimEnd());
  const { BrowserSession } = require('../../server/dist/src/browser-session');
  const session = new BrowserSession(async () => browser);
  const active = await session.ensurePage();
  await active.goto(origin + '/');
  await session.vimium.waitForPage(active);
  await active.keyboard.type('f');
  await active.waitForSelector('.vimiumHintMarker', { visible: true, timeout: 5000 });
  assert.ok(await active.$$eval('.vimiumHintMarker', es => es.length) >= 3);
});


test('public setup serves the bootstrap and copies the same one-line command everywhere', { timeout: 30000 }, async t => {
  const { browser, origin } = await fixture(t);
  const response = await fetch(origin + '/install');
  assert.equal(response.status, 200);
  assert.match(response.headers.get('content-type'), /text\/plain/);
  assert.equal(await response.text(), fs.readFileSync(path.resolve(__dirname, '../../scripts/install.sh'), 'utf8'));
  const page = await browser.newPage();
  await browser.defaultBrowserContext().overridePermissions(origin, ['clipboard-read', 'clipboard-write', 'clipboard-sanitized-write']);
  await page.goto(origin + '/');
  const command = await page.$eval('.install-command code', e => e.textContent);
  assert.equal(command.split('\n').length, 1);
  assert.equal(command, 'curl -fsSL https://termium.dev/install | bash');
  await page.click('.install-command .copy-button');
  await page.waitForFunction(() => document.querySelector('.install-command .copy-button').textContent === 'Copied');
  assert.equal(await page.evaluate(() => navigator.clipboard.readText()), command);
  assert.equal(await page.$$eval('.github-cta img', es => es.length), 2);
  assert.ok(await page.$$eval('.github-cta img', es => es.every(e => e.complete && e.naturalWidth > 0)));
  await page.goto(origin + '/docs/installation/');
  assert.equal(await page.$eval('pre code.language-bash', e => e.textContent.trim()), command);
  assert.ok(fs.readFileSync(path.resolve(__dirname, '../../README.md'), 'utf8').includes(command));
  await page.goto(origin + '/docs/quickstart/');
  assert.equal(await page.$eval('pre code.language-bash', e => e.textContent.trim()), command);
});

test('welcome appears with settled fonts and remains usable when fonts stall or fail', { timeout: 30000 }, async t => {
  const { browser, origin } = await fixture(t);
  for (const outcome of ['loaded', 'failed', 'stalled']) {
    const page = await browser.newPage();
    await page.setViewport({ width: 984, height: 640 });
    await page.setCacheEnabled(false);
    await page.setRequestInterception(true);
    const held = [];
    let requested;
    const fontRequests = new Promise(resolve => { requested = resolve; });
    page.on('request', request => {
      if (request.url().endsWith('.woff2')) {
        held.push(request);
        if (held.length === 2) requested();
      } else void request.continue();
    });
    await page.goto(origin + '/welcome/', { waitUntil: 'domcontentloaded' });
    await fontRequests;
    assert.equal(await page.$eval('main', e => getComputedStyle(e).visibility), 'hidden', 'fallback layout flashed while fonts were pending');
    if (outcome === 'loaded') {
      await Promise.all(held.map(request => request.continue()));
    } else if (outcome === 'failed') {
      await Promise.all(held.map(request => request.abort()));
    }
    await page.waitForFunction(() => getComputedStyle(document.querySelector('main')).visibility === 'visible');
    const fallback = await page.evaluate(() => document.documentElement.classList.contains('welcome-fonts-fallback'));
    assert.equal(fallback, outcome !== 'loaded');
    const geometry = () => page.$eval('.ascii-mark', e => {
      const { width, height } = e.getBoundingClientRect();
      return { width, height, family: getComputedStyle(e).fontFamily };
    });
    const visible = await geometry();
    if (outcome === 'stalled') {
      // Fonts arriving after the deadline must not cause the second layout jump.
      await Promise.all(held.map(request => request.continue()));
      await page.evaluate(() => document.fonts.ready);
      assert.deepEqual(await geometry(), visible);
    }
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.close();
  }
  const page = await browser.newPage();
  await page.setJavaScriptEnabled(false);
  await page.goto(origin + '/welcome/');
  assert.equal(await page.$eval('main', e => getComputedStyle(e).visibility), 'visible', 'no-JS welcome stayed hidden');
  await page.close();
});
