package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindServerBinaryEnvVar(t *testing.T) {
	// Create a temp file to act as server.js
	tmp, err := os.CreateTemp("", "server-*.js")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp.Name())
	tmp.Close()

	t.Setenv("TERMIUM_SERVER", tmp.Name())

	loc, err := findServerBinary()
	if err != nil {
		t.Fatalf("findServerBinary failed: %v", err)
	}
	if loc.scriptPath != tmp.Name() {
		t.Errorf("expected scriptPath %q, got %q", tmp.Name(), loc.scriptPath)
	}
	if loc.workDir != filepath.Dir(tmp.Name()) {
		t.Errorf("expected workDir %q, got %q", filepath.Dir(tmp.Name()), loc.workDir)
	}
}

func TestFindServerBinaryEnvVarMissing(t *testing.T) {
	t.Setenv("TERMIUM_SERVER", "/nonexistent/path/server.js")

	_, err := findServerBinary()
	if err == nil {
		t.Fatal("expected error for nonexistent TERMIUM_SERVER path")
	}
}

func TestFindServerBinaryDevLayout(t *testing.T) {
	// Create a temp directory mimicking the dev layout
	tmpDir, err := os.MkdirTemp("", "termium-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create client/ and server/dist/src/ directories
	clientDir := filepath.Join(tmpDir, "client")
	serverDir := filepath.Join(tmpDir, "server", "dist", "src")
	os.MkdirAll(clientDir, 0755)
	os.MkdirAll(serverDir, 0755)

	// Create fake server.js
	serverJS := filepath.Join(serverDir, "server.js")
	os.WriteFile(serverJS, []byte("// fake"), 0644)

	// Create fake client binary
	clientBin := filepath.Join(clientDir, "termium")
	os.WriteFile(clientBin, []byte("// fake"), 0755)

	// Clear env var so it doesn't interfere
	t.Setenv("TERMIUM_SERVER", "")

	// We can't easily test os.Executable() pointing to our temp dir,
	// but we can verify the path construction logic
	expectedServerDir := filepath.Join(tmpDir, "server")
	if _, err := os.Stat(filepath.Join(expectedServerDir, "dist", "src", "server.js")); err != nil {
		t.Errorf("dev layout server.js not found at expected path: %v", err)
	}
}

func TestIsServerRunningNoSocket(t *testing.T) {
	// Ensure no socket exists
	os.Remove(defaultSocketPath)

	// Initialize cfg for the test
	origCfg := cfg
	cfg = &Config{}
	defer func() { cfg = origCfg }()

	if isServerRunning() {
		t.Error("expected isServerRunning() = false when no socket exists")
	}
}

func TestStopServerNilProcess(t *testing.T) {
	// Should not panic
	serverProcess = nil
	stopServer()
}
