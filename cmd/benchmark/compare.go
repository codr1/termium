package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"termium/internal/perf"
)

func loadReport(filename string) (report, error) {
	var r report
	data, err := os.ReadFile(filename)
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(data, &r); err != nil {
		return r, err
	}
	if r.Schema != perf.Schema || !r.Complete || len(r.Runs) == 0 {
		return r, fmt.Errorf("incomplete or incompatible report %s", filename)
	}
	return r, nil
}

func compatible(a, b report) error {
	for _, key := range []string{"os", "arch", "os_release", "cpu", "cpu_accounting", "chromium", "node", "go", "fixture_sha256"} {
		if a.Environment[key] != b.Environment[key] {
			return fmt.Errorf("cannot compare different %s: %q vs %q", key, a.Environment[key], b.Environment[key])
		}
	}
	if a.Options.Display != b.Options.Display || a.Options.Palette != b.Options.Palette || a.Options.Duration != b.Options.Duration || a.Options.Warmup != b.Options.Warmup {
		return fmt.Errorf("sink, palette, duration, and warmup must match")
	}
	if (a.Options.TerminalPID != 0) != (b.Options.TerminalPID != 0) {
		return fmt.Errorf("terminal process sampling must be enabled in both reports or neither")
	}
	if a.Options.Display && (a.Environment["terminal"] != b.Environment["terminal"] || a.Environment["terminal_program"] != b.Environment["terminal_program"]) {
		return fmt.Errorf("terminal environments differ")
	}
	return nil
}

func runKey(r runResult) string {
	return fmt.Sprintf("%s/%s/%s/%dx%d", r.Scene, r.Client.Renderer, r.Client.Format, r.Client.Width, r.Client.Height)
}

func measurements(r runResult) map[string]float64 {
	c := r.Client
	m := map[string]float64{
		"writes/s":           float64(c.Counts["write"]) / c.Seconds,
		"captures/s":         float64(c.Counts["capture"]) / c.Seconds,
		"capture p95 ms":     c.Latency["capture"].P95,
		"prepare p95 ms":     c.Latency["prepare"].P95,
		"write p95 ms":       c.Latency["write"].P95,
		"age p95 ms":         c.Latency["frame_age"].P95,
		"Go allocated MiB/s": float64(c.Memory.AllocatedBytes) / c.Seconds / (1024 * 1024),
		"graphics MiB/s":     float64(c.Counts["write_bytes"]) / c.Seconds / (1024 * 1024),
		"pending drops/s":    float64(c.Counts["raw_superseded"]+c.Counts["prepared_superseded"]) / c.Seconds,
	}
	for role, s := range r.Resources {
		m[role+" CPU %"] = s.CPUPercent
		m[role+" peak RSS MiB"] = float64(s.PeakRSS) / (1024 * 1024)
	}
	return m
}

func compareReports(files []string) error {
	if len(files) != 2 {
		return fmt.Errorf("--compare requires before/result.json and after/result.json")
	}
	a, err := loadReport(files[0])
	if err != nil {
		return err
	}
	b, err := loadReport(files[1])
	if err != nil {
		return err
	}
	if err = compatible(a, b); err != nil {
		return err
	}
	group := func(r report) map[string][]runResult {
		out := map[string][]runResult{}
		for _, run := range r.Runs {
			key := runKey(run)
			out[key] = append(out[key], run)
		}
		return out
	}
	aa, bb := group(a), group(b)
	if len(aa) != len(bb) {
		return fmt.Errorf("scenario/renderer/format/viewport sets differ")
	}
	keys := []string{}
	for key := range aa {
		if len(bb[key]) == 0 {
			return fmt.Errorf("missing matching configuration %s", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fmt.Printf("%s → %s\nMedians across repeats; ranges show run-to-run variability. CPU 100%% = one core. Writes are not visible FPS.\n", a.Options.Label, b.Options.Label)
	for _, key := range keys {
		fmt.Println("\n" + key)
		metricKeys := []string{}
		for metric := range measurements(aa[key][0]) {
			metricKeys = append(metricKeys, metric)
		}
		sort.Strings(metricKeys)
		for _, metric := range metricKeys {
			collect := func(runs []runResult) []float64 {
				var v []float64
				for _, r := range runs {
					if x, ok := measurements(r)[metric]; ok {
						v = append(v, x)
					}
				}
				return v
			}
			av, bv := collect(aa[key]), collect(bb[key])
			if len(av) == 0 || len(bv) == 0 {
				continue
			}
			x, y := perf.Summarize(av).P50, perf.Summarize(bv).P50
			delta := "n/a"
			if x != 0 {
				delta = fmt.Sprintf("%+.1f%%", (y/x-1)*100)
			}
			sort.Float64s(av)
			sort.Float64s(bv)
			fmt.Printf("  %-23s %9.2f → %9.2f (%s)  ranges %.2f–%.2f / %.2f–%.2f\n", metric, x, y, delta, av[0], av[len(av)-1], bv[0], bv[len(bv)-1])
		}
	}
	return nil
}
