package database

import (
	"strings"
	"testing"
)

func TestSchemaSQLValidity(t *testing.T) {
	if strings.TrimSpace(schema) == "" {
		t.Fatal("embedded schema.sql is empty")
	}

	// Heroku syntax error check: Ensure no literal backslash escape sequences exist
	// (such as raw `\n` or `\r` instead of real newlines, or psql meta-commands).
	if strings.Contains(schema, `\n`) {
		t.Errorf("schema contains literal backslash-n escape sequence ('\\n') which causes syntax error at or near '\\'")
	}
	if strings.Contains(schema, `\r`) {
		t.Errorf("schema contains literal backslash-r escape sequence ('\\r')")
	}

	// Verify mandatory tables exist
	requiredTables := []string{"rooms", "items", "blob_chunks", "users", "sessions", "secrets"}
	for _, tbl := range requiredTables {
		if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS "+tbl) {
			t.Errorf("expected table %s in schema", tbl)
		}
	}
}
