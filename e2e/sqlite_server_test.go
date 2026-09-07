package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"clipboard/internal/api"
	"clipboard/internal/blob"
	"clipboard/internal/config"
	"clipboard/internal/database"
	"clipboard/internal/hub"
	"clipboard/internal/store"
	"log/slog"
)

func TestSQLiteZeroConfigServer(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	blobDir := filepath.Join(tmpDir, "blobs")
	keyFile := filepath.Join(tmpDir, "master.key")

	t.Setenv("DATABASE_URL", "sqlite://"+dbPath)
	t.Setenv("BLOB_DIR", blobDir)
	t.Setenv("KEY_FILE", keyFile)
	t.Setenv("SECRET_MASTER_KEY", "")

	cfg := config.Load()
	if cfg.DatabaseDriver != config.DriverSQLite {
		t.Fatalf("expected SQLite driver, got %s", cfg.DatabaseDriver)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := database.OpenSQLite(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer db.Close()

	st := store.NewSQLite(db)
	blobs, err := blob.NewDisk(cfg.BlobDir)
	if err != nil {
		t.Fatalf("failed to create disk blobs: %v", err)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	h := hub.New(log)
	srv := api.NewServer(cfg, st, blobs, h, nil, log)

	ts := httptest.NewServer(srv.Routes())
	defer ts.Close()

	client := ts.Client()

	// 1. Health check
	resp, err := client.Get(ts.URL + "/api/health")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Touch Room & Post item
	roomCode := api.NewRoomCode()
	itemBody := `{"content":"Testing zero-config SQLite!"}`
	resp, err = client.Post(ts.URL+"/api/rooms/"+roomCode+"/items", "application/json", bytes.NewBufferString(itemBody))
	if err != nil {
		t.Fatalf("create item failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 3. Register user (returns 201 Created)
	regPayload := `{"email":"test@zeroconfig.local","password":"MySuperSecretPassword123!"}`
	resp, err = client.Post(ts.URL+"/api/auth/register", "application/json", bytes.NewBufferString(regPayload))
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201 on register, got %d", resp.StatusCode)
	}
	var regResp struct {
		Token string `json:"token"`
	}
	json.NewDecoder(resp.Body).Decode(&regResp)
	resp.Body.Close()

	if regResp.Token == "" {
		t.Fatal("expected auth token")
	}

	// 4. Create Secret in Vault (returns 201 Created)
	secretPayload := `{"title":"Local Router Admin","kind":"password","username":"admin","url":"http://192.168.1.1","value":"RouterPassword99!","notes":"Local device"}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/secrets", bytes.NewBufferString(secretPayload))
	req.Header.Set("Authorization", "Bearer "+regResp.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("create secret failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201 on create secret, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 5. Lookup Secret by URL
	req, _ = http.NewRequest("GET", ts.URL+"/api/secrets/lookup?url=http://192.168.1.1/login", nil)
	req.Header.Set("Authorization", "Bearer "+regResp.Token)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("lookup secrets failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 on lookup, got %d", resp.StatusCode)
	}

	var lookupResp struct {
		Secrets []struct {
			Title    string `json:"title"`
			Username string `json:"username"`
			Value    string `json:"value"`
		} `json:"secrets"`
	}
	json.NewDecoder(resp.Body).Decode(&lookupResp)
	resp.Body.Close()

	if len(lookupResp.Secrets) != 1 {
		t.Fatalf("expected 1 decrypted secret from SQLite vault, got %d", len(lookupResp.Secrets))
	}
	if lookupResp.Secrets[0].Value != "RouterPassword99!" {
		t.Fatalf("expected decrypted secret 'RouterPassword99!', got %s", lookupResp.Secrets[0].Value)
	}
}
