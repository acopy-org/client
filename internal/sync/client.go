package sync

import (
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/riz/acopy-client/internal/protocol"
)

const (
	pingInterval   = 30 * time.Second
	backoffInitial = 1 * time.Second
	backoffMax     = 30 * time.Second
	stableAfter    = 60 * time.Second
)

type Client struct {
	serverURL string
	token     string
	device    string
	codec     *protocol.Codec

	conn      *websocket.Conn
	connMu    sync.Mutex
	connAlive chan struct{} // closed when current connection dies

	// Pending holds the latest unsent clipboard push.
	// Only the most recent matters — older ones are stale.
	pendingMu sync.Mutex
	pending   *protocol.ClipboardPushPayload

	OnClipboard func(content []byte, device string)

	done chan struct{}
}

func NewClient(serverURL, token, device string) (*Client, error) {
	codec, err := protocol.NewCodec()
	if err != nil {
		return nil, err
	}
	return &Client{
		serverURL: serverURL,
		token:     token,
		device:    device,
		codec:     codec,
		done:      make(chan struct{}),
	}, nil
}

func (c *Client) Run() {
	backoff := backoffInitial
	for {
		select {
		case <-c.done:
			return
		default:
		}

		connectedAt := time.Now()
		err := c.connect()
		if err != nil {
			log.Printf("connect: %v", err)
		} else {
			c.flushPending()
			err = c.readLoop()
			if err != nil {
				log.Printf("connection lost: %v", err)
			}
		}

		select {
		case <-c.done:
			return
		default:
		}

		if time.Since(connectedAt) > stableAfter {
			backoff = backoffInitial
		}

		log.Printf("reconnecting in %v", backoff)
		time.Sleep(backoff)
		backoff = min(backoff*2, backoffMax)
	}
}

func (c *Client) Stop() {
	close(c.done)
	c.connMu.Lock()
	if c.conn != nil {
		c.conn.Close()
	}
	c.connMu.Unlock()
	c.codec.Close()
}

// Send sends a message. If disconnected, clipboard pushes are queued
// (only latest kept) and flushed on reconnect. Other messages are dropped.
func (c *Client) Send(msgType protocol.MsgType, payload any) error {
	c.connMu.Lock()
	connected := c.conn != nil
	c.connMu.Unlock()

	if !connected {
		if msgType == protocol.MsgClipboardPush {
			if p, ok := payload.(*protocol.ClipboardPushPayload); ok {
				c.pendingMu.Lock()
				c.pending = p
				c.pendingMu.Unlock()
				log.Printf("offline — queued clipboard for sync on reconnect")
				return nil
			}
		}
		return fmt.Errorf("not connected")
	}

	err := c.sendFrame(msgType, payload)
	if err != nil && msgType == protocol.MsgClipboardPush {
		if p, ok := payload.(*protocol.ClipboardPushPayload); ok {
			c.pendingMu.Lock()
			c.pending = p
			c.pendingMu.Unlock()
			log.Printf("send failed — queued clipboard for sync on reconnect")
		}
	}
	return err
}

func (c *Client) sendFrame(msgType protocol.MsgType, payload any) error {
	frame, err := c.codec.Encode(msgType, payload)
	if err != nil {
		return err
	}
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	return c.conn.WriteMessage(websocket.BinaryMessage, frame)
}

func (c *Client) connect() error {
	u, err := url.Parse(c.serverURL)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = "/ws"

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	alive := make(chan struct{})

	c.connMu.Lock()
	c.conn = conn
	c.connAlive = alive
	c.connMu.Unlock()

	// Authenticate and wait for Ack
	if err := c.sendFrame(protocol.MsgAuth, &protocol.AuthPayload{Token: c.token}); err != nil {
		c.closeConn()
		return fmt.Errorf("send auth: %w", err)
	}

	// Read auth response before proceeding
	_, frame, err := conn.ReadMessage()
	if err != nil {
		c.closeConn()
		return fmt.Errorf("read auth response: %w", err)
	}
	msgType, raw, err := c.codec.Decode(frame)
	if err != nil {
		c.closeConn()
		return fmt.Errorf("decode auth response: %w", err)
	}
	if msgType == protocol.MsgError {
		p, _ := protocol.DecodePayload[protocol.ErrorPayload](raw)
		c.closeConn()
		if p != nil {
			return fmt.Errorf("auth rejected: [%d] %s", p.Code, p.Msg)
		}
		return fmt.Errorf("auth rejected")
	}
	if msgType != protocol.MsgAck {
		c.closeConn()
		return fmt.Errorf("unexpected response to auth: message type %d", msgType)
	}

	log.Println("connected and authenticated")
	go c.pingLoop(alive)

	return nil
}

func (c *Client) closeConn() {
	c.connMu.Lock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	if c.connAlive != nil {
		select {
		case <-c.connAlive:
		default:
			close(c.connAlive)
		}
	}
	c.connMu.Unlock()
}

func (c *Client) flushPending() {
	c.pendingMu.Lock()
	p := c.pending
	c.pending = nil
	c.pendingMu.Unlock()

	if p == nil {
		return
	}

	log.Printf("flushing queued clipboard")
	if err := c.sendFrame(protocol.MsgClipboardPush, p); err != nil {
		log.Printf("flush failed: %v", err)
		c.pendingMu.Lock()
		c.pending = p
		c.pendingMu.Unlock()
	}
}

func (c *Client) readLoop() error {
	defer c.closeConn()

	for {
		_, frame, err := c.conn.ReadMessage()
		if err != nil {
			return err
		}

		msgType, raw, err := c.codec.Decode(frame)
		if err != nil {
			log.Printf("decode error: %v", err)
			continue
		}

		switch msgType {
		case protocol.MsgClipboardBroadcast:
			p, err := protocol.DecodePayload[protocol.ClipboardBroadcastPayload](raw)
			if err != nil {
				log.Printf("decode clipboard broadcast: %v", err)
				continue
			}
			if c.OnClipboard != nil {
				c.OnClipboard(p.Content, p.Device)
			}

		case protocol.MsgAck:
			// success

		case protocol.MsgError:
			p, err := protocol.DecodePayload[protocol.ErrorPayload](raw)
			if err != nil {
				log.Printf("decode error payload: %v", err)
				continue
			}
			log.Printf("server error: [%d] %s", p.Code, p.Msg)
			if p.Code == 401 {
				return fmt.Errorf("authentication failed: %s", p.Msg)
			}

		case protocol.MsgPong:
			// keepalive response

		case protocol.MsgPing:
			_ = c.sendFrame(protocol.MsgPong, nil)
		}
	}
}

func (c *Client) pingLoop(alive chan struct{}) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-alive:
			return
		case <-ticker.C:
			if err := c.sendFrame(protocol.MsgPing, nil); err != nil {
				return
			}
		}
	}
}
