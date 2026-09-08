package main

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
)

// Parse the emitted wire format independently of the encoder. Every chunk
// must suppress replies, respect the size limit, and terminate continuation.
func decodeKittyPayload(t *testing.T, payload []byte) (string, []byte) {
	t.Helper()
	parts := strings.Split(string(payload), "\033\\")
	if parts[len(parts)-1] != "" {
		t.Fatal("unterminated Kitty payload")
	}
	parts = parts[:len(parts)-1]
	var encoded strings.Builder
	var first string
	for i, part := range parts {
		if !strings.HasPrefix(part, "\033_G") {
			t.Fatalf("bad APC prefix %q", part)
		}
		control, data, ok := strings.Cut(part[3:], ";")
		if !ok || len(data) > 4096 || len(data)%4 != 0 || !strings.Contains(control, "q=2") {
			t.Fatalf("bad chunk %d: control=%q bytes=%d", i, control, len(data))
		}
		if i < len(parts)-1 && !strings.Contains(control, "m=1") {
			t.Fatal("missing continuation")
		}
		if i == len(parts)-1 && len(parts) > 1 && !strings.Contains(control, "m=0") {
			t.Fatal("missing final chunk")
		}
		if i == 0 {
			first = control
		} else if strings.Contains(control, "i=") || strings.Contains(control, "a=") {
			t.Fatal("continuation started a new image")
		}
		encoded.WriteString(data)
	}
	data, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil {
		t.Fatal(err)
	}
	return first, data
}

func TestKittyChunkBoundariesPreserveBytesAndPlacement(t *testing.T) {
	for _, size := range []int{0, 1, 3071, 3072, 3073, 6144, 10001} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			data := make([]byte, size)
			for i := range data {
				data[i] = byte(i * 37)
			}
			payload, err := encodeKittyPNG(data)
			if err != nil {
				t.Fatal(err)
			}
			control, got := decodeKittyPayload(t, payload)
			if !bytes.Equal(got, data) || !strings.Contains(control, "f=100,a=T,i=1,p=1,C=1,q=2") {
				t.Fatal("PNG bytes or replacement placement changed")
			}
		})
	}
}

func TestKittyRGBCompressionPreservesSubimageRowsAndPreviousPayload(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 13, 9))
	for y := 0; y < 9; y++ {
		for x := 0; x < 13; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 17), uint8(y * 29), 83, 255})
		}
	}
	sub := img.SubImage(image.Rect(2, 3, 11, 8)).(*image.RGBA)
	var encoder kittyEncoder
	payload, err := encoder.encodeRGB(sub)
	if err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(payload)
	control, compressed := decodeKittyPayload(t, payload)
	if !strings.Contains(control, "f=24,s=9,v=5,o=z,a=T,i=1,p=1,C=1,q=2") {
		t.Fatal(control)
	}
	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatal(err)
	}
	pixels, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}
	want := image.NewRGBA(image.Rect(0, 0, 9, 5))
	draw.Draw(want, want.Bounds(), sub, sub.Bounds().Min, draw.Src)
	if !bytes.Equal(pixels, rgbPixels(want.Pix)) {
		t.Fatal("Kitty pixels contain padding, wrong colors, or shifted rows")
	}
	if _, err := encoder.encodeRGB(img); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, before) {
		t.Fatal("next frame overwrote published payload")
	}
}

func TestBothRenderersPrepareTheSameJPEGSource(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 37, 19))
	for y := 0; y < 19; y++ {
		for x := 0; x < 37; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x * 7), uint8(y * 11), 101, 255})
		}
	}
	var source bytes.Buffer
	if err := jpeg.Encode(&source, img, &jpeg.Options{Quality: 60}); err != nil {
		t.Fatal(err)
	}
	var sharedPixels []byte
	for _, renderer := range []string{"sixel", "kitty"} {
		p := framePreparer{renderer: renderer, palette: "websafe"}
		raw := &Frame{Data: source.Bytes(), Generation: 1}
		f, err := p.prepare(raw)
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Graphics) == 0 || f.Image == nil || f.Width != 37 || f.Height != 19 {
			t.Fatalf("%s left preparation to the UI", renderer)
		}
		if sharedPixels == nil {
			sharedPixels = bytes.Clone(f.Image.Pix)
		} else if !bytes.Equal(sharedPixels, f.Image.Pix) {
			t.Fatal("renderers did not start from identical decoded pixels")
		}
		if renderer == "kitty" {
			_, data := decodeKittyPayload(t, f.Graphics)
			r, err := zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			pixels, err := io.ReadAll(r)
			r.Close()
			if err != nil || !bytes.Equal(pixels, rgbPixels(sharedPixels)) {
				t.Fatal("Kitty output changed the shared pixels", err)
			}
		}
		reused, err := p.prepare(raw)
		if err != nil || reused != f {
			t.Fatalf("%s re-encoded an unchanged capture", renderer)
		}
	}
}

func TestKittyOutputLimitIncludesBase64Expansion(t *testing.T) {
	_, err := encodeKittyPNG(make([]byte, maxFrameBytes*3/4))
	if err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatal("framing exceeded bounded frame size", err)
	}
}

func TestKittySplashPreservesTransparencyAndCursor(t *testing.T) {
	uiScreen(t, 80, 24)
	previous := graphicsOutput
	t.Cleanup(func() { graphicsOutput = previous })
	var out bytes.Buffer
	graphicsOutput = &out
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(1, 1, color.NRGBA{R: 200, G: 100, B: 50, A: 128})
	if err := displayWithKittyRGBA(img); err != nil {
		t.Fatal(err)
	}
	start, end := "\033[s\033[3;2H", "\033[u"
	if !bytes.HasPrefix(out.Bytes(), []byte(start)) || !bytes.HasSuffix(out.Bytes(), []byte(end)) {
		t.Fatal("graphics disturb toolbar or cursor")
	}
	_, data := decodeKittyPayload(t, out.Bytes()[len(start):out.Len()-len(end)])
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil || color.NRGBAModel.Convert(decoded.At(1, 1)) != color.NRGBAModel.Convert(img.At(1, 1)) {
		t.Fatal("splash transparency changed", err)
	}
}

func rgbPixels(rgba []byte) []byte {
	var rgb []byte
	for i := 0; i < len(rgba); i += 4 {
		rgb = append(rgb, rgba[i:i+3]...)
	}
	return rgb
}
