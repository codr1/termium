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

**Browser Navigation:**
- `Arrow Keys`: Scroll the page (Up/Down/Left/Right)
- `Page Up/Page Down`: Scroll by page
- `Home/End`: Go to top/bottom of page
- `Backspace`: Go back in browser history
- `Shift+Backspace`: Go forward in browser history
- `Tab`: Move focus to next element
- `Shift+Tab`: Move focus to previous element
- `Enter`: Click on focused element or submit form
- `Space`: Click on focused element or scroll down
- `u`: Focus URL bar for entering a new address
- `r`: Reload the current page
- `Escape`: Quit the application

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

#### Dirty Rectangle Detection
The current implementation uses JPEG-compressed images for change detection. This approach leverages JPEG's 8x8 DCT block structure as a natural quantization mechanism - small pixel-level changes that don't survive JPEG compression at 60% quality are likely not perceptually significant. The blockiness acts as a built-in spatial clustering and noise suppression filter, making change detection more robust against minor variations like anti-aliasing and gradient shifts.

TODO: Explore alternative approaches:
- Uncompressed frame differencing for pixel-perfect detection
- Browser-side DOM mutation observers for event-driven updates  
- Perceptual hashing for semantic change detection
- Motion vectors from video encoding techniques
