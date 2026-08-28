package hub

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 25 * time.Second
	maxMessageSize = 1 << 20 // 1 MiB: text pastes travel over the socket
	sendBuffer     = 32
)

// Client is one browser tab attached to a room.
type Client struct {
	ID     string
	Room   string
	Device string

	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	done chan struct{}
	once sync.Once
}

// Serve registers the client, starts its pumps and blocks until it disconnects.
func (h *Hub) Serve(conn *websocket.Conn, id, room, device string) *Client {
	c := &Client{
		ID:     id,
		Room:   room,
		Device: device,
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, sendBuffer),
		done:   make(chan struct{}),
	}
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
func (c *Client) Wait() { <-c.done }

// enqueue drops the message rather than blocking the broadcaster when a slow
// client has filled its buffer; that client is then closed.
func (c *Client) enqueue(raw []byte) {
	select {
	case c.send <- raw:
	default:
		c.hub.log.Warn("dropping slow client", "client", c.ID, "room", c.Room)
		c.close()
	}
}

func (c *Client) close() {
	c.once.Do(func() {
		c.hub.remove(c)
		close(c.done)
		c.conn.Close()
	})
}

func (c *Client) readPump() {
	defer func() {
		c.close()
		c.hub.BroadcastPresence(c.Room)
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			return
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
		c.close()
	}()

	for {
		select {
		case raw, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, raw); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}
