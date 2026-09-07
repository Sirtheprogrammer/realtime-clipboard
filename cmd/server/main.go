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

	var st *store.Store
	var blobs blob.Store
	var bus *events.Bus

	if cfg.DatabaseDriver == config.DriverSQLite {
		db, err := database.OpenSQLite(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer db.Close()
		log.Info("connected to embedded sqlite", "path", cfg.DatabaseURL)

		st = store.NewSQLite(db)

		diskBlobs, err := blob.NewDisk(cfg.BlobDir)
		if err != nil {
			return err
		}
		blobs = diskBlobs
		log.Info("blob storage ready", "backend", blobs.Describe())
	} else {
		pool, err := database.Connect(ctx, cfg.DatabaseURL, cfg.MaxDBConns)
		if err != nil {
			return err
		}
		defer pool.Close()
		log.Info("connected to postgres")

		st = store.New(pool)

		var errBlob error
		blobs, errBlob = newBlobStore(cfg, pool)
		if errBlob != nil {
			return errBlob
		}
		log.Info("blob storage ready", "backend", blobs.Describe())

		bus = events.New(pool, cfg.InstanceID, log)
	}

	h := hub.New(log)

	srv := api.NewServer(cfg, st, blobs, h, bus, log)
	srv.StartJanitor(ctx)
	if bus != nil {
		srv.StartEvents(ctx)
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("clipboard listening",
			"addr", cfg.Addr, "instance", cfg.InstanceID, "retention", cfg.Retention.String(), "driver", cfg.DatabaseDriver)
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
