# Website and browser welcome page

Termium has two separate web experiences:

- The public website at `https://termium.dev/` introduces the product and hosts installation, user, and contributor guides.
- The browser welcome page at `https://termium.dev/welcome/` contains the ASCII mark, key legend, and home-page settings. It is not linked from the public website or included in its sitemap. It carries `noindex, nofollow` directives.

## Sources and build

`site/index.html` is the product-page template. `scripts/build-website.mjs` adds shared navigation and renders the selected Markdown guides in `docs/` into full HTML pages. Links between published guides become website links; contributor documents outside the published selection link to GitHub. Edit the Markdown to update both repository and website documentation.

`site/welcome/index.html` is the separate browser page. `scripts/build-extensions.mjs` copies its HTML, CSS, and icon into the bundled extension. It does not copy the marketing page or website JavaScript. Both experiences share the ASCII mark and `site/assets/mark.svg`, but have separate layouts and stylesheets.

The public site has no external fonts, analytics, or animations. A small optional script adds copy buttons to shell examples; reading, navigation, and FAQ disclosure work without JavaScript.

```bash
npm run build:website
npm run preview:website
```

Preview at `http://localhost:8787`. The deployable output is `dist/website/`. The root Wrangler configuration points to that directory. Run `npm run test:website` after the server build and browser installation; `npm test` includes all prerequisites and website checks.

## Cloudflare Pages

Deploy to the `termium` project in the intended Cloudflare account:

```bash
npm run deploy:website
```

Supply `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` through the environment. Credentials belong outside the repository. The first deployment requires a Pages project; custom-domain setup also requires the domain to be present in the Cloudflare account and its DNS delegation configured.

The `_headers` file applies the static site's content security policy and revalidation behavior. The welcome marker belongs only in `site/welcome/index.html`; it prevents an older client from treating the product site or a registrar parking page as its compact home page.

## Browser behavior

Startup, Home, and new tabs use `https://termium.dev/welcome/` by default. The bundled page checks the hosted page with a 1.5-second timeout and navigates only before the user interacts. Remote HTML never runs in the extension origin. Unavailable, parked, or slow responses leave the local page usable.

Previously saved default URLs at the root of `termium.dev` are treated as the welcome destination for compatibility. Other custom HTTP/HTTPS home pages open directly. `about:termium` chooses the offline page without a network request. See [home-page settings](getting-started.md#choose-your-home-page).
