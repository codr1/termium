package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"os"
	"sync"
	"time"
)

const (
	// Max base64 bytes per chunk per Kitty graphics protocol spec
	kittyChunkSize = 4096

	// Escape sequence fragments — avoid repeated string building
	kittyAPC    = "\033_G"
	kittyST     = "\033\\"
	kittyMore   = "\033_Gm=1;"
	kittyFinal  = "\033_Gm=0;"
	kittyDelete = "\033_Ga=d,d=I,i=1,q=1\033\\"
)

// kittyStats tracks performance metrics for the Kitty renderer
type kittyStats struct {
	mu             sync.Mutex
	framesRendered uint64
	totalEncodeNs  int64
	totalWriteNs   int64
	totalBytes     int64
	totalBase64    int64
}

var kStats kittyStats

// Reusable buffered writer for stdout — avoids per-chunk syscalls.
// Sized to hold a typical frame's worth of base64 + escape overhead.
var kittyWriter = bufio.NewWriterSize(os.Stdout, 4*1024*1024)

// Reusable base64 output buffer — avoids allocation per frame.
// Protected by the same single-threaded display path (called from displayFrame only).
var kittyB64Buf []byte

// displayWithKittyPNG sends PNG bytes directly to the terminal using the Kitty
// graphics protocol (f=100). No image decoding or re-encoding on the client —
// raw PNG passthrough from server to terminal.
func displayWithKittyPNG(pngData []byte) error {
	frameStart := time.Now()

	// Position cursor at the top-left of the usable area (after borders)
	// Same positioning logic as displayWithSixel
	fmt.Fprintf(kittyWriter, "\033[%d;%dH", V_BORDER_WIDTH+1, H_BORDER_WIDTH+1)

	// Delete previous image to avoid stacking.
	// d=I: delete image data and all placements for image id=1
	kittyWriter.WriteString(kittyDelete)

	// Base64 encode into reusable buffer
	encodeStart := time.Now()
	b64Len := base64.StdEncoding.EncodedLen(len(pngData))
	if cap(kittyB64Buf) < b64Len {
		kittyB64Buf = make([]byte, b64Len)
	} else {
		kittyB64Buf = kittyB64Buf[:b64Len]
	}
	base64.StdEncoding.Encode(kittyB64Buf, pngData)
	encodeTime := time.Since(encodeStart)

	// Write chunked Kitty escape sequences through buffered writer
	writeStart := time.Now()
	if err := writeKittyChunked(kittyB64Buf, "f=100,a=T,i=1,p=1,C=1,q=1"); err != nil {
		return fmt.Errorf("kitty write error: %v", err)
	}

	// Flush everything to stdout in one shot
	if err := kittyWriter.Flush(); err != nil {
		return fmt.Errorf("kitty flush error: %v", err)
	}
	writeTime := time.Since(writeStart)

	// Update stats
	kStats.mu.Lock()
	kStats.framesRendered++
	kStats.totalEncodeNs += encodeTime.Nanoseconds()
	kStats.totalWriteNs += writeTime.Nanoseconds()
	kStats.totalBytes += int64(len(pngData))
	kStats.totalBase64 += int64(b64Len)
	frameNum := kStats.framesRendered
	kStats.mu.Unlock()

	if cfg.ShowTimings {
		totalTime := time.Since(frameStart)
		chunks := (b64Len + kittyChunkSize - 1) / kittyChunkSize

		// Single timing line per frame, consistent with sixel output format
		fmt.Fprintf(os.Stderr, "  Kitty encode time: %v (PNG=%d bytes, B64=%d bytes, %d chunks, write=%v, total=%v)\n",
			encodeTime, len(pngData), b64Len, chunks, writeTime, totalTime)

		// Periodic aggregate stats every 30 frames
		if frameNum%30 == 0 {
			kStats.mu.Lock()
			avgEncode := time.Duration(kStats.totalEncodeNs / int64(kStats.framesRendered))
			avgWrite := time.Duration(kStats.totalWriteNs / int64(kStats.framesRendered))
			avgBytes := kStats.totalBytes / int64(kStats.framesRendered)
			kStats.mu.Unlock()
			fmt.Fprintf(os.Stderr, "  Kitty avg (%d frames): Base64=%v Write=%v PNG=%d bytes/frame\n",
				frameNum, avgEncode, avgWrite, avgBytes)
		}
		os.Stderr.Sync()
	}

	Debug(fmt.Sprintf("Kitty frame %d: %d PNG bytes, encode=%v, write=%v",
		frameNum, len(pngData), encodeTime, writeTime), DEBUG)

	return nil
}

// writeKittyChunked writes base64-encoded image data using the Kitty graphics
// protocol's chunked transmission. First chunk includes full control data;
// subsequent chunks only carry the m= continuation flag.
//
// All writes go through kittyWriter (buffered) and are flushed by the caller.
func writeKittyChunked(encoded []byte, controlData string) error {
	// Helper to write and check — logs on first error per frame
	var writeErr error
	w := func(data []byte) {
		if writeErr != nil {
			return
		}
		_, writeErr = kittyWriter.Write(data)
	}
	ws := func(s string) {
		if writeErr != nil {
			return
		}
		_, writeErr = kittyWriter.WriteString(s)
	}

	if len(encoded) <= kittyChunkSize {
		// Single chunk — no chunking needed
		ws(kittyAPC)
		ws(controlData)
		ws(";")
		w(encoded)
		ws(kittyST)
		if writeErr != nil {
			Debug(fmt.Sprintf("Kitty write error (single chunk): %v", writeErr), ERROR)
		}
		return writeErr
	}

	// First chunk with full control data and m=1 (more coming)
	ws(kittyAPC)
	ws(controlData)
	ws(",m=1;")
	w(encoded[:kittyChunkSize])
	ws(kittyST)
	pos := kittyChunkSize

	// Middle chunks
	for pos+kittyChunkSize < len(encoded) {
		ws(kittyMore)
		w(encoded[pos : pos+kittyChunkSize])
		ws(kittyST)
		pos += kittyChunkSize
	}

	// Final chunk
	ws(kittyFinal)
	w(encoded[pos:])
	ws(kittyST)

	if writeErr != nil {
		Debug(fmt.Sprintf("Kitty write error at byte %d/%d: %v", pos, len(encoded), writeErr), ERROR)
	}
	return writeErr
}

// Reusable PNG encode buffer for the RGBA → PNG → Kitty path (splash screen, etc.)
var kittyPNGBuf bytes.Buffer

// displayWithKittyRGBA encodes an RGBA image to PNG and sends it via Kitty.
// Used for non-streaming paths (splash screen, resize redraws) where we have
// an in-memory image rather than raw PNG bytes from the server.
func displayWithKittyRGBA(img *image.RGBA) error {
	kittyPNGBuf.Reset()

	// PNG encode with fastest compression — we're going over a local pipe
	enc := &png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&kittyPNGBuf, img); err != nil {
		return fmt.Errorf("kitty PNG encode error: %v", err)
	}

	return displayWithKittyPNG(kittyPNGBuf.Bytes())
}

// GetKittyStats returns current Kitty renderer performance stats.
func GetKittyStats() (framesRendered uint64, avgEncodeTime, avgWriteTime time.Duration, avgPNGBytes int64) {
	kStats.mu.Lock()
	defer kStats.mu.Unlock()

	framesRendered = kStats.framesRendered
	if framesRendered == 0 {
		return
	}
	avgEncodeTime = time.Duration(kStats.totalEncodeNs / int64(framesRendered))
	avgWriteTime = time.Duration(kStats.totalWriteNs / int64(framesRendered))
	avgPNGBytes = kStats.totalBytes / int64(framesRendered)
	return
}
