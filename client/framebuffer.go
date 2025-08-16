package main

import (
	"sync/atomic"
	"time"
)

// Frame represents a single screenshot frame
type Frame struct {
	Data      []byte
	Width     int
	Height    int
	Timestamp time.Time
}

// FrameBuffer implements triple buffering for smooth frame updates
type FrameBuffer struct {
	frames     [3]*Frame
	writing    int32 // Index being written by receiver (atomic)
	ready      int32 // Index ready for display (atomic)
	displaying int32 // Index currently being displayed (atomic)
	hasNewFrame int32 // 1 if there's an unread frame ready (atomic)
	newFrame   chan struct{} // Signal when new frame available
	
	// Statistics
	framesReceived uint64 // Total frames received from server
	framesDisplayed uint64 // Total frames actually displayed
	framesDropped  uint64 // Frames that went from ready to writing without displaying
}

// NewFrameBuffer creates a new triple-buffered frame buffer
func NewFrameBuffer() *FrameBuffer {
	return &FrameBuffer{
		frames: [3]*Frame{
			&Frame{},
			&Frame{},
			&Frame{},
		},
		writing:    0,
		ready:      1,
		displaying: 2,
		newFrame:   make(chan struct{}, 1),
	}
}

// GetWriteFrame returns the frame that can be written to
func (fb *FrameBuffer) GetWriteFrame() *Frame {
	idx := atomic.LoadInt32(&fb.writing)
	return fb.frames[idx]
}

// SwapWriteFrame promotes the write frame to ready and gets a new write frame
func (fb *FrameBuffer) SwapWriteFrame() {
	// Check if there's already a frame waiting that hasn't been displayed
	if atomic.CompareAndSwapInt32(&fb.hasNewFrame, 1, 1) {
		// There was already a frame waiting, so we're dropping it
		atomic.AddUint64(&fb.framesDropped, 1)
	}
	
	// Atomic swap: writing becomes ready, old ready becomes writing
	oldWriting := atomic.LoadInt32(&fb.writing)
	oldReady := atomic.LoadInt32(&fb.ready)
	
	atomic.StoreInt32(&fb.ready, oldWriting)
	atomic.StoreInt32(&fb.writing, oldReady)
	atomic.AddUint64(&fb.framesReceived, 1)
	
	// Mark that we have a new frame
	atomic.StoreInt32(&fb.hasNewFrame, 1)
	
	// Signal that a new frame is ready
	select {
	case fb.newFrame <- struct{}{}:
	default:
		// Channel full, display thread hasn't consumed yet
	}
}

// GetDisplayFrame returns the latest ready frame for display
// Returns nil if no new frame is available
func (fb *FrameBuffer) GetDisplayFrame() *Frame {
	select {
	case <-fb.newFrame:
		// New frame available, swap ready and displaying
		oldReady := atomic.LoadInt32(&fb.ready)
		oldDisplaying := atomic.LoadInt32(&fb.displaying)
		
		atomic.StoreInt32(&fb.displaying, oldReady)
		atomic.StoreInt32(&fb.ready, oldDisplaying)
		atomic.AddUint64(&fb.framesDisplayed, 1)
		
		// Mark that we've consumed the frame
		atomic.StoreInt32(&fb.hasNewFrame, 0)
		
		return fb.frames[oldReady]
	default:
		// No new frame
		return nil
	}
}

// WaitForFrame blocks until a new frame is available or timeout
func (fb *FrameBuffer) WaitForFrame(timeout time.Duration) *Frame {
	select {
	case <-fb.newFrame:
		// New frame available, swap ready and displaying
		oldReady := atomic.LoadInt32(&fb.ready)
		oldDisplaying := atomic.LoadInt32(&fb.displaying)
		
		atomic.StoreInt32(&fb.displaying, oldReady)
		atomic.StoreInt32(&fb.ready, oldDisplaying)
		atomic.AddUint64(&fb.framesDisplayed, 1)
		
		// Mark that we've consumed the frame
		atomic.StoreInt32(&fb.hasNewFrame, 0)
		
		return fb.frames[oldReady]
	case <-time.After(timeout):
		return nil
	}
}

// GetStats returns frame buffer statistics
func (fb *FrameBuffer) GetStats() (received, displayed, dropped uint64) {
	return atomic.LoadUint64(&fb.framesReceived),
		atomic.LoadUint64(&fb.framesDisplayed),
		atomic.LoadUint64(&fb.framesDropped)
}