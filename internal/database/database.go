package database

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schema string

// Pool is the connection pool the rest of the app passes around.
type Pool = pgxpool.Pool

// Connect dials Postgres, retrying while the container comes up, then applies
// the schema. Compose starts both services at once, so a cold start normally
// spends a few seconds here.
func Connect(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	// Hosted Postgres plans cap total connections (Heroku's smallest allows
	// 20), and every instance shares that budget.
	cfg.MaxConns = maxConns
	cfg.MaxConnIdleTime = 5 * time.Minute

	var pool *pgxpool.Pool
	deadline := time.Now().Add(60 * time.Second)
	for {
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			err = pool.Ping(ctx)
			if err == nil {
				break
			}
			pool.Close()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("connect to postgres: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}

	if err := applySchema(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// schemaLockKey is an arbitrary but fixed advisory-lock id ("clip" in hex).
const schemaLockKey int64 = 0x636c6970

// applySchema serializes startup migrations across instances.
//
// CREATE TABLE IF NOT EXISTS is not atomic: two processes running it at the
// same moment can both pass the existence check and then collide on the
// catalog's unique index. That is not hypothetical — every instance runs this
// on boot, and a platform restarting several at once (`heroku ps:scale web=2`,
// a compose stack coming up) hits it on a fresh database. An advisory lock
// makes the whole thing a queue instead of a race.
func applySchema(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire schema connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, schemaLockKey); err != nil {
		return fmt.Errorf("take schema lock: %w", err)
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx),
			`SELECT pg_advisory_unlock($1)`, schemaLockKey); err != nil {
			slog.Default().Error("release schema lock", "err", err)
		}
	}()

	if _, err := conn.Exec(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}
