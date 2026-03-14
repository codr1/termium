package main

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	pb "termium/client/pb"
)

// Dialog represents a browser dialog box (alert, confirm, prompt)
type Dialog struct {
	ID           string
	Type         pb.DialogType
	Message      string
	DefaultValue string
	InputText    string       // For prompt dialogs
	FocusedButton int         // 0=OK, 1=Cancel
	Active       bool
	
	// UI positioning
	x, y, width, height int
	buttonOKX, buttonOKY int
	buttonCancelX, buttonCancelY int
	inputX, inputY int
	cursorPos int
}

// NewDialog creates a new dialog from a server event
func NewDialog(event *pb.DialogEvent) *Dialog {
	return &Dialog{
		ID:           event.Id,
		Type:         event.Type,
		Message:      event.Message,
		DefaultValue: event.DefaultValue,
		InputText:    event.DefaultValue, // Start with default for prompts
		FocusedButton: 0, // Start with OK focused
		Active:       true,
		cursorPos:    len(event.DefaultValue),
	}
}

// Calculate positions dialog box and buttons
func (d *Dialog) calculateLayout(screenWidth, screenHeight int) {
	// Calculate dialog dimensions
	lines := strings.Split(d.Message, "\n")
	maxLineLen := 0
	for _, line := range lines {
		if len(line) > maxLineLen {
			maxLineLen = len(line)
		}
	}
	
	// Dialog width: message width + padding, min 40 chars
	d.width = maxLineLen + 8
	if d.width < 40 {
		d.width = 40
	}
	if d.width > screenWidth - 10 {
		d.width = screenWidth - 10
	}
	
	// Dialog height based on type
	d.height = len(lines) + 6 // Title + message + buttons + borders
	if d.Type == pb.DialogType_PROMPT {
		d.height += 2 // Extra space for input field
	}
	
	// Center the dialog
	d.x = (screenWidth - d.width) / 2
	d.y = (screenHeight - d.height) / 2
	
	// Calculate button positions
	buttonY := d.y + d.height - 2
	if d.Type == pb.DialogType_ALERT {
		// Single OK button, centered
		d.buttonOKX = d.x + (d.width - 6) / 2
		d.buttonOKY = buttonY
	} else {
		// Two buttons: OK and Cancel
		totalButtonWidth := 14 // "[OK]" + spacing + "[Cancel]"
		startX := d.x + (d.width - totalButtonWidth) / 2
		d.buttonOKX = startX
		d.buttonOKY = buttonY
		d.buttonCancelX = startX + 8
		d.buttonCancelY = buttonY
	}
	
	// Input field position for prompts
	if d.Type == pb.DialogType_PROMPT {
		d.inputX = d.x + 2
		d.inputY = d.y + len(lines) + 3
	}
}

// Draw renders the dialog on the screen
func (d *Dialog) Draw(s tcell.Screen) {
	if !d.Active {
		return
	}
	
	width, height := s.Size()
	d.calculateLayout(width, height)
	
	// Styles
	borderStyle := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite)
	bgStyle := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite)
	buttonStyle := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorYellow)
	focusedStyle := tcell.StyleDefault.Background(tcell.ColorYellow).Foreground(tcell.ColorBlack)
	
	// Draw box background
	for y := d.y; y < d.y + d.height; y++ {
		for x := d.x; x < d.x + d.width; x++ {
			s.SetContent(x, y, ' ', nil, bgStyle)
		}
	}
	
	// Draw border
	// Top border
	s.SetContent(d.x, d.y, '┌', nil, borderStyle)
	for x := d.x + 1; x < d.x + d.width - 1; x++ {
		s.SetContent(x, d.y, '─', nil, borderStyle)
	}
	s.SetContent(d.x + d.width - 1, d.y, '┐', nil, borderStyle)
	
	// Side borders
	for y := d.y + 1; y < d.y + d.height - 1; y++ {
		s.SetContent(d.x, y, '│', nil, borderStyle)
		s.SetContent(d.x + d.width - 1, y, '│', nil, borderStyle)
	}
	
	// Bottom border
	s.SetContent(d.x, d.y + d.height - 1, '└', nil, borderStyle)
	for x := d.x + 1; x < d.x + d.width - 1; x++ {
		s.SetContent(x, d.y + d.height - 1, '─', nil, borderStyle)
	}
	s.SetContent(d.x + d.width - 1, d.y + d.height - 1, '┘', nil, borderStyle)
	
	// Draw title
	title := d.getTitle()
	titleX := d.x + (d.width - len(title) - 4) / 2
	titleStr := fmt.Sprintf("─ %s ─", title)
	for i, ch := range titleStr {
		s.SetContent(titleX + i, d.y, ch, nil, borderStyle)
	}
	
	// Draw message
	lines := strings.Split(d.Message, "\n")
	for i, line := range lines {
		lineX := d.x + 2
		lineY := d.y + 2 + i
		for j, ch := range line {
			if lineX + j < d.x + d.width - 2 {
				s.SetContent(lineX + j, lineY, ch, nil, bgStyle)
			}
		}
	}
	
	// Draw input field for prompts
	if d.Type == pb.DialogType_PROMPT {
		// Draw input box
		inputStyle := tcell.StyleDefault.Background(tcell.ColorWhite).Foreground(tcell.ColorBlack)
		inputWidth := d.width - 4
		for x := 0; x < inputWidth; x++ {
			ch := ' '
			if x < len(d.InputText) {
				ch = rune(d.InputText[x])
			}
			s.SetContent(d.inputX + x, d.inputY, ch, nil, inputStyle)
		}
		// Draw cursor
		if d.FocusedButton == -1 { // -1 means input field is focused
			cursorX := d.inputX + d.cursorPos
			if cursorX < d.inputX + inputWidth {
				s.SetContent(cursorX, d.inputY, '_', nil, inputStyle.Reverse(true))
			}
		}
	}
	
	// Draw buttons
	if d.Type == pb.DialogType_ALERT {
		// Single OK button
		d.drawButton(s, d.buttonOKX, d.buttonOKY, "OK", d.FocusedButton == 0, buttonStyle, focusedStyle)
	} else {
		// OK and Cancel buttons
		okFocused := (d.Type == pb.DialogType_PROMPT && d.FocusedButton == 0) || 
		            (d.Type != pb.DialogType_PROMPT && d.FocusedButton == 0)
		cancelFocused := (d.Type == pb.DialogType_PROMPT && d.FocusedButton == 1) || 
		                (d.Type != pb.DialogType_PROMPT && d.FocusedButton == 1)
		
		d.drawButton(s, d.buttonOKX, d.buttonOKY, "OK", okFocused, buttonStyle, focusedStyle)
		d.drawButton(s, d.buttonCancelX, d.buttonCancelY, "Cancel", cancelFocused, buttonStyle, focusedStyle)
	}
}

func (d *Dialog) drawButton(s tcell.Screen, x, y int, text string, focused bool, normalStyle, focusedStyle tcell.Style) {
	style := normalStyle
	if focused {
		style = focusedStyle
	}
	
	// Draw button with brackets
	buttonText := fmt.Sprintf("[%s]", text)
	for i, ch := range buttonText {
		s.SetContent(x + i, y, ch, nil, style)
	}
}

func (d *Dialog) getTitle() string {
	switch d.Type {
	case pb.DialogType_ALERT:
		return "Alert"
	case pb.DialogType_CONFIRM:
		return "Confirm"
	case pb.DialogType_PROMPT:
		return "Prompt"
	case pb.DialogType_BEFOREUNLOAD:
		return "Leave Page?"
	default:
		return "Dialog"
	}
}

// HandleKey processes keyboard input for the dialog
func (d *Dialog) HandleKey(ev *tcell.EventKey) *pb.DialogResponse {
	if !d.Active {
		return nil
	}
	
	switch d.Type {
	case pb.DialogType_ALERT:
		// Alert only has OK button
		if ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyEscape {
			d.Active = false
			return &pb.DialogResponse{
				Id:       d.ID,
				Accepted: true,
			}
		}
		
	case pb.DialogType_CONFIRM, pb.DialogType_BEFOREUNLOAD:
		switch ev.Key() {
		case tcell.KeyTab:
			// Toggle between OK and Cancel
			d.FocusedButton = 1 - d.FocusedButton
			
		case tcell.KeyEnter:
			// Activate focused button
			d.Active = false
			return &pb.DialogResponse{
				Id:       d.ID,
				Accepted: d.FocusedButton == 0,
			}
			
		case tcell.KeyEscape:
			// Always means Cancel
			d.Active = false
			return &pb.DialogResponse{
				Id:       d.ID,
				Accepted: false,
			}
		}
		
	case pb.DialogType_PROMPT:
		// Prompt has input field plus OK/Cancel
		if d.FocusedButton == -1 {
			// Input field is focused
			switch ev.Key() {
			case tcell.KeyTab:
				d.FocusedButton = 0 // Move to OK button
				
			case tcell.KeyEnter:
				// Submit with current text
				d.Active = false
				return &pb.DialogResponse{
					Id:        d.ID,
					Accepted:  true,
					InputText: d.InputText,
				}
				
			case tcell.KeyEscape:
				// Cancel
				d.Active = false
				return &pb.DialogResponse{
					Id:       d.ID,
					Accepted: false,
				}
				
			case tcell.KeyBackspace, tcell.KeyBackspace2:
				if len(d.InputText) > 0 && d.cursorPos > 0 {
					d.InputText = d.InputText[:d.cursorPos-1] + d.InputText[d.cursorPos:]
					d.cursorPos--
				}
				
			case tcell.KeyDelete:
				if d.cursorPos < len(d.InputText) {
					d.InputText = d.InputText[:d.cursorPos] + d.InputText[d.cursorPos+1:]
				}
				
			case tcell.KeyLeft:
				if d.cursorPos > 0 {
					d.cursorPos--
				}
				
			case tcell.KeyRight:
				if d.cursorPos < len(d.InputText) {
					d.cursorPos++
				}
				
			case tcell.KeyHome:
				d.cursorPos = 0
				
			case tcell.KeyEnd:
				d.cursorPos = len(d.InputText)
				
			default:
				// Add character
				if ev.Rune() != 0 {
					d.InputText = d.InputText[:d.cursorPos] + string(ev.Rune()) + d.InputText[d.cursorPos:]
					d.cursorPos++
				}
			}
		} else {
			// Buttons are focused
			switch ev.Key() {
			case tcell.KeyTab:
				// Cycle through: Input -> OK -> Cancel -> Input
				if d.FocusedButton == 1 {
					d.FocusedButton = -1 // Back to input
				} else {
					d.FocusedButton++
				}
				
			case tcell.KeyEnter:
				// Activate focused button
				d.Active = false
				if d.FocusedButton == 0 {
					return &pb.DialogResponse{
						Id:        d.ID,
						Accepted:  true,
						InputText: d.InputText,
					}
				} else {
					return &pb.DialogResponse{
						Id:       d.ID,
						Accepted: false,
					}
				}
				
			case tcell.KeyEscape:
				// Always means Cancel
				d.Active = false
				return &pb.DialogResponse{
					Id:       d.ID,
					Accepted: false,
				}
			}
		}
	}
	
	return nil
}

// HandleMouse processes mouse input for the dialog
func (d *Dialog) HandleMouse(ev *tcell.EventMouse) *pb.DialogResponse {
	if !d.Active {
		return nil
	}
	
	x, y := ev.Position()
	
	// Check if click is on OK button
	if d.Type == pb.DialogType_ALERT {
		if y == d.buttonOKY && x >= d.buttonOKX && x < d.buttonOKX + 4 {
			d.Active = false
			return &pb.DialogResponse{
				Id:       d.ID,
				Accepted: true,
			}
		}
	} else {
		// Check OK button
		if y == d.buttonOKY && x >= d.buttonOKX && x < d.buttonOKX + 4 {
			d.Active = false
			accepted := true
			response := &pb.DialogResponse{
				Id:       d.ID,
				Accepted: accepted,
			}
			if d.Type == pb.DialogType_PROMPT {
				response.InputText = d.InputText
			}
			return response
		}
		
		// Check Cancel button
		if y == d.buttonCancelY && x >= d.buttonCancelX && x < d.buttonCancelX + 8 {
			d.Active = false
			return &pb.DialogResponse{
				Id:       d.ID,
				Accepted: false,
			}
		}
		
		// Check input field for prompts
		if d.Type == pb.DialogType_PROMPT {
			if y == d.inputY && x >= d.inputX && x < d.inputX + d.width - 4 {
				// Click in input field - set cursor position
				d.FocusedButton = -1 // Focus input
				newPos := x - d.inputX
				if newPos <= len(d.InputText) {
					d.cursorPos = newPos
				} else {
					d.cursorPos = len(d.InputText)
				}
			}
		}
	}
	
	return nil
}

// IsPointInDialog checks if a screen coordinate is within the dialog bounds
func (d *Dialog) IsPointInDialog(x, y int) bool {
	if !d.Active {
		return false
	}
	return x >= d.x && x < d.x + d.width && y >= d.y && y < d.y + d.height
}