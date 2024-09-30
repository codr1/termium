import * as grpc from '@grpc/grpc-js';
import * as puppeteer from 'puppeteer';
import { Command } from 'commander';
import * as fs from 'fs';
import * as path from 'path';
import debugFactory from 'debug';

// Import the generated service and message types from ts-proto
import { BrowserControlService, BrowserControlServer } from '../generated/bc'; // Adjust the path as needed
import { ServerUnaryCall, sendUnaryData } from '@grpc/grpc-js';
import { Empty, Message, ViewportSize, Coordinate, Text, Url, Screenshot } from '../generated/bc';

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

    takeScreenshot: async (_call: ServerUnaryCall<Empty, Screenshot>, callback: sendUnaryData<Screenshot>) => {
        try {
            if (!page) throw new Error('No active page');
            const screenshot = await page.screenshot({ type: 'png' });
            callback(null, { data: screenshot });
        } catch (error) {
            logDebug('Error in takeScreenshot:', (error as Error).message);
            callback({
                code: grpc.status.INTERNAL,
                message: `Failed to take screenshot: ${(error as Error).message}`,
            });
        }
    },
};

// gRPC server setup
function main() {
    const server = new grpc.Server();
    // Register the BrowserControl service
    server.addService(BrowserControlService, browserControlHandlers);

    // Start the server asynchronously
    server.bindAsync('0.0.0.0:50051', grpc.ServerCredentials.createInsecure(), (err, port) => {
        if (err) {
            console.error('Failed to bind server:', err);
            return;
        }
        console.log(`Server running at http://0.0.0.0:${port}`);

        // Start the server after successful bind
        server.start();
    });
}

main();
