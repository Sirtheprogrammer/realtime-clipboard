// Package e2e drives a running Clipboard server the way two browsers would:
// one tab pastes, the other must see it arrive over its websocket.
//
// Skipped unless CLIPBOARD_E2E_URL points at a live server, e.g.
//
//	docker compose up -d
//	CLIPBOARD_E2E_URL=http://localhost:8080 go test ./e2e/...
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

var (
	baseURL string
	// baseURLB points at a second instance sharing the same database, which is
	// what `heroku ps:scale web=2` produces. Optional.
	baseURLB string
)

func TestMain(m *testing.M) {
	baseURL = strings.TrimSuffix(os.Getenv("CLIPBOARD_E2E_URL"), "/")
	baseURLB = strings.TrimSuffix(os.Getenv("CLIPBOARD_E2E_URL_B"), "/")
	os.Exit(m.Run())
}

func requireCluster(t *testing.T) {
	t.Helper()
	requireServer(t)
	if baseURLB == "" {
		t.Skip("set CLIPBOARD_E2E_URL_B to a second instance to run the multi-instance tests")
	}
}

func requireServer(t *testing.T) {
	t.Helper()
	if baseURL == "" {
		t.Skip("set CLIPBOARD_E2E_URL to run the end-to-end tests")
	}
}

func TestHealth(t *testing.T) {
	requireServer(t)
	var body struct{ Status string }
	getJSON(t, "/api/health", &body)
	if body.Status != "ok" {
		t.Fatalf("health status = %q", body.Status)
	}
}

func TestTextPasteReachesTheOtherDevice(t *testing.T) {
	requireServer(t)
	room := createRoom(t)

	laptop := dial(t, room, "laptop")
	phone := dial(t, room, "phone")
	expectType(t, laptop, "welcome")
	expectType(t, phone, "welcome")

	content := "the quick brown fox " + time.Now().Format(time.RFC3339Nano)
	write(t, laptop, "text.create", map[string]string{"content": content})

	item := awaitItem(t, phone, "item.created")
	if item.Content != content {
		t.Fatalf("phone received %q, want %q", item.Content, content)
	}
	if item.Kind != "text" {
		t.Fatalf("kind = %q, want text", item.Kind)
	}
	if item.Device != "laptop" {
		t.Fatalf("device = %q, want laptop", item.Device)
	}
}

func TestPastedURLIsClassifiedAsALink(t *testing.T) {
	requireServer(t)
	room := createRoom(t)
	conn := dial(t, room, "laptop")
	expectType(t, conn, "welcome")

	write(t, conn, "text.create", map[string]string{"content": "https://example.com/page?q=1"})
	if item := awaitItem(t, conn, "item.created"); item.Kind != "link" {
		t.Fatalf("kind = %q, want link", item.Kind)
	}
}

func TestImageUploadBroadcastsAndServesTheOriginalBytes(t *testing.T) {
	requireServer(t)
	room := createRoom(t)
	watcher := dial(t, room, "phone")
	expectType(t, watcher, "welcome")

	original := pngBytes(t, 40, 25)
	uploadFile(t, room, "screenshot.png", "image/png", original)

	item := awaitItem(t, watcher, "item.created")
	if item.Kind != "image" {
		t.Fatalf("kind = %q, want image", item.Kind)
	}
	if item.Width != 40 || item.Height != 25 {
		t.Fatalf("dimensions = %dx%d, want 40x25", item.Width, item.Height)
	}
	if item.SizeBytes != int64(len(original)) {
		t.Fatalf("size = %d, want %d", item.SizeBytes, len(original))
	}

	got := getBytes(t, "/api/items/"+item.ID+"/raw")
	if !bytes.Equal(got, original) {
		t.Fatalf("served %d bytes that differ from the %d uploaded", len(got), len(original))
	}
}

func TestArbitraryFileRoundTrips(t *testing.T) {
	requireServer(t)
	room := createRoom(t)
	watcher := dial(t, room, "phone")
	expectType(t, watcher, "welcome")

	payload := bytes.Repeat([]byte("clipboard-payload;"), 5000) // ~90 KB
	uploadFile(t, room, "notes.bin", "application/octet-stream", payload)

	item := awaitItem(t, watcher, "item.created")
	if item.Kind != "file" {
		t.Fatalf("kind = %q, want file", item.Kind)
	}
	if item.FileName != "notes.bin" {
		t.Fatalf("file name = %q", item.FileName)
	}
	if got := getBytes(t, "/api/items/"+item.ID+"/download"); !bytes.Equal(got, payload) {
		t.Fatalf("download returned %d bytes, want %d", len(got), len(payload))
	}
}

func TestHistoryIsReplayedToALateJoiner(t *testing.T) {
	requireServer(t)
	room := createRoom(t)

	first := dial(t, room, "laptop")
	expectType(t, first, "welcome")
	write(t, first, "text.create", map[string]string{"content": "sent before you arrived"})
	awaitItem(t, first, "item.created")

	late := dial(t, room, "phone")
	welcome := expectType(t, late, "welcome")

	var payload struct {
		Items []item `json:"items"`
	}
	if err := json.Unmarshal(welcome.Payload, &payload); err != nil {
		t.Fatalf("decode welcome: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0].Content != "sent before you arrived" {
		t.Fatalf("late joiner got %d items: %+v", len(payload.Items), payload.Items)
	}
}

func TestDeleteAndClearFanOut(t *testing.T) {
	requireServer(t)
	room := createRoom(t)
	laptop := dial(t, room, "laptop")
	phone := dial(t, room, "phone")
	expectType(t, laptop, "welcome")
	expectType(t, phone, "welcome")

	write(t, laptop, "text.create", map[string]string{"content": "delete me"})
	created := awaitItem(t, phone, "item.created")
	awaitItem(t, laptop, "item.created")

	write(t, laptop, "item.delete", map[string]string{"id": created.ID})
	deleted := expectType(t, phone, "item.deleted")
	var idPayload struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(deleted.Payload, &idPayload)
	if idPayload.ID != created.ID {
		t.Fatalf("deleted id = %q, want %q", idPayload.ID, created.ID)
	}

	write(t, laptop, "text.create", map[string]string{"content": "and another"})
	awaitItem(t, phone, "item.created")
	awaitItem(t, laptop, "item.created")

	write(t, laptop, "room.clear")
	expectType(t, phone, "room.cleared")

	var room2 struct {
		Items []item `json:"items"`
	}
	getJSON(t, "/api/rooms/"+room, &room2)
	if len(room2.Items) != 0 {
		t.Fatalf("room still holds %d items after clear", len(room2.Items))
	}
}

func TestPresenceTracksConnectedDevices(t *testing.T) {
	requireServer(t)
	room := createRoom(t)

	laptop := dial(t, room, "laptop")
	expectType(t, laptop, "welcome")

	phone := dial(t, room, "phone")
	expectType(t, phone, "welcome")

	// Joining broadcasts presence to the whole room, so the laptop sees its own
	// arrival first and then the phone's. Wait for the frame that lists both.
	peers := awaitPeers(t, laptop, 2)
	devices := map[string]bool{}
	for _, p := range peers {
		devices[p.Device] = true
	}
	if !devices["laptop"] || !devices["phone"] {
		t.Fatalf("presence lists %v, want both laptop and phone", devices)
	}
}

// awaitPeers reads presence frames until the room settles at the wanted size.
func awaitPeers(t *testing.T, conn *websocket.Conn, want int) []peer {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for a presence frame with %d peers", want)
		}
		env := expectType(t, conn, "presence")
		var payload struct {
			Peers []peer `json:"peers"`
		}
		if err := json.Unmarshal(env.Payload, &payload); err != nil {
			t.Fatalf("decode presence: %v", err)
		}
		if len(payload.Peers) == want {
			return payload.Peers
		}
	}
}

type peer struct {
	ID     string `json:"id"`
	Device string `json:"device"`
}

func TestRoomsAreIsolated(t *testing.T) {
	requireServer(t)
	roomA, roomB := createRoom(t), createRoom(t)

	a := dial(t, roomA, "laptop")
	b := dial(t, roomB, "stranger")
	expectType(t, a, "welcome")
	expectType(t, b, "welcome")

	write(t, a, "text.create", map[string]string{"content": "private note"})
	awaitItem(t, a, "item.created")

	// Presence frames about room B's own occupants are expected; anything that
	// carries room A's content is a leak.
	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		_, raw, err := b.Read(ctx)
		cancel()
		if err != nil {
			break // nothing more arrived, which is what we want
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if env.Type != "presence" {
			t.Fatalf("room B received a %q frame from room A: %s", env.Type, env.Payload)
		}
	}
}

func TestInvalidRoomCodeIsRejected(t *testing.T) {
	requireServer(t)
	res, err := http.Get(baseURL + "/api/rooms/" + url.PathEscape("../etc"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest && res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 400 or 404", res.StatusCode)
	}
}

func TestDashboardIsServed(t *testing.T) {
	requireServer(t)
	for _, path := range []string{"/", "/r/abcd-efgh", "/app.js", "/styles.css", "/manifest.webmanifest"} {
		res, err := http.Get(baseURL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s returned %d", path, res.StatusCode)
		}
		if len(body) == 0 {
			t.Errorf("%s returned an empty body", path)
		}
	}
}

/* ── helpers ───────────────────────────────────────────────── */

type item struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Content   string `json:"content"`
	FileName  string `json:"file_name"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Device    string `json:"device"`
}

type envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func createRoom(t *testing.T) string {
	t.Helper()
	res, err := http.Post(baseURL+"/api/rooms", "application/json", nil)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create room status = %d", res.StatusCode)
	}
	var room struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(res.Body).Decode(&room); err != nil {
		t.Fatalf("decode room: %v", err)
	}
	return room.Code
}

func dial(t *testing.T, room, device string) *websocket.Conn {
	t.Helper()
	return dialAt(t, baseURL, room, device)
}

func dialAt(t *testing.T, base, room, device string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(base, "http") +
		"/ws?room=" + url.QueryEscape(room) + "&device=" + url.QueryEscape(device)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", device, err)
	}
	conn.SetReadLimit(4 << 20)
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func write(t *testing.T, conn *websocket.Conn, msgType string, payload ...any) {
	t.Helper()
	env := map[string]any{"type": msgType}
	if len(payload) > 0 {
		env["payload"] = payload[0]
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal %s: %v", msgType, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("write %s: %v", msgType, err)
	}
}

// expectType reads frames until it sees the wanted type, skipping the presence
// chatter that other tabs joining produces.
func expectType(t *testing.T, conn *websocket.Conn, want string) envelope {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q", want)
		}
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		_, raw, err := conn.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read while waiting for %q: %v", want, err)
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if env.Type == "error" {
			t.Fatalf("server error while waiting for %q: %s", want, env.Payload)
		}
		if env.Type == want {
			return env
		}
	}
}

func awaitItem(t *testing.T, conn *websocket.Conn, want string) item {
	t.Helper()
	env := expectType(t, conn, want)
	var it item
	if err := json.Unmarshal(env.Payload, &it); err != nil {
		t.Fatalf("decode item: %v", err)
	}
	return it
}

func uploadFile(t *testing.T, room, name, contentType string, payload []byte) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("device", "laptop")

	header := make(map[string][]string)
	header["Content-Disposition"] = []string{
		fmt.Sprintf(`form-data; name="file"; filename=%q`, name),
	}
	header["Content-Type"] = []string{contentType}
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create part: %v", err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write part: %v", err)
	}
	writer.Close()

	res, err := http.Post(baseURL+"/api/rooms/"+room+"/upload", writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		msg, _ := io.ReadAll(res.Body)
		t.Fatalf("upload status = %d: %s", res.StatusCode, msg)
	}
}

func getJSON(t *testing.T, path string, out any) {
	t.Helper()
	res, err := http.Get(baseURL + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get %s status = %d", path, res.StatusCode)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func getBytes(t *testing.T, path string) []byte {
	t.Helper()
	return getBytesFrom(t, baseURL, path)
}

// getRangeFrom asks one instance for a byte range, which is how a browser
// resumes a download or scrubs a video.
func getRangeFrom(t *testing.T, base, path string, start, end int) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatalf("build range request: %v", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("range get %s: %v", path, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusPartialContent {
		t.Fatalf("range get %s status = %d, want 206", path, res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read range %s: %v", path, err)
	}
	return body
}

func getBytesFrom(t *testing.T, base, path string) []byte {
	t.Helper()
	res, err := http.Get(base + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get %s status = %d", path, res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return body
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 9), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
