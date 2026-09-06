//go:build integration

package integration

import (
	"bytes"
	"context"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	pb "termium/client/pb"
)

// Exercise the actual tcell parser, screen lifecycle, input worker and Chromium.
// The fixture's HTTP report is the assertion; ANSI output alone is not proof
// that a click or keystroke reached the right browser control.
func TestTerminalBrowser(t *testing.T) {
	c := startServer(t)
	url, events := fixture(t)
	cmd := exec.Command(filepath.Join(root(t), "client/termium"), "--renderer", "tcell", "--splash", "NONE", "--tcp", c.address, "--url", url)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
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
	waitDisplay := func(text string) {
		t.Helper()
		output.Reset()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		deadline := time.After(5 * time.Second)
		for !strings.Contains(output.String(), text) {
			select {
			case <-tick.C:
			case <-deadline:
				t.Fatalf("client never displayed %q", text)
			}
		}
	}
	waitDisplay("Ready")
	write := func(text string) { t.Helper(); _, err := io.WriteString(terminal, text); requireOK(t, err) }
	// A page input at CSS (28,24) is terminal cell (5,4), including toolbar.
	write("\x1b[<0;5;4M\x1b[<0;5;4m")
	write("\x1b[200~café 世界abc\x1b[201~")
	write("\x1b[D\x7f\r")
	expectEvent(t, events, "café 世界ac")
	// Unicode address editing and an actual browser history transition.
	write("\x0c" + url + "/second\r")
	waitState(t, c, func(s *pb.BrowserState) bool { return s.Url == url+"/second" && !s.Loading })
	waitDisplay("Ready")
	// Click the toolbar Back button, then edit the original form again.
	write("\x1b[<0;3;1M\x1b[<0;3;1m")
	waitState(t, c, func(s *pb.BrowserState) bool { return s.Url == url+"/" && !s.Loading })
	waitDisplay("Ready")
	write("\x1b[<0;5;12M\x1b[<0;5;12m")
	waitDisplay("Prompt")
	requireOK(t, pty.Setsize(terminal, &pty.Winsize{Rows: 30, Cols: 100}))
	write("\x1b[200~Zoë 世界\x1b[201~\r")
	expectEvent(t, events, "Zoë 世界")
	frameCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := c.StreamScreenshots(frameCtx, &pb.ScreenshotRequest{Fps: 10, Format: "png"})
	requireOK(t, err)
	lastSize := image.Config{}
	for {
		frame, err := stream.Recv()
		if err != nil {
			t.Fatalf("viewport stayed %dx%d, want 784x416: %v", lastSize.Width, lastSize.Height, err)
		}
		size, _, err := image.DecodeConfig(bytes.NewReader(frame.Data))
		requireOK(t, err)
		lastSize = size
		if size.Width == 784 && size.Height == 416 {
			break
		}
	}
	cancel()
	// Ctrl+Q must remain a local command and restore the terminal on exit.
	write("\x11\r")
	select {
	case err := <-done:
		exited = true
		requireOK(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("terminal did not exit cleanly")
	}
}

type lockedBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data.Len() > 256*1024 {
		b.data.Reset()
	}
	return b.data.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.data.String() }

func (b *lockedBuffer) Reset() { b.mu.Lock(); defer b.mu.Unlock(); b.data.Reset() }
