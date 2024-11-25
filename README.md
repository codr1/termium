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
- Go (v1.23 or higher)
- npm
- A terminal that supports sixel graphics (Currently using Windows Terminal Preview for development)

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
npm run start:client
```

This will launch the Go client (text-based UI):
This will start the terminal UI that interacts with the TypeScript server.
 
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
