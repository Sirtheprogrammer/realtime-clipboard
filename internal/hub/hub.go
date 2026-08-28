package hub

import (
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// remoteTTL is how long another instance's peer list is trusted without a
// refresh. Instances heartbeat well inside this, so a crashed one's devices
// fade from the sidebar instead of lingering forever.
const remoteTTL = 90 * time.Second

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

type Peer struct {
	ID     string `json:"id"`
	Device string `json:"device"`
}

type remoteEntry struct {
	peers []Peer
	seen  time.Time
}

type Hub struct {
	mu      sync.RWMutex
	rooms   map[string]map[*Client]struct{}
	remote  map[string]map[string]remoteEntry // room -> instance -> its peers
	handler Handler

	// onPresence lets the api publish a presence change to the other
	// instances. Nil in single-instance tests.
	onPresence func(room string)

	log *slog.Logger
}

func New(log *slog.Logger) *Hub {
	return &Hub{
		rooms:  make(map[string]map[*Client]struct{}),
		remote: make(map[string]map[string]remoteEntry),
		log:    log,
	}
}

func (h *Hub) SetHandler(handler Handler) { h.handler = handler }

// SetPresencePublisher installs the callback used to tell other instances that
// this one's occupancy changed.
func (h *Hub) SetPresencePublisher(fn func(room string)) { h.onPresence = fn }

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

// LocalPeers lists the devices connected to this instance.
func (h *Hub) LocalPeers(room string) []Peer {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.localPeersLocked(room)
}

func (h *Hub) localPeersLocked(room string) []Peer {
	peers := make([]Peer, 0, len(h.rooms[room]))
	for c := range h.rooms[room] {
		peers = append(peers, Peer{ID: c.ID, Device: c.Device})
	}
	return peers
}

// Peers lists every device in the room across all instances.
func (h *Hub) Peers(room string) []Peer {
	h.mu.RLock()
	defer h.mu.RUnlock()

	peers := h.localPeersLocked(room)
	cutoff := time.Now().Add(-remoteTTL)
	for _, entry := range h.remote[room] {
		if entry.seen.Before(cutoff) {
			continue
		}
		peers = append(peers, entry.peers...)
	}
	// Stable order so the sidebar does not reshuffle on every update.
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	return peers
}

// SetRemotePeers records another instance's occupancy of a room.
func (h *Hub) SetRemotePeers(room, instance string, peers []Peer) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(peers) == 0 {
		delete(h.remote[room], instance)
		if len(h.remote[room]) == 0 {
			delete(h.remote, room)
		}
		return
	}
	if h.remote[room] == nil {
		h.remote[room] = make(map[string]remoteEntry)
	}
	h.remote[room][instance] = remoteEntry{peers: peers, seen: time.Now()}
}

// RoomsWithLocalClients is what the presence heartbeat iterates over.
func (h *Hub) RoomsWithLocalClients() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	rooms := make([]string, 0, len(h.rooms))
	for room := range h.rooms {
		rooms = append(rooms, room)
	}
	return rooms
}

// DropStaleRemotes forgets instances that have stopped heartbeating and returns
// the rooms that changed, so their occupants can be told.
func (h *Hub) DropStaleRemotes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := time.Now().Add(-remoteTTL)
	var changed []string
	for room, instances := range h.remote {
		dropped := false
		for instance, entry := range instances {
			if entry.seen.Before(cutoff) {
				delete(instances, instance)
				dropped = true
			}
		}
		if len(instances) == 0 {
			delete(h.remote, room)
		}
		if dropped {
			changed = append(changed, room)
		}
	}
	return changed
}

// Broadcast fans a message out to everyone in a room on this instance. Pass an
// empty exceptID to include the sender.
func (h *Hub) Broadcast(room string, msgType string, payload any, exceptID string) {
	raw, err := encode(msgType, payload)
	if err != nil {
		h.log.Error("encode broadcast", "type", msgType, "err", err)
		return
	}
	h.BroadcastRaw(room, raw, exceptID)
}

// BroadcastRaw sends an already-encoded frame, which is how an event relayed
// from another instance avoids a re-encode.
func (h *Hub) BroadcastRaw(room string, raw []byte, exceptID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.rooms[room] {
		if c.ID == exceptID {
			continue
		}
		c.enqueue(raw)
	}
}

// BroadcastPresence tells a room's local clients who is currently connected,
// counting devices on every instance.
func (h *Hub) BroadcastPresence(room string) {
	h.Broadcast(room, "presence", map[string]any{"peers": h.Peers(room)}, "")
}

// PresenceChanged updates this room's occupants and lets the other instances
// know, which is what keeps the device list consistent across dynos.
func (h *Hub) PresenceChanged(room string) {
	h.BroadcastPresence(room)
	if h.onPresence != nil {
		h.onPresence(room)
	}
}

func encode(msgType string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{Type: msgType, Payload: body})
}
