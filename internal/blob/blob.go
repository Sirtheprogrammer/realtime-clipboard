// Package blob stores the bytes behind an uploaded item. Metadata always lives
// in Postgres; the payload goes wherever the deployment can keep it.
package blob

import (
	"errors"
	"io"
)

// ErrTooLarge is returned when a payload runs past the configured limit.
var ErrTooLarge = errors.New("payload exceeds the upload limit")

// Store is the contract both backends implement.
//
// Open returns a ReadSeekCloser because item downloads go through
// http.ServeContent, which needs to seek in order to answer range requests —
// that is what lets a browser scrub a video or resume a download.
type Store interface {
	// Write streams r into storage under a key derived from id, refusing
	// anything larger than maxBytes. It returns the path to hand to Open later
	// and the number of bytes stored.
	Write(id string, r io.Reader, maxBytes int64) (path string, size int64, err error)

	// Open returns the stored payload. A missing payload reports an error for
	// which IsNotExist returns true.
	Open(path string) (io.ReadSeekCloser, error)

	// Remove deletes the payload. Removing something already gone is not an
	// error: a user delete and the expiry janitor can race.
	Remove(path string) error

	// Describe names the backend for startup logs.
	Describe() string
}

// IsNotExist reports whether err means "that payload is not here", across both
// backends.
func IsNotExist(err error) bool {
	return errors.Is(err, ErrNotExist)
}

// ErrNotExist is the backend-independent "no such payload".
var ErrNotExist = errors.New("blob does not exist")
