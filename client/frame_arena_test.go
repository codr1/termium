package main

import (
	"bytes"
	"sync"
	"testing"
)

func mustAcquire(t *testing.T, p *frameArenaPool) *frameArena {
	t.Helper()
	a := p.acquire()
	if a == nil {
		t.Fatal("acquire: unexpected exhaustion")
	}
	return a
}

// ownedArena builds a directly constructed arena with one owner reference so
// growth and bound tests can use small buffers instead of 10 MiB slots.
func ownedArena(size int) *frameArena {
	a := &frameArena{buf: make([]byte, size)}
	a.refs.Store(1)
	return a
}

func TestFrameArenaPoolExhaustion(t *testing.T) {
	var p frameArenaPool
	acquired := make([]*frameArena, 0, frameArenaCount)
	for i := 0; i < frameArenaCount; i++ {
		a := mustAcquire(t, &p)
		for _, prev := range acquired {
			if a == prev {
				t.Fatal("acquire returned a slot still in use")
			}
		}
		acquired = append(acquired, a)
	}
	if a := p.acquire(); a != nil {
		t.Fatalf("fifth acquire: want nil, got %p", a)
	}

	released := acquired[1]
	released.release()
	a := mustAcquire(t, &p)
	if a != released {
		t.Fatalf("acquire after release: want recycled %p, got %p", released, a)
	}
}

func TestFrameArenaRetainedUntilAllRefsReleased(t *testing.T) {
	var p frameArenaPool
	owned := make([]*frameArena, frameArenaCount)
	for i := range owned {
		owned[i] = mustAcquire(t, &p)
	}
	a := owned[0]
	a.retain()  // display holds a reference
	a.release() // owner done; arena still busy

	if got := p.acquire(); got != nil {
		t.Fatalf("acquire with retained arena: want nil, got %p", got)
	}
	a.release() // last ref drops

	got := mustAcquire(t, &p)
	if got != a {
		t.Fatalf("acquire after final release: want %p, got %p", a, got)
	}
}

func TestFrameArenaResetWithoutClearing(t *testing.T) {
	var p frameArenaPool
	a := mustAcquire(t, &p)
	// Pin the remaining slots so rotation wraps back to a's slot.
	pinned := make([]*frameArena, 0, frameArenaCount-1)
	for i := 1; i < frameArenaCount; i++ {
		pinned = append(pinned, mustAcquire(t, &p))
	}
	marker := []byte("termium-arena-bytes")
	buf, err := a.alloc(len(marker))
	if err != nil {
		t.Fatal(err)
	}
	copy(buf, marker)
	capacity := cap(a.buf)
	if capacity < initialFrameArenaBytes {
		t.Fatalf("initial capacity %d < %d", capacity, initialFrameArenaBytes)
	}
	a.release()

	b := mustAcquire(t, &p)
	if b != a {
		t.Fatalf("recycled arena: want %p, got %p", a, b)
	}
	if b.used != 0 {
		t.Fatalf("cursor not reset on reuse: used=%d", b.used)
	}
	if cap(b.buf) < capacity {
		t.Fatalf("capacity not retained on reuse: %d < %d", cap(b.buf), capacity)
	}
	if !bytes.Equal(b.buf[:len(marker)], marker) {
		t.Fatal("reuse cleared the buffer")
	}
	for _, q := range pinned {
		q.release()
	}
	b.release()
}

func TestFrameArenaGrowthPreservesSlices(t *testing.T) {
	a := ownedArena(8)

	first, err := a.alloc(4)
	if err != nil {
		t.Fatal(err)
	}
	copy(first, "ABCD")
	second, err := a.alloc(4)
	if err != nil {
		t.Fatal(err)
	}
	copy(second, "EFGH")

	next, err := a.alloc(2) // forces growth past the 8-byte buffer
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, []byte("ABCD")) || !bytes.Equal(second, []byte("EFGH")) {
		t.Fatalf("earlier slices corrupted by growth: %q %q", first, second)
	}
	if !bytes.Equal(a.buf[:8], []byte("ABCDEFGH")) {
		t.Fatalf("written prefix not preserved in grown buffer: %q", a.buf[:8])
	}
	copy(next, "IJ")
	if !bytes.Equal(a.buf[8:10], []byte("IJ")) {
		t.Fatal("new allocation not backed by the grown buffer")
	}
}

func TestFrameArenaAllocBoundedCapacity(t *testing.T) {
	a := ownedArena(16)

	first, err := a.alloc(4)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.alloc(4)
	if err != nil {
		t.Fatal(err)
	}
	copy(second, "SAFE")
	// Bounded capacity forces append onto a fresh block instead of the arena's
	// next allocation region.
	appended := append(first, 'X')
	if len(appended) != 5 || appended[4] != 'X' {
		t.Fatalf("append failed: %q", appended)
	}
	if !bytes.Equal(second, []byte("SAFE")) {
		t.Fatal("append overwrote the next allocation region")
	}
}

func TestFrameArenaAllocLimits(t *testing.T) {
	a := ownedArena(initialFrameArenaBytes)

	if _, err := a.alloc(-1); err != errFrameArenaLimit {
		t.Fatalf("alloc(-1): want errFrameArenaLimit, got %v", err)
	}
	if _, err := a.alloc(frameArenaLimit + 1); err != errFrameArenaLimit {
		t.Fatalf("alloc over limit: want errFrameArenaLimit, got %v", err)
	}
	// A huge n would overflow used+n if summed before the bound check.
	if _, err := a.alloc(1 << 62); err != errFrameArenaLimit {
		t.Fatalf("alloc(1<<62): want errFrameArenaLimit, got %v", err)
	}

	// Remaining-limit path: sit one byte short of the cap and reject only the
	// overflow, so no large allocation is triggered.
	a.used = frameArenaLimit - 1
	if _, err := a.alloc(2); err != errFrameArenaLimit {
		t.Fatalf("alloc past remaining limit: want errFrameArenaLimit, got %v", err)
	}
}

func TestFrameArenaRotateRecycle(t *testing.T) {
	var p frameArenaPool
	owned := make([]*frameArena, frameArenaCount)
	for i := range owned {
		owned[i] = mustAcquire(t, &p)
	}
	owned[0].buf[0] = 'A'
	owned[1].buf[0] = 'B'
	owned[2].buf[0] = 'C'
	owned[0].release()
	owned[1].release()

	first := mustAcquire(t, &p)
	if first != owned[0] {
		t.Fatalf("rotation: want slot 0 %p, got %p", owned[0], first)
	}
	second := mustAcquire(t, &p)
	if second != owned[1] {
		t.Fatalf("rotation: want slot 1 %p, got %p", owned[1], second)
	}
	first.buf[4] = 'X'
	if first.used != 0 || second.used != 0 {
		t.Fatal("cursor not reset on recycle")
	}
	if owned[1].buf[0] != 'B' || owned[2].buf[0] != 'C' {
		t.Fatal("recycling one arena disturbed another")
	}
}

func TestFrameArenaConcurrentRetainRelease(t *testing.T) {
	var p frameArenaPool
	a := mustAcquire(t, &p) // owner ref
	// Pin the remaining slots so rotation wraps back to a's slot.
	pinned := make([]*frameArena, 0, frameArenaCount-1)
	for i := 1; i < frameArenaCount; i++ {
		pinned = append(pinned, mustAcquire(t, &p))
	}

	const workers = 8
	const iterations = 50
	start := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < iterations; i++ {
				a.retain()
				a.release()
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := a.refs.Load(); got != 1 {
		t.Fatalf("refs after concurrent retain/release: want 1, got %d", got)
	}
	a.release() // owner done; arena free again
	got := mustAcquire(t, &p)
	if got != a {
		t.Fatalf("acquire after final release: want %p, got %p", a, got)
	}
}

func TestFrameArenaPoolConcurrentAcquire(t *testing.T) {
	var p frameArenaPool
	const workers = 8
	const iterations = 200
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				a := p.acquire()
				if a == nil {
					continue // pool exhausted: drop, as the pipeline does
				}
				if _, err := a.alloc(64); err != nil {
					t.Errorf("alloc: %v", err)
					return
				}
				a.release()
			}
		}()
	}
	wg.Wait()
}

func TestFrameArenaReleaseUnderflowPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("release on unreferenced arena did not panic")
		}
	}()
	(&frameArena{}).release()
}

func TestFrameArenaRetainAfterReleasePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("retain on unreferenced arena did not panic")
		}
	}()
	(&frameArena{}).retain()
}

func TestFrameArenaAllocAfterReleasePanics(t *testing.T) {
	var p frameArenaPool
	a := mustAcquire(t, &p)
	a.release() // last ref; arena free

	defer func() {
		if recover() == nil {
			t.Fatal("alloc after final release did not panic")
		}
	}()
	a.alloc(1)
}
