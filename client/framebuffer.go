package main

import (
	"image"
	"sync"
	pb "termium/client/pb"
	"time"
)

type Frame struct {
	storage       *frameArena // owned lease; pixels and graphics remain immutable until release
	Data          []byte
	Generation    uint64
	State         *pb.BrowserState
	Width, Height int
	Timestamp     time.Time
	CapturedAt    time.Time
	Image         *image.RGBA
	Graphics      []byte // complete renderer payload; cursor placement is added by the UI
}

// retain returns another lease on the same immutable frame. Heap-only frames
// need no bookkeeping. Callers must already own a lease when retaining one.
func (f *Frame) retain() *Frame {
	if f != nil && f.storage != nil {
		f.storage.retain()
	}
	return f
}
func (f *Frame) release() {
	if f != nil && f.storage != nil {
		f.storage.release()
	}
}

// Published frames are immutable. Replacing the pending slot drops obsolete
// work without allowing the producer to overwrite a frame being displayed.
type FrameBuffer struct {
	mu                           sync.Mutex
	pending                      *Frame
	received, displayed, dropped uint64
}

func NewFrameBuffer() *FrameBuffer { return &FrameBuffer{} }

// Publish transfers the caller's lease into the pending slot.
func (fb *FrameBuffer) Publish(frame *Frame) bool {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	wake := fb.pending == nil
	if fb.pending != nil {
		fb.dropped++
		fb.pending.release()
		performance.record("prepared_superseded", -1, 0)
	}
	fb.pending = frame
	fb.received++
	return wake
}

// GetDisplayFrame transfers the pending lease to the UI owner.
func (fb *FrameBuffer) GetDisplayFrame() *Frame {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	result := fb.pending
	fb.pending = nil
	if result != nil {
		fb.displayed++
	}
	return result
}
func (fb *FrameBuffer) GetStats() (uint64, uint64, uint64) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	return fb.received, fb.displayed, fb.dropped
}

// Discard releases pending work without counting it as displayed.
func (fb *FrameBuffer) Discard() {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	fb.pending.release()
	fb.pending = nil
}
