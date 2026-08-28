package models

import "time"

// Item kinds understood by both the API and the dashboard.
const (
	KindText  = "text"
	KindLink  = "link"
	KindImage = "image"
	KindFile  = "file"
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
