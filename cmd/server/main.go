package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"clipboard/internal/api"
	"clipboard/internal/blob"
	"clipboard/internal/config"
	"clipboard/internal/database"
	"clipboard/internal/events"
	"clipboard/internal/hub"
	"clipboard/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg := config.Load()
	for _, note := range cfg.Notes {
		log.Info("config", "note", note)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL, cfg.MaxDBConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Info("connected to postgres")

	blobs, err := newBlobStore(cfg, pool)
	if err != nil {
		return err
	}
	log.Info("blob storage ready", "backend", blobs.Describe())

	h := hub.New(log)
	bus := events.New(pool, cfg.InstanceID, log)

	srv := api.NewServer(cfg, store.New(pool), blobs, h, bus, log)
	srv.StartJanitor(ctx)
	srv.StartEvents(ctx)

	httpServer := &http.Server{
		Addr:    cfg.Addr,
		Handler: srv.Routes(),
		// No ReadTimeout or WriteTimeout: uploads can be slow and websockets
		// are long-lived. The header timeout still fends off slowloris.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("clipboard listening",
			"addr", cfg.Addr, "instance", cfg.InstanceID, "retention", cfg.Retention.String())
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

// newBlobStore picks where uploaded bytes live. See config.blobBackend for why
// a Heroku dyno cannot use the filesystem.
func newBlobStore(cfg config.Config, pool *database.Pool) (blob.Store, error) {
	if cfg.BlobBackend == config.BackendPostgres {
		return blob.NewPostgres(pool), nil
	}
	return blob.NewDisk(cfg.BlobDir)
}
