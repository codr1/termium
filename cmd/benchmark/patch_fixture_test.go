package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// The patch scene's visual schedule lives in fixture.html, which is embedded
// into every run and recorded as fixture_sha256 in result.json; the comparison
// command refuses to pair runs with different fixtures. Changing this fixture
// therefore requires matching fixture versions on both sides of any future
// end-to-end comparison.
//
// These tests execute that actual script — not a reimplementation — under
// controlled timestamps and animation callbacks, then observe the colors it
// assigns to #patch at capture times. The retired schedule changed color at
// each logical frame (about every 1/30 s) and returned to the same color every
// other frame (period about 1/15 s); a capture rhythm of ~15 Hz — one sample
// per color period, as in the CI smoke artifact "30 captures, 30 reuses" — is
// consistent with all screenshots being byte-identical. The current schedule
// must therefore show changing pixels under that sampling and under dropped
// animation ticks: reverting to a two-state toggle or collapsing the palette
// would leave no observed change at 15 Hz. main.go's zero-write guard remains
// the runtime backstop for real runs.

const (
	patchWindowMs    = 2000.0 // CI smoke measurement window; equals the new schedule's period
	patchPhaseStepMs = 1.0    // phase grid: resolves every state boundary (shortest state 250 ms) two orders of magnitude finer
)

var patchRates = []struct {
	name string
	hz   float64
}{
	{"2 Hz", 2}, {"4 Hz", 4}, {"6 Hz", 6}, {"7.5 Hz", 7.5}, {"8 Hz", 8},
	{"10 Hz", 10}, {"12 Hz", 12}, {"15 Hz (CI observed)", 15},
	{"20 Hz", 20}, {"24 FPS cap", 24}, {"30 Hz", 30},
}

type fixtureAssignment struct {
	T     float64 `json:"t"`
	Color string  `json:"color"`
}

type skipLog struct {
	Skip        int                 `json:"skip"`
	Assignments []fixtureAssignment `json:"assignments"`
}

// patchScript returns the <script> body of the embedded fixture — the code under test.
func patchScript(t *testing.T) string {
	t.Helper()
	const open, close = "<script>", "</script>"
	start := strings.Index(fixture, open)
	end := strings.Index(fixture, close)
	if start < 0 || end <= start+len(open) {
		t.Fatalf("fixture.html: script block not found")
	}
	return fixture[start+len(open) : end]
}

// patchDriver is the Node harness. It runs the actual fixture script with
// stubbed browser globals, pumps requestAnimationFrame at controlled
// timestamps — only every skip-th nominal 30 Hz tick fires, and pending
// callbacks drain one per fired tick in order — and records each color the
// script assigns to #patch together with its timestamp.
const patchDriver = `
const FIXTURE = __FIXTURE__;
const SKIPS = __SKIPS__;
const TICK_MS = 1000 / 30; // nominal animation tick at target 30 Hz

function makeEnv() {
	const env = { now: 0, queue: [], assignments: [] };
	const patchEl = { style: {} };
	Object.defineProperty(patchEl.style, 'background', {
		set(value) { env.assignments.push({ t: env.now, color: value }); }
	});
	const ctxStub = {
		createImageData() { return { data: new Uint8ClampedArray(4) }; },
		putImageData() {}, createPattern() { return {}; },
		save() {}, translate() {}, fillRect() {}, restore() {}
	};
	const makeCanvas = () => ({ width: 0, height: 0, style: {}, getContext: () => ctxStub });
	const mainEl = { append() {} };
	const canvasEl = makeCanvas();
	env.document = {
		querySelector(sel) {
			if (sel === 'main') return mainEl;
			if (sel === '#patch') return patchEl;
			if (sel === 'canvas') return canvasEl;
			throw new Error('unexpected selector: ' + sel);
		},
		createElement(tag) {
			if (tag === 'p') return { textContent: '' };
			if (tag === 'canvas') return makeCanvas();
			throw new Error('unexpected element: ' + tag);
		}
	};
	env.performance = { now() { return env.now; } };
	env.requestAnimationFrame = (cb) => { env.queue.push(cb); return env.queue.length; };
	env.fetch = () => Promise.resolve({ ok: true });
	env.location = { search: '?scene=patch' };
	env.innerWidth = 1280;
	env.innerHeight = 720;
	return env;
}

function runFixture(env) {
	new Function('document', 'performance', 'requestAnimationFrame', 'fetch', 'location', 'innerWidth', 'innerHeight', FIXTURE)(
		env.document, env.performance, env.requestAnimationFrame, env.fetch, env.location, env.innerWidth, env.innerHeight);
}

function animate(skip, untilMs) {
	const env = makeEnv();
	runFixture(env);
	if (env.queue.length !== 1) throw new Error('expected exactly one pending animation callback after load');
	for (let i = 1; env.now < untilMs && env.queue.length > 0; i++) {
		if (i % skip !== 0) continue;
		env.now = i * TICK_MS;
		env.queue.shift()(env.now);
	}
	return env.assignments;
}

process.stdout.write(JSON.stringify(SKIPS.map((skip) => ({ skip, assignments: animate(skip, 4500) }))));
`

// runPatchFixtureHarness executes the actual fixture script in Node for each
// skip setting and returns the recorded #patch color assignments.
func runPatchFixtureHarness(t *testing.T, skips []int) []skipLog {
	t.Helper()
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("node not found in PATH; skipping fixture script tests")
	}
	scriptLit, err := json.Marshal(patchScript(t))
	if err != nil {
		t.Fatalf("marshal fixture script: %v", err)
	}
	skipLit, err := json.Marshal(skips)
	if err != nil {
		t.Fatalf("marshal skips: %v", err)
	}
	driver := strings.ReplaceAll(strings.ReplaceAll(patchDriver, "__FIXTURE__", string(scriptLit)), "__SKIPS__", string(skipLit))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nodePath)
	cmd.Stdin = strings.NewReader(driver)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("node fixture harness: %v\nstderr:\n%s", err, stderr.String())
	}
	var logs []skipLog
	if err := json.Unmarshal(stdout.Bytes(), &logs); err != nil {
		t.Fatalf("parse fixture harness output: %v\nstdout: %s", err, stdout.String())
	}
	return logs
}

// observedColor returns the color #patch shows at time tMs: the CSS initial
// value until the first assignment, then the most recent assignment up to it.
func observedColor(assignments []fixtureAssignment, tMs float64) string {
	color := "#33cc66" // CSS background before any script assignment
	for _, a := range assignments {
		if a.T <= tMs {
			color = a.Color
		} else {
			break
		}
	}
	return color
}

func checkPatchLogSanity(t *testing.T, log skipLog) {
	t.Helper()
	if len(log.Assignments) == 0 {
		t.Fatalf("skip %d: fixture script made no #patch assignments (scene wiring or harness broken)", log.Skip)
	}
	distinct := map[string]bool{}
	for _, a := range log.Assignments {
		distinct[a.Color] = true
	}
	if len(distinct) < 2 {
		t.Fatalf("skip %d: fixture script assigned only one color (%q); the patch scene must change", log.Skip, log.Assignments[0].Color)
	}
}

// assertWindowShowsChange requires at least one observed color change within
// every two-second window starting on the phase grid, for the given capture rate.
func assertWindowShowsChange(t *testing.T, log skipLog, name string, hz float64) {
	t.Helper()
	periodMs := 1000.0 / hz
	for phase := 0.0; phase < patchWindowMs; phase += patchPhaseStepMs {
		var seq []string
		for at := phase; at < phase+patchWindowMs; at += periodMs {
			seq = append(seq, observedColor(log.Assignments, at))
		}
		changed := false
		for i := 1; i < len(seq); i++ {
			if seq[i] != seq[i-1] {
				changed = true
				break
			}
		}
		if !changed {
			t.Fatalf("skip %d, %s, phase %.0f ms: no observed color change in a two-second window (seq %v)", log.Skip, name, phase, seq)
		}
	}
}

// Representative slower sampling must still observe changing pixels. The sweep
// covers the CI-observed 15 Hz plus rates from 2 Hz to the 30 Hz animation
// rate; phases step by 1 ms over one full period (equal to the window).
func TestPatchFixtureChangesUnderSlowSampling(t *testing.T) {
	for _, log := range runPatchFixtureHarness(t, []int{1}) {
		checkPatchLogSanity(t, log)
		for _, r := range patchRates {
			assertWindowShowsChange(t, log, r.name, r.hz)
		}
	}
}

// Dropped animation frames must not erase the change either: only every 2nd or
// 3rd nominal tick fires, and between updates the page keeps showing the color
// assigned at its last fired tick.
func TestPatchFixtureChangesUnderSkippedAnimationFrames(t *testing.T) {
	for _, log := range runPatchFixtureHarness(t, []int{2, 3}) {
		checkPatchLogSanity(t, log)
		for _, r := range patchRates {
			assertWindowShowsChange(t, log, r.name, r.hz)
		}
	}
}
