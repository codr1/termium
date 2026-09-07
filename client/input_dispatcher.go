package main

import (
	"context"
	"sync"
	pb "termium/client/pb"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type browserOperation struct {
	input      *pb.InputEvent
	navigation *pb.NavigationRequest
	viewport   *pb.ViewportSize
}
type operationResult struct {
	operation browserOperation
	state     *pb.BrowserState
	err       error
	stale     bool
}
type stateUpdate struct {
	state *pb.BrowserState
	err   error
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
				return false
			}
		}
		d.pending = append(d.pending, o)
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
	return true
}
func (d *inputDispatcher) run() {
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.wake:
		}
		for {
			d.mu.Lock()
			if len(d.pending) == 0 {
				d.mu.Unlock()
				break
			}
			o := d.pending[0]
			d.pending[0] = browserOperation{}
			d.pending = d.pending[1:]
			d.mu.Unlock()
			ctx, cancel := context.WithTimeout(d.ctx, 5*time.Second)
			result := operationResult{operation: o}
			var trailer metadata.MD
			switch {
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
			d.notify(result)
		}
	}
}
