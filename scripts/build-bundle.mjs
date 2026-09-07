import fs from 'node:fs/promises';
import { createReadStream } from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { install, Browser } from '@puppeteer/browsers';
import { PUPPETEER_REVISIONS } from 'puppeteer-core';
import { buildDependencyManifest } from './build-dependency-manifest.mjs';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const lock = JSON.parse(await fs.readFile(path.join(root, 'scripts/runtime-lock.json'), 'utf8'));
const nodePlatform = `${process.platform}-${process.arch}`;
if (!lock.sha256[nodePlatform]) throw Error(`No bundle target for ${nodePlatform}`);
const platform = `${process.platform}-${process.arch === 'x64' ? 'amd64' : 'arm64'}`;
const commit = execFileSync('git', ['rev-parse', '--short=12', 'HEAD'], { cwd: root, encoding: 'utf8' }).trim();
const version = process.env.TERMIUM_VERSION || `dev-${commit}`;
if (!/^[a-zA-Z0-9][a-zA-Z0-9._-]*$/.test(version)) throw Error('Invalid bundle version');
const run = (command, args, options = {}) => execFileSync(command, args, { cwd: root, stdio: 'inherit', ...options });
const cache = path.join(os.homedir(), '.cache', 'termium-build');
await fs.mkdir(cache, { recursive: true });
await fs.mkdir(path.join(root, 'dist'), { recursive: true });
// Stay outside the checkout: Node must not resolve an accidentally omitted
// production dependency from the developer's ancestor node_modules directory.
const work = await fs.mkdtemp(path.join(os.tmpdir(), 'termium-bundle-'));
const bundle = path.join(work, 'app');
const hash = async file => {
    const digest = createHash('sha256');
    for await (const chunk of createReadStream(file)) digest.update(chunk);
    return digest.digest('hex');
};
try {
    await fs.mkdir(path.join(bundle, 'bin'), { recursive: true });
    await fs.mkdir(path.join(bundle, 'notices'), { recursive: true });
    run('npm', ['run', 'build:client', '--', '-ldflags', `-s -w -X main.version=${version} -X main.commit=${commit}`], { env: { ...process.env, CGO_ENABLED: '0' } });
    await fs.copyFile(path.join(root, 'client/termium'), path.join(bundle, 'bin/termium'));
    await fs.cp(path.join(root, 'server/dist'), path.join(bundle, 'server/dist'), { recursive: true });
    // Reproduce production dependencies from the workspace lock, with no
    // lifecycle scripts, toolchain, or implicit browser download at install time.
    const deps = path.join(work, 'deps');
    await fs.mkdir(path.join(deps, 'server'), { recursive: true });
    for (const file of ['package.json', 'package-lock.json', 'server/package.json']) {
        await fs.copyFile(path.join(root, file), path.join(deps, file));
    }
    const depEnv = Object.fromEntries(Object.entries(process.env).filter(([key]) => key.toLowerCase() !== 'npm_config_allow_scripts'));
    run('npm', ['ci', '--omit=dev', '--ignore-scripts', '--no-audit', '--no-fund', '--userconfig', '/dev/null'], { cwd: deps, env: depEnv });
    await fs.cp(path.join(deps, 'node_modules'), path.join(bundle, 'node_modules'), { recursive: true, verbatimSymlinks: true });
    await fs.copyFile(path.join(deps, 'server/package.json'), path.join(bundle, 'server/package.json'));
    try { await fs.cp(path.join(deps, 'server/node_modules'), path.join(bundle, 'server/node_modules'), { recursive: true, verbatimSymlinks: true }); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }

    const archiveName = `node-v${lock.node}-${nodePlatform}.tar.gz`;
    const nodeArchive = path.join(cache, archiveName);
    try { if (await hash(nodeArchive) !== lock.sha256[nodePlatform]) await fs.unlink(nodeArchive); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
    try { await fs.access(nodeArchive); } catch {
        run('curl', ['--fail', '--location', '--retry', '3', '--connect-timeout', '15', `https://nodejs.org/dist/v${lock.node}/${archiveName}`, '-o', nodeArchive]);
    }
    if (await hash(nodeArchive) !== lock.sha256[nodePlatform]) throw Error('Node archive checksum mismatch');
    await fs.mkdir(path.join(bundle, 'runtime'));
    run('tar', ['xzf', nodeArchive, '--strip-components=1', '-C', path.join(bundle, 'runtime')]);
    // Only the runtime and its license ship; users never run npm.
    for (const entry of ['include', 'share', 'lib', 'CHANGELOG.md', 'README.md', 'bin/npm', 'bin/npx', 'bin/corepack']) {
        await fs.rm(path.join(bundle, 'runtime', entry), { recursive: true, force: true });
    }
    const chrome = await install({ browser: Browser.CHROME, buildId: PUPPETEER_REVISIONS.chrome, cacheDir: path.join(cache, 'browsers') });
    await fs.cp(chrome.path, path.join(bundle, 'browser'), { recursive: true, verbatimSymlinks: true });
    const browserPath = path.join('browser', path.relative(chrome.path, chrome.executablePath));
    const dependencies = await buildDependencyManifest(root, cache, PUPPETEER_REVISIONS.chrome);
    await fs.writeFile(path.join(bundle, 'bundle.json'), JSON.stringify({ version, commit, platform, node: lock.node, chrome: PUPPETEER_REVISIONS.chrome, browser: browserPath, dependencies }, null, 2)+'\n');
    await fs.writeFile(path.join(bundle, 'fonts.conf'), '<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd"><fontconfig><dir prefix="relative">fonts</dir><cachedir prefix="xdg">termium/fontconfig</cachedir></fontconfig>\n');
    await fs.cp(path.join(root, 'third_party/go-sixel'), path.join(bundle, 'notices/go-sixel'), { recursive: true });
    const modules = execFileSync('go', ['list', '-m', '-f', '{{if .Dir}}{{.Path}}|{{.Dir}}{{end}}', 'all'], { cwd: root, encoding: 'utf8' });
    for (const entry of modules.trim().split('\n')) {
        const separator = entry.indexOf('|');
        if (separator < 0) continue;
        const module = entry.slice(0, separator), directory = entry.slice(separator + 1);
        const destination = path.join(bundle, 'notices/go', module);
        for (const file of await fs.readdir(directory)) {
            if (/^(licen[sc]e|copying|notice|patents)/i.test(file)) {
                await fs.mkdir(destination, { recursive: true });
                await fs.cp(path.join(directory, file), path.join(destination, file), { recursive: true });
            }
        }
    }
    const goroot = execFileSync('go', ['env', 'GOROOT'], { cwd: root, encoding: 'utf8' }).trim();
    await fs.copyFile(path.join(goroot, 'LICENSE'), path.join(bundle, 'notices/Go-LICENSE'));
    await fs.copyFile(path.join(root, 'README.md'), path.join(bundle, 'README.md'));
    if (process.platform === 'linux') {
        run('docker', ['run', '--rm', '-e', `TERMIUM_BUILD_UID=${process.getuid()}`, '-e', `TERMIUM_BUILD_GID=${process.getgid()}`, '-v', `${bundle}:/bundle`, '-v', `${root}/scripts/bundle-linux-libs.sh:/build-libs:ro`, lock.linuxImage, 'sh', '/build-libs']);
    }
    run(path.join(bundle, 'bin/termium'), ['--doctor']);
    // End users fetch the pinned dependencies during installation. Keep only
    // Termium's welcome-page overlay in the extension directory of the app.
    await fs.rm(path.join(bundle, 'browser'), { recursive: true });
    const extension = path.join(bundle, 'server/dist/extensions/vimium');
    const overlay = new Map();
    for (const file of ['termium.js', 'termium.html', 'termium.css', 'termium-mark.svg']) {
        overlay.set(file, await fs.readFile(path.join(extension, 'pages', file)));
    }
    await fs.rm(extension, { recursive: true });
    await fs.mkdir(path.join(extension, 'pages'), { recursive: true });
    for (const [file, data] of overlay) await fs.writeFile(path.join(extension, 'pages', file), data);
    const artifact = `termium-${platform}.tar.gz`;
    run('tar', ['czf', path.join(root, 'dist', artifact), '-C', bundle, '.']);
    await fs.writeFile(path.join(root, 'dist', `${artifact}.sha256`), `${await hash(path.join(root, 'dist', artifact))}  ${artifact}\n`);
    console.log(`Ready: dist/${artifact}`);
} finally {
    if (process.env.TERMIUM_KEEP_BUNDLE === '1') console.log(`Build workspace: ${work}`);
    else await fs.rm(work, { recursive: true, force: true });
}
