package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

// Isolate band comparison/encoding from image decoding and terminal output.
// Alternate actual changed frames while retaining the previous frame's cache.
func BenchmarkSixelBandChanges(b *testing.B) {
	for _, scene := range []string{"one-pixel", "all-pixels"} {
		b.Run(scene, func(b *testing.B) {
			frames := [2]*image.RGBA{}
			for n := range frames {
				img := image.NewRGBA(image.Rect(0, 0, 1920, 1081))
				for y := 0; y < img.Rect.Dy(); y++ {
					for x := 0; x < img.Rect.Dx(); x++ {
						c := color.RGBA{R: 255, A: 255}
						if n == 1 && (scene == "all-pixels" || (x == 1919 && y == 1080)) {
							c = color.RGBA{B: 255, A: 255}
						}
						img.SetRGBA(x, y, c)
					}
				}
				frames[n] = img
			}
			p := framePreparer{renderer: "sixel", palette: "websafe"}
			if _, err := p.encode(frames[1]); err != nil {
				b.Fatal(err)
			}
			p.last = &Frame{Image: frames[1]}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				img := frames[i%2]
				if _, err := p.encode(img); err != nil {
					b.Fatal(err)
				}
				p.last = &Frame{Image: img}
			}
		})
	}
}

// Generate real Chromium captures with npm run benchmark:capture, then point
// TERMIUM_BENCH_CAPTURE_DIR at that output. Both renderers consume the same files.
func BenchmarkRendererPreparation(b *testing.B) {
	dir := os.Getenv("TERMIUM_BENCH_CAPTURE_DIR")
	if dir == "" {
		b.Skip("set TERMIUM_BENCH_CAPTURE_DIR to benchmark:capture output")
	}
	for _, scene := range []string{"text", "canvas"} {
		for _, source := range []string{"jpeg.jpg", "fast-png.png"} {
			data, err := os.ReadFile(filepath.Join(dir, scene+"-"+source))
			if err != nil {
				b.Fatal(err)
			}
			for _, renderer := range []string{"kitty", "sixel"} {
				b.Run(scene+"/"+source+"/"+renderer, func(b *testing.B) {
					p := framePreparer{renderer: renderer, palette: "websafe"}
					raw := &Frame{Data: data, Generation: 1}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						// Full changed-frame work; keep compression scratch warm.
						p.last = nil
						frame, err := p.prepare(raw)
						if err != nil {
							b.Fatal(err)
						}
						b.ReportMetric(float64(len(frame.Graphics)), "payload-B")
					}
				})
			}
		}
	}
}
