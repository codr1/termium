package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/color/palette"
	"image/png"
	"io"
	"strings"
	pb "termium/client/pb"
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
	keyboardHandler.snapshotAfter = time.Now()
	f := &Frame{Graphics: []byte("\033_Gq=2;prepared image\033\\"), Generation: 1, Width: sDims.InnerWidthPx, Height: sDims.InnerHeightPx, State: &pb.BrowserState{Generation: 1, Error: "obsolete capture error"}}
	frames.Publish(f)
	redraw(s)
	if out.Len() == 0 {
		t.Fatal("initial image missing")
	}
	if keyboardHandler.state.Error != "" {
		t.Fatal("cached image replayed obsolete navigation state")
	}
	if !bytes.Contains(out.Bytes(), []byte("q=2")) {
		t.Fatal("Kitty error replies may enter keyboard input")
	}
	out.Reset()
	keyboardHandler.state.Loading = true
	redraw(s)
	if pipeline.paused.Load() {
		t.Fatal("loading subresources hid a committed page")
	}
	keyboardHandler.state.Loading = false
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

func TestInvalidationDeletesKittySplashWithoutBrowserFrame(t *testing.T) {
	s := uiScreen(t, 80, 24)
	oldCfg, oldOutput, oldDisplayed := cfg, graphicsOutput, displayedFrame
	t.Cleanup(func() { cfg, graphicsOutput, displayedFrame = oldCfg, oldOutput, oldDisplayed })
	cfg, displayedFrame = &Config{Renderer: "kitty"}, nil
	var out bytes.Buffer
	graphicsOutput = &out
	if err := displayWithKittyRGBA(image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	invalidateGraphics(s)
	if out.String() != kittyDelete {
		t.Fatal("splash placement survived graphics invalidation")
	}
}

func TestPreparationRejectsOversizedDimensionsBeforePixelAllocation(t *testing.T) {
	for _, size := range [][2]uint32{{16385, 1}, {1, 16385}, {4097, 4096}} {
		// A valid IHDR with mismatched pixel data distinguishes the
		// dimension guard from a later decode failure without allocating pixels.
		raw := pngFrame(t, image.NewRGBA(image.Rect(0, 0, 1, 1)), 1)
		binary.BigEndian.PutUint32(raw.Data[16:20], size[0])
		binary.BigEndian.PutUint32(raw.Data[20:24], size[1])
		binary.BigEndian.PutUint32(raw.Data[29:33], crc32.ChecksumIEEE(raw.Data[12:29]))
		for _, renderer := range []string{"kitty", "sixel", "tcell"} {
			p := &framePreparer{renderer: renderer, palette: "websafe"}
			if _, err := p.prepare(raw); err == nil || !strings.Contains(err.Error(), "dimensions") {
				t.Fatalf("%s accepted oversized dimensions %v or reached pixel decoding: %v", renderer, size, err)
			}
		}
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestGraphicsOutputCursorAndErrors(t *testing.T) {
	var out bytes.Buffer
	if err := writeGraphicsFrame(&out, []byte("pixels"), 3, 2); err != nil {
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
		if err := writeGraphicsFrame(failingWriter{failure}, []byte("pixels"), 3, 2); !errors.Is(err, want) {
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

func fillImage(img *image.RGBA, c color.Color) {
	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			img.Set(x, y, c)
		}
	}
}

func fillBand(img *image.RGBA, index int, c color.Color) {
	bounds := img.Bounds()
	y := index * SIXEL_BAND_HEIGHT
	end := min(y+SIXEL_BAND_HEIGHT, bounds.Max.Y)
	for yy := y; yy < end; yy++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			img.Set(x, yy, c)
		}
	}
}

func decodeSixel(t *testing.T, data []byte) image.Image {
	t.Helper()
	var decoded image.Image
	if err := sixel.NewDecoder(bytes.NewReader(data)).Decode(&decoded); err != nil {
		t.Fatalf("decode Sixel: %v", err)
	}
	return decoded
}

func assertSixelPixels(t *testing.T, data []byte, w, h int, want func(x, y int) color.Color) {
	t.Helper()
	img := decodeSixel(t, data)
	bounds := img.Bounds()
	if bounds.Dx() != w || bounds.Dy() != h {
		t.Fatalf("decoded size = %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), w, h)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			got := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			wantC := color.RGBAModel.Convert(want(x, y)).(color.RGBA)
			if got.A != 255 || channelDistance(got.R, wantC.R) > 3 || channelDistance(got.G, wantC.G) > 3 || channelDistance(got.B, wantC.B) > 3 {
				t.Fatalf("pixel %d,%d: got %v want %v", x, y, got, wantC)
			}
		}
	}
}

// TestSixelBandExactComparison verifies per-band dirty detection is exact: a
// single changed pixel re-encodes only its band, unchanged bands are reused
// byte-for-byte, and A -> B -> A reproduces the original output.
func TestSixelBandExactComparison(t *testing.T) {
	const w = 12
	const h = 19 // three full bands plus a one-row final band

	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}

	base := image.NewRGBA(image.Rect(0, 0, w, h))
	fillImage(base, red)

	p := &framePreparer{renderer: "sixel", palette: "websafe"}
	var generation uint64
	prepareNext := func(img *image.RGBA) *Frame {
		generation++
		frame, err := p.prepare(pngFrame(t, img, generation))
		if err != nil {
			t.Fatalf("prepare generation %d: %v", generation, err)
		}
		return frame
	}

	first := prepareNext(base)
	assertSixelPixels(t, first.Graphics, w, h, func(x, y int) color.Color { return red })

	cases := []struct {
		name string
		x, y int
	}{
		{"first band", 0, 0},
		{"middle band", 6, 9},
		{"short final band", w - 1, h - 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			fillImage(img, red)
			img.Set(tc.x, tc.y, blue)

			frame := prepareNext(img)
			assertSixelPixels(t, frame.Graphics, w, h, func(x, y int) color.Color {
				if x == tc.x && y == tc.y {
					return blue
				}
				return red
			})

			back := prepareNext(base)
			if !bytes.Equal(first.Graphics, back.Graphics) {
				t.Fatal("A -> B -> A did not reproduce the original Sixel output")
			}
		})
	}

	t.Run("geometry change", func(t *testing.T) {
		wide := image.NewRGBA(image.Rect(0, 0, 18, 10))
		fillImage(wide, blue)
		assertSixelPixels(t, prepareNext(wide).Graphics, 18, 10, func(x, y int) color.Color { return blue })

		back := prepareNext(base)
		if !bytes.Equal(first.Graphics, back.Graphics) {
			t.Fatal("geometry change and restore did not reproduce the original output")
		}
	})
}

// TestSixelPreparationRecoversAfterEncodeFailure verifies that a preparation
// which fails after partial band work leaves the band cache and p.last
// untouched, so the next frame re-encodes from the last good image instead of
// mixing bands from two different frames.
func TestSixelPreparationRecoversAfterEncodeFailure(t *testing.T) {
	const w = 12
	const h = 117 // nineteen full bands plus a three-row final band

	red := color.RGBA{R: 255, A: 255}
	blue := color.RGBA{B: 255, A: 255}

	base := image.NewRGBA(image.Rect(0, 0, w, h))
	fillImage(base, red)

	p := &framePreparer{renderer: "sixel", palette: "websafe"}
	if _, err := p.prepare(pngFrame(t, base, 1)); err != nil {
		t.Fatalf("prepare A: %v", err)
	}

	// Trip the real output-size check mid-loop: every cached band is oversized,
	// so only a dirty band's genuine re-encoding keeps the total under 32 MiB.
	dummy := strings.Repeat("x", 1_900_000)
	for i := range p.bands.Bands {
		p.bands.Bands[i].CachedRLE = dummy
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillImage(img, red)
	fillBand(img, 0, blue) // first band changed
	fillBand(img, 19, blue) // short final band changed

	if _, err := p.prepare(pngFrame(t, img, 2)); err == nil {
		t.Fatal("expected the size check to fail mid-encode")
	} else if !strings.Contains(err.Error(), "32 MiB") {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := range p.bands.Bands {
		if p.bands.Bands[i].CachedRLE != dummy {
			t.Fatalf("band %d cache changed after failed preparation", i)
		}
	}
	if p.last.Generation != 1 {
		t.Fatalf("p.last advanced to generation %d after failure", p.last.Generation)
	}

	recovery := image.NewRGBA(image.Rect(0, 0, w, h))
	fillImage(recovery, blue) // every band differs from A: a full re-encode fits
	frame, err := p.prepare(pngFrame(t, recovery, 3))
	if err != nil {
		t.Fatalf("recovery prepare: %v", err)
	}
	assertSixelPixels(t, frame.Graphics, w, h, func(x, y int) color.Color { return blue })
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

func TestUnchangedPixelsKeepFreshTabState(t *testing.T) {
	p := framePreparer{renderer: "kitty"}
	raw := pngFrame(t, image.NewRGBA(image.Rect(0, 0, 8, 8)), 1)
	raw.State = &pb.BrowserState{Generation: 1, Title: "Loading", Loading: true}
	first, err := p.prepare(raw)
	if err != nil {
		t.Fatal(err)
	}
	next := *raw
	next.State = &pb.BrowserState{Generation: 1, Title: "Done"}
	second, err := p.prepare(&next)
	if err != nil {
		t.Fatal(err)
	}
	if second.State.Title != "Done" || second.State.Loading || first.State.Title != "Loading" {
		t.Fatal("cached pixels overwrote state or mutated a published frame")
	}
}
