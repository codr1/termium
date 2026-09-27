//go:build integration

package integration

import (
	"bytes"
	"image"
	"image/draw"
	_ "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
	pb "termium/client/pb"
)

func TestClientPollUpdatesMetadataWithoutChangedPixels(t *testing.T) {
	release := make(chan struct{})
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/title" {
			select {
			case <-release:
				_, _ = io.WriteString(w, "PollAfter")
			case <-r.Context().Done():
			}
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<!doctype html><title>PollBefore</title><body style="margin:0;background:#335577">Stable pixels<script>fetch('/title').then(r=>r.text()).then(title=>{document.title=title})</script>`)
	}))
	t.Cleanup(func() { fixture.CloseClientConnections(); fixture.Close() })
	c := startServer(t)
	cmd := exec.Command(filepath.Join(root(t), "client/termium"), "--renderer", "tcell", "--splash", "NONE", "--tcp", c.address, "--url", fixture.URL)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	requireOK(t, err)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	output := lockedBuffer{screen: vt10x.New(vt10x.WithSize(80, 24))}
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
	waitDisplay := func(text string) {
		t.Helper()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		timeout := time.NewTimer(10 * time.Second)
		defer timeout.Stop()
		for !output.visible(text, "") {
			select {
			case <-tick.C:
			case <-timeout.C:
				t.Fatalf("client never displayed %q", text)
			}
		}
	}
	waitDisplay("PollBefore")
	before, err := c.CaptureScreenshot(deadline(t), &pb.ScreenshotRequest{Format: "png"})
	requireOK(t, err)
	close(release)
	// No test-side GetBrowserState or input acknowledgement can refresh the
	// toolbar here: the actual client's independent metadata poll must do it.
	waitDisplay("PollAfter")
	after, err := c.CaptureScreenshot(deadline(t), &pb.ScreenshotRequest{Format: "png"})
	requireOK(t, err)
	if before.State != nil || after.State != nil {
		t.Fatal("screenshots unexpectedly carried toolbar metadata")
	}
	beforeImage, _, err := image.Decode(bytes.NewReader(before.Data))
	requireOK(t, err)
	afterImage, _, err := image.Decode(bytes.NewReader(after.Data))
	requireOK(t, err)
	if beforeImage.Bounds() != afterImage.Bounds() {
		t.Fatal("title-only fixture changed screenshot dimensions")
	}
	pixels := func(img image.Image) []byte {
		rgba := image.NewRGBA(img.Bounds())
		draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
		return rgba.Pix
	}
	if !bytes.Equal(pixels(beforeImage), pixels(afterImage)) {
		t.Fatal("title-only fixture unexpectedly changed screenshot pixels")
	}
	_, err = io.WriteString(terminal, "\x11\r")
	requireOK(t, err)
	select {
	case err := <-done:
		exited = true
		requireOK(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("client did not exit cleanly")
	}
}
