package main

import "testing"

// The patch scene's visual schedule is defined in fixture.html and embedded
// into every run; its hash is recorded as fixture_sha256 in result.json, and
// the comparison command refuses to pair runs with different fixtures. These
// constants must stay in sync with that file: changing the fixture requires
// matching fixture versions on both sides of any future end-to-end comparison.

const (
	// Time unit: 1/30000 s, chosen so every schedule below is exact.
	oldFrameUnits = 1000  // old fixture: one logical frame at 30 Hz (1/30 s)
	newStateUnits = 7500  // current fixture: one color state held for 250 ms
	windowUnits   = 60000 // CI smoke measurement window: 2 s, equal to the new period
)

// oldPatchState mirrors the retired schedule: frame%2 with
// frame=floor(elapsed*30), a two-state toggle whose color changes every 1/15 s.
func oldPatchState(t int) int { return (t / oldFrameUnits) % 2 }

// newPatchState mirrors the current schedule: floor(elapsed/250ms)%8. All eight
// colors are distinct, so comparing state indices is equivalent to comparing
// rendered colors.
func newPatchState(t int) int { return (t / newStateUnits) % 8 }

// displayedNew models skipped animation frames: the page recomputes its color
// only when requestAnimationFrame fires. With a tick every skip-th nominal
// 30 Hz tick, the displayed state at t is the state computed at the last fired
// tick at or before t.
func displayedNew(t, skip int) int {
	tick := skip * oldFrameUnits
	return newPatchState((t / tick) * tick)
}

func consecutiveChanges(seq []int) int {
	changes := 0
	for i := 1; i < len(seq); i++ {
		if seq[i] != seq[i-1] {
			changes++
		}
	}
	return changes
}

// The CI artifact: "30 captures, 30 reuses, zero writes in two seconds." The
// capture loop settled at ~15 Hz on the loaded runner — exactly one old color
// period (2/30 s) per capture. At that rhythm every screenshot lands in the
// same phase of the toggle for any alignment: floor(u+2i)=floor(u)+2i, so all
// captures are byte-identical, every one is a reuse, and main.go's guard
// (scene != "idle" && Counts["write"] == 0) fails the run. This reproduces
// that failure deterministically without CI load.
func TestOldPatchScheduleAliasingReproducesZeroWrites(t *testing.T) {
	const captures = 30         // two seconds at the observed 15 Hz capture rate
	period := 2 * oldFrameUnits // one color period of the old toggle
	for phase := 0; phase < windowUnits; phase += oldFrameUnits / 4 {
		var seq []int
		for i := 0; i < captures; i++ {
			seq = append(seq, oldPatchState(phase+i*period))
		}
		if changes := consecutiveChanges(seq); changes != 0 {
			t.Fatalf("phase %d: aliased sampling observed %d change(s), want 0 (seq %v)", phase, changes, seq)
		}
	}
}

// Representative slower sampling must still observe changing pixels. The sweep
// covers the CI-observed 15 Hz plus rates from 2 Hz to the 24 FPS cap and the
// old animation rate; phases step by 1 ms over one full period, resolving every
// state boundary (shortest state: 250 ms) two orders of magnitude finer.
func TestNewPatchScheduleSurvivesSlowSampling(t *testing.T) {
	rates := []struct {
		name   string
		period int // capture period in units of 1/30000 s
	}{
		{"2 Hz", 15000}, {"4 Hz", 7500}, {"6 Hz", 5000}, {"7.5 Hz", 4000},
		{"8 Hz", 3750}, {"10 Hz", 3000}, {"12 Hz", 2500},
		{"15 Hz (CI observed)", 2000},
		{"20 Hz", 1500}, {"24 FPS cap", 1250}, {"30 Hz", 1000},
	}
	for _, r := range rates {
		for phase := 0; phase < windowUnits; phase += 30 { // 1 ms
			var seq []int
			for i := 0; ; i++ {
				at := phase + i*r.period
				if at >= phase+windowUnits {
					break
				}
				seq = append(seq, newPatchState(at))
			}
			if changes := consecutiveChanges(seq); changes == 0 {
				t.Fatalf("%s, phase %d: no observed change in a two-second window (seq %v)", r.name, phase, seq)
			}
		}
	}
}

// Dropped animation frames must not erase the change either. Effective RAF
// rates of 30/15/10 Hz are combined with the slowest representative capture
// rates; between updates the page keeps showing the state computed at its last
// fired tick, which is exactly what displayedNew renders.
func TestNewPatchScheduleSurvivesSkippedAnimationFrames(t *testing.T) {
	periods := []struct {
		name   string
		period int
	}{
		{"7.5 Hz", 4000}, {"10 Hz", 3000}, {"15 Hz (CI observed)", 2000}, {"24 FPS cap", 1250},
	}
	for skip := 1; skip <= 3; skip++ {
		for _, r := range periods {
			for phase := 0; phase < windowUnits; phase += 60 { // 2 ms
				var seq []int
				for i := 0; ; i++ {
					at := phase + i*r.period
					if at >= phase+windowUnits {
						break
					}
					seq = append(seq, displayedNew(at, skip))
				}
				if changes := consecutiveChanges(seq); changes == 0 {
					t.Fatalf("skip %d, %s, phase %d: no observed change in a two-second window (seq %v)", skip, r.name, phase, seq)
				}
			}
		}
	}
}
