package main

import (
	"net"
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
	dir := t.TempDir()
	client := filepath.Join(dir, "client", "termium")
	script := filepath.Join(dir, "server", "dist", "src", "server.js")
	for _, path := range []string{client, script} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(dir, "termium-link")
	if err := os.Symlink(client, link); err != nil {
		t.Fatal(err)
	}
	// Darwin's temp directory may itself be reached through a symlink.
	resolved, err := filepath.EvalSymlinks(script)
	if err != nil {
		t.Fatal(err)
	}
	for _, executable := range []string{client, link} {
		loc := findServerNextToExecutable(executable)
		if loc == nil || loc.scriptPath != resolved {
			t.Fatalf("discovery from %q: %+v", executable, loc)
		}
		if loc.workDir != filepath.Dir(filepath.Dir(filepath.Dir(resolved))) {
			t.Fatalf("wrong workDir: %s", loc.workDir)
		}
	}
	if err := os.Remove(script); err != nil {
		t.Fatal(err)
	}
	if loc := findServerNextToExecutable(client); loc != nil {
		t.Fatalf("found missing server: %+v", loc)
	}
}

func TestServerListening(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			address := "127.0.0.1:0"
			if network == "unix" {
				// Short path also fits Darwin's Unix socket limit.
				dir, err := os.MkdirTemp("/tmp", "termium-test-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.RemoveAll(dir) })
				address = filepath.Join(dir, "s")
			}
			listener, err := net.Listen(network, address)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { listener.Close() })
			address = listener.Addr().String()
			if !serverListening(network, address) {
				t.Fatal("failed to detect live listener")
			}
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			if serverListening(network, address) {
				t.Fatal("reported closed listener as live")
			}
			if network == "unix" {
				if err := os.WriteFile(address, []byte("stale"), 0600); err != nil {
					t.Fatal(err)
				}
				if serverListening(network, address) {
					t.Fatal("reported stale file as live")
				}
				if _, err := os.Stat(address); err != nil {
					t.Fatalf("probe changed socket path: %v", err)
				}
			}
		})
	}
}

func TestStopServerNilProcess(t *testing.T) {
	// Should not panic
	serverProcess = nil
	stopServer()
}
