# Clipboard

A realtime shared clipboard. Open the same room on your laptop and your phone,
paste text, a screenshot or a file on one, and it shows up on the other
immediately — over a websocket, with no refresh and no sign-in.

Go backend · vanilla HTML/CSS/JS progressive web app · Postgres · Docker Compose.

```
docker compose up -d --build
# open http://localhost:8080
```

---

## What it does

| | |
|---|---|
| **Text** | `Ctrl+V` anywhere on the page and it is shared instantly. Paste into the composer instead if you want to edit first (`Ctrl+Enter` sends). |
| **Screenshots & images** | Paste straight from the snipping tool. Thumbnails render inline; click for a lightbox; "Copy image" puts it back on your system clipboard. |
| **Files** | Drag and drop anywhere, or use the Files button. Per-file progress bars, resumable-friendly streaming, 100 MB default cap. |
| **Links** | Bare URLs are detected and rendered as clickable links. |
| **Presence** | The sidebar lists every device currently in the room and which one sent each item. |
| **Offline** | Installable PWA. The shell is cached, the socket reconnects with backoff, and a banner tells you when you're catching up. |
| **Expiry** | Items and their files are deleted after `RETENTION` (24h by default) by a background janitor. |

Rooms are created on demand with a code like `kfrb-mp3x`. Anyone with the code
can read and write, so treat it as a password — see [Security](#security).

---

## Running it

### Docker Compose (the intended path)

```bash
cp .env.example .env      # optional: set a real POSTGRES_PASSWORD
docker compose up -d --build
docker compose logs -f app
```

Compose starts Postgres and the app together. The app waits for the database to
become healthy, applies its schema on boot, and serves the dashboard on
`http://localhost:8080`. Uploaded files live in the `blobs` volume, database
data in `pgdata`.

To use another host port: `PORT=8090 docker compose up -d`.

### Sharing with your phone

Both devices need to reach the same server. On a LAN, find your machine's IP and
open `http://<your-ip>:8080/r/<room-code>` on the phone — or hit **Share** in
the top bar to send yourself the link.

Note that browsers restrict the Clipboard API to *secure contexts*:
`localhost` counts, a plain-HTTP LAN address does not. Over HTTP on a LAN,
pasting **into** the page and "Copy" falling back to a text selection still
work, but one-click "Copy image" needs HTTPS. Put it behind a TLS terminator
(Caddy, Traefik, a tunnel) for the full experience.

### Local development without Docker

```bash
docker compose up -d db                      # just Postgres
export DATABASE_URL='postgres://clipboard:clipboard@localhost:5432/clipboard?sslmode=disable'
go run ./cmd/server
```

(That needs the `db` port published — uncomment the `ports` block in
`docker-compose.yml`.)

---

## Configuration

Every setting is an environment variable; see `.env.example`.

| Variable | Default | Meaning |
|---|---|---|
| `ADDR` | `:8080` | Listen address. |
| `DATABASE_URL` | local Postgres | Connection string. |
| `BLOB_DIR` | `./data/blobs` | Where uploaded files are written (`/data/blobs` in the image). |
| `WEB_DIR` | `./web` | Static dashboard files. |
| `MAX_UPLOAD_BYTES` | `104857600` | Largest single upload (100 MiB). |
| `RETENTION` | `24h` | How long items survive before the janitor deletes them. |
| `SWEEP_INTERVAL` | `5m` | How often the janitor runs. |
| `ALLOWED_ORIGINS` | `*` | Comma-separated origins allowed to open a websocket. |

---

## How it fits together

```
browser ──ws──┐
              ├─► hub (rooms → clients) ──► broadcast to every peer in the room
browser ──ws──┘         ▲
                        │
browser ──HTTP POST /upload ──► blob store (volume)  +  Postgres (metadata)
```

- **Text, deletes and clears travel over the websocket.** No HTTP round trip, so
  a paste lands on the other device in one hop.
- **Files go over HTTP** as a streaming multipart upload — nothing is buffered in
  memory, and the browser gets real progress events. When the upload finishes the
  server broadcasts the new item over the socket, so every tab renders it at once.
- **Postgres holds metadata, the volume holds bytes.** Large pastes never go
  through the database.
- **The socket is the source of truth.** On connect the server replays the room's
  history in the `welcome` frame, so a reconnect after a laptop wakes up
  self-heals without any client-side diffing.

### Layout

```
cmd/server/         entrypoint: config, wiring, graceful shutdown
internal/config/    environment parsing
internal/database/  pgx pool, retrying connect, embedded schema.sql
internal/models/    Item and Room
internal/store/     all SQL
internal/blob/      file storage on the volume, with path-escape guards
internal/hub/       websocket rooms, fan-out, ping/pong, slow-client eviction
internal/api/       HTTP routes, upload handling, socket handler, janitor
web/                dashboard: index.html, styles.css, app.js, sw.js, manifest
e2e/                end-to-end tests against a running server
```

### Websocket protocol

Every frame is `{"type": "...", "payload": {...}}`.

Client → server: `text.create` · `item.delete` · `room.clear` ·
`presence.refresh` · `ping`

Server → client: `welcome` (client id, device name, item history, limits) ·
`item.created` · `item.deleted` · `room.cleared` · `presence` · `error` · `pong`

### HTTP API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/health` | Liveness. |
| `POST` | `/api/rooms` | Create a room, returns its code. |
| `GET` | `/api/rooms/{code}` | Room, items, peers, stats, limits. |
| `POST` | `/api/rooms/{code}/items` | Create a text item (JSON). |
| `POST` | `/api/rooms/{code}/upload` | Multipart file upload. |
| `DELETE` | `/api/rooms/{code}/items` | Clear the room. |
| `DELETE` | `/api/rooms/{code}/items/{id}` | Delete one item. |
| `GET` | `/api/items/{id}/raw` | Stream inline (used by `<img>`). |
| `GET` | `/api/items/{id}/download` | Stream as an attachment. |
| `GET` | `/ws?room=&device=` | Websocket. |

---

## Tests

```bash
go test ./...                                          # unit tests, no services needed

docker compose up -d                                   # then, against the live stack:
CLIPBOARD_E2E_URL=http://localhost:8080 go test ./e2e/...
```

The e2e suite opens two websockets and asserts that what one "device" pastes
arrives at the other: text, links, images (byte-for-byte), arbitrary files,
history replay for a late joiner, delete and clear fan-out, presence, and room
isolation. It skips itself when `CLIPBOARD_E2E_URL` is unset.

---

## Security

This is deliberately sign-in free, which shapes the threat model:

- **The room code is the only secret.** Codes are 8 characters from a 28-symbol
  alphabet (~38 bits) drawn from `crypto/rand`. Anyone who learns a code has full
  read/write access to that room. Don't put a code in a public channel.
- **Set `ALLOWED_ORIGINS`** to your real hostnames before exposing this publicly;
  the `*` default accepts websocket upgrades from any origin.
- **Put it behind TLS.** Room codes and content are otherwise in the clear.
- Content is escaped as text everywhere in the dashboard (no `innerHTML` for user
  data), uploads are served with `X-Content-Type-Options: nosniff` and an
  explicit `Content-Disposition`, and stored blob paths are checked against
  escaping the storage root.
- There is no rate limiting. On the open internet, put a reverse proxy in front.

## Possible next steps

Optional passphrase per room · QR code for joining from a phone · Android/iOS
share-target so "Share → Clipboard" works from any app · end-to-end encryption
with the room code as the key · pagination beyond the most recent 200 items.
