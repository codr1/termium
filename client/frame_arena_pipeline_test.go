package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"runtime"
	"testing"
	"time"

	pb "termium/client/pb"
)

func assertArenasReleased(t *testing.T, p *framePreparer) {
	t.Helper()
	for i, a := range p.arenas.slots {
		if a != nil && a.refs.Load() != 0 {
			t.Errorf("arena %d still has %d owners", i, a.refs.Load())
		}
	}
}

func arenaTestRaw(t *testing.T, n int) *Frame {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 13))
	fillImage(img, color.RGBA{R: byte(n * 31), G: byte(n * 17), B: byte(n * 7), A: 255})
	return pngFrame(t, img, uint64(n+1))
}

type heldPayloadWriter struct {
	started chan struct{}
	resume  chan struct{}
	payload []byte
}

func (w *heldPayloadWriter) Write(b []byte) (int, error) {
	// Position/restore sequences are short. Hold the actual payload until the
	// producer has rotated its other buffers many times.
	if len(b) > 64 {
		close(w.started)
		<-w.resume
		w.payload = bytes.Clone(b)
	}
	return len(b), nil
}

func TestArenaPendingReplacementCannotOverwriteTerminalWrite(t *testing.T) {
	p := &framePreparer{renderer: "sixel", palette: "websafe"}
	fb := NewFrameBuffer()
	first, err := p.prepare(arenaTestRaw(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	fb.Publish(first)
	ui := fb.GetDisplayFrame()
	expected := bytes.Clone(ui.Graphics)
	w := &heldPayloadWriter{started: make(chan struct{}), resume: make(chan struct{})}
	done := make(chan error, 1)
	go func() { done <- writeGraphicsFrame(w, ui.Graphics, 1, 1) }()
	<-w.started
	// Do not abandon a blocked writer even if an assertion fails.
	defer func() {
		close(w.resume)
		if err := <-done; err != nil {
			t.Error(err)
		}
		if !bytes.Equal(expected, w.payload) {
			t.Error("in-flight payload overwritten")
		}
		ui.release()
		fb.Discard()
		p.close()
		assertArenasReleased(t, p)
	}()
	for n := 1; n <= 20; n++ {
		frame, err := p.prepare(arenaTestRaw(t, n))
		if err != nil {
			t.Fatal(err)
		}
		fb.Publish(frame) // superseded pending frames must return their leases
	}
	if !bytes.Equal(expected, ui.Graphics) {
		t.Fatal("display owner lost immutable storage")
	}
	received, _, dropped := fb.GetStats()
	if received != 21 || dropped != 19 {
		t.Fatalf("unbounded or incorrect pending slot: received=%d dropped=%d", received, dropped)
	}
}

func TestArenaExhaustionDropsWorkAndRecovers(t *testing.T) {
	p := &framePreparer{renderer: "sixel", palette: "websafe"}
	var held []*Frame
	defer func() {
		for _, f := range held {
			f.release()
		}
		p.close()
		assertArenasReleased(t, p)
	}()
	for n := 0; n < frameArenaCount; n++ {
		f, err := p.prepare(arenaTestRaw(t, n))
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, f)
	}
	previous := p.last
	if f, err := p.prepare(arenaTestRaw(t, 10)); f != nil || err != errFrameArenaBusy {
		t.Fatalf("pool overflow: frame=%p err=%v", f, err)
	}
	if p.last != previous {
		t.Fatal("dropped frame replaced comparison frame")
	}
	held[0].release()
	held[0] = nil
	f, err := p.prepare(arenaTestRaw(t, 10))
	if err != nil {
		t.Fatal(err)
	}
	f.release()
}

func TestArenaReuseKeepsIndependentLeases(t *testing.T) {
	for _, renderer := range []string{"sixel", "kitty", "tcell"} {
		t.Run(renderer, func(t *testing.T) {
			p := &framePreparer{renderer: renderer, palette: "websafe"}
			raw := arenaTestRaw(t, 0)
			raw.State = &pb.BrowserState{Title: "Loading"}
			first, err := p.prepare(raw)
			if err != nil {
				t.Fatal(err)
			}
			same, err := p.prepare(raw)
			if err != nil {
				t.Fatal(err)
			}
			raw.State = &pb.BrowserState{Title: "Done"}
			metadata, err := p.prepare(raw)
			if err != nil {
				t.Fatal(err)
			}
			if same != first || metadata.storage != first.storage || metadata.State.Title != "Done" || first.State.Title != "Loading" {
				t.Fatal("metadata reuse changed old frame or lost shared ownership")
			}
			if got := first.storage.refs.Load(); got != 4 {
				t.Fatalf("three output owners plus last: got %d", got)
			}
			// For decoded renderers, a new generation with identical pixels also shares
			// storage, while the newly acquired working arena is released.
			raw.Generation++
			generation, err := p.prepare(raw)
			if err != nil {
				t.Fatal(err)
			}
			if renderer != "kitty" && generation.storage != first.storage {
				t.Fatal("identical pixels not shared across generation change")
			}
			first.release()
			same.release()
			metadata.release()
			generation.release()
			p.close()
			assertArenasReleased(t, p)
		})
	}
}

func TestArenaFailedPreparationReleasesWorkingStorage(t *testing.T) {
	p := &framePreparer{renderer: "sixel", palette: "websafe"}
	first, err := p.prepare(arenaTestRaw(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	first.release()
	previous := p.last
	good := arenaTestRaw(t, 1)
	// IHDR is intact so DecodeConfig succeeds and an arena is acquired; image
	// decoding fails because the PNG has no image data.
	bad := &Frame{Data: good.Data[:33], Generation: 2}
	for i := 0; i < 12; i++ {
		if f, err := p.prepare(bad); err == nil {
			f.release()
			t.Fatal("truncated PNG accepted")
		}
	}
	if p.last != previous {
		t.Fatal("decode failure replaced last frame")
	}
	f, err := p.prepare(good)
	if err != nil {
		t.Fatal(err)
	}
	f.release()
	p.close()
	assertArenasReleased(t, p)
}

func TestArenaCompositionFailureDoesNotCommitBandCache(t *testing.T) {
	p := &framePreparer{renderer: "sixel", palette: "websafe"}
	first, err := p.prepare(arenaTestRaw(t, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { first.release(); p.close(); assertArenasReleased(t, p) }()
	cache := make([]string, len(p.bands.Bands))
	for i, b := range p.bands.Bands {
		cache[i] = b.CachedRLE
	}
	changed := image.NewRGBA(first.Image.Bounds())
	fillImage(changed, color.RGBA{R: 255, A: 255})
	injected := errors.New("composition allocation failed")
	if _, err := p.encodeInto(changed, func(int) ([]byte, error) { return nil, injected }); !errors.Is(err, injected) {
		t.Fatalf("allocation error: %v", err)
	}
	for i, b := range p.bands.Bands {
		if b.CachedRLE != cache[i] {
			t.Fatalf("band %d committed before composition succeeded", i)
		}
	}
	if p.last != first {
		t.Fatal("failed composition replaced last")
	}
}

func TestArenaSuppressedPublicationReleasesOutput(t *testing.T) {
	for _, mode := range []string{"cancel", "pause"} {
		t.Run(mode, func(t *testing.T) {
			p := &framePreparer{renderer: "tcell"}
			pipeline := newFramePipeline()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			raw := arenaTestRaw(t, 0)
			var prepared *Frame
			blocked, resume, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			go func() {
				defer close(done)
				pipeline.run(ctx, func(f *Frame) (*Frame, error) {
					frame, err := p.prepare(f)
					prepared = frame
					close(blocked)
					<-resume
					return frame, err
				}, func(f *Frame, err error) { f.release(); t.Error("published suppressed frame") })
			}()
			pipeline.offer(raw)
			<-blocked
			if mode == "pause" {
				pipeline.paused.Store(true)
			} else {
				cancel()
			}
			close(resume)
			if mode == "pause" {
				// Observe the pause path's release before cancellation so this test
				// cannot accidentally cover only the cancellation branch twice.
				deadline := time.Now().Add(5 * time.Second)
				for prepared != nil && prepared.storage.refs.Load() != 1 {
					if time.Now().After(deadline) {
						cancel()
						<-done
						t.Fatal("paused output lease was not released")
					}
					runtime.Gosched()
				}
				cancel()
			}
			<-done
			p.close()
			assertArenasReleased(t, p)
		})
	}
}

func TestArenaRecycledPixelsFullyOverwritePreviousImage(t *testing.T) {
	p := &framePreparer{renderer: "tcell"}
	defer func() { p.close(); assertArenasReleased(t, p) }()
	for n := 0; n < 20; n++ {
		w, h := 19, 13
		if n%2 != 0 {
			w, h = 7, 4
		}
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		// First rotation is opaque; later frames include transparent pixels in
		// previously occupied storage and alternate dimensions.
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				a := byte(255)
				if n >= 4 && (x+y)%2 == 0 {
					a = 0
				}
				img.SetNRGBA(x, y, color.NRGBA{R: 255, G: byte(n * 11), B: 77, A: a})
			}
		}
		f, err := p.prepare(pngFrame(t, img, uint64(n+1)))
		if err != nil {
			t.Fatal(err)
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				want := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
				if got := f.Image.RGBAAt(x, y); got != want {
					t.Fatalf("iteration %d pixel %d,%d: got %v want %v", n, x, y, got, want)
				}
			}
		}
		f.release()
	}
}

func TestArenaUIRetainsForOverlayAndReleasesOnResize(t *testing.T) {
	oldCfg, oldKH, oldFrames, oldLatest, oldDisplayed, oldHidden, oldPipeline, oldOutput, oldOverlay := cfg, keyboardHandler, frames, latestFrame, displayedFrame, graphicsHidden, pipeline, graphicsOutput, lastOverlay
	t.Cleanup(func() {
		latestFrame.release()
		frames.Discard()
		cfg, keyboardHandler, frames, latestFrame, displayedFrame, graphicsHidden, pipeline, graphicsOutput, lastOverlay = oldCfg, oldKH, oldFrames, oldLatest, oldDisplayed, oldHidden, oldPipeline, oldOutput, oldOverlay
	})
	lastOverlay = overlayState{}
	s := uiScreen(t, 80, 24)
	cfg = &Config{Renderer: "kitty"}
	keyboardHandler, _ = recorder()
	frames, pipeline = NewFrameBuffer(), newFramePipeline()
	latestFrame, displayedFrame, graphicsHidden = nil, nil, false
	var out bytes.Buffer
	graphicsOutput = &out
	var pool frameArenaPool
	makeFrame := func() *Frame {
		a := pool.acquire()
		if a == nil {
			t.Fatal("no arena")
		}
		data, err := a.alloc(32)
		if err != nil {
			t.Fatal(err)
		}
		copy(data, "owned terminal payload bytes....")
		return &Frame{storage: a, Graphics: data, Generation: 1, Width: sDims.InnerWidthPx, Height: sDims.InnerHeightPx}
	}
	first := makeFrame()
	frames.Publish(first)
	redraw(s)
	keyboardHandler.help = true
	redraw(s)
	if first.storage.refs.Load() != 1 {
		t.Fatal("overlay released frame needed for redraw")
	}
	keyboardHandler.help = false
	redraw(s)
	if first.storage.refs.Load() != 1 {
		t.Fatal("redraw lost frame ownership")
	}
	second := makeFrame()
	frames.Publish(second)
	redraw(s)
	if first.storage.refs.Load() != 0 {
		t.Fatal("replaced UI frame leaked lease")
	}
	handleResize(s)
	if second.storage.refs.Load() != 0 {
		t.Fatal("resize leaked UI lease")
	}
}
