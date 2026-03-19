package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	serverReadySentinel = "TERMIUM_READY"
	serverStartTimeout  = 15 * time.Second
	defaultSocketPath   = "/tmp/termium.sock"
)

// serverProcess holds the child process if we started the server
var serverProcess *exec.Cmd

// serverLocation holds the resolved server.js path and its working directory.
type serverLocation struct {
	scriptPath string // path to server.js
	workDir    string // directory to run node from
}

// findServerBinary locates the server entry point (server.js).
// Search order:
//  1. $TERMIUM_SERVER env var (explicit override)
//  2. Relative to client binary: ../server/dist/src/server.js (dev layout)
//  3. ~/.termium/server/dist/src/server.js (installed layout)
func findServerBinary() (*serverLocation, error) {
	// 1. Explicit env var
	if envPath := os.Getenv("TERMIUM_SERVER"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return &serverLocation{envPath, filepath.Dir(envPath)}, nil
		}
		return nil, fmt.Errorf("TERMIUM_SERVER=%q does not exist", envPath)
	}

	// 2. Relative to client binary (dev layout: client/termium -> server/dist/src/server.js)
	exe, err := os.Executable()
	if err == nil {
		exe, _ = filepath.EvalSymlinks(exe)
		serverDir := filepath.Join(filepath.Dir(exe), "..", "server")
		devPath := filepath.Join(serverDir, "dist", "src", "server.js")
		if _, err := os.Stat(devPath); err == nil {
			return &serverLocation{devPath, serverDir}, nil
		}
	}

	// 3. Installed layout (~/.termium/server/dist/src/server.js)
	home, err := os.UserHomeDir()
	if err == nil {
		serverDir := filepath.Join(home, ".termium", "server")
		installPath := filepath.Join(serverDir, "dist", "src", "server.js")
		if _, err := os.Stat(installPath); err == nil {
			return &serverLocation{installPath, serverDir}, nil
		}
	}

	return nil, fmt.Errorf("server not found: set TERMIUM_SERVER or install to ~/.termium/server/")
}

// isServerRunning checks if the server is actually accepting connections.
// A stale socket file from a crashed server won't fool this.
func isServerRunning() bool {
	if cfg.ServerAddr != "" {
		// TCP mode — try to connect
		conn, err := net.DialTimeout("tcp", cfg.ServerAddr, 500*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}
	// Unix socket — try to connect, not just stat the file
	conn, err := net.DialTimeout("unix", defaultSocketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// startServer launches the server as a child process and waits for it to
// signal readiness. Returns nil if the server is already running.
func startServer() error {
	if isServerRunning() {
		Debug("Server already running, skipping auto-launch", INFO)
		return nil
	}

	loc, err := findServerBinary()
	if err != nil {
		return err
	}

	Debug(fmt.Sprintf("Auto-launching server: node %s (workdir: %s)", loc.scriptPath, loc.workDir), INFO)

	// Find node binary
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("node not found in PATH: %v", err)
	}

	serverProcess = exec.Command(nodePath, loc.scriptPath)
	serverProcess.Dir = loc.workDir

	// Capture stdout to watch for readiness sentinel
	stdout, err := serverProcess.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to capture server stdout: %v", err)
	}

	// Let server stderr pass through for debugging
	serverProcess.Stderr = os.Stderr

	if err := serverProcess.Start(); err != nil {
		return fmt.Errorf("failed to start server: %v", err)
	}

	Debug(fmt.Sprintf("Server process started (pid=%d), waiting for readiness...", serverProcess.Process.Pid), INFO)

	// Wait for readiness sentinel or timeout
	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			Debug(fmt.Sprintf("Server: %s", line), DEBUG)
			if strings.TrimSpace(line) == serverReadySentinel {
				ready <- nil
				// Keep draining stdout so the pipe doesn't block
				go func() {
					for scanner.Scan() {
						Debug(fmt.Sprintf("Server: %s", scanner.Text()), DEBUG)
					}
				}()
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ready <- fmt.Errorf("error reading server output: %v", err)
		} else {
			ready <- fmt.Errorf("server exited before signaling readiness")
		}
	}()

	select {
	case err := <-ready:
		if err != nil {
			stopServer()
			return err
		}
		Debug("Server signaled readiness", INFO)
		return nil
	case <-time.After(serverStartTimeout):
		stopServer()
		return fmt.Errorf("server failed to start within %v", serverStartTimeout)
	}
}

// stopServer kills the server child process if we started it.
func stopServer() {
	if serverProcess == nil || serverProcess.Process == nil {
		return
	}

	Debug(fmt.Sprintf("Stopping server (pid=%d)", serverProcess.Process.Pid), INFO)

	// Try graceful shutdown first
	if err := serverProcess.Process.Signal(os.Interrupt); err != nil {
		Debug(fmt.Sprintf("Failed to send SIGINT to server: %v", err), WARN)
		serverProcess.Process.Kill()
		return
	}

	// Wait briefly for graceful exit
	done := make(chan error, 1)
	go func() { done <- serverProcess.Wait() }()

	select {
	case <-done:
		Debug("Server stopped gracefully", INFO)
	case <-time.After(3 * time.Second):
		Debug("Server didn't stop gracefully, killing", WARN)
		serverProcess.Process.Kill()
		serverProcess.Wait()
	}

	serverProcess = nil
}
