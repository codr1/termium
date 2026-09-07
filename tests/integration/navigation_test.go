//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	pb "termium/client/pb"
)

func waitState(t *testing.T, c pb.BrowserControlClient, match func(*pb.BrowserState) bool) *pb.BrowserState {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var last *pb.BrowserState
	for {
		state, err := c.GetBrowserState(ctx, &pb.Empty{})
		requireOK(t, err)
		last = state
		if match(state) {
			return state
		}
		select {
		case <-ctx.Done():
			t.Fatalf("browser state did not settle: %v", last)
		case <-tick.C:
		}
	}
}
func TestNavigationControls(t *testing.T) {
	c := startServer(t)
	url, _ := fixture(t)
	command := func(action pb.NavigationAction, url string) *pb.BrowserState {
		t.Helper()
		s, err := c.BrowserCommand(deadline(t), &pb.NavigationRequest{Action: action, Url: url})
		requireOK(t, err)
		return s
	}
	initial, err := c.GetBrowserState(deadline(t), &pb.Empty{})
	requireOK(t, err)
	if initial.CanBack || initial.CanForward {
		t.Fatal("new page has history", initial)
	}
	command(pb.NavigationAction_NAVIGATE, url+"/one")
	one := waitState(t, c, func(s *pb.BrowserState) bool { return s.Url == url+"/one" && !s.Loading })
	command(pb.NavigationAction_NAVIGATE, url+"/redirect")
	two := waitState(t, c, func(s *pb.BrowserState) bool { return s.Url == url+"/page?redirected=1" && !s.Loading })
	if !two.CanBack || two.CanForward || two.Generation <= one.Generation {
		t.Fatal("history or generation incorrect", two)
	}
	command(pb.NavigationAction_BACK, "")
	back := waitState(t, c, func(s *pb.BrowserState) bool { return s.Url == url+"/one" && !s.Loading })
	if !back.CanForward {
		t.Fatal("forward unavailable", back)
	}
	command(pb.NavigationAction_FORWARD, "")
	waitState(t, c, func(s *pb.BrowserState) bool { return s.Url == two.Url && !s.Loading })
	before, err := c.GetBrowserState(deadline(t), &pb.Empty{})
	requireOK(t, err)
	command(pb.NavigationAction_RELOAD, "")
	waitState(t, c, func(s *pb.BrowserState) bool { return s.Generation > before.Generation && !s.Loading })
	var trailer metadata.MD
	_, err = c.SendInput(deadline(t), &pb.InputEvent{Kind: pb.InputKind_TEXT_INPUT, Text: "stale", Generation: one.Generation}, grpc.Trailer(&trailer))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("old-document input accepted: %v", err)
	}
	if reason := trailer.Get("termium-reason"); len(reason) != 1 || reason[0] != "stale-target" {
		t.Fatalf("missing machine-readable cancellation reason: %v", trailer)
	}
	_, err = c.BrowserCommand(deadline(t), &pb.NavigationRequest{Action: pb.NavigationAction_CLOSE_TAB, Generation: one.Generation}, grpc.Trailer(&trailer))
	if status.Code(err) != codes.FailedPrecondition || len(trailer.Get("termium-reason")) != 1 || trailer.Get("termium-reason")[0] != "stale-target" {
		t.Fatalf("stale command was not classified: %v %v", err, trailer)
	}
	if state, err := c.GetBrowserState(deadline(t), &pb.Empty{}); err != nil || state.ActiveTabId != before.ActiveTabId {
		t.Fatalf("stale close changed the active tab: %v %v", state, err)
	}
	started := make(chan struct{}, 1)
	hanging := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer hanging.Close()
	command(pb.NavigationAction_NAVIGATE, hanging.URL)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("navigation never started")
	}
	waitState(t, c, func(s *pb.BrowserState) bool { return s.Loading })
	command(pb.NavigationAction_STOP, "")
	waitState(t, c, func(s *pb.BrowserState) bool { return !s.Loading && s.Error == "" })
	command(pb.NavigationAction_NAVIGATE, url+"/recovered")
	waitState(t, c, func(s *pb.BrowserState) bool { return s.Url == url+"/recovered" && !s.Loading })
}

func TestOrderedBrowserInput(t *testing.T) {
	c := startServer(t)
	events := make(chan string, 64)
	html := `<!doctype html><meta charset="utf-8"><style>body{margin:0;height:3000px}input{position:absolute;left:10px;top:10px;width:200px;height:40px}#area{position:absolute;top:100px;width:300px;height:200px}</style><form><input><button>Submit</button></form><div id="area">Drag here</div><script>
 const report=v=>fetch('/event?v='+encodeURIComponent(v));
 document.querySelector('form').onsubmit=e=>{e.preventDefault();report('text:'+document.querySelector('input').value)};
 const area=document.querySelector('#area');for(const type of ['mousedown','mouseup','dblclick'])area.addEventListener(type,e=>report(type+':'+e.button+':'+e.buttons));
 area.addEventListener('mousemove',e=>{if(e.buttons)report('drag:'+e.buttons)});
 area.addEventListener('contextmenu',e=>e.preventDefault());
 area.addEventListener('wheel',e=>report('wheel:'+e.deltaY+':'+e.shiftKey));
 </script>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/event" {
			events <- r.URL.Query().Get("v")
			w.WriteHeader(204)
			return
		}
		fmt.Fprint(w, html)
	}))
	defer server.Close()
	_, err := c.NavigateToUrl(deadline(t), &pb.Url{Url: server.URL})
	requireOK(t, err)
	send := func(e *pb.InputEvent) { t.Helper(); _, err := c.SendInput(deadline(t), e); requireOK(t, err) }
	pointer := func(x, y int32, b, count uint32) {
		send(&pb.InputEvent{Kind: pb.InputKind_POINTER_INPUT, X: x, Y: y, Buttons: b, ClickCount: count})
	}
	pointer(30, 30, 1, 1)
	pointer(30, 30, 0, 1)
	send(&pb.InputEvent{Kind: pb.InputKind_TEXT_INPUT, Text: "replace me"})
	modifier := uint32(2)
	if runtime.GOOS == "darwin" {
		modifier = 4
	}
	send(&pb.InputEvent{Kind: pb.InputKind_KEY_INPUT, Key: "a", Modifiers: modifier})
	send(&pb.InputEvent{Kind: pb.InputKind_PASTE_INPUT, Text: "__KEY__Enter café 世界X"})
	send(&pb.InputEvent{Kind: pb.InputKind_KEY_INPUT, Key: "Backspace"})
	send(&pb.InputEvent{Kind: pb.InputKind_KEY_INPUT, Key: "Enter"})
	expectEvent(t, events, "text:__KEY__Enter café 世界")
	pointer(30, 140, 1, 1)
	expectEvent(t, events, "mousedown:0:1")
	pointer(60, 150, 1, 0)
	expectEvent(t, events, "drag:1")
	pointer(60, 150, 0, 1)
	// The release can include a final held movement before mouseup.
	for {
		select {
		case e := <-events:
			if e == "mouseup:0:0" {
				goto released
			}
			if e != "drag:1" {
				t.Fatalf("unexpected drag event: %s", e)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("missing release")
		}
	}
released:
	pointer(60, 150, 2, 1)
	expectEvent(t, events, "mousedown:2:2")
	send(&pb.InputEvent{Kind: pb.InputKind_RESET_INPUT})
	expectEvent(t, events, "mouseup:2:0")
	send(&pb.InputEvent{Kind: pb.InputKind_WHEEL_INPUT, X: 60, Y: 150, DeltaY: 48, Modifiers: 8})
	expectEvent(t, events, "wheel:48:true")
}
