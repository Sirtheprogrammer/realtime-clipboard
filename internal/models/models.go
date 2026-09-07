package models

import "time"

// Item kinds understood by both the API and the dashboard.
const (
	KindText  = "text"
	KindLink  = "link"
	KindImage = "image"
	KindFile  = "file"
)

// Secret kinds.
const (
	SecretKindPassword = "password"
	SecretKindAPIKey   = "api_key"
	SecretKindToken    = "token"
	SecretKindNote     = "note"
)

type Room struct {
	Code      string    `json:"code"`
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
}

type Item struct {
	ID        string    `json:"id"`
	RoomCode  string    `json:"room_code"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content,omitempty"`
	FileName  string    `json:"file_name,omitempty"`
	MimeType  string    `json:"mime_type,omitempty"`
	SizeBytes int64     `json:"size_bytes,omitempty"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	Device    string    `json:"device"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`

	// blobPath never leaves the server.
	BlobPath string `json:"-"`
}

// HasBlob reports whether the item is backed by a file on disk.
func (i Item) HasBlob() bool { return i.BlobPath != "" }

// User represents an authenticated account.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	GitHubID     string    `json:"github_id,omitempty"`
	GitHubUser   string    `json:"github_user,omitempty"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Session represents an active login token.
type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Secret represents an encrypted credential or note in the secret store.
type Secret struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	Title          string    `json:"title"`
	Kind           string    `json:"kind"`
	Username       string    `json:"username,omitempty"`
	URL            string    `json:"url,omitempty"`
	EncryptedValue string    `json:"-"`
	Value          string    `json:"value,omitempty"` // Decrypted in memory when returned to authorized user
	Notes          string    `json:"notes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
