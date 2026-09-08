package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
