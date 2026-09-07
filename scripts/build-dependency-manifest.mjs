import fs from 'node:fs/promises';
import { createReadStream } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { install, Browser, detectBrowserPlatform, getDownloadUrl } from '@puppeteer/browsers';

async function sha256(file) {
    const hash = createHash('sha256');
    for await (const chunk of createReadStream(file)) hash.update(chunk);
    return hash.digest('hex');
}

export async function reviewedVimiumFiles(directory) {
    const files = {};
    async function walk(relative = '') {
        for (const entry of await fs.readdir(path.join(directory, relative), { withFileTypes: true })) {
            const name = path.posix.join(relative, entry.name);
            if (['TERMIUM.md', 'pages/termium.js'].includes(name)) continue;
            if (entry.isDirectory()) await walk(name);
            else if (entry.isFile()) files[name] = await sha256(path.join(directory, name));
            else throw Error(`Unsupported Vimium source entry: ${name}`);
        }
    }
    await walk();
    if (!files['manifest.json']) throw Error('Vimium manifest is missing');
    return files;
}

// Checksums are produced by the release build and authenticated with the app
// archive. Installation never trusts a checksum fetched beside an upstream ZIP.
export async function buildDependencyManifest(root, cache, chromeVersion) {
    const platform = detectBrowserPlatform();
    const chrome = await install({ browser: Browser.CHROME, buildId: chromeVersion, cacheDir: path.join(cache, 'archives'), unpack: false });
    const provenance = await fs.readFile(path.join(root, 'third_party/vimium/TERMIUM.md'), 'utf8');
    const revision = provenance.match(/Revision: ([a-f0-9]{40})\b/)?.[1];
    if (!revision) throw Error('Missing pinned Vimium revision');
    const vimiumURL = `https://codeload.github.com/philc/vimium/zip/${revision}`;
    const vimium = path.join(cache, `vimium-${revision}.zip`);
    // Fetch the immutable source archive anew: the release must not certify a
    // possibly modified cache entry. The installer caches by its verified hash.
    const response = await fetch(vimiumURL, { signal: AbortSignal.timeout(120000) });
    if (!response.ok) throw Error(`Vimium download failed: ${response.status}`);
    await fs.writeFile(vimium, Buffer.from(await response.arrayBuffer()));
    return [
        { name: 'browser', version: chromeVersion, url: getDownloadUrl(Browser.CHROME, platform, chromeVersion).href, sha256: await sha256(chrome), stripPrefix: '' },
        { name: 'vimium', version: revision, url: vimiumURL, sha256: await sha256(vimium), stripPrefix: `vimium-${revision}/`, files: await reviewedVimiumFiles(path.join(root, 'third_party/vimium')) },
    ];
}
