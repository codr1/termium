package main

import (
	"sync"
	"time"
)

type Frame struct {
	Data          []byte
	Generation    uint64
	Width, Height int
	Timestamp     time.Time
}

// Published frames are immutable. Replacing the pending slot drops obsolete
// work without allowing the producer to overwrite a frame being displayed.
type FrameBuffer struct {
	mu                           sync.Mutex
	pending                      *Frame
	received, displayed, dropped uint64
}

func NewFrameBuffer() *FrameBuffer { return &FrameBuffer{} }
func (fb *FrameBuffer) Publish(frame *Frame) {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	if fb.pending != nil {
		fb.dropped++
	}
	fb.pending = frame
	fb.received++
}
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
