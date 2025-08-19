package main

import (
	"hash/crc32"
	"image"
	"strings"
)

const (
	SIXEL_BAND_HEIGHT = 6 // Sixel encodes 6 pixels vertically
)

// SixelColumn represents a single vertical strip of 6 pixels
type SixelColumn struct {
	ColorIndex uint8 // Palette index
	Pattern    byte  // 6 bits representing 6 vertical pixels
}

// SixelBand represents a horizontal band of the image (6 pixels high)
type SixelBand struct {
	Y          int           // Starting Y coordinate
	Height     int           // Height (usually 6, may be less for last band)
	Hash       uint32        // Hash for quick comparison (CRC32)
	Columns    []SixelColumn // Column data (only if dirty)
	CachedRLE  string        // Pre-encoded sixel string for this band
	IsDirty    bool          // Whether this band needs re-encoding
}

// BandManager manages all sixel bands for efficient partial updates
type BandManager struct {
	Width       int
	Height      int
	Bands       []SixelBand
	NumBands    int
	FrameNumber uint64 // For rolling refresh
}

// NewBandManager creates a new band manager for the given dimensions
func NewBandManager(width, height int) *BandManager {
	numBands := (height + SIXEL_BAND_HEIGHT - 1) / SIXEL_BAND_HEIGHT
	bands := make([]SixelBand, numBands)
	
	for i := 0; i < numBands; i++ {
		y := i * SIXEL_BAND_HEIGHT
		bandHeight := SIXEL_BAND_HEIGHT
		if y+bandHeight > height {
			bandHeight = height - y
		}
		
		bands[i] = SixelBand{
			Y:       y,
			Height:  bandHeight,
			Columns: make([]SixelColumn, width),
			IsDirty: true, // Initially all bands are dirty
		}
	}
	
	return &BandManager{
		Width:    width,
		Height:   height,
		Bands:    bands,
		NumBands: numBands,
	}
}

// Pre-allocated CRC32 table for fast hashing
var crcTable = crc32.MakeTable(crc32.IEEE)

// HashBand computes a CRC32 hash of the pixel data in a band
// CRC32 is much faster than cryptographic hashes and sufficient for change detection
func HashBand(img *image.RGBA, y, height, width int) uint32 {
	// Direct access to pixel data for speed
	bounds := img.Bounds()
	maxY := bounds.Dy()
	maxX := bounds.Dx()
	
	// Work directly with the image's pixel slice
	var crc uint32 = 0
	
	for row := y; row < y+height && row < maxY; row++ {
		// Get the row's pixel data directly
		rowStart := img.PixOffset(0, row)
		rowEnd := img.PixOffset(width, row)
		if rowEnd > len(img.Pix) {
			rowEnd = len(img.Pix)
		}
		if rowStart < rowEnd && width <= maxX {
			// Update CRC with the entire row at once
			crc = crc32.Update(crc, crcTable, img.Pix[rowStart:rowEnd])
		}
	}
	
	return crc
}

// DetectDirtyBands compares the new frame with cached band hashes
func (bm *BandManager) DetectDirtyBands(newFrame *image.RGBA) {
	for i := range bm.Bands {
		band := &bm.Bands[i]
		
		// Compute hash for this band in the new frame
		newHash := HashBand(newFrame, band.Y, band.Height, bm.Width)
		
		// Check if band has changed
		if newHash != band.Hash {
			band.IsDirty = true
			band.Hash = newHash
		} else {
			band.IsDirty = false
		}
		
		// Rolling refresh: force one band per frame to refresh
		if bm.FrameNumber > 0 && int(bm.FrameNumber%uint64(bm.NumBands)) == i {
			band.IsDirty = true
			Debug("Force refreshing band due to rolling update", DEBUG)
		}
	}
	
	bm.FrameNumber++
}

// GetDirtyBandCount returns the number of dirty bands
func (bm *BandManager) GetDirtyBandCount() int {
	count := 0
	for _, band := range bm.Bands {
		if band.IsDirty {
			count++
		}
	}
	return count
}

// ComposeSixelOutput concatenates all band strings into final sixel output
func (bm *BandManager) ComposeSixelOutput() string {
	if len(bm.Bands) == 0 {
		return ""
	}
	
	var output strings.Builder
	
	// Estimate capacity to avoid reallocations
	estimatedSize := len(bm.Bands) * len(bm.Bands[0].CachedRLE)
	output.Grow(estimatedSize)
	
	// Concatenate all band strings
	for i, band := range bm.Bands {
		output.WriteString(band.CachedRLE)
		
		// Add carriage return between bands (except after last)
		if i < len(bm.Bands)-1 {
			output.WriteString("-") // Sixel graphics newline
		}
	}
	
	return output.String()
}

// MarkAllDirty forces all bands to be re-encoded
func (bm *BandManager) MarkAllDirty() {
	for i := range bm.Bands {
		bm.Bands[i].IsDirty = true
	}
}

