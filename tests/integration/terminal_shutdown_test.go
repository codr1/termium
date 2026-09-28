//go:build integration

package integration

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

// Unlike tests with an independently started server, this exercises ownership:
// the real client must stop its own server and Chromium on terminal hangup.
func TestTerminalHangupStopsOwnedProcesses(t *testing.T) {
	for _, closePTY := range []bool{false, true} {
		name := "SIGHUP"
		if closePTY {
			name = "PTY-close"
		}
		t.Run(name, func(t *testing.T) {
			url, _ := fixture(t)
			chrome, browserPIDs := browserWrapper(t, nil)
			node, err := exec.LookPath("node")
			requireOK(t, err)
			dir := t.TempDir()
			serverPID := filepath.Join(dir, "server.pid")
			// Only the auto-launched Node process uses this PATH wrapper.
			wrapper := "#!/bin/sh\nprintf '%s\\n' \"$$\" > " + shellQuote(serverPID) + "\nexec " + shellQuote(node) + " \"$@\"\n"
			requireOK(t, os.WriteFile(filepath.Join(dir, "node"), []byte(wrapper), 0700))
			cmd := exec.Command(filepath.Join(root(t), "client/termium"), "--renderer", "tcell", "--splash", "NONE", "--url", url)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "TERM=xterm-256color", "PATH="+dir+":"+os.Getenv("PATH"),
				"TERMIUM_SERVER="+filepath.Join(root(t), "server/dist/src/server.js"),
				"PUPPETEER_EXECUTABLE_PATH="+chrome, "PUPPETEER_TMP_DIR="+t.TempDir())
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
				// Failure cleanup signals only PIDs recorded by this test's wrappers.
				if data, err := os.ReadFile(serverPID); err == nil {
					if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 1 {
						if syscall.Kill(pid, 0) == nil {
							_ = syscall.Kill(pid, syscall.SIGKILL)
						}
					}
				}
				cleanupBrowsers(t, browserPIDs)
				if t.Failed() {
					t.Logf("terminal output: %q", output.String())
				}
			})
			wait := func(what string, ready func() bool) {
				t.Helper()
				limit := time.NewTimer(20 * time.Second)
				defer limit.Stop()
				tick := time.NewTicker(20 * time.Millisecond)
				defer tick.Stop()
				for !ready() {
					select {
					case <-tick.C:
					case <-limit.C:
						t.Fatalf("timed out waiting for %s", what)
					}
				}
			}
			wait("rendered fixture", func() bool { return output.visible("Ready", url+"/") })
			var owned []int
			for _, file := range []string{serverPID, browserPIDs} {
				data, err := os.ReadFile(file)
				requireOK(t, err)
				fields := strings.Fields(string(data))
				if len(fields) == 0 {
					t.Fatalf("no owned PID in %s", file)
				}
				for _, field := range fields {
					pid, err := strconv.Atoi(field)
					requireOK(t, err)
					if pid <= 1 {
						t.Fatalf("invalid PID %d", pid)
					}
					requireOK(t, syscall.Kill(pid, 0))
					owned = append(owned, pid)
				}
			}
			if closePTY {
				requireOK(t, terminal.Close())
			} else {
				requireOK(t, cmd.Process.Signal(syscall.SIGHUP))
			}
			select {
			case err := <-done:
				exited = true
				requireOK(t, err)
			case <-time.After(8 * time.Second):
				t.Fatal("client did not shut down after terminal hangup")
			}
			for _, pid := range owned {
				if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
					t.Errorf("owned process %d survived client shutdown: %v", pid, err)
				}
			}
		})
	}
}
