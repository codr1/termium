package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "termium/client/pb"
)

// ServerManager handles auto-starting and stopping the Node.js server
type ServerManager struct {
	serverCmd     *exec.Cmd
	autoStarted   bool
	serverPath    string
	connectionURL string
}

// NewServerManager creates a new server manager
func NewServerManager(cfg *Config) *ServerManager {
	// Determine connection URL
	var connURL string
	if cfg.ServerAddr != "" {
		if cfg.ServerAddr == "tcp" {
			connURL = "localhost:50051"
		} else {
			connURL = cfg.ServerAddr
		}
	} else {
		connURL = "unix:///tmp/termium.sock"
	}

	// Find the server directory relative to the client binary
	exePath, err := os.Executable()
	if err != nil {
		Debug(fmt.Sprintf("Failed to get executable path: %v", err), WARN)
		exePath, _ = os.Getwd()
	}

	// Get the directory containing the client binary
	clientDir := filepath.Dir(exePath)

	// Server is in ../server/dist/src/server.js relative to client binary
	serverPath := filepath.Join(clientDir, "..", "server", "dist", "src", "server.js")

	return &ServerManager{
		serverPath:    serverPath,
		connectionURL: connURL,
		autoStarted:   false,
	}
}

// isServerRunning checks if the server is already running by attempting a connection
func (sm *ServerManager) isServerRunning() bool {
	// Determine the target based on connection URL
	target := sm.connectionURL
	if sm.connectionURL != "unix:///tmp/termium.sock" && sm.connectionURL != "" {
		// It's already a proper target
	} else {
		target = "unix:///tmp/termium.sock"
	}

	Debug(fmt.Sprintf("Checking if server is running at %s", target), DEBUG)

	// Try to connect with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		Debug(fmt.Sprintf("Server not running: connection failed: %v", err), DEBUG)
		return false
	}
	defer conn.Close()

	// Just verify we can connect - don't make any RPC calls yet
	// The connection succeeding means the server is listening
	Debug("Server is already running", INFO)
	return true
}

// StartServer starts the Node.js server if it's not already running
func (sm *ServerManager) StartServer() error {
	// First check if server is already running
	if sm.isServerRunning() {
		Debug("Server is already running, skipping auto-start", INFO)
		return nil
	}

	// Check if server files exist
	if _, err := os.Stat(sm.serverPath); os.IsNotExist(err) {
		return fmt.Errorf("server not found at %s. Please run 'npm run build' first", sm.serverPath)
	}

	Debug(fmt.Sprintf("Starting server from %s", sm.serverPath), INFO)

	// Get the server directory
	serverDir := filepath.Join(filepath.Dir(sm.serverPath), "..", "..")

	// Create the command to start the server
	sm.serverCmd = exec.Command("node", sm.serverPath)
	sm.serverCmd.Dir = serverDir

	// Redirect server output to stderr so we can see it
	sm.serverCmd.Stdout = os.Stderr
	sm.serverCmd.Stderr = os.Stderr

	// Start the server process
	if err := sm.serverCmd.Start(); err != nil {
		return fmt.Errorf("failed to start server: %v", err)
	}

	sm.autoStarted = true
	Debug(fmt.Sprintf("Server started with PID %d", sm.serverCmd.Process.Pid), INFO)

	// Wait for server to be ready
	if err := sm.waitForServer(); err != nil {
		sm.StopServer()
		return fmt.Errorf("server failed to become ready: %v", err)
	}

	Debug("Server is ready and accepting connections", INFO)
	return nil
}

// waitForServer waits for the server to be ready to accept connections
func (sm *ServerManager) waitForServer() error {
	maxRetries := 30 // 30 seconds timeout
	retryDelay := 1 * time.Second

	for i := 0; i < maxRetries; i++ {
		if sm.isServerRunning() {
			return nil
		}

		// Check if the process has exited
		if sm.serverCmd != nil && sm.serverCmd.Process != nil {
			// Non-blocking check if process exited
			select {
			case <-time.After(retryDelay):
				// Continue waiting
			default:
			}
		}

		Debug(fmt.Sprintf("Waiting for server to be ready... (%d/%d)", i+1, maxRetries), DEBUG)
		time.Sleep(retryDelay)
	}

	return fmt.Errorf("server did not become ready within timeout")
}

// StopServer stops the auto-started server
func (sm *ServerManager) StopServer() {
	if !sm.autoStarted || sm.serverCmd == nil || sm.serverCmd.Process == nil {
		return
	}

	Debug("Stopping auto-started server", INFO)

	// Try graceful shutdown first
	if err := sm.serverCmd.Process.Signal(os.Interrupt); err != nil {
		Debug(fmt.Sprintf("Failed to send interrupt signal: %v", err), WARN)
	}

	// Wait a bit for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- sm.serverCmd.Wait()
	}()

	select {
	case <-time.After(5 * time.Second):
		// Force kill if graceful shutdown takes too long
		Debug("Server didn't stop gracefully, force killing", WARN)
		if err := sm.serverCmd.Process.Kill(); err != nil {
			Debug(fmt.Sprintf("Failed to kill server: %v", err), ERROR)
		}
	case err := <-done:
		if err != nil {
			Debug(fmt.Sprintf("Server exited with error: %v", err), DEBUG)
		} else {
			Debug("Server stopped cleanly", INFO)
		}
	}
}
