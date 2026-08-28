package e2e

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// These tests cover what happens when more than one instance serves the same
// room — `heroku ps:scale web=2`, or several containers behind a load balancer.
// The hub is in-process, so without the Postgres LISTEN/NOTIFY bridge each
// instance would be its own isolated clipboard.
//
// Run them with docker-compose.cluster.yml; see its header.

func TestTextSyncsAcrossInstances(t *testing.T) {
	requireCluster(t)
	room := createRoom(t)

	onA := dialAt(t, baseURL, room, "laptop-on-a")
	onB := dialAt(t, baseURLB, room, "phone-on-b")
	expectType(t, onA, "welcome")
	expectType(t, onB, "welcome")

	content := "crossing instances at " + time.Now().Format(time.RFC3339Nano)
	write(t, onA, "text.create", map[string]string{"content": content})

	if item := awaitItem(t, onB, "item.created"); item.Content != content {
		t.Fatalf("the other instance received %q, want %q", item.Content, content)
	}
}

func TestUploadSyncsAcrossInstancesAndIsReadableFromEither(t *testing.T) {
	requireCluster(t)
	room := createRoom(t)

	onB := dialAt(t, baseURLB, room, "phone-on-b")
	expectType(t, onB, "welcome")

	original := pngBytes(t, 32, 18)
	uploadFile(t, room, "screenshot.png", "image/png", original) // goes to instance A

	item := awaitItem(t, onB, "item.created")
	if item.Kind != "image" || item.Width != 32 || item.Height != 18 {
		t.Fatalf("relayed item is wrong: %+v", item)
	}

	// The payload must be readable from the instance that did not receive the
	// upload — which is only true because it lives in Postgres, not on a dyno's
	// own disk.
	got := getBytesFrom(t, baseURLB, "/api/items/"+item.ID+"/raw")
	if !bytes.Equal(got, original) {
		t.Fatalf("instance B served %d bytes that differ from the %d uploaded to A", len(got), len(original))
	}
}

func TestRangeRequestWorksOnPostgresStoredBlobs(t *testing.T) {
	requireCluster(t)
	room := createRoom(t)

	payload := bytes.Repeat([]byte("0123456789"), 300_000) // 3 MB, several chunks
	uploadFile(t, room, "big.bin", "application/octet-stream", payload)

	var body struct {
		Items []item `json:"items"`
	}
	getJSON(t, "/api/rooms/"+room, &body)
	if len(body.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(body.Items))
	}

	// Seek across a chunk boundary: the reader stores 1 MiB per row, so this
	// range starts in one chunk and ends in another.
	const start, end = 1_048_570, 1_048_600
	got := getRangeFrom(t, baseURLB, "/api/items/"+body.Items[0].ID+"/raw", start, end)
	if want := payload[start : end+1]; !bytes.Equal(got, want) {
		t.Fatalf("range %d-%d returned %q, want %q", start, end, got, want)
	}
}

func TestDeleteAndClearSyncAcrossInstances(t *testing.T) {
	requireCluster(t)
	room := createRoom(t)

	onA := dialAt(t, baseURL, room, "laptop-on-a")
	onB := dialAt(t, baseURLB, room, "phone-on-b")
	expectType(t, onA, "welcome")
	expectType(t, onB, "welcome")

	write(t, onA, "text.create", map[string]string{"content": "delete me"})
	created := awaitItem(t, onB, "item.created")
	awaitItem(t, onA, "item.created")

	// Delete from the instance that did not create it.
	write(t, onB, "item.delete", map[string]string{"id": created.ID})
	deleted := expectType(t, onA, "item.deleted")
	var idPayload struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(deleted.Payload, &idPayload)
	if idPayload.ID != created.ID {
		t.Fatalf("deleted id = %q, want %q", idPayload.ID, created.ID)
	}

	write(t, onA, "text.create", map[string]string{"content": "and another"})
	awaitItem(t, onA, "item.created")
	awaitItem(t, onB, "item.created")

	write(t, onB, "room.clear")
	expectType(t, onA, "room.cleared")
}

func TestPresenceSpansInstances(t *testing.T) {
	requireCluster(t)
	room := createRoom(t)

	onA := dialAt(t, baseURL, room, "laptop-on-a")
	expectType(t, onA, "welcome")

	onB := dialAt(t, baseURLB, room, "phone-on-b")
	expectType(t, onB, "welcome")

	// Each side must end up listing both devices, not just its own.
	sides := []struct {
		name string
		conn *websocket.Conn
	}{
		{"instance A", onA},
		{"instance B", onB},
	}
	for _, side := range sides {
		peers := awaitPeers(t, side.conn, 2)
		devices := map[string]bool{}
		for _, p := range peers {
			devices[p.Device] = true
		}
		if !devices["laptop-on-a"] || !devices["phone-on-b"] {
			t.Fatalf("%s lists %v, want both devices", side.name, devices)
		}
	}
}
