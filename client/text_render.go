package main

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/gdamore/tcell/v2"
	"golang.org/x/image/draw"
)

// Unicode block elements
const (
	FULL_BLOCK           = '█'
	UPPER_HALF_BLOCK     = '▀'
	LOWER_HALF_BLOCK     = '▄'
	LEFT_HALF_BLOCK      = '▌'
	RIGHT_HALF_BLOCK     = '▐'
	LIGHT_SHADE          = '░'
	MEDIUM_SHADE         = '▒'
	DARK_SHADE           = '▓'
	QUADRANT_UPPER_LEFT  = '▘'
	QUADRANT_UPPER_RIGHT = '▝'
	QUADRANT_LOWER_LEFT  = '▖'
	QUADRANT_LOWER_RIGHT = '▗'
)

func displayWithTcell(s tcell.Screen, img *image.RGBA) error {
	srcWidth := img.Bounds().Dx()
	srcHeight := img.Bounds().Dy()

	// Account for character cell aspect ratio
	charAspect := float64(charSize.Height) / float64(charSize.Width)

	// Calculate scaling while preserving image aspect ratio and accounting for character cells
	scale := math.Min(
		float64(sDims.UsableWidth)/float64(srcWidth),
		float64(sDims.UsableBrowserHeight)*charAspect/float64(srcHeight),
	)

	Debug(
		fmt.Sprintf(
			"Aspect %v\n Avail W: %v\n Avail H: %v\n srcWidth %v\n srcHeight %v\n Scale %v",
			charAspect,
			sDims.UsableWidth,
			sDims.UsableBrowserHeight,
			srcWidth,
			srcHeight,
			scale,
		),
		DEBUG,
	)

	targetWidth := int(float64(srcWidth) * scale * charAspect)
	targetHeight := int(float64(srcHeight) * scale)

	// Scale image
	scaledImg := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	draw.BiLinear.Scale(scaledImg, scaledImg.Bounds(), img, img.Bounds(), draw.Over, nil)

	// Enhance contrast and saturation
	enhanceImage(scaledImg)

	// Center the image
	// Since we process 2x2 blocks, divide target dimensions by 2 to get character cell count
	xOffset := H_BORDER_WIDTH + (sDims.UsableWidth-targetWidth/2)/2
	yOffset := V_BORDER_WIDTH + (sDims.UsableBrowserHeight-targetHeight/2)/2

	// Process image in 2x2 blocks
	for y := 0; y < targetHeight-1; y += 2 {
		for x := 0; x < targetWidth-1; x += 2 {
			screenX := xOffset + x/2
			screenY := yOffset + y/2

			if screenX < H_BORDER_WIDTH || screenX >= sDims.Width-H_BORDER_WIDTH ||
				screenY < V_BORDER_WIDTH || screenY >= sDims.LogPanelY-V_BORDER_WIDTH {

				continue
			}

			// Get colors for 2x2 block
			topLeft := scaledImg.RGBAAt(x, y)
			topRight := scaledImg.RGBAAt(x+1, y)
			bottomLeft := scaledImg.RGBAAt(x, y+1)
			bottomRight := scaledImg.RGBAAt(x+1, y+1)

			char, style := chooseBestChar(topLeft, topRight, bottomLeft, bottomRight)
			s.SetContent(screenX, screenY, char, nil, style)
		}
	}

	return nil
}

func enhanceImage(img *image.RGBA) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)

			// Increase contrast
			r := enhanceChannel(c.R)
			g := enhanceChannel(c.G)
			b := enhanceChannel(c.B)

			// Increase saturation
			h, s, v := rgbToHsv(r, g, b)
			s = math.Min(s*1.5, 1.0) // Increase saturation by 50%
			r, g, b = hsvToRgb(h, s, v)

			img.Set(x, y, color.RGBA{r, g, b, c.A})
		}
	}
}

func chooseBestChar(tl, tr, bl, br color.RGBA) (rune, tcell.Style) {
	// Calculate intensities
	tlI := colorIntensity(tl)
	trI := colorIntensity(tr)
	blI := colorIntensity(bl)
	brI := colorIntensity(br)

	vertDiff := math.Abs(float64(tlI + trI - blI - brI))
	horizDiff := math.Abs(float64(tlI + blI - trI - brI))
	diagDiff1 := math.Abs(float64(tlI + brI - trI - blI))
	diagDiff2 := math.Abs(float64(trI + blI - tlI - brI))

	// Choose foreground and background colors with enhanced contrast
	fgColor := dominantColor([]color.RGBA{tl, tr, bl, br})
	bgColor := averageColor([]color.RGBA{tl, tr, bl, br})

	style := tcell.StyleDefault.
		Foreground(tcellColor(fgColor)).
		Background(tcellColor(bgColor))

	// Higher thresholds for more pronounced patterns
	const threshold = 30.0

	// Choose character based on intensity patterns
	if vertDiff > threshold && vertDiff > horizDiff && vertDiff > diagDiff1 && vertDiff > diagDiff2 {
		if tlI+trI > blI+brI {
			return UPPER_HALF_BLOCK, style
		}
		return LOWER_HALF_BLOCK, style
	}

	if horizDiff > threshold && horizDiff > vertDiff && horizDiff > diagDiff1 && horizDiff > diagDiff2 {
		if tlI+blI > trI+brI {
			return LEFT_HALF_BLOCK, style
		}
		return RIGHT_HALF_BLOCK, style
	}

	if diagDiff1 > threshold || diagDiff2 > threshold {
		if tlI > trI && tlI > blI && tlI > brI {
			return QUADRANT_UPPER_LEFT, style
		}
		if trI > tlI && trI > blI && trI > brI {
			return QUADRANT_UPPER_RIGHT, style
		}
		if blI > tlI && blI > trI && blI > brI {
			return QUADRANT_LOWER_LEFT, style
		}
		if brI > tlI && brI > trI && brI > blI {
			return QUADRANT_LOWER_RIGHT, style
		}
	}

	// Use shading for gradients
	avgIntensity := (tlI + trI + blI + brI) / 4
	if avgIntensity < 85 {
		return DARK_SHADE, style
	}
	if avgIntensity < 170 {
		return MEDIUM_SHADE, style
	}
	if avgIntensity < 255 {
		return LIGHT_SHADE, style
	}

	return FULL_BLOCK, style
}

func enhanceChannel(c uint8) uint8 {
	// Increase contrast using a sigmoid-like function
	x := float64(c) / 255.0
	x = (x-0.5)*1.5 + 0.5 // Increase contrast
	x = math.Max(0, math.Min(1, x))
	return uint8(x * 255)
}

func dominantColor(colors []color.RGBA) color.RGBA {
	var maxIntensity uint8
	var dominant color.RGBA

	for _, c := range colors {
		intensity := colorIntensity(c)
		if intensity > maxIntensity {
			maxIntensity = intensity
			dominant = c
		}
	}

	// Enhance the dominant color
	return color.RGBA{
		R: enhanceChannel(dominant.R),
		G: enhanceChannel(dominant.G),
		B: enhanceChannel(dominant.B),
		A: dominant.A,
	}
}

func averageColor(colors []color.RGBA) color.RGBA {
	var r, g, b, a uint32
	for _, c := range colors {
		r += uint32(c.R)
		g += uint32(c.G)
		b += uint32(c.B)
		a += uint32(c.A)
	}
	n := uint32(len(colors))
	return color.RGBA{
		R: uint8(r / n),
		G: uint8(g / n),
		B: uint8(b / n),
		A: uint8(a / n),
	}
}

func colorIntensity(c color.RGBA) uint8 {
	// Weighted RGB for perceived brightness
	return uint8((float64(c.R)*0.299 + float64(c.G)*0.587 + float64(c.B)*0.114))
}

func tcellColor(c color.RGBA) tcell.Color {
	return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
}

// HSV conversion helpers
func rgbToHsv(r, g, b uint8) (float64, float64, float64) {
	rf := float64(r) / 255.0
	gf := float64(g) / 255.0
	bf := float64(b) / 255.0

	max := math.Max(math.Max(rf, gf), bf)
	min := math.Min(math.Min(rf, gf), bf)
	delta := max - min

	var h float64
	if delta == 0 {
		h = 0
	} else if max == rf {
		h = 60 * ((gf - bf) / delta)
	} else if max == gf {
		h = 60 * (2 + (bf-rf)/delta)
	} else {
		h = 60 * (4 + (rf-gf)/delta)
	}

	if h < 0 {
		h += 360
	}

	s := 0.0
	if max != 0 {
		s = delta / max
	}

	return h, s, max
}

func hsvToRgb(h, s, v float64) (uint8, uint8, uint8) {
	var rf, gf, bf float64

	i := int(math.Floor(h/60)) % 6
	f := h/60 - math.Floor(h/60)
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)

	switch int(i) {
	case 0:
		rf, gf, bf = v, t, p
	case 1:
		rf, gf, bf = q, v, p
	case 2:
		rf, gf, bf = p, v, t
	case 3:
		rf, gf, bf = p, q, v
	case 4:
		rf, gf, bf = t, p, v
	case 5:
		rf, gf, bf = v, p, q
	}

	return uint8(rf * 255), uint8(gf * 255), uint8(bf * 255)
}
