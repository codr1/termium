package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	pb "termium/client/pb"
)

func TestBrowserToolsKeysAndClipboard(t *testing.T) {
	_ = uiScreen(t, 100, 40)
	kh, ops := recorder()
	kh.state = pb.BrowserState{ActiveTabId: "tab", Generation: 7}
	kh.globalKey(key(tcell.KeyF12), false)
	if len(*ops) != 1 || (*ops)[0].navigation.Action != pb.NavigationAction_DEVTOOLS {
		t.Fatal("F12 did not open tools")
	}
	kh.globalKey(key(tcell.KeyF8), false)
	if len(*ops) != 2 || (*ops)[1].selection.TabId != "tab" || (*ops)[1].selection.Generation != 7 {
		t.Fatal("copy lost target identity")
	}
	old := graphicsOutput
	var output bytes.Buffer
	graphicsOutput = &output
	t.Cleanup(func() { graphicsOutput = old })
	text := "世界\n\x1b]52;c;malicious\a"
	kh.result(operationResult{clipboard: &text})
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
	if output.String() != want {
		t.Fatalf("clipboard must encode content, got %q", output.String())
	}
	output.Reset()
	kh.result(operationResult{clipboard: &text, stale: true})
	if output.Len() != 0 {
		t.Fatal("stale copy changed clipboard")
	}
	empty := ""
	kh.result(operationResult{clipboard: &empty})
	if output.Len() != 0 {
		t.Fatal("empty selection cleared clipboard")
	}
	kh.pasteEvent(true)
	kh.globalKey(key(tcell.KeyF12), false)
	if len(*ops) != 2 {
		t.Fatal("paste triggered developer tools")
	}
}

// Exercise the menu users see, including scrolling in a short terminal. Keep
// labels and action routing covered together rather than invoking action IDs.
func TestBrowserToolsMenuKeyboardAndMouse(t *testing.T) {
	for _, name := range []string{"Developer tools", "Copy selected text", "Extensions"} {
		for _, mouse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/mouse=%t", name, mouse), func(t *testing.T) {
				screen := uiScreen(t, 80, 10)
				kh, ops := recorder()
				kh.state = pb.BrowserState{ActiveTabId: "tab", Generation: 7}
				kh.globalKey(key(tcell.KeyF10), false)
				found := false
				for i := 0; i < len(kh.menuLabels()); i++ {
					if strings.HasPrefix(kh.menuLabels()[kh.menuIndex], name) {
						found = true
						break
					}
					kh.HandleKeyEvent(screen, key(tcell.KeyDown))
				}
				if !found {
					t.Fatal("menu entry missing")
				}
				kh.Draw(screen)
				if mouse {
					rect := kh.menuRect()
					y := rect.Min.Y + 1 + kh.menuIndex - kh.menuStart()
					if y <= rect.Min.Y || y >= rect.Max.Y-1 {
						t.Fatal("entry is offscreen")
					}
					kh.HandleMouseEvent(screen, tcell.NewEventMouse(rect.Min.X+2, y, tcell.Button1, 0))
				} else {
					kh.HandleKeyEvent(screen, key(tcell.KeyEnter))
				}
				if kh.menu || len(*ops) != 1 {
					t.Fatal("menu did not dispatch and close")
				}
				op := (*ops)[0]
				switch name {
				case "Developer tools":
					if op.navigation == nil || op.navigation.Action != pb.NavigationAction_DEVTOOLS {
						t.Fatal("wrong tools action")
					}
				case "Extensions":
					if op.navigation == nil || op.navigation.Action != pb.NavigationAction_EXTENSIONS {
						t.Fatal("wrong extensions action")
					}
				case "Copy selected text":
					if op.selection == nil || op.selection.TabId != "tab" || op.selection.Generation != 7 {
						t.Fatal("copy lost selection target")
					}
				}
			})
		}
	}
}
