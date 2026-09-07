package main

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"sync"
	pb "termium/client/pb"
)

// Dialog state, like the rest of the UI, belongs exclusively to the event loop.
var currentDialog *Dialog
var pendingUIDialogs []*pb.DialogEvent
var dialogStream pb.BrowserControl_StreamDialogsClient
var dialogScreen tcell.Screen
var dialogSendMu sync.Mutex

func receiveDialog(event *pb.DialogEvent) {
	if currentDialog != nil {
		pendingUIDialogs = append(pendingUIDialogs, event)
		return
	}
	currentDialog = NewDialog(event)
	for i, tab := range keyboardHandler.state.Tabs {
		if tab.Id == event.TabId {
			currentDialog.Source = fmt.Sprintf(" · Tab %d: %s", i+1, tab.Title)
			break
		}
	}
	currentDialog.mouseDown = keyboardHandler.mouseButtons&tcell.Button1 != 0
}

func startDialogStream(client pb.BrowserControlClient) error {
	stream, err := client.StreamDialogs(appCtx)
	if err != nil {
		return err
	}
	dialogStream = stream
	shutdownWg.Add(1)
	go func() {
		defer shutdownWg.Done()
		for {
			event, err := stream.Recv()
			if err != nil {
				if appCtx.Err() == nil {
					postUI(dialogScreen, stateUpdate{err: fmt.Errorf("dialog connection: %w", err)})
				}
				return
			}
			postUI(dialogScreen, event)
		}
	}()
	return nil
}
func handleDialogInput(ev tcell.Event) bool {
	if currentDialog == nil || !currentDialog.Active {
		return false
	}
	var response *pb.DialogResponse
	switch ev := ev.(type) {
	case *tcell.EventKey:
		response = currentDialog.HandleKey(ev)
	case *tcell.EventMouse:
		response = currentDialog.HandleMouse(ev)
	default:
		return false
	}
	if response != nil {
		currentDialog = nil
		if len(pendingUIDialogs) > 0 {
			next := pendingUIDialogs[0]
			pendingUIDialogs = pendingUIDialogs[1:]
			receiveDialog(next)
		}
		stream := dialogStream
		shutdownWg.Add(1)
		go func() {
			defer shutdownWg.Done()
			dialogSendMu.Lock()
			defer dialogSendMu.Unlock()
			if err := stream.Send(response); err != nil {
				postUI(dialogScreen, stateUpdate{err: fmt.Errorf("dialog response: %w", err)})
			}
		}()
	}
	return true
}
