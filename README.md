# Termium
### Use your Chrome browser from inside your terminal.

Terminum allows you to run Chromium inside any terminal that supports sixel graphics. The project consists of a TypeScript server running a headless Chromium instance, and a Go client providing a text-based UI for interaction.

## Features
- Run headless Chromium from the terminal.
- Control the browser through a text-based UI.
- Forward terminal interactions to the Chromium instance via a server-client architecture.

## Installation

### Prerequisites

- Node.js (v18 or higher)
- Go (v1.23 or higher)
- npm
- A terminal that supports sixel graphics (Currently using Windows Terminal Preview for development)
  - This is a list of terminals with their sixel support.  https://www.arewesixelyet.com/.  Windows Terminal Preview is not updated yet

### Setup
1. Clone the repository:
```
git clone https://github.com/yourusername/terminum.git
cd termium
```

2. Setup and Build   
```
./build.sh
```

### Project Structure
<pre>
termium/
├── proto/
│   └── bc.proto
├── client/          # Go client code
│   ├── main.go
│   └── ...
├── server/          # TypeScript server code
│   ├── src/
│   │   ├── server.ts
│   │   └── ...
│   ├── dist/
│   └── ...
├── README.md
├── build.sh 
└── package.json
</pre>

### Description:
The TypeScript server uses Puppeteer to run a headless Chromium instance and exposes various endpoints for interacting with the browser.
The Go client provides a text-based UI for users to control the browser from within the terminal.


### Usage - Starting the application in two separate terminals (temporarily)
Once we are done with testing - this will be a single command. 

#### Terminal 1 
```
npm run start:server
```

This will launch the headless Chromium instance and expose the necessary endpoints.

#### Terminal 2 
```
npm run start:client [options]
```

This will launch the Go client (text-based UI):
This will start the terminal UI that interacts with the TypeScript server.

### Client Options

- `-u, --url <url>`: Initial URL to navigate to (default: https://www.google.com)
- `-s, --server <address>`: Server address for TCP connection (default: uses Unix socket at /tmp/termium.sock)
- `--tcp`: Force TCP connection to localhost:50051
- `-p, --palette <type>`: Color palette for sixel rendering
  - `adaptive`: Good quality with accurate colors, but slower performance due to per-frame color quantization (default)
  - `websafe`: Web-safe 216 color palette - looks worse but significantly faster performance with cached palette
- `-t, --timings`: Show performance timing information and cache statistics
- `-h, --help`: Show help message

### Keyboard Controls

Once the application is running:

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
- `Home`: Move to beginning of URL
- `End`: Move to end of URL
- `Ctrl+U`: Clear entire URL

**Mouse Controls:**
- Click on elements to interact with them

**Note:** Regular typing in normal mode sends keystrokes to the webpage (for text input fields, etc.)

### Development 
To rebuild everything 
```
npm run buid
```

To clan everything and start fresh:
```
npm run clean:all
./build.sh
```


### Contribution
Feel free to open issues or submit pull requests if you find any bugs or have new features in mind.

License
This project is currently using the CC BY-ND License.   

Additional Notes
Ensure that your terminal supports sixel graphics for optimal display. You may need to configure your terminal settings.
The .env file is used to configure environment-specific settings for both the server and client.

TODO: 
- Add a real home page navigation option

### Technical Notes

#### Band-Level Dirty Detection for Sixel Optimization

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

#### JPEG-Based Change Detection
The system leverages JPEG compression artifacts as features for change detection. JPEG's 8x8 DCT blocks provide natural spatial clustering and noise suppression, filtering out imperceptible changes. This approach treats compression "artifacts" as beneficial preprocessing for determining visually significant changes.
