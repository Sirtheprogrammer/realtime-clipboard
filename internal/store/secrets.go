package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"clipboard/internal/models"
)

const secretColumns = `id, user_id, title, kind, username, url, encrypted_value, notes, created_at, updated_at`

func (s *Store) CreateSecret(ctx context.Context, sec models.Secret) (models.Secret, error) {
	if s.sqlite != nil {
		return s.createSecretSQLite(ctx, sec)
	}
	if sec.ID == "" {
		sec.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	sec.CreatedAt = now
	sec.UpdatedAt = now

	err := s.pool.QueryRow(ctx, `
		INSERT INTO secrets (id, user_id, title, kind, username, url, encrypted_value, notes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at, updated_at`,
		sec.ID, sec.UserID, sec.Title, sec.Kind, sec.Username, sec.URL, sec.EncryptedValue, sec.Notes, sec.CreatedAt, sec.UpdatedAt).
		Scan(&sec.CreatedAt, &sec.UpdatedAt)

	if err != nil {
		return sec, fmt.Errorf("create secret: %w", err)
	}
	return sec, nil
}

func (s *Store) CreateSecretsBatch(ctx context.Context, secrets []models.Secret) (int, error) {
	if s.sqlite != nil {
		return s.createSecretsBatchSQLite(ctx, secrets)
	}
	if len(secrets) == 0 {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
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

		_, err := tx.Exec(ctx, `
			INSERT INTO secrets (id, user_id, title, kind, username, url, encrypted_value, notes, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			sec.ID, sec.UserID, sec.Title, sec.Kind, sec.Username, sec.URL, sec.EncryptedValue, sec.Notes, sec.CreatedAt, sec.UpdatedAt)
		if err != nil {
			return inserted, fmt.Errorf("insert batch secret: %w", err)
		}
		inserted++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit batch secrets: %w", err)
	}

	return inserted, nil
}

func (s *Store) ListSecrets(ctx context.Context, userID string) ([]models.Secret, error) {
	if s.sqlite != nil {
		return s.listSecretsSQLite(ctx, userID)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+secretColumns+`
		FROM secrets
		WHERE user_id = $1
		ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
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

func (s *Store) GetSecret(ctx context.Context, userID, secretID string) (models.Secret, error) {
	if s.sqlite != nil {
		return s.getSecretSQLite(ctx, userID, secretID)
	}
	row := s.pool.QueryRow(ctx, `
		SELECT `+secretColumns+`
		FROM secrets
		WHERE id = $1 AND user_id = $2`, secretID, userID)

	sec, err := scanSecret(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return sec, ErrNotFound
	}
	return sec, err
}

func (s *Store) UpdateSecret(ctx context.Context, sec models.Secret) (models.Secret, error) {
	if s.sqlite != nil {
		return s.updateSecretSQLite(ctx, sec)
	}
	now := time.Now().UTC()
	err := s.pool.QueryRow(ctx, `
		UPDATE secrets
		SET title = $1, kind = $2, username = $3, url = $4, encrypted_value = $5, notes = $6, updated_at = $7
		WHERE id = $8 AND user_id = $9
		RETURNING updated_at`,
		sec.Title, sec.Kind, sec.Username, sec.URL, sec.EncryptedValue, sec.Notes, now, sec.ID, sec.UserID).
		Scan(&sec.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return sec, ErrNotFound
	}
	if err != nil {
		return sec, fmt.Errorf("update secret: %w", err)
	}
	return sec, nil
}

func (s *Store) DeleteSecret(ctx context.Context, userID, secretID string) error {
	if s.sqlite != nil {
		return s.deleteSecretSQLite(ctx, userID, secretID)
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM secrets
		WHERE id = $1 AND user_id = $2`, secretID, userID)
	if err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SearchSecretsByURL(ctx context.Context, userID, domainOrURL string) ([]models.Secret, error) {
	if s.sqlite != nil {
		return s.searchSecretsByURLSQLite(ctx, userID, domainOrURL)
	}
	// Match url containing the domain or starting with it
	pattern := "%" + domainOrURL + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT `+secretColumns+`
		FROM secrets
		WHERE user_id = $1 AND url ILIKE $2
		ORDER BY updated_at DESC`, userID, pattern)
	if err != nil {
		return nil, fmt.Errorf("search secrets by url: %w", err)
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

func scanSecret(s scanner) (models.Secret, error) {
	var sec models.Secret
	err := s.Scan(&sec.ID, &sec.UserID, &sec.Title, &sec.Kind, &sec.Username,
		&sec.URL, &sec.EncryptedValue, &sec.Notes, &sec.CreatedAt, &sec.UpdatedAt)
	return sec, err
}
