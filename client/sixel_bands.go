package main

const (
	SIXEL_BAND_HEIGHT = 6 // Sixel encodes 6 pixels vertically
)

// SixelBand is one horizontal strip of the image and its pre-encoded output.
type SixelBand struct {
	Y         int    // Starting Y coordinate
	Height    int    // Height (usually 6, may be less for last band)
	CachedRLE string // Pre-encoded sixel string for this band
}

// BandManager holds the cached bands of the last successfully prepared image.
type BandManager struct {
	Width    int
	Height   int
	Bands    []SixelBand
	NumBands int
}

// NewBandManager creates a new band manager for the given dimensions.
func NewBandManager(width, height int) *BandManager {
	numBands := (height + SIXEL_BAND_HEIGHT - 1) / SIXEL_BAND_HEIGHT
	bands := make([]SixelBand, numBands)

	for i := range bands {
		y := i * SIXEL_BAND_HEIGHT
		h := SIXEL_BAND_HEIGHT
		if y+h > height {
			h = height - y
		}
		bands[i] = SixelBand{Y: y, Height: h}
	}

	return &BandManager{
		Width:    width,
		Height:   height,
		Bands:    bands,
		NumBands: numBands,
	}
}
