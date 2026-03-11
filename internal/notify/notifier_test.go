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
		eventID   string
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
			eventID:   r.Header.Get("X-Shiva-Event-ID"),
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
		TenantID:          7,
		TenantKey:         "tenant-alpha",
		RepoID:            9,
		RepoPath:          "group/repo",
		APISpecID:         101,
		API:               "api/openapi.yaml",
		APISpecRevisionID: 1001,
		IngestEventID:     333,
		Sha:               "sha-333",
		Branch:            "main",
		ProcessedAt:       now,
		Artifact: store.SpecArtifact{
			APISpecRevisionID: 1001,
			SpecJSON:          []byte(`{"openapi":"3.1.0","paths":{}}`),
			SpecYAML:          "openapi: 3.1.0\npaths: {}\n",
			ETag:              "\"etag-333\"",
			SizeBytes:         128,
		},
		SpecChange: store.SpecChange{
			APISpecID:           101,
			ToAPISpecRevisionID: 1001,
			ChangeJSON:          []byte(`{"version":1,"summary":{"changed_endpoints":0}}`),
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
		if got := envelope["api"]; got != "api/openapi.yaml" {
			t.Fatalf("expected envelope api=%q, got %q", "api/openapi.yaml", got)
		}
		gotAPISpecRevisionID, ok := envelope["api_revision_id"].(float64)
		if !ok || int64(gotAPISpecRevisionID) != 1001 {
			t.Fatalf("expected api_revision_id=%d, got %v", 1001, envelope["api_revision_id"])
		}
		if !strings.Contains(req.eventID, ":api:101:") {
			t.Fatalf("expected event id to include api_spec_id=101, got %q", req.eventID)
		}
	}

	expectedTypes := []string{store.DeliveryEventTypeSpecUpdatedFull, store.DeliveryEventTypeSpecUpdatedDiff}
	for _, expectedType := range expectedTypes {
		if !contains(eventTypes, expectedType) {
			t.Fatalf("expected event type %q to be emitted; got %v", expectedType, eventTypes)
		}
	}
}

func TestNotifierNotifyRevision_EmitsDiffOnlyWhenFullArtifactMissing(t *testing.T) {
	t.Parallel()

	type receivedRequest struct {
		body []byte
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
		received = append(received, receivedRequest{body: body})
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

	notifier := New(
		storeMock,
		WithHTTPClient(server.Client()),
		WithSleep(func(_ context.Context, _ time.Duration) error { return nil }),
	)

	err := notifier.NotifyRevision(context.Background(), RevisionNotification{
		TenantID:          7,
		TenantKey:         "tenant-alpha",
		RepoID:            9,
		RepoPath:          "group/repo",
		APISpecID:         102,
		API:               "api/openapi.yaml",
		APISpecRevisionID: 1002,
		IngestEventID:     333,
		Sha:               "sha-333",
		Branch:            "main",
		ProcessedAt:       time.Now().UTC(),
		IncludeFull:       false,
		SpecChange: store.SpecChange{
			APISpecID:           102,
			ToAPISpecRevisionID: 1002,
			ChangeJSON:          []byte(`{"version":1,"summary":{"changed_endpoints":1}}`),
		},
	})
	if err != nil {
		t.Fatalf("NotifyRevision() unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 1 {
		t.Fatalf("expected 1 outbound request, got %d", len(received))
	}

	var envelope map[string]any
	if err := json.Unmarshal(received[0].body, &envelope); err != nil {
		t.Fatalf("unmarshal outbound payload: %v", err)
	}
	if eventType, _ := envelope["type"].(string); eventType != store.DeliveryEventTypeSpecUpdatedDiff {
		t.Fatalf("expected outbound event type %q, got %q", store.DeliveryEventTypeSpecUpdatedDiff, eventType)
	}
	if envelope["api"] != "api/openapi.yaml" {
		t.Fatalf("expected envelope api=%q, got %q", "api/openapi.yaml", envelope["api"])
	}
	if gotAPISpecRevisionID, ok := envelope["api_revision_id"].(float64); !ok || int64(gotAPISpecRevisionID) != 1002 {
		t.Fatalf("expected api_revision_id=%d, got %v", 1002, envelope["api_revision_id"])
	}
}

func TestNotifierNotifyRevision_EmitsPerAPIPayloadIdentityInOneRevision(t *testing.T) {
	t.Parallel()

	type receivedRequest struct {
		api       string
		eventType string
	}

	var (
		mu       sync.Mutex
		received []receivedRequest
	)

	now := time.Date(2026, 3, 4, 14, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		var envelope struct {
			API       string `json:"api"`
			EventType string `json:"type"`
			Revision  int64  `json:"api_revision_id"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		mu.Lock()
		received = append(received, receivedRequest{api: envelope.API, eventType: envelope.EventType})
		mu.Unlock()

		if envelope.Revision == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	storeMock := newFakeNotifierStore(store.Subscription{
		ID:                    44,
		TenantID:              7,
		RepoID:                9,
		TargetURL:             server.URL,
		Secret:                "top-secret",
		MaxAttempts:           3,
		BackoffInitialSeconds: 1,
		BackoffMaxSeconds:     5,
	})

	notifier := New(
		storeMock,
		WithHTTPClient(server.Client()),
		WithNow(func() time.Time { return now }),
		WithSleep(func(_ context.Context, _ time.Duration) error { return nil }),
	)

	inputs := []RevisionNotification{
		{
			TenantID:          7,
			TenantKey:         "tenant-alpha",
			RepoID:            9,
			RepoPath:          "group/repo",
			APISpecID:         101,
			API:               "api/customers",
			APISpecRevisionID: 5001,
			IngestEventID:     444,
			Sha:               "sha-444",
			Branch:            "main",
			ProcessedAt:       now,
			Artifact:          store.SpecArtifact{APISpecRevisionID: 5001, SpecJSON: []byte(`{"openapi":"3.1.0","paths":{}}`), SpecYAML: "openapi: 3.1.0\npaths: {}\n", ETag: "\"etag-5001\"", SizeBytes: 128},
			SpecChange:        store.SpecChange{APISpecID: 101, ToAPISpecRevisionID: 5001, ChangeJSON: []byte(`{"version":1}`)},
		},
		{
			TenantID:          7,
			TenantKey:         "tenant-alpha",
			RepoID:            9,
			RepoPath:          "group/repo",
			APISpecID:         102,
			API:               "api/orders",
			APISpecRevisionID: 5002,
			IngestEventID:     444,
			Sha:               "sha-444",
			Branch:            "main",
			ProcessedAt:       now,
			Artifact:          store.SpecArtifact{APISpecRevisionID: 5002, SpecJSON: []byte(`{"openapi":"3.1.0","paths":{}}`), SpecYAML: "openapi: 3.1.0\npaths: {}\n", ETag: "\"etag-5002\"", SizeBytes: 128},
			SpecChange:        store.SpecChange{APISpecID: 102, ToAPISpecRevisionID: 5002, ChangeJSON: []byte(`{"version":1}`)},
		},
	}

	for _, input := range inputs {
		if err := notifier.NotifyRevision(context.Background(), input); err != nil {
			t.Fatalf("NotifyRevision() unexpected error: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 4 {
		t.Fatalf("expected 4 outbound requests, got %d", len(received))
	}

	counts := map[string]map[string]struct{}{
		"api/customers": {store.DeliveryEventTypeSpecUpdatedFull: {}, store.DeliveryEventTypeSpecUpdatedDiff: {}},
		"api/orders":    {store.DeliveryEventTypeSpecUpdatedFull: {}, store.DeliveryEventTypeSpecUpdatedDiff: {}},
	}

	for _, req := range received {
		bucket, exists := counts[req.api]
		if !exists {
			t.Fatalf("unexpected api %q in outbound payload", req.api)
		}
		bucket[req.eventType] = struct{}{}
	}

	for api, types := range counts {
		if len(types) != 2 {
			t.Fatalf("expected full+diff for api=%q, got %d types", api, len(types))
		}
	}

	if len(storeMock.createCalls) != 4 {
		t.Fatalf("expected 4 created delivery attempts, got %d", len(storeMock.createCalls))
	}
}

func TestDispatchEvent_DedupeKeyedByAPISpecID(t *testing.T) {
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

	key := deliveryAttemptKey(99, 777, 555, store.DeliveryEventTypeSpecUpdatedDiff)
	storeMock.latest[key] = store.DeliveryAttempt{
		ID:             1,
		SubscriptionID: 99,
		APISpecID:      777,
		IngestEventID:  555,
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
			APISpecID:     888,
			RepoID:        2,
			IngestEventID: 555,
			DeliveryID:    "delivery-555",
			Sha:           "sha-555",
		},
		builtEvent{eventType: store.DeliveryEventTypeSpecUpdatedDiff, body: []byte(`{"type":"spec.updated.diff"}`)},
	)
	if err != nil {
		t.Fatalf("dispatchEvent() unexpected error: %v", err)
	}

	if client.calls != 1 {
		t.Fatalf("expected one HTTP call for different api key, got %d", client.calls)
	}
	if len(storeMock.createCalls) != 1 {
		t.Fatalf("expected one created delivery attempt, got %d", len(storeMock.createCalls))
	}
	if storeMock.createCalls[0].APISpecID != 888 {
		t.Fatalf("expected created attempt for api_spec_id=888, got %d", storeMock.createCalls[0].APISpecID)
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
					APISpecID:     111,
					RepoID:        2,
					IngestEventID: 555,
					DeliveryID:    "delivery-555",
					Sha:           "sha-555",
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
	key := deliveryAttemptKey(99, 777, 777, store.DeliveryEventTypeSpecUpdatedDiff)
	storeMock.latest[key] = store.DeliveryAttempt{
		ID:             1,
		SubscriptionID: 99,
		APISpecID:      777,
		IngestEventID:  777,
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
			APISpecID:     777,
			RepoID:        2,
			IngestEventID: 777,
			DeliveryID:    "delivery-777",
			Sha:           "sha-777",
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
			APISpecID:     901,
			RepoID:        2,
			IngestEventID: 901,
			DeliveryID:    "delivery-901",
			Sha:           "sha-901",
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
	apiSpecID int64,
	ingestEventID int64,
	eventType string,
) (store.DeliveryAttempt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	attempt, ok := f.latest[deliveryAttemptKey(subscriptionID, apiSpecID, ingestEventID, eventType)]
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
		APISpecID:      input.APISpecID,
		IngestEventID:  input.IngestEventID,
		EventType:      input.EventType,
		AttemptNo:      input.AttemptNo,
		Status:         input.Status,
		NextRetryAt:    cloneTime(input.NextRetryAt),
	}
	f.attempts[id] = attempt
	f.latest[deliveryAttemptKey(input.SubscriptionID, input.APISpecID, input.IngestEventID, input.EventType)] = attempt
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
	f.latest[deliveryAttemptKey(attempt.SubscriptionID, attempt.APISpecID, attempt.IngestEventID, attempt.EventType)] = attempt
	f.updateCalls = append(f.updateCalls, input)

	return attempt, nil
}

func deliveryAttemptKey(subscriptionID int64, apiSpecID int64, ingestEventID int64, eventType string) string {
	return strings.Join([]string{
		"sub", strconv.FormatInt(subscriptionID, 10),
		"api", strconv.FormatInt(apiSpecID, 10),
		"rev", strconv.FormatInt(ingestEventID, 10),
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
