package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type processSample struct {
	PID     int     `json:"pid"`
	Parent  int     `json:"parent"`
	CPU     float64 `json:"cpu_seconds"`
	RSS     uint64  `json:"rss_bytes"`
	Command string  `json:"command"`
	Role    string  `json:"role"`
}

type resourceSummary struct {
	CPUSeconds float64 `json:"cpu_seconds"`
	CPUPercent float64 `json:"cpu_percent_one_core"`
	PeakRSS    uint64  `json:"peak_sum_rss_bytes"`
}

var tickOnce sync.Once
var ticksPerSecond float64
var tickError error

func clockTicks(ctx context.Context) (float64, error) {
	tickOnce.Do(func() {
		data, err := exec.CommandContext(ctx, "getconf", "CLK_TCK").Output()
		if err != nil {
			tickError = err
			return
		}
		ticksPerSecond, tickError = strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if tickError == nil && ticksPerSecond <= 0 {
			tickError = fmt.Errorf("invalid clock tick rate")
		}
	})
	return ticksPerSecond, tickError
}

func procCPU(data string, ticks float64) (float64, error) {
	// The parenthesized comm field may itself contain spaces and parentheses.
	end := strings.LastIndex(data, ")")
	if end < 0 {
		return 0, fmt.Errorf("invalid proc stat")
	}
	fields := strings.Fields(data[end+1:])
	if len(fields) < 13 || ticks <= 0 {
		return 0, fmt.Errorf("truncated proc stat")
	}
	user, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, err
	}
	system, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, err
	}
	return float64(user+system) / ticks, nil
}

// ps provides ancestry/RSS on both systems. Linux's proc CPU ticks avoid ps's
// whole-second rounding; macOS ps supplies fractional seconds. Keep raw samples.
func processSnapshot(ctx context.Context) ([]processSample, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var ticks float64
	if runtime.GOOS == "linux" {
		var err error
		ticks, err = clockTicks(ctx)
		if err != nil {
			return nil, err
		}
	}
	data, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,time=,rss=,comm=").Output()
	if err != nil {
		return nil, err
	}
	var result []processSample
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, err
		}
		parent, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, err
		}
		cpu, err := parseCPU(fields[2])
		if err != nil {
			return nil, err
		}
		if runtime.GOOS == "linux" {
			// A process can exit between ps and this read; retain ps's sample.
			if stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
				cpu, err = procCPU(string(stat), ticks)
				if err != nil {
					return nil, err
				}
			}
		}
		rss, err := strconv.ParseUint(fields[3], 10, 64)
		if err != nil {
			return nil, err
		}
		result = append(result, processSample{PID: pid, Parent: parent, CPU: cpu, RSS: rss * 1024, Command: strings.Join(fields[4:], " ")})
	}
	return result, nil
}

func parseCPU(text string) (float64, error) {
	days := 0.
	if d, rest, ok := strings.Cut(text, "-"); ok {
		v, err := strconv.ParseFloat(d, 64)
		if err != nil {
			return 0, err
		}
		days, text = v, rest
	}
	parts := strings.Split(text, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, fmt.Errorf("invalid ps CPU time %q", text)
	}
	var seconds float64
	for _, part := range parts {
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0, err
		}
		seconds = seconds*60 + v
	}
	return days*86400 + seconds, nil
}

type processTracker struct {
	root, terminal int
	roles          map[int]string
	baseline, last map[int]float64
	peak           map[string]uint64
}

func newTracker(root, terminal int) *processTracker {
	return &processTracker{root: root, terminal: terminal, roles: map[int]string{root: "client"},
		baseline: map[int]float64{}, last: map[int]float64{}, peak: map[string]uint64{}}
}

func (p *processTracker) sample(all []processSample, first bool) []processSample {
	if p.terminal > 0 {
		p.roles[p.terminal] = "terminal"
	}
	// Discover ancestry regardless of ps ordering. Detached Chromium children
	// remain tracked after discovery, even if their parent subsequently exits.
	for changed := true; changed; {
		changed = false
		for _, s := range all {
			if _, ok := p.roles[s.PID]; ok {
				continue
			}
			role, ok := p.roles[s.Parent]
			if !ok {
				continue
			}
			if role == "terminal" {
				continue
			} // Other tabs/apps are not Termium.
			if role == "client" {
				role = "server"
			} else {
				role = "browser"
			}
			p.roles[s.PID] = role
			changed = true
		}
	}
	var selected []processSample
	rss := map[string]uint64{}
	for _, s := range all {
		role, ok := p.roles[s.PID]
		if !ok {
			continue
		}
		s.Role = role
		selected = append(selected, s)
		if first {
			p.baseline[s.PID] = s.CPU
		}
		p.last[s.PID] = max(p.last[s.PID], s.CPU)
		rss[role] += s.RSS
	}
	for role, bytes := range rss {
		p.peak[role] = max(p.peak[role], bytes)
	}
	return selected
}

func (p *processTracker) summary(seconds float64) map[string]resourceSummary {
	out := map[string]resourceSummary{}
	for pid, last := range p.last {
		role := p.roles[pid]
		s := out[role]
		s.CPUSeconds += max(0, last-p.baseline[pid])
		s.PeakRSS = p.peak[role]
		out[role] = s
	}
	for role, s := range out {
		s.CPUPercent = s.CPUSeconds / seconds * 100
		out[role] = s
	}
	return out
}
