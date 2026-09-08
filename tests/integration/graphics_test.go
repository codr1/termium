//go:build integration

package integration

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/mattn/go-sixel"
	pb "termium/client/pb"
)

// Decode the actual client's terminal output back to fixture pixels. This
// crosses capture selection, RPC, preparation, and presentation; it does not
// substitute for checking placement or speed in a real graphics terminal.
func TestTerminalGraphics(t *testing.T) {
	for _, tc := range []struct{ renderer, option, source string }{
		{"kitty", "", "png"}, {"kitty", "jpeg", "jpeg"},
		{"sixel", "", "jpeg"}, {"sixel", "png", "png"},
	} {
		t.Run(tc.renderer+"/"+tc.source, func(t *testing.T) {
			c := startServer(t)
			url, _ := fixture(t)
			args := []string{"--renderer", tc.renderer, "--splash", "NONE", "--tcp", c.address, "--url", url, "--save-screenshots"}
			if tc.option != "" {
				args = append(args, "--capture-format", tc.option)
			}
			cmd := exec.Command(filepath.Join(root(t), "client/termium"), args...)
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "TERM=xterm-256color")
			terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80, X: 640, Y: 384})
			requireOK(t, err)
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			var output lockedBuffer
			drained := make(chan struct{})
			go func() { _, _ = io.Copy(&output, terminal); close(drained) }()
			exited := false
			t.Cleanup(func() {
				if !exited {
					_ = cmd.Process.Kill()
					<-done
				}
				terminal.Close()
				<-drained
				if t.Failed() {
					t.Logf("terminal output: %q", output.String())
				}
			})
			waitState(t, c, func(s *pb.BrowserState) bool { return s.Url == url+"/" && !s.Loading })
			ctx := deadline(t)
			for {
				matched := false
				for _, img := range decodeTerminalGraphics(t, output.String(), tc.renderer, tc.source) {
					if img.Bounds().Dx() != 624 || img.Bounds().Dy() != 320 {
						continue
					}
					r, g, b, _ := img.At(400, 280).RGBA()
					// JPEG and Sixel quantization have different color precision.
					if abs(int(r>>8)-18) <= 26 && abs(int(g>>8)-86) <= 26 && abs(int(b>>8)-52) <= 26 {
						matched = true
						break
					}
				}
				if matched {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("terminal never received the fixture pixels at the correct viewport size")
				case <-time.After(20 * time.Millisecond):
				}
			}
			_, err = io.WriteString(terminal, "\x11\r")
			requireOK(t, err)
			select {
			case err := <-done:
				exited = true
				requireOK(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("graphics client did not quit cleanly")
			}
			captures, err := filepath.Glob(filepath.Join(cmd.Dir, "RawImage*"))
			requireOK(t, err)
			if len(captures) == 0 {
				t.Fatal("no saved source captures")
			}
			for _, filename := range captures {
				data, err := os.ReadFile(filename)
				requireOK(t, err)
				_, format, err := image.DecodeConfig(bytes.NewReader(data))
				requireOK(t, err)
				if format != tc.source || filepath.Ext(filename) != "."+tc.source {
					t.Fatalf("unexpected capture format or extension: %s %s", format, filename)
				}
			}
		})
	}
}

func decodeTerminalGraphics(t *testing.T, output, renderer, source string) []image.Image {
	t.Helper()
	var frames []image.Image
	prefix := "\x1b_G"
	if renderer == "sixel" {
		prefix = "\x1bP"
	}
	var encoded strings.Builder
	var first string
	for {
		start := strings.Index(output, prefix)
		if start < 0 {
			break
		}
		output = output[start+len(prefix):]
		end := strings.Index(output, "\x1b\\")
		if end < 0 {
			break
		} // The PTY reader may be in the middle of a write.
		block := output[:end]
		output = output[end+2:]
		if renderer == "sixel" {
			var img image.Image
			err := sixel.NewDecoder(strings.NewReader(prefix + block + "\x1b\\")).Decode(&img)
			requireOK(t, err)
			if img != nil {
				frames = append(frames, img)
			}
			continue
		}
		control, data, ok := strings.Cut(block, ";")
		if !ok {
			continue
		} // Image deletion has no payload.
		if strings.Contains(control, "a=T") {
			first = control
			encoded.Reset()
		}
		if first == "" {
			continue
		}
		if len(data) > 4096 {
			t.Fatal("oversized Kitty chunk")
		}
		encoded.WriteString(data)
		if strings.Contains(control, "m=1") {
			continue
		}
		payload, err := base64.StdEncoding.DecodeString(encoded.String())
		requireOK(t, err)
		if source == "png" {
			if !strings.HasPrefix(first, "f=100,") {
				t.Fatal("PNG passthrough was lost", first)
			}
			img, err := png.Decode(bytes.NewReader(payload))
			requireOK(t, err)
			frames = append(frames, img)
		} else {
			var w, h int
			_, err := fmt.Sscanf(first, "f=24,s=%d,v=%d,o=z,", &w, &h)
			requireOK(t, err)
			if w <= 0 || h <= 0 || w*h > 1024*1024 || !strings.Contains(first, "o=z") {
				t.Fatal("invalid raw Kitty frame", first)
			}
			r, err := zlib.NewReader(bytes.NewReader(payload))
			requireOK(t, err)
			pixels, err := io.ReadAll(io.LimitReader(r, int64(w*h*3+1)))
			r.Close()
			requireOK(t, err)
			if len(pixels) != w*h*3 {
				t.Fatal("wrong Kitty pixel count")
			}
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			for i := 0; i < w*h; i++ {
				copy(img.Pix[i*4:i*4+3], pixels[i*3:i*3+3])
				img.Pix[i*4+3] = 255
			}
			frames = append(frames, img)
		}
		first = ""
	}
	return frames
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
