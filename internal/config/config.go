package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Blob backends.
const (
	BackendDisk     = "disk"
	BackendPostgres = "postgres"
)

// Database drivers.
const (
	DriverPostgres = "postgres"
	DriverSQLite   = "sqlite"
)

// Config holds every knob the server reads from the environment.
type Config struct {
	Addr           string
	DatabaseDriver string
	DatabaseURL    string
	BlobBackend    string
	BlobDir        string
	WebDir         string
	MaxUploadBytes int64
	MaxDBConns     int32
	Retention      time.Duration
	SweepInterval  time.Duration
	AllowedOrigins string

	// Secret Store & Auth
	SecretMasterKey    [32]byte
	GitHubClientID     string
	GitHubClientSecret string
	BaseURL            string

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
		MaxDBConns:         int32(max(envInt64("DB_MAX_CONNS", 10), 2)),
		Retention:          envDuration("RETENTION", 24*time.Hour),
		SweepInterval:      envDuration("SWEEP_INTERVAL", 5*time.Minute),
		AllowedOrigins:     env("ALLOWED_ORIGINS", "*"),
		GitHubClientID:     env("GITHUB_CLIENT_ID", ""),
		GitHubClientSecret: env("GITHUB_CLIENT_SECRET", ""),
		BaseURL:            strings.TrimSuffix(env("BASE_URL", ""), "/"),
	}

	dyno := os.Getenv("DYNO")

	cfg.Addr = listenAddr()
	cfg.DatabaseDriver, cfg.DatabaseURL, cfg.Notes = databaseConfig(cfg.Notes)
	cfg.BlobBackend, cfg.Notes = blobBackend(dyno, cfg.Notes)
	cfg.InstanceID = instanceID(dyno)
	cfg.SecretMasterKey, cfg.Notes = masterKey(cfg.Notes)

	return cfg
}

func databaseConfig(notes []string) (string, string, []string) {
	raw := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if raw == "" {
		sqlitePath := env("SQLITE_PATH", "./data/clipboard.db")
		notes = append(notes, "No DATABASE_URL configured; running in zero-config mode with embedded SQLite ("+sqlitePath+")")
		return DriverSQLite, sqlitePath, notes
	}

	if strings.HasPrefix(raw, "postgres://") || strings.HasPrefix(raw, "postgresql://") {
		u, notes2 := databaseURL(notes)
		return DriverPostgres, u, notes2
	}

	clean := strings.TrimPrefix(raw, "sqlite://")
	notes = append(notes, "Using SQLite database at "+clean)
	return DriverSQLite, clean, notes
}

func masterKey(notes []string) ([32]byte, []string) {
	raw := os.Getenv("SECRET_MASTER_KEY")
	if raw != "" {
		if len(raw) == 64 {
			if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == 32 {
				var k [32]byte
				copy(k[:], decoded)
				return k, notes
			}
		}
		// Derive 32 bytes from arbitrary string
		hash := sha256.Sum256([]byte(raw))
		return hash, notes
	}

	// Persistent key file for zero-config mode
	keyPath := env("KEY_FILE", "./data/master.key")
	if data, err := os.ReadFile(keyPath); err == nil && len(data) >= 32 {
		var k [32]byte
		copy(k[:], data[:32])
		notes = append(notes, "Loaded persistent master encryption key from "+keyPath)
		return k, notes
	}

	// Auto-generate a secure random 32-byte key and persist it
	var randKey [32]byte
	if _, err := rand.Read(randKey[:]); err == nil {
		if dir := filepath.Dir(keyPath); dir != "" && dir != "." {
			_ = os.MkdirAll(dir, 0o700)
		}
		if err := os.WriteFile(keyPath, randKey[:], 0o600); err == nil {
			notes = append(notes, "Generated and saved persistent master encryption key to "+keyPath)
			return randKey, notes
		}
	}

	// Fallback for local development
	const devKey = "clipboard-dev-secret-master-key-v1"
	hash := sha256.Sum256([]byte(devKey))
	return hash, append(notes, "SECRET_MASTER_KEY not set; using development default key")
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
// Docker Compose or SQLite, where /data is local/persistent. A Heroku dyno has only an ephemeral
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
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
