package gitlab

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientCompareChangedPathsRetries5xx(t *testing.T) {
	t.Parallel()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("bad gateway"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"diffs":[]}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", withSleep(noopSleep))
	if err != nil {
		t.Fatalf("NewClient() unexpected error: %v", err)
	}

	if _, err := client.CompareChangedPaths(context.Background(), 42, "from", "to"); err != nil {
		t.Fatalf("CompareChangedPaths() unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestClientCompareChangedPathsRetriesNetworkError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"diffs":[]}`))
	}))
	defer server.Close()

	flaky := &flakyHTTPClient{
		failCount: 1,
		delegate:  server.Client(),
	}
	client, err := NewClient(server.URL, "", WithHTTPClient(flaky), withSleep(noopSleep))
	if err != nil {
		t.Fatalf("NewClient() unexpected error: %v", err)
	}

	if _, err := client.CompareChangedPaths(context.Background(), 42, "from", "to"); err != nil {
		t.Fatalf("CompareChangedPaths() unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&flaky.calls); got < 2 {
		t.Fatalf("http client calls = %d, want at least 2", got)
	}
}

func TestClientCompareChangedPathsCaps4xxRetries(t *testing.T) {
	t.Parallel()

	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", withSleep(noopSleep))
	if err != nil {
		t.Fatalf("NewClient() unexpected error: %v", err)
	}

	_, err = client.CompareChangedPaths(context.Background(), 42, "from", "to")
	if err == nil {
		t.Fatalf("expected error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestClientCompareChangedPathsHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		now        time.Time
		wantWait   time.Duration
		retryAfter func(time.Time) string
	}{
		{
			name:     "seconds",
			wantWait: 2 * time.Second,
			retryAfter: func(_ time.Time) string {
				return "2"
			},
		},
		{
			name:     "http date",
			now:      time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC),
			wantWait: 3 * time.Second,
			retryAfter: func(now time.Time) string {
				return now.Add(3 * time.Second).Format(http.TimeFormat)
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var attempts int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if atomic.AddInt32(&attempts, 1) == 1 {
					w.Header().Set("Retry-After", tc.retryAfter(tc.now))
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"diffs":[]}`))
			}))
			defer server.Close()

			waits := make([]time.Duration, 0, 1)
			options := []Option{
				withSleep(func(_ context.Context, d time.Duration) error {
					waits = append(waits, d)
					return nil
				}),
			}
			if !tc.now.IsZero() {
				options = append(options, withNow(func() time.Time {
					return tc.now
				}))
			}

			client, err := NewClient(server.URL, "", options...)
			if err != nil {
				t.Fatalf("NewClient() unexpected error: %v", err)
			}

			if _, err := client.CompareChangedPaths(context.Background(), 42, "from", "to"); err != nil {
				t.Fatalf("CompareChangedPaths() unexpected error: %v", err)
			}
			if len(waits) != 1 {
				t.Fatalf("sleep calls = %d, want 1", len(waits))
			}
			if waits[0] != tc.wantWait {
				t.Fatalf("sleep duration = %v, want %v", waits[0], tc.wantWait)
			}
		})
	}
}

func TestClientCompareChangedPathsLimiterSerializesRequests(t *testing.T) {
	t.Parallel()

	var inFlight int32
	var maxInFlight int32
	release := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := atomic.AddInt32(&inFlight, 1)
		for {
			max := atomic.LoadInt32(&maxInFlight)
			if current <= max {
				break
			}
			if atomic.CompareAndSwapInt32(&maxInFlight, max, current) {
				break
			}
		}
		<-release
		atomic.AddInt32(&inFlight, -1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"diffs":[]}`))
	}))
	defer server.Close()

	client, err := NewClient(
		server.URL,
		"",
		withSleep(noopSleep),
		withTimeout(5*time.Second),
		withLimiterSettings(1, 0),
	)
	if err != nil {
		t.Fatalf("NewClient() unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := client.CompareChangedPaths(ctx, 42, "from", "to")
			errCh <- err
		}()
	}

	release <- struct{}{}
	release <- struct{}{}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("CompareChangedPaths() unexpected error: %v", err)
		}
	}
	if got := atomic.LoadInt32(&maxInFlight); got != 1 {
		t.Fatalf("max concurrent requests = %d, want 1", got)
	}
}

func noopSleep(_ context.Context, _ time.Duration) error {
	return nil
}

type flakyHTTPClient struct {
	failCount int32
	calls     int32
	delegate  HTTPClient
}

func (c *flakyHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if atomic.AddInt32(&c.calls, 1) <= c.failCount {
		return nil, errors.New("temporary network failure")
	}
	return c.delegate.Do(req)
}
