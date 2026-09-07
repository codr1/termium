# Vimium source review

Historical source review, updated 2026-09-07. Implementation now bundles the extension; see [current architecture](browser-ui.md). Reviewed against upstream commit [`5aa29614bf1dce05e0d316f8c38722e17f9b38c3`](https://github.com/philc/vimium/tree/5aa29614bf1dce05e0d316f8c38722e17f9b38c3), whose manifest declares version 2.4.2. This is a source inspection, not an upstream test run or a claim that Termium implements these features.

## Decision

Updated recommendation after a headless-browser experiment: bundle a pinned upstream Vimium extension by default and integrate it with Termium's native interface. The earlier recommendation to adapt selected algorithms imposed maintenance work before establishing that the extension itself could run. It can run in our packaged browser. Keep Termium's terminal renderer, toolbar, mouse handling, and ordered browser input pipeline; add extension readiness and active-tab integration.

[Puppeteer supports loading unpacked extensions](https://pptr.dev/guides/chrome-extensions) through `enableExtensions`. Our bundle uses full Chrome for Testing with unified headless mode, rather than the separate old headless-shell implementation. See [Chrome's headless documentation](https://developer.chrome.com/docs/automation-and-testing/headless).

The component analysis below remains useful for integration and diagnosing compatibility problems. Extracting or rewriting those components should require a demonstrated blocker that configuration or a small upstream-compatible change cannot solve.

### Local feasibility experiment

A disposable browser using the installed Node 24.20.0, Chrome for Testing 152.0.7977.75, Puppeteer 25.10.0, and the upstream revision above loaded Vimium with `headless: true`, `pipe: true`, and `enableExtensions: [extensionPath]`. No sandbox-disabling flags were used. Only the disposable browser's Vimium smooth-scroll setting was changed.

Observed through actual Puppeteer keyboard input and a local HTTP fixture:

- `f` produced three visible labels, also present in a captured screenshot.
- Selecting a button hint changed the fixture's document title through its click handler.
- Typing `fjgg` in a focused input preserved the literal text and did not open hints.
- `j` scrolled and `gg` returned to the top.
- `?` created the extension help iframe.
- On a fresh page with a cross-origin iframe, main-page and child-frame labels were unique; selecting the child button's hint triggered its click handler.
- `t` created another browser page.

The probe needed to wait for asynchronous keymap initialization before sending the first command. An immediate help-close-to-hints sequence did not pass reliably in the exploratory probe; the isolated frame check passed after navigating to a fresh fixture. Preserve this transition as a regression case and diagnose it before claiming full integration. These are Linux browser-level smoke checks, not the complete Termium PTY path, a terminal graphics benchmark, or macOS acceptance.

Remaining integration work includes selecting the page Vimium makes active, following newly created/closed tabs, preserving per-tab viewport and dialog behavior, handling focus/readiness transitions, configuring static UI and immediate scrolling, bundling the extension and notices, and testing reserved Termium shortcuts and F6 mouse keys interaction.

### Tabs and extension API follow-up (2026-09-07)

Termium uses Puppeteer 25.10.0. Its public API supports enabling extensions at launch, explicitly awaiting `browser.installExtension(path)`, obtaining the extension ID, and accessing extension service workers and content-script realms. Prefer explicit installation followed by application readiness checks. Inspection of our installed `BrowserLauncher.js` found the array-based launch path passes a nested array to `Promise.all`, which does not await the contained installation promises. This is consistent with the first probe seeing an empty extension list immediately after launch. Explicit installation avoids depending on that launch-path behavior; extension keymap readiness remains a separate check.

A second disposable Linux probe verified that explicit awaited installation registered Vimium before continuing. It opened two tabs with the same URL, pressed Vimium's `J`, and observed Chromium's active tab change to the first tab while Puppeteer emitted zero `targetchanged` events. Bringing the second page to the front through Puppeteer then changed Chromium's active tab back. The pages reported visible/hidden states corresponding to the selected tab in this single-window experiment. These observations do not establish an activation adapter for every lifecycle or window configuration.

Puppeteer's [browser events](https://pptr.dev/api/puppeteer.browserevent) report target creation, destruction, and URL changes. `targetchanged` is not a tab-activation notification. Chromium's extension [`tabs` API](https://developer.chrome.com/docs/extensions/reference/api/tabs) provides tab IDs, tab order, active state, and activation events. The integration needs to preserve that identity across the browser/terminal boundary; URLs and indices are not sufficient identifiers. The implementation now pulls Chrome tab-target `embedderData.tabActive` and `tabStripIndex`, mapping the stable tab target to Puppeteer’s `_tabId`. This avoids a companion extension. Puppeteer is pinned and the mapping is covered by real browser tests. Define and test its mapping to Puppeteer pages without requiring changes to upstream Vimium's command implementation or exposing a generic command API to web content.

Recommended first tab UI: one compact tab row above the address/navigation row, with titles, a clear active marker, close targets, and a new-tab button. Keep the active tab visible when the list overflows; provide an overflow picker for narrow terminals. Mouse and F6 mouse keys coexist with Vimium's tab commands. Reserve the row consistently so opening the second tab does not unexpectedly move page coordinates.

Required state and lifecycle work:

- Store each tab's page, stable identity, title, URL, navigation/loading state, and dialog ownership. Preserve Chromium's tab order.
- Use Chromium's active selection as the source of truth for both Vimium commands and toolbar actions. New background tabs must not steal the terminal view.
- Switch input, viewport, screenshots, address, and active-tab UI as one selection transition. Release held input on the old tab and reject stale frames/events using tab identity plus a selection/document generation.
- Capture only the selected tab. Background tabs can continue normal browser work, but should not consume Termium screenshot/encoding bandwidth. Do not promise that background JavaScript or memory use disappears.
- Handle website-created tabs, close/reopen, unsaved-change dialogs, active-tab crashes, and replacement of the last tab with a welcome page. Worker restart, popup windows, and browser-owned pages need explicit acceptance decisions.
- Test both directions: Vimium changes tabs and Termium follows; clicking the tab strip changes the tab Chromium/Vimium treats as active. Include duplicate URLs, pending navigation, rapid switches, capture in flight, and held mouse buttons.

[Playwright also supports Chromium extensions](https://playwright.dev/docs/chrome-extensions), using its persistent-context launch path and bundled Chromium for headless operation. That provides no demonstrated reason to migrate Termium's existing Puppeteer integration. Keep the current library and add the tab/session layer.

Vimium includes an [MIT license](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/MIT-LICENSE.txt). Any copied or adapted source must retain its attribution and license; record the upstream revision and local changes beside vendored code, and include the notice in release bundles. The subsequent implementation vendors the pinned runtime and its MIT notice under `third_party/vimium`.

## How `f` works

The relevant implementation is [`link_hints.js`](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/content_scripts/link_hints.js).

1. The receiving frame temporarily captures keyboard input while hints are being collected. This prevents subsequent keys from accidentally executing normal-mode commands. A fallback timer releases that temporary mode if coordination stalls.
2. Each frame gathers its own actionable elements and retains the actual DOM references locally. Descriptors sent to the coordinator contain frame identity, local index, and optional link text.
3. The background coordinator collects frame responses with deadlines. Every participating frame receives the same combined descriptor set and generates the same labels. Each frame renders only its own labels. Selection state is synchronized across frames.
4. Alphabet hints use short labels of mixed lengths with no label being another label's prefix. Typing narrows the existing set instead of rescanning the page or renumbering remaining targets. Backspace edits the prefix; Escape cancels. Space rotates overlapping labels.
5. Activation distinguishes editable fields from other controls. It selects/focuses editable elements and uses a sequence of pointer and mouse events for clicks. Separate modes support opening links with modifiers or performing other link actions.

The [background coordinator](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/background_scripts/main.js) avoids returning a frame's own descriptors to it. Termium should similarly keep DOM objects in their owning frame and exchange only the metadata needed to coordinate labels.

## Target detection is substantial compatibility work

`LocalHints` handles semantic links and controls, editable elements, ARIA roles, inline click handlers, some framework attributes, and weaker class-name heuristics. It suppresses suspected wrapper duplicates and excludes disabled targets. It traverses accessible shadow roots.

Visibility includes viewport cropping, client rectangles, selected zero-size wrappers with visible children, and hit testing at the center and corners. A nonzero bounding rectangle alone does not establish that a target can be clicked: an overlay may cover it. Image maps and labels associated with controls need special treatment. See [`dom_utils.js`](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/lib/dom_utils.js).

This is still heuristic. Upstream explicitly leaves generic detection of `addEventListener('click', ...)` targets disabled. Closed shadow roots, custom widgets, native dialogs, and arbitrary website event logic require their own compatibility work. Termium must revalidate a selected target after DOM changes; displaying a label does not authorize a later click at stale coordinates.

## Input ownership

Vimium installs content scripts at document start in every matching frame, including about:blank children, as configured by its [manifest](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/manifest.json). Its [frontend](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/content_scripts/vimium_frontend.js) registers capture listeners on `window`, ahead of document listeners. The event wrapper checks trusted events.

[`HandlerStack`](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/lib/handler_stack.js) distinguishes continuing through modes, passing to the page, suppressing propagation, and suppressing the event. [`Mode`](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/content_scripts/mode.js) owns handler lifetime and cancellation. Suppressing a command's keydown also accounts for its trailing events.

[`InsertMode`](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/content_scripts/mode_insert.js) checks the active element when handling input and follows active elements through open shadow roots. Explicit insert mode and editable focus allow typing. This supports Termium's existing design requirement: never intercept `f` in Go based on a previously polled focus flag.

Any replacement helper would have to reproduce the isolation and lifecycle guarantees that extension content scripts already provide. A helper installed only after page load leaves an ordering gap. A generic page-accessible command callback would expose application actions to website scripts. Commands need an isolated execution context, validated context/document identity, and association with dispatched input. Keeping upstream Vimium avoids rebuilding much of this infrastructure. Cross-origin frames and restored back/forward-cache documents still need integration tests.

The superseded emulation plan proposed a difference: Vimium's key handler can pass unmapped keys to the page and recover overlapping command prefixes. Termium's proposed normal mode ignores unbound printable keys and cancels mismatched sequences. That is a product choice to confirm and document, not behavior inherited from upstream.

## Rendering and latency

Vimium renders hint markers separately from target elements, using the browser's popover top layer where available. This lets labels appear above ordinary stacking contexts without changing the controls themselves. Its other UI components use extension iframes inside shadow wrappers; see [`ui_component.js`](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/content_scripts/ui_component.js).

For Termium, render page-relative hints in Chromium so existing screenshots carry them through both Kitty and Sixel. Keep application help and menus in the existing terminal renderer. A separate terminal-coordinate hint renderer would duplicate frame transforms, clipping, and resize handling.

Recommended performance constraints, to be measured in Termium:

- Scan and measure targets on hint entry; batch DOM reads before inserting labels.
- Keep labels stable and filter their visibility while typing. Avoid continuous full-page mutation scans.
- Use static, opaque, high-contrast labels without fades or animated highlights.
- Default keyboard scrolling to immediate steps. Vimium's [`scroller.js`](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/content_scripts/scroller.js) includes animation and tracks the activated scroll container; preserve container selection without copying the animation defaults.
- Measure key dispatch, target collection, overlay creation, capture, image encoding, and terminal write separately. Report latency and bytes written for small and dense pages on each graphics protocol.

These changes should reduce avoidable frames; this review contains no benchmark proving a speedup.

## Tests to bring into our implementation

Upstream has [DOM tests](https://github.com/philc/vimium/blob/5aa29614bf1dce05e0d316f8c38722e17f9b38c3/tests/dom_tests/dom_tests.js), unit tests, and manual harnesses for visibility, forms, event capture, and frames. Some input-focus cases in the DOM suite are commented out, so copying the implementation and running its tests alone would not establish Termium compatibility.

Our acceptance tests should exercise actual Chromium input and independently observe effects:

- Visible links, scripted buttons, and editable controls get usable hints; disabled, hidden, covered, and offscreen targets do not.
- Labels remain unique and prefix-free across alphabet-size boundaries and frames; prefix filtering, Backspace, cancellation, and rapid typing work.
- Click or Tab into a field, then type `f`, `j`, and `gg`; verify literal field contents. Repeat with script autofocus, contenteditable descendants, shadow roots, and same/cross-origin frames.
- Selecting a field's hint must not insert the final hint character into that field. Keyup must not trigger a second site action after mode exit.
- Page-created synthetic keyboard events cannot issue browser commands. Paste remains literal, and composition must not invoke shortcuts.
- Navigation, detached frames, resize, scrolling, removed targets, and failed helpers cancel safely. No stale target activation or stuck input mode.
- Pointer clicks and F6 mouse keys continue working; they cancel or reconcile hints without duplicate clicks or held buttons.
- A real client in a PTY opens hints, selects a target, browses, and quits; terminal restoration is checked separately from page Escape handling.

## Implementation status

The development build now loads bundled Vimium and tracks real Chromium tabs. An older installed application must be updated before `f` activates hints. `termium --doctor` checks the bundled extension and tab commands. See the current user guide and architecture for implemented behavior and remaining limits.

[Browser UI plan](browser-ui.md)
