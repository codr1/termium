// Only bundled code runs in the extension origin. Hosted HTML is never injected
// here: after checking the official page, navigate to its normal web origin.
import './all_content_scripts.js';

const controller = new AbortController();
let touched = false;
const cancel = event => { if (event.isTrusted) { touched = true; controller.abort(); } };
for (const event of ['keydown', 'pointerdown', 'wheel']) window.addEventListener(event, cancel, { capture: true, passive: true });
const timer = setTimeout(() => controller.abort(), 1500);
try {
  const { termiumWebsite } = await chrome.storage.local.get('termiumWebsite');
  if (termiumWebsite && !touched) {
    const response = await fetch(termiumWebsite, { signal: controller.signal, credentials: 'omit' });
    // A parked domain or a generic hosting error is not the Termium home page.
    // Keep the offline page until the actual website is published.
    if (response.ok && new URL(response.url).origin === new URL(termiumWebsite).origin) {
      const html = await response.text();
      const doc = new DOMParser().parseFromString(html, 'text/html');
      if (!touched && !controller.signal.aborted && doc.querySelector('meta[name="termium-welcome"][content="1"]')) {
        location.replace(termiumWebsite);
      }
    }
  }
} catch { /* Offline, unavailable, or already interacting: keep this page. */ }
finally {
  clearTimeout(timer);
  for (const event of ['keydown', 'pointerdown', 'wheel']) window.removeEventListener(event, cancel, { capture: true });
}
