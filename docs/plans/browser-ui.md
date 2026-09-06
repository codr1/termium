# Browser UI and Vimium-style navigation plan

Status: next phase after the one-command installation work. This document proposes the UI; no UI or Vimium implementation is included in the documentation change.

## Goal

Implement Vimium-style keyboard navigation natively in Termium, retaining familiar shortcuts and covering as much of their behavior as practical. Termium owns the controls, modes, and settings. Installing the Vimium extension is not part of this design.

The [user-facing preview](../vimium.md) describes the intended experience. The first UI release has a fixed command scope and input contract below. Visual styling and exact control dimensions remain for the UI review.

## Implementation approach

Implement a native command layer shared by keyboard shortcuts, toolbar buttons, and menu actions. A Back button and its keyboard binding invoke the same command and observe the same browser state.

- **Go client:** own focus on Termium's address field, menu, and dialogs; handle the reserved application bindings. Serialize page keys, mouse actions, and explicit toolbar commands without interpreting page navigation sequences from cached browser focus.
- **Browser server:** own a single input queue per session, tab selection, and navigation identity. Deliver page input through Chromium's input pipeline and execute browser commands emitted by the authorized helper. Send state back for display, not for client-side command-versus-text decisions.
- **Page helpers:** own normal/insert/hint/find modes and recognize page key sequences at event dispatch using the current event target and editable focus. Inspect DOM elements for hints and scrolling. Render hint labels in the page so the screenshot pipeline displays them.

[Puppeteer's page evaluation API](https://pptr.dev/api/puppeteer.page.evaluate) can support DOM operations, but a separate earlier focus query must not authorize a later key as a command. Install the input helper in an isolated browser execution context for each supported frame and decide synchronously during that key event. Traverse editable ancestors and accessible shadow roots. Revalidate hinted elements before activation and cancel stale hints on navigation. Match normal browser click behavior for scripted links and controls.

This approach gives Termium direct control over its UI and eliminates extension installation and lifecycle management. The cost is ownership of behavior that Vimium already implements: hints, search, focus handling, selection, and edge cases across websites. This is substantial compatibility work, not just a keybinding table.

## Compatibility coverage

Use [Vimium's command reference](https://vimium.github.io/commands/) as the behavioral reference. Maintain a tested supported/partial/unavailable matrix for page navigation, hints, search, modes, tabs, clipboard, marks, settings, and exclusions. Include repetition counts, frames, dynamic pages, and focus changes. Describe the product as Vimium-style navigation; do not promise extension compatibility or compatibility with Vimium configuration files unless implemented and tested.

## First UI release: required command matrix

Every row below must pass on each advertised graphics terminal/OS pair before the first UI release. An unavailable required command is a blocker, not a documented exception. Expectations are Termium's chosen behavior; this is not a claim of complete Vimium compatibility.

| Binding / action | Required behavior and observable check |
| --- | --- |
| `h`, `j`, `k`, `l` | Scroll the active scroll container in the corresponding direction; a nested-scroll fixture moves the container rather than the outer page |
| `d`, `u` | Move half of the active container's visible height down/up, clamped at its boundary |
| `gg`, `G` | Reach the active container's top/bottom; the end position is verified |
| Counts | A positive decimal prefix repeats scrolling, `H`/`L`, and `J`/`K`; `3j` performs three scroll steps; other counted commands cancel with a status message |
| `f`, `F` | Label visible actionable elements in the main page and ordinary same/cross-origin iframes; selecting a label activates it, with `F` opening navigable links in a background tab |
| `H`, `L` and Back/Forward buttons | Traverse actual page history; no-op at a boundary; buttons reflect availability and URL changes |
| `r` and Reload/Stop button | Reload the active page; Stop cancels a pending navigation and returns controls to a usable state |
| `o`, `O`, Ctrl+L | `o` opens an empty address field for the current tab; `O` targets a new foreground tab; Ctrl+L selects the current URL for editing; Enter navigates and Escape cancels |
| `/`, `n`, `N` | `/` opens query entry; Enter confirms literal text search in the active frame and returns to normal mode; visibly mark a match; `n`/`N` cycle with wrapping; report no matches without navigating |
| `i`, Escape | Enter pass-through mode; Escape cancels the current page mode, clears partial commands, and leaves editable focus when returning to normal |
| `t`, `x`, `X` | Open a local welcome tab, close the active tab, and restore the most recently closed tab's URL; closing the last tab opens a welcome tab; restoration does not promise form/session recovery |
| `J`, `K` | Select previous/next tab, wrapping at the ends; screenshot, URL, and focus switch together |
| `?`, F10 | Open shortcut help or the application menu; both work by keyboard, display focus, and close with Escape |
| Ctrl+Q and Menu → Quit | Request graceful application shutdown; honor website before-unload confirmation; cancellation leaves the session running |
| Forms and paste | Click or Tab into input, textarea, or contenteditable and type text, including `j`, `gg`, and `f`; text arrives literally; bracketed paste is one text operation and never executes shortcuts |

For first-release `f`, actionable targets include links, buttons, and editable fields. `F` labels only links with a navigable URL; scripted controls remain available through `f`. Hint labels must fit the visible region and remain distinct after resize. Search within multiple frames, closed shadow roots, and specialized editors is additional compatibility work; main-frame and ordinary focused-iframe text search are required.

The first address field accepts URLs, applies `https://` to bare hostnames, and preserves explicit HTTP URLs. It does not yet search bookmarks/history or a search engine; no search-provider choice is needed for this release. Help lists only implemented bindings.

## Delivery order and deferred scope

1. Establish normal/insert modes, ordered input, Escape behavior, command dispatch, and editable-field focus detection.
2. Add scrolling, history, reload, address entry, and matching toolbar/menu actions.
3. Add link hints, in-page search, and tabs, including links opened into new tabs by websites.
4. After the required matrix passes, expand clipboard actions, marks, visual selection, bookmarks/history search, custom mappings, and site exclusions. Also defer browser-window management and Vimium configuration imports. Keep these compatibility items visible in the roadmap.

Development previews can demonstrate intermediate increments, but the first UI release requires all rows above. The broader compatibility goal remains in scope after that release.

## Input ownership

The DOM helper is authoritative for page commands versus text. The Go client must never swallow `j`, `f`, or another page binding based on an asynchronously reported focus flag. Focus notifications are informational. A synchronous capture-phase handler in the receiving frame checks the current composed event path, editable ancestors, composition state, and explicit mode before deciding whether to consume the event.

1. Reserve Ctrl+Q (Quit), Ctrl+L (address field), and F10 (menu) in the client. They do not reach page scripts. Disable terminal XON/XOFF while the TUI is active so Ctrl+Q can arrive; restore terminal settings on exit. Terminals that intercept these bindings need a tested alternative before certification.
2. Local dialogs, menu, and address editing consume their own input. Escape dismisses the focused local UI and restores page focus. Browser modal dialogs take precedence over ordinary page input.
3. Page input enters one ordered queue with session ID, monotonic sequence number, tab ID, and navigation generation. Click, Tab, and the next printable key retain that order through browser dispatch.
4. For each key event, the helper decides from live DOM focus. Editable fields, composition, and explicit insert mode allow text/editing events through. Normal mode consumes recognized bindings and prefixes exactly once. Unbound printable keys are ignored in normal mode; unbound non-printable editing keys pass through. `i` provides explicit pass-through for site-specific keyboard controls.
5. A partial normal-mode sequence has no idle timeout; Escape or a nonmatching key clears it. A nonmatching key is consumed rather than replayed as a second command. Counts are limited to 1–999; overflow cancels the sequence with status feedback. Focus/tab/navigation changes clear pending sequences.
6. Escape in hint/find/insert mode clears the mode and any relevant overlay; leaving insert mode blurs editable focus. Escape in normal mode is a no-op. During an active composition, composition cancellation takes precedence and must not trigger navigation.
7. Acknowledge input after it has been dispatched and classified, with the sequence number and resulting mode. A toolbar or helper command may initiate asynchronous navigation; acknowledgement does not mean the page finished loading. Reject stale tab/navigation input without replaying it into the new document; show a concise canceled-input status. Never retry an unacknowledged input automatically after reconnecting.

Semantic commands from page helpers must be restricted to authorized isolated contexts and tied to the active input sequence. Web content must not gain a generic callable API for closing tabs, reading the clipboard, or quitting the application. Reject untrusted synthetic DOM events even when a real input sequence is active; a sequence number alone is not proof of user input.

If a helper is absent or fails, suspend page shortcuts and show a client-owned recovery dialog with Retry, Pass keys to page, and Quit. Tab/Enter and mouse select those actions without requiring a page helper. Pass keys to page sets a visible, explicit fallback in the client: forward page input without command parsing and use Escape to reopen recovery. Do not infer this mode from cached DOM focus or silently re-enable shortcuts when a helper reconnects. Retry returns to normal navigation only after the replacement helper acknowledges readiness. The native toolbar and reserved application bindings remain available throughout.

Regression fixtures must delay/drop focus notifications while dispatching click → `j`, Tab → `f`, and script autofocus → `gg`. The exact text must reach the field, with no navigation command executed. Also test focus changes inside frames, navigation between queued inputs, duplicate sequence numbers, reconnects, composition, and paste.

Current conflicts include arrow keys moving a local cursor, most Ctrl combinations being discarded, Escape opening exit confirmation, and dialog handling bypassing the purported global triple-Escape shortcut. Individual keystrokes are currently sent by independent goroutines; ordering must be addressed before relying on multi-key commands.

## Navigation controls

Proposed first layout:

```text
[Back] [Forward] [Reload/Stop]  [ Address                         ] [Menu]
|                                                                    |
|                         Browser viewport                           |
|                                                                    |
```

The address bar should show the committed URL, support select-all editing, retain useful input on navigation failure, and restore page focus when dismissed. Browser-originated navigation, redirects, history actions, and tab changes must update it.

Back and Forward must reflect actual history availability. Reload changes to Stop while navigation is in progress. All controls need keyboard access and visible focus. On narrow terminals, move secondary actions into the menu before sacrificing the address field entirely.

The first menu contains New tab, Close tab, Reopen tab, shortcut help, and Quit. Do not show keybinding settings until configuration is implemented. It should present browser actions in user language. Debugging and runtime implementation details belong in diagnostics documentation.

Reserve space for the controls outside the page viewport. Adjust screenshots, resizing, mouse coordinates, and hit testing together so clicks continue to reach the correct page elements.

## Browser state and protocol

The server currently keeps one global page, and `OpenTab` reuses it. Supporting tabs created by commands or websites requires explicit tab IDs, active-tab state, lifecycle events, and screenshot streams that follow the selected tab.

Extend the protocol for navigation state, back/forward/reload/stop, tab selection and closure, hint activation, search, focus notifications, and ordered keyboard events. Toolbar actions and keyboard commands must use the same browser state. Scope asynchronous replies to their tab and navigation so stale replies cannot change the current mode.

Keep application data in an application-owned profile. Each running Termium session owns an exclusive profile lock and its own browser endpoint; a second session must not attach to the first implicitly. Custom keybinding persistence follows the later configuration work. The installation plan defines upgrade/profile recovery requirements.

## Areas needing a compatibility decision

| Area | Investigation required |
| --- | --- |
| Restricted browser pages | Determine where DOM helpers cannot operate and retain access to Termium navigation controls |
| New tabs and browser windows | Provide a usable page for new tabs and decide how new windows appear in a terminal session |
| Clipboard | Bridge browser and local clipboard capabilities; document terminal permission and SSH limitations |
| Browser-native UI | Handle file choosers, downloads, and permission prompts that are not part of the captured webpage |
| Keyboard protocols | Test modifier fidelity and intercepted shortcuts in target terminals and multiplexers |
| DOM helper lifecycle | Reinitialize after navigation, track frames, clean up overlays, and reject stale element references |

## Acceptance criteria

- [ ] Native Vimium-style navigation works immediately after the same one-command setup, without installing an extension.
- [ ] Record the Vimium behavior reference and tested browser version; maintain tests for each supported command.
- [ ] Every row of the first UI release command matrix passes; no required row is marked unavailable or waived as an exception.
- [ ] Link hints remain legible at supported viewport sizes, including links inside frames.
- [ ] Multi-key commands preserve order; live browser focus determines text handling even with stale or missing client focus notifications; Unicode paste never executes commands.
- [ ] Escape leaves modes without quitting; Quit remains reachable with keyboard and mouse.
- [ ] Address and history controls track redirects, keyboard navigation, and tab changes.
- [ ] Tabs created by shortcuts and websites can be selected, displayed, and closed without losing browser state.
- [ ] Deferred clipboard/native-UI features are absent from the shipped command list; native modal states cannot trap input or prevent Quit.
- [ ] Resizing and toolbar changes do not break click coordinates or cover page content.
- [ ] Test on Linux and macOS across the supported terminal matrix before advertising compatibility.

[Documentation home](../README.md)
