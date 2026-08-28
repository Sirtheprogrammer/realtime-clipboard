package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"clipboard/internal/hub"
)

// acceptOptions translates ALLOWED_ORIGINS into the upgrader's origin policy.
// The default wildcard suits local and LAN use; set the variable to your public
// hostnames before exposing the service to the internet.
func (s *Server) acceptOptions() *websocket.AcceptOptions {
	opts := &websocket.AcceptOptions{
		CompressionMode: websocket.CompressionContextTakeover,
	}
	allowed := strings.TrimSpace(s.cfg.AllowedOrigins)
	if allowed == "" || allowed == "*" {
		opts.InsecureSkipVerify = true
		return opts
	}
	for _, candidate := range strings.Split(allowed, ",") {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			opts.OriginPatterns = append(opts.OriginPatterns, candidate)
		}
	}
	return opts
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	code, ok := NormalizeRoomCode(r.URL.Query().Get("room"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid room code")
		return
	}
	device := deviceOf(r, r.URL.Query().Get("device"))

	if _, err := s.store.TouchRoom(r.Context(), code); err != nil {
		s.log.Error("touch room", "err", err)
		writeError(w, http.StatusInternalServerError, "could not open room")
		return
	}

	conn, err := websocket.Accept(w, r, s.acceptOptions())
	if err != nil {
		s.log.Warn("websocket upgrade failed", "err", err)
		return // Accept already wrote a response
	}

	// The request context ends when the handler returns, so the client gets a
	// background context that lives as long as the socket.
	client := s.hub.Serve(context.Background(), conn, uuid.NewString(), code, device)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	items, err := s.store.ListItems(ctx, code, itemPageSize)
	cancel()
	if err != nil {
		s.log.Error("list items for socket", "err", err)
		items = nil
	}

	client.Send("welcome", map[string]any{
		"client_id": client.ID,
		"device":    client.Device,
		"room":      code,
		"items":     items,
		"peers":     s.hub.Peers(code),
		"limits": map[string]any{
			"max_upload_bytes": s.cfg.MaxUploadBytes,
			"retention":        s.cfg.Retention.String(),
		},
	})
	s.hub.BroadcastPresence(code)

	client.Wait()
}

// HandleMessage implements hub.Handler. Text pastes, deletes and clears travel
// over the socket so they land on every device without an HTTP round trip.
func (s *Server) HandleMessage(c *hub.Client, env hub.Envelope) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch env.Type {
	case "text.create":
		var payload struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			c.Send("error", map[string]string{"message": "malformed text payload"})
			return
		}
		item, err := s.createTextItem(ctx, c.Room, payload.Content, c.Device)
		if err != nil {
			c.Send("error", map[string]string{"message": socketError(err)})
			return
		}
		s.hub.Broadcast(c.Room, "item.created", item, "")

	case "item.delete":
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			c.Send("error", map[string]string{"message": "malformed delete payload"})
			return
		}
		if err := s.deleteItem(ctx, c.Room, payload.ID); err != nil {
			c.Send("error", map[string]string{"message": "could not delete that item"})
			return
		}
		s.hub.Broadcast(c.Room, "item.deleted", map[string]string{"id": payload.ID}, "")

	case "room.clear":
		if err := s.clearRoom(ctx, c.Room); err != nil {
			c.Send("error", map[string]string{"message": "could not clear the room"})
			return
		}
		s.hub.Broadcast(c.Room, "room.cleared", map[string]any{}, "")

	case "presence.refresh":
		s.hub.BroadcastPresence(c.Room)

	default:
		c.Send("error", map[string]string{"message": "unknown message type: " + env.Type})
	}
}

func socketError(err error) string {
	switch {
	case errors.Is(err, errEmptyContent):
		return "nothing to paste"
	case errors.Is(err, errTooLarge):
		return "that text is over the size limit"
	default:
		return "could not save that item"
	}
}
