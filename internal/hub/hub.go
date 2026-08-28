package hub

import (
	"encoding/json"
	"log/slog"
	"sync"
)

// Envelope is the wire format for every websocket frame in both directions.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Handler receives client frames. The api package implements it; keeping it an
// interface stops the hub from importing the store.
type Handler interface {
	HandleMessage(c *Client, env Envelope)
}

type Hub struct {
	mu      sync.RWMutex
	rooms   map[string]map[*Client]struct{}
	handler Handler
	log     *slog.Logger
}

func New(log *slog.Logger) *Hub {
	return &Hub{rooms: make(map[string]map[*Client]struct{}), log: log}
}

func (h *Hub) SetHandler(handler Handler) { h.handler = handler }

func (h *Hub) add(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[c.Room] == nil {
		h.rooms[c.Room] = make(map[*Client]struct{})
	}
	h.rooms[c.Room][c] = struct{}{}
}

func (h *Hub) remove(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room, ok := h.rooms[c.Room]
	if !ok {
		return
	}
	delete(room, c)
	if len(room) == 0 {
		delete(h.rooms, c.Room)
	}
}

// Peers lists the devices currently connected to a room.
func (h *Hub) Peers(room string) []Peer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	peers := make([]Peer, 0, len(h.rooms[room]))
	for c := range h.rooms[room] {
		peers = append(peers, Peer{ID: c.ID, Device: c.Device})
	}
	return peers
}

type Peer struct {
	ID     string `json:"id"`
	Device string `json:"device"`
}

// Broadcast fans a message out to everyone in a room. Pass an empty exceptID to
// include the sender.
func (h *Hub) Broadcast(room string, msgType string, payload any, exceptID string) {
	raw, err := encode(msgType, payload)
	if err != nil {
		h.log.Error("encode broadcast", "type", msgType, "err", err)
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.rooms[room] {
		if c.ID == exceptID {
			continue
		}
		c.enqueue(raw)
	}
}

// BroadcastPresence tells a room who is currently connected.
func (h *Hub) BroadcastPresence(room string) {
	h.Broadcast(room, "presence", map[string]any{"peers": h.Peers(room)}, "")
}

func encode(msgType string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{Type: msgType, Payload: body})
}
