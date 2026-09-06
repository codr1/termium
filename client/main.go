package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/jpeg"
	"os"
	"os/signal"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
	"golang.org/x/term"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "termium/client/pb"
)

// Build-time version info (set by GoReleaser ldflags)
var (
	version = "dev"
	commit  = "none"
)

// Screen geometry
const (
	LOG_PANEL_HEIGHT   = 5 // height
	H_BORDER_WIDTH     = 1 // width in chars of all Horizontal borders
	V_BORDER_WIDTH     = 1 // width in chars of all vertical borders
	INTER_PANEL_BORDER = 1 // width in chars of the border between the panels
)

// ScreenDimensions holds the current screen dimensions and panel calculations
type ScreenDimensions struct {
	Width           int // Total screen width
	Height          int // Total screen height
	LogHeight       int // Height of the log panel (constant: 5)
	LogPanelTop     int // Y coordinate where log panel starts
	ViewHeight      int // Height of the browser panel
	InnerWidth      int // Width minus borders
	InnerViewHeight int // Browser height minus borders
	InnerWidthPx    int // Usable width in pixels
	InnerHeightPx   int // Usable browser height in pixels
}

var sDims ScreenDimensions

type CharSize struct {
	Width  int
	Height int
}

var charSize CharSize

type LogBuffer struct {
	messages []string
	mutex    sync.Mutex
}

var logBuffer LogBuffer

type Cursor struct {
	x, y      int
	visible   bool
	blinkOn   bool
	lastBlink time.Time
}

type MouseInfo struct {
	PixelX, PixelY int
	CharX, CharY   int
}

var currentMouse MouseInfo

var cursor Cursor

// Gets
var firstDraw bool = true

var imageBuffer *image.RGBA

var imageBounds image.Rectangle

// GRPC Client
var grpcClient pb.BrowserControlClient

var grpcConn *grpc.ClientConn

var screenshotMutex sync.Mutex

var lastScreenshotTime time.Time

var lastImageNumber int = 1

// Browser control state
type BrowserMode int

const (
	ModeNormal BrowserMode = iota
	ModeURL
)

// Global keyboard handler
var keyboardHandler *KeyboardHandler

// Cached sixel encoder - create once, reuse many times
var sixelEncoder *sixel.Encoder
var sixelEncoderMutex sync.Mutex

// Band manager for efficient sixel updates
var bandManager *BandManager

// Pre-allocated RGBA buffers for zero-allocation frame comparison
var (
    currentRGBA  *image.RGBA
    previousRGBA *image.RGBA
    scaledBuffer *image.RGBA  // Reusable buffer for scaled images
    rgbaLock     sync.Mutex
)

// Channel to signal screenshot loop to stop
var stopScreenshots = make(chan bool, 1)

// Wait group to ensure clean shutdown
var shutdownWg sync.WaitGroup

// MenuAction represents the result of a local key event
type MenuAction int

const (
	MenuNone MenuAction = iota
	MenuContinue
	MenuExit
	MenuBack
	MenuSelect
)

var cfg *Config

// Write implements the io.Writer interface for LogBuffer
func (lb *LogBuffer) Write(p []byte) (n int, err error) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()

	// Convert to string and clean up any control characters
	message := string(p)
	message = cleanString(message)

	// Only append non-empty messages
	if message != "" {
		lb.messages = append(lb.messages, message)

		// Keep buffer size manageable
		if len(lb.messages) > 1000 {
			lb.messages = lb.messages[1:]
		}
	}

	return len(p), nil
}

// cleanString removes control characters and normalizes whitespace
func cleanString(s string) string {
	var result []rune
	for _, r := range s {
		// Skip control characters except newline and tab
		if r >= 32 && r != 127 || r == '\n' || r == '\t' {
			result = append(result, r)
		}
	}
	return string(result)
}

func displayErrorMessage(s tcell.Screen, message string) {
	style := tcell.StyleDefault.Foreground(tcell.ColorRed).Background(tcell.ColorBlack)

	// Clear the bottom line
	for x := 0; x < sDims.Width; x++ {
		s.SetContent(x, sDims.Height-1, ' ', nil, style)
	}

	// Display the error message
	for i, ch := range message {
		if i < sDims.Width {
			s.SetContent(i, sDims.Height-1, ch, nil, style)
		}
	}
	s.Show()
}

func blinkCursor(s tcell.Screen) {
	now := time.Now()
	if now.Sub(cursor.lastBlink) >= 500*time.Millisecond {
		cursor.blinkOn = !cursor.blinkOn
		cursor.lastBlink = now
		redrawImageArea(s, cursor.x, cursor.y)
		s.Show() // Make sure to show the changes
	}
}

// redrawImageArea redraws a specific area of the image, including the cursor if present
func redrawImageArea(s tcell.Screen, x, y int) {
	if imageBuffer == nil {
		return
	}

	pixelX := x * charSize.Width
	pixelY := y * charSize.Height

	for dy := 0; dy < charSize.Height; dy++ {
		for dx := 0; dx < charSize.Width; dx++ {
			if pixelX+dx < imageBounds.Max.X && pixelY+dy < imageBounds.Max.Y {
				c := imageBuffer.At(pixelX+dx, pixelY+dy)
				r, g, b, _ := c.RGBA()
				style := tcell.StyleDefault.Background(tcell.NewRGBColor(int32(r>>8), int32(g>>8), int32(b>>8)))
				s.SetContent(x, y, ' ', nil, style)
			}
		}
	}

	if x == cursor.x && y == cursor.y && cursor.visible {
		style := tcell.StyleDefault.Background(tcell.ColorWhite)
		if !cursor.blinkOn {
			// Use the image color when the cursor is not visible
			c := imageBuffer.At(pixelX, pixelY)
			r, g, b, _ := c.RGBA()
			style = tcell.StyleDefault.Background(tcell.NewRGBColor(int32(r>>8), int32(g>>8), int32(b>>8)))
		}
		s.SetContent(x, y, ' ', nil, style)
	}
}

// main is the entry point of the application
func main() {
	// Parse command line flags
	var err error
	cfg, err = parseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse flags: %v\n", err)
		os.Exit(1)
	}

	// Set up CPU profiling if requested
	if cfg.CPUProfile != "" {
		f, err := os.Create(cfg.CPUProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create CPU profile: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start CPU profile: %v\n", err)
			os.Exit(1)
		}
		defer pprof.StopCPUProfile()
		fmt.Fprintf(os.Stderr, "CPU profiling enabled, writing to %s\n", cfg.CPUProfile)
	}

	// Set up trace profiling if requested
	if cfg.TraceProfile != "" {
		f, err := os.Create(cfg.TraceProfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create trace file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		if err := trace.Start(f); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start trace: %v\n", err)
			os.Exit(1)
		}
		defer trace.Stop()
		fmt.Fprintf(os.Stderr, "Trace profiling enabled, writing to %s\n", cfg.TraceProfile)
	}

	// Set up logging first
	if cfg.LogFile != "" {
		if err := SetLogFile(cfg.LogFile); err != nil {
			Debug(fmt.Sprintf("Failed to set up log file: %v", err), ERROR)
			os.Exit(1)
		}
		defer CloseLogFile()
	}

	// Enable debug if flag is set
	SetDebug(cfg.Debug)

	Debug("Starting application", INFO)
	if cfg.Debug {
		Debug("Debug mode enabled", DEBUG)
	}

	detectTerminalAndCalibrate()
	s := initializeScreen()

	initializeCursor()

	// Show splash screen and wait for user input (unless NONE)
	if cfg.SplashPath != "NONE" {
		Debug(fmt.Sprintf("Loading splash screen from: %s", cfg.SplashPath), DEBUG)
		if err := showSplashScreen(s, cfg.SplashPath); err != nil {
			Debug(fmt.Sprintf("Error showing splash screen: %v", err), ERROR)
			displayErrorMessage(s, fmt.Sprintf("Error showing splash screen: %v", err))
			return
		}
	}

	// Display usage instructions after splash screen
	displayInstructions(s)

	Debug("Setting up signal handlers", DEBUG)
	setupSignalHandling(s)

	// Auto-launch server if not already running
	if err := startServer(); err != nil {
		Debug(fmt.Sprintf("Failed to auto-launch server: %v", err), ERROR)
		displayErrorMessage(s, fmt.Sprintf("Server not available: %v", err))
		// Continue anyway — user may start server manually
	}

	// Connect to gRPC server
	if err := connectToGRPCServer(); err != nil {
		Debug(fmt.Sprintf("Failed to connect to server: %v", err), ERROR)
	}
	defer grpcConn.Close()
	
	// Initialize keyboard handler with the gRPC client
	keyboardHandler = NewKeyboardHandler(grpcClient)
	
	if err := openNewTab(); err != nil {
		Debug(fmt.Sprintf("Failed to open new tab: %v", err), ERROR)
	}

	// Open home page.  For now I have it hardcoded to caffenero.com
	if _, err := grpcClient.NavigateToUrl(
		context.Background(),
		//&pb.Url{Url: "https://www.caffenero.com"},
		&pb.Url{Url: "https://kleki.com/"},
	); err != nil {
		Debug(fmt.Sprintf("Failed to navigate to homepage: %v", err), ERROR)
	}
	Debug("Successfully opened homepage", INFO)

	// Start the screenshot goroutine
	shutdownWg.Add(1)
	go screenshotLoop(s)

	if err := runMainLoop(s); err != nil {
		displayErrorMessage(s, fmt.Sprintf("Error in main loop: %v", err))
	}
	
	// Perform clean shutdown
	cleanShutdown(s)
}

// Draws teal borders around both panels and sets the bottom panel background to navy
func drawBorder(s tcell.Screen) {
	borderStyle := tcell.StyleDefault.Foreground(tcell.ColorTeal)
	navyStyle := tcell.StyleDefault.Background(tcell.ColorNavy)

	// Draw outer frame
	for x := 0; x < sDims.Width; x++ {
		s.SetContent(x, 0, '─', nil, borderStyle)                           // Top edge
		s.SetContent(x, sDims.LogPanelTop, '─', nil, borderStyle)           // Middle divider
		s.SetContent(x, sDims.Height-V_BORDER_WIDTH, '─', nil, borderStyle) // Bottom edge
	}

	// Draw vertical borders for top panel
	for y := V_BORDER_WIDTH; y < sDims.LogPanelTop; y++ {
		s.SetContent(0, y, '│', nil, borderStyle)
		s.SetContent(sDims.Width-V_BORDER_WIDTH, y, '│', nil, borderStyle)
	}

	// Draw vertical borders for bottom panel and fill with navy background
	for y := sDims.LogPanelTop + 1; y < sDims.Height-1; y++ {
		s.SetContent(0, y, '│', nil, borderStyle)
		s.SetContent(sDims.Width-1, y, '│', nil, borderStyle)
		// Fill bottom panel with navy background
		for x := 1; x < sDims.Width-1; x++ {
			s.SetContent(x, y, ' ', nil, navyStyle)
		}
	}

	// Draw corners for top panel
	s.SetContent(0, 0, '┌', nil, borderStyle)
	s.SetContent(sDims.Width-1, 0, '┐', nil, borderStyle)

	// Draw corners for middle divider
	s.SetContent(0, sDims.LogPanelTop, '├', nil, borderStyle)
	s.SetContent(sDims.Width-1, sDims.LogPanelTop, '┤', nil, borderStyle)

	// Draw corners for bottom panel
	s.SetContent(0, sDims.Height-1, '└', nil, borderStyle)
	s.SetContent(sDims.Width-1, sDims.Height-1, '┘', nil, borderStyle)
}

// initializeScreen creates and initializes the tcell screen
func initializeScreen() tcell.Screen {
	s, err := tcell.NewScreen()
	if err != nil {
		Debug(fmt.Sprintf("Failed to create screen: %v", err), ERROR)
		os.Exit(1)
	}
	if err := s.Init(); err != nil {
		Debug(fmt.Sprintf("Failed to initialize screen: %v", err), ERROR)
		os.Exit(1)
	}
	s.EnableMouse()

	// Clear screen and draw border
	s.Clear()
	updateScreenDimensions(s)
	drawBorder(s)
	s.Show()

	Debug("Screen initialized with border", DEBUG)
	return s
}

// finalizeScreen properly closes the tcell screen
func finalizeScreen(s tcell.Screen) {
	s.Fini()
	Debug("Screen finalized", DEBUG)
}

// cleanShutdown performs a clean shutdown of all components
func cleanShutdown(s tcell.Screen) {
	Debug("Starting clean shutdown", DEBUG)
	
	// Signal screenshot loop to stop
	select {
	case stopScreenshots <- true:
		Debug("Sent stop signal to screenshot loop", DEBUG)
	default:
		Debug("Screenshot loop already stopping", DEBUG)
	}
	
	// Wait for screenshot loop to finish with timeout
	done := make(chan struct{})
	go func() {
		shutdownWg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		Debug("Screenshot loop stopped cleanly", DEBUG)
	case <-time.After(2 * time.Second):
		Debug("Timeout waiting for screenshot loop to stop", ERROR)
	}
	
	// Close the screen
	finalizeScreen(s)
	
	// Close gRPC connection if it exists
	if grpcConn != nil {
		grpcConn.Close()
		Debug("gRPC connection closed", DEBUG)
	}

	// Stop the server if we launched it
	stopServer()

	fmt.Println("Terminal restored.")
}

// setupSignalHandling sets up handlers for system signals
func setupSignalHandling(s tcell.Screen) {
	Debug("Initializing signal handling", DEBUG)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalChan
		Debug(fmt.Sprintf("Received signal: %v", sig), INFO)
		cleanShutdown(s)
		os.Exit(0)
	}()
	Debug("Signal handlers established", DEBUG)
}

// initializeCursor sets up the initial cursor position
func initializeCursor() {
	cursor = Cursor{
		x:         sDims.Width / 2,
		y:         sDims.Height / 2,
		visible:   true,
		blinkOn:   true,
		lastBlink: time.Now(),
	}
}

// connectToGRPCServer connects to the gRPC server
func connectToGRPCServer() error {
	var target string
	
	// Determine connection type
	if cfg.ServerAddr != "" {
		// TCP connection
		if cfg.ServerAddr == "tcp" {
			// Just --tcp flag without address, use default
			target = "localhost:50051"
		} else {
			// --tcp with specific address
			target = cfg.ServerAddr
		}
		Debug(fmt.Sprintf("Connecting to gRPC server via TCP at %s", target), DEBUG)
	} else {
		// Unix domain socket (default)
		target = "unix:///tmp/termium.sock"
		Debug("Connecting to gRPC server via Unix domain socket at /tmp/termium.sock", DEBUG)
	}

	var err error
	// As of 1.63, the Dial() function family is deprecated in favor of
	//   NewClient()
	grpcConn, err = grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		Debug(fmt.Sprintf("gRPC connection failed: %v", err), ERROR)
		return fmt.Errorf("failed to connect: %v", err)
	}
	grpcClient = pb.NewBrowserControlClient(grpcConn)
	Debug("Successfully connected to gRPC server", INFO)
	return nil
}

// openNewTab calls the openTab method on the server
func openNewTab() error {
	_, err := grpcClient.OpenTab(context.Background(), &pb.Empty{})
	if err != nil {
		return fmt.Errorf("failed to open new tab: %v", err)
	}
	Debug("Opened new tab on the browser", DEBUG)

	// Set initial viewport size after connecting
	if err := updateViewportSize(); err != nil {
		Debug(fmt.Sprintf("Failed to set initial viewport size: %v", err), ERROR)
	}

	return nil
}

// updateViewportSize sends the current viewport dimensions to the server
func updateViewportSize() error {
	Debug(fmt.Sprintf("Updating viewport size to %dx%d pixels", sDims.InnerWidthPx, sDims.InnerHeightPx), DEBUG)
	_, err := grpcClient.SetViewport(context.Background(), &pb.ViewportSize{
		Width:  int32(sDims.InnerWidthPx),
		Height: int32(sDims.InnerHeightPx),
	})
	if err != nil {
		return fmt.Errorf("failed to update viewport size: %v", err)
	}
	Debug("Successfully updated viewport size", DEBUG)
	return nil
}

// screenshotLoop handles the screenshot stream from the server
func screenshotLoop(s tcell.Screen) {
	defer shutdownWg.Done()
	
	// Store screen reference for dialog system
	dialogScreen = s
	
	// Start the dialog stream first
	if err := startDialogStream(grpcClient); err != nil {
		Debug(fmt.Sprintf("Failed to start dialog stream: %v", err), ERROR)
		// Continue anyway - dialogs will just auto-accept on server
	}
	
	// Start the streaming RPC
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Kitty uses PNG passthrough — request PNG format and higher FPS
	screenshotFps := int32(24)
	screenshotFormat := ""
	if cfg.Renderer == "kitty" {
		screenshotFps = 30
		screenshotFormat = "png"
		Debug(fmt.Sprintf("Kitty renderer: requesting PNG at %d FPS", screenshotFps), INFO)
	}

	stream, err := grpcClient.StreamScreenshots(ctx, &pb.ScreenshotRequest{
		Fps:    screenshotFps,
		Format: screenshotFormat,
	})
	if err != nil {
		Debug(fmt.Sprintf("Failed to start screenshot stream: %v", err), ERROR)
		return
	}
	
	// Create frame buffer for triple buffering
	frameBuffer := NewFrameBuffer()
	
	// Start receiver goroutine
	go func() {
		for {
			resp, err := stream.Recv()
			if err != nil {
				Debug(fmt.Sprintf("Stream receive error: %v", err), ERROR)
				return
			}
			
			// Write to the current write frame
			frame := frameBuffer.GetWriteFrame()
			frame.Data = resp.Data
			frame.Timestamp = time.Now()
			
			// Swap to make it ready for display
			frameBuffer.SwapWriteFrame()
		}
	}()
	
	// Display loop
	for {
		select {
		case <-stopScreenshots:
			Debug("Screenshot loop stopped", INFO)
			cancel()
			return
		default:
			// Try to get the latest frame (non-blocking)
			frame := frameBuffer.GetDisplayFrame()
			if frame != nil && len(frame.Data) > 0 {
				if err := displayFrame(s, frame, frameBuffer); err != nil {
					Debug(fmt.Sprintf("Error displaying frame: %v", err), ERROR)
				}
			} else {
				// No new frame, wait a bit
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
}

func clearDrawingArea(s tcell.Screen) {
	// Clear the drawing area (viewport area between borders)
	/*	for y := V_BORDER_WIDTH; y < sDims.LogPanelTop; y++ {
		for x := H_BORDER_WIDTH; x < sDims.InnerWidth+H_BORDER_WIDTH; x++ {
			s.SetContent(x, y, ' ', nil, tcell.StyleDefault)
		}
	}*/
}

// displayFrame displays a frame from the buffer
func displayFrame(s tcell.Screen, frame *Frame, fb *FrameBuffer) error {
	frameStart := time.Now()
	var decodeTime, displayTime, renderTime time.Duration
	
	// Kitty PNG passthrough: skip all decode/encode, send PNG bytes directly
	if cfg.Renderer == "kitty" {
		displayStart := time.Now()
		if err := displayWithKittyPNG(frame.Data); err != nil {
			Debug(fmt.Sprintf("Error displaying Kitty frame: %v", err), ERROR)
			return err
		}
		displayTime = time.Since(displayStart)

		// Timing output is handled inside displayWithKittyPNG (per-frame detail)
		// and here (frame-level stats) — same pattern as sixel path below.
		if cfg.ShowTimings {
			totalTime := time.Since(frameStart)
			received, displayed, dropped := fb.GetStats()
			fmt.Fprintf(os.Stderr, "Frame timings: Total=%v Display=%v | Received=%d Displayed=%d Dropped=%d\n",
				totalTime, displayTime, received, displayed, dropped)
			os.Stderr.Sync()
		}
		return nil
	}

	// Only save debug screenshots if flag is enabled
	if cfg.SaveScreenshots {
		// Save the raw bytes first (JPEG now)
		rawImageFile, err := os.Create(fmt.Sprintf("RawImage%03d.jpg", lastImageNumber))
		if err != nil {
			Debug(fmt.Sprintf("Error creating raw image file: %v", err), ERROR)
		} else {
			rawImageFile.Write(frame.Data)
			rawImageFile.Close()
		}
	}

	// Measure decode time
	decodeStart := time.Now()
	img, err := jpeg.Decode(bytes.NewReader(frame.Data))
	if err != nil {
		Debug(fmt.Sprintf("Error decoding screenshot JPEG: %v", err), ERROR)
		return err
	}
	decodeTime = time.Since(decodeStart)

	// Only save decoded image if flag is enabled
	if cfg.SaveScreenshots {
		// Save the decoded image as JPEG
		outputFile, err := os.Create(fmt.Sprintf("Image%03d.jpg", lastImageNumber))
		if err != nil {
			Debug(fmt.Sprintf("Error creating output file: %v", err), ERROR)
		} else {
			if err := jpeg.Encode(outputFile, img, &jpeg.Options{Quality: 90}); err != nil {
				Debug(fmt.Sprintf("Error encoding JPEG: %v", err), ERROR)
			}
			outputFile.Close()
		}
		lastImageNumber++
	}

	screenshotMutex.Lock()
	// Reuse imageBuffer if dimensions haven't changed
	newBounds := img.Bounds()
	if imageBuffer == nil || imageBuffer.Bounds() != newBounds {
		imageBuffer = image.NewRGBA(newBounds)
	}
	draw.Draw(imageBuffer, newBounds, img, image.Point{0, 0}, draw.Src)
	imageBounds = newBounds
	screenshotMutex.Unlock()

	// Update the display
	//   Only do it on first draw or after resize.  Otherwise images from the server should be the same size
	if firstDraw {
		clearDrawingArea(s)
	}
	firstDraw = false

	// Measure display time
	displayStart := time.Now()
	if err := displayImageBuffer(s); err != nil {
		Debug(fmt.Sprintf("Error displaying image buffer: %v", err), ERROR)
		return err
	}
	displayTime = time.Since(displayStart)

	// Draw dialog if active (on top of everything)
	dialogLock.Lock()
	if currentDialog != nil && currentDialog.Active {
		currentDialog.Draw(s)
	}
	dialogLock.Unlock()
	
	// Measure render time (Show)
	renderStart := time.Now()
	s.Show()
	renderTime = time.Since(renderStart)

	// Print timing info if requested
	if cfg.ShowTimings {
		totalTime := time.Since(frameStart)
		
		// Get frame buffer stats
		received, displayed, dropped := fb.GetStats()
		
		fmt.Fprintf(os.Stderr, "Frame timings: Total=%v Decode=%v Display=%v Show=%v | Stats: Received=%d Displayed=%d Dropped=%d\n",
			totalTime, decodeTime, displayTime, renderTime, received, displayed, dropped)
		os.Stderr.Sync() // Force flush stderr
	}

	return nil
}

// runMainLoop runs the main event loop of the application
func runMainLoop(s tcell.Screen) error {
	Debug("Entering main event loop", DEBUG)
	for {
		ev := s.PollEvent()
		
		// Check if dialog is active and should handle this event
		if handleDialogInput(ev) {
			continue // Dialog consumed the event
		}
		
		switch ev := ev.(type) {
		case *tcell.EventResize:
			Debug("Screen resize event detected", DEBUG)
			handleResize(s)
		case *tcell.EventKey:
			if shouldExit := keyboardHandler.HandleKeyEvent(s, ev); shouldExit {
				Debug("Exiting main loop", DEBUG)
				return nil
			}
		case *tcell.EventMouse:
			handleMouseEvent(s, ev)
		case *tcell.EventInterrupt:
			// Check if exit was requested
			if keyboardHandler.IsExitRequested() {
				Debug("Exiting after user confirmation", INFO)
				return nil
			}
			handleInterrupt(s)
		}
	}
}

// updateScreenDimensions updates the screen dimensions struct
func updateScreenDimensions(s tcell.Screen) {
	width, height := s.Size()
	sDims = ScreenDimensions{
		Width:           width,
		Height:          height,
		LogHeight:       LOG_PANEL_HEIGHT,
		ViewHeight:      height - LOG_PANEL_HEIGHT,
		LogPanelTop:     height - LOG_PANEL_HEIGHT,
		InnerWidth:      width - (2 * H_BORDER_WIDTH),
		InnerViewHeight: height - LOG_PANEL_HEIGHT - (2 * V_BORDER_WIDTH),
		InnerWidthPx:    (width - (2 * H_BORDER_WIDTH)) * charSize.Width,
		InnerHeightPx:   (height - LOG_PANEL_HEIGHT - (2 * V_BORDER_WIDTH)) * charSize.Height,
	}
	Debug(fmt.Sprintf("Screen dimensions updated: %+v", sDims), DEBUG)
}

// handleResize handles screen resize events
func handleResize(s tcell.Screen) {
	Debug("Resize event", DEBUG)
	s.Clear()
	updateScreenDimensions(s)

	// Update server with new viewport size
	if err := updateViewportSize(); err != nil {
		Debug(fmt.Sprintf("Failed to update viewport size after resize: %v", err), ERROR)
	}

	drawBorder(s)
	
	// Redraw the bottom panel (status bar)
	displayBottomPanel(s)
	
	// Draw dialog if active
	dialogLock.Lock()
	if currentDialog != nil && currentDialog.Active {
		currentDialog.Draw(s)
	}
	dialogLock.Unlock()
	
	s.Show()
	clearDrawingArea(s)
	s.Sync()
	// No need to redisplay static content, as the screenshot will be updated by the goroutine
}

// handleInterrupt handles interrupt events for updating the display
func handleInterrupt(s tcell.Screen) {
	blinkCursor(s)
	displayMouseInfo(s)
}

// handleMouseEvent handles mouse events
func handleMouseEvent(s tcell.Screen, ev *tcell.EventMouse) {
	x, y := ev.Position()
	currentMouse.CharX = x
	currentMouse.CharY = y
	currentMouse.PixelX = (x - H_BORDER_WIDTH) * charSize.Width
	currentMouse.PixelY = (y - V_BORDER_WIDTH) * charSize.Height

	displayMouseInfo(s)

	// Handle mouse click
	button := ev.Buttons()
	if button&tcell.Button1 != 0 {
		go sendMouseClick(currentMouse.PixelX, currentMouse.PixelY)
	}
}

// sendMouseClick sends a mouse click event to the server
func sendMouseClick(x, y int) {
	Debug(fmt.Sprintf("Sending mouse click at (%d, %d)", x, y), DEBUG)
	_, err := grpcClient.ClickMouse(context.Background(), &pb.Coordinate{X: int32(x), Y: int32(y)})
	if err != nil {
		Debug(fmt.Sprintf("Failed to send mouse click: %v", err), ERROR)
	}
}


func handleLocalKeyEvent(ev *tcell.EventKey) MenuAction {
	// Check for Ctrl+Key combinations first
	if ev.Modifiers()&tcell.ModCtrl != 0 {
		switch ev.Key() {
		case tcell.KeyCtrlC:
			return MenuExit
		}
	}

	// Handle regular keys
	switch ev.Key() {
	case tcell.KeyEscape:
		return MenuBack
	case tcell.KeyEnter:
		return MenuSelect
	case tcell.KeyUp:
		return MenuContinue
	case tcell.KeyDown:
		return MenuContinue
	case tcell.KeyLeft:
		return MenuContinue
	case tcell.KeyRight:
		return MenuContinue
	}

	// Handle printable characters if needed
	if ev.Rune() != 0 {
		switch ev.Rune() {
		case 'q', 'Q':
			return MenuExit
		}
	}

	return MenuNone
}

// detectTerminalAndCalibrate detects the terminal type, auto-detects the
// renderer if needed, and calibrates the character size.
func detectTerminalAndCalibrate() {
	termType := os.Getenv("TERM")
	Debug(fmt.Sprintf("Terminal type: %s", termType), DEBUG)

	// Auto-detect renderer if not explicitly set
	if cfg.Renderer == "auto" {
		cfg.Renderer = detectRenderer()
		Debug(fmt.Sprintf("Auto-detected renderer: %s", cfg.Renderer), INFO)
	}

	if strings.HasPrefix(termType, "xterm") || strings.Contains(termType, "256color") {
		Debug("xterm-compatible terminal detected. Attempting to calibrate.", DEBUG)
		if err := calibrateXterm(); err != nil {
			Debug(fmt.Sprintf("Terminal calibration failed: %v", err), WARN)
			Debug("Falling back to default character size", INFO)
			setDefaultCharSize()
		}
	} else {
		Debug("Non-xterm terminal detected, using defaults", DEBUG)
		setDefaultCharSize()
	}
}

// detectRenderer probes the terminal to determine the best graphics protocol.
// Tries Kitty first (query with a 1x1 pixel image), falls back to sixel.
func detectRenderer() string {
	// Try Kitty graphics query: send a 1x1 transparent pixel and check for OK response
	// \033_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\033\\
	// If the terminal supports Kitty, it responds with \033_Gi=31;OK\033\\
	kittyQuery := "\033_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\033\\"

	response, err := queryTerminalWithTimeout(kittyQuery, 500)
	if err != nil {
		Debug(fmt.Sprintf("Kitty detection query failed: %v", err), DEBUG)
		return "sixel"
	}

	Debug(fmt.Sprintf("Kitty detection response: %q", response), DEBUG)

	if strings.Contains(response, "OK") {
		Debug("Terminal supports Kitty graphics protocol", INFO)
		return "kitty"
	}

	Debug("Terminal does not support Kitty, defaulting to sixel", DEBUG)
	return "sixel"
}

// queryTerminalWithTimeout sends a query and reads the response with a timeout in ms.
// Unlike queryTerminal, this won't block forever if the terminal doesn't respond.
func queryTerminalWithTimeout(query string, timeoutMs int) (string, error) {
	_, err := fmt.Fprint(os.Stdout, query)
	if err != nil {
		return "", err
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Set read deadline so we don't block forever if terminal doesn't respond
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	os.Stdin.SetReadDeadline(deadline)
	defer os.Stdin.SetReadDeadline(time.Time{}) // clear deadline

	response := make([]byte, 64)
	n, err := os.Stdin.Read(response)
	if err != nil {
		return "", err
	}

	return string(response[:n]), nil
}

// calibrateXterm calibrates the character size for xterm-compatible terminals
func calibrateXterm() error {
	Debug("Starting terminal calibration", DEBUG)

	charResponse, err := queryTerminal("\033[18t")
	if err != nil {
		Debug(fmt.Sprintf("Terminal character query failed: %v", err), ERROR)
		return fmt.Errorf("terminal query failed: %v", err)
	}
	Debug(fmt.Sprintf("Raw character response: %q", charResponse), DEBUG)

	pixelResponse, err := queryTerminal("\033[14t")
	if err != nil {
		Debug(fmt.Sprintf("Terminal pixel query failed: %v", err), ERROR)
		return fmt.Errorf("pixel query failed: %v", err)
	}
	Debug(fmt.Sprintf("Raw pixel response: %q", pixelResponse), DEBUG)

	var charRows, charCols, pixelHeight, pixelWidth int
	_, err = fmt.Sscanf(charResponse, "\033[8;%d;%dt", &charRows, &charCols)
	if err != nil {
		Debug(fmt.Sprintf("Failed to parse character dimensions: %v", err), ERROR)
		return fmt.Errorf("parse error: %v", err)
	}
	Debug(fmt.Sprintf("Parsed character dimensions - Rows: %d, Cols: %d", charRows, charCols), DEBUG)

	_, err = fmt.Sscanf(pixelResponse, "\033[4;%d;%dt", &pixelHeight, &pixelWidth)
	if err != nil {
		Debug(fmt.Sprintf("Failed to parse pixel dimensions: %v", err), ERROR)
		return fmt.Errorf("parse error: %v", err)
	}
	Debug(fmt.Sprintf("Parsed pixel dimensions - Height: %d, Width: %d", pixelHeight, pixelWidth), DEBUG)

	// Check for zero values to avoid division by zero
	if charRows == 0 || charCols == 0 {
		Debug("Invalid character dimensions (zero values detected)", ERROR)
		return fmt.Errorf("invalid character dimensions")
	}

	charSize.Width = pixelWidth / charCols
	charSize.Height = pixelHeight / charRows

	Debug(fmt.Sprintf("Calibrated character size: %dx%d pixels", charSize.Width, charSize.Height), INFO)

	// Sanity check the results
	if charSize.Width < 1 || charSize.Height < 1 {
		Debug(fmt.Sprintf("Unreasonable character size calculated: %dx%d", charSize.Width, charSize.Height), ERROR)
		return fmt.Errorf("unreasonable character size calculated")
	}

	return nil
}

// queryTerminal sends a query to the terminal and returns the response
func queryTerminal(query string) (string, error) {
	_, err := fmt.Fprint(os.Stdout, query)
	if err != nil {
		return "", err
	}

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	response := make([]byte, 32)
	n, err := os.Stdin.Read(response)
	if err != nil {
		return "", err
	}

	return string(response[:n]), nil
}

// setDefaultCharSize sets default character size when calibration fails
func setDefaultCharSize() {
	charSize = CharSize{Width: 8, Height: 16}
	Debug("Using default character size: 8x16 pixels", DEBUG)
}

// Displays the image buffer using either sixel or character-based rendering within tcell's framework
func displayImageBuffer(s tcell.Screen) error {
	if s == nil || imageBuffer == nil {
		return fmt.Errorf("invalid screen or image buffer")
	}

	startTime := time.Now()
	defer func() {
		Debug(fmt.Sprintf("Total displayImageBuffer time: %v", time.Since(startTime)), INFO)
	}()

	screenshotMutex.Lock()
	defer screenshotMutex.Unlock()

	// Calculate the maximum available space for the image, respecting borders
	maxWidth := sDims.Width - (2 * H_BORDER_WIDTH)
	maxHeight := sDims.LogPanelTop - (2 * V_BORDER_WIDTH)
	maxWidthPx := maxWidth * charSize.Width
	maxHeightPx := maxHeight * charSize.Height

	// Scale image to fit available space
	scaledImage := scaleImage(imageBuffer, maxWidthPx, maxHeightPx)

	switch cfg.Renderer {
	case "kitty":
		return displayWithKittyRGBA(scaledImage)
	case "tcell":
		return displayWithTcell(s, scaledImage)
	default: // "sixel"
		// Use band-based optimization for websafe palette
		if cfg.Palette == "websafe" {
			return displayWithSixelBands(scaledImage)
		}
		return displayWithSixel(scaledImage)
	}
}

// Scales image efficiently using shared logic
func scaleImage(src *image.RGBA, targetWidth, targetHeight int) *image.RGBA {
	srcWidth := src.Bounds().Dx()
	srcHeight := src.Bounds().Dy()

	// Only scale if the image is larger than target dimensions
	if srcWidth <= targetWidth && srcHeight <= targetHeight {
		Debug("Image fits within target dimensions, no scaling needed", DEBUG)
		return src
	}

	scaleX := float64(targetWidth) / float64(srcWidth)
	scaleY := float64(targetHeight) / float64(srcHeight)
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}

	// Only scale down, never up
	if scale >= 1.0 {
		Debug("Scale factor >= 1.0, no scaling needed", DEBUG)
		return src
	}

	newWidth := int(float64(srcWidth) * scale)
	newHeight := int(float64(srcHeight) * scale)

	Debug(fmt.Sprintf("Scaling image down to: %dx%d", newWidth, newHeight), DEBUG)
	
	// Reuse scaledBuffer if dimensions match, otherwise allocate new
	newBounds := image.Rect(0, 0, newWidth, newHeight)
	if scaledBuffer == nil || scaledBuffer.Bounds() != newBounds {
		scaledBuffer = image.NewRGBA(newBounds)
	}
	
	draw.ApproxBiLinear.Scale(scaledBuffer, scaledBuffer.Bounds(), src, src.Bounds(), draw.Over, nil)

	return scaledBuffer
}

// displayWithSixelBands uses band-level caching for optimized sixel encoding
func displayWithSixelBands(img *image.RGBA) error {
	sixelStart := time.Now()
	
	rgbaLock.Lock()
	defer rgbaLock.Unlock()
	
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	
	// Initialize or recreate if dimensions changed
	if bandManager == nil || width != bandManager.Width || height != bandManager.Height {
		bandManager = NewBandManager(width, height)
		currentRGBA = image.NewRGBA(image.Rect(0, 0, width, height))
		previousRGBA = image.NewRGBA(image.Rect(0, 0, width, height))
		Debug(fmt.Sprintf("Initialized band manager and RGBA buffers (%dx%d)", width, height), INFO)
	}
	
	// Copy image data to current buffer (reuses the allocated buffer)
	draw.Draw(currentRGBA, currentRGBA.Bounds(), img, image.Point{}, draw.Src)
	
	// Detect dirty bands by comparing current frame with previous frame
	// DetectDirtyBands compares the new frame against stored hashes
	bandManager.DetectDirtyBands(currentRGBA)
	
	dirtyCount := bandManager.GetDirtyBandCount()
	if cfg.ShowTimings {
		fmt.Fprintf(os.Stderr, "Dirty bands: %d/%d (%.1f%%)\n", 
			dirtyCount, bandManager.NumBands, 
			float64(dirtyCount)*100.0/float64(bandManager.NumBands))
	}
	
	// Only encode dirty bands and reuse cached bands
	if dirtyCount > 0 {
		encodeStart := time.Now()
		
		// Determine palette type from config
		var paletteType sixel.PaletteType
		switch cfg.Palette {
		case "websafe":
			paletteType = sixel.PaletteWebSafe
		case "plan9":
			paletteType = sixel.PalettePlan9
		default:
			paletteType = sixel.PaletteAdaptive
		}
		
		// Create a band encoder
		bandEncoder := NewBandEncoder(paletteType, bandManager.Width, bandManager.NumBands*6)
		
		// Process each band
		bandStrings := make([]string, bandManager.NumBands)
		for i := range bandManager.Bands {
			band := &bandManager.Bands[i]
			
			if band.IsDirty {
				// Encode this dirty band
				encodedBand, err := bandEncoder.EncodeBand(currentRGBA, band.Y, band.Height)
				if err != nil {
					return err
				}
				
				// Cache the encoded string
				band.CachedRLE = encodedBand
				band.IsDirty = false
				// Update the hash for this band
				band.Hash = HashBand(currentRGBA, band.Y, band.Height, bandManager.Width)
			}
			
			// Use the cached string (either newly encoded or previously cached)
			bandStrings[i] = band.CachedRLE
		}
		
		// Compose the full sixel output from all bands
		var pal color.Palette
		switch paletteType {
		case sixel.PaletteWebSafe:
			pal = palette.WebSafe
		case sixel.PalettePlan9:
			pal = palette.Plan9
		default:
			pal = nil
		}
		
		fullSixel := ComposeFullSixel(bandStrings, bandManager.Width, bandManager.NumBands*6, pal)
		
		// Position cursor at the top-left of the usable area (after borders)
		fmt.Printf("\033[%d;%dH", V_BORDER_WIDTH+1, H_BORDER_WIDTH+1)
		
		// Save cursor position before sixel output
		fmt.Print("\033[s")
		
		// Write the composed sixel to stdout
		if _, err := os.Stdout.WriteString(fullSixel); err != nil {
			return err
		}
		
		// Restore cursor position after sixel output
		fmt.Print("\033[u")
		
		if cfg.ShowTimings {
			fmt.Fprintf(os.Stderr, "  Band encode time: %v (encoded %d dirty bands)\n", 
				time.Since(encodeStart), dirtyCount)
		}
	} else {
		// No dirty bands - recompose from all cached bands
		bandStrings := make([]string, bandManager.NumBands)
		for i := range bandManager.Bands {
			bandStrings[i] = bandManager.Bands[i].CachedRLE
		}
		
		// Determine palette type from config (same as above)
		var pal color.Palette
		switch cfg.Palette {
		case "websafe":
			pal = palette.WebSafe
		case "plan9":
			pal = palette.Plan9
		default:
			pal = nil
		}
		
		fullSixel := ComposeFullSixel(bandStrings, bandManager.Width, bandManager.NumBands*6, pal)
		
		// Position cursor at the top-left of the usable area (after borders)
		fmt.Printf("\033[%d;%dH", V_BORDER_WIDTH+1, H_BORDER_WIDTH+1)
		
		// Save cursor position before sixel output
		fmt.Print("\033[s")
		
		// Write the composed sixel to stdout
		if _, err := os.Stdout.WriteString(fullSixel); err != nil {
			return err
		}
		
		// Restore cursor position after sixel output
		fmt.Print("\033[u")
		
		if cfg.ShowTimings {
			fmt.Fprintf(os.Stderr, "  No encoding needed - all bands clean!\n")
		}
	}
	
	// Swap buffers for next frame (pointer swap, no copy)
	currentRGBA, previousRGBA = previousRGBA, currentRGBA
	
	Debug(fmt.Sprintf("Band-based display took %v total", time.Since(sixelStart)), INFO)
	return nil
}

// displayWithSixel uses the Go sixel library
func displayWithSixel(img *image.RGBA) error {
	sixelStart := time.Now()
	
	buf := bufio.NewWriter(os.Stdout)
	defer buf.Flush() // Ensures all data is written before function returns

	bounds := img.Bounds()
	if bounds.Dx() > sDims.InnerWidthPx || bounds.Dy() > sDims.InnerHeightPx {
		Debug(fmt.Sprintf("Image dimensions %dx%d exceed available space %dx%d",
			bounds.Dx(), bounds.Dy(),
			sDims.InnerWidthPx, sDims.InnerHeightPx), WARN)
	}

	// Position cursor at the top-left of the usable area (after borders)
	// Add 1 to border width because terminal coordinates are 1-based
	fmt.Printf("\033[%d;%dH", V_BORDER_WIDTH+1, H_BORDER_WIDTH+1)

	// Save cursor position before sixel output
	fmt.Print("\033[s")

	// Initialize encoder once on first use
	sixelEncoderMutex.Lock()
	if sixelEncoder == nil {
		sixelEncoder = sixel.NewEncoder(os.Stdout)
		sixelEncoder.Dither = false  // Disable dithering for speed
		
		// Set palette based on config
		switch cfg.Palette {
		case "websafe":
			sixelEncoder.Palette = sixel.PaletteWebSafe
		case "plan9":
			sixelEncoder.Palette = sixel.PalettePlan9
		default: // "adaptive"
			sixelEncoder.Palette = sixel.PaletteAdaptive
		}
		
		// TODO: When adding support for other protocols (Kitty, iTerm2, etc),
		// adjust color depth based on protocol capabilities:
		// - Sixel: 256 colors max
		// - Kitty: 24-bit true color support
		// - iTerm2: 24-bit true color support
		Debug("Created sixel encoder (one-time initialization)", INFO)
	}
	
	// Update dimensions for this frame
	sixelEncoder.Width = img.Bounds().Dx()
	sixelEncoder.Height = img.Bounds().Dy()
	sixelEncoderMutex.Unlock()

	// Encode the image
	encodeStart := time.Now()
	if err := sixelEncoder.Encode(img); err != nil {
		Debug(fmt.Sprintf("Sixel encoding error: %v", err), ERROR)
		return fmt.Errorf("sixel encoding error: %v", err)
	}
	
	if cfg.ShowTimings {
		// Get cache stats if using fixed palette
		hits, misses, hitRate := sixelEncoder.GetCacheStats()
		if hits > 0 || misses > 0 {
			fmt.Fprintf(os.Stderr, "Cache stats: hits=%d misses=%d (%.1f%% hit rate)\n", 
				hits, misses, hitRate)
		}
		
		fmt.Fprintf(os.Stderr, "  Sixel encode time: %v (rendered size: %dx%d pixels)\n", 
			time.Since(encodeStart), img.Bounds().Dx(), img.Bounds().Dy())
		os.Stderr.Sync() // Force flush stderr
	}

	// Restore cursor position
	fmt.Print("\033[u")

	Debug(fmt.Sprintf("Displayed sixel image at (%d,%d) with size %dx%d (took %v)",
		H_BORDER_WIDTH, V_BORDER_WIDTH,
		img.Bounds().Dx(), img.Bounds().Dy(),
		time.Since(sixelStart)), DEBUG)

	return nil
}


// Displays log messages in the bottom panel with navy background
func displayBottomPanel(s tcell.Screen) error {
	baseStyle := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorNavy)

	// First clear the entire bottom panel (respect borders)
	for y := sDims.LogPanelTop + 1; y < sDims.Height-1; y++ {
		for x := H_BORDER_WIDTH; x < sDims.Width-H_BORDER_WIDTH; x++ {
			s.SetContent(x, y, ' ', nil, baseStyle)
		}
	}

	logBuffer.mutex.Lock()
	defer logBuffer.mutex.Unlock()

	// Calculate how many messages we can display (account for top and bottom borders)
	displayLines := sDims.LogHeight - 2 // Subtract 2 for top and bottom borders
	startIdx := 0
	if len(logBuffer.messages) > displayLines {
		startIdx = len(logBuffer.messages) - displayLines
	}

	// Display messages
	for i := 0; i < displayLines && startIdx+i < len(logBuffer.messages); i++ {
		message := logBuffer.messages[startIdx+i]

		// Truncate message if it's too long
		if len(message) > sDims.InnerWidth {
			message = message[:sDims.InnerWidth-3] + "..."
		}

		// Write the message
		y := sDims.LogPanelTop + 1 + i // Add 1 to start after the top border
		for x, ch := range message {
			if x >= sDims.InnerWidth {
				break
			}
			// Skip any control characters
			if ch < 32 || ch == 127 {
				continue
			}
			s.SetContent(x+H_BORDER_WIDTH, y, ch, nil, baseStyle)
		}
	}

	return nil
}

// Displays current mouse coordinate information on top of the bottom border
func displayMouseInfo(s tcell.Screen) {
	style := tcell.StyleDefault.Foreground(tcell.ColorYellow).Background(tcell.ColorNavy)

	// Calculate maximum width needed for coordinates (assuming max 4 digits per number)
	// Format: "Mouse Pixel: (XXXX, XXXX), Mouse Char: (XXXX, XXXX)" = 47 chars
	maxWidth := 47

	// Clear only the area we need; start drwaing at 3
	for x := 3; x < maxWidth+3 && x < sDims.Width-2*H_BORDER_WIDTH; x++ {
		s.SetContent(x+H_BORDER_WIDTH, sDims.Height, ' ', nil, style)
	}

	// Format and display the coordinate information
	info := fmt.Sprintf("Mouse Pixel: (%4d, %4d), Mouse Char: (%4d, %4d)",
		currentMouse.PixelX,
		currentMouse.PixelY,
		currentMouse.CharX,
		currentMouse.CharY)

	for x, ch := range info {
		if x+3+H_BORDER_WIDTH < sDims.Width-H_BORDER_WIDTH {
			s.SetContent(x+3+H_BORDER_WIDTH, sDims.Height, ch, nil, style)
		}
	}

	s.Show()
}

// Displays usage instructions in the bottom panel
func displayInstructions(s tcell.Screen) {
	message := "Use arrow keys to move cursor. Mouse over image for coordinates. Press ESC or Ctrl+C to exit"
	logBuffer.Write([]byte(message))
	displayBottomPanel(s)
}

// Displays a cool graphic as a splash screen
func showSplashScreen(s tcell.Screen, splashPath string) error {
	// Load and decode the splash image
	var img image.Image
	var err error

	if splashPath == "" {
		// Use embedded image
		reader := bytes.NewReader(embeddedSplashImage)
		img, err = jpeg.Decode(reader)
		if err != nil {
			return fmt.Errorf("failed to decode embedded splash image: %v", err)
		}
	} else {
		// Use specified file
		file, err := os.Open(splashPath)
		if err != nil {
			return fmt.Errorf("failed to open splash image: %v", err)
		}
		defer file.Close()

		img, err = jpeg.Decode(file)
		if err != nil {
			return fmt.Errorf("failed to decode splash image: %v", err)
		}
	}

	// Convert the image to RGBA format
	bounds := img.Bounds()
	Debug(fmt.Sprintf("Original image dimensions: %dx%d", bounds.Dx(), bounds.Dy()), DEBUG)

	// Explore RGBA64 at some point and see if we can improve the image quality
	allocStart := time.Now()
	rgbaImg := image.NewRGBA(bounds)
	Debug(fmt.Sprintf("RGBA allocation took: %v", time.Since(allocStart)), DEBUG)

	drawStart := time.Now()
	draw.Draw(rgbaImg, bounds, img, bounds.Min, draw.Src)
	drawTime := time.Since(drawStart)
	Debug(fmt.Sprintf("draw.Draw() operation took: %v", drawTime), DEBUG)

	// Set the global imageBuffer
	screenshotMutex.Lock()
	imageBuffer = rgbaImg
	imageBounds = imageBuffer.Bounds()
	screenshotMutex.Unlock()

	// Clear the viewport area first
	clearDrawingArea(s)
	// TODO: Uncomment this.:
	// Display initial image
	if err := displayImageBuffer(s); err != nil {
		return fmt.Errorf("failed to display splash image: %v", err)
	}

	logBuffer.Write([]byte("Press Enter to continue..."))
	displayBottomPanel(s)
	s.Show()

	// Event loop
	for {
		ev := s.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventKey:
			Debug(fmt.Sprintf("Splash screen received key event: %v", ev.Key()), DEBUG)
			action := handleLocalKeyEvent(ev)
			switch action {
			case MenuExit:
				return fmt.Errorf("user requested exit")
			case MenuSelect, MenuContinue:
				return nil
			case MenuBack:
				return nil
			}
		case *tcell.EventResize:
			updateScreenDimensions(s)
			if err := displayImageBuffer(s); err != nil {
				return fmt.Errorf("failed to redisplay splash image after resize: %v", err)
			}
			Debug(fmt.Sprintf("Displayed imge %v by %v", imageBuffer.Bounds().Size().X, imageBuffer.Bounds().Size().Y), INFO)
			displayBottomPanel(s)
			s.Show()
		}
	}
}

