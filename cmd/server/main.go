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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Info("connected to postgres")

	blobs, err := blob.New(cfg.BlobDir)
	if err != nil {
		return err
	}

	h := hub.New(log)
	srv := api.NewServer(cfg, store.New(pool), blobs, h, log)
	srv.StartJanitor(ctx)

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
		log.Info("clipboard listening", "addr", cfg.Addr, "retention", cfg.Retention.String())
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
