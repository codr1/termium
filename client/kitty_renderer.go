package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
)

const (
	kittyChunkSize = 4096 // Maximum Base64 bytes per protocol chunk.
	kittyAPC       = "\033_G"
	kittyST        = "\033\\"
	kittyMore      = "\033_Gm=1,q=2;"
	kittyFinal     = "\033_Gm=0,q=2;"
	kittyDelete    = "\033_Ga=d,d=I,i=1,q=2\033\\"
	kittyPlacement = "a=T,i=1,p=1,C=1,q=2"
)

// Owned by the preparation worker. Compression scratch may be reused, but the
// published protocol payload always has its own immutable backing storage.
type kittyEncoder struct {
	compressed bytes.Buffer
	compressor *zlib.Writer
	row        []byte
}

// JPEG captures have already been decoded for renderer preparation. Send those
// opaque pixels directly: Kitty supports zlib-compressed RGB, so no PNG encode is needed.
func (e *kittyEncoder) encodeRGB(img *image.RGBA) ([]byte, error) {
	if img == nil || img.Bounds().Empty() {
		return nil, fmt.Errorf("Kitty image is empty")
	}
	e.compressed.Reset()
	writer := boundedFrameWriter{&e.compressed}
	if e.compressor == nil {
		var err error
		e.compressor, err = zlib.NewWriterLevel(writer, zlib.BestSpeed)
		if err != nil {
			return nil, err
		}
	} else {
		e.compressor.Reset(writer)
	}
	bounds := img.Bounds()
	if cap(e.row) < bounds.Dx()*3 {
		e.row = make([]byte, bounds.Dx()*3)
	}
	e.row = e.row[:bounds.Dx()*3]
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		start := img.PixOffset(bounds.Min.X, y)
		pixels := img.Pix[start : start+bounds.Dx()*4]
		// JPEG is opaque. Do not transmit an unused alpha byte per pixel.
		for x := 0; x < bounds.Dx(); x++ {
			e.row[x*3], e.row[x*3+1], e.row[x*3+2] = pixels[x*4], pixels[x*4+1], pixels[x*4+2]
		}
		if _, err := e.compressor.Write(e.row); err != nil {
			_ = e.compressor.Close()
			return nil, err
		}
	}
	if err := e.compressor.Close(); err != nil {
		return nil, err
	}
	return encodeKittyPayload(e.compressed.Bytes(), fmt.Sprintf("f=24,s=%d,v=%d,o=z,%s", bounds.Dx(), bounds.Dy(), kittyPlacement))
}

// Explicit lossless PNG capture can pass through unchanged. Both this path and
// RGB complete Base64 encoding and protocol framing before reaching the UI.
func encodeKittyPNG(data []byte) ([]byte, error) {
	return encodeKittyPayload(data, "f=100,"+kittyPlacement)
}

func encodeKittyPayload(data []byte, control string) ([]byte, error) {
	encodedSize := base64.StdEncoding.EncodedLen(len(data))
	chunks := max(1, (encodedSize+kittyChunkSize-1)/kittyChunkSize)
	// Conservative framing allowance; guard allocation before growing the buffer.
	size := encodedSize + chunks*(len(kittyMore)+len(kittyST)) + len(control)
	if size > maxFrameBytes {
		return nil, fmt.Errorf("Kitty output exceeds 32 MiB; reduce the terminal size")
	}
	var output bytes.Buffer
	output.Grow(size)
	var chunk [kittyChunkSize]byte
	const rawChunkSize = kittyChunkSize / 4 * 3
	for offset := 0; ; offset += rawChunkSize {
		end := min(offset+rawChunkSize, len(data))
		more := end < len(data)
		if offset == 0 {
			output.WriteString(kittyAPC)
			output.WriteString(control)
			if more {
				output.WriteString(",m=1")
			}
			output.WriteByte(';')
		} else if more {
			output.WriteString(kittyMore)
		} else {
			output.WriteString(kittyFinal)
		}
		base64.StdEncoding.Encode(chunk[:], data[offset:end])
		output.Write(chunk[:base64.StdEncoding.EncodedLen(end-offset)])
		output.WriteString(kittyST)
		if !more {
			return output.Bytes(), nil
		}
	}
}

// Splash images are prepared once on the UI owner, outside the browser pipeline.
func displayWithKittyRGBA(img *image.RGBA) error {
	var data bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&data, img); err != nil {
		return err
	}
	payload, err := encodeKittyPNG(data.Bytes())
	if err != nil {
		return err
	}
	return writeGraphicsFrame(graphicsOutput, payload, sDims.ViewTop+1, H_BORDER_WIDTH+1)
}
