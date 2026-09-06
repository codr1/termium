//go:build integration

package integration

import (
	"bytes"
	"image"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "termium/client/pb"
)

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
	stream, err := c.StreamScreenshots(deadline(t), &pb.ScreenshotRequest{Fps: 61, Format: "png"})
	requireOK(t, err)
	_, err = stream.Recv()
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid stream rate accepted: %v", err)
	}
}
