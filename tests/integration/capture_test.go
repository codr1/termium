//go:build integration

package integration

import (
	"bytes"
	"image"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "termium/client/pb"
)

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
