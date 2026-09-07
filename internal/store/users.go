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

func (s *Store) CreateUser(ctx context.Context, email, passwordHash, githubID, githubUser, avatarURL string) (models.User, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	var ghID *string
	if githubID != "" {
		ghID = &githubID
	}

	var u models.User
	var scannedGhID *string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (id, email, password_hash, github_id, github_user, avatar_url, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, email, password_hash, github_id, github_user, avatar_url, created_at, updated_at`,
		id, email, passwordHash, ghID, githubUser, avatarURL, now, now).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &scannedGhID, &u.GitHubUser, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)

	if err != nil {
		return u, fmt.Errorf("create user: %w", err)
	}
	if scannedGhID != nil {
		u.GitHubID = *scannedGhID
	}
	return u, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	var u models.User
	var scannedGhID *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, github_id, github_user, avatar_url, created_at, updated_at
		FROM users
		WHERE lower(email) = lower($1)`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &scannedGhID, &u.GitHubUser, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, fmt.Errorf("get user by email: %w", err)
	}
	if scannedGhID != nil {
		u.GitHubID = *scannedGhID
	}
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id string) (models.User, error) {
	var u models.User
	var scannedGhID *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, github_id, github_user, avatar_url, created_at, updated_at
		FROM users
		WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &scannedGhID, &u.GitHubUser, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, fmt.Errorf("get user by id: %w", err)
	}
	if scannedGhID != nil {
		u.GitHubID = *scannedGhID
	}
	return u, nil
}

func (s *Store) GetUserByGitHubID(ctx context.Context, githubID string) (models.User, error) {
	var u models.User
	var scannedGhID *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, github_id, github_user, avatar_url, created_at, updated_at
		FROM users
		WHERE github_id = $1`, githubID).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &scannedGhID, &u.GitHubUser, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, fmt.Errorf("get user by github id: %w", err)
	}
	if scannedGhID != nil {
		u.GitHubID = *scannedGhID
	}
	return u, nil
}

func (s *Store) LinkGitHubAccount(ctx context.Context, userID, githubID, githubUser, avatarURL string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users
		SET github_id = $1, github_user = $2, avatar_url = $3, updated_at = now()
		WHERE id = $4`, githubID, githubUser, avatarURL, userID)
	return err
}

func (s *Store) CreateSession(ctx context.Context, userID, token string, expiresAt time.Time) (models.Session, error) {
	var sess models.Session
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sessions (token, user_id, expires_at)
		VALUES ($1, $2, $3)
		RETURNING token, user_id, created_at, expires_at`,
		token, userID, expiresAt).
		Scan(&sess.Token, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt)

	if err != nil {
		return sess, fmt.Errorf("create session: %w", err)
	}
	return sess, nil
}

func (s *Store) GetSession(ctx context.Context, token string) (models.Session, error) {
	var sess models.Session
	err := s.pool.QueryRow(ctx, `
		SELECT token, user_id, created_at, expires_at
		FROM sessions
		WHERE token = $1 AND expires_at > now()`, token).
		Scan(&sess.Token, &sess.UserID, &sess.CreatedAt, &sess.ExpiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return sess, ErrNotFound
	}
	if err != nil {
		return sess, fmt.Errorf("get session: %w", err)
	}
	return sess, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token = $1`, token)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
