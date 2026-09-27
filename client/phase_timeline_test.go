package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPhaseTimelinePreservesOpenWaitAndFreezes(t *testing.T) {
	old := pipelineTimeline
	t.Cleanup(func() { pipelineTimeline = old })
	pipelineTimeline = &phaseTimeline{}
	wait := tracePhaseBegin("prepare.wait", time.Time{})
	start := time.Now()
	work := tracePhaseBegin("capture.rpc", start)
	tracePhaseEnd(work)
	end := time.Now()
	path := filepath.Join(t.TempDir(), "client.json")
	if err := pipelineTimeline.save(path, start, end); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path + ".timeline.json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Events []phaseEvent `json:"events"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Events) != 2 || got.Events[0].EndUS != end.UnixMicro() || got.Events[0].StartUS > start.UnixMicro() || got.Events[1].FrameUS != start.UnixMicro() {
		t.Fatalf("lost a boundary-spanning wait or frame identity: %+v", got.Events)
	}
	tracePhaseEnd(wait)
	tracePhaseBegin("too-late", time.Time{})
	if len(pipelineTimeline.events) != 2 || pipelineTimeline.events[0].EndUS != end.UnixMicro() {
		t.Fatal("late completion changed frozen timeline")
	}
}

func TestServerDiagnosticsRequiresSuccessfulCompletion(t *testing.T) {
	t.Setenv("TERMIUM_CAPTURE_TRACE", "1")
	path := filepath.Join(t.TempDir(), "client.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitServerDiagnostics(ctx, path); err == nil {
		t.Fatal("missing trace ignored cancellation")
	}
	for _, tc := range []struct {
		json string
		ok   bool
	}{{`{"error":"trace lost data"}`, false}, {`{}`, false}, {`{"complete":true}`, true}} {
		if err := os.WriteFile(path+".server.done", []byte(tc.json), 0600); err != nil {
			t.Fatal(err)
		}
		if err := waitServerDiagnostics(context.Background(), path); (err == nil) != tc.ok {
			t.Fatalf("%s: %v", tc.json, err)
		}
	}
}
