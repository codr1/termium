package main

import (
	"errors"
	"sync"
	"sync/atomic"
)

const frameArenaCount = 4
const initialFrameArenaBytes = 10 * 1024 * 1024

// Per-frame logical allocation cap: decoded pixels plus the encoded terminal payload.
const frameArenaLimit = maxFramePixels*4 + maxFrameBytes

var errFrameArenaBusy = errors.New("frame arena pool exhausted")
var errFrameArenaLimit = errors.New("frame arena allocation exceeds per-frame limit")

// frameArena is a growable byte buffer reused across frames. buf and used are
// touched only by the exclusive preparation worker that owns the arena; refs
// may be retained and released concurrently by display owners.
type frameArena struct {
	buf  []byte
	used int
	refs atomic.Int32
}

func (a *frameArena) retain() {
	for {
		old := a.refs.Load()
		if old == 0 {
			panic("frameArena.retain: use after release")
		}
		if a.refs.CompareAndSwap(old, old+1) {
			return
		}
	}
}

func (a *frameArena) release() {
	for {
		old := a.refs.Load()
		if old == 0 {
			panic("frameArena.release: refcount underflow")
		}
		if a.refs.CompareAndSwap(old, old-1) {
			return
		}
	}
}

// alloc carves n bytes out of the arena's logical cursor. Returned slices have
// bounded capacity so append cannot write into the next allocation. Growth
// copies the written prefix; earlier returned slices keep their data in the
// old backing block until consumers release it.
func (a *frameArena) alloc(n int) ([]byte, error) {
	if a.refs.Load() == 0 {
		panic("frameArena.alloc: use after release")
	}
	if n < 0 || n > frameArenaLimit-a.used {
		return nil, errFrameArenaLimit
	}
	need := a.used + n
	if need > cap(a.buf) {
		newCap := cap(a.buf)
		if newCap == 0 {
			newCap = initialFrameArenaBytes
		}
		for newCap < need { // bounded by frameArenaLimit, so doubling cannot overflow
			newCap *= 2
		}
		grown := make([]byte, newCap)
		copy(grown, a.buf[:a.used])
		a.buf = grown
	}
	out := a.buf[a.used : a.used+n : a.used+n]
	a.used += n
	return out, nil
}

// frameArenaPool is a fixed set of arenas rotated round-robin. The zero value
// is usable; slots are allocated lazily on first acquire and the pool never
// grows past frameArenaCount or waits for a free slot.
type frameArenaPool struct {
	mu    sync.Mutex
	next  int
	slots [frameArenaCount]*frameArena
}

func (p *frameArenaPool) acquire() *frameArena {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := 0; i < frameArenaCount; i++ {
		idx := (p.next + i) % frameArenaCount
		a := p.slots[idx]
		if a == nil {
			a = &frameArena{}
			p.slots[idx] = a
		}
		if !a.refs.CompareAndSwap(0, 1) {
			continue
		}
		a.used = 0
		if len(a.buf) < initialFrameArenaBytes {
			a.buf = make([]byte, initialFrameArenaBytes)
		}
		p.next = (idx + 1) % frameArenaCount
		return a
	}
	return nil
}
