package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clipboard/internal/blob"
	"clipboard/internal/config"
	"clipboard/internal/hub"
	"clipboard/internal/store"
)

type Server struct {
	cfg   config.Config
	store *store.Store
	blobs *blob.Store
	hub   *hub.Hub
	log   *slog.Logger
}

func NewServer(cfg config.Config, st *store.Store, blobs *blob.Store, h *hub.Hub, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, store: st, blobs: blobs, hub: h, log: log}
	h.SetHandler(s)
	return s
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/rooms", s.handleCreateRoom)
	mux.HandleFunc("GET /api/rooms/{code}", s.handleGetRoom)
	mux.HandleFunc("POST /api/rooms/{code}/items", s.handleCreateTextItem)
	mux.HandleFunc("POST /api/rooms/{code}/upload", s.handleUpload)
	mux.HandleFunc("DELETE /api/rooms/{code}/items", s.handleClearRoom)
	mux.HandleFunc("DELETE /api/rooms/{code}/items/{id}", s.handleDeleteItem)
	mux.HandleFunc("GET /api/items/{id}/raw", s.handleRawItem)
	mux.HandleFunc("GET /api/items/{id}/download", s.handleDownloadItem)
	mux.HandleFunc("GET /ws", s.handleWebSocket)

	s.mountStatic(mux)

	return s.withRecovery(s.withLogging(mux))
}

// mountStatic serves the dashboard and rewrites /r/{code} deep links back onto
// the single-page shell.
func (s *Server) mountStatic(mux *http.ServeMux) {
	fileServer := http.FileServer(http.Dir(s.cfg.WebDir))
	shell := filepath.Join(s.cfg.WebDir, "index.html")

	mux.HandleFunc("GET /r/{code}", func(w http.ResponseWriter, r *http.Request) {
		noStore(w)
		http.ServeFile(w, r, shell)
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Join(s.cfg.WebDir, filepath.Clean("/"+strings.TrimPrefix(r.URL.Path, "/")))
		if r.URL.Path == "/" {
			noStore(w)
			http.ServeFile(w, r, shell)
			return
		}
		if info, err := os.Stat(clean); err != nil || info.IsDir() {
			noStore(w)
			http.ServeFile(w, r, shell)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/sw.js") {
			noStore(w) // never let a stale worker pin the app
		}
		fileServer.ServeHTTP(w, r)
	})
}

func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

// StartJanitor sweeps expired items and their blobs on an interval.
func (s *Server) StartJanitor(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.SweepInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sweep(ctx)
			}
		}
	}()
}

func (s *Server) sweep(ctx context.Context) {
	paths, err := s.store.DeleteExpired(ctx)
	if err != nil {
		s.log.Error("sweep expired items", "err", err)
		return
	}
	for _, p := range paths {
		if err := s.blobs.Remove(p); err != nil {
			s.log.Error("remove expired blob", "path", p, "err", err)
		}
	}
	if err := s.store.DeleteEmptyRooms(ctx, 7*24*time.Hour); err != nil {
		s.log.Error("prune empty rooms", "err", err)
	}
	if len(paths) > 0 {
		s.log.Info("swept expired items", "blobs", len(paths))
	}
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/ws" {
			s.log.Info("request",
				"method", r.Method, "path", r.URL.Path,
				"status", rec.status, "dur", time.Since(start).Round(time.Millisecond))
		}
	})
}

func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic serving request", "path", r.URL.Path, "err", rec)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Hijack lets the websocket upgrader take the connection back off the
// logging wrapper.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hj.Hijack()
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
