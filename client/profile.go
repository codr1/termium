package main

// Diagnostic-only opt-in profiling for benchmark runs, aligned with the
// performance recorder's warmed measurement window. Activated by the
// TERMIUM_PROFILE environment variable ("cpu" or "memory"); output paths are
// derived from the run's unique TERMIUM_PERF_REPORT path. Profiles are always
// finalized before that report is published, so the harness cannot terminate
// the client mid-profile.
//
// Boundary and sampling caveats (deliberate; do not read these profiles as a
// perfect allocation window):
//   - Heap snapshots are sampled: one allocation per runtime.MemProfileRate
//     bytes on average (default 512 KiB on this toolchain; the active rate is
//     printed to stderr at activation and recorded in each file header).
//     alloc_* deltas between before/after are estimates, not exact counts.
//   - A forced GC precedes each heap snapshot so inuse_* reflects live state
//     at snapshot time instead of lagging by up to two GC cycles (see the
//     runtime.MemProfile docs). Both GCs run outside the measurement window.
//   - Snapshots bracket the recorder window while the pipeline keeps running.
//     Sampling and snapshot serialization prevent an exact allocation-window
//     claim. UTC logs record snapshot completion and recorder boundaries, not
//     exact sampling instants. GC includes brief stop-the-world pauses; the
//     pipeline is otherwise not quiesced while collecting and writing these
//     snapshots. Treat alloc deltas as diagnostic attribution estimates.
//   - inuse_space is Go live heap at snapshot time, not process RSS.

import (
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"time"
)

const profileEnv = "TERMIUM_PROFILE"

type profileMode string

const (
	profileCPU    profileMode = "cpu"
	profileMemory profileMode = "memory"
)

// windowProfile holds the opt-in mode and derived output paths for one run.
type windowProfile struct {
	mode       profileMode
	cpuPath    string // <report>.cpuprofile: CPU samples over the measurement window
	heapBefore string // <report>.heap.before: cumulative alloc+inuse at window start (post-GC)
	heapAfter  string // <report>.heap.after: cumulative alloc+inuse and live inuse at window end (post-GC)
	cpuFile    *os.File
	started    bool // CPU profile currently sampling
	finalized  bool // profile fully written; cancel is a no-op
}

// logProfileEvent stamps diagnostic events with real UTC time for correlating
// profile files against the measurement window in client.log.
func logProfileEvent(format string, args ...any) {
	args = append([]any{time.Now().UTC().Format(time.RFC3339Nano)}, args...)
	fmt.Fprintf(os.Stderr, "window profiling %s "+format+"\n", args...)
}

// initWindowProfile validates TERMIUM_PROFILE and derives per-run paths from
// the unique performance report path. It rejects unknown modes and any
// conflict with the process-wide --cpuprofile flag, which would make windowed
// CPU sampling impossible (StartCPUProfile refuses a second profile).
func initWindowProfile(reportPath string) (*windowProfile, error) {
	raw := os.Getenv(profileEnv)
	if raw == "" {
		return nil, nil
	}
	var mode profileMode
	switch raw {
	case "cpu":
		mode = profileCPU
	case "memory":
		mode = profileMemory
	default:
		return nil, fmt.Errorf("%s=%q must be \"cpu\" or \"memory\"", profileEnv, raw)
	}
	if cfg.CPUProfile != "" {
		return nil, fmt.Errorf("%s conflicts with --cpuprofile %q; run one CPU profiling mode at a time", profileEnv, cfg.CPUProfile)
	}
	p := &windowProfile{mode: mode}
	switch mode {
	case profileCPU:
		p.cpuPath = reportPath + ".cpuprofile"
	default:
		p.heapBefore = reportPath + ".heap.before"
		p.heapAfter = reportPath + ".heap.after"
	}
	logProfileEvent("enabled: mode=%s memProfileRate=%d (default, unchanged)", raw, runtime.MemProfileRate)
	return p, nil
}

// begin starts the windowed profile at measurement-window start. CPU mode
// begins sampling; memory mode forces a GC and writes its pre-window
// cumulative snapshot. Both run outside the recorder mutex and before the
// window's MemStats(before), so diagnostic work stays out of recorder totals.
func (p *windowProfile) begin() error {
	if p == nil {
		return nil
	}
	switch p.mode {
	case profileCPU:
		f, err := os.Create(p.cpuPath)
		if err != nil {
			return fmt.Errorf("create CPU profile %s: %w", p.cpuPath, err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			f.Close()
			return fmt.Errorf("start CPU profile: %w", err)
		}
		p.cpuFile = f
		p.started = true
		logProfileEvent("CPU profile started -> %s", p.cpuPath)
	case profileMemory:
		runtime.GC() // outside the measurement window; avoids up to two GC cycles of inuse lag
		if err := writeHeapSnapshot(p.heapBefore); err != nil {
			return err
		}
		logProfileEvent("heap snapshot before window (forced GC first) -> %s", p.heapBefore)
	}
	return nil
}

// end finalizes the active profile at measurement-window end. CPU sampling is
// stopped; memory mode forces a GC and writes its post-window cumulative
// snapshot, which also serves as the live inuse view (caches/arenas still
// exist). Called after the recorder has frozen active=false and read
// MemStats(after), outside the recorder mutex.
func (p *windowProfile) end() error {
	if p == nil || p.finalized {
		return nil
	}
	switch p.mode {
	case profileCPU:
		if p.started {
			pprof.StopCPUProfile() // returns only after all profile writes complete
			p.started = false
			if err := p.cpuFile.Close(); err != nil {
				return fmt.Errorf("close CPU profile %s: %w", p.cpuPath, err)
			}
		}
		logProfileEvent("CPU profile finalized -> %s", p.cpuPath)
	case profileMemory:
		runtime.GC() // forced so the post-window inuse view does not lag by up to two GC cycles
		if err := writeHeapSnapshot(p.heapAfter); err != nil {
			return err
		}
		logProfileEvent("heap snapshot after window (forced GC first; live inuse view) -> %s", p.heapAfter)
	}
	p.finalized = true
	return nil
}

// cancel finalizes whatever is active so a shutdown cannot leave a truncated
// CPU profile. Idempotent; safe when nothing was started or end already ran.
func (p *windowProfile) cancel() {
	if p == nil || p.finalized {
		return
	}
	if p.mode == profileCPU && p.started {
		pprof.StopCPUProfile()
		p.started = false
		if err := p.cpuFile.Close(); err != nil {
			logProfileEvent("close CPU profile %s on cancellation failed: %v", p.cpuPath, err)
			return
		}
		logProfileEvent("CPU profile finalized on cancellation -> %s", p.cpuPath)
	}
}

// writeHeapSnapshot writes one cumulative heap snapshot containing inuse and
// alloc sample types (the sampling rate is recorded in the file header). It
// does not itself force a GC; callers do that when they need current inuse.
func writeHeapSnapshot(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create heap snapshot %s: %w", path, err)
	}
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		f.Close()
		return fmt.Errorf("write heap snapshot %s: %w", path, err)
	}
	return f.Close()
}
