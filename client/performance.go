package main

// Opt-in benchmark instrumentation. Normal runs only encounter nil checks;
// benchmark runs retain bounded samples in memory and write once per run.
import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"termium/internal/perf"
	"time"
)

var performance *performanceRecorder

type performanceRecorder struct {
	mu                    sync.Mutex
	path                  string
	ready                 sync.Once
	active                bool
	start, end, lastWrite time.Time
	counts                map[string]uint64
	samples               map[string][]float64
	width, height         int
}

func startPerformanceRecorder() *performanceRecorder {
	path := os.Getenv("TERMIUM_PERF_REPORT")
	if path == "" {
		return nil
	}
	p := &performanceRecorder{path: path, counts: make(map[string]uint64), samples: make(map[string][]float64)}
	go func() {
		if err := p.run(); err != nil {
			fmt.Fprintln(os.Stderr, "performance report:", err)
		}
	}()
	return p
}

func (p *performanceRecorder) run() error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var control perf.Control
	for {
		data, err := os.ReadFile(p.path + ".start")
		if err == nil {
			if err := json.Unmarshal(data, &control); err != nil {
				return err
			}
			break
		}
		if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-appCtx.Done():
			return appCtx.Err()
		case <-ticker.C:
		}
	}
	if control.Duration < time.Second || control.Duration > 10*time.Minute || control.Start.IsZero() {
		return fmt.Errorf("invalid measurement window")
	}
	if !waitFrameDelay(appCtx, time.Until(control.Start)) {
		return appCtx.Err()
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	p.mu.Lock()
	p.start = time.Now()
	p.end = p.start.Add(control.Duration)
	p.active = true
	p.mu.Unlock()
	if !waitFrameDelay(appCtx, control.Duration) {
		return appCtx.Err()
	}
	p.mu.Lock()
	p.active = false
	runtime.ReadMemStats(&after)
	report := perf.Client{Schema: perf.Schema, Start: p.start, Seconds: control.Duration.Seconds(),
		Renderer: cfg.Renderer, Format: cfg.screenshotFormat(), Width: p.width, Height: p.height,
		Counts: p.counts, Latency: make(map[string]perf.Distribution),
		Memory: perf.Memory{AllocatedBytes: after.TotalAlloc - before.TotalAlloc, Allocations: after.Mallocs - before.Mallocs,
			GCs: after.NumGC - before.NumGC, GCPauseMS: float64(after.PauseTotalNs-before.PauseTotalNs) / 1e6, HeapBytesAtEnd: after.HeapAlloc}}
	for name, values := range p.samples {
		report.Latency[name] = perf.Summarize(values)
	}
	err := perf.WriteJSON(p.path, report)
	p.mu.Unlock()
	return err
}

func (p *performanceRecorder) record(name string, duration time.Duration, bytes int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.active || !time.Now().Before(p.end) {
		return
	}
	p.counts[name]++
	if bytes > 0 {
		p.counts[name+"_bytes"] += uint64(bytes)
	}
	if duration >= 0 && len(p.samples[name]) < 65536 {
		p.samples[name] = append(p.samples[name], float64(duration)/1e6)
	}
}

func (p *performanceRecorder) presented(f *Frame, elapsed time.Duration, err error) {
	if p == nil {
		return
	}
	if err != nil {
		p.record("write_error", -1, 0)
		return
	}
	p.ready.Do(func() {
		if err := perf.WriteJSON(p.path+".ready", map[string]int{"width": f.Width, "height": f.Height}); err != nil {
			fmt.Fprintln(os.Stderr, "performance readiness:", err)
		}
	})
	p.record("write", elapsed, len(f.Graphics))
	p.record("frame_age", time.Since(f.Timestamp), 0)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.width, p.height = f.Width, f.Height
	if !p.active || !time.Now().Before(p.end) {
		return
	}
	now := time.Now()
	if !p.lastWrite.IsZero() && len(p.samples["write_interval"]) < 65536 {
		p.samples["write_interval"] = append(p.samples["write_interval"], float64(now.Sub(p.lastWrite))/1e6)
	}
	p.lastWrite = now
}
