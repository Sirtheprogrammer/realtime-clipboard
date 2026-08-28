package blob

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Store keeps uploaded payloads on a mounted volume; Postgres only holds the
// metadata, which keeps large pastes out of the database.
type Store struct {
	root string
}

func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create blob dir: %w", err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Store{root: abs}, nil
}

// Write streams r to disk under a path derived from id, capped at maxBytes.
// It returns the storage-relative path and the number of bytes written.
func (s *Store) Write(id string, r io.Reader, maxBytes int64) (string, int64, error) {
	rel := filepath.Join(id[:2], id)
	full := filepath.Join(s.root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", 0, err
	}

	f, err := os.Create(full)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	// One extra byte tells us the client blew past the limit.
	n, err := io.Copy(f, io.LimitReader(r, maxBytes+1))
	if err != nil {
		os.Remove(full)
		return "", 0, err
	}
	if n > maxBytes {
		os.Remove(full)
		return "", 0, ErrTooLarge
	}
	return filepath.ToSlash(rel), n, nil
}

var ErrTooLarge = errors.New("payload exceeds the upload limit")

func (s *Store) Open(rel string) (*os.File, error) {
	full, err := s.resolve(rel)
	if err != nil {
		return nil, err
	}
	return os.Open(full)
}

func (s *Store) Remove(rel string) error {
	full, err := s.resolve(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	os.Remove(filepath.Dir(full)) // best effort: drop the shard if it is empty
	return nil
}

// resolve guards against a stored path escaping the blob root.
func (s *Store) resolve(rel string) (string, error) {
	if rel == "" {
		return "", errors.New("empty blob path")
	}
	full := filepath.Join(s.root, filepath.FromSlash(rel))
	if !strings.HasPrefix(full, s.root+string(os.PathSeparator)) {
		return "", errors.New("blob path escapes root")
	}
	return full, nil
}
