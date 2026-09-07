package api

import (
	"context"
	"encoding/json"
	"time"

	"clipboard/internal/events"
	"clipboard/internal/hub"
	"clipboard/internal/models"
)

// presenceHeartbeatInterval must stay comfortably under hub's remoteTTL so a
// healthy instance never looks stale to its siblings.
const presenceHeartbeatInterval = 30 * time.Second

/* ── outbound: tell our own clients, then the other instances ──────────── */

func (s *Server) fanoutItemCreated(ctx context.Context, item models.Item) {
	s.hub.Broadcast(item.RoomCode, "item.created", item, "")
	if s.bus != nil {
		// Only the id travels: a pasted item can be far larger than a NOTIFY
		// payload allows, so the other instances load it from the database.
		s.bus.Publish(ctx, events.Event{
			Room:   item.RoomCode,
			Type:   "item.created",
			ItemID: item.ID,
		})
	}
}

func (s *Server) fanoutItemDeleted(ctx context.Context, room, id string) {
	payload := map[string]string{"id": id}
	s.hub.Broadcast(room, "item.deleted", payload, "")
	if s.bus != nil {
		s.bus.Publish(ctx, events.Event{Room: room, Type: "item.deleted", Payload: mustJSON(payload)})
	}
}

func (s *Server) fanoutRoomCleared(ctx context.Context, room string) {
	s.hub.Broadcast(room, "room.cleared", map[string]any{}, "")
	if s.bus != nil {
		s.bus.Publish(ctx, events.Event{Room: room, Type: "room.cleared"})
	}
}

func (s *Server) publishPresence(ctx context.Context, room string) {
	if s.bus != nil {
		peers := s.hub.LocalPeers(room)
		s.bus.Publish(ctx, events.Event{
			Room:    room,
			Type:    "presence",
			Payload: mustJSON(map[string]any{"peers": peers}),
		})
	}
}

/* ── inbound: relay another instance's event to our clients ────────────── */

func (s *Server) handleRemoteEvent(e events.Event) {
	switch e.Type {
	case "item.created":
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		item, err := s.store.GetItem(ctx, e.ItemID)
		if err != nil {
			// Already deleted, or the row has not landed yet. Either way the
			// next reconnect replays the room, so this is not fatal.
			s.log.Debug("load remote item", "id", e.ItemID, "err", err)
			return
		}
		s.hub.Broadcast(item.RoomCode, "item.created", item, e.Origin)

	case "item.deleted":
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			s.log.Error("decode remote delete", "err", err)
			return
		}
		s.hub.Broadcast(e.Room, "item.deleted", payload, e.Origin)

	case "room.cleared":
		s.hub.Broadcast(e.Room, "room.cleared", map[string]any{}, e.Origin)

	case "presence":
		var payload struct {
			Peers []hub.Peer `json:"peers"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			s.log.Error("decode remote presence", "err", err)
			return
		}
		s.hub.SetRemotePeers(e.Room, e.Origin, payload.Peers)
		s.hub.BroadcastPresence(e.Room)

	default:
		s.log.Warn("unknown remote event", "type", e.Type)
	}
}

// presenceHeartbeat republishes this instance's occupancy and expires siblings
// that have gone quiet, so a crashed instance's devices leave the sidebar
// instead of haunting it.
func (s *Server) presenceHeartbeat(ctx context.Context) {
	if s.bus == nil {
		return
	}
	ticker := time.NewTicker(presenceHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, room := range s.hub.RoomsWithLocalClients() {
				s.publishPresence(ctx, room)
			}
			for _, room := range s.hub.DropStaleRemotes() {
				s.hub.BroadcastPresence(room)
			}
		}
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
