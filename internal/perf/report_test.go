package perf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestQuantilesAndEmptySamples(t *testing.T) {
	if got := Summarize(nil); got.Samples != 0 || got.P95 != 0 {
		t.Fatal(got)
	}
	v := []float64{100, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19}
	got := Summarize(v)
	if got.P50 != 10 || got.P95 != 19 || got.P99 != 100 || v[0] != 100 {
		t.Fatal(got, v)
	}
}

func TestReportPublication(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.json")
	if err := WriteJSON(file, map[string]int{"samples": 7}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]int
	if err = json.Unmarshal(data, &report); err != nil || report["samples"] != 7 {
		t.Fatal(string(data), err)
	}
	if _, err = os.Stat(file + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temporary report survived publication", err)
	}
}
