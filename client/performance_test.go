package main

import (
	"sync"
	"testing"
	"time"
)

func TestPerformanceWindowAndConcurrentCounters(t *testing.T) {
	p := &performanceRecorder{counts: map[string]uint64{}, samples: map[string][]float64{}}
	p.record("capture", time.Millisecond, 100)
	if len(p.counts) != 0 {
		t.Fatal("warmup counted")
	}
	p.active = true
	p.end = time.Now().Add(time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				p.record("capture", 2*time.Millisecond, 100)
			}
		}()
	}
	wg.Wait()
	if p.counts["capture"] != 400 || p.counts["capture_bytes"] != 40000 || len(p.samples["capture"]) != 400 {
		t.Fatal(p.counts)
	}
	p.end = time.Now().Add(-time.Second)
	p.record("capture", time.Millisecond, 100)
	if p.counts["capture"] != 400 {
		t.Fatal("late event counted")
	}
	var disabled *performanceRecorder
	disabled.record("capture", 0, 0)
}
