//go:build integration

package integration

import (
	"bytes"
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "termium/client/pb"
)

// Check effects in the page through an independent HTTP fixture, not RPC success
// strings or a mocked Puppeteer API. No public website is needed.
const fixtureHTML = `<!doctype html><html><head><meta charset="utf-8"><style>
body { margin:0; background:rgb(18,86,52); }
input,button { position:absolute; left:10px; width:220px; height:40px; }
input { top:10px; } #submit { top:70px; } #prompt { top:130px; } #confirm { top:190px; }
</style></head><body>
<form><input aria-label="Text"><button id="submit">Submit</button></form>
<button id="prompt">Prompt</button><button id="confirm">Confirm</button>
<script>
const report = value => fetch('/event?value=' + encodeURIComponent(value));
document.querySelector('form').onsubmit = e => { e.preventDefault(); report(document.querySelector('input').value); };
document.querySelector('#prompt').onclick = () => report(String(prompt('Your name?', 'default name')));
document.querySelector('#confirm').onclick = () => report(String(confirm('Continue?')));
</script></body></html>`

func fixture(t *testing.T) (string, <-chan string) {
	t.Helper()
	events := make(chan string, 16)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fixtureHTML))
	})
	mux.HandleFunc("/hint", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<title>Hint test</title><button onclick="fetch('/event?value=hint-click')">Only hint</button>`))
	})
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/page?redirected=1", http.StatusFound)
	})
	mux.HandleFunc("/event", func(w http.ResponseWriter, r *http.Request) {
		select {
		case events <- r.URL.Query().Get("value"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusTooManyRequests)
		}
	})
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL, events
}

func expectEvent(t *testing.T, events <-chan string, want string) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatalf("page reported %q; want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("page did not report %q", want)
	}
}

func TestBrowser(t *testing.T) {
	client := startServer(t)
	if !t.Run("open tab", func(t *testing.T) {
		_, err := client.OpenTab(deadline(t), &pb.Empty{})
		requireOK(t, err)
	}) {
		t.Fatal("browser startup failed; cannot exercise page behavior")
	}

	t.Run("failed navigation and recovery", func(t *testing.T) {
		url, _ := fixture(t)
		ctx := deadline(t)
		_, err := client.NavigateToUrl(ctx, &pb.Url{Url: url})
		requireOK(t, err)
		_, err = client.NavigateToUrl(ctx, &pb.Url{Url: url + "/broken"})
		if status.Code(err) != codes.Internal {
			t.Fatalf("network failure reported incorrectly: %v", err)
		}
		_, err = client.NavigateToUrl(ctx, &pb.Url{Url: url + "/recovered"})
		requireOK(t, err)
		current, err := client.GetCurrentUrl(ctx, &pb.Empty{})
		requireOK(t, err)
		if current.Url != url+"/recovered" {
			t.Fatalf("navigation did not recover: %v", current)
		}
	})

	t.Run("redirect and tab reuse", func(t *testing.T) {
		url, _ := fixture(t)
		ctx := deadline(t)
		_, err := client.NavigateToUrl(ctx, &pb.Url{Url: url + "/redirect"})
		requireOK(t, err)
		_, err = client.OpenTab(ctx, &pb.Empty{})
		requireOK(t, err)
		current, err := client.GetCurrentUrl(ctx, &pb.Empty{})
		requireOK(t, err)
		if current.Url != url+"/page?redirected=1" {
			t.Fatalf("unexpected URL after redirect/reuse: %s", current.Url)
		}
	})

	t.Run("mouse unicode text and special keys", func(t *testing.T) {
		url, events := fixture(t)
		ctx := deadline(t)
		_, err := client.NavigateToUrl(ctx, &pb.Url{Url: url})
		requireOK(t, err)
		_, err = client.ClickMouse(ctx, &pb.Coordinate{X: 30, Y: 30})
		requireOK(t, err)
		for _, text := range []string{"hello café 世界X", "__KEY__Backspace", "__KEY__Enter"} {
			_, err = client.SendKeyboardInput(ctx, &pb.Text{Content: text})
			requireOK(t, err)
		}
		expectEvent(t, events, "hello café 世界")
		_, err = client.ClickMouse(ctx, &pb.Coordinate{X: 30, Y: 90})
		requireOK(t, err)
		expectEvent(t, events, "hello café 世界")
		_, err = client.SendKeyboardInput(ctx, &pb.Text{Content: "__KEY__DefinitelyNotAKey"})
		if status.Code(err) != codes.Internal {
			t.Fatalf("invalid key should return INTERNAL, got %v", err)
		}
		_, err = client.GetCurrentUrl(ctx, &pb.Empty{})
		requireOK(t, err) // A rejected command must not kill the server.
	})

	t.Run("screenshots resize cancel and reconnect", func(t *testing.T) {
		url, _ := fixture(t)
		ctx := deadline(t)
		_, err := client.NavigateToUrl(ctx, &pb.Url{Url: url})
		requireOK(t, err)
		for _, tc := range []struct {
			format        string
			width, height int32
		}{{"png", 640, 480}, {"jpeg", 800, 600}} {
			_, err = client.SetViewport(ctx, &pb.ViewportSize{Width: tc.width, Height: tc.height})
			requireOK(t, err)
			streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			stream, err := client.StreamScreenshots(streamCtx, &pb.ScreenshotRequest{Fps: 10, Format: tc.format})
			requireOK(t, err)
			t.Cleanup(cancel)
			for i := 0; i < 2; {
				frame, err := stream.Recv()
				requireOK(t, err)
				decoded, format, err := image.Decode(bytes.NewReader(frame.Data))
				requireOK(t, err)
				if format != tc.format {
					t.Fatalf("wrong screenshot: %s %v", format, decoded.Bounds())
				}
				// Chromium's compositor can deliver the previous surface briefly
				// after viewport acknowledgement, notably on macOS. Require two
				// correctly resized frames within the stream deadline.
				if decoded.Bounds().Dx() != int(tc.width) || decoded.Bounds().Dy() != int(tc.height) {
					continue
				}
				// Background pixel proves this is the rendered fixture, not a blank frame.
				r, g, b, _ := decoded.At(400, 300).RGBA()
				for j, got := range []uint32{r >> 8, g >> 8, b >> 8} {
					want := []int{18, 86, 52}[j]
					if delta := int(got) - want; delta < -4 || delta > 4 {
						t.Fatalf("wrong screenshot background: %d,%d,%d", r>>8, g>>8, b>>8)
					}
				}
				i++
			}
			cancel()
			// Drain any frames already in flight; the cancelled stream must terminate.
			for {
				if _, err = stream.Recv(); err != nil {
					break
				}
			}
			if status.Code(err) != codes.Canceled {
				t.Fatalf("stream cancellation: %v", err)
			}
		}
	})

	for _, tc := range []struct {
		name        string
		kind        pb.DialogType
		y           int32
		accept      bool
		input, want string
	}{
		{"prompt text", pb.DialogType_PROMPT, 150, true, "Ada", "Ada"},
		{"prompt empty text", pb.DialogType_PROMPT, 150, true, "", ""},
		{"prompt dismiss", pb.DialogType_PROMPT, 150, false, "", "null"},
		{"confirm dismiss", pb.DialogType_CONFIRM, 210, false, "", "false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url, events := fixture(t)
			ctx := deadline(t)
			_, err := client.NavigateToUrl(ctx, &pb.Url{Url: url})
			requireOK(t, err)
			streamCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			stream, err := client.StreamDialogs(streamCtx)
			requireOK(t, err)
			_, err = stream.Header() // Explicit registration barrier; no timing sleeps.
			requireOK(t, err)
			clickDone := make(chan error, 1)
			go func() { _, err := client.ClickMouse(ctx, &pb.Coordinate{X: 30, Y: tc.y}); clickDone <- err }()
			event, err := stream.Recv()
			requireOK(t, err)
			if event.Id == "" || event.Type != tc.kind {
				t.Fatalf("wrong dialog: %v", event)
			}
			if tc.kind == pb.DialogType_PROMPT && (event.Message != "Your name?" || event.DefaultValue != "default name") {
				t.Fatalf("lost prompt fields: %v", event)
			}
			requireOK(t, stream.Send(&pb.DialogResponse{Id: event.Id, Accepted: tc.accept, InputText: tc.input}))
			expectEvent(t, events, tc.want)
			requireOK(t, <-clickDone)
			requireOK(t, stream.CloseSend())
			// A half-closed dialog stream must finish, allowing graceful shutdown.
			_, err = stream.Recv()
			if err != io.EOF {
				t.Fatalf("dialog stream did not finish: %v", err)
			}
		})
	}

	t.Run("dialog disconnect releases page and allows reconnect", func(t *testing.T) {
		url, events := fixture(t)
		ctx := deadline(t)
		_, err := client.NavigateToUrl(ctx, &pb.Url{Url: url})
		requireOK(t, err)
		streamCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		stream, err := client.StreamDialogs(streamCtx)
		requireOK(t, err)
		_, err = stream.Header()
		requireOK(t, err)

		// A second subscriber must not steal the first subscriber's dialogs.
		other, err := client.StreamDialogs(ctx)
		requireOK(t, err)
		_, err = other.Recv()
		if status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("second subscriber: %v", err)
		}

		clickDone := make(chan error, 1)
		go func() { _, err := client.ClickMouse(ctx, &pb.Coordinate{X: 30, Y: 150}); clickDone <- err }()
		event, err := stream.Recv()
		requireOK(t, err)
		if event.Type != pb.DialogType_PROMPT {
			t.Fatalf("wrong dialog: %v", event)
		}
		cancel() // Disconnect without answering a pending prompt.
		expectEvent(t, events, "default name")
		requireOK(t, <-clickDone)

		reconnected, err := client.StreamDialogs(ctx)
		requireOK(t, err)
		_, err = reconnected.Header()
		requireOK(t, err)
		requireOK(t, reconnected.CloseSend())
		if _, err = reconnected.Recv(); err != io.EOF {
			t.Fatalf("reconnected stream: %v", err)
		}
		_, err = client.ClickMouse(ctx, &pb.Coordinate{X: 30, Y: 90})
		requireOK(t, err)
		expectEvent(t, events, "") // The page is still interactive after disconnect.
	})
}

func TestMissingBrowserReturnsError(t *testing.T) {
	client := startServer(t, "PUPPETEER_EXECUTABLE_PATH=/nonexistent/termium-test-chrome")
	_, err := client.OpenTab(deadline(t), &pb.Empty{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("missing browser should return INTERNAL, got %v", err)
	}
	_, err = client.NavigateToUrl(deadline(t), &pb.Url{Url: "http://127.0.0.1:1/"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("navigation hid browser launch failure: %v", err)
	}
	_, err = client.GetCurrentUrl(deadline(t), &pb.Empty{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("server must remain available after launch failure, got %v", err)
	}
}
