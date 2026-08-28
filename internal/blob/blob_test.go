package blob

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteReadRemove(t *testing.T) {
	store := newTestStore(t)

	const id = "0123abcd-0000-0000-0000-000000000000"
	payload := "hello clipboard"

	path, n, err := store.Write(id, strings.NewReader(payload), 1024)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != int64(len(payload)) {
		t.Fatalf("wrote %d bytes, want %d", n, len(payload))
	}
	if want := "01/" + id; path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	f, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != payload {
		t.Fatalf("read back %q, want %q", got, payload)
	}

	if err := store.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := store.Open(path); !os.IsNotExist(err) {
		t.Fatalf("expected the blob to be gone, got err=%v", err)
	}
	// Removing twice is not an error; the janitor and a user delete can race.
	if err := store.Remove(path); err != nil {
		t.Fatalf("second remove: %v", err)
	}
}

func TestWriteRejectsOversizePayload(t *testing.T) {
	store := newTestStore(t)
	const id = "ffff0000-0000-0000-0000-000000000000"

	_, _, err := store.Write(id, strings.NewReader(strings.Repeat("x", 100)), 32)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}

	// The partial file must not be left behind.
	if entries, _ := os.ReadDir(filepath.Join(storeRoot(t, store), "ff")); len(entries) != 0 {
		t.Fatalf("partial upload left %d files behind", len(entries))
	}
}

func TestResolveRejectsEscapes(t *testing.T) {
	store := newTestStore(t)
	for _, bad := range []string{"", "../secret", "ab/../../secret", "/etc/passwd"} {
		if _, err := store.Open(bad); err == nil {
			t.Errorf("Open(%q) should not have resolved", bad)
		}
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store
}

func storeRoot(t *testing.T, s *Store) string {
	t.Helper()
	return s.root
}
