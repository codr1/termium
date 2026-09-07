# Bundled Vimium

Upstream: https://github.com/philc/vimium
Revision: 5aa29614bf1dce05e0d316f8c38722e17f9b38c3.
License: MIT-LICENSE.txt.

Runtime source is unchanged. Termium adds pages/termium.js for its welcome-page bootstrap. The build generates pages/termium.html, termium.css, and termium-mark.svg from site/welcome/index.html and site/assets/. The public product website is built separately and is not bundled. The bootstrap can navigate to the published website as an ordinary web origin; it never executes fetched HTML in the extension origin. Settings are configured through the extension background context at startup. Update the pinned source and run the real extension/tab integration tests together with browser/runtime updates.
