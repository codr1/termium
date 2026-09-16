//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "termium/client/pb"
)

func TestInFlightCaptureCancellationAndShutdown(t *testing.T) {
	for _, action := range []string{"cancel", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			// Hold one capture inside the actual server after it enters the viewport
			// queue. A client-side cancelled context alone would not exercise this.
			dir := t.TempDir()
			entered, release := filepath.Join(dir, "entered"), filepath.Join(dir, "release")
			hook := filepath.Join(dir, "hold-capture.cjs")
			script := fmt.Sprintf(`const fs = require('node:fs');
const { BrowserControls } = require(%q);
const original = BrowserControls.prototype.capture;
let held = false;
BrowserControls.prototype.capture = async function(...args) {
  if (!held) {
    held = true;
    fs.writeFileSync(%q, '');
    await new Promise(resolve => {
      const timer = setInterval(() => {
        if (fs.existsSync(%q)) { clearInterval(timer); resolve(); }
      }, 10);
    });
  }
  return original.apply(this, args);
};
`, filepath.Join(root(t), "server/dist/src/browser-controls.js"), entered, release)
			requireOK(t, os.WriteFile(hook, []byte(script), 0600))
			c := startServer(t, "NODE_OPTIONS="+os.Getenv("NODE_OPTIONS")+fmt.Sprintf(" --require=%q", hook))
			ctx := deadline(t)
			_, err := c.OpenTab(ctx, &pb.Empty{})
			requireOK(t, err)
			_, err = c.SetViewport(ctx, &pb.ViewportSize{Width: 640, Height: 360})
			requireOK(t, err)
			captureCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			finished := make(chan error, 1)
			go func() {
				_, err := c.CaptureScreenshot(captureCtx, &pb.ScreenshotRequest{Format: "png"})
				finished <- err
			}()
			for {
				if _, err := os.Stat(entered); err == nil {
					break
				}
				select {
				case err := <-finished:
					t.Fatalf("capture finished before reaching the server barrier: %v", err)
				case <-ctx.Done():
					t.Fatal("capture never reached the server barrier")
				case <-time.After(10 * time.Millisecond):
				}
			}
			if action == "cancel" {
				cancel()
			} else {
				c.stop(t)
			}
			select {
			case err := <-finished:
				if err == nil || (action == "cancel" && status.Code(err) != codes.Canceled) {
					t.Fatalf("%s did not terminate the pending capture correctly: %v", action, err)
				}
			case <-ctx.Done():
				t.Fatalf("%s left the capture pending", action)
			}
			if action == "cancel" {
				requireOK(t, os.WriteFile(release, nil, 0600))
				frame, err := c.CaptureScreenshot(ctx, &pb.ScreenshotRequest{Format: "png"})
				requireOK(t, err)
				img, format, err := image.Decode(bytes.NewReader(frame.Data))
				requireOK(t, err)
				if format != "png" || img.Bounds().Dx() != 640 || img.Bounds().Dy() != 360 {
					t.Fatalf("capture did not recover after cancellation: %s %v", format, img.Bounds())
				}
			}
		})
	}
}

func TestCaptureWhileSubresourceStallsLoad(t *testing.T) {
	c := startServer(t)
	requested := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/pending.png" {
			select {
			case requested <- struct{}{}:
			default:
			}
			<-r.Context().Done() // Keep load pending until cleanup closes the connection.
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<body style="background:lime">Visible before load<img src="/pending.png">`))
	}))
	t.Cleanup(func() { server.CloseClientConnections(); server.Close() })
	_, err := c.BrowserCommand(deadline(t), &pb.NavigationRequest{Url: server.URL})
	requireOK(t, err)
	select {
	case <-requested:
	case <-time.After(5 * time.Second):
		t.Fatal("page did not request the stalled resource")
	}
	state := waitState(t, c, func(s *pb.BrowserState) bool { return s.Url == server.URL+"/" && s.Loading })
	ctx := deadline(t)
	for {
		frame, err := c.CaptureScreenshot(ctx, &pb.ScreenshotRequest{Format: "png"})
		requireOK(t, err)
		img, _, err := image.Decode(bytes.NewReader(frame.Data))
		requireOK(t, err)
		r, g, b, _ := img.At(img.Bounds().Dx()/2, img.Bounds().Dy()/2).RGBA()
		if frame.Generation == state.Generation && r == 0 && g == 65535 && b == 0 {
			break // Verify real page pixels, allowing the compositor's first paint to settle.
		}
		select {
		case <-ctx.Done():
			t.Fatalf("committed page stayed invisible while loading: generation=%d pixel=%d,%d,%d", frame.Generation, r, g, b)
		case <-time.After(20 * time.Millisecond):
		}
	}
	state, err = c.GetBrowserState(deadline(t), &pb.Empty{})
	requireOK(t, err)
	if !state.Loading {
		t.Fatal("capture waited for the stalled load event")
	}
}

func TestPacedScreenshotCapture(t *testing.T) {
	c := startServer(t)
	url, _ := fixture(t)
	_, err := c.BrowserCommand(deadline(t), &pb.NavigationRequest{Url: url})
	requireOK(t, err)
	state := waitState(t, c, func(s *pb.BrowserState) bool { return s.Url == url+"/" && !s.Loading })
	_, err = c.SetViewport(deadline(t), &pb.ViewportSize{Width: 1100, Height: 701})
	requireOK(t, err)
	for _, format := range []string{"png", "jpeg"} {
		frame, err := c.CaptureScreenshot(deadline(t), &pb.ScreenshotRequest{Format: format})
		requireOK(t, err)
		dim, actualFormat, err := image.DecodeConfig(bytes.NewReader(frame.Data))
		requireOK(t, err)
		if dim.Width != 1100 || dim.Height != 701 || actualFormat != format || frame.Generation != state.Generation {
			t.Fatalf("bad capture: %v %s generation %d", dim, actualFormat, frame.Generation)
		}
	}
	_, err = c.SetViewport(deadline(t), &pb.ViewportSize{Width: 16384, Height: 16384})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("unbounded pixel allocation accepted: %v", err)
	}
	_, err = c.CaptureScreenshot(deadline(t), &pb.ScreenshotRequest{Format: "raw"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid format accepted: %v", err)
	}
	_, err = c.CaptureScreenshot(deadline(t), &pb.ScreenshotRequest{Format: "png"})
	requireOK(t, err)
}
