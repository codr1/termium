//go:build integration

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	pb "termium/client/pb"
)

func TestShutdownWithLiveStreams(t *testing.T) {
	server := startServer(t)
	ctx := deadline(t)
	_, err := server.OpenTab(ctx, &pb.Empty{})
	requireOK(t, err)
	dialogs, err := server.StreamDialogs(ctx)
	requireOK(t, err)
	_, err = dialogs.Header()
	requireOK(t, err)
	frames, err := server.StreamScreenshots(ctx, &pb.ScreenshotRequest{Format: "png", Fps: 10})
	requireOK(t, err)
	_, err = frames.Recv()
	requireOK(t, err)
	// Keep both streams and the connection open during shutdown. Closing the
	// client first would hide a server that waits forever on active streams.
	server.stop(t)
	if _, err = dialogs.Recv(); err == nil {
		t.Error("dialog stream survived shutdown")
	}
	for {
		if _, err = frames.Recv(); err != nil {
			break
		}
	}
}

func TestCleanupKillsDetachedBrowserProcess(t *testing.T) {
	// Model the detached process left behind if the server needs SIGKILL.
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	requireOK(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		if cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
	})
	pidFile := filepath.Join(t.TempDir(), "browser.pids")
	requireOK(t, os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0600))
	leaked, err := killBrowserGroups(pidFile)
	requireOK(t, err)
	if len(leaked) != 1 || leaked[0] != cmd.Process.Pid {
		t.Fatalf("failed to detect leaked process: %v", leaked)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("detached process exited without being killed")
	}
	if syscall.Kill(cmd.Process.Pid, 0) != syscall.ESRCH {
		t.Fatal("detached browser process survived cleanup")
	}
}

func TestShutdownDuringBrowserLaunch(t *testing.T) {
	// A browser executable that starts but never opens DevTools makes this race
	// deterministic. The same PID wrapper used for Chrome checks for leaks.
	blocked := filepath.Join(t.TempDir(), "blocked-chrome")
	requireOK(t, os.WriteFile(blocked, []byte("#!/bin/sh\nexec sleep 60\n"), 0700))
	server := startServer(t, "PUPPETEER_EXECUTABLE_PATH="+blocked)
	ctx := deadline(t)
	opened := make(chan error, 1)
	go func() { _, err := server.OpenTab(ctx, &pb.Empty{}); opened <- err }()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		data, _ := os.ReadFile(server.browserPIDs)
		if len(data) > 0 {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("browser process never started")
		}
	}
	server.stop(t)
	if err := <-opened; err == nil {
		t.Error("incomplete launch reported success")
	}
}

func TestShutdownWithPendingDialog(t *testing.T) {
	server := startServer(t)
	ctx := deadline(t)
	url, _ := fixture(t)
	_, err := server.NavigateToUrl(ctx, &pb.Url{Url: url})
	requireOK(t, err)
	dialogs, err := server.StreamDialogs(ctx)
	requireOK(t, err)
	_, err = dialogs.Header()
	requireOK(t, err)
	clicked := make(chan error, 1)
	go func() { _, err := server.ClickMouse(ctx, &pb.Coordinate{X: 30, Y: 150}); clicked <- err }()
	_, err = dialogs.Recv()
	requireOK(t, err)
	// SIGTERM must also close successfully when a page is waiting for a reply.
	server.stop(t, syscall.SIGTERM)
	<-clicked
}
