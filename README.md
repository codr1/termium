# Termium
### Use your Chrome browser from inside your terminal.

Termium allows you to run Chromium inside any terminal that supports sixel or Kitty graphics. The project consists of a TypeScript server running a headless Chromium instance, and a Go client providing a text-based UI for interaction.

## Features
- Run headless Chromium from the terminal.
- Control the browser through a text-based UI.
- Supports sixel graphics (xterm, Windows Terminal Preview, etc.) and Kitty graphics protocol (Ghostty, Kitty).
- Auto-launches the server — just run `./termium`.

## Quick Start

```bash
curl -fsSL https://raw.githubusercontent.com/codr1/termium/main/scripts/install.sh | bash
termium
```

For Ghostty or Kitty terminals:
```bash
termium --renderer kitty
```

That's it. The client auto-launches the server. First run downloads Chromium (~300MB, one-time).

### Prerequisites

- Node.js (v18 or higher) — required for the server
- A terminal that supports sixel or Kitty graphics
  - Sixel: https://www.arewesixelyet.com/
  - Kitty protocol: Ghostty, Kitty

### Building from Source

If you prefer to build from source instead of using the installer:

```bash
git clone https://github.com/codr1/termium.git
cd termium
npm install
npm run build
cd client
./termium
```

Building from source additionally requires: Go (v1.23+), protoc, protoc-gen-go, protoc-gen-go-grpc.

## Usage

Just run `termium`. The client automatically finds and starts the server.

To run the server manually (optional):
```
# Terminal 1
npm run start:server

# Terminal 2
cd client
./termium
```

### Client Options

- `--renderer <type>`: Rendering protocol (default: `sixel`)
  - `sixel`: For terminals with sixel support
  - `kitty`: For Ghostty, Kitty, and other terminals with Kitty graphics protocol
  - `tcell`: Character-based fallback (no graphics required)
- `-p, --palette <type>`: Color palette for sixel rendering
  - `adaptive`: Best quality, slower (default)
  - `websafe`: Faster with cached palette
  - `plan9`: Plan9 color palette
- `-s, --tcp <address>`: Use TCP connection instead of Unix socket
- `--timings`: Show performance timing information
- `--splash <path>`: Custom splash screen image (or `NONE` to skip)
- `--debug`: Enable debug output
- `--logfile <path>`: Write logs to file
- `--cpuprofile <path>`: Write CPU profile
- `-h, --help`: Show help message

### Environment Variables

- `TERMIUM_SERVER`: Path to `server.js` to override auto-discovery. The client searches for the server in this order:
  1. `$TERMIUM_SERVER`
  2. `../server/dist/src/server.js` relative to client binary (dev layout)
  3. `~/.termium/server/server.js` (installed layout)

### Keyboard Controls

**Splash Screen:**
- `Enter`: Continue to browser

**Browser Controls:**
- `Ctrl+L`: Open URL navigation bar (shows current URL)
- `Escape`: Exit the application

**URL Navigation Mode (after pressing Ctrl+L):**
- Type to enter a new URL
- `Enter`: Navigate to the entered URL
- `Escape`: Cancel URL input and return to normal mode
- `Backspace`: Delete character before cursor
- `Delete`: Delete character at cursor
- `Left/Right Arrow`: Move cursor within URL
- `Home`/`End`: Move to beginning/end of URL
- `Ctrl+U`: Clear entire URL

**Mouse Controls:**
- Click on elements to interact with them

Regular typing in normal mode sends keystrokes to the webpage.

## Project Structure
<pre>
termium/
├── proto/
│   └── bc.proto           # gRPC service definitions
├── client/                # Go client code
│   ├── main.go
│   ├── config.go
│   ├── kitty_renderer.go  # Kitty graphics protocol renderer
│   ├── sixel_band_encoder.go
│   ├── sixel_bands.go
│   ├── server_launcher.go # Auto-launch server from client
│   ├── text_render.go     # tcell character-based fallback
│   ├── keyboard.go
│   ├── dialog.go
│   ├── dialog_stream.go
│   └── ...
├── server/                # TypeScript server code
│   ├── src/
│   │   └── server.ts
│   ├── dist/              # Compiled JS (generated)
│   └── generated/         # Proto TS code (generated)
├── README.md
├── CLAUDE.md
└── package.json
</pre>

## Architecture

The TypeScript server uses Puppeteer to run a headless Chromium instance and streams screenshots to the client via gRPC. The Go client renders these frames in the terminal using one of three renderers:

- **Sixel**: Palette-quantized sixel graphics with band-level dirty detection and caching
- **Kitty**: PNG passthrough — server sends PNG, client base64-encodes and writes Kitty escape sequences. No image decoding or re-encoding on the client. Targets 30 FPS.
- **tcell**: Character-based rendering using Unicode block elements (fallback)

Communication uses Unix domain socket (`/tmp/termium.sock`) by default, with optional TCP.

## Development

Rebuild everything:
```
npm run build
```

Build just the client:
```
cd client
go build -o termium
```

Clean and start fresh:
```
npm run clean:all
npm install
npm run build
```

## Technical Notes

### Kitty Graphics Renderer

The Kitty renderer uses PNG passthrough for maximum performance. The server captures screenshots as PNG, streams the raw bytes over gRPC, and the client sends them directly to the terminal via the Kitty graphics protocol (`f=100`). Client-side work per frame is just base64 encoding and chunked escape sequence writing — typically under 3ms. This is the recommended renderer for Ghostty and Kitty terminals.

### Band-Level Dirty Detection for Sixel Optimization

The implementation uses a band-level caching strategy optimized for sixel graphics constraints. Instead of traditional dirty rectangles, we track changes at the sixel band level (6-pixel high horizontal strips).

**Algorithm:**

1. **Band Structure**: Divide the screen into horizontal bands of 6 pixels (sixel's atomic unit)
2. **Change Detection**:
   - Hash each band's pixel data for fast comparison
   - Compare new frame bands with cached bands
   - Mark bands as clean (unchanged) or dirty (changed)
3. **Selective Encoding**:
   - Clean bands: Use cached sixel string (massive performance win)
   - Dirty bands: Re-encode only these bands
4. **Composition**: Concatenate all band strings (cached + new) for single terminal write

**Performance Characteristics:**
- Full screen updates (scrolling): No optimization, ~50ms
- Partial updates (typing): Only dirty bands encoded, ~5ms (10x faster)
- Minimal updates (cursor): Single band update, ~2ms (25x faster)
- Static content: No encoding needed, <1ms (50x faster)

**Design Rationale:**
- Sixel cannot update individual pixels - must redraw complete horizontal bands
- Traditional quadtree/dirty rectangles don't align with sixel's constraints
- Band-level caching provides optimal granularity for sixel format
- Trades ~2MB memory for 10-25x performance improvement on typical updates

**Limitations:**
- Requires websafe palette for stable caching (adaptive palette changes invalidate cache)
- Memory overhead increases with screen size (one cache entry per band)

### JPEG-Based Change Detection
The system leverages JPEG compression artifacts as features for change detection. JPEG's 8x8 DCT blocks provide natural spatial clustering and noise suppression, filtering out imperceptible changes. This approach treats compression "artifacts" as beneficial preprocessing for determining visually significant changes.

## License
This project is currently using the CC BY-ND License.
