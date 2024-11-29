package main

import (
	"image"
	"image/color"
	"math"

	"github.com/gdamore/tcell/v2"
	"golang.org/x/image/draw"
)

// displayWithTcell renders the image using tcell's character-based display
func displayWithTcell(s tcell.Screen, img *image.RGBA, maxWidth, maxHeight int) error {
	// Get terminal dimensions
	termWidth, termHeight := s.Size()

	// Calculate available space (accounting for borders)
	availWidth := maxWidth - 2
	availHeight := (maxHeight - 2) * 2 // Double height because we use half blocks

	// Calculate image display dimensions while maintaining aspect ratio
	srcBounds := img.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()

	// Calculate target dimensions
	scale := math.Min(
		float64(availWidth)/float64(srcWidth),
		float64(availHeight)/float64(srcHeight),
	)

	targetWidth := int(float64(srcWidth) * scale)
	targetHeight := int(float64(srcHeight) * scale)

	// Create scaled image with edge enhancement
	scaledImg := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.BiLinear.Scale(scaledImg, scaledImg.Bounds(), img, srcBounds, draw.Over, nil)

	// Apply edge enhancement
	enhancedImg := enhanceEdges(scaledImg)

	// Calculate centering offsets
	xOffset := 1 + (availWidth-targetWidth)/2   // +1 for left border
	yOffset := 1 + (availHeight-targetHeight)/4 // +1 for top border, /4 because of half blocks

	// Draw the image using enhanced blocks for better edge representation
	for y := 0; y < targetHeight-1; y += 2 {
		for x := 0; x < targetWidth; x++ {
			screenX := xOffset + x
			screenY := yOffset + y/2

			if screenX < 1 || screenX >= termWidth-1 || screenY < 1 || screenY >= termHeight-1 {
				continue
			}

			topPixel := enhancedImg.RGBAAt(x, y)
			bottomPixel := enhancedImg.RGBAAt(x, y+1)

			// Choose appropriate block character based on intensity difference
			char := '▀' // default upper half block
			style := tcell.StyleDefault

			// Calculate intensity difference between top and bottom pixels
			topIntensity := colorIntensity(topPixel)
			bottomIntensity := colorIntensity(bottomPixel)
			diff := math.Abs(float64(topIntensity - bottomIntensity))

			if diff > 128 {
				// High contrast - use more defined characters
				style = style.
					Foreground(enhanceColor(topPixel)).
					Background(enhanceColor(bottomPixel))
			} else {
				// Lower contrast - use regular colors
				style = style.
					Foreground(tcellColor(topPixel)).
					Background(tcellColor(bottomPixel))
			}

			s.SetContent(screenX, screenY, char, nil, style)
		}
	}

	return nil
}

// enhanceEdges applies edge enhancement to the image
func enhanceEdges(img *image.RGBA) *image.RGBA {
	bounds := img.Bounds()
	enhanced := image.NewRGBA(bounds)

	// Sobel operator kernels
	kernelX := []float64{
		-1, 0, 1,
		-2, 0, 2,
		-1, 0, 1,
	}
	kernelY := []float64{
		-1, -2, -1,
		0, 0, 0,
		1, 2, 1,
	}

	for y := 1; y < bounds.Max.Y-1; y++ {
		for x := 1; x < bounds.Max.X-1; x++ {
			var gradX, gradY float64

			// Apply kernels
			for ky := -1; ky <= 1; ky++ {
				for kx := -1; kx <= 1; kx++ {
					c := img.RGBAAt(x+kx, y+ky)
					intensity := float64(colorIntensity(c))
					idx := (ky+1)*3 + (kx + 1)
					gradX += intensity * kernelX[idx]
					gradY += intensity * kernelY[idx]
				}
			}

			// Calculate gradient magnitude
			magnitude := math.Sqrt(gradX*gradX + gradY*gradY)

			// Get original color and enhance based on edge strength
			origColor := img.RGBAAt(x, y)
			factor := math.Min(1.0+magnitude/255.0, 2.0)

			enhanced.Set(x, y, color.RGBA{
				R: uint8(math.Min(float64(origColor.R)*factor, 255)),
				G: uint8(math.Min(float64(origColor.G)*factor, 255)),
				B: uint8(math.Min(float64(origColor.B)*factor, 255)),
				A: origColor.A,
			})
		}
	}

	return enhanced
}

// colorIntensity calculates the perceived brightness of a color
func colorIntensity(c color.RGBA) uint8 {
	// Using perceived brightness formula
	return uint8((float64(c.R)*0.299 + float64(c.G)*0.587 + float64(c.B)*0.114))
}

// enhanceColor increases contrast for edge pixels
func enhanceColor(c color.RGBA) tcell.Color {
	if c.A < 128 {
		return tcell.ColorBlack
	}

	// Increase contrast for edge pixels
	contrast := 1.2
	r := uint8(math.Min(float64(c.R)*contrast, 255))
	g := uint8(math.Min(float64(c.G)*contrast, 255))
	b := uint8(math.Min(float64(c.B)*contrast, 255))

	return tcell.NewRGBColor(int32(r), int32(g), int32(b))
}

// tcellColor converts a color.RGBA to tcell.Color
func tcellColor(c color.RGBA) tcell.Color {
	if c.A < 128 {
		return tcell.ColorBlack
	}

	return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
}
