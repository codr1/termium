const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');

test('release overlay retains the welcome page asset graph and font licenses', async t => {
  const { retainWelcomeOverlay } = await import('../../scripts/retain-welcome-overlay.mjs');
  const root = path.resolve(__dirname, '../..');
  const work = await fs.mkdtemp(path.join(os.tmpdir(), 'termium-welcome-bundle-'));
  t.after(() => fs.rm(work, { recursive: true, force: true }));
  const extension = path.join(work, 'vimium');
  // Exercise the generated extension consumed by build-bundle, not a second
  // rendering of the source HTML with independently maintained path rewrites.
  await fs.cp(path.join(root, 'server/dist/extensions/vimium'), extension, { recursive: true });
  await retainWelcomeOverlay(extension);
  assert.deepEqual(await fs.readdir(extension), ['pages']);
  await assert.rejects(fs.access(path.join(extension, 'manifest.json')), { code: 'ENOENT' });
  await assert.rejects(fs.access(path.join(extension, 'pages/options.html')), { code: 'ENOENT' });

  const html = await fs.readFile(path.join(extension, 'pages/termium.html'), 'utf8');
  const checked = new Set();
  async function checkAsset(relative) {
    if (checked.has(relative)) return;
    checked.add(relative);
    const data = await fs.readFile(path.join(extension, relative));
    assert.ok(data.length, `empty welcome asset: ${relative}`);
    if (relative.endsWith('.css')) {
      for (const [, reference] of data.toString().matchAll(/url\(["']?([^"')]+)["']?\)/g)) {
        await checkAsset(path.posix.normalize(path.posix.join(path.posix.dirname(relative), reference)));
      }
    }
  }
  for (const [, reference] of html.matchAll(/<(?:link|script)\b[^>]*\b(?:href|src)="([^"]+)"/g)) {
    const relative = path.posix.normalize(path.posix.join('pages', reference));
    // This stylesheet belongs to the verified upstream dependency restored by
    // the installer; every other head asset must ship in the local overlay.
    if (relative === 'content_scripts/vimium.css') continue;
    await checkAsset(relative);
  }
  for (const name of await fs.readdir(path.join(root, 'site/assets/fonts'))) {
    assert.deepEqual(
      await fs.readFile(path.join(extension, 'pages/fonts', name)),
      await fs.readFile(path.join(root, 'site/assets/fonts', name)),
      `font payload or license changed: ${name}`,
    );
  }
  assert.deepEqual(
    await fs.readFile(path.join(extension, 'pages/termium.js')),
    await fs.readFile(path.join(root, 'third_party/vimium/pages/termium.js')),
  );
});
