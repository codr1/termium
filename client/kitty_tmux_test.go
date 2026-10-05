package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestKittyTmuxFramingAndPlaceholders(t *testing.T) {
	wrapped := wrapTmuxPassthrough([]byte("\033_Gq=2;abc\033\\"))
	if string(wrapped) != "\033Ptmux;\033\033_Gq=2;abc\033\033\\\033\\" {
		t.Fatalf("bad tmux passthrough framing: %q", wrapped)
	}

	previousSession, previousSize := kittySession, charSize
	t.Cleanup(func() { kittySession, charSize = previousSession, previousSize })
	kittySession = kittySessionState{tmux: true, imageID: 0x123456}
	charSize = CharSize{Width: 8, Height: 16}
	encoder := kittyEncoder{tmux: true, imageID: kittySession.imageID, cell: charSize}
	payload, err := encodeKittyPayloadInto([]byte("png"), "f=100,"+encoder.placement(16, 32), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := writeKittyFrame(&out, payload, 3, 2, 16, 32); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "a=T,i=1193046,p=1,C=1,q=2,U=1,c=2,r=2") ||
		!strings.Contains(text, "\033[38;2;18;52;86m") || strings.Count(text, string(kittyPlaceholder)) != 4 ||
		!strings.HasPrefix(text, "\033[s"+tmuxPassthroughPrefix) || !strings.HasSuffix(text, "\033[39m\033[u") {
		t.Fatalf("bad tmux Kitty frame: %q", text)
	}

	out.Reset()
	if err := writeKittyFrame(&out, payload, 3, 2, 16, 32); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(out.String(), kittyPlaceholder) {
		t.Fatal("unchanged placeholder grid was redrawn")
	}
	if got := string(kittyDeleteSequence()); !strings.Contains(got, "a=d,d=I,i=1193046") || !strings.HasPrefix(got, tmuxPassthroughPrefix) {
		t.Fatalf("bad tmux Kitty delete: %q", got)
	}
}

func TestKittyCellsAndImageID(t *testing.T) {
	columns, rows := kittyCells(17, 33, CharSize{Width: 8, Height: 16})
	if columns != 3 || rows != 3 {
		t.Fatalf("got %dx%d cells", columns, rows)
	}
	id, err := randomKittyImageID(bytes.NewReader([]byte{0, 0, 0, 0x12, 0x34, 0x56}))
	if err != nil || id != 0x123456 {
		t.Fatalf("got image ID %#x: %v", id, err)
	}
}
