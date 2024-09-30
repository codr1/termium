# Terminum
### Use your Chrome browser from inside your terminal.

Terminum allows you to run Chromium inside any terminal that supports sixel graphics. The project consists of a TypeScript server running a headless Chromium instance, and a Go client providing a text-based UI for interaction.

## Features
- Run headless Chromium from the terminal.
- Control the browser through a text-based UI.
- Forward terminal interactions to the Chromium instance via a server-client architecture.

## Installation

### Prerequisites

- Node.js (v18 or higher)
- Go (v1.16 or higher)
- npm
- A terminal that supports sixel graphics (Currently using Windows Terminal Preview for development)

### Setup
1. Clone the repository:
```
git clone https://github.com/yourusername/terminum.git
cd termium
```

2. Install TypeScript server dependencies:
```
npm install
```

3. Install Go dependencies:
go mod tidy

4. Build TypeScript project:
npm run build
This will compile the TypeScript files into the dist/ directory.

### Project Structure
<pre>
terminum/
│
├── dist/               # Compiled TypeScript files (server)
├── src/                # TypeScript source files
│   ├── bc.proto        # gRPC proto file
│   └── server.ts       # TypeScript server for Chromium interaction
│
├── main.go             # Go client file
├── go.mod              # Go module dependencies
├── go.sum              # Go module checksums
│
├── .env                # Environment variables (ignored in .gitignore)
├── README.md           # This file
</pre>

### Description:
The TypeScript server uses Puppeteer to run a headless Chromium instance and exposes various endpoints for interacting with the browser.
The Go client provides a text-based UI for users to control the browser from within the terminal.


### Usage
Start the TypeScript Server
After building the project, you can start the TypeScript server by running:

npm run start

This will launch the headless Chromium instance and expose the necessary endpoints.

### Run the Go Client
To run the Go client (text-based UI):

go run main.go

This will start the terminal UI that interacts with the TypeScript server.

### Contribution
Feel free to open issues or submit pull requests if you find any bugs or have new features in mind.

License
This project is currently using the CC BY-ND License.   

Additional Notes
Ensure that your terminal supports sixel graphics for optimal display. You may need to configure your terminal settings.
The .env file is used to configure environment-specific settings for both the server and client.
