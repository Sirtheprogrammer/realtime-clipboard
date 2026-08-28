package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds every knob the server reads from the environment.
type Config struct {
	Addr           string
	DatabaseURL    string
	BlobDir        string
	WebDir         string
	MaxUploadBytes int64
	Retention      time.Duration
	SweepInterval  time.Duration
	AllowedOrigins string
}

func Load() Config {
	return Config{
		Addr:           env("ADDR", ":8080"),
		DatabaseURL:    env("DATABASE_URL", "postgres://clipboard:clipboard@localhost:5432/clipboard?sslmode=disable"),
		BlobDir:        env("BLOB_DIR", "./data/blobs"),
		WebDir:         env("WEB_DIR", "./web"),
		MaxUploadBytes: envInt64("MAX_UPLOAD_BYTES", 100<<20), // 100 MiB
		Retention:      envDuration("RETENTION", 24*time.Hour),
		SweepInterval:  envDuration("SWEEP_INTERVAL", 5*time.Minute),
		AllowedOrigins: env("ALLOWED_ORIGINS", "*"),
	}
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
