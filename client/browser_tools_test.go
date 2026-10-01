package main

import (
	"bytes"
	"encoding/base64"
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
