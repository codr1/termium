package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/png"
	"io"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-sixel"
)

func pngFrame(t *testing.T, img image.Image, generation uint64) *Frame {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return &Frame{Data: data.Bytes(), Generation: generation}
}

func TestPreparationReusesUnchangedFrameButPreservesDocumentIdentity(t *testing.T) {
	for _, renderer := range []string{"kitty", "sixel", "tcell"} {
		t.Run(renderer, func(t *testing.T) {
			p := &framePreparer{renderer: renderer, palette: "websafe"}
			raw := pngFrame(t, image.NewRGBA(image.Rect(0, 0, 20, 13)), 1)
			first, err := p.prepare(raw)
			if err != nil {
				t.Fatal(err)
			}
			second, err := p.prepare(&Frame{Data: bytes.Clone(raw.Data), Generation: 1})
			if err != nil || first != second {
				t.Fatal("unchanged screenshot prepared twice", err)
			}
			third, err := p.prepare(&Frame{Data: raw.Data, Generation: 2})
			if err != nil || third.Generation != 2 || first.Generation != 1 {
				t.Fatal("document identity lost", err)
			}
		})
	}
}

func TestPreparationKeepsOnlyNewestPendingFrame(t *testing.T) {
	p := newFramePipeline()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, release, done := make(chan uint64, 3), make(chan struct{}), make(chan struct{})
	defer func() { close(release); cancel(); <-done }()
	go func() {
		defer close(done)
		p.run(ctx, func(f *Frame) (*Frame, error) {
			started <- f.Generation
			if f.Generation == 1 {
				<-release
			}
			return f, nil
		}, func(*Frame, error) {})
	}()
	p.offer(&Frame{Generation: 1})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	// Hundreds of captures must not queue behind a blocked encoder.
	for i := 2; i <= 1000; i++ {
		p.offer(&Frame{Generation: uint64(i)})
	}
	s := uiScreen(t, 80, 24)
	kh, _ := recorder()
	kh.globalKey(key(tcell.KeyCtrlL), false)
	kh.HandleKeyEvent(s, tcell.NewEventKey(tcell.KeyRune, 'x', 0))
	if kh.editor.value() != "x" {
		t.Fatal("local editor did not progress during preparation")
	}
	release <- struct{}{}
	select {
	case generation := <-started:
		if generation != 1000 {
			t.Fatalf("prepared obsolete frame %d", generation)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not consume newest frame")
	}
	p.paused.Store(true)
	p.offer(&Frame{Generation: 1001})
	select {
	case <-started:
		t.Fatal("prepared hidden viewport")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestImageDamageIndependentOfChromeAndRestoredAfterOverlay(t *testing.T) {
	oldOverlay := lastOverlay
	t.Cleanup(func() { lastOverlay = oldOverlay })
	lastOverlay = overlayState{}
	s := uiScreen(t, 80, 24)
	oldCfg, oldKH, oldFrames, oldLatest, oldDisplayed, oldHidden, oldPipeline, oldOutput := cfg, keyboardHandler, frames, latestFrame, displayedFrame, graphicsHidden, pipeline, graphicsOutput
	t.Cleanup(func() {
		cfg, keyboardHandler, frames, latestFrame, displayedFrame, graphicsHidden, pipeline, graphicsOutput = oldCfg, oldKH, oldFrames, oldLatest, oldDisplayed, oldHidden, oldPipeline, oldOutput
	})
	cfg = &Config{Renderer: "kitty"}
	keyboardHandler, _ = recorder()
	frames, pipeline = NewFrameBuffer(), newFramePipeline()
	latestFrame, displayedFrame, graphicsHidden = nil, nil, false
	var out bytes.Buffer
	graphicsOutput = &out
	kittyWriter.Reset(&out)
	f := &Frame{Data: []byte("test PNG payload"), Generation: 1, Width: sDims.InnerWidthPx, Height: sDims.InnerHeightPx}
	frames.Publish(f)
	redraw(s)
	if out.Len() == 0 {
		t.Fatal("initial image missing")
	}
	if !bytes.Contains(out.Bytes(), []byte("q=2")) {
		t.Fatal("Kitty error replies may enter keyboard input")
	}
	out.Reset()
	keyboardHandler.openAddress()
	for i := 0; i < 20; i++ {
		keyboardHandler.Draw(s)
		redraw(s)
	}
	if out.Len() != 0 {
		t.Fatal("local UI retransmitted browser pixels")
	}
	keyboardHandler.help = true
	redraw(s)
	if out.String() != kittyDelete || !pipeline.paused.Load() {
		t.Fatal("overlay did not hide graphics and pause capture")
	}
	out.Reset()
	keyboardHandler.help = false
	redraw(s)
	if out.Len() == 0 || pipeline.paused.Load() {
		t.Fatal("closing overlay did not restore image and capture")
	}
	out.Reset()
	keyboardHandler.state.Generation = 2
	redraw(s)
	if out.String() != kittyDelete {
		t.Fatal("old document graphics remained visible")
	}
	out.Reset()
	frames.Publish(f)
	redraw(s)
	if out.Len() != 0 {
		t.Fatal("stale image was repainted")
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestSixelOutputCursorAndErrors(t *testing.T) {
	var out bytes.Buffer
	if err := writeSixelFrame(&out, []byte("pixels"), 3, 2); err != nil {
		t.Fatal(err)
	}
	if out.String() != "\033[s\033[3;2Hpixels\033[u" {
		t.Fatalf("cursor corrupted: %q", out.String())
	}
	for _, failure := range []error{io.ErrClosedPipe, nil} {
		want := failure
		if want == nil {
			want = io.ErrShortWrite
		}
		if err := writeSixelFrame(failingWriter{failure}, []byte("pixels"), 3, 2); !errors.Is(err, want) {
			t.Fatal("write failure lost", err)
		}
		encoder := sixel.NewEncoder(failingWriter{failure})
		encoder.Palette = sixel.PaletteWebSafe
		if err := encoder.Encode(image.NewRGBA(image.Rect(0, 0, 2, 2))); !errors.Is(err, want) {
			t.Fatal("encoder lost writer error", err)
		}
	}
}

func TestSixelPaletteRoundTripAndPartialBands(t *testing.T) {
	for _, name := range []string{"websafe", "plan9"} {
		pal := palette.WebSafe
		if name == "plan9" {
			pal = palette.Plan9
		}
		for h := 7; h <= 11; h++ {
			t.Run(fmt.Sprintf("%s/height%d", name, h), func(t *testing.T) {
				img := image.NewRGBA(image.Rect(0, 0, 1024, h))
				for y := 0; y < h; y++ {
					for x := 0; x < 1024; x++ {
						img.Set(x, y, pal[x%len(pal)])
					}
				}
				p := &framePreparer{renderer: "sixel", palette: name}
				data, err := p.encode(img)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(data, []byte(fmt.Sprintf("q\"1;1;1024;%d#", h))) {
					t.Fatal("wrong raster dimensions")
				}
				if bytes.Contains(data, []byte("#256")) {
					t.Fatal("palette exceeds 256 registers")
				}
				var decoded image.Image
				if err := sixel.NewDecoder(bytes.NewReader(data)).Decode(&decoded); err != nil {
					t.Fatal(err)
				}
				for y := 0; y < h; y++ {
					for x := 0; x < 1024; x++ {
						want := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
						got := color.RGBAModel.Convert(decoded.At(x, y)).(color.RGBA)
						if got.A != 255 || channelDistance(got.R, want.R) > 3 || channelDistance(got.G, want.G) > 3 || channelDistance(got.B, want.B) > 3 {
							t.Fatalf("pixel %d,%d: got %v want %v", x, y, got, want)
						}
					}
				}
			})
		}
	}
}

func TestPacingRespondsToSlowPreparationAndOutput(t *testing.T) {
	p := newFramePipeline()
	p.cost.Store(int64(300 * time.Millisecond))
	if p.interval() < 300*time.Millisecond {
		t.Fatal("capture outruns encoder")
	}
	p.writeCost.Store(int64(500 * time.Millisecond))
	if p.interval() < 500*time.Millisecond {
		t.Fatal("capture outruns output")
	}
}

func channelDistance(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }

func BenchmarkWebsafePreparation(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 800; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 255 / 800), uint8(y * 255 / 600), uint8((x + y) % 256), 255})
		}
	}
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		b.Fatal(err)
	}
	p := &framePreparer{renderer: "sixel", palette: "websafe"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Force a full preparation to measure decode and encode, not deduplication.
		p.last = nil
		p.bands = nil
		if _, err := p.prepare(&Frame{Data: raw.Bytes(), Generation: 1}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnchangedPreparation(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 600))
	var raw bytes.Buffer
	if err := png.Encode(&raw, img); err != nil {
		b.Fatal(err)
	}
	p := &framePreparer{renderer: "sixel", palette: "websafe"}
	frame := &Frame{Data: raw.Bytes(), Generation: 1}
	if _, err := p.prepare(frame); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	frame = &Frame{Data: bytes.Clone(raw.Bytes()), Generation: 1}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.prepare(frame); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBandCacheConfirmsHashMatchesWithPixels(t *testing.T) {
	p := &framePreparer{renderer: "sixel", palette: "websafe"}
	old := image.NewRGBA(image.Rect(0, 0, 12, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 12; x++ {
			old.SetRGBA(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	first, err := p.prepare(pngFrame(t, old, 1))
	if err != nil {
		t.Fatal(err)
	}
	changed := image.NewRGBA(old.Bounds())
	for y := 0; y < 6; y++ {
		for x := 0; x < 12; x++ {
			changed.SetRGBA(x, y, color.RGBA{0, 0, 255, 255})
		}
	}
	// Force the hash-equality branch without depending on a particular collision.
	p.bands.Bands[0].Hash = HashBand(changed, 0, 6, 12)
	second, err := p.prepare(pngFrame(t, changed, 1))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.Sixel, second.Sixel) {
		t.Fatal("hash match concealed changed pixels")
	}
}
