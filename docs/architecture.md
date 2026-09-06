# Architecture

This page describes the current implementation. For planned changes, see [installation](plans/one-command-install.md) and [browser UI](plans/browser-ui.md).

## Browser and client

The TypeScript server controls a headless Chromium browser through Puppeteer. The Go client sends navigation, input, and viewport requests over gRPC and renders streamed screenshots in the terminal.

```mermaid
flowchart LR
    User[Keyboard and mouse] --> Client[Go terminal client]
    Client -->|Control requests| Server[TypeScript gRPC server]
    Server -->|Puppeteer| Browser[Chromium]
    Browser -->|Screenshots| Server
    Server -->|Frame stream| Client
    Client --> Terminal[Terminal display]
```

The protocol lives in `proto/bc.proto`; Go and TypeScript bindings are generated during the build. Dialog events use a bidirectional stream.

## Renderers

- **Kitty:** requests PNG screenshots and sends the encoded image bytes through the Kitty graphics protocol. The streaming path reads image dimensions to reject stale resize frames, then passes PNG bytes through without decoding pixels or re-encoding. It requests 24 frames per second; achieved frame rate must be measured.
- **Sixel:** decodes screenshots, quantizes colors, and encodes sixel output. Band-level caching reuses encoded regions when their contents are unchanged. Fixed palettes such as websafe support more stable caching.
- **tcell:** samples two colors per cell and draws a Unicode half block across the shared viewport. This is a screenshot renderer, not a DOM text browser.

Sixel bands are six pixels high. Caching at that granularity follows the image format, but effectiveness depends on page changes, palette selection, and viewport size. Earlier README timing figures were not a cross-platform benchmark and should not be used as release guarantees.

`OPTIMUS.md` contains historical performance investigations, including items that may already be implemented. Confirm its suggestions against the current code before treating them as active tasks.

## Process and transport lifecycle

The client checks whether a server accepts connections, discovers its entry point, and launches Node if necessary. It watches stdout for `TERMIUM_READY`. That sentinel means the gRPC listener is available; browser startup happens later when requested.

The server applies desktop viewport metrics directly to the active Chromium target. It serializes capture and resize, and reapplies the viewport after target replacement. This avoids Puppeteer's additional touch-emulation operations, which hung after modal dialogs in native macOS tests.

The default endpoint is `/tmp/termium.sock`, with optional TCP. The server maintains one global page and dialog stream. This does not provide isolated multi-client sessions.

Shutdown attempts to close the browser, server, connection, and terminal screen. Terminal probes have bounded deadlines. The event loop alone draws or finalizes the screen; workers post events and publish immutable frames into a latest-frame slot. A single client worker orders navigation, keys, mouse transitions, and resize requests. Browser input carries a document generation so queued input cannot act on a replacement page. Session isolation remains release work.

## Code map

| Location | Responsibility |
| --- | --- |
| `client/main.go` | Application lifecycle, terminal events, viewport, and rendering coordination |
| `client/config.go` | Flags and renderer selection |
| `client/server_launcher.go` | Server discovery and child process lifecycle |
| `client/keyboard.go`, `client/navigation_ui.go`, `client/text_editor.go` | Keyboard routing, navigation bar, menu, and Unicode editing |
| `client/mouse.go`, `client/input_dispatcher.go` | Mouse capture, keyboard pointer, and ordered input |
| `client/ui_layout.go`, `client/framebuffer.go` | Shared viewport geometry and immutable frame handoff |
| `client/kitty_renderer.go` | Kitty graphics encoding |
| `client/sixel_bands.go`, `client/sixel_band_encoder.go` | Sixel band processing and caching |
| `client/dialog.go`, `client/dialog_stream.go` | Browser and local dialogs |
| `server/src/server.ts`, `server/src/browser-controls.ts` | Puppeteer lifecycle, history/loading state, ordered input, and gRPC handlers |
| `proto/bc.proto` | Client/server protocol |

[Documentation home](README.md)
