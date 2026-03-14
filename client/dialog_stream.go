package main

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/gdamore/tcell/v2"
	pb "termium/client/pb"
)

var (
	currentDialog *Dialog
	dialogLock    sync.Mutex
	dialogStream  pb.BrowserControl_StreamDialogsClient
	dialogScreen  tcell.Screen // Set by main when screen is ready
)

// startDialogStream establishes the bidirectional stream for dialogs
func startDialogStream(client pb.BrowserControlClient) error {
	stream, err := client.StreamDialogs(context.Background())
	if err != nil {
		return fmt.Errorf("failed to start dialog stream: %v", err)
	}
	
	dialogStream = stream
	
	// Start goroutine to receive dialog events from server
	go receiveDialogs(stream)
	
	Debug("Dialog stream connected", INFO)
	return nil
}

// receiveDialogs handles incoming dialog events from the server
func receiveDialogs(stream pb.BrowserControl_StreamDialogsClient) {
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			Debug("Dialog stream closed by server", INFO)
			return
		}
		if err != nil {
			Debug(fmt.Sprintf("Error receiving dialog: %v", err), ERROR)
			return
		}
		
		// Create and show the dialog
		dialogLock.Lock()
		currentDialog = NewDialog(event)
		dialogLock.Unlock()
		
		Debug(fmt.Sprintf("Dialog received: type=%v, message=%s", event.Type, event.Message), INFO)
		
		// Force screen refresh to show dialog
		if dialogScreen != nil {
			dialogScreen.PostEvent(tcell.NewEventResize(0, 0))
		}
	}
}

// sendDialogResponse sends the user's response back to the server
func sendDialogResponse(response *pb.DialogResponse) error {
	if dialogStream == nil {
		return fmt.Errorf("dialog stream not connected")
	}
	
	err := dialogStream.Send(response)
	if err != nil {
		return fmt.Errorf("failed to send dialog response: %v", err)
	}
	
	Debug(fmt.Sprintf("Dialog response sent: id=%s, accepted=%v", response.Id, response.Accepted), DEBUG)
	return nil
}

// Local dialog callback for non-server dialogs (like exit confirmation)
var localDialogCallback func(*pb.DialogResponse)

// showLocalDialog shows a dialog that doesn't involve the server
func showLocalDialog(dialogType pb.DialogType, message string, callback func(*pb.DialogResponse)) {
	event := &pb.DialogEvent{
		Id:      "local_dialog",
		Type:    dialogType,
		Message: message,
	}
	
	dialogLock.Lock()
	currentDialog = NewDialog(event)
	localDialogCallback = callback
	dialogLock.Unlock()
	
	// Force screen refresh
	if dialogScreen != nil {
		dialogScreen.PostEvent(tcell.NewEventResize(0, 0))
	}
}

// handleDialogInput processes keyboard/mouse input when a dialog is active
func handleDialogInput(ev tcell.Event) bool {
	dialogLock.Lock()
	defer dialogLock.Unlock()
	
	if currentDialog == nil || !currentDialog.Active {
		return false
	}
	
	var response *pb.DialogResponse
	
	switch ev := ev.(type) {
	case *tcell.EventKey:
		response = currentDialog.HandleKey(ev)
	case *tcell.EventMouse:
		// Check if click is within dialog bounds
		x, y := ev.Position()
		if currentDialog.IsPointInDialog(x, y) {
			response = currentDialog.HandleMouse(ev)
			return true // Consume the event
		}
		return true // Still consume clicks outside dialog (modal behavior)
	}
	
	// If we got a response, handle it
	if response != nil {
		currentDialog = nil // Clear dialog
		
		// Check if this is a local dialog
		if response.Id == "local_dialog" && localDialogCallback != nil {
			callback := localDialogCallback
			localDialogCallback = nil
			go callback(response)
		} else {
			// Server dialog - send response
			go func() {
				if err := sendDialogResponse(response); err != nil {
					Debug(fmt.Sprintf("Failed to send dialog response: %v", err), ERROR)
				}
			}()
		}
		
		// Force screen refresh to clear dialog
		if dialogScreen != nil {
			dialogScreen.PostEvent(tcell.NewEventResize(0, 0))
		}
	}
	
	return true // Dialog consumed the event
}