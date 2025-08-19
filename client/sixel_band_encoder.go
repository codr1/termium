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

// EncodeBand encodes a single band to sixel format
func (be *BandEncoder) EncodeBand(img *image.RGBA, bandY int, bandHeight int) (string, error) {
	// Clear the buffer
	be.buffer.Reset()
	
	// Create a sub-image for just this band
	bandRect := image.Rect(0, bandY, be.width, bandY+bandHeight)
	bandSubImg := img.SubImage(bandRect).(*image.RGBA)
	
	// Reuse the pre-allocated normalized image buffer
	// Adjust bounds if band height is different (e.g., last band)
	normalizedRect := image.Rect(0, 0, be.width, bandHeight)
	if normalizedRect != be.normalizedImg.Bounds() {
		// Only reallocate if dimensions changed (rare - only for last band)
		be.normalizedImg = image.NewRGBA(normalizedRect)
	}
	draw.Draw(be.normalizedImg, normalizedRect, bandSubImg, bandSubImg.Bounds().Min, draw.Src)
	
	// Temporarily set the encoder dimensions to just this band
	be.encoder.Width = be.width
	be.encoder.Height = bandHeight
	
	// Encode the normalized band image
	if err := be.encoder.Encode(be.normalizedImg.SubImage(normalizedRect)); err != nil {
		return "", err
	}
	
	// Get the encoded string
	encoded := be.buffer.String()
	
	// Strip the sixel header/footer since we'll compose them ourselves
	// Sixel format: ESC P ... ESC \
	// We want just the middle part for bands
	stripped := stripSixelWrapper(encoded)
	
	return stripped, nil
}

// stripSixelWrapper removes the sixel introducer and terminator
// and extracts ONLY the pixel data (no header, no dimensions, no palette)
func stripSixelWrapper(sixelStr string) string {
	// Sixel format: ESC P params q "dimensions" #palette_entries pixel_data ESC \
	// We want ONLY the pixel data part
	
	// Strategy: Find where palette definitions end and pixel data begins
	// Palette entries look like: #N;2;R;G;B
	// Pixel data starts with color selections like #N followed by pixel chars
	
	lastPaletteEnd := -1
	
	// Find all palette entries (they have ;2; after the number)
	for i := 0; i < len(sixelStr)-7; i++ {
		if sixelStr[i] == '#' {
			j := i + 1
			// Skip the number
			numStart := j
			for j < len(sixelStr) && sixelStr[j] >= '0' && sixelStr[j] <= '9' {
				j++
			}
			// Check if this is a palette definition
			if j > numStart && j+2 < len(sixelStr) && sixelStr[j:j+3] == ";2;" {
				// This is a palette entry, find where it ends
				j += 3 // Skip ;2;
				// Skip R value
				for j < len(sixelStr) && sixelStr[j] != ';' {
					j++
				}
				if j < len(sixelStr) {
					j++ // Skip semicolon
					// Skip G value
					for j < len(sixelStr) && sixelStr[j] != ';' {
						j++
					}
					if j < len(sixelStr) {
						j++ // Skip semicolon
						// Skip B value
						for j < len(sixelStr) && sixelStr[j] >= '0' && sixelStr[j] <= '9' {
							j++
						}
						// j now points to first char after this palette entry
						lastPaletteEnd = j
					}
				}
			}
		}
	}
	
	pixelStart := -1
	if lastPaletteEnd != -1 {
		pixelStart = lastPaletteEnd
	} else {
		// Fallback: look for pixel data after 'q' and optional dimension spec
		for i := 0; i < len(sixelStr)-1; i++ {
			if sixelStr[i] == 'q' {
				j := i + 1
				// Skip optional dimension spec like "1;1;896;900
				if j < len(sixelStr) && sixelStr[j] == '"' {
					// Skip until we're past the dimension spec
					for j < len(sixelStr) && sixelStr[j] != '#' && sixelStr[j] != '$' && !(sixelStr[j] >= '?' && sixelStr[j] <= '~') {
						j++
					}
				}
				// If we hit a #, there are palette entries to skip
				if j < len(sixelStr) && sixelStr[j] == '#' {
					// Already handled above, this shouldn't happen
					continue
				}
				// Otherwise we should be at pixel data
				if j < len(sixelStr) && (sixelStr[j] == '$' || (sixelStr[j] >= '?' && sixelStr[j] <= '~')) {
					pixelStart = j
					break
				}
			}
		}
	}
	
	if pixelStart == -1 {
		return ""
	}
	
	// Find end (before ESC \)
	endIdx := len(sixelStr)
	for i := len(sixelStr) - 2; i >= 0; i-- {
		if sixelStr[i] == 0x1b && sixelStr[i+1] == '\\' {
			endIdx = i
			break
		}
	}
	
	if pixelStart < endIdx {
		return sixelStr[pixelStart:endIdx]
	}
	return ""
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
		idx += writeInt(palBuf[idx:], n+1)
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