import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { Marked } from 'marked';
import { execFileSync } from 'node:child_process';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const source = path.join(root, 'site');
const output = path.join(root, 'dist/website');
const origin = 'https://termium.dev';
let revision = 'main';
try { revision = execFileSync('git', ['rev-parse', 'HEAD'], { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim(); } catch { /* Source archives can use the default branch. */ }
const githubSource = 'https://github.com/codr1/termium/blob/' + revision;
const docs = [
  ['quickstart', 'Quickstart', 'Install, open your first page, and learn the essential keys.', 'Start here'],
  ['installation', 'Installation', 'Build, install, and launch Termium.', 'Start here'],
  ['getting-started', 'Getting started', 'Your first page, mouse input, menus, and home-page settings.', 'Start here'],
  ['vimium', 'Keyboard & tabs', 'Vimium bindings, real tabs, and the mouse keys.', 'Using Termium'],
  ['terminals', 'Terminal support', 'Kitty, sixel, ASCII graphics, and graphics performance.', 'Using Termium'],
  ['troubleshooting', 'Troubleshooting', 'Resolve display, navigation, and startup problems.', 'Using Termium'],
  ['development', 'Development', 'Build from source and contribute a change.', 'Contributing'],
  ['testing', 'Testing', 'Browser, terminal, and installation checks.', 'Contributing'],
  ['architecture', 'Architecture', 'How the browser, input, and rendering fit together.', 'Contributing'],
];
const escape = value => value.replaceAll('&', '&amp;').replaceAll('"', '&quot;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
const header = current => `<a class="skip-link" href="#main">Skip to content</a>
<header class="site-header container"><a class="brand" href="/" aria-label="Termium website"><img src="/assets/mark.svg" alt="" width="26" height="26"><span>termium<span class="accent">_</span></span></a>
<nav class="site-nav" aria-label="Main"><a href="/docs/quickstart/">Quickstart</a><a href="/docs/"${current === 'docs' ? ' aria-current="page"' : ''}>Docs</a><a class="github-cta" href="https://github.com/codr1/termium"><img class="github-icon" src="/assets/github.svg" alt="" width="16" height="16">GitHub ↗</a><a class="nav-install" href="/docs/installation/"${current === 'installation' ? ' aria-current="page"' : ''}>Get Termium</a></nav></header>`;
const footer = `<footer class="site-footer container"><div><a class="brand" href="/"><span>termium<span class="accent">_</span></span></a><p>Chromium. In your terminal.</p></div><nav aria-label="Footer"><a href="/docs/">Documentation</a><a href="/docs/development/">Contribute</a><a href="https://github.com/codr1/termium/issues">Issues</a><a href="https://github.com/codr1/termium/releases">Releases ↗</a></nav></footer>`;
function sidebar(current) {
  let lastGroup;
  let html = '<aside class="docs-sidebar" aria-label="Documentation"><a href="/docs/">Documentation overview</a>';
  for (const [slug, title, , group] of docs) {
    if (group !== lastGroup) {
      if (lastGroup) html += '</div>';
      html += `<p>${group.toUpperCase()}</p><div class="sidebar-links">`;
      lastGroup = group;
    }
    html += `<a href="/docs/${slug}/"${slug === current ? ' aria-current="page"' : ''}>${title}</a>`;
  }
  return html + '</div></aside>';
}
function documentPage(title, description, slug, content) {
  const route = slug ? `/docs/${slug}/` : '/docs/';
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="description" content="${escape(description)}"><meta name="theme-color" content="#0c1118"><link rel="canonical" href="${origin}${route}"><link rel="icon" href="/assets/mark.svg" type="image/svg+xml"><link rel="stylesheet" href="/assets/site.css"><script src="/assets/site.mjs" type="module"></script><title>${escape(title)} — Termium</title></head><body>${header(slug === 'installation' ? slug : 'docs')}<main class="docs-layout container" id="main">${sidebar(slug)}<article class="docs-content"><p class="eyebrow">TERMIUM / DOCUMENTATION</p>${content}${slug ? `<div class="doc-meta">See GitHub Releases for version history. <a href="${githubSource}/docs/${slug}.md">Edit on GitHub ↗</a></div>` : ''}</article></main>${footer}</body></html>`;
}
function rewriteLink(href, filename) {
  if (!href || /^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i.test(href)) return href;
  const url = new URL(href, `https://source.invalid/docs/${filename}.md`);
  if (url.pathname === '/docs/README.md') return '/docs/' + url.hash;
  const doc = docs.find(([slug]) => url.pathname === `/docs/${slug}.md`);
  if (doc) return `/docs/${doc[0]}/` + url.hash;
  return githubSource + url.pathname + url.hash;
}
await fs.rm(output, { recursive: true, force: true });
await fs.mkdir(output, { recursive: true });
await fs.cp(path.join(source, 'assets'), path.join(output, 'assets'), { recursive: true });
await fs.cp(path.join(source, 'welcome'), path.join(output, 'welcome'), { recursive: true });
await fs.copyFile(path.join(source, '_headers'), path.join(output, '_headers'));
// Serve the reviewed bootstrap verbatim; never maintain a separate web copy.
await fs.copyFile(path.join(root, 'scripts/install.sh'), path.join(output, 'install'));
const welcome = await fs.readFile(path.join(source, 'welcome/index.html'), 'utf8');
const logo = welcome.match(/<pre class="ascii-mark"[\s\S]*?<\/pre>/)?.[0];
if (!logo) throw Error('Welcome page is missing the shared ASCII mark');
const index = (await fs.readFile(path.join(source, 'index.html'), 'utf8'))
  .replace('{{header}}', header('')).replace('{{footer}}', footer).replace('{{logo}}', logo);
if (/\{\{\w+\}\}/.test(index)) throw Error('Unresolved website template');
await fs.writeFile(path.join(output, 'index.html'), index);
for (const [slug, title, description] of docs) {
  const markdown = await fs.readFile(path.join(root, 'docs', `${slug}.md`), 'utf8');
  const marked = new Marked({ walkTokens(token) {
    if (token.type === 'link') token.href = rewriteLink(token.href, slug);
  }});
  const ids = new Map();
  let html = await marked.parse(markdown);
  html = html.replace(/<h([1-6])>(.*?)<\/h\1>/g, (_, level, text) => {
    const base = text.replace(/<[^>]*>/g, '').replace(/&[^;]+;/g, '').toLowerCase().replace(/[^a-z0-9\s-]/g, '').trim().replace(/\s+/g, '-');
    const count = ids.get(base) ?? 0;
    ids.set(base, count + 1);
    return `<h${level} id="${base}${count ? '-' + count : ''}">${text}</h${level}>`;
  });
  html = html.replaceAll('<table>', '<div class="table-wrap" role="region" aria-label="Reference table" tabindex="0"><table>').replaceAll('</table>', '</table></div>');
  await fs.mkdir(path.join(output, 'docs', slug), { recursive: true });
  await fs.writeFile(path.join(output, 'docs', slug, 'index.html'), documentPage(title, description, slug, html));
}
const cards = docs.map(([slug, title, description]) => `<a class="doc-card" href="/docs/${slug}/"><h2>${title} ↗</h2><p>${description}</p></a>`).join('');
await fs.writeFile(path.join(output, 'docs/index.html'), documentPage('Documentation', 'Install, navigate, and build Termium. Guides for users and contributors.', '', `<h1>Find your way around.</h1><p>Start with Quickstart and your first page. Come back for the keys, terminal setup, and a look under the hood.</p><div class="doc-cards">${cards}</div>`));
await fs.writeFile(path.join(output, '404.html'), `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex"><title>Page not found — Termium</title><link rel="stylesheet" href="/assets/site.css"><link rel="icon" href="/assets/mark.svg"></head><body>${header('')}<main id="main" class="container not-found"><p class="eyebrow">404 / NOTHING AT THIS ADDRESS</p><h1>A wrong turn.<br>An easy way back<span class="accent">_</span></h1><a class="button primary" href="/">Back to Termium →</a></main>${footer}</body></html>`);
const routes = ['/', '/docs/', ...docs.map(([slug]) => `/docs/${slug}/`)];
await fs.writeFile(path.join(output, 'sitemap.xml'), `<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">${routes.map(route => `<url><loc>${origin}${route}</loc></url>`).join('')}</urlset>`);
await fs.writeFile(path.join(output, 'robots.txt'), `User-agent: *\nDisallow: /welcome\nSitemap: ${origin}/sitemap.xml\n`);
console.log(`Built ${routes.length} public pages, a separate welcome page, and a 404 page in dist/website`);
