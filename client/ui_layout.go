package main

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
	"image"
	"strings"
	"unicode"
)

// Every renderer and input path uses this rectangle in terminal cells.
func viewportRect(width, height int) image.Rectangle {
	if width < 3 || height < 5 {
		return image.Rectangle{}
	}
	return image.Rect(1, 2, width-1, height-2)
}

func pagePoint(rect image.Rectangle, cell image.Point, clamp bool) (image.Point, bool) {
	if rect.Empty() || charSize.Width < 1 || charSize.Height < 1 {
		return image.Point{}, false
	}
	if !cell.In(rect) {
		if !clamp {
			return image.Point{}, false
		}
		cell.X = max(rect.Min.X, min(cell.X, rect.Max.X-1))
		cell.Y = max(rect.Min.Y, min(cell.Y, rect.Max.Y-1))
	}
	return image.Pt((cell.X-rect.Min.X)*charSize.Width+charSize.Width/2,
		(cell.Y-rect.Min.Y)*charSize.Height+charSize.Height/2), true
}

func drawText(s tcell.Screen, x, y, width int, text string, style tcell.Style) {
	w, h := s.Size()
	if y < 0 || y >= h || width <= 0 {
		return
	}
	end := min(w, x+width)
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	clusters := uniseg.NewGraphemes(text)
	for clusters.Next() {
		runes := clusters.Runes()
		n := clusters.Width()
		if n == 0 {
			continue
		}
		if x+n > end {
			break
		}
		if x >= 0 {
			s.SetContent(x, y, runes[0], runes[1:], style)
		}
		x += n
	}

}

func fillRect(s tcell.Screen, rect image.Rectangle, style tcell.Style) {
	w, h := s.Size()
	rect = rect.Intersect(image.Rect(0, 0, w, h))
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			s.SetContent(x, y, ' ', nil, style)
		}
	}
}
