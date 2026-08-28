package blob

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Disk keeps uploaded payloads on a mounted volume. This is the right backend
// wherever the filesystem outlives the process — Docker Compose, a VM, a
// Kubernetes PVC.
type Disk struct {
	root string
}

func NewDisk(root string) (*Disk, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create blob dir: %w", err)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Disk{root: abs}, nil
}

func (s *Disk) Describe() string { return "disk (" + s.root + ")" }

// Write streams r to disk under a path derived from id, capped at maxBytes.
func (s *Disk) Write(id string, r io.Reader, maxBytes int64) (string, int64, error) {
	rel := filepath.Join(id[:2], id)
	full := filepath.Join(s.root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", 0, err
	}

	f, err := os.Create(full)
	if err != nil {
		return "", 0, err
	}

	// One extra byte tells us the client blew past the limit. Close before any
	// cleanup: Windows refuses to unlink a file that still has an open handle.
	n, copyErr := io.Copy(f, io.LimitReader(r, maxBytes+1))
	closeErr := f.Close()

	switch {
	case copyErr != nil:
		os.Remove(full)
		return "", 0, copyErr
	case closeErr != nil:
		os.Remove(full)
		return "", 0, closeErr
	case n > maxBytes:
		os.Remove(full)
		return "", 0, ErrTooLarge
	}
	return filepath.ToSlash(rel), n, nil
}

func (s *Disk) Open(rel string) (io.ReadSeekCloser, error) {
	full, err := s.resolve(rel)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNotExist, rel)
		}
		return nil, err
	}
	return f, nil
}

func (s *Disk) Remove(rel string) error {
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
func (s *Disk) resolve(rel string) (string, error) {
	if rel == "" {
		return "", errors.New("empty blob path")
	}
	full := filepath.Join(s.root, filepath.FromSlash(rel))
	if !strings.HasPrefix(full, s.root+string(os.PathSeparator)) {
		return "", errors.New("blob path escapes root")
	}
	return full, nil
}
