package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"reflect"
	"testing"

	sixel "github.com/mattn/go-sixel"
)

// Reuse must preserve the fresh encoder's output across changing geometry,
// palette, alpha and source stride, for both public encoding entry points.
func TestSixelQuantizationReuseMatchesFresh(t *testing.T) {
	for _, pixelsOnly := range []bool{false, true} {
		for _, dither := range []bool{false, true} {
			t.Run(fmt.Sprintf("pixels=%t/dither=%t", pixelsOnly, dither), func(t *testing.T) {
				var got bytes.Buffer
				reused := sixel.NewEncoder(&got)
				check := func(name string, img image.Image, pal sixel.PaletteType) {
					t.Helper()
					got.Reset()
					var want bytes.Buffer
					fresh := sixel.NewEncoder(&want)
					for _, enc := range []*sixel.Encoder{reused, fresh} {
						enc.Palette, enc.Dither = pal, dither
						var err error
						if pixelsOnly && pal != sixel.PaletteAdaptive {
							err = enc.EncodePixelData(img)
						} else {
							err = enc.Encode(img)
						}
						if err != nil {
							t.Fatalf("%s: %v", name, err)
						}
					}
					if !bytes.Equal(got.Bytes(), want.Bytes()) {
						t.Fatalf("%s palette=%d: reused output differs from fresh", name, pal)
					}
				}
				for _, pal := range []sixel.PaletteType{sixel.PaletteWebSafe, sixel.PalettePlan9, sixel.PaletteWebSafe} {
					check("warm", quantizationPattern(image.Rect(0, 0, 36, 12)), pal)
					for h := 1; h <= 5; h++ {
						check(fmt.Sprintf("short-%d", h), quantizationPattern(image.Rect(0, 0, 12, h)), pal)
						check("full-again", quantizationPattern(image.Rect(0, 0, 36, 6)), pal)
					}
					for _, r := range []image.Rectangle{image.Rect(0, 0, 48, 6), image.Rect(0, 0, 96, 6), image.Rect(0, 0, 17, 13), image.Rect(2, 1, 15, 6), image.Rect(-2, -1, 11, 5)} {
						check(r.String(), quantizationPattern(r), pal)
					}
					parent := quantizationPattern(image.Rect(0, 0, 37, 9))
					check("padded-stride", parent.SubImage(image.Rect(0, 0, 21, 6)), pal)
					check("transparent", image.NewRGBA(image.Rect(0, 0, 12, 6)), pal)
					check("opaque-again", quantizationPattern(image.Rect(0, 0, 12, 6)), pal)
					check("empty", image.NewRGBA(image.Rectangle{}), pal)
					// Adaptive conversion must not adopt or poison fixed scratch.
					check("adaptive-interlude", quantizationPattern(image.Rect(0, 0, 9, 7)), sixel.PaletteAdaptive)
				}
			})
		}
	}
}

func quantizationPattern(r image.Rectangle) *image.RGBA {
	img := image.NewRGBA(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			u, v := x-r.Min.X, y-r.Min.Y
			c := color.NRGBA{R: byte(u*37 + v*11), G: byte(u*19 + v*53), B: byte(u*71 + v*7), A: 255}
			if (u+v)%7 == 0 {
				c.A = 0
			}
			if (u+v)%7 == 1 {
				c.A = 127
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// Known colors supply an independent pixel oracle, including Plan9 register 255.
func TestSixelQuantizationReuseKnownPixels(t *testing.T) {
	var out bytes.Buffer
	enc := sixel.NewEncoder(&out)
	for _, dither := range []bool{false, true} {
		for _, pal := range []sixel.PaletteType{sixel.PaletteWebSafe, sixel.PalettePlan9, sixel.PaletteWebSafe} {
			for _, size := range []image.Point{{32, 12}, {7, 1}, {19, 5}, {11, 6}} {
				img := image.NewRGBA(image.Rectangle{Max: size})
				for y := 0; y < size.Y; y++ {
					for x := 0; x < size.X; x++ {
						var c color.Color = palette.WebSafe[(x*13+y*37)%len(palette.WebSafe)]
						if pal == sixel.PalettePlan9 {
							c = palette.Plan9[255]
						}
						img.Set(x, y, c)
					}
				}
				enc.Palette, enc.Dither = pal, dither
				out.Reset()
				if err := enc.Encode(img); err != nil {
					t.Fatal(err)
				}
				assertSixelPixels(t, out.Bytes(), size.X, size.Y, img.At)
			}
		}
	}
}

func TestSixelQuantizationDoesNotAdoptBorrowedImage(t *testing.T) {
	pal := color.Palette{color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	parent := image.NewPaletted(image.Rect(0, 0, 13, 5), pal)
	for i := range parent.Pix {
		parent.Pix[i] = byte(i % 2)
	}
	borrowed := parent.SubImage(image.Rect(0, 0, 7, 3)).(*image.Paletted)
	original := append([]byte(nil), parent.Pix...)
	originalPalette := append(color.Palette(nil), pal...)
	rect, stride := borrowed.Rect, borrowed.Stride
	var out bytes.Buffer
	enc := sixel.NewEncoder(&out)
	enc.Palette = sixel.PaletteWebSafe
	if err := enc.Encode(quantizationPattern(image.Rect(0, 0, 36, 12))); err != nil {
		t.Fatal(err)
	}
	var borrowedOutput []byte
	for i := 0; i < 3; i++ {
		out.Reset()
		if err := enc.Encode(borrowed); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			borrowedOutput = append([]byte(nil), out.Bytes()...)
		} else if !bytes.Equal(out.Bytes(), borrowedOutput) {
			t.Fatal("borrowed image output changed")
		}
		out.Reset()
		if err := enc.Encode(quantizationPattern(image.Rect(0, 0, 3+i*27, 2+i*7))); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(parent.Pix, original) || !reflect.DeepEqual(borrowed.Palette, originalPalette) || borrowed.Rect != rect || borrowed.Stride != stride {
			t.Fatal("encoder mutated caller-owned paletted input")
		}
	}
}
