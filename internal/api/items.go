package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"clipboard/internal/blob"
	"clipboard/internal/models"
	"clipboard/internal/store"
)

const (
	maxTextBytes = 512 << 10 // 512 KiB of pasted text
	itemPageSize = 200
)

func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	code := NewRoomCode()
	room, err := s.store.TouchRoom(r.Context(), code)
	if err != nil {
		s.log.Error("create room", "err", err)
		writeError(w, http.StatusInternalServerError, "could not create room")
		return
	}
	writeJSON(w, http.StatusCreated, room)
}

func (s *Server) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	code, ok := NormalizeRoomCode(r.PathValue("code"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid room code")
		return
	}
	room, err := s.store.TouchRoom(r.Context(), code)
	if err != nil {
		s.log.Error("touch room", "err", err)
		writeError(w, http.StatusInternalServerError, "could not open room")
		return
	}
	items, err := s.store.ListItems(r.Context(), code, itemPageSize)
	if err != nil {
		s.log.Error("list items", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load items")
		return
	}
	count, bytes, err := s.store.RoomStats(r.Context(), code)
	if err != nil {
		s.log.Error("room stats", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room":  room,
		"items": items,
		"peers": s.hub.Peers(code),
		"stats": map[string]any{"items": count, "bytes": bytes},
		"limits": map[string]any{
			"max_upload_bytes": s.cfg.MaxUploadBytes,
			"retention":        s.cfg.Retention.String(),
		},
	})
}

type createTextRequest struct {
	Content string `json:"content"`
	Device  string `json:"device"`
}

func (s *Server) handleCreateTextItem(w http.ResponseWriter, r *http.Request) {
	code, ok := NormalizeRoomCode(r.PathValue("code"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid room code")
		return
	}
	var req createTextRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxTextBytes+1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	item, err := s.createTextItem(r.Context(), code, req.Content, deviceOf(r, req.Device))
	if err != nil {
		s.writeCreateError(w, err)
		return
	}
	s.hub.Broadcast(code, "item.created", item, "")
	writeJSON(w, http.StatusCreated, item)
}

// createTextItem is shared by the REST route and the websocket handler so a
// pasted string takes the same path either way.
func (s *Server) createTextItem(ctx context.Context, roomCode, content, device string) (models.Item, error) {
	content = strings.TrimRight(content, "\r\n")
	if strings.TrimSpace(content) == "" {
		return models.Item{}, errEmptyContent
	}
	if len(content) > maxTextBytes {
		return models.Item{}, errTooLarge
	}
	if _, err := s.store.TouchRoom(ctx, roomCode); err != nil {
		return models.Item{}, err
	}
	return s.store.CreateItem(ctx, models.Item{
		ID:        uuid.NewString(),
		RoomCode:  roomCode,
		Kind:      classifyText(content),
		Content:   content,
		SizeBytes: int64(len(content)),
		Device:    device,
		ExpiresAt: time.Now().Add(s.cfg.Retention),
	})
}

var (
	errEmptyContent = errors.New("empty content")
	errTooLarge     = errors.New("content too large")
)

func (s *Server) writeCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errEmptyContent):
		writeError(w, http.StatusBadRequest, "nothing to paste")
	case errors.Is(err, errTooLarge), errors.Is(err, blob.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "that payload is over the size limit")
	default:
		s.log.Error("create item", "err", err)
		writeError(w, http.StatusInternalServerError, "could not save that item")
	}
}

// classifyText marks bare URLs so the dashboard can render them as links.
func classifyText(content string) string {
	trimmed := strings.TrimSpace(content)
	if strings.ContainsAny(trimmed, " \t\n") {
		return models.KindText
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Host == "" {
		return models.KindText
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		return models.KindLink
	}
	return models.KindText
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	code, ok := NormalizeRoomCode(r.PathValue("code"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid room code")
		return
	}
	if _, err := s.store.TouchRoom(r.Context(), code); err != nil {
		s.log.Error("touch room", "err", err)
		writeError(w, http.StatusInternalServerError, "could not open room")
		return
	}

	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected a multipart upload")
		return
	}

	device := deviceOf(r, "")
	created := make([]models.Item, 0, 4)

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			s.rollback(r.Context(), code, created)
			writeError(w, http.StatusBadRequest, "malformed upload")
			return
		}
		if part.FormName() == "device" && part.FileName() == "" {
			if v, readErr := io.ReadAll(io.LimitReader(part, 128)); readErr == nil && len(v) > 0 {
				device = sanitizeDevice(string(v))
			}
			part.Close()
			continue
		}
		if part.FileName() == "" {
			part.Close()
			continue
		}

		item, err := s.storeUploadedPart(r.Context(), code, device, part)
		part.Close()
		if err != nil {
			s.rollback(r.Context(), code, created)
			s.writeCreateError(w, err)
			return
		}
		created = append(created, item)
	}

	if len(created) == 0 {
		writeError(w, http.StatusBadRequest, "no files in that upload")
		return
	}
	for _, item := range created {
		s.hub.Broadcast(code, "item.created", item, "")
	}
	writeJSON(w, http.StatusCreated, map[string]any{"items": created})
}

// rollback drops the items already written for a request that then failed, so a
// half-finished multi-file upload does not leave partial state behind.
func (s *Server) rollback(ctx context.Context, roomCode string, created []models.Item) {
	for _, done := range created {
		if err := s.deleteItem(ctx, roomCode, done.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			s.log.Error("roll back item", "id", done.ID, "err", err)
		}
	}
}

func (s *Server) storeUploadedPart(ctx context.Context, roomCode, device string, part *multipart.Part) (models.Item, error) {
	id := uuid.NewString()
	path, size, err := s.blobs.Write(id, part, s.cfg.MaxUploadBytes)
	if err != nil {
		return models.Item{}, err
	}

	name := filepath.Base(filepath.FromSlash(part.FileName()))
	if name == "." || name == string(filepath.Separator) {
		name = "pasted-file"
	}
	mimeType := part.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		if guess := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); guess != "" {
			mimeType = guess
		}
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	kind := models.KindFile
	width, height := 0, 0
	if strings.HasPrefix(mimeType, "image/") {
		kind = models.KindImage
		width, height = s.imageDimensions(path)
	}

	item, err := s.store.CreateItem(ctx, models.Item{
		ID:        id,
		RoomCode:  roomCode,
		Kind:      kind,
		FileName:  name,
		MimeType:  mimeType,
		SizeBytes: size,
		Width:     width,
		Height:    height,
		Device:    device,
		BlobPath:  path,
		ExpiresAt: time.Now().Add(s.cfg.Retention),
	})
	if err != nil {
		_ = s.blobs.Remove(path)
		return models.Item{}, err
	}
	return item, nil
}

func (s *Server) imageDimensions(blobPath string) (int, int) {
	f, err := s.blobs.Open(blobPath)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return 0, 0 // svg, webp and friends simply go without dimensions
	}
	return cfg.Width, cfg.Height
}

func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	code, ok := NormalizeRoomCode(r.PathValue("code"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid room code")
		return
	}
	id := r.PathValue("id")
	if err := s.deleteItem(r.Context(), code, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
			return
		}
		s.log.Error("delete item", "err", err)
		writeError(w, http.StatusInternalServerError, "could not delete that item")
		return
	}
	s.hub.Broadcast(code, "item.deleted", map[string]string{"id": id}, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteItem(ctx context.Context, roomCode, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return store.ErrNotFound
	}
	item, err := s.store.DeleteItem(ctx, roomCode, id)
	if err != nil {
		return err
	}
	if item.HasBlob() {
		if err := s.blobs.Remove(item.BlobPath); err != nil {
			s.log.Error("remove blob", "path", item.BlobPath, "err", err)
		}
	}
	return nil
}

func (s *Server) handleClearRoom(w http.ResponseWriter, r *http.Request) {
	code, ok := NormalizeRoomCode(r.PathValue("code"))
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid room code")
		return
	}
	if err := s.clearRoom(r.Context(), code); err != nil {
		s.log.Error("clear room", "err", err)
		writeError(w, http.StatusInternalServerError, "could not clear the room")
		return
	}
	s.hub.Broadcast(code, "room.cleared", map[string]any{}, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clearRoom(ctx context.Context, code string) error {
	paths, err := s.store.ClearRoom(ctx, code)
	if err != nil {
		return err
	}
	for _, p := range paths {
		if err := s.blobs.Remove(p); err != nil {
			s.log.Error("remove blob", "path", p, "err", err)
		}
	}
	return nil
}

func (s *Server) handleRawItem(w http.ResponseWriter, r *http.Request) {
	s.serveBlob(w, r, false)
}

func (s *Server) handleDownloadItem(w http.ResponseWriter, r *http.Request) {
	s.serveBlob(w, r, true)
}

func (s *Server) serveBlob(w http.ResponseWriter, r *http.Request, asAttachment bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	item, err := s.store.GetItem(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "item not found")
			return
		}
		s.log.Error("get item", "err", err)
		writeError(w, http.StatusInternalServerError, "could not load that item")
		return
	}
	if !item.HasBlob() {
		writeError(w, http.StatusNotFound, "that item has no file")
		return
	}

	f, err := s.blobs.Open(item.BlobPath)
	if err != nil {
		s.log.Error("open blob", "path", item.BlobPath, "err", err)
		writeError(w, http.StatusNotFound, "file is no longer available")
		return
	}
	defer f.Close()

	name := item.FileName
	if name == "" {
		name = "clipboard-" + item.ID
	}
	disposition := "inline"
	if asAttachment {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", item.MimeType)
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("%s; filename*=UTF-8''%s", disposition, url.PathEscape(name)))
	// The URL carries a UUID and the bytes never change, so cache it hard.
	w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	http.ServeContent(w, r, name, item.CreatedAt, f)
}

func deviceOf(r *http.Request, explicit string) string {
	if explicit != "" {
		return sanitizeDevice(explicit)
	}
	if v := r.Header.Get("X-Device-Name"); v != "" {
		return sanitizeDevice(v)
	}
	if v := r.URL.Query().Get("device"); v != "" {
		return sanitizeDevice(v)
	}
	return NewDeviceName()
}

// sanitizeDevice keeps device labels short and free of control characters,
// since they are rendered in every connected dashboard.
func sanitizeDevice(v string) string {
	v = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if len(v) > 48 {
		v = v[:48]
	}
	if v == "" {
		return NewDeviceName()
	}
	return v
}
