// Package events carries room activity between server instances.
//
// The hub is in-process, so on its own a second dyno (or a second container)
// would be a second, disjoint clipboard: two people in the same room would each
// see only what their own instance received. This package closes that gap with
// Postgres LISTEN/NOTIFY, which every deployment already has a database for —
// no Redis, no extra add-on.
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Channel is the Postgres NOTIFY channel every instance listens on.
const Channel = "clipboard_events"

// maxPayload is Postgres' own limit on a NOTIFY payload (8000 bytes), minus
// room for the JSON envelope.
const maxPayload = 7000

// Event is one thing that happened in a room.
//
// item.created carries only the id: a pasted item can be far larger than a
// NOTIFY payload allows, so the receiving instance loads it from the database.
// Everything else is small enough to travel inline.
type Event struct {
	Origin  string          `json:"origin"`
	Room    string          `json:"room"`
	Type    string          `json:"type"`
	ItemID  string          `json:"item_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Bus struct {
	pool   *pgxpool.Pool
	origin string
	log    *slog.Logger
}

func New(pool *pgxpool.Pool, origin string, log *slog.Logger) *Bus {
	return &Bus{pool: pool, origin: origin, log: log}
}

// Publish announces an event to the other instances. The caller has already
// delivered it to its own clients, so this never loops back.
func (b *Bus) Publish(ctx context.Context, e Event) {
	e.Origin = b.origin

	raw, err := json.Marshal(e)
	if err != nil {
		b.log.Error("encode event", "type", e.Type, "err", err)
		return
	}
	if len(raw) > maxPayload {
		b.log.Error("event payload too large to notify", "type", e.Type, "bytes", len(raw))
		return
	}
	if _, err := b.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, Channel, string(raw)); err != nil {
		b.log.Error("publish event", "type", e.Type, "err", err)
	}
}

// Listen delivers events from other instances to handler until ctx is done. It
// holds one pooled connection open and reconnects on its own, because a dropped
// listener is the difference between synced and silently diverged.
func (b *Bus) Listen(ctx context.Context, handler func(Event)) {
	go func() {
		backoff := time.Second
		for ctx.Err() == nil {
			if err := b.listenOnce(ctx, handler); err != nil && ctx.Err() == nil {
				b.log.Error("event listener dropped, reconnecting", "err", err, "in", backoff)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, 30*time.Second)
				continue
			}
			backoff = time.Second
		}
	}()
}

func (b *Bus) listenOnce(ctx context.Context, handler func(Event)) error {
	conn, err := b.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire listener connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	b.log.Info("listening for events from other instances", "channel", Channel, "instance", b.origin)

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("wait for notification: %w", err)
		}

		var e Event
		if err := json.Unmarshal([]byte(notification.Payload), &e); err != nil {
			b.log.Error("decode event", "err", err)
			continue
		}
		if e.Origin == b.origin {
			continue // we already delivered this to our own clients
		}
		handler(e)
	}
}
