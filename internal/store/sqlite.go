package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"clipboard/internal/models"
)

// touchRoomSQLite handles room touch/creation on SQLite.
func (s *Store) touchRoomSQLite(ctx context.Context, code string) (models.Room, error) {
	var r models.Room
	now := time.Now().UTC()
	err := s.sqlite.QueryRowContext(ctx, `
		INSERT INTO rooms (code, created_at, last_seen)
		VALUES (?, ?, ?)
		ON CONFLICT (code) DO UPDATE SET last_seen = excluded.last_seen
		RETURNING code, created_at, last_seen`, code, now, now).
		Scan(&r.Code, &r.CreatedAt, &r.LastSeen)
	if err != nil {
		return r, fmt.Errorf("touch room sqlite: %w", err)
	}
	return r, nil
}

// createItemSQLite handles item creation on SQLite.
func (s *Store) createItemSQLite(ctx context.Context, it models.Item) (models.Item, error) {
	now := time.Now().UTC()
	it.CreatedAt = now
	_, err := s.sqlite.ExecContext(ctx, `
		INSERT INTO items (id, room_code, kind, content, file_name, mime_type,
			size_bytes, width, height, device, blob_path, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		it.ID, it.RoomCode, it.Kind, it.Content, it.FileName, it.MimeType,
		it.SizeBytes, it.Width, it.Height, it.Device, it.BlobPath, now, it.ExpiresAt)
	if err != nil {
		return it, fmt.Errorf("create item sqlite: %w", err)
	}
	return it, nil
}

// listItemsSQLite retrieves valid items in a room.
func (s *Store) listItemsSQLite(ctx context.Context, roomCode string, limit int) ([]models.Item, error) {
	rows, err := s.sqlite.QueryContext(ctx, `
		SELECT `+itemColumns+`
		FROM items
		WHERE room_code = ? AND expires_at > ?
		ORDER BY created_at DESC
		LIMIT ?`, roomCode, time.Now().UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list items sqlite: %w", err)
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

// getItemSQLite fetches a single item by id.
func (s *Store) getItemSQLite(ctx context.Context, id string) (models.Item, error) {
	row := s.sqlite.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items WHERE id = ?`, id)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}

// deleteItemSQLite deletes an item and returns its metadata.
func (s *Store) deleteItemSQLite(ctx context.Context, roomCode, id string) (models.Item, error) {
	it, err := s.getItemSQLite(ctx, id)
	if err != nil {
		return it, err
	}
	if it.RoomCode != roomCode {
		return it, ErrNotFound
	}
	res, err := s.sqlite.ExecContext(ctx, `DELETE FROM items WHERE id = ? AND room_code = ?`, id, roomCode)
	if err != nil {
		return it, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return it, ErrNotFound
	}
	return it, nil
}

// clearRoomSQLite deletes all items in a room and returns blob paths.
func (s *Store) clearRoomSQLite(ctx context.Context, roomCode string) ([]string, error) {
	rows, err := s.sqlite.QueryContext(ctx, `SELECT blob_path FROM items WHERE room_code = ? AND blob_path != ''`, roomCode)
	if err != nil {
		return nil, err
	}
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil && p != "" {
			paths = append(paths, p)
		}
	}
	rows.Close()

	_, err = s.sqlite.ExecContext(ctx, `DELETE FROM items WHERE room_code = ?`, roomCode)
	return paths, err
}

// deleteExpiredSQLite removes expired items and returns blob paths.
func (s *Store) deleteExpiredSQLite(ctx context.Context) ([]string, error) {
	now := time.Now().UTC()
	rows, err := s.sqlite.QueryContext(ctx, `SELECT blob_path FROM items WHERE expires_at <= ? AND blob_path != ''`, now)
	if err != nil {
		return nil, err
	}
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil && p != "" {
			paths = append(paths, p)
		}
	}
	rows.Close()

	_, err = s.sqlite.ExecContext(ctx, `DELETE FROM items WHERE expires_at <= ?`, now)
	return paths, err
}

// deleteEmptyRoomsSQLite deletes rooms with no activity and no items.
func (s *Store) deleteEmptyRoomsSQLite(ctx context.Context, idleFor time.Duration) error {
	cutoff := time.Now().UTC().Add(-idleFor)
	_, err := s.sqlite.ExecContext(ctx, `
		DELETE FROM rooms
		WHERE last_seen < ?
		  AND code NOT IN (SELECT DISTINCT room_code FROM items)`, cutoff)
	return err
}

// roomStatsSQLite calculates room stats on SQLite.
func (s *Store) roomStatsSQLite(ctx context.Context, roomCode string) (count int, bytes int64, err error) {
	err = s.sqlite.QueryRowContext(ctx, `
		SELECT count(*), coalesce(sum(size_bytes), 0)
		FROM items WHERE room_code = ? AND expires_at > ?`, roomCode, time.Now().UTC()).
		Scan(&count, &bytes)
	return
}

/* ───────────────────────── Users (SQLite) ───────────────────────── */

func (s *Store) createUserSQLite(ctx context.Context, email, passwordHash, githubID, githubUser, avatarURL string) (models.User, error) {
	id := uuid.New().String()
	now := time.Now().UTC()
	var ghID *string
	if githubID != "" {
		ghID = &githubID
	}

	_, err := s.sqlite.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, github_id, github_user, avatar_url, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, email, passwordHash, ghID, githubUser, avatarURL, now, now)
	if err != nil {
		return models.User{}, fmt.Errorf("create user sqlite: %w", err)
	}

	return models.User{
		ID:           id,
		Email:        email,
		PasswordHash: passwordHash,
		GitHubID:     githubID,
		GitHubUser:   githubUser,
		AvatarURL:    avatarURL,
		HasPassword:  passwordHash != "",
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (s *Store) getUserByEmailSQLite(ctx context.Context, email string) (models.User, error) {
	var u models.User
	var scannedGhID *string
	err := s.sqlite.QueryRowContext(ctx, `
		SELECT id, email, password_hash, github_id, github_user, avatar_url, created_at, updated_at
		FROM users
		WHERE lower(email) = lower(?)`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &scannedGhID, &u.GitHubUser, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	if scannedGhID != nil {
		u.GitHubID = *scannedGhID
	}
	u.HasPassword = u.PasswordHash != ""
	return u, nil
}

func (s *Store) getUserByGitHubSQLite(ctx context.Context, githubID string) (models.User, error) {
	var u models.User
	var scannedGhID *string
	err := s.sqlite.QueryRowContext(ctx, `
		SELECT id, email, password_hash, github_id, github_user, avatar_url, created_at, updated_at
		FROM users
		WHERE github_id = ?`, githubID).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &scannedGhID, &u.GitHubUser, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	if scannedGhID != nil {
		u.GitHubID = *scannedGhID
	}
	u.HasPassword = u.PasswordHash != ""
	return u, nil
}

func (s *Store) getUserByIDSQLite(ctx context.Context, id string) (models.User, error) {
	var u models.User
	var scannedGhID *string
	err := s.sqlite.QueryRowContext(ctx, `
		SELECT id, email, password_hash, github_id, github_user, avatar_url, created_at, updated_at
		FROM users
		WHERE id = ?`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &scannedGhID, &u.GitHubUser, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	if scannedGhID != nil {
		u.GitHubID = *scannedGhID
	}
	u.HasPassword = u.PasswordHash != ""
	return u, nil
}

func (s *Store) updateUserPasswordSQLite(ctx context.Context, userID, passwordHash string) error {
	now := time.Now().UTC()
	res, err := s.sqlite.ExecContext(ctx, `
		UPDATE users
		SET password_hash = ?, updated_at = ?
		WHERE id = ?`, passwordHash, now, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

/* ───────────────────────── Secrets (SQLite) ───────────────────────── */

func (s *Store) createSecretSQLite(ctx context.Context, sec models.Secret) (models.Secret, error) {
	if sec.ID == "" {
		sec.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	sec.CreatedAt = now
	sec.UpdatedAt = now

	_, err := s.sqlite.ExecContext(ctx, `
		INSERT INTO secrets (id, user_id, title, kind, username, url, encrypted_value, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sec.ID, sec.UserID, sec.Title, sec.Kind, sec.Username, sec.URL, sec.EncryptedValue, sec.Notes, now, now)
	if err != nil {
		return sec, fmt.Errorf("create secret sqlite: %w", err)
	}
	return sec, nil
}

func (s *Store) createSecretsBatchSQLite(ctx context.Context, secrets []models.Secret) (int, error) {
	tx, err := s.sqlite.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO secrets (id, user_id, title, kind, username, url, encrypted_value, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for _, sec := range secrets {
		if sec.ID == "" {
			sec.ID = uuid.New().String()
		}
		if sec.CreatedAt.IsZero() {
			sec.CreatedAt = now
		}
		if sec.UpdatedAt.IsZero() {
			sec.UpdatedAt = now
		}
		_, err := stmt.ExecContext(ctx, sec.ID, sec.UserID, sec.Title, sec.Kind, sec.Username, sec.URL, sec.EncryptedValue, sec.Notes, sec.CreatedAt, sec.UpdatedAt)
		if err != nil {
			return inserted, err
		}
		inserted++
	}
	return inserted, tx.Commit()
}

func (s *Store) listSecretsSQLite(ctx context.Context, userID string) ([]models.Secret, error) {
	rows, err := s.sqlite.QueryContext(ctx, `
		SELECT `+secretColumns+`
		FROM secrets
		WHERE user_id = ?
		ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var secrets []models.Secret
	for rows.Next() {
		sec, err := scanSecret(rows)
		if err != nil {
			return nil, err
		}
		secrets = append(secrets, sec)
	}
	return secrets, rows.Err()
}

func (s *Store) getSecretSQLite(ctx context.Context, userID, secretID string) (models.Secret, error) {
	row := s.sqlite.QueryRowContext(ctx, `
		SELECT `+secretColumns+`
		FROM secrets
		WHERE id = ? AND user_id = ?`, secretID, userID)
	sec, err := scanSecret(row)
	if errors.Is(err, sql.ErrNoRows) {
		return sec, ErrNotFound
	}
	return sec, err
}

func (s *Store) updateSecretSQLite(ctx context.Context, sec models.Secret) (models.Secret, error) {
	now := time.Now().UTC()
	sec.UpdatedAt = now
	res, err := s.sqlite.ExecContext(ctx, `
		UPDATE secrets
		SET title = ?, kind = ?, username = ?, url = ?, encrypted_value = ?, notes = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`,
		sec.Title, sec.Kind, sec.Username, sec.URL, sec.EncryptedValue, sec.Notes, now, sec.ID, sec.UserID)
	if err != nil {
		return sec, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sec, ErrNotFound
	}
	return sec, nil
}

func (s *Store) deleteSecretSQLite(ctx context.Context, userID, secretID string) error {
	res, err := s.sqlite.ExecContext(ctx, `DELETE FROM secrets WHERE id = ? AND user_id = ?`, secretID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) searchSecretsByURLSQLite(ctx context.Context, userID, domainOrURL string) ([]models.Secret, error) {
	pattern := "%" + domainOrURL + "%"
	rows, err := s.sqlite.QueryContext(ctx, `
		SELECT `+secretColumns+`
		FROM secrets
		WHERE user_id = ? AND lower(url) LIKE lower(?)
		ORDER BY updated_at DESC`, userID, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var secrets []models.Secret
	for rows.Next() {
		sec, err := scanSecret(rows)
		if err != nil {
			return nil, err
		}
		secrets = append(secrets, sec)
	}
	return secrets, rows.Err()
}

/* ───────────────────────── Sessions (SQLite) ───────────────────────── */

func (s *Store) createSessionSQLite(ctx context.Context, userID, token string, expiresAt time.Time) (models.Session, error) {
	now := time.Now().UTC()
	_, err := s.sqlite.ExecContext(ctx, `
		INSERT INTO sessions (token, user_id, created_at, expires_at)
		VALUES (?, ?, ?, ?)`, token, userID, now, expiresAt)
	if err != nil {
		return models.Session{}, fmt.Errorf("create session sqlite: %w", err)
	}
	return models.Session{
		Token:     token,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Store) getSessionSQLite(ctx context.Context, token string) (models.Session, error) {
	var sess models.Session
	err := s.sqlite.QueryRowContext(ctx, `
		SELECT token, user_id, created_at, expires_at
		FROM sessions
		WHERE token = ? AND expires_at > ?`, token, time.Now().UTC()).
		Scan(&sess.Token, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return sess, ErrNotFound
	}
	if err != nil {
		return sess, fmt.Errorf("get session sqlite: %w", err)
	}
	return sess, nil
}

func (s *Store) deleteSessionSQLite(ctx context.Context, token string) error {
	_, err := s.sqlite.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *Store) deleteExpiredSessionsSQLite(ctx context.Context) (int64, error) {
	res, err := s.sqlite.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
