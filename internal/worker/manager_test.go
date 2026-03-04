package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestManager_ProcessesSameRepoInCommitOrder(t *testing.T) {
	t.Parallel()

	queue := newFakeQueue(
		queueEvent{eventID: 1, repoID: 100, sha: "a1"},
		queueEvent{eventID: 2, repoID: 200, sha: "b1"},
		queueEvent{eventID: 3, repoID: 100, sha: "a2"},
		queueEvent{eventID: 4, repoID: 100, sha: "a3"},
		queueEvent{eventID: 5, repoID: 200, sha: "b2"},
	)

	var mu sync.Mutex
	repoOrder := map[int64][]string{}

	processor := processorFunc(func(_ context.Context, job QueueJob) error {
		if job.RepoID == 100 && job.Sha == "a1" {
			time.Sleep(25 * time.Millisecond)
		}

		mu.Lock()
		repoOrder[job.RepoID] = append(repoOrder[job.RepoID], job.Sha)
		mu.Unlock()
		return nil
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := New(
		3,
		logger,
		WithQueue(queue),
		WithProcessor(processor),
		WithPollDelay(1*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	if !waitUntil(3*time.Second, func() bool { return queue.processedCount() == 5 }) {
		t.Fatalf("timed out waiting for events to be processed")
	}

	cancel()
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	expectedRepo100 := []string{"a1", "a2", "a3"}
	if !slices.Equal(repoOrder[100], expectedRepo100) {
		t.Fatalf("repo 100 order mismatch: expected %v, got %v", expectedRepo100, repoOrder[100])
	}

	expectedRepo200 := []string{"b1", "b2"}
	if !slices.Equal(repoOrder[200], expectedRepo200) {
		t.Fatalf("repo 200 order mismatch: expected %v, got %v", expectedRepo200, repoOrder[200])
	}
}

func TestManager_RetriesWithBackoff(t *testing.T) {
	t.Parallel()

	queue := newFakeQueue(queueEvent{eventID: 11, repoID: 300, sha: "retry-sha"})

	var mu sync.Mutex
	attempts := []int32{}
	failures := 0

	processor := processorFunc(func(_ context.Context, job QueueJob) error {
		mu.Lock()
		attempts = append(attempts, job.AttemptCount)
		if failures == 0 {
			failures++
			mu.Unlock()
			return errors.New("temporary failure")
		}
		mu.Unlock()
		return nil
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := New(
		1,
		logger,
		WithQueue(queue),
		WithProcessor(processor),
		WithPollDelay(1*time.Millisecond),
		WithBackoff(ExponentialBackoff{Initial: 10 * time.Millisecond, Max: 10 * time.Millisecond}),
		WithMaxAttempts(3),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Start() unexpected error: %v", err)
	}

	if !waitUntil(3*time.Second, func() bool { return queue.processedCount() == 1 }) {
		t.Fatalf("timed out waiting for retry flow to finish")
	}

	cancel()
	if err := manager.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() unexpected error: %v", err)
	}

	mu.Lock()
	gotAttempts := append([]int32(nil), attempts...)
	mu.Unlock()

	expectedAttempts := []int32{1, 2}
	if !slices.Equal(gotAttempts, expectedAttempts) {
		t.Fatalf("expected attempts %v, got %v", expectedAttempts, gotAttempts)
	}

	retryDelay, hasRetry := queue.firstRetryDelay()
	if !hasRetry {
		t.Fatalf("expected at least one scheduled retry")
	}
	if retryDelay < 9*time.Millisecond {
		t.Fatalf("expected retry delay close to 10ms, got %s", retryDelay)
	}
}

type processorFunc func(context.Context, QueueJob) error

func (f processorFunc) Process(ctx context.Context, job QueueJob) error {
	return f(ctx, job)
}

type queueEvent struct {
	eventID int64
	repoID  int64
	sha     string
}

type fakeQueue struct {
	mu        sync.Mutex
	events    []fakeQueueState
	retries   []time.Duration
	claimed   int
	processed int
}

type fakeQueueState struct {
	job       QueueJob
	status    string
	nextRetry time.Time
}

func newFakeQueue(events ...queueEvent) *fakeQueue {
	now := time.Now()
	queueStates := make([]fakeQueueState, 0, len(events))
	for _, event := range events {
		queueStates = append(queueStates, fakeQueueState{
			job: QueueJob{
				EventID: event.eventID,
				RepoID:  event.repoID,
				Sha:     event.sha,
			},
			status:    "pending",
			nextRetry: now,
		})
	}
	return &fakeQueue{events: queueStates}
}

func (q *fakeQueue) ClaimNext(_ context.Context) (QueueJob, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	for index := range q.events {
		event := &q.events[index]
		if event.status != "pending" || now.Before(event.nextRetry) {
			continue
		}

		if q.hasOlderActive(event.job.RepoID, event.job.EventID) {
			continue
		}

		event.status = "processing"
		event.job.AttemptCount++
		q.claimed++
		return event.job, true, nil
	}

	return QueueJob{}, false, nil
}

func (q *fakeQueue) MarkProcessed(_ context.Context, eventID int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for index := range q.events {
		event := &q.events[index]
		if event.job.EventID == eventID {
			event.status = "processed"
			q.processed++
			return nil
		}
	}
	return errors.New("event not found")
}

func (q *fakeQueue) ScheduleRetry(_ context.Context, eventID int64, nextRetryAt time.Time, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	for index := range q.events {
		event := &q.events[index]
		if event.job.EventID == eventID {
			event.status = "pending"
			event.nextRetry = nextRetryAt
			q.retries = append(q.retries, nextRetryAt.Sub(now))
			return nil
		}
	}
	return errors.New("event not found")
}

func (q *fakeQueue) MarkFailed(_ context.Context, eventID int64, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	for index := range q.events {
		event := &q.events[index]
		if event.job.EventID == eventID {
			event.status = "failed"
			return nil
		}
	}
	return errors.New("event not found")
}

func (q *fakeQueue) hasOlderActive(repoID int64, eventID int64) bool {
	for _, event := range q.events {
		if event.job.RepoID != repoID {
			continue
		}
		if event.job.EventID >= eventID {
			continue
		}
		if event.status == "pending" || event.status == "processing" {
			return true
		}
	}
	return false
}

func (q *fakeQueue) processedCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.processed
}

func (q *fakeQueue) firstRetryDelay() (time.Duration, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.retries) == 0 {
		return 0, false
	}
	return q.retries[0], true
}

func waitUntil(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return condition()
}
