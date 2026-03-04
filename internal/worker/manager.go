package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrQueueNotConfigured = errors.New("worker queue is not configured")
var ErrProcessorNotConfigured = errors.New("worker processor is not configured")

type QueueJob struct {
	EventID      int64
	RepoID       int64
	Sha          string
	Branch       string
	ParentSha    string
	AttemptCount int32
}

type Queue interface {
	ClaimNext(ctx context.Context) (QueueJob, bool, error)
	MarkProcessed(ctx context.Context, eventID int64) error
	ScheduleRetry(ctx context.Context, eventID int64, nextRetryAt time.Time, errorMessage string) error
	MarkFailed(ctx context.Context, eventID int64, errorMessage string) error
}

type Processor interface {
	Process(ctx context.Context, job QueueJob) error
}

type Backoff interface {
	Duration(attempt int32) time.Duration
}

type Manager struct {
	logger      *slog.Logger
	concurrency int
	queue       Queue
	processor   Processor
	backoff     Backoff
	pollDelay   time.Duration
	maxAttempts int32
	now         func() time.Time
	started     atomic.Bool
	wg          sync.WaitGroup
}

type Option func(*Manager)

func WithQueue(queue Queue) Option {
	return func(m *Manager) {
		m.queue = queue
	}
}

func WithProcessor(processor Processor) Option {
	return func(m *Manager) {
		m.processor = processor
	}
}

func WithBackoff(backoff Backoff) Option {
	return func(m *Manager) {
		m.backoff = backoff
	}
}

func WithPollDelay(delay time.Duration) Option {
	return func(m *Manager) {
		if delay > 0 {
			m.pollDelay = delay
		}
	}
}

func WithMaxAttempts(maxAttempts int32) Option {
	return func(m *Manager) {
		if maxAttempts > 0 {
			m.maxAttempts = maxAttempts
		}
	}
}

func WithNowFunc(now func() time.Time) Option {
	return func(m *Manager) {
		if now != nil {
			m.now = now
		}
	}
}

func New(concurrency int, logger *slog.Logger, options ...Option) *Manager {
	if logger == nil {
		logger = slog.Default()
	}

	manager := &Manager{
		logger:      logger,
		concurrency: concurrency,
		backoff: ExponentialBackoff{
			Initial: time.Second,
			Max:     30 * time.Second,
		},
		pollDelay:   250 * time.Millisecond,
		maxAttempts: 5,
		now:         time.Now,
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

func (m *Manager) Start(ctx context.Context) error {
	if m.concurrency < 1 {
		return errors.New("worker concurrency must be at least 1")
	}
	if m.queue == nil {
		return ErrQueueNotConfigured
	}
	if m.processor == nil {
		return ErrProcessorNotConfigured
	}

	if !m.started.CompareAndSwap(false, true) {
		return nil
	}

	for i := 0; i < m.concurrency; i++ {
		m.wg.Add(1)
		go m.loop(ctx, i)
	}

	m.logger.Info("worker manager started", "concurrency", m.concurrency)
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	if !m.started.CompareAndSwap(true, false) {
		return nil
	}

	complete := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(complete)
	}()

	select {
	case <-complete:
		m.logger.Info("worker manager stopped")
		return nil
	case <-ctx.Done():
		return fmt.Errorf("worker manager shutdown timeout: %w", ctx.Err())
	}
}

func (m *Manager) loop(ctx context.Context, id int) {
	defer m.wg.Done()
	m.logger.Debug("worker loop started", "worker_id", id)
	for {
		if ctx.Err() != nil {
			break
		}

		job, ok, err := m.queue.ClaimNext(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			m.logger.Error("claim ingest job failed", "worker_id", id, "error", err)
			if !sleepContext(ctx, m.pollDelay) {
				break
			}
			continue
		}
		if !ok {
			if !sleepContext(ctx, m.pollDelay) {
				break
			}
			continue
		}

		if err := m.processor.Process(ctx, job); err != nil {
			m.handleProcessError(ctx, id, job, err)
			continue
		}

		if err := m.queue.MarkProcessed(ctx, job.EventID); err != nil {
			m.logger.Error(
				"mark ingest job processed failed",
				"worker_id", id,
				"event_id", job.EventID,
				"repo_id", job.RepoID,
				"sha", job.Sha,
				"error", err,
			)
		}
	}
	m.logger.Debug("worker loop exiting", "worker_id", id)
}

func (m *Manager) handleProcessError(ctx context.Context, workerID int, job QueueJob, processErr error) {
	errorMessage := strings.TrimSpace(processErr.Error())
	if errorMessage == "" {
		errorMessage = "ingest processing failed"
	}

	if IsPermanent(processErr) || job.AttemptCount >= m.maxAttempts {
		if err := m.queue.MarkFailed(ctx, job.EventID, errorMessage); err != nil {
			m.logger.Error(
				"mark ingest job failed failed",
				"worker_id", workerID,
				"event_id", job.EventID,
				"repo_id", job.RepoID,
				"sha", job.Sha,
				"error", err,
			)
			return
		}
		m.logger.Warn(
			"ingest job marked failed",
			"worker_id", workerID,
			"event_id", job.EventID,
			"repo_id", job.RepoID,
			"sha", job.Sha,
			"attempt_count", job.AttemptCount,
			"error", errorMessage,
		)
		return
	}

	retryAfter := m.backoff.Duration(job.AttemptCount)
	nextRetryAt := m.now().Add(retryAfter)
	if err := m.queue.ScheduleRetry(ctx, job.EventID, nextRetryAt, errorMessage); err != nil {
		m.logger.Error(
			"schedule ingest job retry failed",
			"worker_id", workerID,
			"event_id", job.EventID,
			"repo_id", job.RepoID,
			"sha", job.Sha,
			"error", err,
		)
		return
	}

	m.logger.Warn(
		"ingest job scheduled for retry",
		"worker_id", workerID,
		"event_id", job.EventID,
		"repo_id", job.RepoID,
		"sha", job.Sha,
		"attempt_count", job.AttemptCount,
		"retry_after", retryAfter.String(),
		"error", errorMessage,
	)
}

func sleepContext(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
