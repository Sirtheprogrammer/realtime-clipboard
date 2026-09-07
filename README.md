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
| **Scan to join** | A QR code in the sidebar opens the room directly on a phone — no typing a code across devices. |
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

Hit **Scan to join** in the sidebar (or the QR button in the top bar) and point
your phone's camera at the code. It opens straight into the room.

The QR encodes the address *you* are viewing the dashboard on, reconstructed
server-side from the request — including `X-Forwarded-Proto`, so behind a TLS
terminator like Heroku's router it produces an `https://` link rather than one
that redirects. If you are on `localhost`, the modal says so: your phone cannot
reach that, so reopen the dashboard at your machine's LAN address first and the
QR will follow.

Failing that, **Share** in the top bar sends the link through the native share
sheet, or copies it.

Note that browsers restrict the Clipboard API to *secure contexts*:
`localhost` counts, a plain-HTTP LAN address does not. Over HTTP on a LAN,
pasting **into** the page and "Copy" falling back to a text selection still
work, but one-click "Copy image" needs HTTPS. Put it behind a TLS terminator
(Caddy, Traefik, a tunnel) for the full experience.

### Heroku

```bash
heroku create your-clipboard
heroku stack:set container
heroku addons:create heroku-postgresql:essential-0
git push heroku main
heroku open
```

`heroku.yml` builds the same Dockerfile Compose uses, so what runs on Heroku is
the image you tested locally. There is no migration step — the app applies its
schema on boot.

Three things work differently on a dyno, and the app handles each of them:

- **`$PORT`.** Heroku assigns a port at runtime; the server binds it in
  preference to `ADDR`.
- **The filesystem is ephemeral.** A dyno's disk is wiped on every restart and
  deploy, at least daily, so uploads written to it would silently vanish.
  Detecting `DYNO`, the app switches `BLOB_BACKEND` to `postgres` and stores
  payloads as chunked rows instead. Set `BLOB_BACKEND` explicitly to override —
  the startup log always says which backend is in use.
- **`DATABASE_URL` arrives without an `sslmode`,** and Heroku Postgres requires
  TLS while presenting a certificate signed by its own authority. The app adds
  `sslmode=require` when the host is not local, which encrypts without demanding
  a chain it cannot verify.

Then set the origin allowlist, which defaults to accepting any origin:

```bash
heroku config:set ALLOWED_ORIGINS=https://your-clipboard.herokuapp.com
```

Heroku's router speaks WebSocket, so no extra configuration is needed there. It
does close idle connections after about a minute; both ends heartbeat well
inside that.

**Watch the database size.** With the Postgres blob backend, uploads consume
your plan's storage — `essential-0` gives you 1 GB. `app.json` therefore sets a
10 MB upload cap and 12h retention rather than the 100 MB / 24h used with a
volume. `heroku pg:info` shows where you stand.

**Scaling past one dyno works.** Instances relay room activity to each other
over Postgres `LISTEN`/`NOTIFY`, so two people on different dynos still share a
room, and the presence list spans both. Keep `quantity x DB_MAX_CONNS` under
your plan's connection limit (20 on Essential); each dyno also holds one
connection open to listen.

```bash
heroku ps:scale web=2
```

A **Deploy to Heroku** button works too — `app.json` provisions Postgres and
sets sensible defaults. Point its `repository` field at your fork first.

<details>
<summary>Deploying with the Go buildpack instead of a container</summary>

`Procfile` and the `+heroku` directives in `go.mod` support this path:

```bash
heroku create your-clipboard --buildpack heroku/go
heroku addons:create heroku-postgresql:essential-0
heroku config:set BLOB_BACKEND=postgres
git push heroku main
```

The container path is the recommended one: it reuses the tested image, and it
does not depend on the buildpack offering the Go version in `go.mod`.
</details>

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
| `BLOB_BACKEND` | `disk` | `disk` (a volume) or `postgres` (rows in the database). Defaults to `postgres` on a Heroku dyno. |
| `BLOB_DIR` | `./data/blobs` | Where uploaded files are written with the `disk` backend (`/data/blobs` in the image). |
| `DB_MAX_CONNS` | `10` | Postgres connections per instance; minimum 2. |
| `WEB_DIR` | `./web` | Static dashboard files. |
| `MAX_UPLOAD_BYTES` | `104857600` | Largest single upload (100 MiB). |
| `RETENTION` | `24h` | How long items survive before the janitor deletes them. |
| `SWEEP_INTERVAL` | `5m` | How often the janitor runs. |
| `ALLOWED_ORIGINS` | `*` | Comma-separated origins allowed to open a websocket. |
| `PORT` | — | If set, overrides `ADDR`. This is how Heroku assigns a port. |
| `INSTANCE_ID` | random | Names this instance in logs and cross-instance events. The dyno name on Heroku. |

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
- **The hub is in-process, so instances talk to each other.** With more than one
  container or dyno serving a room, two people can land on different instances.
  Each instance relays room activity to the others over Postgres
  `LISTEN`/`NOTIFY` — no Redis, no extra add-on. `item.created` carries only the
  id, because a pasted item can be larger than a NOTIFY payload allows; the
  receiving instance loads it from the database.

### Layout

```
cmd/server/         entrypoint: config, wiring, graceful shutdown
internal/config/    environment parsing
internal/database/  pgx pool, retrying connect, embedded schema.sql
internal/models/    Item and Room
internal/store/     all SQL
internal/blob/      payload storage: a disk volume, or Postgres rows where
                    the filesystem is ephemeral
internal/events/    cross-instance fan-out over Postgres LISTEN/NOTIFY
internal/hub/       websocket rooms, fan-out, ping/pong, slow-client eviction
internal/api/       HTTP routes, upload handling, socket handler, janitor
web/                dashboard: index.html, styles.css, app.js, sw.js, manifest
e2e/                end-to-end tests against a running server

Dockerfile          multi-stage build, used by Compose and Heroku alike
docker-compose.yml  app + Postgres
  ...cluster.yml    overlay adding a second instance, for the multi-instance tests
heroku.yml          Heroku container build
app.json            Heroku Deploy button / review apps
Procfile            Go buildpack path only
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
| `GET` | `/api/rooms/{code}/qr.svg` | QR code for the room's URL, as SVG. |
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

The QR code has its own tests: the rendered SVG is parsed back into a module
matrix and compared against the encoder's, and the URL construction is checked
across proxy-header combinations. To confirm the output actually scans, dump the
fixtures and read them with any external decoder:

```bash
QR_SVG_DUMP_DIR=/tmp/qr go test ./internal/api/ -run TestDumpQRSVGs
```

Each `qr-N.svg` should decode to the URL in the matching `qr-N.svg.txt`.

To exercise the multi-instance path — two dynos, or several containers behind a
load balancer — bring up a second instance against the same database:

```bash
docker compose -f docker-compose.yml -f docker-compose.cluster.yml up -d --build

CLIPBOARD_E2E_URL=http://localhost:8080 CLIPBOARD_E2E_URL_B=http://localhost:8081 go test ./e2e/...
```

That adds five tests covering what only works because of the LISTEN/NOTIFY
bridge and the Postgres blob backend: a paste on one instance reaching the
other, an upload to one being readable from the other (including a range request
that spans a chunk boundary), deletes and clears crossing over, and the presence
list spanning both.

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


