package sixel

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"io"
	"strconv"

	"github.com/soniakeys/quant/median"
)

// PaletteType represents the color palette strategy
type PaletteType int

const (
	PaletteAdaptive PaletteType = iota
	PaletteWebSafe
	PalettePlan9
)

// CachedPaletteIndex caches palette.Index lookups for faster color quantization
type CachedPaletteIndex struct {
	palette         color.Palette
	cache           map[uint32]uint8
	websafe         bool
	hits            uint64
	misses          uint64
	lastFrameHits   uint64
	lastFrameMisses uint64
}

// writeInt writes an integer to the buffer and returns the number of bytes written
func writeInt(buf []byte, n int) int {
	return len(strconv.AppendInt(buf[:0], int64(n), 10))
}

func NewCachedPaletteIndex(p color.Palette) *CachedPaletteIndex {
	return &CachedPaletteIndex{
		palette: p,
		cache:   make(map[uint32]uint8, 50000),
	}
}

func (c *CachedPaletteIndex) Index(col color.Color) uint8 {
	r, g, b, a := col.RGBA()
	if c.websafe && a == 0xffff {
		return uint8(((r>>8)+25)/51*36 + ((g>>8)+25)/51*6 + ((b>>8)+25)/51)
	}
	key := (r>>8)<<24 | (g>>8)<<16 | (b>>8)<<8 | (a >> 8)

	if idx, ok := c.cache[key]; ok {
		c.hits++
		return idx
	}

	c.misses++
	idx := uint8(c.palette.Index(col))
	if len(c.cache) >= 65536 {
		clear(c.cache)
	}
	c.cache[key] = idx
	return idx
}

// GetCacheStats returns cache statistics for the last frame
func (c *CachedPaletteIndex) GetCacheStats() (hits, misses uint64, hitRate float64) {
	if c.lastFrameHits+c.lastFrameMisses == 0 {
		return 0, 0, 0.0
	}
	total := c.lastFrameHits + c.lastFrameMisses
	rate := float64(c.lastFrameHits) * 100.0 / float64(total)
	return c.lastFrameHits, c.lastFrameMisses, rate
}

// cachedDraw draws src onto dst using cached palette lookups
func (e *Encoder) cachedDraw(dst *image.Paletted, r image.Rectangle, src image.Image, sp image.Point, paletteType PaletteType) {
	var cached *CachedPaletteIndex

	// Reuse an encoder-owned, bounded cache for fixed palettes
	if paletteType == PaletteWebSafe || paletteType == PalettePlan9 {
		if e.cache == nil || e.cachePalette != paletteType {
			e.cache = NewCachedPaletteIndex(dst.Palette)
			e.cachePalette = paletteType
			e.cache.websafe = paletteType == PaletteWebSafe
		}
		cached = e.cache
	} else {
		// For adaptive palette, create new cache each time (for now)
		cached = NewCachedPaletteIndex(dst.Palette)
	}

	startHits := cached.hits
	startMisses := cached.misses

	bounds := r.Intersect(dst.Bounds())

	// Optimize for RGBA which is the most common case
	if img, ok := src.(*image.RGBA); ok {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				srcX := x - bounds.Min.X + sp.X
				srcY := y - bounds.Min.Y + sp.Y
				c := img.RGBAAt(srcX, srcY)
				var idx uint8
				if paletteType == PaletteWebSafe && c.A == 255 {
					// Avoid boxing a color interface for every screenshot pixel.
					idx = uint8((int(c.R)+25)/51*36 + (int(c.G)+25)/51*6 + (int(c.B)+25)/51)
				} else {
					idx = cached.Index(c)
				}
				dst.SetColorIndex(x, y, idx)
			}
		}
	} else {
		// Fallback for other image types
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				srcX := x - bounds.Min.X + sp.X
				srcY := y - bounds.Min.Y + sp.Y
				c := src.At(srcX, srcY)
				idx := cached.Index(c)
				dst.SetColorIndex(x, y, idx)
			}
		}
	}

	// Store frame stats for later retrieval
	cached.lastFrameHits = cached.hits - startHits
	cached.lastFrameMisses = cached.misses - startMisses
}

// Encoder encode image to sixel format
type Encoder struct {
	cache        *CachedPaletteIndex
	cachePalette PaletteType
	w            io.Writer

	// Dither, if true, will dither the image when generating a paletted version
	// using the Floyd–Steinberg dithering algorithm.
	Dither bool

	// Width is the maximum width to draw to.
	Width int
	// Height is the maximum height to draw to.
	Height int

	// Colors sets the number of colors for the encoder to quantize if needed.
	// If the value is below 2 (e.g. the zero value), then 255 is used.
	// A color is always reserved for alpha, so 2 colors give you 1 color.
	Colors int

	// Palette specifies the color palette strategy to use
	Palette PaletteType

	// Reusable buffers to avoid allocations
	buf  []byte
	cset []bool
}

// GetCacheStats returns the cache statistics from this encoder’s palette cache (if using fixed palettes)
func (e *Encoder) GetCacheStats() (hits, misses uint64, hitRate float64) {
	if e.Palette == PaletteWebSafe || e.Palette == PalettePlan9 {
		if e.cache != nil {
			return e.cache.GetCacheStats()
		}
	}
	return 0, 0, 0.0
}

// NewEncoder return new instance of Encoder
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

const (
	specialChNr = byte(0x6d)
	specialChCr = byte(0x64)
)

// Encode do encoding
func (e *Encoder) Encode(img image.Image) error {
	nc := e.Colors // (>= 2, 8bit, index 0 is reserved for transparent key color)
	if nc < 2 {
		nc = 255
	}

	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width == 0 || height == 0 {
		return nil
	}
	if e.Width > 0 {
		width = e.Width
	}
	if e.Height > 0 {
		height = e.Height
	}
	var paletted *image.Paletted

	// fast path for paletted images
	if p, ok := img.(*image.Paletted); ok && len(p.Palette) < int(nc) {
		paletted = p
	} else {
		switch e.Palette {
		case PaletteWebSafe:
			paletted = image.NewPaletted(img.Bounds(), palette.WebSafe)
			nc = len(palette.WebSafe) + 1 // Adjust nc for fixed palette
			if e.Dither {
				draw.FloydSteinberg.Draw(paletted, img.Bounds(), img, image.Point{})
			} else {
				e.cachedDraw(paletted, img.Bounds(), img, image.Point{}, PaletteWebSafe)
			}

		case PalettePlan9:
			paletted = image.NewPaletted(img.Bounds(), palette.Plan9)
			nc = len(palette.Plan9) + 1 // Adjust nc for fixed palette
			if e.Dither {
				draw.FloydSteinberg.Draw(paletted, img.Bounds(), img, image.Point{})
			} else {
				e.cachedDraw(paletted, img.Bounds(), img, image.Point{}, PalettePlan9)
			}

		default: // PaletteAdaptive
			q := median.Quantizer(nc - 1)
			paletted = q.Paletted(img)

			if e.Dither {
				draw.FloydSteinberg.Draw(paletted, img.Bounds(), img, image.Point{})
			} else {
				e.cachedDraw(paletted, img.Bounds(), img, image.Point{}, PaletteAdaptive)
			}
		}
	}

	w := &checkedWriter{writer: e.w}
	// DECSIXEL Introducer(\033P0;0;8q) + DECGRA ("Pan;Pad;Ph;Pv): Set Raster Attributes
	// Format: ESC P 0;0;8 q " Pan;Pad;Ph;Pv
	// Pan=1 (pixel aspect ratio numerator), Pad=1 (denominator), Ph=width, Pv=height
	var headerBuf [64]byte
	idx := 0
	// ESC P 0;0;8q"1;1;
	copy(headerBuf[idx:], []byte{0x1b, 0x50, 0x30, 0x3b, 0x30, 0x3b, 0x38, 0x71, 0x22, 0x31, 0x3b, 0x31, 0x3b})
	idx += 13
	// width
	idx += writeInt(headerBuf[idx:], width)
	headerBuf[idx] = ';'
	idx++
	// height
	idx += writeInt(headerBuf[idx:], height)
	w.Write(headerBuf[:idx])

	// Pre-allocate a buffer for color palette
	var palBuf [32]byte
	for n, v := range paletted.Palette {
		r, g, b, _ := v.RGBA()
		r = r * 100 / 0xFFFF
		g = g * 100 / 0xFFFF
		b = b * 100 / 0xFFFF
		// DECGCI (#): Graphics Color Introducer
		// Build the color string manually to avoid fmt overhead
		palBuf[0] = '#'
		idx := 1
		idx += writeInt(palBuf[idx:], n)
		palBuf[idx] = ';'
		idx++
		palBuf[idx] = '2'
		idx++
		palBuf[idx] = ';'
		idx++
		idx += writeInt(palBuf[idx:], int(r))
		palBuf[idx] = ';'
		idx++
		idx += writeInt(palBuf[idx:], int(g))
		palBuf[idx] = ';'
		idx++
		idx += writeInt(palBuf[idx:], int(b))
		w.Write(palBuf[:idx])
	}

	// Reuse or resize buffers as needed
	bufSize := width * nc
	if cap(e.buf) < bufSize {
		// Allocate with some extra capacity to avoid frequent reallocations
		e.buf = make([]byte, bufSize, bufSize*2)
	} else {
		e.buf = e.buf[:bufSize]
	}

	if cap(e.cset) < nc {
		e.cset = make([]bool, nc, nc*2)
	} else {
		e.cset = e.cset[:nc]
	}
	ch0 := specialChNr
	rgba, _ := img.(*image.RGBA)
	for z := 0; z < (height+5)/6; z++ {
		// DECGNL (-): Graphics Next Line
		if z > 0 {
			w.Write([]byte{0x2d})
		}
		// Clear the buffer using the optimized builtin
		clear(e.buf)
		// For cset, we need all true values, not zero values
		// The doubling copy trick is fastest for this
		if len(e.cset) > 0 {
			e.cset[0] = true
			for i := 1; i < len(e.cset); i *= 2 {
				copy(e.cset[i:], e.cset[:i])
			}
		}
		for p := 0; p < 6; p++ {
			y := z*6 + p
			for x := 0; x < width; x++ {
				var opaque bool
				if rgba != nil {
					opaque = rgba.RGBAAt(x, y).A != 0
				} else {
					_, _, _, a := img.At(x, y).RGBA()
					opaque = a != 0
				}
				if opaque {
					idx := int(paletted.ColorIndexAt(x, y)) + 1
					e.cset[idx] = false // mark as used
					e.buf[width*int(idx)+x] |= 1 << uint(p)
				}
			}
		}
		for n := 1; n < nc; n++ {
			if e.cset[n] {
				continue
			}
			e.cset[n] = true
			// DECGCR ($): Graphics Carriage Return
			if ch0 == specialChCr {
				w.Write([]byte{0x24})
			}
			// Register zero is valid; transparency is only an internal slot.
			var selection [8]byte
			selection[0] = '#'
			digits := writeInt(selection[1:], n-1)
			w.Write(selection[:digits+1])
			cnt := 0
			for x := 0; x < width; x++ {
				// make sixel character from 6 pixels
				ch := e.buf[width*n+x]
				if ch0 < 0x40 && ch != ch0 {
					// output sixel character
					s := 63 + ch0
					for ; cnt > 255; cnt -= 255 {
						w.Write([]byte{0x21, 0x32, 0x35, 0x35, s})
					}
					if cnt == 1 {
						w.Write([]byte{s})
					} else if cnt == 2 {
						w.Write([]byte{s, s})
					} else if cnt == 3 {
						w.Write([]byte{s, s, s})
					} else if cnt >= 100 {
						digit1 := cnt / 100
						digit2 := (cnt - digit1*100) / 10
						digit3 := cnt % 10
						c1 := byte(0x30 + digit1)
						c2 := byte(0x30 + digit2)
						c3 := byte(0x30 + digit3)
						// DECGRI (!): - Graphics Repeat Introducer
						w.Write([]byte{0x21, c1, c2, c3, s})
					} else if cnt >= 10 {
						c1 := byte(0x30 + cnt/10)
						c2 := byte(0x30 + cnt%10)
						// DECGRI (!): - Graphics Repeat Introducer
						w.Write([]byte{0x21, c1, c2, s})
					} else if cnt > 0 {
						// DECGRI (!): - Graphics Repeat Introducer
						w.Write([]byte{0x21, byte(0x30 + cnt), s})
					}
					cnt = 0
				}
				ch0 = ch
				cnt++
			}
			if ch0 != 0 {
				// output sixel character
				s := 63 + ch0
				for ; cnt > 255; cnt -= 255 {
					w.Write([]byte{0x21, 0x32, 0x35, 0x35, s})
				}
				if cnt == 1 {
					w.Write([]byte{s})
				} else if cnt == 2 {
					w.Write([]byte{s, s})
				} else if cnt == 3 {
					w.Write([]byte{s, s, s})
				} else if cnt >= 100 {
					digit1 := cnt / 100
					digit2 := (cnt - digit1*100) / 10
					digit3 := cnt % 10
					c1 := byte(0x30 + digit1)
					c2 := byte(0x30 + digit2)
					c3 := byte(0x30 + digit3)
					// DECGRI (!): - Graphics Repeat Introducer
					w.Write([]byte{0x21, c1, c2, c3, s})
				} else if cnt >= 10 {
					c1 := byte(0x30 + cnt/10)
					c2 := byte(0x30 + cnt%10)
					// DECGRI (!): - Graphics Repeat Introducer
					w.Write([]byte{0x21, c1, c2, s})
				} else if cnt > 0 {
					// DECGRI (!): - Graphics Repeat Introducer
					w.Write([]byte{0x21, byte(0x30 + cnt), s})
				}
			}
			ch0 = specialChCr
		}
	}
	// string terminator(ST)
	w.Write([]byte{0x1b, 0x5c})

	return w.err
}

// Preserve the first output error, including short writes. Encode can finish
// its bookkeeping without sending further fragments after terminal failure.
type checkedWriter struct {
	writer io.Writer
	err    error
}

func (w *checkedWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

// Decoder decode sixel format into image
type Decoder struct {
	r io.Reader
}

// NewDecoder return new instance of Decoder
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r}
}

// Decode do decoding from image
func (e *Decoder) Decode(img *image.Image) error {
	buf := bufio.NewReader(e.r)
	_, err := buf.ReadBytes('\x1B')
	if err != nil {
		if err == io.EOF {
			err = nil
		}
		return err
	}
	c, err := buf.ReadByte()
	if err != nil {
		return err
	}
	switch c {
	case 'P':
		_, err := buf.ReadString('q')
		if err != nil {
			return err
		}
	default:
		return errors.New("Invalid format: illegal header")
	}
	colors := map[uint]color.Color{
		// 16 predefined color registers of VT340
		0:  sixelRGB(0, 0, 0),
		1:  sixelRGB(20, 20, 80),
		2:  sixelRGB(80, 13, 13),
		3:  sixelRGB(20, 80, 20),
		4:  sixelRGB(80, 20, 80),
		5:  sixelRGB(20, 80, 80),
		6:  sixelRGB(80, 80, 20),
		7:  sixelRGB(53, 53, 53),
		8:  sixelRGB(26, 26, 26),
		9:  sixelRGB(33, 33, 60),
		10: sixelRGB(60, 26, 26),
		11: sixelRGB(33, 60, 33),
		12: sixelRGB(60, 33, 60),
		13: sixelRGB(33, 60, 60),
		14: sixelRGB(60, 60, 33),
		15: sixelRGB(80, 80, 80),
	}
	dx, dy := 0, 0
	dw, dh, w, h := 0, 0, 200, 200
	pimg := image.NewNRGBA(image.Rect(0, 0, w, h))
	var cn uint
data:
	for {
		c, err = buf.ReadByte()
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return err
		}
		if c == '\r' || c == '\n' || c == '\b' {
			continue
		}
		switch {
		case c == '\x1b':
			c, err = buf.ReadByte()
			if err != nil {
				return err
			}
			if c == '\\' {
				break data
			}
		case c == '"':
			params := []int{}
			for {
				var i int
				n, err := fmt.Fscanf(buf, "%d", &i)
				if err == io.EOF {
					return err
				}
				if n == 0 {
					i = 0
				}
				params = append(params, i)
				c, err = buf.ReadByte()
				if err != nil {
					return err
				}
				if c != ';' {
					break
				}
			}
			if len(params) >= 4 {
				if w < params[2] {
					w = params[2]
				}
				if h < params[3]+6 {
					h = params[3] + 6
				}
				pimg = expandImage(pimg, w, h)
			}
			err = buf.UnreadByte()
			if err != nil {
				return err
			}
		case c == '$':
			dx = 0
		case c == '!':
			err = buf.UnreadByte()
			if err != nil {
				return err
			}
			var nc uint
			var c byte
			n, err := fmt.Fscanf(buf, "!%d%c", &nc, &c)
			if err != nil {
				return err
			}
			if n != 2 || c < '?' || c > '~' {
				return fmt.Errorf("invalid format: illegal repeating data tokens '!%d%c'", nc, c)
			}
			if w <= dx+int(nc)-1 {
				w *= 2
				pimg = expandImage(pimg, w, h)
			}
			m := byte(1)
			c -= '?'
			for p := 0; p < 6; p++ {
				if c&m != 0 {
					for q := 0; q < int(nc); q++ {
						pimg.Set(dx+q, dy+p, colors[cn])
					}
					if dh < dy+p+1 {
						dh = dy + p + 1
					}
				}
				m <<= 1
			}
			dx += int(nc)
			if dw < dx {
				dw = dx
			}
		case c == '-':
			dx = 0
			dy += 6
			if h <= dy+6 {
				h *= 2
				pimg = expandImage(pimg, w, h)
			}
		case c == '#':
			err = buf.UnreadByte()
			if err != nil {
				return err
			}
			var nc, csys uint
			var r, g, b uint
			var c byte
			n, err := fmt.Fscanf(buf, "#%d%c", &nc, &c)
			if err != nil {
				return err
			}
			if n != 2 {
				return fmt.Errorf("invalid format: illegal color specifier '#%d%c'", nc, c)
			}
			if c == ';' {
				n, err := fmt.Fscanf(buf, "%d;%d;%d;%d", &csys, &r, &g, &b)
				if err != nil {
					return err
				}
				if n != 4 {
					return fmt.Errorf("invalid format: illegal color specifier '#%d;%d;%d;%d;%d'", nc, csys, r, g, b)
				}
				if csys == 1 {
					colors[nc] = sixelHLS(r, g, b)
				} else {
					colors[nc] = sixelRGB(r, g, b)
				}
			} else {
				err = buf.UnreadByte()
				if err != nil {
					return err
				}
			}
			cn = nc
			if _, ok := colors[cn]; !ok {
				return fmt.Errorf("invalid format: undefined color number %d", cn)
			}
		default:
			if c >= '?' && c <= '~' {
				if w <= dx {
					w *= 2
					pimg = expandImage(pimg, w, h)
				}
				m := byte(1)
				c -= '?'
				for p := 0; p < 6; p++ {
					if c&m != 0 {
						pimg.Set(dx, dy+p, colors[cn])
						if dh < dy+p+1 {
							dh = dy + p + 1
						}
					}
					m <<= 1
				}
				dx++
				if dw < dx {
					dw = dx
				}
				break
			}
			return errors.New("invalid format: illegal data tokens")
		}
	}
	rect := image.Rect(0, 0, dw, dh)
	tmp := image.NewNRGBA(rect)
	draw.Draw(tmp, rect, pimg, image.Point{0, 0}, draw.Src)
	*img = tmp
	return nil
}

func sixelRGB(r, g, b uint) color.Color {
	return color.NRGBA{uint8(r * 0xFF / 100), uint8(g * 0xFF / 100), uint8(b * 0xFF / 100), 0xFF}
}

func sixelHLS(h, l, s uint) color.Color {
	var r, g, b, max, min float64

	/* https://wikimedia.org/api/rest_v1/media/math/render/svg/17e876f7e3260ea7fed73f69e19c71eb715dd09d */
	/* https://wikimedia.org/api/rest_v1/media/math/render/svg/f6721b57985ad83db3d5b800dc38c9980eedde1d */
	if l > 50 {
		max = float64(l) + float64(s)*(1.0-float64(l)/100.0)
		min = float64(l) - float64(s)*(1.0-float64(l)/100.0)
	} else {
		max = float64(l) + float64(s*l)/100.0
		min = float64(l) - float64(s*l)/100.0
	}

	/* sixel hue color ring is roteted -120 degree from nowdays general one. */
	h = (h + 240) % 360

	/* https://wikimedia.org/api/rest_v1/media/math/render/svg/937e8abdab308a22ff99de24d645ec9e70f1e384 */
	switch h / 60 {
	case 0: /* 0 <= hue < 60 */
		r = max
		g = min + (max-min)*(float64(h)/60.0)
		b = min
		break
	case 1: /* 60 <= hue < 120 */
		r = min + (max-min)*(float64(120-h)/60.0)
		g = max
		b = min
		break
	case 2: /* 120 <= hue < 180 */
		r = min
		g = max
		b = min + (max-min)*(float64(h-120)/60.0)
		break
	case 3: /* 180 <= hue < 240 */
		r = min
		g = min + (max-min)*(float64(240-h)/60.0)
		b = max
		break
	case 4: /* 240 <= hue < 300 */
		r = min + (max-min)*(float64(h-240)/60.0)
		g = min
		b = max
		break
	case 5: /* 300 <= hue < 360 */
		r = max
		g = min
		b = min + (max-min)*(float64(360-h)/60.0)
		break
	default:
	}
	return sixelRGB(uint(r), uint(g), uint(b))
}

func expandImage(pimg *image.NRGBA, w, h int) *image.NRGBA {
	b := pimg.Bounds()
	if w < b.Max.X {
		w = b.Max.X
	}
	if h < b.Max.Y {
		h = b.Max.Y
	}
	tmp := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(tmp, b, pimg, image.Point{0, 0}, draw.Src)
	return tmp
}
