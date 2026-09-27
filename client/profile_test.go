package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func withConfig(t *testing.T, c *Config) {
	t.Helper()
	old := cfg
	cfg = c
	t.Cleanup(func() { cfg = old })
}

func TestInitWindowProfileModesAndConflicts(t *testing.T) {
	withConfig(t, &Config{})
	report := "/tmp/termium-profile-test/client.json"

	t.Setenv(profileEnv, "")
	if p, err := initWindowProfile(report); err != nil || p != nil {
		t.Fatalf("no env: got %v, %v", p, err)
	}

	t.Setenv(profileEnv, "bogus")
	if _, err := initWindowProfile(report); err == nil {
		t.Fatal("unknown mode accepted")
	}

	t.Setenv(profileEnv, "cpu")
	withConfig(t, &Config{CPUProfile: "/tmp/termium-profile-test/full.cpuprofile"})
	if _, err := initWindowProfile(report); err == nil {
		t.Fatal("--cpuprofile conflict not rejected")
	}
	withConfig(t, &Config{})

	p, err := initWindowProfile(report)
	if err != nil || p.cpuPath != report+".cpuprofile" {
		t.Fatalf("cpu mode: %v %q", err, p.cpuPath)
	}

	t.Setenv(profileEnv, "memory")
	p, err = initWindowProfile(report)
	if err != nil || p.heapBefore != report+".heap.before" || p.heapAfter != report+".heap.after" {
		t.Fatalf("memory mode: %v %+v", err, p)
	}
}

func TestCPUProfileLifecycle(t *testing.T) {
	withConfig(t, &Config{})
	dir := t.TempDir()
	t.Setenv(profileEnv, "cpu")
	p, err := initWindowProfile(filepath.Join(dir, "client.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Cancellation while sampling must finalize a complete profile file.
	if err := p.begin(); err != nil {
		t.Fatal(err)
	}
	p.cancel()
	data, err := os.ReadFile(p.cpuPath)
	if err != nil || len(data) == 0 {
		t.Fatalf("cancelled CPU profile missing or empty: %v", err)
	}

	// begin -> end -> cancel must be idempotent and not truncate the file.
	if err := p.begin(); err != nil {
		t.Fatal(err)
	}
	if err := p.end(); err != nil {
		t.Fatal(err)
	}
	p.cancel()
	data, err = os.ReadFile(p.cpuPath)
	if err != nil || len(data) == 0 {
		t.Fatalf("finalized CPU profile missing or empty: %v", err)
	}
}

func TestProfileBeginFailureUnwritable(t *testing.T) {
	withConfig(t, &Config{})
	report := filepath.Join("/nonexistent-termium-profile-dir", "client.json")

	t.Setenv(profileEnv, "cpu")
	p, err := initWindowProfile(report)
	if err != nil {
		t.Fatal(err) // path derivation must succeed; creation fails in begin
	}
	if err := p.begin(); err == nil {
		t.Fatal("begin succeeded on unwritable path")
	}
	p.cancel() // must not panic after a failed begin

	t.Setenv(profileEnv, "memory")
	p, err = initWindowProfile(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.begin(); err == nil {
		t.Fatal("begin succeeded on unwritable path (memory mode)")
	}
	p.cancel() // must not panic after a failed begin
}

func TestMemoryProfileLifecycle(t *testing.T) {
	withConfig(t, &Config{})
	dir := t.TempDir()
	t.Setenv(profileEnv, "memory")
	p, err := initWindowProfile(filepath.Join(dir, "client.json"))
	if err != nil {
		t.Fatal(err)
	}

	// begin forces a GC and writes the pre-window cumulative snapshot.
	if err := p.begin(); err != nil {
		t.Fatal(err)
	}
	if !isGzip(t, p.heapBefore) {
		t.Fatal("heap.before missing or not a gzip pprof snapshot")
	}

	// end forces a GC and writes the post-window snapshot (also the live inuse view).
	if err := p.end(); err != nil {
		t.Fatal(err)
	}
	if !isGzip(t, p.heapAfter) {
		t.Fatal("heap.after missing or not a gzip pprof snapshot")
	}

	// cancel after end is idempotent and must not rewrite the snapshots.
	beforeBytes := readAll(t, p.heapBefore, p.heapAfter)
	p.cancel()
	for i, path := range []string{p.heapBefore, p.heapAfter} {
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, beforeBytes[i]) {
			t.Fatalf("cancel altered %s", path)
		}
	}
}

func isGzip(t *testing.T, path string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	return err == nil && len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}

func readAll(t *testing.T, paths ...string) [][]byte {
	t.Helper()
	var out [][]byte
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		out = append(out, data)
	}
	return out
}
