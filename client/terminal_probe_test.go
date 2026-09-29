package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// A direct PTY slave and an explicitly reopened /dev/tty are different devices
// on macOS. Exercise the installer-style pipe and redirection, not just a PTY.
func TestGraphicsProbePTY(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, launch, renderer string
		delay                  time.Duration
	}{
		{"direct-kitty", "direct", "kitty", 500 * time.Millisecond},
		{"redirect-kitty", "redirect", "kitty", 0},
		{"installer-kitty", "pipe", "kitty", 500 * time.Millisecond},
		{"installer-sixel", "pipe", "sixel", 500 * time.Millisecond},
		{"installer-no-response", "pipe", "tcell", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestGraphicsProbePTYHelper$")
			switch tc.launch {
			case "redirect":
				cmd = exec.CommandContext(ctx, "bash", "-c", `exec "$TERMIUM_PROBE_EXECUTABLE" -test.run=^TestGraphicsProbePTYHelper$ </dev/tty`)
			case "pipe":
				cmd = exec.CommandContext(ctx, "bash", "-o", "pipefail", "-c",
					`printf '%s\n' 'exec "$TERMIUM_PROBE_EXECUTABLE" -test.run=^TestGraphicsProbePTYHelper$ </dev/tty' | bash`)
			}
			cmd.Env = append(os.Environ(), "TERMIUM_PROBE_EXECUTABLE="+executable, "TERMIUM_PROBE_EXPECT="+tc.renderer)
			terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
			if err != nil {
				t.Fatal(err)
			}
			defer terminal.Close()
			var output bytes.Buffer
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				var pending string
				buf := make([]byte, 1024)
				for {
					n, err := terminal.Read(buf)
					if n > 0 {
						output.Write(buf[:n])
						pending += string(buf[:n])
						if strings.Contains(pending, "\033_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\033\\") {
							pending = ""
							if tc.renderer == "kitty" {
								time.Sleep(tc.delay)
								_, _ = io.WriteString(terminal, "\033_Gi=31;OK\033\\")
							} else if tc.renderer == "sixel" {
								_, _ = io.WriteString(terminal, "\033_Gi=31;ENOTSUP\033\\")
							}
						}
						if strings.Contains(pending, "\033[c") {
							pending = ""
							if tc.renderer == "sixel" {
								time.Sleep(tc.delay)
								_, _ = io.WriteString(terminal, "\033[?62;4c")
							}
						}
					}
					if err != nil {
						return
					}
				}
			}()
			err = cmd.Wait()
			terminal.Close()
			<-drained
			t.Logf("PTY output: %q", output.String())
			if err != nil {
				t.Fatalf("%s: %v", tc.launch, err)
			}
		})
	}
}

func TestGraphicsProbePTYHelper(t *testing.T) {
	want := os.Getenv("TERMIUM_PROBE_EXPECT")
	if want == "" {
		return
	}
	fds := []unix.PollFd{{Fd: 0, Events: unix.POLLIN}}
	n, pollErr := unix.Poll(fds, 0)
	fmt.Fprintf(os.Stderr, "initial poll: n=%d revents=%#x err=%v\n", n, fds[0].Revents, pollErr)
	got := detectRenderer()
	if got != want {
		t.Fatalf("renderer=%s, want %s", got, want)
	}
}
