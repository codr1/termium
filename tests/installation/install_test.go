//go:build installation

package installation

import (
	"crypto/sha256"
	"fmt"
	"github.com/creack/pty"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A fresh home and PATH containing only bootstrap tools exercise the delivered
// archive without Node, npm, Go, protoc, or a browser command available.
func TestPackagedInstallation(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	archive := os.Getenv("TERMIUM_INSTALL_ARTIFACT")
	if archive == "" {
		archive = filepath.Join(root, "dist", fmt.Sprintf("termium-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH))
	}
	file, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", hash.Sum(nil))
	base := filepath.Join(t.TempDir(), "spaces and 'quotes")
	user, tools := filepath.Join(base, "user"), filepath.Join(base, "tools")
	for _, dir := range []string{user, tools} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range []string{"bash", "cat", "uname", "getconf", "awk", "tar", "gzip", "mktemp", "rm", "mkdir", "sha256sum", "shasum"} {
		if command, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(command, filepath.Join(tools, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	env := []string{"HOME=" + user, "PATH=" + tools, "TERM=xterm-256color", "LANG=en_US.UTF-8", "PUPPETEER_EXECUTABLE_PATH=/no/system/browser"}
	if runtime.GOOS == "darwin" {
		env = append(env, "TMPDIR="+os.TempDir())
	}
	run := func(checksum string) ([]byte, error) {
		cmd := exec.Command(filepath.Join(tools, "bash"), filepath.Join(root, "scripts/install.sh"), "--archive", archive, "--checksum", checksum)
		cmd.Env, cmd.Dir = env, base
		return cmd.CombinedOutput()
	}
	if output, err := run(strings.Repeat("0", 64)); err == nil {
		t.Fatalf("corrupt checksum installed: %s", output)
	}
	for i := 0; i < 2; i++ {
		if output, err := run(digest); err != nil {
			t.Fatalf("install attempt %d: %v\n%s", i, err, output)
		}
	}
	bin := filepath.Join(user, ".local", "bin", "termium")
	installedBinary, err := filepath.EvalSymlinks(bin)
	if err != nil {
		t.Fatal(err)
	}
	installedLicense, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(installedBinary)), "LICENSE"))
	if err != nil || !strings.Contains(string(installedLicense), "MIT License") || !strings.Contains(string(installedLicense), "Termium contributors") {
		t.Fatalf("release is missing the project license: %v", err)
	}
	cmd := exec.Command(bin, "--doctor")
	cmd.Env, cmd.Dir = env, base
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("installed runtime: %v\n%s", err, output)
	}
	cmd = exec.Command(filepath.Join(tools, "bash"), "--noprofile", "--norc", "-c", `source "$HOME/.bashrc"; termium --version`)
	cmd.Env, cmd.Dir = env, base
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shell integration: %v\n%s", err, output)
	}
	requested := make(chan struct{}, 1)
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case requested <- struct{}{}:
		default:
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<body style="background:lime">Installed Termium works</body>`))
	}))
	defer fixture.Close()
	cmd = exec.Command(bin, "--set-homepage", fixture.URL)
	cmd.Env, cmd.Dir = env, base
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("set fixture home page: %v\n%s", err, output)
	}
	// Exercise the public pipe-and-launch boundary with the real release.
	// Auto renderer selection must fall back when the PTY has no graphics.
	cmd = exec.Command(filepath.Join(tools, "bash"), "-o", "pipefail", "-c",
		`cat "$1" | bash -s -- --archive "$2" --checksum "$3"`, "install-test",
		filepath.Join(root, "scripts/install.sh"), archive, digest)
	cmd.Env, cmd.Dir = env, base
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	waited := false
	defer func() {
		terminal.Close()
		if !waited {
			// The PTY session includes the piped installer and launched app.
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	}()
	log, err := os.CreateTemp(t.TempDir(), "terminal-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	go func() { _, _ = io.Copy(log, terminal) }()
	select {
	case <-requested:
	case err := <-done:
		waited = true
		output, _ := os.ReadFile(log.Name())
		t.Fatalf("installed app exited early: %v\n%s", err, output)
	case <-time.After(60 * time.Second):
		t.Fatal("installed app never reached the fixture")
	}
	if _, err := terminal.Write([]byte{'\x11', '\r'}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		waited = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("installed app did not quit cleanly")
	}
}
