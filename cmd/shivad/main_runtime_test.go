package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunConfigLoadFailureLeavesJSONDefaultLogger(t *testing.T) {
	var stdout, stderr bytes.Buffer
	oldStdout := stdoutWriter
	oldStderr := stderrWriter
	oldDefault := slog.Default()
	stdoutWriter = &stdout
	stderrWriter = &stderr
	t.Cleanup(func() {
		stdoutWriter = oldStdout
		stderrWriter = oldStderr
		slog.SetDefault(oldDefault)
		log.SetOutput(io.Discard)
	})

	t.Setenv("SHIVA_DATABASE_URL", "")

	err := run(context.Background())
	if err == nil {
		t.Fatalf("expected config load error")
	}
	slog.Default().Error("shiva startup failed", "error", err)

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	var frame map[string]any
	if decodeErr := json.Unmarshal(stderr.Bytes(), &frame); decodeErr != nil {
		t.Fatalf("stderr is not JSON: %q: %v", stderr.String(), decodeErr)
	}
	if frame["level"] != "ERROR" || frame["msg"] != "shiva startup failed" {
		t.Fatalf("unexpected log frame: %#v", frame)
	}
	if frame["env"] != "prod" || frame["system"] != "shiva-server" || frame["inst"] != "shiva.t-tech.team" {
		t.Fatalf("unexpected identity: %#v", frame)
	}
	if frame["error"] != "SHIVA_DATABASE_URL must not be empty" {
		t.Fatalf("unexpected error field: %#v", frame)
	}
}

func TestStartIngestRuntimeStartsWorkerWithoutWaitingForStartupIndexing(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	var workerStarted atomic.Bool
	startupObservedWorkerStarted := make(chan bool, 1)
	startupStarted := make(chan struct{})
	releaseStartup := make(chan struct{})

	done := make(chan error, 1)
	go func() {
		done <- startIngestRuntime(
			ctx,
			logger,
			fakeWorkerStarter{
				start: func(context.Context) error {
					workerStarted.Store(true)
					return nil
				},
			},
			func(context.Context) error {
				startupObservedWorkerStarted <- workerStarted.Load()
				close(startupStarted)
				<-releaseStartup
				return nil
			},
		)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("startIngestRuntime() unexpected error: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("startIngestRuntime() blocked waiting for startup indexing")
	}

	select {
	case <-startupStarted:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("startup indexing was not launched")
	}

	select {
	case sawWorkerStarted := <-startupObservedWorkerStarted:
		if !sawWorkerStarted {
			t.Fatalf("startup indexing launched before worker start completed")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("startup indexing did not report worker state")
	}

	close(releaseStartup)
}

func TestStartIngestRuntimePropagatesWorkerStartError(t *testing.T) {
	t.Parallel()

	startErr := errors.New("worker start failed")
	var startupCalled atomic.Bool

	err := startIngestRuntime(
		context.Background(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		fakeWorkerStarter{
			start: func(context.Context) error {
				return startErr
			},
		},
		func(context.Context) error {
			startupCalled.Store(true)
			return nil
		},
	)
	if !errors.Is(err, startErr) {
		t.Fatalf("expected worker start error %v, got %v", startErr, err)
	}
	if startupCalled.Load() {
		t.Fatalf("startup indexing should not run when worker start fails")
	}
}

func TestRunStartupIndexingAsyncLogsFailure(t *testing.T) {
	t.Parallel()

	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, nil))
	started := make(chan struct{})

	runStartupIndexingAsync(context.Background(), logger, func(context.Context) error {
		close(started)
		return errors.New("gitlab unavailable")
	})

	select {
	case <-started:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("startup indexing goroutine was not launched")
	}

	if !waitUntil(1*time.Second, func() bool {
		logText := logBuffer.String()
		return strings.Contains(logText, "startup indexing failed") && strings.Contains(logText, "gitlab unavailable")
	}) {
		t.Fatalf("expected startup indexing failure to be logged, got %q", logBuffer.String())
	}
}

type fakeWorkerStarter struct {
	start func(context.Context) error
}

func (f fakeWorkerStarter) Start(ctx context.Context) error {
	if f.start == nil {
		return nil
	}
	return f.start(ctx)
}
