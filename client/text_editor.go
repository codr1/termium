package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"strings"
	"unicode"
)

type textEditor struct {
	text     []rune
	cursor   int
	selected bool
	start    int
}

func (e *textEditor) set(text string, selected bool) {
	e.text = []rune(text)
	e.cursor = len(e.text)
	e.start = 0
	e.selected = selected
}
func (e *textEditor) value() string { return string(e.text) }
func (e *textEditor) insert(text string) {
	// Local fields are single-line; terminal paste must not become commands.
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	added := []rune(text)
	existing := len(e.text)
	if e.selected {
		existing = 0
	}
	if existing+len(added) > 8192 {
		return
	}
	if e.selected {
		e.text = nil
		e.cursor = 0
		e.selected = false
	}
	tail := append([]rune(nil), e.text[e.cursor:]...)
	e.text = append(append(e.text[:e.cursor], added...), tail...)
	e.cursor += len(added)
}

func (e *textEditor) key(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyCtrlA:
		e.selected = true
		return true
	case tcell.KeyCtrlU:
		e.set("", false)
		return true
	case tcell.KeyLeft:
		if e.selected {
			e.cursor = 0
		} else {
			e.cursor = max(0, e.cursor-1)
		}
		e.selected = false
	case tcell.KeyRight:
		if e.selected {
			e.cursor = len(e.text)
		} else {
			e.cursor = min(len(e.text), e.cursor+1)
		}
		e.selected = false
	case tcell.KeyHome:
		e.cursor = 0
		e.selected = false
	case tcell.KeyEnd:
		e.cursor = len(e.text)
		e.selected = false
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if e.selected {
			e.set("", false)
		} else if e.cursor > 0 {
			e.text = append(e.text[:e.cursor-1], e.text[e.cursor:]...)
			e.cursor--
		}
	case tcell.KeyDelete:
		if e.selected {
			e.set("", false)
		} else if e.cursor < len(e.text) {
			e.text = append(e.text[:e.cursor], e.text[e.cursor+1:]...)
		}
	default:
		if ev.Key() != tcell.KeyRune || ev.Modifiers()&(tcell.ModCtrl|tcell.ModAlt|tcell.ModMeta) != 0 {
			return false
		}
		e.insert(string(ev.Rune()))
	}
	return true
}

func (e *textEditor) draw(s tcell.Screen, x, y, width int, focused bool, style tcell.Style) {
	if width < 1 {
		return
	}
	e.start = min(e.start, e.cursor)
	for e.start < e.cursor && runewidth.StringWidth(string(e.text[e.start:e.cursor])) >= width {
		e.start++
	}
	if e.start > len(e.text) {
		e.start = len(e.text)
	}
	if focused && e.selected {
		style = style.Reverse(true)
	}
	drawText(s, x, y, width, string(e.text[e.start:]), style)
	if focused {
		s.ShowCursor(x+min(width-1, runewidth.StringWidth(string(e.text[e.start:e.cursor]))), y)
	}
}
