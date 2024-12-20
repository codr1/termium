package main

import (
	"bufio"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"os/signal"
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
		Debug(fmt.Sprintf("Failed to parse flags: %v", err), ERROR)
		os.Exit(1)
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
	defer finalizeScreen(s)

	initializeCursor()

	// Show splash screen and wait for user input
	Debug(fmt.Sprintf("Loading splash screen from: %s", cfg.SplashPath), DEBUG)
	if err := showSplashScreen(s, cfg.SplashPath); err != nil {
		Debug(fmt.Sprintf("Error showing splash screen: %v", err), ERROR)
		displayErrorMessage(s, fmt.Sprintf("Error showing splash screen: %v", err))
		return
	}

	// Display usage instructions after splash screen
	displayInstructions(s)

	Debug("Setting up signal handlers", DEBUG)
	setupSignalHandling(s)

	// Connect to gRPC server
	Debug(fmt.Sprintf("Connecting to server at %s", cfg.ServerAddr), INFO)
	if err := connectToGRPCServer(cfg.ServerAddr); err != nil {
		Debug(fmt.Sprintf("Failed to connect to server: %v", err), ERROR)
	}
	defer grpcConn.Close()
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
	go screenshotLoop(s)

	if err := runMainLoop(s); err != nil {
		displayErrorMessage(s, fmt.Sprintf("Error in     main loop: %v", err))
	}
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

// setupSignalHandling sets up handlers for system signals
func setupSignalHandling(s tcell.Screen) {
	Debug("Initializing signal handling", DEBUG)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalChan
		Debug(fmt.Sprintf("Received signal: %v", sig), INFO)
		finalizeScreen(s)
		fmt.Println("Terminal restored.")
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
func connectToGRPCServer(serverAddr string) error {
	Debug(fmt.Sprintf("Connecting to gRPC server at %s", serverAddr), DEBUG)
	var err error

	// As of 1.63, the Dial() function family is depricated in favor of
	//   NewClient()
	grpcConn, err = grpc.NewClient(
		serverAddr,
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

// screenshotLoop requests screenshots from the server every 200ms
func screenshotLoop(s tcell.Screen) {
	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		screenshotMutex.Lock()
		// Ensure at least 200ms have passed since the last screenshot
		if time.Since(lastScreenshotTime) < 200*time.Millisecond {
			screenshotMutex.Unlock()
			continue
		}
		lastScreenshotTime = time.Now()
		screenshotMutex.Unlock()

		err := requestAndDisplayScreenshot(s)
		if err != nil {
			Debug(fmt.Sprintf("Error requesting screenshot: %v", err), ERROR)
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

// requestAndDisplayScreenshot requests a screenshot and updates the display
func requestAndDisplayScreenshot(s tcell.Screen) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := grpcClient.TakeScreenshot(ctx, &pb.Empty{})
	if err != nil {
		Debug(fmt.Sprintf("Error taking screenshot: %v", err), ERROR)
		return err
	}
	// Save the raw bytes first
	rawImageFile, err := os.Create(fmt.Sprintf("RawImage%03d.png", lastImageNumber))
	if err != nil {
		Debug(fmt.Sprintf("Error creating raw image file: %v", err), ERROR)
	} else {
		rawImageFile.Write([]byte(resp.Data))
		rawImageFile.Close()
	}

	img, err := png.Decode(strings.NewReader(string(resp.Data)))
	if err != nil {
		Debug(fmt.Sprintf("Error decoding screenshot PNG: %v", err), ERROR)
		return err
	}

	// Save the decoded image
	outputFile, err := os.Create(fmt.Sprintf("Image%03d.png", lastImageNumber))
	if err != nil {
		Debug(fmt.Sprintf("Error creating output file: %v", err), ERROR)
	} else {
		if err := png.Encode(outputFile, img); err != nil {
			Debug(fmt.Sprintf("Error encoding PNG: %v", err), ERROR)
		}
		outputFile.Close()
	}
	lastImageNumber++

	screenshotMutex.Lock()
	imageBuffer = image.NewRGBA(img.Bounds())
	draw.Draw(imageBuffer, img.Bounds(), img, image.Point{0, 0}, draw.Src)
	imageBounds = imageBuffer.Bounds()
	screenshotMutex.Unlock()

	// Update the display
	//   Only do it on first draw or after resize.  Otherwise images from the server should be the same size
	if firstDraw {
		clearDrawingArea(s)
	}
	firstDraw = false

	// Display the new image
	if err := displayImageBuffer(s); err != nil {
		Debug(fmt.Sprintf("Error displaying image buffer: %v", err), ERROR)
		return err
	}

	s.Show()
	return nil
}

// runMainLoop runs the main event loop of the application
func runMainLoop(s tcell.Screen) error {
	Debug("Entering main event loop", DEBUG)
	for {
		ev := s.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventResize:
			Debug("Screen resize event detected", DEBUG)
			handleResize(s)
		case *tcell.EventKey:
			handleKeyEvent(s, ev)
		case *tcell.EventMouse:
			handleMouseEvent(s, ev)
		case *tcell.EventInterrupt:
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

// Handle keyboard within the browser context
func handleKeyEvent(s tcell.Screen, ev *tcell.EventKey) {
	oldX, oldY := cursor.x, cursor.y

	if ev.Modifiers()&tcell.ModCtrl != 0 {
		// Ctrl+Key combinations
		switch ev.Key() {
		case tcell.KeyUp:
			if ev.Modifiers()&tcell.ModCtrl != 0 {
				// Handle Ctrl+Up
				Debug("Ctrl+Up pressed", DEBUG)
			} else {
				// Handle regular Up key
			}
		case tcell.KeyDown:
			if ev.Modifiers()&tcell.ModCtrl != 0 {
				// Handle Ctrl+Down
				Debug("Ctrl+Down pressed", DEBUG)
			} else {
				// Handle regular Down key
			}
		case tcell.KeyLeft:
			if ev.Modifiers()&tcell.ModCtrl != 0 {
				// Handle Ctrl+Left
				Debug("Ctrl+Left pressed", DEBUG)
			} else {
				// Handle regular Left key
			}
		case tcell.KeyRight:
			if ev.Modifiers()&tcell.ModCtrl != 0 {
				// Handle Ctrl+Right
				Debug("Ctrl+Right pressed", DEBUG)
			} else {
				// Handle regular Right key
			}
		}
	} else {
		// Regular keys
		switch ev.Key() {
		case tcell.KeyEscape:
			Debug("Exit key pressed", DEBUG)
			s.Fini()
			os.Exit(0)
		case tcell.KeyUp:
			if cursor.y > V_BORDER_WIDTH {
				cursor.y--
			}
		case tcell.KeyDown:
			if cursor.y < sDims.Height-(V_BORDER_WIDTH+sDims.LogHeight) {
				cursor.y++
			}
		case tcell.KeyLeft:
			if cursor.x > H_BORDER_WIDTH {
				cursor.x--
			}
		case tcell.KeyRight:
			if cursor.x < sDims.Width-H_BORDER_WIDTH {
				cursor.x++
			}
		default:
			// Send keyboard input to server
			go sendKeyboardInput(string(ev.Rune()))
		}
	}

	if oldX != cursor.x || oldY != cursor.y {
		redrawImageArea(s, oldX, oldY)
		redrawImageArea(s, cursor.x, cursor.y)
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

// detectTerminalAndCalibrate detects the terminal type and calibrates the character size
func detectTerminalAndCalibrate() {
	termType := os.Getenv("TERM")
	Debug(fmt.Sprintf("Terminal type: %s", termType), DEBUG)

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

	if cfg.UseTCell {
		// Fallback to character-based rendering for terminals without sixel
		return displayWithTcell(s, scaledImage)
	}

	// Use sixel rendering while respecting tcell boundaries
	return displayWithSixel(scaledImage)
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
	scaled := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.ApproxBiLinear.Scale(scaled, scaled.Bounds(), src, src.Bounds(), draw.Over, nil)

	return scaled
}

// Displays sixel graphics while respecting tcell's window boundaries and panels
func displayWithSixel(img *image.RGBA) error {
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

	// Configure and encode the sixel output
	enc := sixel.NewEncoder(os.Stdout)
	enc.Width = img.Bounds().Dx()
	enc.Height = img.Bounds().Dy()
	enc.Dither = true

	if err := enc.Encode(img); err != nil {
		Debug(fmt.Sprintf("Sixel encoding error: %v", err), ERROR)
		return fmt.Errorf("sixel encoding error: %v", err)
	}

	// Restore cursor position
	fmt.Print("\033[u")

	Debug(fmt.Sprintf("Displayed sixel image at (%d,%d) with size %dx%d in viewport %dx%d",
		H_BORDER_WIDTH, V_BORDER_WIDTH,
		img.Bounds().Dx(), img.Bounds().Dy(),
		sDims.InnerWidthPx, sDims.InnerHeightPx), DEBUG)

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

	file, err := os.Open(splashPath)
	if err != nil {
		return fmt.Errorf("failed to open splash image: %v", err)
	}
	defer file.Close()

	img, err := jpeg.Decode(file)
	if err != nil {
		return fmt.Errorf("failed to decode splash image: %v", err)
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

	logBuffer.Write([]byte("Press any key to continue..."))
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

// sendKeyboardInput sends keyboard input to the server
func sendKeyboardInput(text string) {
	Debug(fmt.Sprintf("Sending keyboard input: %s", text), DEBUG)
	_, err := grpcClient.SendKeyboardInput(context.Background(), &pb.Text{Content: text})
	if err != nil {
		Debug(fmt.Sprintf("Failed to send keyboard input: %v", err), ERROR)
	}
}
