//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pb "termium/client/pb"
)

func root(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func deadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// Every server gets an OS-assigned loopback port. Never touch the user's socket.
// Keep stdout/stderr on disk so a failed assertion includes server diagnostics.
type testServer struct {
	pb.BrowserControlClient
	cmd         *exec.Cmd
	done        chan error
	exited      bool
	browserPIDs string
}

func (s *testServer) stop(t *testing.T, signals ...os.Signal) {
	t.Helper()
	if s.exited {
		return
	}
	s.exited = true
	var signal os.Signal = os.Interrupt
	if len(signals) > 0 {
		signal = signals[0]
	}
	_ = s.cmd.Process.Signal(signal)
	select {
	case err := <-s.done:
		if err != nil {
			t.Errorf("server shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("server did not shut down within 5 seconds")
		_ = syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		<-s.done
	}
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }

// The wrapper execs real Chrome, retaining its PID. Puppeteer launches Chrome in
// a separate process group, so the harness must own that group's cleanup too.
func browserWrapper(t *testing.T, env []string) (string, string) {
	t.Helper()
	resolve := exec.CommandContext(deadline(t), "node", "--input-type=module", "-e", "import {executablePath} from 'puppeteer'; process.stdout.write(await executablePath())")
	resolve.Dir = root(t)
	resolve.Env = append(os.Environ(), env...)
	executable, err := resolve.Output()
	requireOK(t, err)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "browser.pids")
	wrapper := filepath.Join(dir, "chrome")
	script := "#!/bin/sh\nprintf '%s\\n' \"$$\" >> " + shellQuote(pidFile) + "\nexec " + shellQuote(string(executable)) + " \"$@\"\n"
	requireOK(t, os.WriteFile(wrapper, []byte(script), 0700))
	return wrapper, pidFile
}

func cleanupBrowsers(t *testing.T, pidFile string) {
	t.Helper()
	leaked, err := killBrowserGroups(pidFile)
	requireOK(t, err)
	if len(leaked) > 0 {
		t.Errorf("server left browser processes running: %v", leaked)
	}
}

func killBrowserGroups(pidFile string) ([]int, error) {
	data, err := os.ReadFile(pidFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var leaked []int
	for _, field := range strings.Fields(string(data)) {
		pid, err := strconv.Atoi(field)
		if err != nil || pid <= 1 {
			return leaked, fmt.Errorf("invalid owned browser PID %q", field)
		}
		if syscall.Kill(pid, 0) == nil {
			leaked = append(leaked, pid)
		}
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return leaked, err
		}
	}
	return leaked, nil
}

func startServer(t *testing.T, env ...string) *testServer {
	t.Helper()
	log, err := os.CreateTemp(t.TempDir(), "server-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	cmd := exec.Command("node", filepath.Join(root(t), "server/dist/src/server.js"), "--tcp", "127.0.0.1:0")
	cmd.Dir = root(t)
	wrapper, pidFile := browserWrapper(t, env)
	cmd.Env = append(append(os.Environ(), env...), "PUPPETEER_EXECUTABLE_PATH="+wrapper, "PUPPETEER_TMP_DIR="+t.TempDir())
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	server := &testServer{cmd: cmd, done: make(chan error, 1), browserPIDs: pidFile}
	go func() { server.done <- cmd.Wait() }()
	t.Cleanup(func() {
		server.stop(t)
		cleanupBrowsers(t, pidFile)
		if t.Failed() {
			output, _ := os.ReadFile(log.Name())
			t.Logf("server output:\n%s", output)
		}
	})
	addressPattern := regexp.MustCompile(`Server running at (127\.0\.0\.1:\d+)`)
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-server.done:
			server.exited = true
			t.Fatalf("server exited before readiness: %v", err)
		case <-timer.C:
			t.Fatal("server never signaled readiness")
		case <-tick.C:
			output, err := os.ReadFile(log.Name())
			if err != nil {
				t.Fatal(err)
			}
			match := addressPattern.FindSubmatch(output)
			if len(match) == 0 || !strings.Contains(string(output), "TERMIUM_READY") {
				continue
			}
			conn, err := grpc.NewClient(string(match[1]), grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { conn.Close() })
			server.BrowserControlClient = pb.NewBrowserControlClient(conn)
			return server
		}
	}
}

func TestServerBindFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx := deadline(t)
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root(t), "server/dist/src/server.js"), "--tcp", listener.Addr().String())
	cmd.WaitDelay = time.Second
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("bind failure hung: %s", output)
	}
	if err == nil {
		t.Fatalf("bind failure exited successfully: %s", output)
	}
	if !strings.Contains(string(output), "Failed to bind server") || strings.Contains(string(output), "TERMIUM_READY") {
		t.Fatalf("incorrect startup failure output: %s", output)
	}
}

func TestBinaryCLI(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"version", []string{"--version"}, 0, "termium "},
		{"short version", []string{"-v"}, 0, "termium "},
		{"help", []string{"--help"}, 0, "--renderer"},
		{"invalid renderer", []string{"--renderer", "bogus"}, 1, `invalid renderer "bogus"`},
		{"unknown flag", []string{"--this-flag-does-not-exist"}, 2, "flag provided but not defined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := deadline(t)
			cmd := exec.CommandContext(ctx, filepath.Join(root(t), "client/termium"), tc.args...)
			cmd.WaitDelay = time.Second
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("CLI hung: %s", output)
			}
			if cmd.ProcessState == nil {
				t.Fatalf("cannot execute binary (run npm run build): %v", err)
			}
			if cmd.ProcessState.ExitCode() != tc.code || !strings.Contains(string(output), tc.want) {
				t.Fatalf("want exit %d and %q; got exit %d: %s", tc.code, tc.want, cmd.ProcessState.ExitCode(), output)
			}
		})
	}
}

func requireOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(fmt.Errorf("operation failed: %w", err))
	}
}
