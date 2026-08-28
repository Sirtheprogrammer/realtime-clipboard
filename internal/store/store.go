package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"clipboard/internal/models"
)

var ErrNotFound = errors.New("not found")

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const itemColumns = `id, room_code, kind, content, file_name, mime_type,
	size_bytes, width, height, device, blob_path, created_at, expires_at`

// TouchRoom creates the room if it is new and refreshes its last_seen stamp.
func (s *Store) TouchRoom(ctx context.Context, code string) (models.Room, error) {
	var r models.Room
	err := s.pool.QueryRow(ctx, `
		INSERT INTO rooms (code) VALUES ($1)
		ON CONFLICT (code) DO UPDATE SET last_seen = now()
		RETURNING code, created_at, last_seen`, code).
		Scan(&r.Code, &r.CreatedAt, &r.LastSeen)
	if err != nil {
		return r, fmt.Errorf("touch room: %w", err)
	}
	return r, nil
}

func (s *Store) CreateItem(ctx context.Context, it models.Item) (models.Item, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO items (id, room_code, kind, content, file_name, mime_type,
			size_bytes, width, height, device, blob_path, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING created_at`,
		it.ID, it.RoomCode, it.Kind, it.Content, it.FileName, it.MimeType,
		it.SizeBytes, it.Width, it.Height, it.Device, it.BlobPath, it.ExpiresAt).
		Scan(&it.CreatedAt)
	if err != nil {
		return it, fmt.Errorf("create item: %w", err)
	}
	return it, nil
}

func (s *Store) ListItems(ctx context.Context, roomCode string, limit int) ([]models.Item, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+itemColumns+`
		FROM items
		WHERE room_code = $1 AND expires_at > now()
		ORDER BY created_at DESC
		LIMIT $2`, roomCode, limit)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()

	items := make([]models.Item, 0, limit)
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func (s *Store) GetItem(ctx context.Context, id string) (models.Item, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+itemColumns+` FROM items WHERE id = $1`, id)
	it, err := scanItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

// DeleteItem removes one item and returns it so the caller can unlink its blob.
func (s *Store) DeleteItem(ctx context.Context, roomCode, id string) (models.Item, error) {
	row := s.pool.QueryRow(ctx, `
		DELETE FROM items WHERE id = $1 AND room_code = $2
		RETURNING `+itemColumns, id, roomCode)
	it, err := scanItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

// ClearRoom empties a room and returns the blob paths that are now orphaned.
func (s *Store) ClearRoom(ctx context.Context, roomCode string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`DELETE FROM items WHERE room_code = $1 RETURNING blob_path`, roomCode)
	if err != nil {
		return nil, fmt.Errorf("clear room: %w", err)
	}
	defer rows.Close()
	return collectPaths(rows)
}

// DeleteExpired drops items past their TTL and returns their blob paths.
func (s *Store) DeleteExpired(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`DELETE FROM items WHERE expires_at <= now() RETURNING blob_path`)
	if err != nil {
		return nil, fmt.Errorf("delete expired: %w", err)
	}
	defer rows.Close()
	return collectPaths(rows)
}

// DeleteEmptyRooms prunes rooms nobody has touched and that hold no items.
func (s *Store) DeleteEmptyRooms(ctx context.Context, idleFor time.Duration) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM rooms r
		WHERE r.last_seen < now() - $1::interval
		  AND NOT EXISTS (SELECT 1 FROM items i WHERE i.room_code = r.code)`,
		idleFor.String())
	return err
}

func (s *Store) RoomStats(ctx context.Context, roomCode string) (count int, bytes int64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT count(*), coalesce(sum(size_bytes), 0)
		FROM items WHERE room_code = $1 AND expires_at > now()`, roomCode).
		Scan(&count, &bytes)
	return
}

type scanner interface {
	Scan(dest ...any) error
}

func scanItem(s scanner) (models.Item, error) {
	var it models.Item
	err := s.Scan(&it.ID, &it.RoomCode, &it.Kind, &it.Content, &it.FileName,
		&it.MimeType, &it.SizeBytes, &it.Width, &it.Height, &it.Device,
		&it.BlobPath, &it.CreatedAt, &it.ExpiresAt)
	return it, err
}

func collectPaths(rows pgx.Rows) ([]string, error) {
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, rows.Err()
}
