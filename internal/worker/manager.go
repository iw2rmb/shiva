package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
)

type Manager struct {
	logger      *slog.Logger
	concurrency int
	started     atomic.Bool
	wg          sync.WaitGroup
}

func New(concurrency int, logger *slog.Logger) *Manager {
	return &Manager{
		logger:      logger,
		concurrency: concurrency,
	}
}

func (m *Manager) Start(ctx context.Context) error {
	if m.concurrency < 1 {
		return errors.New("worker concurrency must be at least 1")
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
	<-ctx.Done()
	m.logger.Debug("worker loop exiting", "worker_id", id)
}
