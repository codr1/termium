import * as grpc from '@grpc/grpc-js';
import { BrowserControls } from './browser-controls';
import * as puppeteer from 'puppeteer';
import { Command } from 'commander';
import * as fs from 'fs';
import * as path from 'path';
import debugFactory from 'debug';

// Update import paths
import { ServerUnaryCall, sendUnaryData, ServerWritableStream } from '@grpc/grpc-js';
import { BrowserControlService, BrowserControlServer } from '../generated/bc';
import { Empty, Message, ViewportSize, Coordinate, Text, Url, Screenshot, ScreenshotRequest, DialogEvent, DialogResponse } from '../generated/bc';

const program = new Command();
const logDebug = debugFactory('server:debug');

// Puppeteer browser and page instances
let browser: puppeteer.Browser | null = null;
let page: puppeteer.Page | null = null;
let browserLaunch: Promise<void> | null = null;
let shuttingDown = false;
const browserAbort = new AbortController();
let pageCreation: Promise<puppeteer.Page> | null = null;
const controls = new BrowserControls(ensurePage);

async function ensurePage(): Promise<puppeteer.Page> {
    if (page && !page.isClosed()) return page;
    if (!pageCreation) {
        pageCreation = (async () => {
            await launchOrConnectToBrowser();
            const created = await browser!.newPage();
            page = created;
            setupDialogHandler(created);
            controls.attach(created);
            return created;
        })().finally(() => { pageCreation = null; });
    }
    return pageCreation;
}

// Dialog handling
interface PendingDialog {
    dialog: puppeteer.Dialog;
    owner: grpc.ServerDuplexStream<DialogResponse, DialogEvent>;
    resolver: () => void;
}
let dialogStream: grpc.ServerDuplexStream<DialogResponse, DialogEvent> | null = null;
const pendingDialogs = new Map<string, PendingDialog>();
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
                const owner = dialogStream;
                await new Promise<void>((resolve) => {
                    pendingDialogs.set(dialogId, { dialog, owner, resolver: resolve });
                    owner.write({
                        id: dialogId,
                        type: protoType,
                        message,
                        defaultValue: defaultValue || ''
                    });
                });
            } catch (error) {
                pendingDialogs.delete(dialogId);
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
    if (shuttingDown) throw new Error('Server is shutting down');
    if (browser) return;
    // Concurrent RPCs must share one launch, including its cancellation.
    if (!browserLaunch) {
        browserLaunch = launchBrowser().finally(() => { browserLaunch = null; });
    }
    await browserLaunch;
}

async function launchBrowser() {
    if (options.browser) {
        // Connect to an existing browser instance using DevTools protocol
        logDebug('Connecting to existing browser instance at', options.browser);
        browser = await puppeteer.connect({ browserWSEndpoint: `ws://${options.browser}` });
    } else {
        // Launch a new headless browser if no browser address is provided
        logDebug('Launching a new headless browser');
        browser = await puppeteer.launch({ 
            handleSIGINT: false, // The server owns signal handling and process exit.
            handleSIGTERM: false,
            signal: browserAbort.signal,
            headless: true,
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
    openTab: async (_call, callback) => {
        try {
            await ensurePage();
            callback(null, { text: 'Tab ready' });
        } catch (error) {
            callback({ code: grpc.status.INTERNAL, message: (error as Error).message });
        }
    },

    getBrowserState: async (_call, callback) => {
        try { callback(null, await controls.state()); }
        catch (error) { callback({ code: grpc.status.INTERNAL, message:(error as Error).message }); }
    },
    browserCommand: async (call, callback) => {
        try { callback(null, await controls.command(call.request)); }
        catch (error) { callback({ code:(error as any).code ?? grpc.status.INTERNAL, message:(error as Error).message }); }
    },
    sendInput: async (call, callback) => {
        try { await controls.input(call.request); callback(null, { text:'Input dispatched' }); }
        catch (error) { callback({ code:(error as any).code ?? grpc.status.INTERNAL, message:(error as Error).message }); }
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
            
            await ensurePage();
            // Start navigation
            logDebug(`Starting navigation to: ${url}`);
            await page!.goto(url, {
                waitUntil: 'networkidle0',
                timeout: 8000
            });
            
            const newUrl = page!.url();
            logDebug(`Successfully navigated to: ${url}, actual URL: ${newUrl}`);
            callback(null, { text: `Navigated to ${newUrl}` });
        } catch (error) {
            const errorMessage = (error as Error).message;
            const url = call.request.url;
            
            // Log more details about the error
            logDebug(`Navigation error for '${url}': ${errorMessage}`);
            
            // A responsive old page is not evidence that this navigation worked.
            // Preserve real failures, including browser launch and network errors.
            callback({
                code: error instanceof puppeteer.TimeoutError
                    ? grpc.status.DEADLINE_EXCEEDED : grpc.status.INTERNAL,
                message: `Navigation to '${url}' failed: ${errorMessage}`,
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
        const format = call.request.format || 'jpeg';
        const interval = 1000 / fps;
        logDebug(`Starting screenshot stream at ${fps} FPS, format: ${format}`);

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
                const screenshotOptions: any = format === 'png'
                    ? { type: 'png' }
                    : { type: 'jpeg', quality: 60 };
                const generation = controls.generation;
                const screenshotPromise = page.screenshot(screenshotOptions);
                
                const timeoutPromise = new Promise<never>((_, reject) => {
                    setTimeout(() => reject(new Error('Screenshot timeout after 1 second')), 1000);
                });

                // Race between screenshot and timeout
                let screenshot: Buffer;
                try {
                    screenshot = await Promise.race([screenshotPromise, timeoutPromise]) as unknown as Buffer;
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
                if (!isCancelled && generation === controls.generation) {
                    const success = call.write({ data: screenshotBuffer, generation });
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

    streamDialogs: (call) => {
        logDebug('Dialog stream connected');
        if (dialogStream) {
            call.emit('error', { code: grpc.status.RESOURCE_EXHAUSTED, message: 'A dialog stream is already connected' });
            return;
        }
        dialogStream = call;
        // A client can wait for headers before triggering a page dialog.
        call.sendMetadata(new grpc.Metadata());
        
        // Resolve once, and always consume Puppeteer's promise rejection.
        const finish = async (id: string, pending: PendingDialog, response: Pick<DialogResponse, 'accepted' | 'inputText'>) => {
            pendingDialogs.delete(id);
            try {
                if (response.accepted) {
                    await pending.dialog.accept(pending.dialog.type() === 'prompt' ? response.inputText : undefined);
                } else {
                    await pending.dialog.dismiss();
                }
            } catch (error) {
                logDebug(`Failed to resolve dialog ${id}: ${(error as Error).message}`);
            } finally {
                pending.resolver();
            }
        };

        call.on('data', (response: DialogResponse) => {
            const pending = pendingDialogs.get(response.id);
            if (pending && pending.owner === call) {
                void finish(response.id, pending, response);
            }
        });

        let disconnected = false;
        const disconnect = () => {
            if (disconnected) return;
            disconnected = true;
            if (dialogStream === call) dialogStream = null;
            for (const [id, pending] of pendingDialogs) {
                if (pending.owner === call) {
                    void finish(id, pending, { accepted: true, inputText: pending.dialog.defaultValue() });
                }
            }
            call.end();
        };
        call.on('end', disconnect);
        call.on('cancelled', disconnect);
        call.on('error', disconnect);
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
      process.exitCode = 1;
      server.forceShutdown();
      return;
    }
    if (options.tcp) {
      // Report the actual port when the OS assigns one (--tcp 127.0.0.1:0).
      console.log(`Server running at ${bindAddress.replace(/:\d+$/, `:${port}`)}`);
    } else {
      console.log(`Server running on Unix domain socket: /tmp/termium.sock`);
    }
    // Readiness sentinel — client watches for this line to know server is accepting connections
    console.log('TERMIUM_READY');
  });

  // Termination must not wait for clients to close long-lived streams. Abort
  // any in-flight browser launch too; browser may not have been assigned yet.
  const shutdown = async (signal: string) => {
    if (shuttingDown) return;
    shuttingDown = true;
    console.log(`Received ${signal}, shutting down...`);
    server.forceShutdown();
    const forcedExit = setTimeout(() => {
      console.error('Shutdown timed out');
      browserAbort.abort();
      process.exit(1);
    }, 2000);
    try {
      if (!browser) browserAbort.abort();
      await browserLaunch?.catch(() => {});
      if (browser) await browser.close();
      console.log('Server shutdown complete');
      clearTimeout(forcedExit);
      process.exit(0);
    } catch (error) {
      console.error('Error shutting down:', error);
      browserAbort.abort();
      clearTimeout(forcedExit);
      process.exit(1);
    }
  };
  for (const signal of ['SIGINT', 'SIGTERM']) {
    process.on(signal, () => { void shutdown(signal); });
  }

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
