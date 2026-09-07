package main

import (
	"context"
	"fmt"
	"image"
	"net/url"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	pb "termium/client/pb"
)

type navControl struct {
	id, label string
	rect      image.Rectangle
	enabled   bool
}

type KeyboardHandler struct {
	grpcClient              pb.BrowserControlClient
	dispatcher              *inputDispatcher
	submit                  func(browserOperation) bool
	state                   pb.BrowserState
	editor                  textEditor
	focus                   string
	menu, help, quitConfirm bool
	menuIndex               int
	status                  string
	staleNotice             bool
	pendingAddress          string
	awaitingNavigation      bool
	pointerMode             bool
	tabsMenu                bool
	pointer                 image.Point
	pointerHeld             uint32
	mouseButtons            tcell.ButtonMask
	capturePage, captureUI  bool
	lastClick               time.Time
	lastClickAt             image.Point
	lastClickButton         tcell.ButtonMask
	clickCount              uint32
	escCount                int
	lastEscTime             time.Time
	exitRequested           bool
	pasting                 bool
	paste                   strings.Builder
}

func NewKeyboardHandler(client pb.BrowserControlClient) *KeyboardHandler {
	return &KeyboardHandler{grpcClient: client, focus: "page", status: "Ctrl+L: address   F10: menu   F6: keyboard pointer   Ctrl+Q: quit"}
}
func (kh *KeyboardHandler) start(ctx context.Context, s tcell.Screen) {
	notify := func(value any) { postUI(s, value) }
	kh.dispatcher = newInputDispatcher(ctx, kh.grpcClient, notify)
	kh.submit = kh.dispatcher.enqueue
	shutdownWg.Add(1)
	go func() {
		defer shutdownWg.Done()
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			state, err := kh.grpcClient.GetBrowserState(callCtx, &pb.Empty{})
			cancel()
			if ctx.Err() != nil {
				return
			}
			notify(stateUpdate{state, err})
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
}
func (kh *KeyboardHandler) queue(o browserOperation) {
	if kh.submit == nil || !kh.submit(o) {
		kh.status = "Input queue is busy; try again"
	}
}
func (kh *KeyboardHandler) input(event *pb.InputEvent) {
	event.Generation = kh.state.Generation
	event.TabId = kh.state.ActiveTabId
	kh.queue(browserOperation{input: event})
}
func (kh *KeyboardHandler) applyState(state *pb.BrowserState) {
	if state == nil || state.Generation < kh.state.Generation {
		return
	}
	if state.Generation != kh.state.Generation {
		kh.pointerHeld = 0
		kh.capturePage = false
	}
	kh.state = pb.BrowserState{Url: state.Url, Title: state.Title, CanBack: state.CanBack, CanForward: state.CanForward, Loading: state.Loading, Generation: state.Generation, Error: state.Error, Tabs: state.Tabs, ActiveTabId: state.ActiveTabId, VimiumStatus: state.VimiumStatus}
	if kh.tabsMenu {
		kh.menuIndex = min(kh.menuIndex, len(kh.menuActions())-1)
	}
	if state.Error != "" {
		kh.staleNotice = false
		kh.status = state.Error
		if kh.pendingAddress != "" && kh.focus == "page" {
			kh.editor.set(kh.pendingAddress, false)
			kh.focus = "address"
		}
		kh.pendingAddress = ""
	} else if !state.Loading && !kh.awaitingNavigation {
		kh.pendingAddress = ""
		if kh.status == "Loading…" {
			kh.status = "Ready · Ctrl+L: address · F10: menu"
		}
	}
}
func (kh *KeyboardHandler) result(result operationResult) {
	if result.operation.navigation != nil {
		kh.awaitingNavigation = false
	}
	if result.err != nil {
		kh.staleNotice = false
		kh.status = result.err.Error()
		if result.operation.navigation != nil && result.operation.navigation.Action == pb.NavigationAction_NAVIGATE && kh.focus == "page" {
			kh.editor.set(result.operation.navigation.Url, false)
			kh.focus = "address"
		}
		return
	}
	if result.stale {
		kh.applyState(result.state)
		// Reset and hover/release events routinely arrive after navigation.
		// They need no warning. A cancelled deliberate action needs feedback.
		e := result.operation.input
		passive := e != nil && (e.Kind == pb.InputKind_RESET_INPUT || e.Kind == pb.InputKind_POINTER_INPUT && e.Buttons == 0)
		if !passive && kh.state.Error == "" {
			kh.status = "Page changed; repeat the last action"
			kh.staleNotice = true
		}
		if result.operation.navigation != nil && result.operation.navigation.Action == pb.NavigationAction_NAVIGATE && kh.focus == "page" {
			kh.editor.set(result.operation.navigation.Url, false)
			kh.focus = "address"
		}
		return
	}
	if kh.staleNotice && (result.operation.navigation != nil || result.operation.input != nil && result.operation.input.Kind != pb.InputKind_RESET_INPUT) {
		kh.status = "Ready · Ctrl+L: address · F10: menu"
		kh.staleNotice = false
	}
	if result.operation.navigation != nil && result.operation.navigation.Action == pb.NavigationAction_NAVIGATE {
		kh.pendingAddress = result.operation.navigation.Url
	}
	kh.applyState(result.state)
}
func (kh *KeyboardHandler) controls(width int) []navControl {
	if width < 1 {
		return nil
	}
	menuWidth := min(6, width)
	controls := []navControl{}
	x := 0
	if width >= 28 {
		labels := []string{"[<]", "[>]", "[R]"}
		if kh.state.Loading {
			labels[2] = "[X]"
		}
		if width >= 70 {
			labels = []string{"[Back]", "[Forward]", "[Reload]"}
			if kh.state.Loading {
				labels[2] = "[Stop]"
			}
		}
		for i, id := range []string{"back", "forward", "reload"} {
			enabled := true
			if id == "back" {
				enabled = kh.state.CanBack && !kh.state.Loading
			}
			if id == "forward" {
				enabled = kh.state.CanForward && !kh.state.Loading
			}
			controls = append(controls, navControl{id, labels[i], image.Rect(x, 1, x+len(labels[i]), 2), enabled})
			x += len(labels[i]) + 1
		}
	}
	end := max(x, width-menuWidth-1)
	controls = append(controls, navControl{"address", "", image.Rect(x, 1, end, 2), true})
	controls = append(controls, navControl{"menu", "[Menu]", image.Rect(max(0, width-menuWidth), 1, width, 2), true})
	return controls
}
func (kh *KeyboardHandler) openAddress() {
	if kh.pointerHeld != 0 || kh.capturePage {
		kh.input(&pb.InputEvent{Kind: pb.InputKind_RESET_INPUT})
		kh.pointerHeld = 0
		kh.capturePage = false
	}
	kh.focus = "address"
	kh.menu = false
	kh.help = false
	text := kh.state.Url
	if text == "about:blank" {
		text = ""
	}
	kh.editor.set(text, true)
}
func normalizeAddress(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("Enter an address")
	}
	if value == "about:blank" {
		return value, nil
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.ContainsAny(parsed.Host, " \t\r\n") {
		return "", fmt.Errorf("Use a valid HTTP or HTTPS address")
	}
	return parsed.String(), nil
}
func (kh *KeyboardHandler) navigate(action pb.NavigationAction, address string) {
	if kh.submit == nil || !kh.submit(browserOperation{navigation: &pb.NavigationRequest{Action: action, Url: address, TabId: kh.state.ActiveTabId, Generation: kh.state.Generation}}) {
		kh.status = "Input queue is busy; try again"
		return
	}
	kh.awaitingNavigation = true
	kh.focus = "page"
	kh.menu = false
	if action != pb.NavigationAction_STOP {
		kh.state.Loading = true
		kh.status = "Loading…"
	}
}
func (kh *KeyboardHandler) action(id string) {
	if kh.tabAction(id) {
		return
	}
	switch id {
	case "home":
		kh.navigate(pb.NavigationAction_HOME, "")
	case "address":
		kh.openAddress()
	case "back":
		if kh.state.CanBack && !kh.state.Loading {
			kh.navigate(pb.NavigationAction_BACK, "")
		}
	case "forward":
		if kh.state.CanForward && !kh.state.Loading {
			kh.navigate(pb.NavigationAction_FORWARD, "")
		}
	case "reload":
		if kh.state.Loading {
			kh.navigate(pb.NavigationAction_STOP, "")
		} else {
			kh.navigate(pb.NavigationAction_RELOAD, "")
		}
	case "menu":
		kh.tabsMenu = false
		kh.help = false
		kh.menu = !kh.menu
		kh.menuIndex = 0
		kh.focus = "page"
	case "pointer":
		kh.help = false
		kh.pointerMode = !kh.pointerMode
		kh.menu = false
		kh.focus = "page"
		kh.input(&pb.InputEvent{Kind: pb.InputKind_RESET_INPUT})
		kh.pointerHeld = 0
		rect := viewportRect(sDims.Width, sDims.Height)
		if !kh.pointer.In(rect) {
			kh.pointer = image.Pt((rect.Min.X+rect.Max.X)/2, (rect.Min.Y+rect.Max.Y)/2)
		}
	case "help":
		kh.help = !kh.help
		kh.menu = false
	case "quit":
		kh.quitConfirm = true
		kh.menu = false
	}
}
func ctrl(ev *tcell.EventKey, key tcell.Key, r rune) bool {
	return ev.Key() == key || ev.Modifiers()&tcell.ModCtrl != 0 && (ev.Rune() == r || ev.Rune() == r-32)
}

// Global shortcuts are checked before modal input. Paste content never executes them.
func (kh *KeyboardHandler) globalKey(ev *tcell.EventKey, modal bool) (bool, bool) {
	if kh.pasting {
		return false, false
	}
	if ev.Key() == tcell.KeyEscape {
		now := time.Now()
		if now.Sub(kh.lastEscTime) > time.Second {
			kh.escCount = 0
		}
		kh.escCount++
		kh.lastEscTime = now
		if kh.escCount >= 3 {
			return true, true
		}
	} else {
		kh.escCount = 0
	}
	if ctrl(ev, tcell.KeyCtrlQ, 'q') {
		kh.action("quit")
		return false, true
	}
	if modal || kh.quitConfirm {
		return false, false
	}
	switch {
	case ctrl(ev, tcell.KeyCtrlL, 'l'):
		kh.openAddress()
	case ev.Key() == tcell.KeyF10:
		kh.action("menu")
	case ev.Key() == tcell.KeyF1:
		kh.action("help")
	case ev.Key() == tcell.KeyF6:
		kh.action("pointer")
	case ctrl(ev, tcell.KeyCtrlT, 't'):
		kh.action("newtab")
	case ctrl(ev, tcell.KeyCtrlW, 'w'):
		kh.action("closetab")
	case ev.Key() == tcell.KeyF5 || ctrl(ev, tcell.KeyCtrlR, 'r'):
		kh.action("reload")
	case ev.Key() == tcell.KeyHome && ev.Modifiers()&tcell.ModAlt != 0:
		kh.action("home")
	case ev.Key() == tcell.KeyLeft && ev.Modifiers()&tcell.ModAlt != 0:
		kh.action("back")
	case ev.Key() == tcell.KeyRight && ev.Modifiers()&tcell.ModAlt != 0:
		kh.action("forward")
	default:
		return false, false
	}
	return false, true
}
func (kh *KeyboardHandler) HandleKeyEvent(s tcell.Screen, ev *tcell.EventKey) bool {
	if kh.pasting {
		if ev.Key() == tcell.KeyRune && kh.paste.Len() < 1024*1024 {
			kh.paste.WriteRune(ev.Rune())
		} else if ev.Key() == tcell.KeyEnter {
			kh.paste.WriteByte('\n')
		} else if ev.Key() == tcell.KeyTab {
			kh.paste.WriteByte('\t')
		}
		return false
	}
	if kh.quitConfirm {
		switch ev.Key() {
		case tcell.KeyEnter:
			return true
		case tcell.KeyEscape:
			kh.quitConfirm = false
		}
		if ev.Rune() == 'n' || ev.Rune() == 'N' {
			kh.quitConfirm = false
		}
		if ev.Rune() == 'y' || ev.Rune() == 'Y' {
			return true
		}
		return false
	}
	if kh.help {
		if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyEnter || ev.Key() == tcell.KeyF1 {
			kh.help = false
		}
		return false
	}
	if kh.menu {
		switch ev.Key() {
		case tcell.KeyEscape:
			kh.menu = false
		case tcell.KeyUp, tcell.KeyBacktab:
			kh.menuIndex = (kh.menuIndex + len(kh.menuActions()) - 1) % len(kh.menuActions())
		case tcell.KeyDown, tcell.KeyTab:
			kh.menuIndex = (kh.menuIndex + 1) % len(kh.menuActions())
		case tcell.KeyEnter:
			kh.action(kh.menuActions()[kh.menuIndex])
		}
		return false
	}
	if kh.focus == "address" {
		switch ev.Key() {
		case tcell.KeyEscape:
			kh.focus = "page"
		case tcell.KeyEnter:
			address, err := normalizeAddress(kh.editor.value())
			if err != nil {
				kh.status = err.Error()
				return false
			}
			kh.pendingAddress = kh.editor.value()
			kh.navigate(pb.NavigationAction_NAVIGATE, address)
		case tcell.KeyTab:
			kh.focus = "menu"
		case tcell.KeyBacktab:
			kh.focus = "reload"
		default:
			kh.editor.key(ev)
		}
		return false
	}
	if kh.focus != "page" {
		controls := kh.controls(sDims.Width)
		if len(controls) == 0 {
			return false
		}
		index := 0
		for i, c := range controls {
			if c.id == kh.focus {
				index = i
			}
		}
		switch ev.Key() {
		case tcell.KeyEnter:
			kh.action(kh.focus)
		case tcell.KeyEscape:
			kh.focus = "page"
		case tcell.KeyTab, tcell.KeyRight:
			kh.focus = controls[(index+1)%len(controls)].id
		case tcell.KeyBacktab, tcell.KeyLeft:
			kh.focus = controls[(index+len(controls)-1)%len(controls)].id
		}
		if kh.focus == "address" {
			kh.openAddress()
		}
		return false
	}
	if kh.pointerMode {
		kh.pointerKey(ev)
		return false
	}
	kh.pageKey(ev)
	return false
}
func (kh *KeyboardHandler) GetBrowserMode() BrowserMode {
	if kh.focus == "address" {
		return ModeURL
	}
	return ModeNormal
}
func (kh *KeyboardHandler) IsExitRequested() bool { return kh.exitRequested }
func (kh *KeyboardHandler) pasteEvent(start bool) {
	if !start && !kh.pasting {
		return
	}
	if start {
		kh.pasting = true
		kh.paste.Reset()
		kh.escCount = 0
		return
	}
	text := kh.paste.String()
	kh.pasting = false
	kh.paste.Reset()
	if kh.focus == "address" {
		kh.editor.insert(text)
	} else if !kh.menu && !kh.help && !kh.quitConfirm {
		kh.input(&pb.InputEvent{Kind: pb.InputKind_PASTE_INPUT, Text: text})
	}
}
func inputModifiers(mod tcell.ModMask) uint32 {
	var result uint32
	if mod&tcell.ModAlt != 0 {
		result |= 1
	}
	if mod&tcell.ModCtrl != 0 {
		result |= 2
	}
	if mod&tcell.ModMeta != 0 {
		result |= 4
	}
	if mod&tcell.ModShift != 0 {
		result |= 8
	}
	return result
}
func (kh *KeyboardHandler) pageKey(ev *tcell.EventKey) {
	names := map[tcell.Key]string{tcell.KeyEnter: "Enter", tcell.KeyTab: "Tab", tcell.KeyBacktab: "Tab", tcell.KeyEscape: "Escape", tcell.KeyBackspace: "Backspace", tcell.KeyBackspace2: "Backspace", tcell.KeyDelete: "Delete", tcell.KeyInsert: "Insert", tcell.KeyLeft: "ArrowLeft", tcell.KeyRight: "ArrowRight", tcell.KeyUp: "ArrowUp", tcell.KeyDown: "ArrowDown", tcell.KeyHome: "Home", tcell.KeyEnd: "End", tcell.KeyPgUp: "PageUp", tcell.KeyPgDn: "PageDown"}
	mod := inputModifiers(ev.Modifiers())
	key := names[ev.Key()]
	if ev.Key() == tcell.KeyBacktab {
		mod |= 8
	}
	if key == "" && ev.Key() >= tcell.KeyCtrlA && ev.Key() <= tcell.KeyCtrlZ {
		key = string(rune('a' + ev.Key() - tcell.KeyCtrlA))
		mod |= 2
	}
	if ev.Key() == tcell.KeyRune {
		if mod&7 == 0 {
			kh.input(&pb.InputEvent{Kind: pb.InputKind_TEXT_INPUT, Text: string(ev.Rune()), Modifiers: mod})
			return
		}
		key = string(ev.Rune())
	}
	if key != "" {
		kh.input(&pb.InputEvent{Kind: pb.InputKind_KEY_INPUT, Key: key, Modifiers: mod})
	}
}
