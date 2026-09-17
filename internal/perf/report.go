// Package perf defines the versioned output of the opt-in performance harness.
package perf

import (
	"encoding/json"
	"math"
	"os"
	"sort"
	"time"
)

const Schema = 1

type Control struct {
	Start    time.Time     `json:"start"`
	Duration time.Duration `json:"duration_ns"`
}

type Distribution struct {
	Samples int     `json:"samples"`
	P50     float64 `json:"p50_ms"`
	P95     float64 `json:"p95_ms"`
	P99     float64 `json:"p99_ms"`
	Max     float64 `json:"max_ms"`
}

func Summarize(values []float64) Distribution {
	if len(values) == 0 {
		return Distribution{}
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	quantile := func(q float64) float64 { return v[max(0, int(math.Ceil(q*float64(len(v))))-1)] }
	return Distribution{len(v), quantile(.5), quantile(.95), quantile(.99), v[len(v)-1]}
}

type Memory struct {
	AllocatedBytes uint64  `json:"allocated_bytes"`
	Allocations    uint64  `json:"allocations"`
	GCs            uint32  `json:"gc_cycles"`
	GCPauseMS      float64 `json:"gc_pause_ms"`
	HeapBytesAtEnd uint64  `json:"heap_bytes_at_end"`
}

type Client struct {
	Schema   int                     `json:"schema"`
	Start    time.Time               `json:"start"`
	Seconds  float64                 `json:"seconds"`
	Renderer string                  `json:"renderer"`
	Format   string                  `json:"format"`
	Width    int                     `json:"width"`
	Height   int                     `json:"height"`
	Counts   map[string]uint64       `json:"counts"`
	Latency  map[string]Distribution `json:"latency"`
	Memory   Memory                  `json:"go_memory"`
}

// Publish only complete reports. The runner treats a missing file as failure.
func WriteJSON(filename string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filename+".tmp", append(data, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(filename+".tmp", filename)
}
