package store_test

import (
	"context"
	"testing"
	"time"

	"clipboard/internal/database"
	"clipboard/internal/models"
	"clipboard/internal/store"
)

func TestSQLiteStore(t *testing.T) {
	ctx := context.Background()
	// Open in-memory SQLite
	db, err := database.OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer db.Close()

	st := store.NewSQLite(db)

	// 1. Rooms
	room, err := st.TouchRoom(ctx, "test-room")
	if err != nil {
		t.Fatalf("TouchRoom failed: %v", err)
	}
	if room.Code != "test-room" {
		t.Errorf("expected room code 'test-room', got '%s'", room.Code)
	}

	// 2. Items
	now := time.Now().UTC()
	item := models.Item{
		ID:        "item-1",
		RoomCode:  "test-room",
		Kind:      "text",
		Content:   "hello world",
		ExpiresAt: now.Add(1 * time.Hour),
	}
	createdItem, err := st.CreateItem(ctx, item)
	if err != nil {
		t.Fatalf("CreateItem failed: %v", err)
	}
	if createdItem.ID != "item-1" {
		t.Errorf("expected item id 'item-1', got '%s'", createdItem.ID)
	}

	// List items
	items, err := st.ListItems(ctx, "test-room", 10)
	if err != nil {
		t.Fatalf("ListItems failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Content != "hello world" {
		t.Errorf("expected content 'hello world', got '%s'", items[0].Content)
	}

	// 3. Users
	user, err := st.CreateUser(ctx, "user@example.com", "hashed_pw", "", "", "")
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	if user.Email != "user@example.com" {
		t.Errorf("expected email 'user@example.com', got '%s'", user.Email)
	}

	// Get user by email (case-insensitive)
	fetchedUser, err := st.GetUserByEmail(ctx, "USER@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetchedUser.ID != user.ID {
		t.Errorf("expected user ID '%s', got '%s'", user.ID, fetchedUser.ID)
	}

	// 4. Secrets
	sec := models.Secret{
		UserID:         user.ID,
		Title:          "GitHub Account",
		Kind:           "password",
		Username:       "octocat",
		URL:            "https://github.com/login",
		EncryptedValue: "enc_data_123",
		Notes:          "main work account",
	}
	createdSec, err := st.CreateSecret(ctx, sec)
	if err != nil {
		t.Fatalf("CreateSecret failed: %v", err)
	}
	if createdSec.ID == "" {
		t.Fatal("expected non-empty secret ID")
	}

	// List secrets
	secrets, err := st.ListSecrets(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListSecrets failed: %v", err)
	}
	if len(secrets) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(secrets))
	}

	// Search secrets by URL
	matches, err := st.SearchSecretsByURL(ctx, user.ID, "github.com")
	if err != nil {
		t.Fatalf("SearchSecretsByURL failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match for 'github.com', got %d", len(matches))
	}

	// Update secret
	createdSec.Title = "GitHub Account (Updated)"
	updatedSec, err := st.UpdateSecret(ctx, createdSec)
	if err != nil {
		t.Fatalf("UpdateSecret failed: %v", err)
	}
	if updatedSec.Title != "GitHub Account (Updated)" {
		t.Errorf("expected updated title, got '%s'", updatedSec.Title)
	}

	// Delete secret
	err = st.DeleteSecret(ctx, user.ID, createdSec.ID)
	if err != nil {
		t.Fatalf("DeleteSecret failed: %v", err)
	}

	secretsAfterDelete, err := st.ListSecrets(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListSecrets after delete failed: %v", err)
	}
	if len(secretsAfterDelete) != 0 {
		t.Fatalf("expected 0 secrets after delete, got %d", len(secretsAfterDelete))
	}
}
