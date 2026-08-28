package hub

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fanout spins up a real websocket server backed by the hub so the tests
// exercise the same path the browser takes.
type fanout struct {
	hub      *Hub
	server   *httptest.Server
	received chan Envelope
}

func newFanout(t *testing.T) *fanout {
	t.Helper()
	h := New(slog.New(slog.NewTextHandler(io.Discard, nil)))
	f := &fanout{hub: h, received: make(chan Envelope, 32)}
	h.SetHandler(handlerFunc(func(c *Client, env Envelope) {
		f.received <- env
		h.Broadcast(c.Room, "echo", map[string]string{"from": c.Device}, "")
	}))

	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		client := h.Serve(context.Background(), conn, r.URL.Query().Get("id"),
			r.URL.Query().Get("room"), r.URL.Query().Get("device"))
		client.Wait()
	}))
	t.Cleanup(f.server.Close)
	return f
}

type handlerFunc func(*Client, Envelope)

func (f handlerFunc) HandleMessage(c *Client, env Envelope) { f(c, env) }

func (f *fanout) dial(t *testing.T, id, room, device string) *websocket.Conn {
	t.Helper()
	url := "ws" + f.server.URL[len("http"):] +
		"?id=" + id + "&room=" + room + "&device=" + device
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func readEnvelope(t *testing.T, conn *websocket.Conn) Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return env
}

func TestBroadcastReachesEveryPeerInTheRoom(t *testing.T) {
	f := newFanout(t)

	laptop := f.dial(t, "c1", "abcd-efgh", "laptop")
	phone := f.dial(t, "c2", "abcd-efgh", "phone")
	stranger := f.dial(t, "c3", "zzzz-zzzz", "stranger")

	waitForPeers(t, f.hub, "abcd-efgh", 2)

	f.hub.Broadcast("abcd-efgh", "item.created", map[string]string{"id": "x"}, "")

	for name, conn := range map[string]*websocket.Conn{"laptop": laptop, "phone": phone} {
		if env := readEnvelope(t, conn); env.Type != "item.created" {
			t.Fatalf("%s got %q, want item.created", name, env.Type)
		}
	}

	// The other room must not see it.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, _, err := stranger.Read(ctx); err == nil {
		t.Fatal("a client in another room received the broadcast")
	}
}

func TestBroadcastCanSkipTheSender(t *testing.T) {
	f := newFanout(t)
	sender := f.dial(t, "c1", "abcd-efgh", "sender")
	other := f.dial(t, "c2", "abcd-efgh", "other")
	waitForPeers(t, f.hub, "abcd-efgh", 2)

	f.hub.Broadcast("abcd-efgh", "item.created", map[string]string{"id": "x"}, "c1")

	if env := readEnvelope(t, other); env.Type != "item.created" {
		t.Fatalf("other got %q", env.Type)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, _, err := sender.Read(ctx); err == nil {
		t.Fatal("sender should have been skipped")
	}
}

func TestClientMessagesReachTheHandler(t *testing.T) {
	f := newFanout(t)
	conn := f.dial(t, "c1", "abcd-efgh", "laptop")
	waitForPeers(t, f.hub, "abcd-efgh", 1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	payload, _ := json.Marshal(Envelope{Type: "text.create", Payload: json.RawMessage(`{"content":"hi"}`)})
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case env := <-f.received:
		if env.Type != "text.create" {
			t.Fatalf("handler saw %q", env.Type)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler never saw the message")
	}

	if env := readEnvelope(t, conn); env.Type != "echo" {
		t.Fatalf("expected the echo broadcast, got %q", env.Type)
	}
}

func TestPingIsAnsweredWithoutTheHandler(t *testing.T) {
	f := newFanout(t)
	conn := f.dial(t, "c1", "abcd-efgh", "laptop")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if env := readEnvelope(t, conn); env.Type != "pong" {
		t.Fatalf("got %q, want pong", env.Type)
	}
	select {
	case env := <-f.received:
		t.Fatalf("ping should not reach the handler, but it saw %q", env.Type)
	default:
	}
}

func TestDisconnectRemovesThePeer(t *testing.T) {
	f := newFanout(t)
	conn := f.dial(t, "c1", "abcd-efgh", "laptop")
	waitForPeers(t, f.hub, "abcd-efgh", 1)

	conn.CloseNow()
	waitForPeers(t, f.hub, "abcd-efgh", 0)
}

func waitForPeers(t *testing.T, h *Hub, room string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(h.Peers(room)) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("room %q settled at %d peers, want %d", room, len(h.Peers(room)), want)
}
