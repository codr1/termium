package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	pb "termium/client/pb"
)

// KeyboardHandler manages all keyboard input for the application
type KeyboardHandler struct {
	browserMode  BrowserMode
	urlBuffer    string
	urlCursorPos int
	grpcClient   pb.BrowserControlClient
}

// NewKeyboardHandler creates a new keyboard handler
func NewKeyboardHandler(client pb.BrowserControlClient) *KeyboardHandler {
	return &KeyboardHandler{
		browserMode: ModeNormal,
		grpcClient:  client,
	}
}

// HandleKeyEvent processes keyboard events and returns true if should exit
func (kh *KeyboardHandler) HandleKeyEvent(s tcell.Screen, ev *tcell.EventKey) bool {
	// Handle URL input mode separately
	if kh.browserMode == ModeURL {
		return kh.handleURLModeKey(s, ev)
	}

	// Normal mode handling
	return kh.handleNormalModeKey(s, ev)
}

// handleURLModeKey handles keyboard input when in URL input mode
func (kh *KeyboardHandler) handleURLModeKey(s tcell.Screen, ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEscape:
		// Cancel URL input
		kh.browserMode = ModeNormal
		kh.clearURLPrompt(s)
		Debug("URL input cancelled", DEBUG)
		return false

	case tcell.KeyEnter:
		// Submit URL and immediately return to normal mode
		url := kh.urlBuffer
		
		// Immediately show status and return to normal mode
		logBuffer.Write([]byte(fmt.Sprintf("Navigation request sent to: %s", url)))
		displayBottomPanel(s)
		
		kh.browserMode = ModeNormal
		kh.clearURLPrompt(s)
		
		// Navigate asynchronously in the background
		go kh.navigateToURLAsync(url, s)
		
		Debug(fmt.Sprintf("URL submitted: %s", url), DEBUG)
		return false

	case tcell.KeyBackspace, tcell.KeyBackspace2:
		// Delete character before cursor
		if len(kh.urlBuffer) > 0 && kh.urlCursorPos > 0 {
			kh.urlBuffer = kh.urlBuffer[:kh.urlCursorPos-1] + kh.urlBuffer[kh.urlCursorPos:]
			kh.urlCursorPos--
			kh.showURLPrompt(s)
		}
		return false

	case tcell.KeyDelete:
		// Delete character at cursor
		if kh.urlCursorPos < len(kh.urlBuffer) {
			kh.urlBuffer = kh.urlBuffer[:kh.urlCursorPos] + kh.urlBuffer[kh.urlCursorPos+1:]
			kh.showURLPrompt(s)
		}
		return false

	case tcell.KeyLeft:
		// Move cursor left
		if kh.urlCursorPos > 0 {
			kh.urlCursorPos--
			kh.showURLPrompt(s)
		}
		return false

	case tcell.KeyRight:
		// Move cursor right
		if kh.urlCursorPos < len(kh.urlBuffer) {
			kh.urlCursorPos++
			kh.showURLPrompt(s)
		}
		return false

	case tcell.KeyHome:
		// Move to beginning
		kh.urlCursorPos = 0
		kh.showURLPrompt(s)
		return false

	case tcell.KeyEnd:
		// Move to end
		kh.urlCursorPos = len(kh.urlBuffer)
		kh.showURLPrompt(s)
		return false

	case tcell.KeyCtrlU:
		// Clear entire line
		kh.urlBuffer = ""
		kh.urlCursorPos = 0
		kh.showURLPrompt(s)
		return false

	default:
		// Add character to buffer
		if ev.Rune() != 0 {
			kh.urlBuffer = kh.urlBuffer[:kh.urlCursorPos] + string(ev.Rune()) + kh.urlBuffer[kh.urlCursorPos:]
			kh.urlCursorPos++
			kh.showURLPrompt(s)
		}
	}
	return false
}

// handleNormalModeKey handles keyboard input in normal browsing mode
func (kh *KeyboardHandler) handleNormalModeKey(s tcell.Screen, ev *tcell.EventKey) bool {
	oldX, oldY := cursor.x, cursor.y

	if ev.Modifiers()&tcell.ModCtrl != 0 {
		// Ctrl+Key combinations
		// Check both Rune and Key for Ctrl+L (tcell might report it differently)
		if ev.Rune() == 'l' || ev.Rune() == 'L' || ev.Key() == tcell.KeyCtrlL {
			// Ctrl+L - Show URL bar with current URL
			Debug("Ctrl+L detected, entering URL mode", DEBUG)
			kh.browserMode = ModeURL
			kh.urlBuffer = kh.getCurrentURL()
			kh.urlCursorPos = len(kh.urlBuffer)
			kh.showURLPrompt(s)
			return false
		}

		// Handle other Ctrl+Key combinations if needed
		switch ev.Key() {
		case tcell.KeyUp:
			Debug("Ctrl+Up pressed", DEBUG)
			return false
		case tcell.KeyDown:
			Debug("Ctrl+Down pressed", DEBUG)
			return false
		case tcell.KeyLeft:
			Debug("Ctrl+Left pressed", DEBUG)
			return false
		case tcell.KeyRight:
			Debug("Ctrl+Right pressed", DEBUG)
			return false
		default:
			// Don't send Ctrl+key combinations to the server
			Debug(fmt.Sprintf("Unhandled Ctrl+key: Key=%v, Rune=%c", ev.Key(), ev.Rune()), DEBUG)
			return false
		}
	} else {
		// Regular keys
		switch ev.Key() {
		case tcell.KeyEscape:
			Debug("Exit key pressed", DEBUG)
			// Clean shutdown will be handled by main loop
			return true // Signal to exit

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

		case tcell.KeyEnter:
			// Send Enter key to browser (for form submission, etc.)
			Debug("Sending Enter key to browser", DEBUG)
			go kh.sendSpecialKey("Enter")
			
		case tcell.KeyTab:
			// Send Tab key to browser (for form navigation)
			Debug("Sending Tab key to browser", DEBUG)
			go kh.sendSpecialKey("Tab")
			
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			// Send Backspace to browser (for text input)
			Debug("Sending Backspace key to browser", DEBUG)
			go kh.sendSpecialKey("Backspace")

		default:
			// Send regular keyboard input to server
			if ev.Rune() != 0 {
				go kh.sendKeyboardInput(string(ev.Rune()))
			}
		}
	}

	if oldX != cursor.x || oldY != cursor.y {
		redrawImageArea(s, oldX, oldY)
		redrawImageArea(s, cursor.x, cursor.y)
	}

	return false // Don't exit
}

// GetBrowserMode returns the current browser mode
func (kh *KeyboardHandler) GetBrowserMode() BrowserMode {
	return kh.browserMode
}

// URL prompt display functions

// showURLPrompt displays the URL input field at the bottom of the terminal
func (kh *KeyboardHandler) showURLPrompt(s tcell.Screen) {
	// Clear the bottom line (inside the log panel area)
	y := sDims.Height - 2 // One line up from bottom border
	style := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite)

	// Clear the line first
	for x := 1; x < sDims.Width-1; x++ {
		s.SetContent(x, y, ' ', nil, style)
	}

	// Draw the prompt
	prompt := "URL: "
	x := 2
	for _, ch := range prompt {
		s.SetContent(x, y, ch, nil, style.Bold(true))
		x++
	}

	// Draw the URL buffer with selection highlight (browser-like behavior)
	urlStyle := tcell.StyleDefault.Background(tcell.ColorWhite).Foreground(tcell.ColorBlack)
	for _, ch := range kh.urlBuffer {
		if x < sDims.Width-2 {
			s.SetContent(x, y, ch, nil, urlStyle)
			x++
		}
	}

	// Draw cursor at end if buffer is not empty
	if kh.urlCursorPos >= len(kh.urlBuffer) && x < sDims.Width-2 {
		s.SetContent(x, y, ' ', nil, urlStyle.Reverse(true))
	}

	s.Show()
}

// clearURLPrompt clears the URL prompt from the bottom of the screen
func (kh *KeyboardHandler) clearURLPrompt(s tcell.Screen) {
	// Restore the bottom line to normal log panel appearance
	y := sDims.Height - 2
	navyStyle := tcell.StyleDefault.Background(tcell.ColorNavy)

	for x := 1; x < sDims.Width-1; x++ {
		s.SetContent(x, y, ' ', nil, navyStyle)
	}

	s.Show()
}

// Browser interaction functions

// getCurrentURL fetches the current URL from the browser
func (kh *KeyboardHandler) getCurrentURL() string {
	resp, err := kh.grpcClient.GetCurrentUrl(context.Background(), &pb.Empty{})
	if err != nil {
		Debug(fmt.Sprintf("Failed to get current URL: %v", err), ERROR)
		return ""
	}
	return resp.Url
}

// navigateToURLAsync sends the URL to the server asynchronously and updates status
func (kh *KeyboardHandler) navigateToURLAsync(url string, s tcell.Screen) {
	if url == "" {
		logBuffer.Write([]byte("Empty URL"))
		displayBottomPanel(s)
		return
	}

	// Add https:// if no protocol specified
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	Debug(fmt.Sprintf("Navigating to URL: %s", url), INFO)
	
	// Set a reasonable timeout for the navigation request
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	_, err := kh.grpcClient.NavigateToUrl(ctx, &pb.Url{Url: url})
	if err != nil {
		errorMsg := fmt.Sprintf("Navigation failed for '%s': %v", url, err)
		Debug(errorMsg, ERROR)
		logBuffer.Write([]byte(errorMsg))
	} else {
		// Log successful navigation
		successMsg := fmt.Sprintf("Successfully navigated to: %s", url)
		Debug(successMsg, INFO)
		logBuffer.Write([]byte(successMsg))
	}
	displayBottomPanel(s)
}

// sendKeyboardInput sends keyboard input to the server
func (kh *KeyboardHandler) sendKeyboardInput(text string) {
	Debug(fmt.Sprintf("Sending keyboard input: %s", text), DEBUG)
	_, err := kh.grpcClient.SendKeyboardInput(context.Background(), &pb.Text{Content: text})
	if err != nil {
		Debug(fmt.Sprintf("Failed to send keyboard input: %v", err), ERROR)
	}
}

// sendSpecialKey sends special keys like Enter, Tab, etc. to the server
func (kh *KeyboardHandler) sendSpecialKey(key string) {
	Debug(fmt.Sprintf("Sending special key: %s", key), DEBUG)
	// We need to send a special marker for these keys
	// The server should interpret these as keyboard.press() instead of keyboard.type()
	specialKeyMarker := fmt.Sprintf("__KEY__%s", key)
	_, err := kh.grpcClient.SendKeyboardInput(context.Background(), &pb.Text{Content: specialKeyMarker})
	if err != nil {
		Debug(fmt.Sprintf("Failed to send special key %s: %v", key, err), ERROR)
	}
}