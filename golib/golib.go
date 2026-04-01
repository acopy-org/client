// Package golib exposes acopy's sync/protocol layer to Android via gomobile.
// gomobile bind compiles this into an AAR that the Kotlin app consumes.
package golib

import (
	"log"

	"github.com/riz/acopy-client/internal/protocol"
	acSync "github.com/riz/acopy-client/internal/sync"
)

// Callback is implemented by the Android layer to receive events.
type Callback interface {
	OnClipboardReceived(content []byte, device string)
	OnConnectionStateChanged(connected bool)
	OnError(msg string)
}

// Bridge wraps the sync client for Android consumption.
type Bridge struct {
	client *acSync.Client
	cb     Callback
}

// NewBridge creates a Bridge connected to the given server.
func NewBridge(serverURL, token, deviceName string, cb Callback) (*Bridge, error) {
	client, err := acSync.NewClient(serverURL, token, deviceName)
	if err != nil {
		return nil, err
	}

	b := &Bridge{client: client, cb: cb}

	client.OnClipboard = func(content []byte, device string) {
		cb.OnClipboardReceived(content, device)
	}
	client.OnConnectionState = func(connected bool) {
		cb.OnConnectionStateChanged(connected)
	}

	return b, nil
}

// Start begins the WebSocket connection loop in a background goroutine.
func (b *Bridge) Start() {
	go b.client.Run()
}

// Stop shuts down the connection and releases resources.
func (b *Bridge) Stop() {
	b.client.Stop()
}

// PushClipboard sends clipboard content to the server for broadcast.
func (b *Bridge) PushClipboard(content []byte, device string) {
	err := b.client.Send(protocol.MsgClipboardPush, &protocol.ClipboardPushPayload{
		Content: content,
		Device:  device,
	})
	if err != nil {
		log.Printf("push clipboard: %v", err)
		b.cb.OnError("push failed: " + err.Error())
	}
}

// IsConnected returns whether the WebSocket is currently connected.
func (b *Bridge) IsConnected() bool {
	return b.client.IsConnected()
}

// Reconnect forces the current connection to close, triggering an immediate
// reconnection attempt. Useful when Android detects network restoration.
func (b *Bridge) Reconnect() {
	b.client.ForceReconnect()
}
