package main

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	pb "termium/client/pb"
)

type cancelledInputServer struct {
	pb.UnimplementedBrowserControlServer
	reason        string
	code          codes.Code
	refreshError  bool
	writes, reads atomic.Int32
	entered       chan struct{}
	release       chan struct{}
}

func (s *cancelledInputServer) reject(ctx context.Context) error {
	s.writes.Add(1)
	if s.entered != nil {
		select {
		case s.entered <- struct{}{}:
		default:
		}
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.reason != "" {
		grpc.SetTrailer(ctx, metadata.Pairs("termium-reason", s.reason))
	}
	return status.Error(s.code, "test rejection")
}

func TestStaleQueueNeedsOnlyOneRecoveryRead(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	impl := &cancelledInputServer{reason: "stale-target", code: codes.FailedPrecondition, entered: make(chan struct{}, 1), release: make(chan struct{})}
	pb.RegisterBrowserControlServer(server, impl)
	go server.Serve(listener)
	defer server.Stop()
	defer listener.Close()
	conn, err := grpc.NewClient("passthrough:///fixture", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan operationResult, 40)
	d := newInputDispatcher(ctx, pb.NewBrowserControlClient(conn), func(v any) { results <- v.(operationResult) })
	old := browserOperation{input: &pb.InputEvent{Kind: pb.InputKind_KEY_INPUT, Key: "x", Generation: 8, TabId: "old"}}
	d.enqueue(old)
	select {
	case <-impl.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first input never sent")
	}
	for i := 0; i < 30; i++ {
		if !d.enqueue(old) {
			t.Fatal("queue rejected")
		}
	}
	close(impl.release)
	for i := 0; i < 31; i++ {
		select {
		case result := <-results:
			if !result.stale || result.err != nil || result.state.GetGeneration() != 9 {
				t.Fatal("lost cancellation notification", result)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("stale queue stalled")
		}
	}
	if impl.writes.Load() != 1 || impl.reads.Load() != 1 {
		t.Fatalf("repeated stale RPCs: writes=%d reads=%d", impl.writes.Load(), impl.reads.Load())
	}
	// Fresh input and the protocol's generation-zero wildcard must still reach
	// the server; local cancellation is only justified by an older generation.
	for _, generation := range []uint64{9, 0} {
		d.enqueue(browserOperation{input: &pb.InputEvent{Kind: pb.InputKind_KEY_INPUT, Generation: generation, TabId: "replacement"}})
		select {
		case <-results:
		case <-time.After(3 * time.Second):
			t.Fatal("fresh input stalled")
		}
	}
	if impl.writes.Load() != 3 {
		t.Fatal("fresh input was discarded locally")
	}
}

func TestLateNavigationResultPreservesNewerSubmission(t *testing.T) {
	kh, ops := recorder()
	kh.editor.set("https://one.example/", false)
	kh.navigate(pb.NavigationAction_NAVIGATE, kh.editor.value())
	first := (*ops)[0]
	kh.editor.set("https://two.example/", false)
	kh.navigate(pb.NavigationAction_NAVIGATE, kh.editor.value())
	second := (*ops)[1]
	kh.result(operationResult{operation: first, stale: true, state: &pb.BrowserState{Generation: 9}})
	if !kh.awaitingNavigation || kh.focus != "page" || kh.editor.value() != "https://two.example/" {
		t.Fatal("older cancellation overwrote newer navigation")
	}
	kh.result(operationResult{operation: second, stale: true, state: &pb.BrowserState{Generation: 9}})
	if kh.awaitingNavigation || kh.focus != "address" || kh.editor.value() != "https://two.example/" {
		t.Fatal("latest cancelled address was not restored")
	}
}

func TestOldFrameStateCannotInterruptPendingNavigation(t *testing.T) {
	kh, _ := recorder()
	kh.pendingAddress = "https://old.example/"
	kh.editor.set("https://new.example/", false)
	kh.navigate(pb.NavigationAction_NAVIGATE, kh.editor.value())
	// A capture begun before the command may deliver its old error afterward.
	kh.applyState(&pb.BrowserState{Generation: 9, Error: "old navigation failed"})
	if kh.focus != "page" || kh.editor.value() != "https://new.example/" || !kh.state.Loading || kh.state.Error != "" {
		t.Fatal("old snapshot interrupted the pending navigation")
	}
}

func TestSnapshotBegunBeforeNavigationReplyCannotRestoreOldError(t *testing.T) {
	kh, ops := recorder()
	kh.state.Generation = 9
	kh.editor.set("https://new.example/", false)
	kh.navigate(pb.NavigationAction_NAVIGATE, kh.editor.value())
	started := time.Now()
	kh.result(operationResult{operation: (*ops)[0], state: &pb.BrowserState{Generation: 9, Loading: true, Url: "https://new.example/"}})
	kh.applySnapshot(&pb.BrowserState{Generation: 9, Error: "old failure", Url: "https://old.example/"}, started)
	if kh.focus != "page" || !kh.state.Loading || kh.state.Error != "" || kh.state.Url != "https://new.example/" {
		t.Fatal("late snapshot replaced newer command state")
	}
	kh.applySnapshot(&pb.BrowserState{Generation: 9, Url: "https://new.example/"}, time.Now())
	if kh.state.Loading {
		t.Fatal("fresh state was also rejected")
	}
}
func (s *cancelledInputServer) SendInput(ctx context.Context, _ *pb.InputEvent) (*pb.Message, error) {
	return nil, s.reject(ctx)
}
func (s *cancelledInputServer) BrowserCommand(ctx context.Context, _ *pb.NavigationRequest) (*pb.BrowserState, error) {
	return nil, s.reject(ctx)
}
func (s *cancelledInputServer) GetBrowserState(context.Context, *pb.Empty) (*pb.BrowserState, error) {
	s.reads.Add(1)
	if s.refreshError {
		return nil, status.Error(codes.Unavailable, "refresh failed")
	}
	return &pb.BrowserState{Generation: 9, ActiveTabId: "replacement"}, nil
}

// Exercise actual gRPC trailers, not an error-message match or a mocked call option.
func TestCancelledInputRefreshesWithoutReplay(t *testing.T) {
	for _, tc := range []struct {
		name, reason                    string
		code                            codes.Code
		navigation, refreshError, stale bool
	}{
		{"key", "stale-target", codes.FailedPrecondition, false, false, true},
		{"command", "stale-target", codes.FailedPrecondition, true, false, true},
		{"loading precondition", "", codes.FailedPrecondition, true, false, false},
		{"other reason", "something-else", codes.FailedPrecondition, false, false, false},
		{"real failure", "stale-target", codes.Internal, false, false, false},
		{"refresh failure", "stale-target", codes.FailedPrecondition, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener := bufconn.Listen(1024 * 1024)
			server := grpc.NewServer()
			impl := &cancelledInputServer{reason: tc.reason, code: tc.code, refreshError: tc.refreshError}
			pb.RegisterBrowserControlServer(server, impl)
			go server.Serve(listener)
			defer server.Stop()
			defer listener.Close()
			conn, err := grpc.NewClient("passthrough:///fixture", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			results := make(chan operationResult, 2)
			d := newInputDispatcher(ctx, pb.NewBrowserControlClient(conn), func(v any) { results <- v.(operationResult) })
			op := browserOperation{input: &pb.InputEvent{Kind: pb.InputKind_KEY_INPUT, Key: "x", Generation: 8, TabId: "old"}}
			if tc.navigation {
				op = browserOperation{navigation: &pb.NavigationRequest{Action: pb.NavigationAction_CLOSE_TAB, Generation: 8, TabId: "old"}}
			}
			if !d.enqueue(op) {
				t.Fatal("queue rejected")
			}
			select {
			case result := <-results:
				if result.stale != tc.stale {
					t.Fatalf("stale=%t", result.stale)
				}
				if tc.stale && !tc.refreshError {
					if result.err != nil || result.state.GetActiveTabId() != "replacement" || result.state.GetGeneration() != 9 {
						t.Fatalf("state not refreshed: %+v", result)
					}
				} else if result.err == nil {
					t.Fatal("real error hidden")
				}
				if tc.refreshError && status.Code(result.err) != codes.Unavailable {
					t.Fatal("refresh failure hidden", result.err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("dispatcher stalled")
			}
			wantReads := int32(0)
			if tc.stale {
				wantReads = 1
			}
			if impl.writes.Load() != 1 || impl.reads.Load() != wantReads {
				t.Fatalf("unexpected replay/read: writes=%d reads=%d", impl.writes.Load(), impl.reads.Load())
			}
		})
	}
}

func TestCancelledInputStatusRecovers(t *testing.T) {
	for _, kind := range []pb.InputKind{pb.InputKind_POINTER_INPUT, pb.InputKind_RESET_INPUT, pb.InputKind_KEY_INPUT, pb.InputKind_PASTE_INPUT} {
		t.Run(kind.String(), func(t *testing.T) {
			kh := NewKeyboardHandler(nil)
			original := kh.status
			kh.state = pb.BrowserState{Generation: 8, ActiveTabId: "old"}
			kh.pointerHeld, kh.capturePage = 1, true
			op := browserOperation{input: &pb.InputEvent{Kind: kind, Generation: 8, TabId: "old"}}
			state := &pb.BrowserState{Generation: 9, ActiveTabId: "replacement"}
			kh.result(operationResult{operation: op, stale: true, state: state})
			if kh.state.ActiveTabId != "replacement" || kh.pointerHeld != 0 || kh.capturePage {
				t.Fatal("stale input left old tab/drag state")
			}
			passive := kind == pb.InputKind_POINTER_INPUT || kind == pb.InputKind_RESET_INPUT
			if passive && kh.status != original {
				t.Fatal("passive cancellation displayed an error", kh.status)
			}
			if !passive && (!kh.staleNotice || kh.status != "Page changed; repeat the last action") {
				t.Fatal("lost deliberate input needs feedback", kh.status)
			}
			kh.result(operationResult{operation: browserOperation{input: &pb.InputEvent{Kind: pb.InputKind_KEY_INPUT}}, state: state})
			if kh.staleNotice || kh.status == "Page changed; repeat the last action" {
				t.Fatal("notice stuck after successful input")
			}
		})
	}
	kh := NewKeyboardHandler(nil)
	kh.awaitingNavigation = true
	kh.result(operationResult{operation: browserOperation{navigation: &pb.NavigationRequest{Action: pb.NavigationAction_NAVIGATE, Url: "https://example.com/"}}, stale: true, state: &pb.BrowserState{Generation: 9}})
	if kh.awaitingNavigation || kh.focus != "address" || kh.editor.value() != "https://example.com/" {
		t.Fatal("cancelled address was lost or navigation stayed blocked")
	}
}
