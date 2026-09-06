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
	_ "image/png"
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
	"golang.org/x/sys/unix"
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
	H_BORDER_WIDTH = 1 // width in chars of all Horizontal borders
	V_BORDER_WIDTH = 1 // width in chars of all vertical borders
)

// ScreenDimensions holds the current screen dimensions and panel calculations
type ScreenDimensions struct {
	ViewTop         int
	Width           int // Total screen width
	Height          int // Total screen height
	LogHeight       int // Height of the status row
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

var imageBuffer *image.RGBA

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
	scaledBuffer *image.RGBA // Reusable buffer for scaled images
	rgbaLock     sync.Mutex
)

// Channel to signal screenshot loop to stop

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
var appCtx, appCancel = context.WithCancel(context.Background())
var latestFrame *Frame
var lastSavedFrame *Frame
var frames = NewFrameBuffer()
var graphicsHidden bool

type quitEvent struct{}
type frameEvent struct{}

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

	if err := runInteractive(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func runInteractive() error {
	detectTerminalAndCalibrate()
	s := initializeScreen()
	defer cleanShutdown(s)
	setupSignalHandling(s)
	if cfg.SplashPath != "NONE" && cfg.SplashPath != "" {
		if err := showSplashScreen(s, cfg.SplashPath); err != nil {
			return err
		}
	}
	if err := startServer(); err != nil {
		return err
	}
	if err := connectToGRPCServer(); err != nil {
		return err
	}
	if err := openNewTab(); err != nil {
		return err
	}
	keyboardHandler = NewKeyboardHandler(grpcClient)
	dialogScreen = s
	if err := startDialogStream(grpcClient); err != nil {
		return err
	}
	keyboardHandler.start(appCtx, s)
	if cfg.InitialURL != "" && cfg.InitialURL != "about:blank" {
		address, err := normalizeAddress(cfg.InitialURL)
		if err != nil {
			return err
		}
		keyboardHandler.navigate(pb.NavigationAction_NAVIGATE, address)
	}
	shutdownWg.Add(1)
	go screenshotLoop(s)
	return runMainLoop(s)
}

// Draws teal borders around both panels and sets the bottom panel background to navy
func drawBorder(s tcell.Screen) {
	w, h := s.Size()
	if w < 1 || h < 3 {
		return
	}
	style := tcell.StyleDefault.Foreground(tcell.ColorTeal)
	drawText(s, 0, 1, w, strings.Repeat("─", w), style)
	drawText(s, 0, h-2, w, strings.Repeat("─", w), style)
	for y := 2; y < h-2; y++ {
		s.SetContent(0, y, '│', nil, style)
		s.SetContent(w-1, y, '│', nil, style)
	}
}

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
	s.EnableMouse(tcell.MouseMotionEvents)
	s.EnablePaste()

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
	if cfg.Renderer == "kitty" {
		fmt.Print(kittyDelete)
	}
	s.Fini()
	Debug("Screen finalized", DEBUG)
}

// cleanShutdown performs a clean shutdown of all components
func cleanShutdown(s tcell.Screen) {
	appCancel()
	if grpcConn != nil {
		_ = grpcConn.Close()
	}
	shutdownWg.Wait()
	finalizeScreen(s)
	stopServer()
}

func setupSignalHandling(s tcell.Screen) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(signalChan)
		select {
		case <-appCtx.Done():
			return
		case <-signalChan:
			postUI(s, quitEvent{})
		}
	}()
}

// Retry important events under backpressure without touching the screen buffer.
func postUI(s tcell.Screen, value any) {
	for appCtx.Err() == nil {
		if s.PostEvent(tcell.NewEventInterrupt(value)) == nil {
			return
		}
		select {
		case <-appCtx.Done():
			return
		case <-time.After(10 * time.Millisecond):
		}
	}
}

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
	ctx, cancel := context.WithTimeout(appCtx, 25*time.Second)
	defer cancel()
	_, err := grpcClient.OpenTab(ctx, &pb.Empty{})
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
	if sDims.InnerWidthPx < 1 || sDims.InnerHeightPx < 1 {
		return nil
	}
	ctx, cancel := context.WithTimeout(appCtx, 5*time.Second)
	defer cancel()
	Debug(fmt.Sprintf("Updating viewport size to %dx%d pixels", sDims.InnerWidthPx, sDims.InnerHeightPx), DEBUG)
	_, err := grpcClient.SetViewport(ctx, &pb.ViewportSize{
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
	format := ""
	if cfg.Renderer == "kitty" {
		format = "png"
	}
	stream, err := grpcClient.StreamScreenshots(appCtx, &pb.ScreenshotRequest{Fps: 24, Format: format})
	if err != nil {
		postUI(s, stateUpdate{err: err})
		return
	}
	for {
		response, err := stream.Recv()
		if err != nil {
			if appCtx.Err() == nil {
				postUI(s, stateUpdate{err: err})
			}
			return
		}
		frames.Publish(&Frame{Data: response.Data, Generation: response.Generation, Timestamp: time.Now()})
		// Dropping a wakeup is safe: the next frame or state poll also drains the slot.
		_ = s.PostEvent(tcell.NewEventInterrupt(frameEvent{}))
	}
}

func clearDrawingArea(s tcell.Screen) { fillRect(s, viewportRect(s.Size()), tcell.StyleDefault) }

func displayFrame(s tcell.Screen, frame *Frame, fb *FrameBuffer) error {
	if frame == nil || viewportRect(s.Size()).Empty() {
		clearDrawingArea(s)
		return nil
	}
	if frame.Generation != 0 && keyboardHandler != nil && frame.Generation < keyboardHandler.state.Generation {
		clearDrawingArea(s)
		return nil
	}
	if cfg.SaveScreenshots && frame != lastSavedFrame {
		ext := "jpg"
		if cfg.Renderer == "kitty" {
			ext = "png"
		}
		if err := os.WriteFile(fmt.Sprintf("RawImage%03d.%s", lastImageNumber, ext), frame.Data, 0600); err != nil {
			return err
		}
		lastImageNumber++
		lastSavedFrame = frame
	}
	dimensions, _, err := image.DecodeConfig(bytes.NewReader(frame.Data))
	if err != nil {
		return err
	}
	if dimensions.Width != sDims.InnerWidthPx || dimensions.Height != sDims.InnerHeightPx {
		clearDrawingArea(s)
		return nil
	}
	if cfg.Renderer == "kitty" {
		return displayWithKittyPNG(frame.Data)
	}
	img, _, err := image.Decode(bytes.NewReader(frame.Data))
	if err != nil {
		return err
	}
	imageBuffer = image.NewRGBA(img.Bounds())
	draw.Draw(imageBuffer, img.Bounds(), img, img.Bounds().Min, draw.Src)
	return displayImageBuffer(s)
}
func redraw(s tcell.Screen) {
	if f := frames.GetDisplayFrame(); f != nil {
		latestFrame = f
		if f.Generation > keyboardHandler.state.Generation {
			keyboardHandler.state.Generation = f.Generation
			keyboardHandler.pointerHeld = 0
			keyboardHandler.capturePage = false
		}
	}
	overlay := keyboardHandler.hasOverlay() || currentDialog != nil
	if cfg.Renderer != "tcell" && overlay != graphicsHidden {
		if cfg.Renderer == "kitty" {
			fmt.Print(kittyDelete)
		}
		s.Clear()
		s.Sync()
		graphicsHidden = overlay
	}
	if cfg.Renderer == "tcell" || !overlay {
		if err := displayFrame(s, latestFrame, frames); err != nil {
			keyboardHandler.status = err.Error()
		}
	}
	drawBorder(s)
	keyboardHandler.Draw(s)
	if currentDialog != nil && !keyboardHandler.quitConfirm {
		s.HideCursor()
		currentDialog.Draw(s)
	}
	s.Show()
}

func runMainLoop(s tcell.Screen) error {
	redraw(s)
	for {
		ev := s.PollEvent()
		if ev == nil {
			return nil
		}
		switch ev := ev.(type) {
		case *tcell.EventResize:
			handleResize(s)
		case *tcell.EventInterrupt:
			switch value := ev.Data().(type) {
			case quitEvent:
				return nil
			case operationResult:
				keyboardHandler.result(value)
			case stateUpdate:
				if value.err != nil {
					keyboardHandler.status = value.err.Error()
				} else if !keyboardHandler.awaitingNavigation {
					keyboardHandler.applyState(value.state)
				}
			case *pb.DialogEvent:
				currentDialog = NewDialog(value)
				currentDialog.mouseDown = keyboardHandler.mouseButtons&tcell.Button1 != 0
				keyboardHandler.input(&pb.InputEvent{Kind: pb.InputKind_RESET_INPUT})
			}
		case *tcell.EventPaste:
			if !ev.Start() && currentDialog != nil {
				if keyboardHandler.pasting && currentDialog.FocusedButton == -1 {
					currentDialog.editor.insert(keyboardHandler.paste.String())
				}
				keyboardHandler.pasting = false
				keyboardHandler.paste.Reset()
			} else {
				keyboardHandler.pasteEvent(ev.Start())
			}
		case *tcell.EventKey:
			exit, handled := keyboardHandler.globalKey(ev, currentDialog != nil)
			if exit {
				return nil
			}
			if !handled {
				if keyboardHandler.pasting || keyboardHandler.quitConfirm || !handleDialogInput(ev) {
					if keyboardHandler.HandleKeyEvent(s, ev) {
						return nil
					}
				}
			}
		case *tcell.EventMouse:
			if keyboardHandler.quitConfirm || !handleDialogInput(ev) {
				keyboardHandler.HandleMouseEvent(s, ev)
			}
			if keyboardHandler.exitRequested {
				return nil
			}
		}
		redraw(s)
	}
}

func updateScreenDimensions(s tcell.Screen) {
	w, h := s.Size()
	r := viewportRect(w, h)
	sDims = ScreenDimensions{Width: w, Height: h, ViewTop: r.Min.Y, LogHeight: 1, LogPanelTop: h - 2, ViewHeight: r.Dy(), InnerWidth: r.Dx(), InnerViewHeight: r.Dy(), InnerWidthPx: r.Dx() * charSize.Width, InnerHeightPx: r.Dy() * charSize.Height}
}

func handleResize(s tcell.Screen) {
	s.Clear()
	s.Sync()
	updateScreenDimensions(s)
	latestFrame = nil
	if keyboardHandler != nil {
		keyboardHandler.sizeStatus()
		keyboardHandler.pointerHeld = 0
		keyboardHandler.capturePage = false
		keyboardHandler.input(&pb.InputEvent{Kind: pb.InputKind_RESET_INPUT})
		if sDims.InnerWidthPx > 0 && sDims.InnerHeightPx > 0 {
			keyboardHandler.queue(browserOperation{viewport: &pb.ViewportSize{Width: int32(sDims.InnerWidthPx), Height: int32(sDims.InnerHeightPx)}})
		}
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
	setDefaultCharSize()
	if cfg.Renderer == "auto" {
		cfg.Renderer = detectRenderer()
	}
	if cfg.Renderer == "tcell" {
		return
	}
	if size, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ); err == nil && size.Col > 0 && size.Row > 0 && size.Xpixel >= size.Col && size.Ypixel >= size.Row {
		charSize = CharSize{int(size.Xpixel / size.Col), int(size.Ypixel / size.Row)}
	} else if err := calibrateXterm(); err != nil {
		setDefaultCharSize()
	}
}

func detectRenderer() string {
	response, err := queryTerminalWithTimeout("\033_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\033\\", 200)
	if err == nil && strings.Contains(response, "i=31;OK") {
		return "kitty"
	}
	response, err = queryTerminalWithTimeout("\033[c", 200)
	if err == nil {
		for _, parameter := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(response, "\033[?"), "c"), ";") {
			if parameter == "4" {
				return "sixel"
			}
		}
	}
	return "tcell"
}

func queryTerminalWithTimeout(query string, timeoutMs int) (string, error) {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, old)
	if _, err = fmt.Fprint(os.Stdout, query); err != nil {
		return "", err
	}
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	response := make([]byte, 0, 128)
	for time.Now().Before(deadline) {
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, max(1, int(time.Until(deadline).Milliseconds())))
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return "", err
		}
		if n == 0 {
			break
		}
		if poll[0].Revents&unix.POLLIN == 0 {
			return "", fmt.Errorf("terminal closed")
		}
		buf := make([]byte, 128)
		n, err = unix.Read(fd, buf)
		if err != nil {
			return "", err
		}
		response = append(response, buf[:n]...)
		text := string(response)
		if strings.HasSuffix(text, "t") || strings.HasSuffix(text, "c") || strings.HasSuffix(text, "\033\\") {
			return text, nil
		}
		if len(response) > 1024 {
			break
		}
	}
	return "", fmt.Errorf("terminal query timed out")
}

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
func queryTerminal(query string) (string, error) { return queryTerminalWithTimeout(query, 200) }

func setDefaultCharSize() {
	charSize = CharSize{Width: 8, Height: 16}
	Debug("Using default character size: 8x16 pixels", DEBUG)
}

// Displays the image buffer using either sixel or character-based rendering within tcell's framework
func displayImageBuffer(s tcell.Screen) error {
	if viewportRect(s.Size()).Empty() {
		return nil
	}
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
	maxHeight := sDims.InnerViewHeight
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
		fmt.Printf("\033[%d;%dH", sDims.ViewTop+1, H_BORDER_WIDTH+1)

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
		fmt.Printf("\033[%d;%dH", sDims.ViewTop+1, H_BORDER_WIDTH+1)

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
	fmt.Printf("\033[%d;%dH", sDims.ViewTop+1, H_BORDER_WIDTH+1)

	// Save cursor position before sixel output
	fmt.Print("\033[s")

	// Initialize encoder once on first use
	sixelEncoderMutex.Lock()
	if sixelEncoder == nil {
		sixelEncoder = sixel.NewEncoder(os.Stdout)
		sixelEncoder.Dither = false // Disable dithering for speed

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
	if keyboardHandler != nil {
		keyboardHandler.Draw(s)
	}
	return nil
}

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
	screenshotMutex.Unlock()

	// Clear the viewport area first
	clearDrawingArea(s)
	// TODO: Uncomment this.:
	// Display initial image
	if err := displayImageBuffer(s); err != nil {
		return fmt.Errorf("failed to display splash image: %v", err)
	}

	drawText(s, 0, sDims.Height-1, sDims.Width, "Press Enter to continue…", tcell.StyleDefault)
	displayBottomPanel(s)
	s.Show()

	// Event loop
	for {
		ev := s.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventInterrupt:
			if _, ok := ev.Data().(quitEvent); ok {
				return fmt.Errorf("interrupted")
			}
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
