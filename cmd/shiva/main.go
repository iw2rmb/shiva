package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/iw2rmb/shiva/internal/config"
	httpserver "github.com/iw2rmb/shiva/internal/http"
	"github.com/iw2rmb/shiva/internal/store"
	"github.com/iw2rmb/shiva/internal/worker"
)

func main() {
	if err := run(context.Background()); err != nil {
		logger := slog.Default()
		logger.Error("shiva startup failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := config.NewLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	storeInstance, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer storeInstance.Close()

	workerManager := worker.New(cfg.WorkerConcurrency, logger)
	if err := workerManager.Start(ctx); err != nil {
		return err
	}

	server := httpserver.New(cfg, logger, storeInstance)
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Start()
	}()

	logger.Info("shiva started", "http_addr", cfg.HTTPAddr)

	select {
	case sig := <-shutdown:
		logger.Info("shutdown signal received", "signal", sig.String())
	case srvErr := <-errCh:
		if srvErr != nil {
			return srvErr
		}
	}

	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("http shutdown returned error", "error", err)
	}

	if err := workerManager.Stop(shutdownCtx); err != nil {
		logger.Warn("worker shutdown returned error", "error", err)
	}

	return nil
}
