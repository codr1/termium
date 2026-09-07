import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const dest = path.join(root, 'server/dist/extensions/vimium');
await fs.rm(dest, { recursive: true, force: true });
await fs.mkdir(path.dirname(dest), { recursive: true });
await fs.cp(path.join(root, 'third_party/vimium'), dest, { recursive: true });

// One welcome-page source serves both the website and the offline bundle.
const site = path.join(root, 'site');
let html = await fs.readFile(path.join(site, 'index.html'), 'utf8');
html = html.replace(/<title>.*?<\/title>/, '<title>New tab · Termium</title>');
html = html.replace('assets/welcome.css', 'termium.css').replace('assets/mark.svg', 'termium-mark.svg');
html = html.replace('</head>', '<link rel="stylesheet" href="../content_scripts/vimium.css"><script type="module" src="termium.js"></script></head>');
await fs.writeFile(path.join(dest, 'pages/termium.html'), html);
await fs.copyFile(path.join(site, 'assets/welcome.css'), path.join(dest, 'pages/termium.css'));
await fs.copyFile(path.join(site, 'assets/mark.svg'), path.join(dest, 'pages/termium-mark.svg'));
