package main

// Bounded, diagnostic-only wall-time spans. Work spans are elapsed time, not
// CPU accounting; sampled CPU profiles provide that separate view.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"termium/internal/perf"
)

var pipelineTimeline *phaseTimeline

type phaseEvent struct {
	Name    string `json:"name"`
	StartUS int64  `json:"start_us"`
	EndUS   int64  `json:"end_us"`
	FrameUS int64  `json:"frame_us,omitempty"`
	start   time.Time
}
type phaseTimeline struct {
	mu      sync.Mutex
	events  []phaseEvent
	dropped int
	sealed  bool
}
type phaseToken struct {
	timeline *phaseTimeline
	index    int
}

func tracePhaseBegin(name string, frame time.Time) phaseToken {
	t := pipelineTimeline
	if t == nil {
		return phaseToken{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sealed {
		return phaseToken{}
	}
	if len(t.events) >= 16384 {
		t.dropped++
		return phaseToken{}
	}
	now := time.Now()
	e := phaseEvent{Name: name, StartUS: now.UnixMicro(), start: now}
	if !frame.IsZero() {
		e.FrameUS = frame.UnixMicro()
	}
	t.events = append(t.events, e)
	return phaseToken{t, len(t.events) - 1}
}
func tracePhaseEnd(token phaseToken) {
	t := token.timeline
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.sealed {
		e := &t.events[token.index]
		e.EndUS = e.start.Add(time.Since(e.start)).UnixMicro()
	}
}
func (t *phaseTimeline) save(path string, start, end time.Time) error {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	t.sealed = true
	// Keep intervals crossing either boundary; analysis clips them to this
	// window. A pending read or in-flight preparation is closed at the end.
	for i := range t.events {
		if t.events[i].EndUS == 0 {
			t.events[i].EndUS = max(end.UnixMicro(), t.events[i].StartUS)
		}
	}
	report := struct {
		StartUS int64        `json:"start_us"`
		EndUS   int64        `json:"end_us"`
		Dropped int          `json:"dropped"`
		Events  []phaseEvent `json:"events"`
	}{start.UnixMicro(), end.UnixMicro(), t.dropped, t.events}
	t.mu.Unlock()
	return perf.WriteJSON(path+".timeline.json", report)
}

func waitServerDiagnostics(ctx context.Context, path string) error {
	if os.Getenv("TERMIUM_CAPTURE_TRACE") != "1" {
		return nil
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(4 * time.Second)
	defer timeout.Stop()
	for {
		data, err := os.ReadFile(path + ".server.done")
		if err == nil {
			var result struct {
				Complete bool   `json:"complete"`
				Error    string `json:"error"`
			}
			if err := json.Unmarshal(data, &result); err != nil {
				return err
			}
			if result.Error != "" || !result.Complete {
				return fmt.Errorf("server diagnostics incomplete: %s", result.Error)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C:
			return fmt.Errorf("server diagnostics did not finalize within 4s")
		case <-ticker.C:
		}
	}
}
