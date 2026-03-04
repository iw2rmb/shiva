package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/store"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNotifierNotifyRevision_EmitsFullAndDiffWithSigning(t *testing.T) {
	t.Parallel()

	type receivedRequest struct {
		body      []byte
		signature string
		timestamp string
	}

	var (
		mu       sync.Mutex
		received []receivedRequest
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		mu.Lock()
		received = append(received, receivedRequest{
			body:      body,
			signature: r.Header.Get(HeaderSignature),
			timestamp: r.Header.Get(HeaderTimestamp),
		})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	storeMock := newFakeNotifierStore(
		store.Subscription{
			ID:                    44,
			TenantID:              7,
			RepoID:                9,
			TargetURL:             server.URL,
			Secret:                "top-secret",
			MaxAttempts:           3,
			BackoffInitialSeconds: 1,
			BackoffMaxSeconds:     5,
		},
	)

	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	notifier := New(
		storeMock,
		WithHTTPClient(server.Client()),
		WithNow(func() time.Time { return now }),
		WithSleep(func(_ context.Context, _ time.Duration) error { return nil }),
	)

	err := notifier.NotifyRevision(context.Background(), RevisionNotification{
		TenantID:    7,
		TenantKey:   "tenant-alpha",
		RepoID:      9,
		RepoPath:    "group/repo",
		RevisionID:  333,
		Sha:         "sha-333",
		Branch:      "main",
		ProcessedAt: now,
		Artifact: store.SpecArtifact{
			RevisionID: 333,
			SpecJSON:   []byte(`{"openapi":"3.1.0","paths":{}}`),
			SpecYAML:   "openapi: 3.1.0\npaths: {}\n",
			ETag:       "\"etag-333\"",
			SizeBytes:  128,
		},
		SpecChange: store.SpecChange{
			RepoID:       9,
			ToRevisionID: 333,
			ChangeJSON:   []byte(`{"version":1,"summary":{"changed_endpoints":0}}`),
		},
	})
	if err != nil {
		t.Fatalf("NotifyRevision() unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Fatalf("expected 2 outbound requests, got %d", len(received))
	}

	eventTypes := make([]string, 0, 2)
	for _, req := range received {
		if req.timestamp != now.Format(time.RFC3339Nano) {
			t.Fatalf("unexpected timestamp header: %q", req.timestamp)
		}
		expectedSignature := signPayload("top-secret", req.body)
		if req.signature != expectedSignature {
			t.Fatalf("unexpected signature header: expected %q, got %q", expectedSignature, req.signature)
		}

		var envelope map[string]any
		if err := json.Unmarshal(req.body, &envelope); err != nil {
			t.Fatalf("unmarshal outbound payload: %v", err)
		}
		eventType, _ := envelope["type"].(string)
		eventTypes = append(eventTypes, eventType)
	}

	expectedTypes := []string{store.DeliveryEventTypeSpecUpdatedFull, store.DeliveryEventTypeSpecUpdatedDiff}
	for _, expectedType := range expectedTypes {
		if !contains(eventTypes, expectedType) {
			t.Fatalf("expected event type %q to be emitted; got %v", expectedType, eventTypes)
		}
	}
}

func TestDispatchEvent_RetryAndTerminalStates(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name               string
		responses          []int
		maxAttempts        int32
		expectedHTTPCalls  int
		expectedStatuses   []string
		expectedSleepCalls int
	}{
		{
			name:               "succeeds on second attempt",
			responses:          []int{http.StatusInternalServerError, http.StatusOK},
			maxAttempts:        3,
			expectedHTTPCalls:  2,
			expectedStatuses:   []string{store.DeliveryAttemptStatusRetryScheduled, store.DeliveryAttemptStatusSucceeded},
			expectedSleepCalls: 1,
		},
		{
			name:               "terminal failure after max attempts",
			responses:          []int{http.StatusBadGateway, http.StatusBadGateway},
			maxAttempts:        2,
			expectedHTTPCalls:  2,
			expectedStatuses:   []string{store.DeliveryAttemptStatusRetryScheduled, store.DeliveryAttemptStatusFailed},
			expectedSleepCalls: 1,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			storeMock := newFakeNotifierStore(store.Subscription{
				ID:                    10,
				TenantID:              1,
				RepoID:                2,
				TargetURL:             "https://example.com/hook",
				Secret:                "s3cr3t",
				MaxAttempts:           testCase.maxAttempts,
				BackoffInitialSeconds: 2,
				BackoffMaxSeconds:     8,
			})

			client := &scriptedHTTPClient{responses: append([]int(nil), testCase.responses...)}
			now := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
			sleepCalls := 0

			notifier := New(
				storeMock,
				WithHTTPClient(client),
				WithNow(func() time.Time { return now }),
				WithSleep(func(_ context.Context, _ time.Duration) error {
					sleepCalls++
					return nil
				}),
			)

			err := notifier.dispatchEvent(
				context.Background(),
				storeMock.subscriptions[0],
				RevisionNotification{
					RepoID:     2,
					RevisionID: 555,
					DeliveryID: "delivery-555",
					Sha:        "sha-555",
				},
				builtEvent{eventType: store.DeliveryEventTypeSpecUpdatedFull, body: []byte(`{"type":"spec.updated.full"}`)},
			)
			if err != nil {
				t.Fatalf("dispatchEvent() unexpected error: %v", err)
			}

			if client.calls != testCase.expectedHTTPCalls {
				t.Fatalf("expected %d http calls, got %d", testCase.expectedHTTPCalls, client.calls)
			}
			if sleepCalls != testCase.expectedSleepCalls {
				t.Fatalf("expected %d sleep calls, got %d", testCase.expectedSleepCalls, sleepCalls)
			}

			if len(storeMock.updateCalls) != len(testCase.expectedStatuses) {
				t.Fatalf("expected %d update calls, got %d", len(testCase.expectedStatuses), len(storeMock.updateCalls))
			}
			for i, expectedStatus := range testCase.expectedStatuses {
				if storeMock.updateCalls[i].Status != expectedStatus {
					t.Fatalf("update call %d expected status %q, got %q", i, expectedStatus, storeMock.updateCalls[i].Status)
				}
			}
		})
	}
}

func TestDispatchEvent_SkipsTerminalAttempt(t *testing.T) {
	t.Parallel()

	storeMock := newFakeNotifierStore(store.Subscription{
		ID:                    99,
		TenantID:              1,
		RepoID:                2,
		TargetURL:             "https://example.com/hook",
		Secret:                "secret",
		MaxAttempts:           2,
		BackoffInitialSeconds: 1,
		BackoffMaxSeconds:     2,
	})
	key := deliveryAttemptKey(99, 777, store.DeliveryEventTypeSpecUpdatedDiff)
	storeMock.latest[key] = store.DeliveryAttempt{
		ID:             1,
		SubscriptionID: 99,
		RevisionID:     777,
		EventType:      store.DeliveryEventTypeSpecUpdatedDiff,
		AttemptNo:      1,
		Status:         store.DeliveryAttemptStatusSucceeded,
	}

	client := &scriptedHTTPClient{responses: []int{http.StatusOK}}
	notifier := New(
		storeMock,
		WithHTTPClient(client),
		WithSleep(func(_ context.Context, _ time.Duration) error { return nil }),
	)

	err := notifier.dispatchEvent(
		context.Background(),
		storeMock.subscriptions[0],
		RevisionNotification{
			RepoID:     2,
			RevisionID: 777,
			DeliveryID: "delivery-777",
			Sha:        "sha-777",
		},
		builtEvent{eventType: store.DeliveryEventTypeSpecUpdatedDiff, body: []byte(`{"type":"spec.updated.diff"}`)},
	)
	if err != nil {
		t.Fatalf("dispatchEvent() unexpected error: %v", err)
	}
	if client.calls != 0 {
		t.Fatalf("expected no http calls for terminal state, got %d", client.calls)
	}
	if len(storeMock.createCalls) != 0 {
		t.Fatalf("expected no attempt creation for terminal state, got %d", len(storeMock.createCalls))
	}
}

func TestCalculateBackoff(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		initial  int32
		max      int32
		attempt  int32
		expected time.Duration
	}{
		{name: "first attempt uses initial", initial: 2, max: 20, attempt: 1, expected: 2 * time.Second},
		{name: "second attempt doubles", initial: 2, max: 20, attempt: 2, expected: 4 * time.Second},
		{name: "caps at max", initial: 4, max: 8, attempt: 5, expected: 8 * time.Second},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := calculateBackoff(testCase.initial, testCase.max, testCase.attempt)
			if got != testCase.expected {
				t.Fatalf("expected %s, got %s", testCase.expected, got)
			}
		})
	}
}

func TestDispatchEvent_EmitsNotifyDispatchSpan(t *testing.T) {
	t.Parallel()

	storeMock := newFakeNotifierStore(store.Subscription{
		ID:                    50,
		TenantID:              1,
		RepoID:                2,
		TargetURL:             "https://example.com/hook",
		Secret:                "secret",
		MaxAttempts:           1,
		BackoffInitialSeconds: 1,
		BackoffMaxSeconds:     1,
	})

	spanRecorder := tracetest.NewSpanRecorder()
	traceProvider := sdktrace.NewTracerProvider()
	traceProvider.RegisterSpanProcessor(spanRecorder)
	t.Cleanup(func() {
		_ = traceProvider.Shutdown(context.Background())
	})
	tracer := traceProvider.Tracer("notify-test")

	notifier := New(
		storeMock,
		WithHTTPClient(&scriptedHTTPClient{responses: []int{http.StatusOK}}),
		WithSleep(func(_ context.Context, _ time.Duration) error { return nil }),
		WithTracer(tracer),
	)

	err := notifier.dispatchEvent(
		context.Background(),
		storeMock.subscriptions[0],
		RevisionNotification{
			RepoID:     2,
			RevisionID: 901,
			DeliveryID: "delivery-901",
			Sha:        "sha-901",
		},
		builtEvent{eventType: store.DeliveryEventTypeSpecUpdatedFull, body: []byte(`{"type":"spec.updated.full"}`)},
	)
	if err != nil {
		t.Fatalf("dispatchEvent() unexpected error: %v", err)
	}

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatalf("expected ended spans")
	}

	found := false
	for _, span := range spans {
		if span.Name() == "notify.dispatch" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected notify.dispatch span in ended spans")
	}
}

type scriptedHTTPClient struct {
	mu        sync.Mutex
	responses []int
	calls     int
}

func (c *scriptedHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls++
	statusCode := http.StatusOK
	if len(c.responses) > 0 {
		statusCode = c.responses[0]
		c.responses = c.responses[1:]
	}

	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(http.StatusText(statusCode))),
		Header:     make(http.Header),
	}, nil
}

type fakeNotifierStore struct {
	subscriptions []store.Subscription

	mu          sync.Mutex
	nextID      int64
	attempts    map[int64]store.DeliveryAttempt
	latest      map[string]store.DeliveryAttempt
	createCalls []store.CreateDeliveryAttemptInput
	updateCalls []store.UpdateDeliveryAttemptResultInput
}

func newFakeNotifierStore(subscriptions ...store.Subscription) *fakeNotifierStore {
	return &fakeNotifierStore{
		subscriptions: append([]store.Subscription(nil), subscriptions...),
		nextID:        1,
		attempts:      make(map[int64]store.DeliveryAttempt),
		latest:        make(map[string]store.DeliveryAttempt),
	}
}

func (f *fakeNotifierStore) ListEnabledSubscriptionsByRepo(
	_ context.Context,
	tenantID int64,
	repoID int64,
) ([]store.Subscription, error) {
	result := make([]store.Subscription, 0, len(f.subscriptions))
	for _, subscription := range f.subscriptions {
		if subscription.TenantID == tenantID && subscription.RepoID == repoID {
			result = append(result, subscription)
		}
	}
	return result, nil
}

func (f *fakeNotifierStore) GetLatestDeliveryAttemptByKey(
	_ context.Context,
	subscriptionID int64,
	revisionID int64,
	eventType string,
) (store.DeliveryAttempt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	attempt, ok := f.latest[deliveryAttemptKey(subscriptionID, revisionID, eventType)]
	return attempt, ok, nil
}

func (f *fakeNotifierStore) CreateDeliveryAttempt(
	_ context.Context,
	input store.CreateDeliveryAttemptInput,
) (store.DeliveryAttempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	id := f.nextID
	f.nextID++

	attempt := store.DeliveryAttempt{
		ID:             id,
		SubscriptionID: input.SubscriptionID,
		RevisionID:     input.RevisionID,
		EventType:      input.EventType,
		AttemptNo:      input.AttemptNo,
		Status:         input.Status,
		NextRetryAt:    cloneTime(input.NextRetryAt),
	}
	f.attempts[id] = attempt
	f.latest[deliveryAttemptKey(input.SubscriptionID, input.RevisionID, input.EventType)] = attempt
	f.createCalls = append(f.createCalls, input)

	return attempt, nil
}

func (f *fakeNotifierStore) UpdateDeliveryAttemptResult(
	_ context.Context,
	input store.UpdateDeliveryAttemptResultInput,
) (store.DeliveryAttempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	attempt := f.attempts[input.ID]
	attempt.Status = input.Status
	attempt.ResponseCode = cloneInt32(input.ResponseCode)
	attempt.Error = input.Error
	attempt.NextRetryAt = cloneTime(input.NextRetryAt)

	f.attempts[input.ID] = attempt
	f.latest[deliveryAttemptKey(attempt.SubscriptionID, attempt.RevisionID, attempt.EventType)] = attempt
	f.updateCalls = append(f.updateCalls, input)

	return attempt, nil
}

func deliveryAttemptKey(subscriptionID, revisionID int64, eventType string) string {
	return strings.Join([]string{
		"sub", strconv.FormatInt(subscriptionID, 10),
		"rev", strconv.FormatInt(revisionID, 10),
		"event", eventType,
	}, ":")
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func cloneInt32(value *int32) *int32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
