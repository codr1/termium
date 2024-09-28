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

func main() {
	// Set up logging to both file and our custom buffer
	logFile, err := os.Create("debug.log")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating log file: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()

	logBuffer = LogBuffer{}
	multiWriter := io.MultiWriter(logFile, &logBuffer)
	log.SetOutput(multiWriter)
	log.SetFlags(log.Ltime | log.Lmicroseconds)
	log.Println("Program started")

	// Check if image file path was provided
	if len(os.Args) < 2 {
		log.Println("Insufficient arguments")
		fmt.Println("Usage: go run main.go <path_to_image>")
		os.Exit(1)
	}
	imagePath := os.Args[1]

	// Detect terminal and calibrate char size
	detectTerminalAndCalibrate()

	log.Printf("Image path: %s", imagePath)
	log.Printf("Character size: %dx%d pixels", charSize.Width, charSize.Height)

	// Initialize screen
	s, err := tcell.NewScreen()
	if err != nil {
		log.Printf("Error creating new screen: %v", err)
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	defer func() {
		s.Fini()
		log.Println("Screen finalized")
		fmt.Println("Terminal restored.")
	}()

	if err := s.Init(); err != nil {
		log.Printf("Error initializing screen: %v", err)
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	log.Println("Screen initialized")

	// Set up signal handling for graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signalChan
		log.Printf("Received signal: %v", sig)
		s.Fini()
		fmt.Println("Received termination signal. Terminal restored.")
		os.Exit(0)
	}()

	// Clear the screen
	s.Clear()
	log.Println("Screen cleared")

	// Display content
	if err := displayContent(s, imagePath); err != nil {
		log.Printf("Error displaying content: %v", err)
		fmt.Fprintf(os.Stderr, "Error displaying content: %v\n", err)
		return
	}
	log.Println("Content displayed")

	// Start a goroutine to update the log display
	go func() {
		for {
			s.PostEvent(tcell.NewEventInterrupt(nil))
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// Main event loop
	log.Println("Entering main event loop")
	for {
		ev := s.PollEvent()
		// Event Received debug messages are commented out as requested
		switch ev := ev.(type) {
		case *tcell.EventResize:
			log.Println("Resize event")
			s.Clear()
			s.Sync()
			if err := displayContent(s, imagePath); err != nil {
				log.Printf("Error redisplaying content after resize: %v", err)
				fmt.Fprintf(os.Stderr, "Error redisplaying content: %v\n", err)
			}
		case *tcell.EventKey:
			log.Printf("Key event: %v", ev.Key())
			if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyCtrlC {
				log.Println("Exit key pressed")
				return
			}
		case *tcell.EventInterrupt:
			if err := displayLogRightHalf(s); err != nil {
				log.Printf("Error updating log display: %v", err)
			}
		}
	}
}

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

	// TODO: Implement a more robust calibration method
	// This could involve displaying a calibration image or pattern and asking the user
	// to adjust it to match a known physical size. For example:
	// 1. Display a rectangle of a specific number of characters (e.g., 10x5)
	// 2. Ask the user to measure the physical size of this rectangle
	// 3. Use this information to calculate the pixel size of each character
	// This would provide a more accurate calibration across different terminal types and configurations.
}

func calibrateXterm() error {
	// Query terminal size in characters
	charResponse, err := queryTerminal("\033[18t")
	if err != nil {
		return fmt.Errorf("failed to query terminal size in characters: %v", err)
	}

	// Query terminal size in pixels
	pixelResponse, err := queryTerminal("\033[14t")
	if err != nil {
		return fmt.Errorf("failed to query terminal size in pixels: %v", err)
	}

	// Parse responses
	var charRows, charCols, pixelHeight, pixelWidth int
	_, err = fmt.Sscanf(charResponse, "\033[8;%d;%dt", &charRows, &charCols)
	if err != nil {
		return fmt.Errorf("failed to parse character size response: %v", err)
	}
	_, err = fmt.Sscanf(pixelResponse, "\033[4;%d;%dt", &pixelHeight, &pixelWidth)
	if err != nil {
		return fmt.Errorf("failed to parse pixel size response: %v", err)
	}

	// Calculate character size
	charSize.Width = pixelWidth / charCols
	charSize.Height = pixelHeight / charRows

	log.Printf("Calibrated character size: %dx%d pixels", charSize.Width, charSize.Height)
	return nil
}

func queryTerminal(query string) (string, error) {
	_, err := fmt.Fprint(os.Stdout, query)
	if err != nil {
		return "", err
	}

	// Use a raw terminal to read the response
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

func setDefaultCharSize() {
	charSize = CharSize{Width: 8, Height: 16}
	log.Println("Using default character size: 8x16 pixels")
}

func displayContent(s tcell.Screen, imagePath string) error {
	log.Println("Displaying content")
	// Get the terminal size
	width, height := s.Size()
	log.Printf("Screen size: %dx%d characters", width, height)

	// Calculate the width of each half
	halfWidth := width / 2

	// Display Sixel on the left half
	if err := displaySixelLeftHalf(s, imagePath, halfWidth, height); err != nil {
		return fmt.Errorf("error displaying image: %v", err)
	}

	// Display log on the right half
	if err := displayLogRightHalf(s); err != nil {
		return fmt.Errorf("error displaying log: %v", err)
	}

	// Display instructions
	style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)
	for x := 0; x < width; x++ {
		s.SetContent(x, height-1, ' ', nil, style)
	}
	message := "Press ESC to exit"
	for i, ch := range message {
		s.SetContent(i, height-1, ch, nil, style)
	}
	s.Show()
	log.Println("Content display completed")

	return nil
}

func displaySixelLeftHalf(s tcell.Screen, imagePath string, widthChars, heightChars int) error {
	log.Println("Displaying Sixel")

	// Convert character dimensions to pixel dimensions
	widthPixels := widthChars * charSize.Width
	heightPixels := (heightChars - 1) * charSize.Height // Leave space for instructions

	log.Printf("Image area: %dx%d characters, %dx%d pixels", widthChars, heightChars, widthPixels, heightPixels)

	// Open the image file
	file, err := os.Open(imagePath)
	if err != nil {
		log.Printf("Error opening image file: %v", err)
		return fmt.Errorf("error opening image: %v", err)
	}
	defer file.Close()

	// Decode the image
	img, _, err := image.Decode(file)
	if err != nil {
		log.Printf("Error decoding image: %v", err)
		return fmt.Errorf("error decoding image: %v", err)
	}

	// Calculate the scaling factor to fit the image width to the available space
	srcWidth := img.Bounds().Dx()
	srcHeight := img.Bounds().Dy()
	scaleX := float64(widthPixels) / float64(srcWidth)
	scaleY := float64(heightPixels) / float64(srcHeight)
	scale := scaleX
	if scaleY < scale {
		scale = scaleY
	}

	// Calculate the new dimensions
	newWidth := int(float64(srcWidth) * scale)
	newHeight := int(float64(srcHeight) * scale)

	log.Printf("Original image size: %dx%d", srcWidth, srcHeight)
	log.Printf("Scaled image size: %dx%d", newWidth, newHeight)

	// Scale the image
	scaledImg := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.ApproxBiLinear.Scale(scaledImg, scaledImg.Bounds(), img, img.Bounds(), draw.Over, nil)

	// Create a new Sixel encoder
	enc := sixel.NewEncoder(os.Stdout)
	enc.Width = newWidth
	enc.Height = newHeight

	// Move cursor to top-left corner
	fmt.Print("\033[H")

	// Encode and output the image as Sixel
	if err := enc.Encode(scaledImg); err != nil {
		log.Printf("Error encoding image to Sixel: %v", err)
		return fmt.Errorf("error encoding image to Sixel: %v", err)
	}
	log.Println("Sixel display completed")

	return nil
}

func displayLogRightHalf(s tcell.Screen) error {
	width, height := s.Size()
	halfWidth := width / 2
	style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorBlack)

	logBuffer.mutex.Lock()
	defer logBuffer.mutex.Unlock()

	startIndex := 0
	if len(logBuffer.messages) > height-1 {
		startIndex = len(logBuffer.messages) - (height - 1)
	}

	for y := 0; y < height-1; y++ {
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

	s.Show()
	return nil
}
