# Clipboard & Vault

A realtime shared clipboard and encrypted credential vault. Open the same room on your laptop and your phone,
paste text, a screenshot or a file on one, and it shows up on the other
immediately — over a websocket, with no refresh and no sign-in.

Go backend · vanilla HTML/CSS/JS progressive web app · SQLite (Zero-Config) / Postgres · Docker Compose · Chrome/Edge/Brave Extension.

---

## Zero-Config Standalone Mode (No Postgres or Docker Required)

You can run the entire Clipboard & Vault server **out of the box with zero configuration**!
The server automatically uses an embedded pure-Go SQLite database and creates persistent encryption keys and blob storage in `./data`.

### One-Click Startup
- **Windows (Command Prompt / Explorer)**: Double-click or run `start-server.bat`
- **Windows (PowerShell)**: Run `.\start-server.ps1`
- **Linux / macOS**: Run `./start-server.sh`
- **Using Go**:
  ```bash
  go run ./cmd/server
  ```

Open **`http://localhost:8080`** in your browser. All features (realtime clipboard, encrypted password manager, browser extension autofill, and file sharing) work locally with zero setup!

---

## What it does

| | |
|---|---|
| **Zero-Config SQLite** | Run as a single standalone executable without installing PostgreSQL or Docker. |
| **Encrypted Vault** | Store logins and secrets encrypted with AES-GCM-256 using auto-persisted master keys. |
| **Extension & Autofill** | Companion browser extension auto-detects and autofills logins seamlessly. |
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

### Zero-Config (Recommended for Local / Desktop)
Run `./start-server.bat` (Windows) or `./start-server.sh` (Linux/macOS).

### Docker Compose (Multi-container with Postgres)

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

---

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
