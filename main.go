package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mattn/go-sixel"
	"golang.org/x/image/draw"
	"golang.org/x/term"

	"github.com/gdamore/tcell/v2"
)

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
	CharX, CharY int
}

var currentMouse MouseInfo
var cursor Cursor
var imageBuffer *image.RGBA
var imageBounds image.Rectangle

// Write implements the io.Writer interface for LogBuffer
func (lb *LogBuffer) Write(p []byte) (n int, err error) {
	lb.mutex.Lock()
	defer lb.mutex.Unlock()
	message := string(p)
	lb.messages = append(lb.messages, strings.TrimSpace(message))
	if len(lb.messages) > 1000 {
		lb.messages = lb.messages[1:]
	}
	return len(p), nil
}

func displayErrorMessage(s tcell.Screen, message string) {
    width, height := s.Size()
    style := tcell.StyleDefault.Foreground(tcell.ColorRed).Background(tcell.ColorBlack)
    
    // Clear the bottom line
    for x := 0; x < width; x++ {
        s.SetContent(x, height-1, ' ', nil, style)
    }
    
    // Display the error message
    for i, ch := range message {
        if i < width {
            s.SetContent(i, height-1, ch, nil, style)
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
        s.Show()  // Make sure to show the changes
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
	imagePath := parseArgs()
	setupLogging()
	detectTerminalAndCalibrate()
	s := initializeScreen()
	defer finalizeScreen(s)

	setupSignalHandling(s)
	initializeCursor(s)
	if err := runMainLoop(s, imagePath); err != nil {
		displayErrorMessage(s, fmt.Sprint("Error in main loop: %v", err))
	}
}

// parseArgs parses command-line arguments and returns the image path
func parseArgs() string {
	if len(os.Args) < 2 {
		log.Println("Insufficient arguments")
		fmt.Println("Usage: go run main.go <path_to_image>")
		os.Exit(1)
	}
	return os.Args[1]
}

// setupLogging initializes the logging system
func setupLogging() {
	logFile, err := os.Create("debug.log")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating log file: %v\n", err)
		os.Exit(1)
	}
	logBuffer = LogBuffer{}
	multiWriter := io.MultiWriter(logFile, &logBuffer)
	log.SetOutput(multiWriter)
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.Println("Program started")
}

// initializeScreen creates and initializes the tcell screen
func initializeScreen() tcell.Screen {
	s, err := tcell.NewScreen()
	if err != nil {
		log.Printf("Error creating new screen: %v", err)
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if err := s.Init(); err != nil {
		log.Printf("Error initializing screen: %v", err)
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	s.EnableMouse()

	log.Println("Screen initialized")
	s.Clear()
	return s
}

// finalizeScreen properly closes the tcell screen
func finalizeScreen(s tcell.Screen) {
	s.Fini()
	log.Println("Screen finalized")
	fmt.Println("Terminal restored.")
}

// setupSignalHandling sets up handlers for system signals
func setupSignalHandling(s tcell.Screen) {
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalChan
		log.Printf("Received signal: %v", sig)
		finalizeScreen(s)
		fmt.Println("Received termination signal. Terminal restored.")
		os.Exit(0)
	}()
}

// initializeCursor sets up the initial cursor position
func initializeCursor(s tcell.Screen) {
	width, height := s.Size()
	cursor = Cursor{
		x:         width / 4,
		y:         height / 2,
		visible:   true,
		blinkOn:   true,
		lastBlink: time.Now(),
	}
}

// runMainLoop runs the main event loop of the application
func runMainLoop(s tcell.Screen, imagePath string) error {
	if err := displayContent(s, imagePath); err != nil {
		log.Printf("Error displaying content: %v", err)
		return fmt.Errorf("Error displaying content: %v", err)
	}
	log.Println("Content displayed")

	go func() {
		for {
			s.PostEvent(tcell.NewEventInterrupt(nil))
			time.Sleep(50 * time.Millisecond)  // Reduced from 100ms to 50ms for more frequent updates
		}
	}()

	log.Println("Entering main event loop")
	for {
		ev := s.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventResize:
			handleResize(s, imagePath)
		case *tcell.EventKey:
			handleKeyEvent(s, ev)
		case *tcell.EventMouse:
			handleMouseEvent(s, ev)
		case *tcell.EventInterrupt:
			handleInterrupt(s)
		}
	}
}

// handleResize handles screen resize events
func handleResize(s tcell.Screen, imagePath string) {
	log.Println("Resize event")
	s.Clear()
	s.Sync()
	if err := displayContent(s, imagePath); err != nil {
		log.Printf("Error redisplaying content after resize: %v", err)
		displayErrorMessage(s, fmt.Sprintf("Error redisplaying content: %v", err))
	}
}

// handleInterrupt handles interrupt events for updating the display
func handleInterrupt(s tcell.Screen) {
    if err := displayLogRightHalf(s); err != nil {
        log.Printf("Error updating log display: %v", err)
    }
    blinkCursor(s)
    displayMouseInfo(s)
}

func handleMouseEvent(s tcell.Screen, ev *tcell.EventMouse) {
    x, y := ev.Position()
    currentMouse.CharX = x
    currentMouse.CharY = y
    currentMouse.PixelX = x * charSize.Width
    currentMouse.PixelY = y * charSize.Height
    
    // Only update if the mouse is in the left half of the screen
    width, _ := s.Size()
    if x < width/2 {
        displayMouseInfo(s)
    }
}

// detectTerminalAndCalibrate detects the terminal type and calibrates the character size
func detectTerminalAndCalibrate() {
	termType := os.Getenv("TERM")
	log.Printf("Detected terminal type: %s", termType)

	if strings.HasPrefix(termType, "xterm") || strings.Contains(termType, "256color") {
		log.Println("xterm-compatible terminal detected. Attempting to calibrate.")
		if err := calibrateXterm(); err != nil {
			log.Printf("Calibration failed: %v. Falling back to defaults.", err)
			setDefaultCharSize()
		}
	} else {
		log.Println("Non-xterm terminal or unable to detect. Using default character size.")
		setDefaultCharSize()
	}
}

// calibrateXterm calibrates the character size for xterm-compatible terminals
func calibrateXterm() error {
	charResponse, err := queryTerminal("\033[18t")
	if err != nil {
		return fmt.Errorf("failed to query terminal size in characters: %v", err)
	}

	pixelResponse, err := queryTerminal("\033[14t")
	if err != nil {
		return fmt.Errorf("failed to query terminal size in pixels: %v", err)
	}

	var charRows, charCols, pixelHeight, pixelWidth int
	_, err = fmt.Sscanf(charResponse, "\033[8;%d;%dt", &charRows, &charCols)
	if err != nil {
		return fmt.Errorf("failed to parse character size response: %v", err)
	}
	_, err = fmt.Sscanf(pixelResponse, "\033[4;%d;%dt", &pixelHeight, &pixelWidth)
	if err != nil {
		return fmt.Errorf("failed to parse pixel size response: %v", err)
	}

	charSize.Width = pixelWidth / charCols
	charSize.Height = pixelHeight / charRows

	log.Printf("Calibrated character size: %dx%d pixels", charSize.Width, charSize.Height)
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
	log.Println("Using default character size: 8x16 pixels")
}

// displayContent displays the image and log on the screen
func displayContent(s tcell.Screen, imagePath string) error {
	log.Println("Displaying content")
	width, height := s.Size()
	log.Printf("Screen size: %dx%d characters", width, height)

	halfWidth := width / 2

	if err := displaySixelLeftHalf(s, imagePath, halfWidth, height); err != nil {
		displayErrorMessage(s, fmt.Sprintf("Error displaying image: %v", err))
		return fmt.Errorf("error displaying image: %v", err)
	}

	if err := displayLogRightHalf(s); err != nil {
		displayErrorMessage(s, fmt.Sprintf("Error displaying log: %v", err))
		return fmt.Errorf("error displaying log: %v", err)
	}

	displayInstructions(s, width, height)
	s.Show()
	log.Println("Content display completed")

	return nil
}

// displayInstructions displays usage instructions at the bottom of the screen
func displayInstructions(s tcell.Screen, width, height int) {
	style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	for x := 0; x < width; x++ {
		s.SetContent(x, height-1, ' ', nil, style)
	}
    message := "Use arrow keys to move cursor. Mouse over image for coordinates. Press ESC to exit"
	for i, ch := range message {
		s.SetContent(i, height-1, ch, nil, style)
	}
}

// displaySixelLeftHalf displays the image on the left half of the screen
func displaySixelLeftHalf(s tcell.Screen, imagePath string, widthChars, heightChars int) error {
	log.Println("Displaying Sixel")

	widthPixels := widthChars * charSize.Width
	heightPixels := (heightChars - 1) * charSize.Height

	log.Printf("Image area: %dx%d characters, %dx%d pixels", widthChars, heightChars, widthPixels, heightPixels)

	file, err := os.Open(imagePath)
	if err != nil {
		log.Printf("Error opening image file: %v", err)
		displayErrorMessage(s, fmt.Sprintf("Error opening image: %v", err))
		return fmt.Errorf("error opening image: %v", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		log.Printf("Error decoding image: %v", err)
		displayErrorMessage(s, fmt.Sprintf("Error decoding image: %v", err))
		return fmt.Errorf("error decoding image: %v", err)
	}

	srcWidth := img.Bounds().Dx()
	srcHeight := img.Bounds().Dy()
	scaleX := float64(widthPixels) / float64(srcWidth)
	scaleY := float64(heightPixels) / float64(srcHeight)
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}

	newWidth := int(float64(srcWidth) * scale)
	newHeight := int(float64(srcHeight) * scale)

	log.Printf("Original image size: %dx%d", srcWidth, srcHeight)
	log.Printf("Scaled image size: %dx%d", newWidth, newHeight)

	imageBuffer = image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.ApproxBiLinear.Scale(imageBuffer, imageBuffer.Bounds(), img, img.Bounds(), draw.Over, nil)
	imageBounds = imageBuffer.Bounds()

	enc := sixel.NewEncoder(os.Stdout)
	enc.Width = newWidth
	enc.Height = newHeight

	fmt.Print("\033[H")

	if err := enc.Encode(imageBuffer); err != nil {
		log.Printf("Error encoding image to Sixel: %v", err)
		displayErrorMessage(s, fmt.Sprintf("Error encoding image to Sixel: %v", err))
		return fmt.Errorf("error encoding image to Sixel: %v", err)
	}
	log.Println("Sixel display completed")

	return nil
}

// displayLogRightHalf displays the log messages on the right half of the screen
func displayLogRightHalf(s tcell.Screen) error {
    width, height := s.Size()
    halfWidth := width / 2
    style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)

    logBuffer.mutex.Lock()
    defer logBuffer.mutex.Unlock()

    startIndex := 0
    if len(logBuffer.messages) > height-4 {  // Changed from height-1 to height-4
        startIndex = len(logBuffer.messages) - (height - 4)
    }

    for y := 0; y < height-4; y++ {  // Changed from height-1 to height-4
        for x := halfWidth; x < width; x++ {
            s.SetContent(x, y, ' ', nil, style)
        }
        if y < len(logBuffer.messages)-startIndex {
            message := logBuffer.messages[startIndex+y]
            for x, ch := range message {
                if halfWidth+x < width {
                    s.SetContent(halfWidth+x, y, ch, nil, style)
                }
            }
        }
    }

    return nil
}

func displayMouseInfo(s tcell.Screen) {
    width, height := s.Size()
    halfWidth := width / 2
    style := tcell.StyleDefault.Foreground(tcell.ColorYellow).Background(tcell.ColorBlack)

    // Clear the area for mouse info
    for y := height - 4; y < height - 1; y++ {
        for x := halfWidth; x < width; x++ {
            s.SetContent(x, y, ' ', nil, style)
        }
    }

    // Display mouse info
    info := []string{
        fmt.Sprintf("Mouse Pixel: (%d, %d)", currentMouse.PixelX, currentMouse.PixelY),
        fmt.Sprintf("Mouse Char:  (%d, %d)", currentMouse.CharX, currentMouse.CharY),
    }

    for i, line := range info {
        for x, ch := range line {
            if halfWidth+x < width {
                s.SetContent(halfWidth+x, height-4+i, ch, nil, style)
            }
        }
    }

    s.Show()
}

// handleKeyEvent handles key events for cursor movement and exit
func handleKeyEvent(s tcell.Screen, ev *tcell.EventKey) {
	width, height := s.Size()
	halfWidth := width / 2
	
	oldX, oldY := cursor.x, cursor.y

	switch ev.Key() {
	case tcell.KeyEscape, tcell.KeyCtrlC:
		log.Println("Exit key pressed")
		s.Fini()
		os.Exit(0)
	case tcell.KeyUp:
		if cursor.y > 0 {
			cursor.y--
		}
	case tcell.KeyDown:
		if cursor.y < height-2 {
			cursor.y++
		}
	case tcell.KeyLeft:
		if cursor.x > 0 {
			cursor.x--
		}
	case tcell.KeyRight:
		if cursor.x < halfWidth-1 {
			cursor.x++
		}
	}

	if oldX != cursor.x || oldY != cursor.y {
		redrawImageArea(s, oldX, oldY)
		redrawImageArea(s, cursor.x, cursor.y)
	}
}
