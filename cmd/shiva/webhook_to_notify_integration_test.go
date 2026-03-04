package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/gitlab"
	httpserver "github.com/iw2rmb/shiva/internal/http"
	"github.com/iw2rmb/shiva/internal/notify"
	"github.com/iw2rmb/shiva/internal/openapi"
	"github.com/iw2rmb/shiva/internal/store"
	"github.com/iw2rmb/shiva/internal/worker"
)

func TestIntegrationWebhookToNotifyFlow(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	outboundCapture := &capturedOutboundRequests{}
	outboundReceiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		outboundCapture.record(capturedOutboundRequest{
			body:      body,
			signature: r.Header.Get(notify.HeaderSignature),
			timestamp: r.Header.Get(notify.HeaderTimestamp),
		})
		w.WriteHeader(http.StatusOK)
	}))
	defer outboundReceiver.Close()

	revisionStore := newIntegrationRevisionStore()
	notifierStore := newIntegrationNotifierStore(store.Subscription{
		ID:                    71,
		TenantID:              revisionStore.tenant.ID,
		RepoID:                revisionStore.repo.ID,
		TargetURL:             outboundReceiver.URL,
		Secret:                "notify-secret",
		Enabled:               true,
		MaxAttempts:           3,
		BackoffInitialSeconds: 1,
		BackoffMaxSeconds:     4,
	})
	notifier := notify.New(
		notifierStore,
		notify.WithHTTPClient(outboundReceiver.Client()),
		notify.WithSleep(func(_ context.Context, _ time.Duration) error { return nil }),
	)

	resolver, err := openapi.NewResolver(openapi.ResolverConfig{
		IncludeGlobs: []string{"api/**/*.yaml"},
		MaxFetches:   32,
	})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	gitlabClient := &integrationGitLabClient{
		changedPaths: []gitlab.ChangedPath{{NewPath: "api/openapi.yaml"}},
		files: map[string]string{
			"api/openapi.yaml":    "openapi: 3.1.0\ninfo:\n  title: Flow API\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n      summary: List pets\n      responses:\n        '200':\n          description: ok\n          content:\n            application/json:\n              schema:\n                $ref: ./components.yaml#/components/schemas/Pet\n",
			"api/components.yaml": "components:\n  schemas:\n    Pet:\n      type: object\n      properties:\n        id:\n          type: string\n",
		},
	}

	queue := newIntegrationQueue()
	processor := revisionProcessor{
		store:         revisionStore,
		gitlabClient:  gitlabClient,
		openapiLoader: resolver,
		notifier:      notifier,
		logger:        logger,
	}

	workerManager := worker.New(
		1,
		logger,
		worker.WithQueue(queue),
		worker.WithProcessor(processor),
		worker.WithPollDelay(1*time.Millisecond),
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := workerManager.Start(ctx); err != nil {
		t.Fatalf("worker.Start() unexpected error: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopCancel()
		if err := workerManager.Stop(stopCtx); err != nil {
			t.Fatalf("worker.Stop() unexpected error: %v", err)
		}
	})

	ingestor := &integrationWebhookIngestor{
		repoID: revisionStore.repo.ID,
		queue:  queue,
	}
	httpServer := httpserver.New(
		config.Config{
			HTTPAddr:            ":8080",
			GitLabWebhookSecret: "secret-token",
			TenantKey:           revisionStore.tenant.Key,
		},
		logger,
		&store.Store{},
		httpserver.WithGitLabWebhookIngestor(ingestor),
	)

	requestPayload := `{
	  "object_kind":"push",
	  "ref":"refs/heads/main",
	  "before":"1111111111111111111111111111111111111111",
	  "after":"2222222222222222222222222222222222222222",
	  "project":{"id":42,"path_with_namespace":"acme/platform-api","default_branch":"main"}
	}`
	request := httptest.NewRequest(http.MethodPost, "/internal/webhooks/gitlab", strings.NewReader(requestPayload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Gitlab-Token", "secret-token")
	request.Header.Set("X-Gitlab-Delivery", "delivery-1001")
	request.Header.Set("X-Gitlab-Event", "Push Hook")

	response, err := httpServer.App().Test(request, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("expected webhook status 202, got %d", response.StatusCode)
	}

	if !waitUntil(3*time.Second, func() bool {
		return queue.processedCount() == 1 && outboundCapture.count() == 2
	}) {
		t.Fatalf("timed out waiting for webhook->worker->notify flow")
	}

	revision, err := revisionStore.latestRevisionBySHA("2222222222222222222222222222222222222222")
	if err != nil {
		t.Fatalf("latestRevisionBySHA() unexpected error: %v", err)
	}
	if revision.Status != "processed" {
		t.Fatalf("expected processed revision status, got %q", revision.Status)
	}
	if revision.OpenAPIChanged == nil || !*revision.OpenAPIChanged {
		t.Fatalf("expected openapi_changed=true")
	}

	artifact, err := revisionStore.GetSpecArtifactByRevisionID(context.Background(), revision.ID)
	if err != nil {
		t.Fatalf("GetSpecArtifactByRevisionID() unexpected error: %v", err)
	}
	if len(artifact.SpecJSON) == 0 || artifact.SpecYAML == "" {
		t.Fatalf("expected canonical artifact payloads")
	}

	specChange, err := revisionStore.GetSpecChangeByToRevision(context.Background(), revision.ID)
	if err != nil {
		t.Fatalf("GetSpecChangeByToRevision() unexpected error: %v", err)
	}
	if len(specChange.ChangeJSON) == 0 {
		t.Fatalf("expected spec change payload")
	}

	eventTypes := outboundCapture.eventTypes(t)
	expectedTypes := []string{store.DeliveryEventTypeSpecUpdatedDiff, store.DeliveryEventTypeSpecUpdatedFull}
	for _, expectedType := range expectedTypes {
		if !containsString(eventTypes, expectedType) {
			t.Fatalf("expected outbound event type %q, got %v", expectedType, eventTypes)
		}
	}
	for _, request := range outboundCapture.snapshot() {
		if strings.TrimSpace(request.signature) == "" {
			t.Fatalf("expected signature header on outbound request")
		}
		if strings.TrimSpace(request.timestamp) == "" {
			t.Fatalf("expected timestamp header on outbound request")
		}
	}
}

type integrationWebhookIngestor struct {
	mu          sync.Mutex
	nextEventID int64
	repoID      int64
	queue       *integrationQueue
}

func (f *integrationWebhookIngestor) PersistGitLabWebhook(
	_ context.Context,
	input store.GitLabIngestInput,
) (store.GitLabIngestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.nextEventID++
	eventID := f.nextEventID
	f.queue.enqueue(worker.QueueJob{
		EventID:      eventID,
		RepoID:       f.repoID,
		DeliveryID:   input.DeliveryID,
		Sha:          input.Sha,
		Branch:       input.Branch,
		ParentSha:    input.ParentSha,
		AttemptCount: 0,
	})

	return store.GitLabIngestResult{EventID: eventID, RepoID: f.repoID, Duplicate: false}, nil
}

type queueItem struct {
	job       worker.QueueJob
	status    string
	nextRetry time.Time
}

type integrationQueue struct {
	mu        sync.Mutex
	order     []int64
	items     map[int64]*queueItem
	processed int
}

func newIntegrationQueue() *integrationQueue {
	return &integrationQueue{items: make(map[int64]*queueItem)}
}

func (q *integrationQueue) enqueue(job worker.QueueJob) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.order = append(q.order, job.EventID)
	q.items[job.EventID] = &queueItem{job: job, status: "pending", nextRetry: time.Now()}
}

func (q *integrationQueue) ClaimNext(_ context.Context) (worker.QueueJob, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	for _, eventID := range q.order {
		item := q.items[eventID]
		if item == nil || item.status != "pending" || now.Before(item.nextRetry) {
			continue
		}
		item.status = "processing"
		item.job.AttemptCount++
		return item.job, true, nil
	}
	return worker.QueueJob{}, false, nil
}

func (q *integrationQueue) MarkProcessed(_ context.Context, eventID int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	item := q.items[eventID]
	if item == nil {
		return fmt.Errorf("queue event %d not found", eventID)
	}
	item.status = "processed"
	q.processed++
	return nil
}

func (q *integrationQueue) ScheduleRetry(_ context.Context, eventID int64, nextRetryAt time.Time, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	item := q.items[eventID]
	if item == nil {
		return fmt.Errorf("queue event %d not found", eventID)
	}
	item.status = "pending"
	item.nextRetry = nextRetryAt
	return nil
}

func (q *integrationQueue) MarkFailed(_ context.Context, eventID int64, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	item := q.items[eventID]
	if item == nil {
		return fmt.Errorf("queue event %d not found", eventID)
	}
	item.status = "failed"
	return nil
}

func (q *integrationQueue) processedCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.processed
}

type integrationGitLabClient struct {
	changedPaths []gitlab.ChangedPath
	treeEntries  []gitlab.TreeEntry
	files        map[string]string
}

func (c *integrationGitLabClient) CompareChangedPaths(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]gitlab.ChangedPath, error) {
	return c.changedPaths, nil
}

func (c *integrationGitLabClient) GetFileContent(
	_ context.Context,
	_ int64,
	filePath,
	_ string,
) ([]byte, error) {
	content, exists := c.files[filePath]
	if !exists {
		return nil, fmt.Errorf("%w: path=%s", gitlab.ErrNotFound, filePath)
	}
	return []byte(content), nil
}

func (c *integrationGitLabClient) ListRepositoryTree(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
	_ bool,
) ([]gitlab.TreeEntry, error) {
	return c.treeEntries, nil
}

type integrationRevisionStore struct {
	mu             sync.Mutex
	tenant         store.Tenant
	repo           store.Repo
	bootstrapState store.RepoBootstrapState
	nextRevisionID int64
	revisions      map[int64]store.Revision
	revisionBySHA  map[string]int64
	artifacts      map[int64]store.SpecArtifact
	endpoints      map[int64][]store.EndpointIndexRecord
	specChanges    map[int64]store.SpecChange
}

func newIntegrationRevisionStore() *integrationRevisionStore {
	return &integrationRevisionStore{
		tenant: store.Tenant{ID: 5, Key: "tenant-a"},
		repo: store.Repo{
			ID:                44,
			TenantID:          5,
			GitLabProjectID:   42,
			PathWithNamespace: "acme/platform-api",
			DefaultBranch:     "main",
		},
		bootstrapState: store.RepoBootstrapState{
			ActiveAPICount: 1,
			ForceRescan:    false,
		},
		nextRevisionID: 1000,
		revisions:      make(map[int64]store.Revision),
		revisionBySHA:  make(map[string]int64),
		artifacts:      make(map[int64]store.SpecArtifact),
		endpoints:      make(map[int64][]store.EndpointIndexRecord),
		specChanges:    make(map[int64]store.SpecChange),
	}
}

func (s *integrationRevisionStore) UpsertRevisionFromIngestEvent(
	_ context.Context,
	event store.IngestQueueEvent,
) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextRevisionID++
	revisionID := s.nextRevisionID
	revision := store.Revision{
		ID:        revisionID,
		RepoID:    event.RepoID,
		Sha:       event.Sha,
		Branch:    event.Branch,
		ParentSHA: event.ParentSha,
		Status:    "processing",
	}
	s.revisions[revisionID] = revision
	s.revisionBySHA[event.Sha] = revisionID
	return revisionID, nil
}

func (s *integrationRevisionStore) MarkRevisionProcessed(_ context.Context, revisionID int64, openapiChanged bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision, exists := s.revisions[revisionID]
	if !exists {
		return fmt.Errorf("revision %d not found", revisionID)
	}
	processedAt := time.Now().UTC()
	revision.ProcessedAt = &processedAt
	revision.Status = "processed"
	revision.OpenAPIChanged = boolPtr(openapiChanged)
	s.revisions[revisionID] = revision
	return nil
}

func (s *integrationRevisionStore) MarkRevisionFailed(_ context.Context, revisionID int64, errorMessage string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision, exists := s.revisions[revisionID]
	if !exists {
		return fmt.Errorf("revision %d not found", revisionID)
	}
	revision.Status = "failed"
	revision.Error = strings.TrimSpace(errorMessage)
	s.revisions[revisionID] = revision
	return nil
}

func (s *integrationRevisionStore) GetRepoByID(_ context.Context, repoID int64) (store.Repo, error) {
	if s.repo.ID != repoID {
		return store.Repo{}, fmt.Errorf("repo %d not found", repoID)
	}
	return s.repo, nil
}

func (s *integrationRevisionStore) GetRepoBootstrapState(
	_ context.Context,
	repoID int64,
) (store.RepoBootstrapState, error) {
	if s.repo.ID != repoID {
		return store.RepoBootstrapState{}, fmt.Errorf("repo %d not found", repoID)
	}
	return s.bootstrapState, nil
}

func (s *integrationRevisionStore) ClearRepoForceRescan(_ context.Context, repoID int64) error {
	if s.repo.ID != repoID {
		return fmt.Errorf("repo %d not found", repoID)
	}
	s.bootstrapState.ForceRescan = false
	return nil
}

func (s *integrationRevisionStore) UpsertAPISpec(_ context.Context, _ store.UpsertAPISpecInput) (store.APISpec, error) {
	return store.APISpec{}, fmt.Errorf("unexpected UpsertAPISpec call")
}

func (s *integrationRevisionStore) CreateAPISpecRevision(
	_ context.Context,
	_ store.CreateAPISpecRevisionInput,
) (store.APISpecRevision, error) {
	return store.APISpecRevision{}, fmt.Errorf("unexpected CreateAPISpecRevision call")
}

func (s *integrationRevisionStore) ReplaceAPISpecDependencies(
	_ context.Context,
	_ store.ReplaceAPISpecDependenciesInput,
) error {
	return fmt.Errorf("unexpected ReplaceAPISpecDependencies call")
}

func (s *integrationRevisionStore) PersistCanonicalSpec(_ context.Context, input store.PersistCanonicalSpecInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.artifacts[input.RevisionID] = store.SpecArtifact{
		RevisionID: input.RevisionID,
		SpecJSON:   append([]byte(nil), input.SpecJSON...),
		SpecYAML:   input.SpecYAML,
		ETag:       input.ETag,
		SizeBytes:  input.SizeBytes,
	}
	rows := make([]store.EndpointIndexRecord, len(input.Endpoints))
	copy(rows, input.Endpoints)
	s.endpoints[input.RevisionID] = rows
	return nil
}

func (s *integrationRevisionStore) GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
	_ context.Context,
	repoID int64,
	branch string,
	excludeRevisionID int64,
) (store.Revision, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var latest store.Revision
	found := false
	for _, revision := range s.revisions {
		if revision.RepoID != repoID || revision.Branch != branch || revision.ID == excludeRevisionID {
			continue
		}
		if revision.Status != "processed" || revision.OpenAPIChanged == nil || !*revision.OpenAPIChanged {
			continue
		}
		if !found || revision.ID > latest.ID {
			latest = revision
			found = true
		}
	}
	if !found {
		return store.Revision{}, false, nil
	}
	return latest, true, nil
}

func (s *integrationRevisionStore) ListEndpointIndexByRevision(
	_ context.Context,
	revisionID int64,
) ([]store.EndpointIndexRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, exists := s.endpoints[revisionID]
	if !exists {
		return nil, nil
	}
	copyRows := make([]store.EndpointIndexRecord, len(rows))
	copy(copyRows, rows)
	return copyRows, nil
}

func (s *integrationRevisionStore) PersistSpecChange(_ context.Context, input store.PersistSpecChangeInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.specChanges[input.ToRevisionID] = store.SpecChange{
		RepoID:         input.RepoID,
		FromRevisionID: input.FromRevisionID,
		ToRevisionID:   input.ToRevisionID,
		ChangeJSON:     append([]byte(nil), input.ChangeJSON...),
	}
	return nil
}

func (s *integrationRevisionStore) GetTenantByID(_ context.Context, tenantID int64) (store.Tenant, error) {
	if s.tenant.ID != tenantID {
		return store.Tenant{}, fmt.Errorf("tenant %d not found", tenantID)
	}
	return s.tenant, nil
}

func (s *integrationRevisionStore) GetRevisionByID(_ context.Context, revisionID int64) (store.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision, exists := s.revisions[revisionID]
	if !exists {
		return store.Revision{}, fmt.Errorf("revision %d not found", revisionID)
	}
	return revision, nil
}

func (s *integrationRevisionStore) GetSpecArtifactByRevisionID(_ context.Context, revisionID int64) (store.SpecArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	artifact, exists := s.artifacts[revisionID]
	if !exists {
		return store.SpecArtifact{}, fmt.Errorf("artifact revision %d not found", revisionID)
	}
	return artifact, nil
}

func (s *integrationRevisionStore) GetSpecChangeByToRevision(_ context.Context, toRevisionID int64) (store.SpecChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	specChange, exists := s.specChanges[toRevisionID]
	if !exists {
		return store.SpecChange{}, fmt.Errorf("spec change revision %d not found", toRevisionID)
	}
	return specChange, nil
}

func (s *integrationRevisionStore) latestRevisionBySHA(sha string) (store.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	revisionID, exists := s.revisionBySHA[sha]
	if !exists {
		return store.Revision{}, fmt.Errorf("revision for sha %q not found", sha)
	}
	revision, exists := s.revisions[revisionID]
	if !exists {
		return store.Revision{}, fmt.Errorf("revision id %d not found", revisionID)
	}
	return revision, nil
}

type integrationNotifierStore struct {
	mu            sync.Mutex
	subscriptions []store.Subscription
	latest        map[string]int64
	attempts      map[int64]store.DeliveryAttempt
	nextAttemptID int64
}

func newIntegrationNotifierStore(subscription store.Subscription) *integrationNotifierStore {
	return &integrationNotifierStore{
		subscriptions: []store.Subscription{subscription},
		latest:        make(map[string]int64),
		attempts:      make(map[int64]store.DeliveryAttempt),
		nextAttemptID: 100,
	}
}

func (s *integrationNotifierStore) ListEnabledSubscriptionsByRepo(
	_ context.Context,
	tenantID,
	repoID int64,
) ([]store.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]store.Subscription, 0, len(s.subscriptions))
	for _, subscription := range s.subscriptions {
		if subscription.Enabled && subscription.TenantID == tenantID && subscription.RepoID == repoID {
			result = append(result, subscription)
		}
	}
	return result, nil
}

func (s *integrationNotifierStore) GetLatestDeliveryAttemptByKey(
	_ context.Context,
	subscriptionID int64,
	revisionID int64,
	eventType string,
) (store.DeliveryAttempt, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fmt.Sprintf("%d:%d:%s", subscriptionID, revisionID, eventType)
	attemptID, exists := s.latest[key]
	if !exists {
		return store.DeliveryAttempt{}, false, nil
	}
	attempt, exists := s.attempts[attemptID]
	if !exists {
		return store.DeliveryAttempt{}, false, nil
	}
	return attempt, true, nil
}

func (s *integrationNotifierStore) CreateDeliveryAttempt(
	_ context.Context,
	input store.CreateDeliveryAttemptInput,
) (store.DeliveryAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextAttemptID++
	attempt := store.DeliveryAttempt{
		ID:             s.nextAttemptID,
		SubscriptionID: input.SubscriptionID,
		RevisionID:     input.RevisionID,
		EventType:      input.EventType,
		AttemptNo:      input.AttemptNo,
		Status:         input.Status,
		NextRetryAt:    input.NextRetryAt,
	}
	s.attempts[attempt.ID] = attempt
	key := fmt.Sprintf("%d:%d:%s", input.SubscriptionID, input.RevisionID, input.EventType)
	s.latest[key] = attempt.ID
	return attempt, nil
}

func (s *integrationNotifierStore) UpdateDeliveryAttemptResult(
	_ context.Context,
	input store.UpdateDeliveryAttemptResultInput,
) (store.DeliveryAttempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	attempt, exists := s.attempts[input.ID]
	if !exists {
		return store.DeliveryAttempt{}, fmt.Errorf("attempt %d not found", input.ID)
	}
	attempt.Status = input.Status
	attempt.ResponseCode = input.ResponseCode
	attempt.Error = input.Error
	attempt.NextRetryAt = input.NextRetryAt
	s.attempts[input.ID] = attempt
	return attempt, nil
}

type capturedOutboundRequest struct {
	body      []byte
	signature string
	timestamp string
}

type capturedOutboundRequests struct {
	mu       sync.Mutex
	requests []capturedOutboundRequest
}

func (c *capturedOutboundRequests) record(request capturedOutboundRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, request)
}

func (c *capturedOutboundRequests) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.requests)
}

func (c *capturedOutboundRequests) snapshot() []capturedOutboundRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows := make([]capturedOutboundRequest, len(c.requests))
	copy(rows, c.requests)
	return rows
}

func (c *capturedOutboundRequests) eventTypes(t *testing.T) []string {
	t.Helper()

	rows := c.snapshot()
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		var envelope map[string]any
		if err := json.Unmarshal(row.body, &envelope); err != nil {
			t.Fatalf("json.Unmarshal outbound body: %v", err)
		}
		eventType, _ := envelope["type"].(string)
		values = append(values, eventType)
	}
	return values
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func waitUntil(timeout time.Duration, condition func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return condition()
}

func boolPtr(value bool) *bool {
	return &value
}
