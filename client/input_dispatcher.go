package main

import (
	"context"
	"fmt"
	"sync"
	pb "termium/client/pb"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type browserOperation struct {
	selection  *pb.NavigationRequest
	input      *pb.InputEvent
	navigation *pb.NavigationRequest
	viewport   *pb.ViewportSize
}
type operationResult struct {
	clipboard *string
	operation browserOperation
	state     *pb.BrowserState
	err       error
	stale     bool
}
type stateUpdate struct {
	state   *pb.BrowserState
	err     error
	started time.Time
}

type inputDispatcher struct {
	mu      sync.Mutex
	pending []browserOperation
	wake    chan struct{}
	ctx     context.Context
	client  pb.BrowserControlClient
	notify  func(any)
}

func newInputDispatcher(ctx context.Context, client pb.BrowserControlClient, notify func(any)) *inputDispatcher {
	d := &inputDispatcher{ctx: ctx, client: client, notify: notify, wake: make(chan struct{}, 1)}
	shutdownWg.Add(1)
	go func() { defer shutdownWg.Done(); d.run() }()
	return d
}
func (o browserOperation) motion() bool {
	return o.input != nil && o.input.Kind == pb.InputKind_POINTER_INPUT && o.input.ClickCount == 0
}
func (d *inputDispatcher) enqueue(o browserOperation) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx.Err() != nil {
		return false
	}
	// Coalesce hover/drag positions only. Never reorder button transitions,
	// keyboard input, paste, or navigation across a pointer movement.
	if n := len(d.pending); n > 0 && o.motion() && d.pending[n-1].motion() && o.input.Buttons == d.pending[n-1].input.Buttons {
		d.pending[n-1] = o
	} else {
		if len(d.pending) >= 512 {
			// A reset or release must still be admitted to prevent stuck drags.
			release := o.input != nil && (o.input.Kind == pb.InputKind_RESET_INPUT || o.input.Kind == pb.InputKind_POINTER_INPUT && o.input.Buttons == 0 && o.input.ClickCount > 0)
			if !release {
				if debugEnabled {
					Debug("input rejected: queue full", DEBUG)
				}
				return false
			}
		}
		d.pending = append(d.pending, o)
		if debugEnabled && o.input != nil {
			Debug(fmt.Sprintf("input queued kind=%s key=%s text_bytes=%d generation=%d tab=%s depth=%d", o.input.Kind, o.input.Key, len(o.input.Text), o.input.Generation, o.input.TabId, len(d.pending)), DEBUG)
		}
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
	return true
}
func (d *inputDispatcher) run() {
	var known *pb.BrowserState
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.wake:
		}
		for {
			if d.ctx.Err() != nil {
				return
			}
			d.mu.Lock()
			if len(d.pending) == 0 {
				d.mu.Unlock()
				break
			}
			o := d.pending[0]
			d.pending[0] = browserOperation{}
			d.pending = d.pending[1:]
			d.mu.Unlock()
			var generation uint64
			if o.input != nil {
				generation = o.input.Generation
			}
			if o.selection != nil {
				generation = o.selection.Generation
			}
			if o.navigation != nil {
				generation = o.navigation.Generation
			}
			if known != nil && generation != 0 && generation < known.Generation {
				// The read which recovered the first cancellation already proves
				// these queued actions are stale. Report them without N more RPCs.
				if debugEnabled {
					Debug(fmt.Sprintf("input cancelled: queued generation=%d current=%d", generation, known.Generation), DEBUG)
				}
				d.notify(operationResult{operation: o, state: known, stale: true})
				continue
			}
			if debugEnabled && o.input != nil {
				Debug(fmt.Sprintf("input dispatch kind=%s generation=%d tab=%s", o.input.Kind, o.input.Generation, o.input.TabId), DEBUG)
			}
			var started time.Time
			if debugEnabled {
				started = time.Now()
			}
			ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
			result := operationResult{operation: o}
			var trailer metadata.MD
			switch {
			case o.selection != nil:
				var reply *pb.Message
				reply, result.err = d.client.GetSelection(ctx, o.selection, grpc.Trailer(&trailer))
				if reply != nil && result.err == nil {
					result.state = reply.State
					result.clipboard = &reply.Text
				}
			case o.input != nil:
				var reply *pb.Message
				reply, result.err = d.client.SendInput(ctx, o.input, grpc.Trailer(&trailer))
				if reply != nil {
					result.state = reply.State
				}
			case o.navigation != nil:
				result.state, result.err = d.client.BrowserCommand(ctx, o.navigation, grpc.Trailer(&trailer))
			case o.viewport != nil:
				_, result.err = d.client.SetViewport(ctx, o.viewport)
			}
			cancel()
			if status.Code(result.err) == codes.FailedPrecondition && len(trailer.Get("termium-reason")) == 1 && trailer.Get("termium-reason")[0] == "stale-target" {
				result.stale = true
				// Refresh only the read. Replaying a cancelled key/click could
				// act on a different document or close the replacement tab.
				refreshCtx, refreshCancel := context.WithTimeout(d.ctx, 2*time.Second)
				result.state, result.err = d.client.GetBrowserState(refreshCtx, &pb.Empty{})
				refreshCancel()
			}
			if d.ctx.Err() != nil {
				return
			}
			if result.state != nil && (known == nil || result.state.Generation >= known.Generation) {
				known = result.state
			}
			if debugEnabled && o.input != nil {
				Debug(fmt.Sprintf("input result kind=%s generation=%d tab=%s elapsed=%s stale=%t error=%v", o.input.Kind, o.input.Generation, o.input.TabId, time.Since(started), result.stale, result.err), DEBUG)
			}
			d.notify(result)
		}
	}
}
