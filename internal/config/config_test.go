package config

import (
	"net/url"
	"strings"
	"testing"
)

func TestListenAddrPrefersPort(t *testing.T) {
	// Heroku assigns a port and expects the process to bind it.
	t.Setenv("PORT", "43210")
	t.Setenv("ADDR", ":9999")
	if got := listenAddr(); got != ":43210" {
		t.Errorf("listenAddr() = %q, want :43210", got)
	}

	t.Setenv("PORT", "0.0.0.0:43210")
	if got := listenAddr(); got != "0.0.0.0:43210" {
		t.Errorf("listenAddr() = %q, want the host:port as given", got)
	}

	t.Setenv("PORT", "")
	if got := listenAddr(); got != ":9999" {
		t.Errorf("listenAddr() = %q, want ADDR's :9999", got)
	}
}

func TestDatabaseURLAddsSSLModeForRemoteHosts(t *testing.T) {
	// A Heroku Postgres URL arrives with credentials and no sslmode.
	t.Setenv("DATABASE_URL", "postgres://u:p@ec2-1-2-3-4.compute.amazonaws.com:5432/d1234")
	got, notes := databaseURL(nil)

	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("result is not a URL: %v", err)
	}
	if mode := parsed.Query().Get("sslmode"); mode != "require" {
		t.Errorf("sslmode = %q, want require", mode)
	}
	if parsed.User.String() == "" || parsed.Host == "" {
		t.Errorf("rewriting lost part of the URL: %q", got)
	}
	if len(notes) != 1 {
		t.Errorf("expected one explanatory note, got %v", notes)
	}
}

func TestDatabaseURLLeavesExplicitAndLocalAlone(t *testing.T) {
	cases := map[string]string{
		"explicit sslmode is honored":     "postgres://u:p@example.com:5432/d?sslmode=verify-full",
		"a compose service is not remote": "postgres://clipboard:clipboard@db:5432/clipboard?sslmode=disable",
		"localhost is not remote":         "postgres://clipboard@localhost:5432/clipboard",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", in)
			got, notes := databaseURL(nil)
			if got != in {
				t.Errorf("URL was rewritten to %q", got)
			}
			if len(notes) != 0 {
				t.Errorf("unexpected notes: %v", notes)
			}
		})
	}
}

func TestBlobBackendDefaultsToPostgresOnHeroku(t *testing.T) {
	t.Setenv("BLOB_BACKEND", "")
	backend, notes := blobBackend("web.1", nil)
	if backend != BackendPostgres {
		t.Errorf("backend = %q, want postgres: a dyno's filesystem is ephemeral", backend)
	}
	if len(notes) == 0 {
		t.Error("the switch to Postgres should be explained in the log")
	}

	backend, notes = blobBackend("", nil)
	if backend != BackendDisk {
		t.Errorf("backend = %q, want disk off-Heroku", backend)
	}
	if len(notes) != 0 {
		t.Errorf("unexpected notes: %v", notes)
	}
}

func TestBlobBackendWarnsWhenDiskIsForcedOnHeroku(t *testing.T) {
	t.Setenv("BLOB_BACKEND", "disk")
	backend, notes := blobBackend("web.1", nil)
	if backend != BackendDisk {
		t.Errorf("backend = %q, want the explicit disk choice honored", backend)
	}
	if len(notes) == 0 || !strings.Contains(notes[0], "lost") {
		t.Errorf("expected a warning about data loss, got %v", notes)
	}
}

func TestMaxDBConnsNeverDropsBelowTwo(t *testing.T) {
	// One connection is held open for the whole process to listen for events
	// from other instances, so a pool of one would deadlock.
	t.Setenv("DB_MAX_CONNS", "1")
	if got := Load().MaxDBConns; got < 2 {
		t.Errorf("MaxDBConns = %d, want at least 2", got)
	}
}

func TestInstanceIDPrefersDyno(t *testing.T) {
	if got := instanceID("web.3"); got != "web.3" {
		t.Errorf("instanceID = %q, want the dyno name", got)
	}
	t.Setenv("INSTANCE_ID", "")
	a, b := instanceID(""), instanceID("")
	if a == b {
		t.Error("generated instance ids should differ between processes")
	}
}
