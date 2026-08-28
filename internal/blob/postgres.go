package blob

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// chunkSize is the unit a payload is split into. Every chunk except the last is
// exactly this size, which is what makes seeking pure arithmetic rather than a
// lookup table.
const chunkSize = 1 << 20 // 1 MiB

// Postgres stores payloads as rows in the database. It is slower and more
// expensive than a volume, and it exists for hosts with no durable filesystem —
// a Heroku dyno being the case in point, since its disk is wiped on every
// restart and deploy.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (s *Postgres) Describe() string { return "postgres (blob_chunks table)" }

// Write streams r into chunk rows inside one transaction, so a failed or
// oversize upload leaves nothing behind.
func (s *Postgres) Write(id string, r io.Reader, maxBytes int64) (string, int64, error) {
	ctx := context.Background()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", 0, fmt.Errorf("begin blob write: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var total int64
	buf := make([]byte, chunkSize)

	for seq := 0; ; seq++ {
		n, readErr := io.ReadFull(r, buf)
		if n > 0 {
			total += int64(n)
			if total > maxBytes {
				return "", 0, ErrTooLarge
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO blob_chunks (blob_id, seq, data) VALUES ($1, $2, $3)`,
				id, seq, buf[:n]); err != nil {
				return "", 0, fmt.Errorf("write blob chunk: %w", err)
			}
		}
		if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
			break // ErrUnexpectedEOF is the normal short final chunk
		}
		if readErr != nil {
			return "", 0, readErr
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", 0, fmt.Errorf("commit blob write: %w", err)
	}
	return id, total, nil
}

func (s *Postgres) Open(path string) (io.ReadSeekCloser, error) {
	if path == "" {
		return nil, errors.New("empty blob path")
	}
	ctx := context.Background()

	var size int64
	var chunks int
	err := s.pool.QueryRow(ctx,
		`SELECT coalesce(sum(length(data)), 0), count(*) FROM blob_chunks WHERE blob_id = $1`,
		path).Scan(&size, &chunks)
	if err != nil {
		return nil, fmt.Errorf("stat blob: %w", err)
	}
	if chunks == 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotExist, path)
	}
	return &chunkReader{pool: s.pool, id: path, size: size, chunk: -1}, nil
}

func (s *Postgres) Remove(path string) error {
	if path == "" {
		return nil
	}
	_, err := s.pool.Exec(context.Background(),
		`DELETE FROM blob_chunks WHERE blob_id = $1`, path)
	return err
}

// chunkReader presents the chunk rows of one blob as a seekable stream, holding
// at most a single chunk in memory at a time.
type chunkReader struct {
	pool *pgxpool.Pool
	id   string
	size int64
	pos  int64

	chunk int // index of the chunk currently in buf, -1 when none
	buf   []byte
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.pos >= r.size {
		return 0, io.EOF
	}
	want := int(r.pos / chunkSize)
	if want != r.chunk {
		if err := r.load(want); err != nil {
			return 0, err
		}
	}
	offset := int(r.pos % chunkSize)
	if offset >= len(r.buf) {
		return 0, io.EOF
	}
	n := copy(p, r.buf[offset:])
	r.pos += int64(n)
	return n, nil
}

func (r *chunkReader) load(seq int) error {
	var data []byte
	err := r.pool.QueryRow(context.Background(),
		`SELECT data FROM blob_chunks WHERE blob_id = $1 AND seq = $2`, r.id, seq).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s chunk %d", ErrNotExist, r.id, seq)
	}
	if err != nil {
		return fmt.Errorf("read blob chunk: %w", err)
	}
	r.chunk, r.buf = seq, data
	return nil
}

func (r *chunkReader) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.pos + offset
	case io.SeekEnd:
		next = r.size + offset
	default:
		return 0, fmt.Errorf("invalid whence %d", whence)
	}
	if next < 0 {
		return 0, errors.New("negative seek position")
	}
	r.pos = next
	return next, nil
}

func (r *chunkReader) Close() error {
	r.buf, r.chunk = nil, -1
	return nil
}
