package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema_sqlite.sql
var schemaSQLite string

// OpenSQLite opens an embedded SQLite database, ensures its directory exists,
// sets high-concurrency PRAGMAs (WAL mode, busy timeout, foreign keys),
// and applies the database schema automatically.
func OpenSQLite(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" {
		path = "./data/clipboard.db"
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite dir: %w", err)
		}
	}

	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite db: %w", err)
	}

	// SQLite handles concurrent reads well in WAL mode, but writes are serialized.
	// Cap open connections to avoid lock contention.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite db: %w", err)
	}

	if _, err := db.ExecContext(ctx, schemaSQLite); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply sqlite schema: %w", err)
	}

	return db, nil
}
