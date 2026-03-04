package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	var workerManager *worker.Manager
	if storeInstance.IsConfigured() {
		workerManager = worker.New(
			cfg.WorkerConcurrency,
			logger,
			worker.WithQueue(storeQueueAdapter{store: storeInstance}),
			worker.WithProcessor(revisionProcessor{store: storeInstance}),
		)
		if err := workerManager.Start(ctx); err != nil {
			return err
		}
	} else {
		logger.Info("worker manager disabled: database is not configured")
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

	if workerManager != nil {
		if err := workerManager.Stop(shutdownCtx); err != nil {
			logger.Warn("worker shutdown returned error", "error", err)
		}
	}

	return nil
}

type storeQueueAdapter struct {
	store *store.Store
}

func (q storeQueueAdapter) ClaimNext(ctx context.Context) (worker.QueueJob, bool, error) {
	event, ok, err := q.store.ClaimNextIngestEvent(ctx)
	if err != nil {
		return worker.QueueJob{}, false, err
	}
	if !ok {
		return worker.QueueJob{}, false, nil
	}
	return worker.QueueJob{
		EventID:      event.ID,
		RepoID:       event.RepoID,
		Sha:          event.Sha,
		Branch:       event.Branch,
		ParentSha:    event.ParentSha,
		AttemptCount: event.AttemptCount,
	}, true, nil
}

func (q storeQueueAdapter) MarkProcessed(ctx context.Context, eventID int64) error {
	return q.store.MarkIngestEventProcessed(ctx, eventID)
}

func (q storeQueueAdapter) ScheduleRetry(ctx context.Context, eventID int64, nextRetryAt time.Time, errorMessage string) error {
	return q.store.ScheduleIngestEventRetry(ctx, eventID, nextRetryAt, errorMessage)
}

func (q storeQueueAdapter) MarkFailed(ctx context.Context, eventID int64, errorMessage string) error {
	return q.store.MarkIngestEventFailed(ctx, eventID, errorMessage)
}

type revisionProcessor struct {
	store *store.Store
}

func (p revisionProcessor) Process(ctx context.Context, job worker.QueueJob) error {
	_, err := p.store.UpsertRevisionFromIngestEvent(ctx, store.IngestQueueEvent{
		ID:           job.EventID,
		RepoID:       job.RepoID,
		Sha:          job.Sha,
		Branch:       job.Branch,
		ParentSha:    job.ParentSha,
		AttemptCount: job.AttemptCount,
	})
	if err != nil {
		return fmt.Errorf("upsert revision from ingest event %d: %w", job.EventID, err)
	}
	return nil
}
