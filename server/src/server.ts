import * as grpc from '@grpc/grpc-js';
import * as puppeteer from 'puppeteer';
import { Command } from 'commander';
import * as fs from 'fs';
import * as path from 'path';
import debugFactory from 'debug';

// Update import paths
import { ServerUnaryCall, sendUnaryData, ServerWritableStream } from '@grpc/grpc-js';
import { BrowserControlService, BrowserControlServer } from '../generated/bc';
import { Empty, Message, ViewportSize, Coordinate, Text, Url, Screenshot, ScreenshotRequest } from '../generated/bc';

const program = new Command();
const logDebug = debugFactory('server:debug');

// Puppeteer browser and page instances
let browser: puppeteer.Browser | null = null;
let page: puppeteer.Page | null = null;

// CLI setup with Commander
program
    .option('-b, --browser <ip:port>', 'Connect to an existing browser instance (ip:port)', '')
    .option('-d, --debug [filename]', 'Enable debug mode (log to stdout or optional file)', '')
    .option('--daemon', 'Run server as a daemon')
    .option('--tcp <ip:port>', 'Use TCP socket instead of Unix domain socket', '')
    .option('-h, --help', 'Display help information')
    .description('gRPC server for browser control using Puppeteer');

program.parse(process.argv);
const options = program.opts();

// Setup debugging
if (options.debug !== undefined) {
    debugFactory.enable('server:debug');
    if (options.debug) {
        // If a filename is provided, log to file
        const logStream = fs.createWriteStream(path.resolve(options.debug), { flags: 'a' });
        logDebug.log = (...args: any[]) => logStream.write(args.join(' ') + '\n');
    } else {
        // Log to stdout by default
        logDebug.log = console.log.bind(console);
    }
}

// Daemonize the process if --daemon is passed (only works on Linux/Mac)
if (options.daemon) {
    const daemonize = require('daemonize2').setup({
        main: path.join(__dirname, 'server.js'),
        name: 'grpc-browser-control',
        pidfile: 'grpc-browser-control.pid'
    });

    daemonize.start();
    process.exit(0);
}

// Function to launch or connect to the browser
async function launchOrConnectToBrowser() {
    if (options.browser) {
        // Connect to an existing browser instance using DevTools protocol
        logDebug('Connecting to existing browser instance at', options.browser);
        browser = await puppeteer.connect({ browserWSEndpoint: `ws://${options.browser}` });
    } else {
        // Launch a new headless browser if no browser address is provided
        logDebug('Launching a new headless browser');
        browser = await puppeteer.launch({ headless: true });
    }
}

const browserControlHandlers: BrowserControlServer = {
    openTab: async (_call: ServerUnaryCall<Empty, Message>, callback: sendUnaryData<Message>) => {
        try {
            if (!browser) {
                await launchOrConnectToBrowser();
            }
            if (browser) {
              page = await browser.newPage();
            } else {
              throw new Error('Browser instance is not initiated.');
            }
            callback(null, { text: 'New tab opened' });
        } catch (error) {
            logDebug('Error in openTab:', (error as Error).message);
            callback({
                code: grpc.status.INTERNAL,
                message: `Failed to open a new tab: ${(error as Error).message}`,
            });
        }
    },

    setViewport: async (call: ServerUnaryCall<ViewportSize, Message>, callback: sendUnaryData<Message>) => {
        try {
            if (!page) throw new Error('No active page');
            const { width, height } = call.request;
            await page.setViewport({ width, height });
            callback(null, { text: 'Viewport set' });
        } catch (error) {
            logDebug('Error in setViewport:', (error as Error).message);
            callback({
                code: grpc.status.INTERNAL,
                message: `Failed to set viewport: ${(error as Error).message}`,
            });
        }
    },

    clickMouse: async (call: ServerUnaryCall<Coordinate, Message>, callback: sendUnaryData<Message>) => {
        try {
            if (!page) throw new Error('No active page');
            const { x, y } = call.request;
            await page.mouse.click(x, y);
            callback(null, { text: 'Mouse clicked' });
        } catch (error) {
            logDebug('Error in clickMouse:', (error as Error).message);
            callback({
                code: grpc.status.INTERNAL,
                message: `Failed to click mouse: ${(error as Error).message}`,
            });
        }
    },

    sendKeyboardInput: async (call: ServerUnaryCall<Text, Message>, callback: sendUnaryData<Message>) => {
        try {
            if (!page) throw new Error('No active page');
            await page.keyboard.type(call.request.content);
            callback(null, { text: 'Keyboard input sent' });
        } catch (error) {
            logDebug('Error in sendKeyboardInput:', (error as Error).message);
            callback({
                code: grpc.status.INTERNAL,
                message: `Failed to send keyboard input: ${(error as Error).message}`,
            });
        }
    },

    navigateToUrl: async (call: ServerUnaryCall<Url, Message>, callback: sendUnaryData<Message>) => {
        try {
            if (!page) throw new Error('No active page');
            await page.goto(call.request.url);
            callback(null, { text: 'Navigated to URL' });
        } catch (error) {
            logDebug('Error in navigateToUrl:', (error as Error).message);
            callback({
                code: grpc.status.INTERNAL,
                message: `Failed to navigate to URL: ${(error as Error).message}`,
            });
        }
    },


    streamScreenshots: async (call: ServerWritableStream<ScreenshotRequest, Screenshot>) => {
        const fps = call.request.fps || 10;
        const interval = 1000 / fps;
        logDebug(`Starting screenshot stream at ${fps} FPS`);

        const intervalId = setInterval(async () => {
            try {
                if (!page) {
                    logDebug('No active page, stopping stream');
                    clearInterval(intervalId);
                    call.end();
                    return;
                }

                const screenshot = await page.screenshot({ 
                    type: 'jpeg',
                    quality: 60
                });
                const screenshotBuffer = Buffer.from(screenshot);

                // Write to stream
                const success = call.write({ data: screenshotBuffer });
                if (!success) {
                    logDebug('Stream backpressure detected');
                }
            } catch (error) {
                logDebug('Error in streamScreenshots:', (error as Error).message);
                clearInterval(intervalId);
                call.destroy(error as Error);
            }
        }, interval);

        // Handle stream cancellation
        call.on('cancelled', () => {
            logDebug('Stream cancelled by client');
            clearInterval(intervalId);
        });

        call.on('error', (err) => {
            logDebug('Stream error:', err.message);
            clearInterval(intervalId);
        });
    },
};

// gRPC server setup
function main() {
  const server = new grpc.Server();
  server.addService(BrowserControlService, browserControlHandlers);

  // Determine binding address
  let bindAddress: string;
  if (options.tcp) {
    bindAddress = options.tcp;
    console.log(`Using TCP socket: ${bindAddress}`);
  } else {
    // Use Unix domain socket by default
    bindAddress = 'unix:///tmp/termium.sock';
    // Clean up any existing socket file
    const socketPath = '/tmp/termium.sock';
    if (fs.existsSync(socketPath)) {
      fs.unlinkSync(socketPath);
    }
    console.log(`Using Unix domain socket: ${socketPath}`);
  }

  server.bindAsync(bindAddress, grpc.ServerCredentials.createInsecure(), (err, port) => {
    if (err) {
      console.error('Failed to bind server:', err);
      return;
    }
    if (options.tcp) {
      console.log(`Server running at ${bindAddress}`);
    } else {
      console.log(`Server running on Unix domain socket: /tmp/termium.sock`);
    }
  });

  // Handle shutdown gracefully
  const signals = ['SIGINT', 'SIGTERM'];
  signals.forEach(signal => {
    process.on(signal, () => {
      console.log(`Received ${signal}, shutting down...`);
      server.tryShutdown(() => {
        console.log('Server shutdown complete');
        process.exit(0);
      });
    });
  });
}

main();
