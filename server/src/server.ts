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

// Dialog handling
interface PendingDialog {
    dialog: puppeteer.Dialog;
    resolver: (response: any) => void;
}
let dialogStream: any = null; // Will be set when client connects
let pendingDialogs = new Map<string, PendingDialog>();
let dialogIdCounter = 0;

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

// Helper function to set up dialog handler on a page
function setupDialogHandler(page: puppeteer.Page) {
    page.on('dialog', async (dialog) => {
        const dialogType = dialog.type();
        const message = dialog.message();
        const defaultValue = dialog.defaultValue();
        
        logDebug(`Dialog detected - Type: ${dialogType}, Message: ${message}`);
        
        // If we have a connected client, send dialog to them
        if (dialogStream) {
            const dialogId = `dialog_${++dialogIdCounter}`;
            
            // Map Puppeteer dialog type to our proto enum
            let protoType = 0; // ALERT
            if (dialogType === 'confirm') protoType = 1;
            else if (dialogType === 'prompt') protoType = 2;
            else if (dialogType === 'beforeunload') protoType = 3;
            
            // Send dialog event to client
            try {
                dialogStream.write({
                    id: dialogId,
                    type: protoType,
                    message: message,
                    defaultValue: defaultValue || ''
                });
                
                // Wait for response from client
                await new Promise<void>((resolve) => {
                    pendingDialogs.set(dialogId, {
                        dialog: dialog,
                        resolver: resolve
                    });
                });
            } catch (error) {
                logDebug(`Error sending dialog to client: ${(error as Error).message}`);
                // Fallback to auto-accept if client communication fails
                await dialog.accept(defaultValue || '');
            }
        } else {
            // No client connected, auto-accept to prevent blocking
            logDebug('No client connected, auto-accepting dialog');
            await dialog.accept(defaultValue || '');
        }
    });
    logDebug('Dialog handler installed on page');
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
        browser = await puppeteer.launch({ 
            headless: 'new' as any,  // Use the new headless mode (less detectable) - cast to any for older types
            args: [
                '--no-sandbox',
                '--disable-setuid-sandbox',
                '--disable-blink-features=AutomationControlled',  // Hide automation
                '--user-agent=Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
            ]
        });
    }
}

const browserControlHandlers: BrowserControlServer = {
    openTab: async (_call: ServerUnaryCall<Empty, Message>, callback: sendUnaryData<Message>) => {
        try {
            if (!browser) {
                await launchOrConnectToBrowser();
            }
            
            // Only create a new page if we don't have one already
            if (!page || page.isClosed()) {
                if (browser) {
                    page = await browser.newPage();
                    logDebug('Created new page in openTab');
                    setupDialogHandler(page);
                } else {
                    throw new Error('Browser instance is not initiated.');
                }
                callback(null, { text: 'New tab opened' });
            } else {
                logDebug('Page already exists, reusing it');
                callback(null, { text: 'Using existing tab' });
            }
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
            if (!page || page.isClosed()) throw new Error('No active page or page is closed');
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
            if (!page || page.isClosed()) throw new Error('No active page or page is closed');
            const content = call.request.content;
            
            // Check if this is a special key (format: __KEY__KeyName)
            if (content.startsWith('__KEY__')) {
                const key = content.substring(7); // Remove __KEY__ prefix
                logDebug(`Pressing special key: ${key}`);
                await page.keyboard.press(key as puppeteer.KeyInput);
            } else {
                // Regular text input
                logDebug(`Typing text: ${content}`);
                await page.keyboard.type(content);
            }
            
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
            const url = call.request.url;
            logDebug(`Attempting to navigate to URL: ${url}`);
            
            // List all pages before navigation
            if (browser) {
                const pages = await browser.pages();
                logDebug(`Pages BEFORE navigation (${pages.length} total):`);
                for (let i = 0; i < pages.length; i++) {
                    const pageUrl = pages[i].url();
                    const isCurrent = pages[i] === page;
                    logDebug(`  Page ${i}: ${pageUrl}${isCurrent ? ' (current)' : ''}`);
                }
            }
            
            // Check if we have no page at all
            if (!page) {
                logDebug('No page exists, creating initial page');
                if (!browser) {
                    await launchOrConnectToBrowser();
                }
                if (browser) {
                    page = await browser.newPage();
                    logDebug('Created initial page');
                    setupDialogHandler(page);
                } else {
                    throw new Error('Failed to create browser');
                }
            } else if (page.isClosed()) {
                // Page was closed, need to create a new one
                logDebug('Page was closed, creating new page');
                page = await browser!.newPage();
                logDebug('Created replacement page');
                setupDialogHandler(page);
            } else {
                // Page exists and is open - reuse it!
                logDebug('Reusing existing page for navigation');
            }
            
            // Set up dialog handler if not already set
            if (!page.listenerCount('dialog')) {
                setupDialogHandler(page);
            }
            
            // Log current URL before navigation
            const currentUrl = page.url();
            logDebug(`Current URL before navigation: ${currentUrl}`);
            
            // Set up event listeners for debugging only - no promises that could reject
            const loadListener = () => {
                logDebug(`Page 'load' event fired`);
            };
            const domContentLoadedListener = () => {
                logDebug(`Page 'domcontentloaded' event fired`);
            };
            const errorListener = (err: Error) => {
                logDebug(`Page error during navigation: ${err.message}`);
            };
            
            page!.once('load', loadListener);
            page!.once('domcontentloaded', domContentLoadedListener);
            page!.once('error', errorListener);
            
            // Start navigation
            logDebug(`Starting navigation to: ${url}`);
            await page.goto(url, {
                waitUntil: 'networkidle0',
                timeout: 8000
            });
            
            const newUrl = page.url();
            logDebug(`Successfully navigated to: ${url}, actual URL: ${newUrl}`);
            callback(null, { text: `Navigated to ${newUrl}` });
        } catch (error) {
            const errorMessage = (error as Error).message;
            const url = call.request.url;
            
            // Log more details about the error
            logDebug(`Navigation error for '${url}': ${errorMessage}`);
            
            // IMPORTANT: Check what URL we actually ended up on, even if navigation "failed"
            let actualUrl = 'unknown';
            let pageIsResponsive = false;
            let previousUrl = '';
            
            try {
                // Get the URL we were on before navigation attempt
                previousUrl = page?.url() || '';
            } catch (e) {
                // Page might not be accessible
            }
            
            try {
                actualUrl = page?.url() || 'unknown';
                pageIsResponsive = true;
                logDebug(`Despite timeout, page is responsive and loaded URL: ${actualUrl}`);
                
                // If we're on a different URL than before, navigation partially succeeded
                // Also check if we're on a redirect of the requested URL
                const requestedDomain = new URL(url).hostname;
                const actualDomain = actualUrl !== 'unknown' && actualUrl !== 'about:blank' ? new URL(actualUrl).hostname : '';
                
                if ((actualUrl !== previousUrl && actualUrl !== 'about:blank') || 
                    (actualDomain && actualDomain.includes(requestedDomain.replace('www.', '').replace('.com', '')))) {
                    logDebug(`Navigation succeeded (with timeout) - browser is now at: ${actualUrl}`);
                    // Return success since we did navigate somewhere
                    callback(null, { text: `Navigated to ${actualUrl}` });
                    return;
                }
            } catch (e) {
                logDebug(`Page is not responsive: ${(e as Error).message}`);
            }
            
            // Only return error if we truly failed to navigate
            callback({
                code: grpc.status.DEADLINE_EXCEEDED,
                message: `Navigation to '${url}' timed out. Current URL: ${actualUrl}`,
            });
        }
    },

    getCurrentUrl: async (_call: ServerUnaryCall<Empty, Url>, callback: sendUnaryData<Url>) => {
        try {
            if (!page || page.isClosed()) throw new Error('No active page or page is closed');
            const currentUrl = page.url();
            callback(null, { url: currentUrl });
        } catch (error) {
            logDebug('Error in getCurrentUrl:', (error as Error).message);
            callback({
                code: grpc.status.INTERNAL,
                message: `Failed to get current URL: ${(error as Error).message}`,
            });
        }
    },


    streamScreenshots: async (call: ServerWritableStream<ScreenshotRequest, Screenshot>) => {
        const fps = call.request.fps || 10;
        const interval = 1000 / fps;
        logDebug(`Starting screenshot stream at ${fps} FPS`);

        let intervalId: NodeJS.Timeout | null = null;
        let isCancelled = false;
        let frameCount = 0;
        let errorCount = 0;
        let lastPageUrl = '';
        let isScreenshotInProgress = false;

        intervalId = setInterval(async () => {
            // Check if stream is cancelled before attempting to write
            if (isCancelled || !intervalId) {
                return;
            }

            // Skip if a screenshot is already in progress
            if (isScreenshotInProgress) {
                // Don't log this - it's too noisy
                return;
            }

            try {
                if (!page || page.isClosed()) {
                    logDebug('Page is closed or invalid, stopping stream');
                    if (intervalId) {
                        clearInterval(intervalId);
                        intervalId = null;
                    }
                    isCancelled = true;
                    call.end();
                    return;
                }

                // Check if URL changed (navigation happened)
                const currentUrl = page.url();
                if (currentUrl !== lastPageUrl) {
                    logDebug(`Page URL changed from '${lastPageUrl}' to '${currentUrl}'`);
                    lastPageUrl = currentUrl;
                }

                // Mark screenshot as in progress
                isScreenshotInProgress = true;
                const startTime = Date.now();

                // Create a promise that times out after 1 second
                const screenshotPromise = page.screenshot({ 
                    type: 'jpeg',
                    quality: 60
                });
                
                const timeoutPromise = new Promise<never>((_, reject) => {
                    setTimeout(() => reject(new Error('Screenshot timeout after 1 second')), 1000);
                });

                // Race between screenshot and timeout
                let screenshot: Uint8Array;
                try {
                    screenshot = await Promise.race([screenshotPromise, timeoutPromise]);
                } finally {
                    // ALWAYS clear the flag, even if we timeout
                    isScreenshotInProgress = false;
                    const elapsed = Date.now() - startTime;
                    if (elapsed > 100) {
                        logDebug(`Screenshot took ${elapsed}ms`);
                    }
                }
                const screenshotBuffer = Buffer.from(screenshot);

                // Only write if not cancelled
                if (!isCancelled) {
                    const success = call.write({ data: screenshotBuffer });
                    if (!success) {
                        logDebug('Stream backpressure detected');
                    } else {
                        frameCount++;
                        // Reset error count on success
                        if (errorCount > 0) {
                            errorCount = 0;
                            logDebug('Screenshot errors cleared after successful frame');
                        }
                        // Log successful screenshot periodically (every 24 frames = 1 second at 24fps)
                        if (frameCount % 24 === 0) {
                            logDebug(`Screenshots sent: ${frameCount}`);
                        }
                    }
                }
            } catch (error) {
                // Clear the in-progress flag on error
                isScreenshotInProgress = false;
                
                errorCount++;
                logDebug(`Error in streamScreenshots (error #${errorCount}): ${(error as Error).message}`);
                
                // If screenshot failed, list all open pages
                if (browser) {
                    try {
                        const pages = await browser.pages();
                        logDebug(`Currently open pages (${pages.length} total):`);
                        for (let i = 0; i < pages.length; i++) {
                            const pageUrl = pages[i].url();
                            const isCurrent = pages[i] === page;
                            logDebug(`  Page ${i}: ${pageUrl}${isCurrent ? ' (current)' : ''}`);
                        }
                    } catch (listError) {
                        logDebug(`Failed to list pages: ${(listError as Error).message}`);
                    }
                }
                
                // Don't stop on first error - try to continue
                if (errorCount > 10) {
                    logDebug('Too many screenshot errors, stopping stream');
                    if (intervalId) {
                        clearInterval(intervalId);
                        intervalId = null;
                    }
                    if (!isCancelled) {
                        call.destroy(error as Error);
                    }
                }
            }
        }, interval);

        // Handle stream cancellation
        call.on('cancelled', () => {
            logDebug('Stream cancelled by client');
            isCancelled = true;
            if (intervalId) {
                clearInterval(intervalId);
                intervalId = null;
            }
        });

        call.on('error', (err) => {
            logDebug('Stream error:', err.message);
            isCancelled = true;
            if (intervalId) {
                clearInterval(intervalId);
                intervalId = null;
            }
        });

        // Handle stream end
        call.on('end', () => {
            logDebug('Stream ended by client');
            isCancelled = true;
            if (intervalId) {
                clearInterval(intervalId);
                intervalId = null;
            }
        });
    },

    streamDialogs: (call: any) => {
        logDebug('Dialog stream connected');
        dialogStream = call;
        
        // Handle incoming dialog responses from client
        call.on('data', (response: any) => {
            logDebug(`Received dialog response: id=${response.id}, accepted=${response.accepted}`);
            
            const pending = pendingDialogs.get(response.id);
            if (pending) {
                // Handle the dialog based on response
                if (response.accepted) {
                    if (response.inputText !== undefined && response.inputText !== '') {
                        // Prompt with text
                        pending.dialog.accept(response.inputText).then(() => {
                            logDebug(`Dialog ${response.id} accepted with text: ${response.inputText}`);
                        });
                    } else {
                        // Regular accept
                        pending.dialog.accept().then(() => {
                            logDebug(`Dialog ${response.id} accepted`);
                        });
                    }
                } else {
                    // Dismiss/cancel
                    pending.dialog.dismiss().then(() => {
                        logDebug(`Dialog ${response.id} dismissed`);
                    });
                }
                
                // Resolve the promise and clean up
                pending.resolver(response);
                pendingDialogs.delete(response.id);
            } else {
                logDebug(`No pending dialog found for id: ${response.id}`);
            }
        });
        
        call.on('end', () => {
            logDebug('Dialog stream disconnected');
            dialogStream = null;
            
            // Auto-accept any pending dialogs since client disconnected
            for (const [id, pending] of pendingDialogs) {
                logDebug(`Auto-accepting dialog ${id} due to stream disconnect`);
                pending.dialog.accept();
                pending.resolver(null);
            }
            pendingDialogs.clear();
        });
        
        call.on('error', (err: Error) => {
            logDebug('Dialog stream error:', err.message);
            dialogStream = null;
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
    process.on(signal, async () => {
      console.log(`Received ${signal}, shutting down...`);
      
      // Close browser if it exists
      if (browser) {
        try {
          logDebug('Closing browser...');
          await browser.close();
          console.log('Browser closed successfully');
        } catch (error) {
          console.error('Error closing browser:', error);
        }
      }
      
      server.tryShutdown(() => {
        console.log('Server shutdown complete');
        process.exit(0);
      });
    });
  });
  
  // Also handle uncaught exceptions and unhandled rejections
  process.on('uncaughtException', async (error) => {
    console.error('Uncaught exception:', error);
    if (browser) {
      try {
        await browser.close();
      } catch (closeError) {
        console.error('Error closing browser on uncaught exception:', closeError);
      }
    }
    process.exit(1);
  });

  process.on('unhandledRejection', async (reason, promise) => {
    console.error('Unhandled rejection at:', promise, 'reason:', reason);
    if (browser) {
      try {
        await browser.close();
      } catch (closeError) {
        console.error('Error closing browser on unhandled rejection:', closeError);
      }
    }
    process.exit(1);
  });
}

main();
