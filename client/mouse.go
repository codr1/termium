package main

import (
	"github.com/gdamore/tcell/v2"
	"image"
	pb "termium/client/pb"
	"time"
)

func browserButtons(buttons tcell.ButtonMask) uint32 {
	var result uint32
	if buttons&tcell.Button1 != 0 {
		result |= 1
	}
	if buttons&tcell.Button2 != 0 {
		result |= 2
	}
	if buttons&tcell.Button3 != 0 {
		result |= 4
	}
	return result
}
func (kh *KeyboardHandler) HandleMouseEvent(s tcell.Screen, ev *tcell.EventMouse) {
	x, y := ev.Position()
	cell := image.Pt(x, y)
	buttons := ev.Buttons() & (tcell.Button1 | tcell.Button2 | tcell.Button3 | tcell.Button4 | tcell.Button5)
	wheel := ev.Buttons() & (tcell.WheelUp | tcell.WheelDown | tcell.WheelLeft | tcell.WheelRight)
	pressed := buttons &^ kh.mouseButtons
	if wheel == 0 {
		kh.mouseButtons = buttons
	}
	if kh.captureUI {
		if buttons == 0 && wheel == 0 {
			kh.captureUI = false
		}
		return
	}
	if kh.quitConfirm {
		if pressed&tcell.Button1 != 0 {
			quit, stay := quitButtons(s.Size())
			if cell.In(quit) {
				kh.exitRequested = true
			}
			if cell.In(stay) {
				kh.quitConfirm = false
			}
		}
		return
	}
	if kh.help {
		if pressed&tcell.Button1 != 0 {
			kh.help = false
		}
		return
	}
	if kh.menu {
		rect := kh.menuRect()
		index := y - rect.Min.Y - 1 + kh.menuStart()
		if cell.In(rect) && y > rect.Min.Y && y < rect.Max.Y-1 && index >= 0 && index < len(menuIDs) {
			kh.menuIndex = index
			if pressed&tcell.Button1 != 0 {
				kh.action(menuIDs[index])
			}
		} else if pressed&tcell.Button1 != 0 {
			kh.menu = false
		}
		return
	}

	if !kh.capturePage && y == 0 {
		if pressed&tcell.Button1 != 0 {
			kh.captureUI = true
			width, _ := s.Size()
			for _, control := range kh.controls(width) {
				if cell.In(control.rect) && control.enabled {
					kh.action(control.id)
					break
				}
			}
		}
		return
	}
	rect := viewportRect(sDims.Width, sDims.Height)
	point, inside := pagePoint(rect, cell, kh.capturePage)
	if !inside {
		return
	}
	if pressed&tcell.Button4 != 0 {
		kh.action("back")
		return
	}
	if pressed&tcell.Button5 != 0 {
		kh.action("forward")
		return
	}
	if wheel != 0 {
		dx, dy := int32(0), int32(0)
		step := int32(max(1, charSize.Height) * 3)
		if wheel&tcell.WheelUp != 0 {
			dy = -step
		}
		if wheel&tcell.WheelDown != 0 {
			dy = step
		}
		if wheel&tcell.WheelLeft != 0 {
			dx = -step
		}
		if wheel&tcell.WheelRight != 0 {
			dx = step
		}
		if ev.Modifiers()&tcell.ModShift != 0 && dx == 0 {
			dx = dy
			dy = 0
		}
		kh.input(&pb.InputEvent{Kind: pb.InputKind_WHEEL_INPUT, X: int32(point.X), Y: int32(point.Y), DeltaX: dx, DeltaY: dy, Modifiers: inputModifiers(ev.Modifiers())})
		return
	}
	if pressed&(tcell.Button1|tcell.Button2|tcell.Button3) != 0 {
		kh.focus = "page"
		kh.capturePage = true
		now := ev.When()
		if now.Sub(kh.lastClick) < 400*time.Millisecond && kh.lastClickAt == cell && kh.lastClickButton == pressed {
			kh.clickCount = kh.clickCount%3 + 1
		} else {
			kh.clickCount = 1
		}
		kh.lastClick = now
		kh.lastClickAt = cell
		kh.lastClickButton = pressed
	}
	count := uint32(0)
	if pressed != 0 || kh.capturePage && buttons == 0 {
		count = max(1, kh.clickCount)
	}
	kh.input(&pb.InputEvent{Kind: pb.InputKind_POINTER_INPUT, X: int32(point.X), Y: int32(point.Y), Buttons: browserButtons(buttons), Modifiers: inputModifiers(ev.Modifiers()), ClickCount: count})
	kh.pointer = cell
	if buttons == 0 {
		kh.capturePage = false
	}
}
func (kh *KeyboardHandler) pointerKey(ev *tcell.EventKey) {
	rect := viewportRect(sDims.Width, sDims.Height)
	if rect.Empty() {
		return
	}
	delta := image.Point{}
	step := 1
	if ev.Modifiers()&tcell.ModShift != 0 {
		step = 5
	}
	switch ev.Key() {
	case tcell.KeyEscape:
		kh.action("pointer")
		return
	case tcell.KeyLeft:
		delta.X = -step
	case tcell.KeyRight:
		delta.X = step
	case tcell.KeyUp:
		delta.Y = -step
	case tcell.KeyDown:
		delta.Y = step
	case tcell.KeyEnter:
		kh.pointerClick(1)
		return
	case tcell.KeyPgUp:
		kh.pointerWheel(-rect.Dy() / 2)
		return
	case tcell.KeyPgDn:
		kh.pointerWheel(rect.Dy() / 2)
		return
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'h':
			delta.X = -step
		case 'l':
			delta.X = step
		case 'k':
			delta.Y = -step
		case 'j':
			delta.Y = step
		case ' ':
			kh.pointerHeld ^= 1
		case 'r':
			kh.pointerClick(2)
			return
		case 'm':
			kh.pointerClick(4)
			return
		case 'u':
			kh.pointerWheel(-3)
			return
		case 'd':
			kh.pointerWheel(3)
			return
		}
	}
	kh.pointer.X = max(rect.Min.X, min(rect.Max.X-1, kh.pointer.X+delta.X))
	kh.pointer.Y = max(rect.Min.Y, min(rect.Max.Y-1, kh.pointer.Y+delta.Y))
	point, _ := pagePoint(rect, kh.pointer, true)
	count := uint32(0)
	if ev.Rune() == ' ' {
		count = 1
	}
	kh.input(&pb.InputEvent{Kind: pb.InputKind_POINTER_INPUT, X: int32(point.X), Y: int32(point.Y), Buttons: kh.pointerHeld, ClickCount: count})
}
func (kh *KeyboardHandler) pointerClick(button uint32) {
	point, ok := pagePoint(viewportRect(sDims.Width, sDims.Height), kh.pointer, true)
	if !ok {
		return
	}
	kh.input(&pb.InputEvent{Kind: pb.InputKind_POINTER_INPUT, X: int32(point.X), Y: int32(point.Y), Buttons: button, ClickCount: 1})
	kh.input(&pb.InputEvent{Kind: pb.InputKind_POINTER_INPUT, X: int32(point.X), Y: int32(point.Y), Buttons: 0, ClickCount: 1})
	kh.pointerHeld = 0
}
func (kh *KeyboardHandler) pointerWheel(rows int) {
	point, ok := pagePoint(viewportRect(sDims.Width, sDims.Height), kh.pointer, true)
	if !ok {
		return
	}
	kh.input(&pb.InputEvent{Kind: pb.InputKind_WHEEL_INPUT, X: int32(point.X), Y: int32(point.Y), DeltaY: int32(rows * charSize.Height)})
}
