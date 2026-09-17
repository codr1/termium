package main

import (
	"path/filepath"
	"strings"
	"termium/internal/perf"
	"testing"
	"time"
)

func TestComparisonRequiresCompleteRunsAndMatchingViewports(t *testing.T) {
	dir := t.TempDir()
	aFile, bFile := filepath.Join(dir, "a.json"), filepath.Join(dir, "b.json")
	a := report{Schema: perf.Schema, Complete: true, Options: options{Label: "a"}, Runs: []runResult{{Scene: "patch", Client: perf.Client{Renderer: "sixel", Format: "jpeg", Width: 640, Height: 360, Seconds: 2, Counts: map[string]uint64{"write": 20}, Latency: map[string]perf.Distribution{}}}}}
	b := a
	b.Options.Label = "b"
	if err := perf.WriteJSON(aFile, a); err != nil {
		t.Fatal(err)
	}
	if err := perf.WriteJSON(bFile, b); err != nil {
		t.Fatal(err)
	}
	if err := compareReports([]string{aFile, bFile}); err != nil {
		t.Fatal(err)
	}
	b.Complete = false
	if err := perf.WriteJSON(bFile, b); err != nil {
		t.Fatal(err)
	}
	if err := compareReports([]string{aFile, bFile}); err == nil {
		t.Fatal("accepted partial suite")
	}
	b.Complete = true
	b.Runs[0].Client.Width = 800
	if err := perf.WriteJSON(bFile, b); err != nil {
		t.Fatal(err)
	}
	if err := compareReports([]string{aFile, bFile}); err == nil {
		t.Fatal("accepted viewport mismatch")
	}
}

func TestProcCPUParsesNamesWithSpacesAndParentheses(t *testing.T) {
	stat := "42 (a tricky ) name) " + strings.Repeat("0 ", 11) + "125 75 0"
	got, err := procCPU(stat, 100)
	if err != nil || got != 2 {
		t.Fatal(got, err)
	}
	if _, err := procCPU("42 (x) 0", 100); err == nil {
		t.Fatal("accepted truncated stat")
	}
}

func TestCPUTimeFormats(t *testing.T) {
	for input, want := range map[string]float64{"01:02": 62, "01:02.50": 62.5, "02:03:04": 7384, "2-01:02:03": 176523} {
		got, err := parseCPU(input)
		if err != nil || got != want {
			t.Errorf("%s: %v %v", input, got, err)
		}
	}
	if _, err := parseCPU("bad"); err == nil {
		t.Fatal("accepted invalid CPU time")
	}
}

func TestProcessTreeAccountingExcludesWarmupAndUnrelatedProcesses(t *testing.T) {
	p := newTracker(10, 90)
	first := []processSample{
		{PID: 30, Parent: 20, CPU: 5, RSS: 300}, // Chromium precedes Node in ps.
		{PID: 10, Parent: 1, CPU: 2, RSS: 100},
		{PID: 20, Parent: 10, CPU: 3, RSS: 200},
		{PID: 90, Parent: 1, CPU: 9, RSS: 900},
		{PID: 99, Parent: 90, CPU: 100, RSS: 9999}, // Another terminal child.
	}
	if got := p.sample(first, true); len(got) != 4 {
		t.Fatal(got)
	}
	p.sample([]processSample{{PID: 10, Parent: 1, CPU: 3, RSS: 120}, {PID: 20, Parent: 10, CPU: 5, RSS: 180},
		{PID: 30, Parent: 1, CPU: 8, RSS: 320}, {PID: 31, Parent: 30, CPU: 1, RSS: 50}, {PID: 90, Parent: 1, CPU: 10, RSS: 890}}, false)
	s := p.summary(2)
	if s["client"].CPUPercent != 50 || s["server"].CPUSeconds != 2 || s["browser"].CPUSeconds != 4 || s["browser"].PeakRSS != 370 || s["terminal"].CPUSeconds != 1 {
		t.Fatal(s)
	}
}

func TestComparisonRejectsDifferentMeasurementConditions(t *testing.T) {
	a := report{Environment: map[string]string{"cpu": "one", "fixture_sha256": "a"}, Options: options{Duration: time.Second}}
	b := a
	b.Environment = map[string]string{"cpu": "two", "fixture_sha256": "a"}
	if compatible(a, b) == nil {
		t.Fatal("different CPUs accepted")
	}
	b = a
	b.Options.Display = true
	if compatible(a, b) == nil {
		t.Fatal("PTY and real terminal accepted")
	}
	b = a
	if err := compatible(a, b); err != nil {
		t.Fatal(err)
	}
	x := runResult{Scene: "patch", Client: perf.Client{Renderer: "sixel", Format: "jpeg", Width: 640, Height: 360}}
	y := x
	y.Client.Width = 800
	if runKey(x) == runKey(y) {
		t.Fatal("different viewports grouped")
	}
}
