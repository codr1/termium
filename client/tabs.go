package main

import (
	"fmt"
	"image"
	"strings"
	pb "termium/client/pb"
)

func (kh *KeyboardHandler) tabControls(width int) []navControl {
	if width < 1 {
		return nil
	}
	if width < 16 {
		return []navControl{{"tabs", "[Tabs]", image.Rect(0, 0, width, 1), true}}
	}
	available := width - 9
	count := min(len(kh.state.Tabs), max(1, available/16))
	active := 0
	for i, t := range kh.state.Tabs {
		if t.Id == kh.state.ActiveTabId {
			active = i
		}
	}
	start := max(0, min(active-count+1, len(kh.state.Tabs)-count))
	result := []navControl{}
	if count > 0 {
		size := min(26, available/count)
		for i := 0; i < count; i++ {
			t := kh.state.Tabs[start+i]
			x := i * size
			title := t.Title
			if title == "" {
				title = "New tab"
			}
			result = append(result, navControl{"tab:" + t.Id, fmt.Sprintf("%d %s", start+i+1, title), image.Rect(x, 0, x+size-3, 1), true},
				navControl{"close:" + t.Id, " × ", image.Rect(x+size-3, 0, x+size, 1), true})
		}
	}
	result = append(result, navControl{"tabs", "[≡]", image.Rect(width-8, 0, width-5, 1), true},
		navControl{"newtab", "[+]", image.Rect(width-3, 0, width, 1), true})
	return result
}

func (kh *KeyboardHandler) menuActions() []string {
	if !kh.tabsMenu {
		return menuIDs
	}
	ids := []string{}
	for _, t := range kh.state.Tabs {
		ids = append(ids, "tab:"+t.Id)
	}
	return append(ids, "newtab", "reopentab")
}

func (kh *KeyboardHandler) tabAction(id string) bool {
	if id == "tabs" {
		kh.tabsMenu = true
		kh.menu = true
		kh.help = false
		kh.menuIndex = 0
		kh.focus = "page"
		return true
	}
	action := pb.NavigationAction_NEW_TAB
	target := kh.state.ActiveTabId
	switch {
	case id == "newtab":
	case id == "closetab":
		action = pb.NavigationAction_CLOSE_TAB
	case id == "reopentab":
		action = pb.NavigationAction_REOPEN_TAB
	case strings.HasPrefix(id, "tab:"):
		action = pb.NavigationAction_SELECT_TAB
		target = strings.TrimPrefix(id, "tab:")
	case strings.HasPrefix(id, "close:"):
		action = pb.NavigationAction_CLOSE_TAB
		target = strings.TrimPrefix(id, "close:")
	default:
		return false
	}
	kh.input(&pb.InputEvent{Kind: pb.InputKind_RESET_INPUT})
	kh.pointerHeld = 0
	kh.capturePage = false
	kh.queue(browserOperation{navigation: &pb.NavigationRequest{Action: action, TabId: target, Generation: kh.state.Generation}})
	kh.menu = false
	kh.help = false
	kh.focus = "page"
	return true
}
