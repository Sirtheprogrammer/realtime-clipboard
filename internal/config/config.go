package config

import (
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Blob backends.
const (
	BackendDisk     = "disk"
	BackendPostgres = "postgres"
)

// Config holds every knob the server reads from the environment.
type Config struct {
	Addr           string
	DatabaseURL    string
	BlobBackend    string
	BlobDir        string
	WebDir         string
	MaxUploadBytes int64
	MaxDBConns     int32
	Retention      time.Duration
	SweepInterval  time.Duration
	AllowedOrigins string

	// InstanceID distinguishes this process from its siblings when several are
	// sharing one database. On Heroku it is the dyno name.
	InstanceID string

	// Notes records decisions Load made on the caller's behalf, so main can log
	// them instead of leaving an operator guessing.
	Notes []string
}

func Load() Config {
	cfg := Config{
		WebDir:         env("WEB_DIR", "./web"),
		BlobDir:        env("BLOB_DIR", "./data/blobs"),
		MaxUploadBytes: envInt64("MAX_UPLOAD_BYTES", 100<<20), // 100 MiB
		// Must be at least 2: the cross-instance event listener holds one
		// connection open for as long as the process runs.
		MaxDBConns:     int32(max(envInt64("DB_MAX_CONNS", 10), 2)),
		Retention:      envDuration("RETENTION", 24*time.Hour),
		SweepInterval:  envDuration("SWEEP_INTERVAL", 5*time.Minute),
		AllowedOrigins: env("ALLOWED_ORIGINS", "*"),
	}

	dyno := os.Getenv("DYNO")

	cfg.Addr = listenAddr()
	cfg.DatabaseURL, cfg.Notes = databaseURL(cfg.Notes)
	cfg.BlobBackend, cfg.Notes = blobBackend(dyno, cfg.Notes)
	cfg.InstanceID = instanceID(dyno)

	return cfg
}

// listenAddr prefers PORT, which is how Heroku (and most other PaaS hosts) tell
// a process where to listen.
func listenAddr() string {
	if port := os.Getenv("PORT"); port != "" {
		if !strings.Contains(port, ":") {
			return ":" + port
		}
		return port
	}
	return env("ADDR", ":8080")
}

// databaseURL normalizes what a provider hands us. Heroku Postgres supplies a
// URL with no sslmode and requires TLS, but presents a certificate signed by
// its own authority — so require encryption without verifying the chain, which
// is what "require" means to pgx.
func databaseURL(notes []string) (string, []string) {
	raw := env("DATABASE_URL", "postgres://clipboard:clipboard@localhost:5432/clipboard?sslmode=disable")

	u, err := url.Parse(raw)
	if err != nil {
		return raw, notes
	}
	query := u.Query()
	if query.Get("sslmode") != "" {
		return raw, notes
	}
	if isLocalHost(u.Hostname()) {
		return raw, notes
	}
	query.Set("sslmode", "require")
	u.RawQuery = query.Encode()
	return u.String(), append(notes, "DATABASE_URL had no sslmode and is not local, so sslmode=require was added")
}

func isLocalHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "db", "postgres", "":
		return true
	}
	return false
}

// blobBackend decides where uploaded bytes live. Disk is the right default for
// Docker Compose, where /data is a volume. A Heroku dyno has only an ephemeral
// filesystem that is wiped on every restart and deploy, so files there have to
// go in Postgres instead.
func blobBackend(dyno string, notes []string) (string, []string) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BLOB_BACKEND"))) {
	case BackendPostgres:
		return BackendPostgres, notes
	case BackendDisk:
		if dyno != "" {
			notes = append(notes, "BLOB_BACKEND=disk on a Heroku dyno: uploaded files will be lost on every restart and deploy")
		}
		return BackendDisk, notes
	case "":
		if dyno != "" {
			return BackendPostgres, append(notes,
				"detected a Heroku dyno, so uploads are stored in Postgres (the dyno filesystem is ephemeral)")
		}
		return BackendDisk, notes
	default:
		return BackendDisk, append(notes, "unrecognized BLOB_BACKEND, falling back to disk")
	}
}

func instanceID(dyno string) string {
	if dyno != "" {
		return dyno
	}
	if id := os.Getenv("INSTANCE_ID"); id != "" {
		return id
	}
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "instance"
	}
	return hex.EncodeToString(buf)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
