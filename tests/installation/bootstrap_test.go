//go:build installation

package installation

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// A tiny release fixture isolates the bootstrap's pipe/TTY boundary from the
// browser. TestPackagedInstallation separately checks real bundle activation.
func TestBootstrapTerminalLaunch(t *testing.T) {
	script, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	launcher := `#!/bin/bash
set -eu
case "$1" in
  --install-bundle)
    echo BUNDLE_CHECK
    [ "${FAIL_BUNDLE:-0}" != 1 ] || exit 42
    mkdir -p "$HOME/.local/bin"
    cp "$0" "$HOME/.local/bin/termium"
    ;;
  --first-run)
    [ -t 0 ] && [ -t 1 ] || exit 43
    read -r key
    echo "FIRST_RUN $key"
    ;;
  *) exit 44 ;;
esac
`
	var bundle bytes.Buffer
	gz := gzip.NewWriter(&bundle)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "bin/termium", Mode: 0755, Size: int64(len(launcher))}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, launcher); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(bundle.Bytes()))
	for _, tc := range []struct {
		name                           string
		terminal, noLaunch, failBundle bool
		badChecksum, truncated         bool
	}{
		{name: "piped installer reconnects keyboard", terminal: true},
		{name: "redirected output never launches"},
		{name: "explicit no-launch", terminal: true, noLaunch: true},
		{name: "bundle failure never launches", terminal: true, failBundle: true},
		{name: "checksum failure never launches", terminal: true, badChecksum: true},
		{name: "interrupted script never installs", terminal: true, truncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "home with 'quotes and spaces")
			tmp := filepath.Join(base, "tmp")
			if err := os.MkdirAll(tmp, 0755); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(base, "release.tar.gz")
			if err := os.WriteFile(archive, bundle.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			payload := script
			if tc.truncated {
				// Cut off the launch tail: no installation should run, even
				// though the earlier install-bundle command arrived intact.
				payload = script[:bytes.Index(script, []byte("# The script arrives"))]
			}
			input := filepath.Join(base, "install.sh")
			if err := os.WriteFile(input, payload, 0600); err != nil {
				t.Fatal(err)
			}
			checksum := digest
			if tc.badChecksum {
				checksum = strings.Repeat("0", 64)
			}
			args := []string{"-o", "pipefail", "-c", `cat "$1" | bash -s -- --archive "$2" --checksum "$3" "${@:4}"`, "bootstrap-test", input, archive, checksum}
			if tc.noLaunch {
				args = append(args, "--no-launch")
			}
			cmd := exec.Command("bash", args...)
			// No existing installation or parent PATH integration is inherited.
			cmd.Env = []string{"HOME=" + base, "TMPDIR=" + tmp, "PATH=" + os.Getenv("PATH"), "TERM=xterm-256color"}
			if tc.failBundle {
				cmd.Env = append(cmd.Env, "FAIL_BUNDLE=1")
			}
			var output []byte
			var runErr error
			if tc.terminal {
				terminal, err := pty.Start(cmd)
				if err != nil {
					t.Fatal(err)
				}
				defer terminal.Close()
				captured := make(chan []byte, 1)
				go func() { b, _ := io.ReadAll(terminal); captured <- b }()
				if _, err := terminal.Write([]byte("keyboard-input\n")); err != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					_ = cmd.Wait()
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- cmd.Wait() }()
				select {
				case runErr = <-done:
				case <-time.After(10 * time.Second):
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					<-done
					t.Fatal("bootstrap did not finish with keyboard input available")
				}
				output = <-captured
			} else {
				output, runErr = cmd.CombinedOutput()
			}
			wantFailure := tc.failBundle || tc.badChecksum || tc.truncated
			if (runErr != nil) != wantFailure {
				t.Fatalf("unexpected result: %v\n%s", runErr, output)
			}
			wantLaunch := tc.terminal && !tc.noLaunch && !wantFailure
			if bytes.Contains(output, []byte("FIRST_RUN keyboard-input")) != wantLaunch {
				t.Fatalf("expected launch=%v:\n%s", wantLaunch, output)
			}
			if (tc.truncated || tc.badChecksum) && bytes.Contains(output, []byte("BUNDLE_CHECK")) {
				t.Fatalf("unverified or partial input executed the bundle:\n%s", output)
			}
			entries, err := os.ReadDir(tmp)
			if err != nil || len(entries) != 0 {
				t.Fatalf("installer left temporary files: %v, %v", entries, err)
			}
		})
	}
}
