package main

import (
	"context"
	"image"
	"image/color"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"google.golang.org/grpc"
	pb "termium/client/pb"
)

func uiScreen(t *testing.T, w, h int) tcell.SimulationScreen {
	t.Helper()
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(w, h)
	oldDims, oldChars := sDims, charSize
	charSize = CharSize{8, 16}
	updateScreenDimensions(s)
	t.Cleanup(func() { s.Fini(); sDims = oldDims; charSize = oldChars })
	return s
}
func recorder() (*KeyboardHandler, *[]browserOperation) {
	ops := []browserOperation{}
	kh := NewKeyboardHandler(nil)
	kh.submit = func(op browserOperation) bool { ops = append(ops, op); return true }
	return kh, &ops
}
func key(k tcell.Key) *tcell.EventKey { return tcell.NewEventKey(k, 0, tcell.ModNone) }

func TestTcellViewportColorsAndClipping(t *testing.T) {
	s := uiScreen(t, 12, 9)
	sentinel := tcell.StyleDefault.Background(tcell.ColorPurple)
	fillRect(s, image.Rect(0, 0, 12, 9), sentinel)
	r := viewportRect(12, 9)
	img := image.NewRGBA(image.Rect(7, 11, 7+r.Dx(), 11+r.Dy()*2))
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			c := color.RGBA{250, 240, 230, 255}
			if (y-img.Bounds().Min.Y)%2 == 1 {
				c = color.RGBA{10, 20, 30, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	if err := displayWithTcell(s, img); err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 9; y++ {
		for x := 0; x < 12; x++ {
			ch, _, style, _ := s.GetContent(x, y)
			fg, bg, _ := style.Decompose()
			if !image.Pt(x, y).In(r) {
				if bg != tcell.ColorPurple {
					t.Fatalf("renderer overwrote chrome at %d,%d", x, y)
				}
				continue
			}
			if ch != '▀' || fg != tcell.NewRGBColor(250, 240, 230) || bg != tcell.NewRGBColor(10, 20, 30) {
				t.Fatalf("bad sample at %d,%d: %c %v %v", x, y, ch, fg, bg)
			}
		}
	}
}
func TestTinyTerminalAndUnicodeEditor(t *testing.T) {
	s := uiScreen(t, 80, 24)
	kh, _ := recorder()
	d := NewDialog(&pb.DialogEvent{Type: pb.DialogType_PROMPT, Message: "界é\nA very long message", DefaultValue: "café世界"})
	d.HandleKey(key(tcell.KeyRight))
	d.HandleKey(key(tcell.KeyBackspace2))
	d.HandleKey(key(tcell.KeyLeft))
	d.HandleKey(key(tcell.KeyDelete))
	if d.editor.value() != "café" {
		t.Fatalf("Unicode edit corrupted: %q", d.editor.value())
	}
	d.HandleKey(key(tcell.KeyCtrlA))
	d.HandleKey(tcell.NewEventKey(tcell.KeyRune, '界', 0))
	if d.editor.value() != "界" {
		t.Fatal(d.editor.value())
	}
	for _, size := range []image.Point{{1, 1}, {2, 2}, {5, 3}, {12, 5}, {40, 10}} {
		s.SetSize(size.X, size.Y)
		updateScreenDimensions(s)
		kh.menu = true
		kh.menuIndex = 6
		kh.Draw(s)
		d.Draw(s)
		if d.width < 0 || d.height < 0 || d.x < 0 || d.y < 0 {
			t.Fatalf("negative dialog geometry: %+v", d)
		}
		if size.Y >= 5 {
			r := kh.menuRect()
			if kh.menuIndex-kh.menuStart() >= r.Dy()-2 {
				t.Fatal("selected menu item invisible")
			}
		}
	}
}
func TestPageKeysAndPasteDoNotBecomeCommands(t *testing.T) {
	s := uiScreen(t, 80, 24)
	kh, ops := recorder()
	kh.HandleKeyEvent(s, key(tcell.KeyLeft))
	kh.HandleKeyEvent(s, key(tcell.KeyCtrlA))
	if (*ops)[0].input.Key != "ArrowLeft" || (*ops)[1].input.Key != "a" || (*ops)[1].input.Modifiers != 2 {
		t.Fatalf("wrong page keys: %+v", *ops)
	}
	kh.pasteEvent(true)
	for _, r := range "__KEY__Enter 世界" {
		ev := tcell.NewEventKey(tcell.KeyRune, r, 0)
		if exit, handled := kh.globalKey(ev, false); exit || handled {
			t.Fatal("paste executed a shortcut")
		}
		kh.HandleKeyEvent(s, ev)
	}
	kh.pasteEvent(false)
	paste := (*ops)[2].input
	if paste.Kind != pb.InputKind_PASTE_INPUT || paste.Text != "__KEY__Enter 世界" {
		t.Fatal(paste)
	}
	kh.openAddress()
	kh.editor.insert("example.com/世界")
	kh.HandleKeyEvent(s, key(tcell.KeyEnter))
	nav := (*ops)[3].navigation
	if nav == nil || nav.Url != "https://example.com/%E4%B8%96%E7%95%8C" {
		t.Fatalf("bad address: %v", nav)
	}
}
func TestMouseCaptureWheelAndKeyboardPointer(t *testing.T) {
	s := uiScreen(t, 80, 24)
	kh, ops := recorder()
	mouse := func(x, y int, b tcell.ButtonMask, m tcell.ModMask) {
		kh.HandleMouseEvent(s, tcell.NewEventMouse(x, y, b, m))
	}
	mouse(1, 2, tcell.Button1, 0)
	mouse(4, 5, tcell.Button1, 0)
	mouse(90, 30, 0, 0)
	if len(*ops) != 3 {
		t.Fatal(len(*ops))
	}
	down, drag, up := (*ops)[0].input, (*ops)[1].input, (*ops)[2].input
	if down.X != 4 || down.Y != 8 || down.Buttons != 1 || down.ClickCount != 1 {
		t.Fatal(down)
	}
	if drag.Buttons != 1 || drag.ClickCount != 0 {
		t.Fatal("drag emitted another press", drag)
	}
	if up.Buttons != 0 || up.X != 620 || up.Y != 312 || up.ClickCount != 1 {
		t.Fatal("release outside viewport lost", up)
	}
	mouse(4, 5, tcell.WheelDown, tcell.ModShift)
	wheel := (*ops)[3].input
	if wheel.Kind != pb.InputKind_WHEEL_INPUT || wheel.DeltaX != 48 || wheel.DeltaY != 0 {
		t.Fatal(wheel)
	}
	mouse(4, 5, tcell.Button2, 0)
	mouse(4, 5, 0, 0)
	if (*ops)[4].input.Buttons != 2 {
		t.Fatal("right click lost")
	}
	kh.action("pointer")
	kh.HandleKeyEvent(s, tcell.NewEventKey(tcell.KeyRune, ' ', 0))
	kh.HandleKeyEvent(s, key(tcell.KeyRight))
	kh.HandleKeyEvent(s, key(tcell.KeyEscape))
	last := (*ops)[len(*ops)-1].input
	if last.Kind != pb.InputKind_RESET_INPUT || kh.pointerMode {
		t.Fatal("keyboard drag not released on exit")
	}
}
func TestDialogsNeedClicksAndAllowGlobalEvents(t *testing.T) {
	s := uiScreen(t, 80, 24)
	d := NewDialog(&pb.DialogEvent{Id: "dialog", Type: pb.DialogType_CONFIRM, Message: "Continue?"})
	d.Draw(s)
	if d.HandleMouse(tcell.NewEventMouse(d.buttonOKX, d.buttonOKY, 0, 0)) != nil {
		t.Fatal("hover accepted dialog")
	}
	if r := d.HandleMouse(tcell.NewEventMouse(d.buttonOKX, d.buttonOKY, tcell.Button1, 0)); r == nil || !r.Accepted {
		t.Fatal("click did not accept")
	}
	old := currentDialog
	t.Cleanup(func() { currentDialog = old })
	currentDialog = NewDialog(&pb.DialogEvent{Type: pb.DialogType_PROMPT})
	if handleDialogInput(tcell.NewEventResize(20, 8)) || handleDialogInput(tcell.NewEventInterrupt(nil)) {
		t.Fatal("modal swallowed global event")
	}
	kh, _ := recorder()
	for i := 0; i < 3; i++ {
		exit, _ := kh.globalKey(key(tcell.KeyEscape), true)
		if exit != (i == 2) {
			t.Fatal("emergency exit inaccessible through modal")
		}
	}
}
func TestFrameBufferNeverReusesDisplayedStorage(t *testing.T) {
	fb := NewFrameBuffer()
	fb.Publish(&Frame{Data: []byte{42}})
	shown := fb.GetDisplayFrame()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10000; i++ {
			fb.Publish(&Frame{Data: []byte{byte(i)}})
		}
	}()
	for i := 0; i < 10000; i++ {
		if shown.Data[0] != 42 {
			t.Fatal("producer overwrote displayed frame")
		}
		fb.GetDisplayFrame()
	}
	wg.Wait()
	received, displayed, dropped := fb.GetStats()
	pending := uint64(0)
	if fb.GetDisplayFrame() != nil {
		pending = 1
	}
	if received != displayed+dropped+pending {
		t.Fatal("frame accounting lost ownership", received, displayed, dropped)
	}
}

type blockedInputClient struct {
	pb.BrowserControlClient
	entered chan *pb.InputEvent
	release chan struct{}
}

func (c *blockedInputClient) SendInput(ctx context.Context, in *pb.InputEvent, _ ...grpc.CallOption) (*pb.Message, error) {
	select {
	case c.entered <- in:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-c.release:
		return &pb.Message{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func TestInputQueuePreservesTransitionsAndOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &blockedInputClient{entered: make(chan *pb.InputEvent, 8), release: make(chan struct{}, 8)}
	d := newInputDispatcher(ctx, client, func(any) {})
	send := func(e *pb.InputEvent) {
		if !d.enqueue(browserOperation{input: e}) {
			t.Fatal("queue rejected")
		}
	}
	receive := func() *pb.InputEvent {
		select {
		case e := <-client.entered:
			return e
		case <-time.After(time.Second):
			t.Fatal("input stalled")
			return nil
		}
	}
	send(&pb.InputEvent{Kind: pb.InputKind_POINTER_INPUT, Buttons: 1, ClickCount: 1})
	if receive().Buttons != 1 {
		t.Fatal("missing press")
	}
	send(&pb.InputEvent{Kind: pb.InputKind_POINTER_INPUT, Buttons: 1, X: 10})
	send(&pb.InputEvent{Kind: pb.InputKind_POINTER_INPUT, Buttons: 1, X: 20})
	send(&pb.InputEvent{Kind: pb.InputKind_POINTER_INPUT, ClickCount: 1})
	send(&pb.InputEvent{Kind: pb.InputKind_TEXT_INPUT, Text: "x"})
	send(&pb.InputEvent{Kind: pb.InputKind_KEY_INPUT, Key: "Enter"})
	client.release <- struct{}{}
	if receive().X != 20 {
		t.Fatal("motion did not coalesce")
	}
	client.release <- struct{}{}
	if e := receive(); e.Buttons != 0 || e.ClickCount != 1 {
		t.Fatal("release lost", e)
	}
	client.release <- struct{}{}
	if receive().Text != "x" {
		t.Fatal("typing reordered")
	}
	client.release <- struct{}{}
	if receive().Key != "Enter" {
		t.Fatal("submit reordered")
	}
	cancel()
}

func TestMenuMouseCaptureDoesNotEatNextPageClick(t *testing.T) {
	s := uiScreen(t, 80, 24)
	kh, ops := recorder()
	mouse := func(x, y int, b tcell.ButtonMask) { kh.HandleMouseEvent(s, tcell.NewEventMouse(x, y, b, 0)) }
	mouse(77, 0, tcell.Button1)
	mouse(77, 0, 0)
	if !kh.menu || kh.captureUI {
		t.Fatal("toolbar capture survived release")
	}
	kh.HandleKeyEvent(s, key(tcell.KeyEscape))
	mouse(4, 5, tcell.Button1)
	if len(*ops) != 1 || (*ops)[0].input.Buttons != 1 {
		t.Fatal("first page click after menu was lost")
	}
	kh.action("quit")
	mouse(4, 5, 0)
	quit, _ := quitButtons(s.Size())
	mouse(quit.Min.X, quit.Min.Y, tcell.Button1)
	if !kh.exitRequested {
		t.Fatal("mouse quit unreachable")
	}
}
func TestCombiningTextAndOversizedReplacement(t *testing.T) {
	s := uiScreen(t, 20, 8)
	drawText(s, 1, 0, 18, "e\u0301界", tcell.StyleDefault)
	r, combining, _, _ := s.GetContent(1, 0)
	if r != 'e' || len(combining) != 1 || combining[0] != '\u0301' {
		t.Fatal("combining accent lost", r, combining)
	}
	var e textEditor
	e.set("keep this address", true)
	value := make([]rune, 9000)
	for i := range value {
		value[i] = 'x'
	}
	e.insert(string(value))
	if e.value() != "keep this address" {
		t.Fatal("oversized paste destroyed original address")
	}
}

func TestPointerDoesNotStealAddressCursorAndOverlaysAreExclusive(t *testing.T) {
	s := uiScreen(t, 80, 24)
	kh, _ := recorder()
	kh.action("pointer")
	kh.openAddress()
	kh.Draw(s)
	_, y, visible := s.GetCursor()
	if !visible || y != 0 {
		t.Fatal("pointer stole address caret")
	}
	kh.action("help")
	kh.Draw(s)
	_, _, visible = s.GetCursor()
	if visible {
		t.Fatal("editor cursor leaked through help")
	}
	kh.action("menu")
	if kh.help || !kh.menu {
		t.Fatal("help obscures menu")
	}
	kh.action("help")
	kh.action("pointer")
	if kh.help || kh.menu {
		t.Fatal("overlay obscures pointer mode")
	}
}

func TestLostResizeNotificationStillUpdatesBrowserViewport(t *testing.T) {
	s := uiScreen(t, 80, 24)
	kh, ops := recorder()
	old := keyboardHandler
	keyboardHandler = kh
	t.Cleanup(func() { keyboardHandler = old })
	// SimulationScreen changes size without delivering EventResize, reproducing
	// the dropped notification in tcell's bounded event queue under frame load.
	s.SetSize(100, 30)
	ensureLayout(s)
	if sDims.Width != 100 || sDims.Height != 30 {
		t.Fatal("layout did not catch up")
	}
	if len(*ops) != 2 || (*ops)[1].viewport.Width != 784 || (*ops)[1].viewport.Height != 416 {
		t.Fatal("browser viewport remained stale", *ops)
	}
	ensureLayout(s)
	if len(*ops) != 2 {
		t.Fatal("unchanged geometry resent resize")
	}
}
