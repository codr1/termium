package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"image"
	"strings"
	pb "termium/client/pb"
)

type Dialog struct {
	ID                                                                 string
	Type                                                               pb.DialogType
	Message, DefaultValue, InputText                                   string
	FocusedButton                                                      int // -1=input, 0=OK, 1=Cancel
	Active                                                             bool
	x, y, width, height                                                int
	buttonOKX, buttonOKY, buttonCancelX, buttonCancelY, inputX, inputY int
	editor                                                             textEditor
	mouseDown                                                          bool
}

func NewDialog(event *pb.DialogEvent) *Dialog {
	d := &Dialog{ID: event.Id, Type: event.Type, Message: event.Message, DefaultValue: event.DefaultValue, InputText: event.DefaultValue, Active: true}
	d.editor.set(event.DefaultValue, true)
	if d.Type == pb.DialogType_PROMPT {
		d.FocusedButton = -1
	}
	return d
}
func (d *Dialog) calculateLayout(w, h int) {
	d.width = min(w, 60)
	d.height = min(h, 8+min(6, len(strings.Split(d.Message, "\n"))))
	d.x = (w - d.width) / 2
	d.y = (h - d.height) / 2
	d.buttonOKX = d.x + 1
	d.buttonOKY = d.y + max(0, d.height-1)
	d.buttonCancelX = d.x + min(8, max(0, d.width-8))
	d.buttonCancelY = d.buttonOKY
	d.inputX = d.x + 1
	d.inputY = d.y + max(0, d.height-3)
}
func (d *Dialog) Draw(s tcell.Screen) {
	if !d.Active {
		return
	}
	d.calculateLayout(s.Size())
	style := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite)
	fillRect(s, image.Rect(d.x, d.y, d.x+d.width, d.y+d.height), style)
	drawText(s, d.x+1, d.y, d.width-2, d.getTitle(), style.Bold(true))
	for i, line := range strings.Split(d.Message, "\n") {
		y := d.y + 2 + i
		limit := d.buttonOKY - 1
		if d.Type == pb.DialogType_PROMPT {
			limit = d.inputY - 1
		}
		if y >= limit {
			break
		}
		drawText(s, d.x+1, y, d.width-2, line, style)
	}
	if d.Type == pb.DialogType_PROMPT {
		field := style.Background(tcell.ColorWhite).Foreground(tcell.ColorBlack)
		fillRect(s, image.Rect(d.inputX, d.inputY, d.x+d.width-1, d.inputY+1), field)
		d.editor.draw(s, d.inputX, d.inputY, d.width-2, d.FocusedButton == -1, field)
	}
	d.drawButton(s, d.buttonOKX, d.buttonOKY, "OK", d.FocusedButton == 0, style)
	if d.Type != pb.DialogType_ALERT {
		d.drawButton(s, d.buttonCancelX, d.buttonCancelY, "Cancel", d.FocusedButton == 1, style)
	}
}
func (d *Dialog) drawButton(s tcell.Screen, x, y int, label string, focused bool, style tcell.Style) {
	if focused {
		style = style.Reverse(true)
	}
	drawText(s, x, y, max(0, d.x+d.width-x), "["+label+"]", style)
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
		return "Leave page?"
	}
	return "Dialog"
}
func (d *Dialog) respond(accepted bool) *pb.DialogResponse {
	d.Active = false
	d.InputText = d.editor.value()
	return &pb.DialogResponse{Id: d.ID, Accepted: accepted, InputText: d.InputText}
}
func (d *Dialog) HandleKey(ev *tcell.EventKey) *pb.DialogResponse {
	if !d.Active {
		return nil
	}
	switch ev.Key() {
	case tcell.KeyEscape:
		return d.respond(d.Type == pb.DialogType_ALERT)
	case tcell.KeyEnter:
		return d.respond(d.FocusedButton != 1)
	case tcell.KeyTab, tcell.KeyBacktab:
		first, count := 0, 2
		if d.Type == pb.DialogType_ALERT {
			count = 1
		}
		if d.Type == pb.DialogType_PROMPT {
			first = -1
			count = 3
		}
		step := 1
		if ev.Key() == tcell.KeyBacktab {
			step = count - 1
		}
		d.FocusedButton = first + (d.FocusedButton-first+step)%count
	default:
		if d.Type == pb.DialogType_PROMPT && d.FocusedButton == -1 {
			d.editor.key(ev)
			d.InputText = d.editor.value()
		}
	}
	return nil
}
func (d *Dialog) HandleMouse(ev *tcell.EventMouse) *pb.DialogResponse {
	down := ev.Buttons()&tcell.Button1 != 0
	pressed := down && !d.mouseDown
	d.mouseDown = down
	if !d.Active || !pressed {
		return nil
	}
	x, y := ev.Position()
	if y == d.buttonOKY && x >= d.buttonOKX && x < d.buttonOKX+4 {
		return d.respond(true)
	}
	if d.Type != pb.DialogType_ALERT && y == d.buttonCancelY && x >= d.buttonCancelX && x < d.buttonCancelX+8 {
		return d.respond(false)
	}
	if d.Type == pb.DialogType_PROMPT && y == d.inputY && x >= d.inputX && x < d.x+d.width-1 {
		d.FocusedButton = -1
		d.editor.selected = false
		pos := d.editor.start
		col := d.inputX
		for pos < len(d.editor.text) && col < x {
			col += runewidth.RuneWidth(d.editor.text[pos])
			pos++
		}
		d.editor.cursor = pos
	}
	return nil
}
func (d *Dialog) IsPointInDialog(x, y int) bool {
	return d.Active && image.Pt(x, y).In(image.Rect(d.x, d.y, d.x+d.width, d.y+d.height))
}
