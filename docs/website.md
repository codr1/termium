# Termium website and welcome page

The static website lives in `site/`. Its HTML contains the ASCII mark and key legend; `site/assets/mark.svg` is the matching vector icon. There are no external fonts, analytics, animations, or JavaScript dependencies on the website.

Preview it locally:

```bash
python3 -m http.server 8787 --bind 127.0.0.1 --directory site
```

Open `http://127.0.0.1:8787`. The layout adapts to narrow windows and short terminal viewports. Build the application normally with `npm run build`; the build generates its offline welcome page from the same files.

## Cloudflare Pages

The root `wrangler.toml` describes the `termium` Pages project and `site/` output directory. Once the project and Cloudflare credentials are configured, deploy that directory and connect `termium.dev` as the project's custom domain. Website publication and DNS changes are separate from building the application; adding this configuration does not deploy the site.

Keep `<meta name="termium-welcome" content="1">` in the published HTML. The application checks that marker to avoid opening a registrar's parking page or a generic hosting error. The Pages `_headers` file sets revalidation and a restrictive policy for the static website.

## Offline behavior

The bundled welcome page contains a trusted extension bootstrap that provides Vimium support. It checks the official website with a 1.5-second timeout and navigates there only before the user interacts. It never renders fetched HTML or scripts with extension privileges. The deployed website runs as a normal HTTPS page with Vimium's regular content scripts.

Users can select the bundled page permanently, a blank page, or any HTTP/HTTPS home page. See [home-page settings](getting-started.md#choose-your-home-page). Tests and installation doctor use the bundled page without requesting the public website.
