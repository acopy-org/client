package monitor

import (
	"crypto/sha256"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/riz/acopy-client/internal/clipboard"
	"github.com/riz/acopy-client/internal/imgcomp"
	"github.com/riz/acopy-client/internal/protocol"
	acSync "github.com/riz/acopy-client/internal/sync"
)

const pollInterval = 500 * time.Millisecond

type Monitor struct {
	client    *acSync.Client
	device    string
	serverURL string
	Debug     bool

	mu            sync.Mutex
	lastCount     int64
	lastWasRemote bool
	pushing       bool             // prevent concurrent pushes of same content
	lastPushHash  [sha256.Size]byte // dedup repeated pushes of same content
	lastPushTime  time.Time        // quiet period after push to let clipboard settle

	done chan struct{}
}

func (m *Monitor) debugf(format string, args ...any) {
	if m.Debug {
		log.Printf("[debug] "+format, args...)
	}
}

func New(client *acSync.Client, device string, serverURL string) *Monitor {
	m := &Monitor{
		client:    client,
		device:    device,
		serverURL: serverURL,
		done:      make(chan struct{}),
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
	m.mu.Lock()
	if m.pushing {
		m.mu.Unlock()
		return
	}

	count := clipboard.ChangeCount()
	if count == m.lastCount {
		m.mu.Unlock()
		return
	}
	m.debugf("clipboard change detected (count %d -> %d)", m.lastCount, count)
	m.lastCount = count

	if m.lastWasRemote {
		m.lastWasRemote = false
		m.debugf("skipping — was remote write")
		m.mu.Unlock()
		return
	}

	// Quiet period after push: let clipboard settle (macOS type population,
	// OSC 52 round-trips via terminal emulator, etc.)
	if !m.lastPushTime.IsZero() && time.Since(m.lastPushTime) < 2*time.Second {
		m.debugf("skipping — quiet period (%v since last push)", time.Since(m.lastPushTime))
		m.mu.Unlock()
		return
	}

	m.pushing = true
	m.mu.Unlock()

	readStart := time.Now()
	content, contentType, err := clipboard.Read()

	// Re-snapshot change count after Read(), because on macOS reading
	// image data via AppleScript can mutate clipboard state.
	m.mu.Lock()
	m.lastCount = clipboard.ChangeCount()
	m.mu.Unlock()

	m.debugf("clipboard read: %d bytes, type=%s, took %v", len(content), contentType, time.Since(readStart))

	if err != nil {
		log.Printf("clipboard read: %v", err)
		m.mu.Lock()
		m.pushing = false
		m.mu.Unlock()
		return
	}

	if len(content) == 0 {
		m.debugf("skipping — empty clipboard")
		m.mu.Lock()
		m.pushing = false
		m.mu.Unlock()
		return
	}

	if len(content) > protocol.MaxPayloadSize {
		log.Printf("clipboard content too large (%d bytes), skipping", len(content))
		m.mu.Lock()
		m.pushing = false
		m.mu.Unlock()
		return
	}

	// Deduplicate: skip if content is identical to last push
	hash := sha256.Sum256(content)
	m.mu.Lock()
	if hash == m.lastPushHash {
		m.pushing = false
		m.debugf("skipping — duplicate content (hash match)")
		m.mu.Unlock()
		return
	}
	m.lastPushHash = hash
	m.mu.Unlock()

	// Compress large images
	if contentType == "image/png" && len(content) > imgcomp.Threshold {
		compressStart := time.Now()
		if compressed, ct, err := imgcomp.CompressImage(content); err != nil {
			log.Printf("image compress: %v (sending original)", err)
		} else if compressed != nil {
			m.debugf("compressed %s %d -> %s %d bytes in %v", contentType, len(content), ct, len(compressed), time.Since(compressStart))
			content = compressed
			contentType = ct
		}
	}

	err = m.client.Send(protocol.MsgClipboardPush, &protocol.ClipboardPushPayload{
		Content:     content,
		Device:      m.device,
		ContentType: contentType,
	})
	m.mu.Lock()
	m.pushing = false
	if err == nil {
		m.lastPushTime = time.Now()
	}
	m.mu.Unlock()
	if err != nil {
		log.Printf("push clipboard: %v", err)
	} else {
		log.Printf("pushed clipboard (%d bytes, %s)", len(content), contentType)
		m.debugf("ws connected=%v", m.client.IsConnected())
	}
}

func (m *Monitor) onRemoteClipboard(content []byte, device string, contentType string, id string) {
	// Ignore echoes of our own pushes (server should exclude sender, but be safe)
	if device == m.device {
		return
	}

	var clipURL string
	if strings.HasPrefix(contentType, "image/") && id != "" {
		ext := ".png"
		if contentType == "image/jpeg" {
			ext = ".jpg"
		} else if contentType == "image/gif" {
			ext = ".gif"
		} else if contentType == "image/webp" {
			ext = ".webp"
		}
		clipURL = strings.TrimRight(m.serverURL, "/") + "/c/" + id + ext
	}

	if err := clipboard.Write(content, contentType, clipURL); err != nil {
		log.Printf("clipboard write: %v", err)
		return
	}

	m.mu.Lock()
	m.lastWasRemote = true
	m.lastCount = clipboard.ChangeCount()
	// Mark the written content as "already pushed" so we don't echo it back.
	// For images with a URL, the clipboard gets the URL text, not the raw image.
	if clipURL != "" {
		m.lastPushHash = sha256.Sum256([]byte(clipURL))
	} else {
		m.lastPushHash = sha256.Sum256(content)
	}
	m.mu.Unlock()

	log.Printf("clipboard updated from %s", device)
}

