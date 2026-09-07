package main

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"image"
)

var menuIDs = []string{"newtab", "closetab", "reopentab", "tabs", "home", "back", "forward", "reload", "address", "pointer", "help", "quit"}

func (kh *KeyboardHandler) menuLabels() []string {
	if kh.tabsMenu {
		labels := []string{}
		for i, t := range kh.state.Tabs {
			marker := "  "
			if t.Id == kh.state.ActiveTabId {
				marker = "● "
			}
			labels = append(labels, fmt.Sprintf("%s%d %s", marker, i+1, t.Title))
		}
		return append(labels, "New tab                Ctrl+T", "Reopen closed tab      X")
	}
	reload := "Reload                 Ctrl+R"
	if kh.state.Loading {
		reload = "Stop loading"
	}
	pointer := "Mouse keys             F6"
	if kh.pointerMode {
		pointer = "Mouse keys: on         F6"
	}
	return []string{"New tab                Ctrl+T", "Close tab              Ctrl+W", "Reopen closed tab      X", "All tabs", "Home                   Alt+Home", "Back                   Alt+Left", "Forward                Alt+Right", reload, "Open address           Ctrl+L", pointer, "Shortcut help          F1", "Quit                   Ctrl+Q"}
}
func (kh *KeyboardHandler) menuRect() image.Rectangle {
	width := min(34, sDims.Width)
	height := min(len(kh.menuActions())+2, sDims.Height)
	return image.Rect(max(0, sDims.Width-width), min(2, sDims.Height-height), sDims.Width, min(2, sDims.Height-height)+height)
}
func (kh *KeyboardHandler) hasOverlay() bool { return kh.menu || kh.help || kh.quitConfirm }
func (kh *KeyboardHandler) Draw(s tcell.Screen) {
	width, height := s.Size()
	if width == 0 || height == 0 {
		return
	}
	s.HideCursor()
	base := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite)
	fillRect(s, image.Rect(0, 0, width, 2), base)
	for _, c := range kh.tabControls(width) {
		style := base.Background(tcell.ColorBlack)
		if c.id == "tab:"+kh.state.ActiveTabId || c.id == "close:"+kh.state.ActiveTabId {
			style = style.Background(tcell.ColorDarkSlateGray).Bold(true)
		}
		fillRect(s, c.rect, style)
		drawText(s, c.rect.Min.X, 0, c.rect.Dx(), c.label, style)
	}
	for _, c := range kh.controls(width) {
		style := base
		if !c.enabled {
			style = style.Foreground(tcell.ColorGray)
		}
		if kh.focus == c.id {
			style = style.Foreground(tcell.ColorBlack).Background(tcell.ColorYellow)
		}
		fillRect(s, c.rect, style)
		if c.id == "address" {
			style = style.Background(tcell.ColorDarkSlateGray).Foreground(tcell.ColorWhite)
			fillRect(s, c.rect, style)
			if kh.focus == "address" {
				kh.editor.draw(s, c.rect.Min.X, 1, c.rect.Dx(), true, style)
			} else {
				text := kh.state.Url
				if text == "" || text == "about:blank" {
					text = "Ctrl+L: enter an address"
				}
				drawText(s, c.rect.Min.X, 1, c.rect.Dx(), text, style)
			}
		} else {
			drawText(s, c.rect.Min.X, 1, c.rect.Dx(), c.label, style)
		}
	}
	if height > 1 {
		fillRect(s, image.Rect(0, height-1, width, height), base)
		status := kh.status
		if kh.state.VimiumStatus != "" && kh.state.VimiumStatus != "Vimium" {
			status = kh.state.VimiumStatus
		}
		if kh.state.Loading {
			status = "Loading…  Reload becomes Stop"
		}
		if kh.pointerMode {
			status = "Mouse keys: arrows/hjkl · Enter click · Space drag · r right-click · u/d scroll · Esc exit"
		}
		drawText(s, 0, height-1, width, status, base)
	}
	if kh.hasOverlay() {
		s.HideCursor()
	}
	if kh.pointerMode && kh.focus == "page" && kh.pointer.In(viewportRect(width, height)) && !kh.hasOverlay() {
		s.ShowCursor(kh.pointer.X, kh.pointer.Y)
	}
	if kh.menu {
		rect := kh.menuRect()
		fillRect(s, rect, base)
		for i, label := range kh.menuLabels() {
			y := rect.Min.Y + 1 + i - kh.menuStart()
			if i < kh.menuStart() {
				continue
			}
			if y >= rect.Max.Y-1 {
				break
			}
			style := base
			if i == kh.menuIndex {
				style = style.Reverse(true)
			}
			fillRect(s, image.Rect(rect.Min.X, y, rect.Max.X, y+1), style)
			drawText(s, rect.Min.X+1, y, rect.Dx()-2, label, style)
		}
	}
	if kh.help {
		lines := []string{"Termium shortcuts", "", "Ctrl+L  Address (select all)", "Alt+Left / Alt+Right  Back / Forward", "Alt+Home  Home page", "Ctrl+R or F5  Reload / Stop", "F10  Menu     F6  Mouse keys", "Ctrl+Q  Quit   Escape x3  Emergency exit", "", "Vimium: f/F links · hjkl scroll · i insert", "t new tab · J/K switch · x close · X reopen", "? Vimium help · Escape cancels a mode", "Ctrl+T new tab · Ctrl+W close tab", "Mouse: click, drag, wheel, right/middle buttons.", "Mouse keys: arrows/hjkl, Enter click, Space drag,", "r right-click, m middle-click, u/d scroll.", "", "Enter or Escape to close"}
		drawOverlay(s, lines)
	}
	if kh.quitConfirm {
		drawOverlay(s, []string{"Quit Termium?", "", "[Quit]  [Stay]", "Enter / Y: quit     Escape / N: stay"})
	}
}
func drawOverlay(s tcell.Screen, lines []string) {
	width, height := s.Size()
	rect := overlayRect(width, height, len(lines))
	h := rect.Dy()
	style := tcell.StyleDefault.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite)
	fillRect(s, rect, style)
	for i, line := range lines {
		if i+1 >= h {
			break
		}
		drawText(s, rect.Min.X+1, rect.Min.Y+1+i, rect.Dx()-2, line, style)
	}
}
func (kh *KeyboardHandler) sizeStatus() {
	if viewportRect(sDims.Width, sDims.Height).Empty() {
		kh.status = fmt.Sprintf("Enlarge terminal (%d×%d)", sDims.Width, sDims.Height)
	}
}

func (kh *KeyboardHandler) menuStart() int {
	return max(0, kh.menuIndex-max(1, kh.menuRect().Dy()-2)+1)
}
func overlayRect(width, height, lines int) image.Rectangle {
	w := min(58, width)
	h := min(lines+2, height)
	return image.Rect((width-w)/2, (height-h)/2, (width-w)/2+w, (height-h)/2+h)
}
func quitButtons(width, height int) (image.Rectangle, image.Rectangle) {
	r := overlayRect(width, height, 4)
	y := r.Min.Y + 3
	return image.Rect(r.Min.X+1, y, r.Min.X+7, y+1), image.Rect(r.Min.X+9, y, r.Min.X+15, y+1)
}
