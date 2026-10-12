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

	dapr "github.com/dapr/go-sdk/client"

	"github.com/redjax/dapr-xkcd/internal/archive"
	"github.com/redjax/dapr-xkcd/internal/config"
	"github.com/redjax/dapr-xkcd/internal/daprio"
	"github.com/redjax/dapr-xkcd/internal/dashboard"
	"github.com/redjax/dapr-xkcd/internal/httpapi"
	"github.com/redjax/dapr-xkcd/internal/postgres"
	"github.com/redjax/dapr-xkcd/internal/viewer"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := run(log); err != nil {
		log.Error("API failed", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	startupCtx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	pool, err := postgres.Open(startupCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer pool.Close()

	client, err := dapr.NewClient()
	if err != nil {
		return err
	}
	defer client.Close()

	store := postgres.NewStore(pool)

	source := daprio.NewXKCD(client, daprio.XKCDOptions{
		UserAgent:         cfg.UserAgent,
		RequestGap:        cfg.RequestGap,
		RateLimitCooldown: cfg.RateLimitCooldown,
		MaxImageBytes:     cfg.MaxImageBytes,
	})

	service := archive.NewService(source, store, log)

	dashboardHandler, err := dashboard.New(
		store,
		log,
		cfg.StateReadTimeout,
	)
	if err != nil {
		return err
	}

	viewerHandler, err := viewer.New(
		store,
		log,
		cfg.StateReadTimeout,
	)
	if err != nil {
		return err
	}

	galleryHandler, err := viewer.NewGallery(
		store,
		log,
		cfg.StateReadTimeout,
	)
	if err != nil {
		return err
	}

	server := httpapi.NewServer(service, log, httpapi.Options{
		Address:          cfg.Address,
		IngestionTimeout: cfg.IngestionTimeout,
		StateReadTimeout: cfg.StateReadTimeout,
		Dashboard:        dashboardHandler,
		Viewer:           viewerHandler,
		Gallery:          galleryHandler,
	})

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	serverErrors := make(chan error, 1)

	go func() {
		log.Info("API listening", "address", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err

	case <-ctx.Done():
		log.Info("API shutting down")

		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}

		return nil
	}
}
