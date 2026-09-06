package main

import (
	"github.com/gdamore/tcell/v2"
	"golang.org/x/image/draw"
	"image"
)

// Two samples per terminal cell preserve both color and the viewport geometry.
// All sampling uses the source bounds, including images with a nonzero origin.
func displayWithTcell(s tcell.Screen, img *image.RGBA) error {
	rect := viewportRect(s.Size())
	if rect.Empty() || img == nil || img.Bounds().Empty() {
		return nil
	}
	sampled := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()*2))
	draw.ApproxBiLinear.Scale(sampled, sampled.Bounds(), img, img.Bounds(), draw.Src, nil)
	for y := 0; y < rect.Dy(); y++ {
		for x := 0; x < rect.Dx(); x++ {
			top, bottom := sampled.RGBAAt(x, 2*y), sampled.RGBAAt(x, 2*y+1)
			fg := tcell.NewRGBColor(int32(top.R), int32(top.G), int32(top.B))
			bg := tcell.NewRGBColor(int32(bottom.R), int32(bottom.G), int32(bottom.B))
			s.SetContent(rect.Min.X+x, rect.Min.Y+y, '▀', nil, tcell.StyleDefault.Foreground(fg).Background(bg))
		}
	}
	return nil
}
