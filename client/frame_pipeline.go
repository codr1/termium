package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"io"
	"sync/atomic"
	"time"

	"github.com/mattn/go-sixel"
	"google.golang.org/protobuf/proto"
)

const maxFramePixels = 16 * 1024 * 1024
const maxFrameBytes = 32 * 1024 * 1024
const maxFrameDimension = 16384

// The receiver, preparer, and presenter each own at most one current frame.
// Pending work is replaceable; published frames and their pixels are immutable.
type framePipeline struct {
	pending   chan *Frame
	paused    atomic.Bool
	cost      atomic.Int64
	writeCost atomic.Int64
}

func newFramePipeline() *framePipeline { return &framePipeline{pending: make(chan *Frame, 1)} }
func (p *framePipeline) offer(f *Frame) {
	select {
	case p.pending <- f:
		return
	default:
	}
	select {
	case <-p.pending:
	default:
	}
	select {
	case p.pending <- f:
	default:
	}
}
func (p *framePipeline) interval() time.Duration {
	// Preparation and presentation are separate stages. Pace to the slower
	// stage, with headroom to keep the UI responsive. Never exceed 24 FPS.
	cost := max(p.cost.Load(), p.writeCost.Load())
	return max(time.Second/24, time.Duration(cost)*5/4)
}
func (p *framePipeline) run(ctx context.Context, prepare func(*Frame) (*Frame, error), publish func(*Frame, error)) {
	for {
		select {
		case <-ctx.Done():
			return
		case raw := <-p.pending:
			if p.paused.Load() {
				continue
			}
			start := time.Now()
			frame, err := prepare(raw)
			p.cost.Store(max(int64(time.Since(start)), p.cost.Load()*7/8))
			if ctx.Err() != nil {
				return
			}
			if !p.paused.Load() {
				publish(frame, err)
			}
		}
	}
}

// This object belongs exclusively to the preparation worker. No screen, global
// geometry, or terminal writer is accessed during decoding or quantization.
type framePreparer struct {
	renderer, palette string
	last              *Frame
	encoder           *sixel.Encoder
	buffer            bytes.Buffer
	bands             *BandManager
	bandEncoder       *BandEncoder
}

func (p *framePreparer) prepare(raw *Frame) (*Frame, error) {
	if len(raw.Data) > maxFrameBytes {
		return nil, fmt.Errorf("Screenshot exceeds the 32 MiB limit")
	}
	if p.last != nil && raw.Generation == p.last.Generation && bytes.Equal(raw.Data, p.last.Data) {
		return p.reuse(raw), nil
	}
	dim, _, err := image.DecodeConfig(bytes.NewReader(raw.Data))
	if err != nil {
		return nil, err
	}
	if dim.Width < 1 || dim.Height < 1 || dim.Width > maxFrameDimension || dim.Height > maxFrameDimension || int64(dim.Width)*int64(dim.Height) > maxFramePixels {
		return nil, fmt.Errorf("Screenshot dimensions exceed 16384 pixels per side or 16 megapixels")
	}
	f := &Frame{Data: raw.Data, Generation: raw.Generation, State: raw.State, Width: dim.Width, Height: dim.Height, Timestamp: raw.Timestamp}
	if p.renderer == "kitty" {
		// PNG is already encoded by Chromium. Keep its bytes intact.
		p.last = f
		return f, nil
	}
	img, _, err := image.Decode(bytes.NewReader(raw.Data))
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	if p.last != nil && p.last.Image != nil && rgba.Bounds() == p.last.Image.Bounds() && bytes.Equal(rgba.Pix, p.last.Image.Pix) {
		if f.Generation == p.last.Generation {
			return p.reuse(raw), nil
		}
		f.Image, f.Sixel = p.last.Image, p.last.Sixel
	} else {
		f.Image = rgba
		if p.renderer != "tcell" {
			f.Sixel, err = p.encode(rgba)
			if err != nil {
				return nil, err
			}
		}
	}
	p.last = f
	return f, nil
}

// Pixel reuse must not replay old loading, title, or tab-strip metadata.
func (p *framePreparer) reuse(raw *Frame) *Frame {
	if proto.Equal(raw.State, p.last.State) {
		return p.last
	}
	frame := *p.last
	frame.State, frame.Timestamp = raw.State, raw.Timestamp
	p.last = &frame
	return p.last
}

func (p *framePreparer) encode(img *image.RGBA) ([]byte, error) {
	if p.palette == "websafe" {
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		if p.bands == nil || p.bands.Width != w || p.bands.Height != h {
			p.bands = NewBandManager(w, h)
			p.bandEncoder = NewBandEncoder(sixel.PaletteWebSafe, w, h)
		}
		p.bands.DetectDirtyBands(img)
		bands := make([]string, p.bands.NumBands)
		size := 8192 // Palette and raster header allowance.
		for i := range p.bands.Bands {
			b := &p.bands.Bands[i]
			// A hash can reject equality, never prove it. Without this check a
			// CRC collision could preserve an old band indefinitely.
			if !b.IsDirty {
				previous := p.last
				if previous == nil || previous.Image == nil || previous.Image.Bounds() != img.Bounds() {
					b.IsDirty = true
				} else {
					start, end := b.Y*img.Stride, (b.Y+b.Height)*img.Stride
					b.IsDirty = !bytes.Equal(img.Pix[start:end], previous.Image.Pix[start:end])
				}
			}
			if b.IsDirty || b.CachedRLE == "" {
				encoded, err := p.bandEncoder.EncodeBand(img, b.Y, b.Height)
				if err != nil {
					return nil, err
				}
				b.CachedRLE, b.IsDirty = encoded, false
			}
			bands[i] = b.CachedRLE
			size += len(b.CachedRLE) + 1
			if size > maxFrameBytes {
				return nil, fmt.Errorf("Sixel output exceeds 32 MiB; reduce the terminal size")
			}
		}
		return []byte(ComposeFullSixel(bands, w, h, p.bandEncoder.palette)), nil
	}
	p.buffer.Reset()
	if p.encoder == nil {
		p.encoder = sixel.NewEncoder(boundedFrameWriter{&p.buffer})
		p.encoder.Dither = false
		if p.palette == "plan9" {
			p.encoder.Palette = sixel.PalettePlan9
		}
	}
	if err := p.encoder.Encode(img); err != nil {
		return nil, err
	}
	return bytes.Clone(p.buffer.Bytes()), nil
}

type boundedFrameWriter struct{ buffer *bytes.Buffer }

func (w boundedFrameWriter) Write(data []byte) (int, error) {
	if len(data) > maxFrameBytes-w.buffer.Len() {
		return 0, fmt.Errorf("Sixel output exceeds 32 MiB; reduce the terminal size")
	}
	return w.buffer.Write(data)
}

// All terminal writes stay with the UI owner. Save before positioning, and
// restore even after a payload error. A successful short write is still failure.
func writeSixelFrame(w io.Writer, data []byte, row, column int) error {
	write := func(data []byte) error {
		n, err := w.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
		return err
	}
	if err := write([]byte(fmt.Sprintf("\033[s\033[%d;%dH", row, column))); err != nil {
		return err
	}
	err := write(data)
	restoreErr := write([]byte("\033[u"))
	if err != nil {
		return err
	}
	return restoreErr
}
