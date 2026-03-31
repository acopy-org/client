package monitor

import (
	"log"
	"sync"
	"time"

	"github.com/riz/acopy-client/internal/clipboard"
	"github.com/riz/acopy-client/internal/protocol"
	acSync "github.com/riz/acopy-client/internal/sync"
)

const pollInterval = 500 * time.Millisecond

type Monitor struct {
	client *acSync.Client
	device string

	mu            sync.Mutex
	lastCount     int64
	lastWasRemote bool

	done chan struct{}
}

func New(client *acSync.Client, device string) *Monitor {
	m := &Monitor{
		client: client,
		device: device,
		done:   make(chan struct{}),
	}
	client.OnClipboard = m.onRemoteClipboard
	return m
}

func (m *Monitor) Run() {
	m.mu.Lock()
	m.lastCount = clipboard.ChangeCount()
	m.mu.Unlock()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.poll()
		}
	}
}

func (m *Monitor) Stop() {
	close(m.done)
}

func (m *Monitor) poll() {
	count := clipboard.ChangeCount()

	m.mu.Lock()
	if count == m.lastCount {
		m.mu.Unlock()
		return
	}
	m.lastCount = count

	if m.lastWasRemote {
		m.lastWasRemote = false
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()

	content, err := clipboard.Read()
	if err != nil {
		log.Printf("clipboard read: %v", err)
		return
	}

	if len(content) == 0 {
		return
	}

	if len(content) > protocol.MaxPayloadSize {
		log.Printf("clipboard content too large (%d bytes), skipping", len(content))
		return
	}

	err = m.client.Send(protocol.MsgClipboardPush, &protocol.ClipboardPushPayload{
		Content: content,
		Device:  m.device,
	})
	if err != nil {
		log.Printf("push clipboard: %v", err)
	}
}

func (m *Monitor) onRemoteClipboard(content []byte, device string) {
	if err := clipboard.Write(content); err != nil {
		log.Printf("clipboard write: %v", err)
		return
	}

	m.mu.Lock()
	m.lastWasRemote = true
	m.lastCount = clipboard.ChangeCount()
	m.mu.Unlock()

	log.Printf("clipboard updated from %s", device)
}
