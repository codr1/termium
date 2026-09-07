# Browser UI and Vimium integration

Implemented in the development build. This supersedes the original plan to emulate Vimium in a native page helper. See [source review](vimium-source-review.md) for the decision and [user guide](../vimium.md) for controls.

## Ownership

Upstream Vimium owns webpage modes, hints, find, scrolling, and browser tab commands. Its runtime source is vendored unchanged at the revision recorded in `third_party/vimium/TERMIUM.md`, with the MIT notice. Termium adds an offline welcome page and configures immediate scrolling and static hints through extension settings.

Termium owns terminal input, local help/dialogs, the tab strip, address bar, keyboard pointer, and screenshot presentation. Puppeteer loads the extension with an explicitly awaited `browser.installExtension`; managed Chromium launches headless over a pipe with extension support. No extension installation is required from the user.

## Tab identity and consistency

`BrowserSession` maintains a `BrowserControls` instance per Chromium page and a single ordered input/command queue. Chrome's tab targets expose `embedderData.tabActive` and `tabStripIndex` through `Target.getTargets`. This metadata is pulled; Puppeteer's `targetchanged` does not report tab activation. URL equality never establishes identity.

The adapter maps Puppeteer's internal `_tabId` to native tab targets. Puppeteer is pinned to 25.10.0, and runtime checks reject missing identity/metadata. Updating Puppeteer or Chromium requires real tab integration tests and bundle doctor checks. Remote browsers need compatible tab metadata and extension loading enabled; the packaged browser is the supported default.

State and screenshots carry the active tab ID and a session generation that changes with selection or document changes. Inputs carry both and stale inputs are rejected without replay. State reads verify they did not straddle a document change. Captures are discarded if selection changes while capturing. Switching releases held pointer buttons on the previous page. Only the selected page is captured; background pages may still run JavaScript and consume memory.

The tab row uses terminal row zero and the address bar uses row one. The page keeps its previous origin and dimensions. Overflow keeps the selected tab visible and offers an all-tabs picker. Dialog events carry their owning tab and the terminal queues concurrent dialogs.

## Input and rendering

Page input is dispatched through Chromium as trusted keyboard/mouse events. Readiness is checked in Vimium's isolated extension contexts before key delivery; cached focus does not classify a key as navigation or typing. Bracketed paste stays literal. F6 pointer mode and local application shortcuts are handled before page input.

Local overlays pause screenshot presentation and clear terminal graphics before drawing. Vimium's own overlays are rendered within Chromium and captured normally. No additional terminal UI framework controls the sixel/Kitty output stream.

## Remaining boundaries

Persistent profiles, host clipboard integration, native file choosers, crash recovery, and complete multi-window behavior need further work. Protected browser pages cannot run Vimium. Default installation targets remain Linux x86-64 and macOS Intel/Apple Silicon, with Windows later. Tests establish core integration, not universal upstream command compatibility.
