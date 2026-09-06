package main

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	pb "termium/client/pb"
)

// Dialog state, like the rest of the UI, belongs exclusively to the event loop.
var currentDialog *Dialog
var dialogStream pb.BrowserControl_StreamDialogsClient
var dialogScreen tcell.Screen

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
		stream := dialogStream
		shutdownWg.Add(1)
		go func() {
			defer shutdownWg.Done()
			if err := stream.Send(response); err != nil {
				postUI(dialogScreen, stateUpdate{err: fmt.Errorf("dialog response: %w", err)})
			}
		}()
	}
	return true
}
