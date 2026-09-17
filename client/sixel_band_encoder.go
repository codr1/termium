package main

import (
	"bytes"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"io"

	"github.com/mattn/go-sixel"
)

// BandEncoder handles sixel encoding for individual bands
type BandEncoder struct {
	encoder       *sixel.Encoder
	palette       color.Palette
	paletteType   sixel.PaletteType
	width         int
	height        int
	buffer        *bytes.Buffer
	normalizedImg *image.RGBA // Reusable buffer for normalized band images
}

// NewBandEncoder creates a new band encoder
func NewBandEncoder(paletteType sixel.PaletteType, width, height int) *BandEncoder {
	// Pre-allocate buffer with reasonable capacity (estimate ~4 bytes per pixel)
	buf := &bytes.Buffer{}
	buf.Grow(width * SIXEL_BAND_HEIGHT * 4)

	encoder := sixel.NewEncoder(buf)
	encoder.Dither = false
	encoder.Palette = paletteType

	var pal color.Palette
	switch paletteType {
	case sixel.PaletteWebSafe:
		pal = palette.WebSafe
	case sixel.PalettePlan9:
		pal = palette.Plan9
	default:
		// For adaptive, we'll need to handle this differently
		pal = nil
	}

	// Pre-allocate normalized image buffer for maximum band height
	normalizedImg := image.NewRGBA(image.Rect(0, 0, width, SIXEL_BAND_HEIGHT))

	return &BandEncoder{
		encoder:       encoder,
		palette:       pal,
		paletteType:   paletteType,
		width:         width,
		height:        height,
		buffer:        buf,
		normalizedImg: normalizedImg,
	}
}

// EncodeBand encodes one band as Sixel pixel data: DECGNL row separators and
// per-register RLE runs only. The introducer, raster dimensions, palette
// definitions, and terminator are written once per frame by ComposeFullSixel.
func (be *BandEncoder) EncodeBand(img *image.RGBA, bandY int, bandHeight int) (string, error) {
	// Clear the buffer
	be.buffer.Reset()

	// Create a sub-image for just this band
	bandRect := image.Rect(0, bandY, be.width, bandY+bandHeight)
	bandSubImg := img.SubImage(bandRect).(*image.RGBA)

	// The buffer always keeps its maximum height; a short final band encodes a
	// bounded view of it. Stale rows below the band are harmless because the
	// encoder reads through accessors that respect the view's bounds; do not
	// index Pix directly in the encoder.
	normalizedRect := image.Rect(0, 0, be.width, bandHeight)
	draw.Draw(be.normalizedImg, normalizedRect, bandSubImg, bandSubImg.Bounds().Min, draw.Src)

	// Temporarily set the encoder dimensions to just this band
	be.encoder.Width = be.width
	be.encoder.Height = bandHeight

	if err := be.encoder.EncodePixelData(be.normalizedImg.SubImage(normalizedRect)); err != nil {
		return "", err
	}

	// Return an owned copy: buffer.Reset() on the next band reuses this storage.
	return be.buffer.String(), nil
}

// ComposeFullSixel creates a complete sixel image from band strings
func ComposeFullSixel(bands []string, width, height int, pal color.Palette) string {
	// Pre-allocate buffer with estimated capacity
	// Estimate: header(~30) + palette(~2KB) + bands data
	estimatedSize := 30 + 2048
	for _, band := range bands {
		estimatedSize += len(band)
	}

	var buf bytes.Buffer
	buf.Grow(estimatedSize)

	// Write sixel header with dimensions
	// Format: ESC P <P1>;<P2>;<P3> q "Pan;Pad;Ph;Pv
	// P1=0 (aspect ratio), P2=0 (background), P3=8 (8-bit color)
	buf.WriteString("\x1bP0;0;8q\"1;1;")
	buf.Write(intToBytes(width))
	buf.WriteByte(';')
	buf.Write(intToBytes(height))

	// Write palette definitions (if using fixed palette)
	if pal != nil {
		writePalette(&buf, pal)
	}

	// Write each band's pixel data
	for i, bandStr := range bands {
		if bandStr == "" {
			// Skip empty bands (shouldn't happen but be safe)
			continue
		}

		// Add Graphics New Line between bands to move down 6 pixels
		if i > 0 {
			// DECGNL - Graphics Next Line (moves cursor down 6 pixels)
			buf.WriteByte('-')
		}

		buf.WriteString(bandStr)
	}

	// Write sixel terminator
	// DECGRA ST (ESC \)
	buf.Write([]byte{0x1b, 0x5c})

	return buf.String()
}

// writePalette writes color palette definitions to the buffer
func writePalette(w io.Writer, pal color.Palette) {
	var palBuf [32]byte
	for n, v := range pal {
		r, g, b, _ := v.RGBA()
		r = r * 100 / 0xFFFF
		g = g * 100 / 0xFFFF
		b = b * 100 / 0xFFFF

		// Build color definition string
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
}

// writeInt writes an integer to the buffer and returns the number of bytes written
func writeInt(buf []byte, n int) int {
	if n == 0 {
		buf[0] = '0'
		return 1
	}

	// Write digits in reverse, then reverse the result
	end := 0
	for n > 0 {
		buf[end] = byte('0' + n%10)
		n /= 10
		end++
	}

	// Reverse the digits
	for i := 0; i < end/2; i++ {
		buf[i], buf[end-1-i] = buf[end-1-i], buf[i]
	}

	return end
}

// intToBytes converts an integer to its ASCII byte representation
func intToBytes(n int) []byte {
	if n == 0 {
		return []byte{'0'}
	}

	// Pre-allocate reasonable size
	buf := make([]byte, 0, 10)
	tmp := make([]byte, 10)

	// Write digits in reverse
	end := 0
	for n > 0 {
		tmp[end] = byte('0' + n%10)
		n /= 10
		end++
	}

	// Append in correct order
	for i := end - 1; i >= 0; i-- {
		buf = append(buf, tmp[i])
	}

	return buf
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
