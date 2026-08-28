package hub

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	writeWait      = 10 * time.Second
	pingPeriod     = 25 * time.Second
	pingWait       = 15 * time.Second
	maxMessageSize = 1 << 20 // 1 MiB: pasted text travels over the socket
	sendBuffer     = 32
)

// Client is one browser tab attached to a room.
type Client struct {
	ID     string
	Room   string
	Device string

	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

// Serve registers a freshly accepted connection and starts its read/write
// pumps. Call Wait to block until the client goes away.
func (h *Hub) Serve(ctx context.Context, conn *websocket.Conn, id, room, device string) *Client {
	ctx, cancel := context.WithCancel(ctx)
	c := &Client{
		ID:     id,
		Room:   room,
		Device: device,
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, sendBuffer),
		ctx:    ctx,
		cancel: cancel,
	}
	conn.SetReadLimit(maxMessageSize)
	h.add(c)
	go c.writePump()
	go c.readPump()
	return c
}

// Send delivers a message to this client only.
func (c *Client) Send(msgType string, payload any) {
	raw, err := encode(msgType, payload)
	if err != nil {
		c.hub.log.Error("encode message", "type", msgType, "err", err)
		return
	}
	c.enqueue(raw)
}

// Wait blocks until the client's connection is torn down.
func (c *Client) Wait() { <-c.ctx.Done() }

// enqueue drops a client that has stopped draining its buffer rather than
// letting it stall the broadcaster.
func (c *Client) enqueue(raw []byte) {
	select {
	case c.send <- raw:
	case <-c.ctx.Done():
	default:
		c.hub.log.Warn("dropping slow client", "client", c.ID, "room", c.Room)
		c.close(websocket.StatusPolicyViolation, "client too slow")
	}
}

func (c *Client) close(status websocket.StatusCode, reason string) {
	c.once.Do(func() {
		c.hub.remove(c)
		c.cancel()
		_ = c.conn.Close(status, reason)
	})
}

func (c *Client) readPump() {
	defer func() {
		c.close(websocket.StatusNormalClosure, "")
		c.hub.BroadcastPresence(c.Room)
	}()

	for {
		typ, raw, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var env Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			c.Send("error", map[string]string{"message": "malformed frame"})
			continue
		}
		if env.Type == "ping" {
			c.Send("pong", map[string]any{"t": time.Now().UnixMilli()})
			continue
		}
		if c.hub.handler != nil {
			c.hub.handler.HandleMessage(c, env)
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.close(websocket.StatusNormalClosure, "")
	}()

	for {
		select {
		case raw := <-c.send:
			if err := c.write(raw); err != nil {
				return
			}
		case <-ticker.C:
			// A failed ping is how we notice a laptop that slept or a phone
			// that dropped off wifi without closing the socket.
			ctx, cancel := context.WithTimeout(c.ctx, pingWait)
			err := c.conn.Ping(ctx)
			cancel()
			if err != nil {
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Client) write(raw []byte) error {
	ctx, cancel := context.WithTimeout(c.ctx, writeWait)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, raw)
}
