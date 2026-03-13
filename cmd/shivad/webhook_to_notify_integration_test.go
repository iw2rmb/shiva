package main

import (
	"context"
	"encoding/json"
	"errors"
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

	queue := newIntegrationQueue(revisionStore)
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
		repoID:        revisionStore.repo.ID,
		queue:         queue,
		revisionStore: revisionStore,
	}
	httpServer := httpserver.New(
		config.Config{
			HTTPAddr:            ":8080",
			GitLabWebhookSecret: "secret-token",
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

func TestIntegrationWebhookToNotifyFlow_BootstrapRegressionGuards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		treeEntries        []gitlab.TreeEntry
		files              map[string]string
		wantOpenAPIChanged bool
		wantOutboundCount  int
		wantEndpointCount  int
		wantCompareCalls   int
		wantTreeCalls      int
	}{
		{
			name: "bootstrap builds from repository tree when compare has no openapi files",
			treeEntries: []gitlab.TreeEntry{
				{Path: "api/openapi.yaml", Type: "blob"},
				{Path: "api/components.yaml", Type: "blob"},
				{Path: "README.md", Type: "blob"},
			},
			files: map[string]string{
				"api/openapi.yaml":    "openapi: 3.1.0\ninfo:\n  title: Bootstrap Flow API\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPetsBootstrap\n      summary: List pets from bootstrap\n      responses:\n        '200':\n          description: ok\n          content:\n            application/json:\n              schema:\n                $ref: ./components.yaml#/components/schemas/Pet\n",
				"api/components.yaml": "components:\n  schemas:\n    Pet:\n      type: object\n      properties:\n        id:\n          type: string\n",
				"README.md":           "# bootstrap regression fixture\n",
			},
			wantOpenAPIChanged: true,
			wantOutboundCount:  2,
			wantEndpointCount:  1,
			wantCompareCalls:   0,
			wantTreeCalls:      1,
		},
		{
			name: "bootstrap zero roots marks unchanged and sends no notifications",
			treeEntries: []gitlab.TreeEntry{
				{Path: "README.md", Type: "blob"},
				{Path: "docs/changelog.txt", Type: "blob"},
			},
			files: map[string]string{
				"README.md":          "# no openapi roots here\n",
				"docs/changelog.txt": "release notes\n",
			},
			wantOpenAPIChanged: false,
			wantOutboundCount:  0,
			wantEndpointCount:  0,
			wantCompareCalls:   0,
			wantTreeCalls:      1,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := runWebhookToNotifyIntegrationCase(t, integrationWebhookToNotifyCase{
				parentSHA:      "1111111111111111111111111111111111111111",
				targetSHA:      "3333333333333333333333333333333333333333",
				bootstrapState: store.RepoBootstrapState{ActiveAPICount: 0, ForceRescan: false},
				changedPaths: []gitlab.ChangedPath{
					{NewPath: "README.md"},
					{NewPath: "docs/changelog.txt"},
				},
				treeEntries:  tc.treeEntries,
				files:        tc.files,
				waitOutbound: tc.wantOutboundCount,
			})

			revision := result.revision
			if revision.Status != "processed" {
				t.Fatalf("expected processed revision status, got %q", revision.Status)
			}
			if revision.OpenAPIChanged == nil {
				t.Fatalf("expected non-nil openapi_changed")
			}
			if *revision.OpenAPIChanged != tc.wantOpenAPIChanged {
				t.Fatalf("expected openapi_changed=%t, got %t", tc.wantOpenAPIChanged, *revision.OpenAPIChanged)
			}

			endpoints, err := result.revisionStore.ListEndpointIndexByRevision(context.Background(), revision.ID)
			if err != nil {
				t.Fatalf("ListEndpointIndexByRevision() unexpected error: %v", err)
			}
			if len(endpoints) != tc.wantEndpointCount {
				t.Fatalf("expected %d endpoint rows, got %d", tc.wantEndpointCount, len(endpoints))
			}

			if tc.wantOpenAPIChanged {
				artifact, err := result.revisionStore.GetSpecArtifactByRevisionID(context.Background(), revision.ID)
				if err != nil {
					t.Fatalf("GetSpecArtifactByRevisionID() unexpected error: %v", err)
				}
				if len(artifact.SpecJSON) == 0 || artifact.SpecYAML == "" {
					t.Fatalf("expected canonical artifact payloads")
				}

				specChange, err := result.revisionStore.GetSpecChangeByToRevision(context.Background(), revision.ID)
				if err != nil {
					t.Fatalf("GetSpecChangeByToRevision() unexpected error: %v", err)
				}
				if len(specChange.ChangeJSON) == 0 {
					t.Fatalf("expected spec change payload")
				}
			} else {
				if _, err := result.revisionStore.GetSpecArtifactByRevisionID(context.Background(), revision.ID); err == nil {
					t.Fatalf("expected no canonical artifact for openapi_changed=false")
				}
				if _, err := result.revisionStore.GetSpecChangeByToRevision(context.Background(), revision.ID); err == nil {
					t.Fatalf("expected no spec change for openapi_changed=false")
				}
			}

			outboundCount := result.outboundCapture.count()
			if outboundCount != tc.wantOutboundCount {
				t.Fatalf("expected %d outbound notifications, got %d", tc.wantOutboundCount, outboundCount)
			}
			if tc.wantOutboundCount > 0 {
				eventTypes := result.outboundCapture.eventTypes(t)
				expectedTypes := []string{store.DeliveryEventTypeSpecUpdatedDiff, store.DeliveryEventTypeSpecUpdatedFull}
				for _, expectedType := range expectedTypes {
					if !containsString(eventTypes, expectedType) {
						t.Fatalf("expected outbound event type %q, got %v", expectedType, eventTypes)
					}
				}
				for _, request := range result.outboundCapture.snapshot() {
					if strings.TrimSpace(request.signature) == "" {
						t.Fatalf("expected signature header on outbound request")
					}
					if strings.TrimSpace(request.timestamp) == "" {
						t.Fatalf("expected timestamp header on outbound request")
					}
				}
			}

			if got := result.gitlabClient.compareCallCount(); got != tc.wantCompareCalls {
				t.Fatalf("expected %d compare calls, got %d", tc.wantCompareCalls, got)
			}
			if got := result.gitlabClient.treeCallCount(); got != tc.wantTreeCalls {
				t.Fatalf("expected %d repository-tree calls, got %d", tc.wantTreeCalls, got)
			}
		})
	}
}

func TestIntegrationWebhookToNotifyFlow_IncrementalDeletedRootEmitsDiffOnly(t *testing.T) {
	t.Parallel()

	result := runWebhookToNotifyIntegrationCase(t, integrationWebhookToNotifyCase{
		parentSHA:      "1111111111111111111111111111111111111111",
		targetSHA:      "4444444444444444444444444444444444444444",
		bootstrapState: store.RepoBootstrapState{ActiveAPICount: 1, ForceRescan: false},
		changedPaths: []gitlab.ChangedPath{
			{OldPath: "api/openapi.yaml", DeletedFile: true},
		},
		files:        map[string]string{},
		waitOutbound: 1,
	})

	revision := result.revision
	if revision.Status != "processed" {
		t.Fatalf("expected processed revision status, got %q", revision.Status)
	}
	if revision.OpenAPIChanged == nil || !*revision.OpenAPIChanged {
		t.Fatalf("expected openapi_changed=true for deleted-root incremental revision")
	}

	if _, err := result.revisionStore.GetSpecArtifactByRevisionID(context.Background(), revision.ID); !errors.Is(err, store.ErrSpecArtifactNotFound) {
		t.Fatalf("expected ErrSpecArtifactNotFound for deleted-root revision artifact lookup, got %v", err)
	}

	specChange, err := result.revisionStore.GetSpecChangeByToRevision(context.Background(), revision.ID)
	if err != nil {
		t.Fatalf("GetSpecChangeByToRevision() unexpected error: %v", err)
	}
	if len(specChange.ChangeJSON) == 0 {
		t.Fatalf("expected spec change payload for deleted-root revision")
	}

	activeSpecs, err := result.revisionStore.ListActiveAPISpecsWithLatestDependencies(context.Background(), result.revisionStore.repo.ID)
	if err != nil {
		t.Fatalf("ListActiveAPISpecsWithLatestDependencies() unexpected error: %v", err)
	}
	if len(activeSpecs) != 0 {
		t.Fatalf("expected deleted root to be deactivated, got %d active specs", len(activeSpecs))
	}

	if outboundCount := result.outboundCapture.count(); outboundCount != 1 {
		t.Fatalf("expected 1 outbound notification, got %d", outboundCount)
	}
	eventTypes := result.outboundCapture.eventTypes(t)
	if len(eventTypes) != 1 || eventTypes[0] != store.DeliveryEventTypeSpecUpdatedDiff {
		t.Fatalf("expected only %q event, got %v", store.DeliveryEventTypeSpecUpdatedDiff, eventTypes)
	}
	for _, request := range result.outboundCapture.snapshot() {
		if strings.TrimSpace(request.signature) == "" {
			t.Fatalf("expected signature header on outbound request")
		}
		if strings.TrimSpace(request.timestamp) == "" {
			t.Fatalf("expected timestamp header on outbound request")
		}
	}
}

type integrationWebhookToNotifyCase struct {
	parentSHA      string
	targetSHA      string
	bootstrapState store.RepoBootstrapState
	changedPaths   []gitlab.ChangedPath
	treeEntries    []gitlab.TreeEntry
	files          map[string]string
	waitOutbound   int
}

type integrationWebhookToNotifyResult struct {
	revision        store.Revision
	revisionStore   *integrationRevisionStore
	outboundCapture *capturedOutboundRequests
	gitlabClient    *integrationGitLabClient
}

func runWebhookToNotifyIntegrationCase(
	t *testing.T,
	tc integrationWebhookToNotifyCase,
) integrationWebhookToNotifyResult {
	t.Helper()

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
	revisionStore.bootstrapState = tc.bootstrapState

	notifierStore := newIntegrationNotifierStore(store.Subscription{
		ID:                    71,
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
		changedPaths: tc.changedPaths,
		treeEntries:  tc.treeEntries,
		files:        tc.files,
	}

	queue := newIntegrationQueue(revisionStore)
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
		repoID:        revisionStore.repo.ID,
		queue:         queue,
		revisionStore: revisionStore,
	}
	httpServer := httpserver.New(
		config.Config{
			HTTPAddr:            ":8080",
			GitLabWebhookSecret: "secret-token",
		},
		logger,
		&store.Store{},
		httpserver.WithGitLabWebhookIngestor(ingestor),
	)

	requestPayload := fmt.Sprintf(`{
	  "object_kind":"push",
	  "ref":"refs/heads/main",
	  "before":"%s",
	  "after":"%s",
	  "project":{"id":42,"path_with_namespace":"acme/platform-api","default_branch":"main"}
	}`, tc.parentSHA, tc.targetSHA)
	request := httptest.NewRequest(http.MethodPost, "/internal/webhooks/gitlab", strings.NewReader(requestPayload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Gitlab-Token", "secret-token")
	request.Header.Set("X-Gitlab-Delivery", "delivery-2001")
	request.Header.Set("X-Gitlab-Event", "Push Hook")

	response, err := httpServer.App().Test(request, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("expected webhook status 202, got %d", response.StatusCode)
	}

	if tc.waitOutbound > 0 {
		if !waitUntil(3*time.Second, func() bool {
			return queue.processedCount() == 1 && outboundCapture.count() >= tc.waitOutbound
		}) {
			t.Fatalf("timed out waiting for webhook->worker->notify flow")
		}
	} else {
		if !waitUntil(3*time.Second, func() bool {
			return queue.processedCount() == 1
		}) {
			t.Fatalf("timed out waiting for webhook->worker flow")
		}
	}

	revision, err := revisionStore.latestRevisionBySHA(tc.targetSHA)
	if err != nil {
		t.Fatalf("latestRevisionBySHA() unexpected error: %v", err)
	}

	return integrationWebhookToNotifyResult{
		revision:        revision,
		revisionStore:   revisionStore,
		outboundCapture: outboundCapture,
		gitlabClient:    gitlabClient,
	}
}

type integrationWebhookIngestor struct {
	mu            sync.Mutex
	nextEventID   int64
	repoID        int64
	queue         *integrationQueue
	revisionStore *integrationRevisionStore
}

func (f *integrationWebhookIngestor) PersistGitLabWebhook(
	_ context.Context,
	input store.GitLabIngestInput,
) (store.GitLabIngestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.nextEventID++
	eventID := f.nextEventID
	job := worker.QueueJob{
		EventID:      eventID,
		RepoID:       f.repoID,
		DeliveryID:   input.DeliveryID,
		Sha:          input.Sha,
		Branch:       input.Branch,
		ParentSha:    input.ParentSha,
		AttemptCount: 0,
	}
	if f.revisionStore != nil {
		f.revisionStore.recordIngestEvent(job)
	}
	f.queue.enqueue(job)

	return store.GitLabIngestResult{EventID: eventID, RepoID: f.repoID, Duplicate: false}, nil
}

type queueItem struct {
	job       worker.QueueJob
	status    string
	nextRetry time.Time
}

type integrationQueue struct {
	mu            sync.Mutex
	order         []int64
	items         map[int64]*queueItem
	processed     int
	revisionStore *integrationRevisionStore
}

func newIntegrationQueue(revisionStore *integrationRevisionStore) *integrationQueue {
	return &integrationQueue{
		items:         make(map[int64]*queueItem),
		revisionStore: revisionStore,
	}
}

func (q *integrationQueue) enqueue(job worker.QueueJob) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.order = append(q.order, job.EventID)
	q.items[job.EventID] = &queueItem{job: job, status: "pending", nextRetry: time.Now()}
}

func (q *integrationQueue) ClaimNext(_ context.Context) (worker.QueueJob, bool, error) {
	q.mu.Lock()
	now := time.Now()
	for _, eventID := range q.order {
		item := q.items[eventID]
		if item == nil || item.status != "pending" || now.Before(item.nextRetry) {
			continue
		}
		item.status = "processing"
		item.job.AttemptCount++
		job := item.job
		revisionStore := q.revisionStore
		q.mu.Unlock()
		if revisionStore != nil {
			if err := revisionStore.markProcessing(job.EventID); err != nil {
				return worker.QueueJob{}, false, err
			}
		}
		return job, true, nil
	}
	q.mu.Unlock()
	return worker.QueueJob{}, false, nil
}

func (q *integrationQueue) MarkProcessed(_ context.Context, eventID int64, result worker.ProcessResult) error {
	q.mu.Lock()
	item := q.items[eventID]
	if item == nil {
		q.mu.Unlock()
		return fmt.Errorf("queue event %d not found", eventID)
	}
	item.status = "processed"
	q.processed++
	revisionStore := q.revisionStore
	q.mu.Unlock()
	if revisionStore != nil {
		return revisionStore.markProcessed(eventID, result.OpenAPIChanged)
	}
	return nil
}

func (q *integrationQueue) ScheduleRetry(_ context.Context, eventID int64, nextRetryAt time.Time, _ string) error {
	q.mu.Lock()
	item := q.items[eventID]
	if item == nil {
		q.mu.Unlock()
		return fmt.Errorf("queue event %d not found", eventID)
	}
	item.status = "pending"
	item.nextRetry = nextRetryAt
	revisionStore := q.revisionStore
	q.mu.Unlock()
	if revisionStore != nil {
		return revisionStore.markPending(eventID)
	}
	return nil
}

func (q *integrationQueue) MarkFailed(_ context.Context, eventID int64, _ string) error {
	q.mu.Lock()
	item := q.items[eventID]
	if item == nil {
		q.mu.Unlock()
		return fmt.Errorf("queue event %d not found", eventID)
	}
	item.status = "failed"
	revisionStore := q.revisionStore
	q.mu.Unlock()
	if revisionStore != nil {
		return revisionStore.markFailed(eventID)
	}
	return nil
}

func (q *integrationQueue) processedCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.processed
}

type integrationGitLabClient struct {
	mu           sync.Mutex
	changedPaths []gitlab.ChangedPath
	treeEntries  []gitlab.TreeEntry
	files        map[string]string
	compareCalls int
	treeCalls    int
}

func (c *integrationGitLabClient) CompareChangedPaths(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]gitlab.ChangedPath, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.compareCalls++
	changed := make([]gitlab.ChangedPath, len(c.changedPaths))
	copy(changed, c.changedPaths)
	return changed, nil
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
	c.mu.Lock()
	defer c.mu.Unlock()

	c.treeCalls++
	tree := make([]gitlab.TreeEntry, len(c.treeEntries))
	copy(tree, c.treeEntries)
	return tree, nil
}

func (c *integrationGitLabClient) compareCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.compareCalls
}

func (c *integrationGitLabClient) treeCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.treeCalls
}

type integrationRevisionStore struct {
	mu              sync.Mutex
	repo            store.Repo
	bootstrapState  store.RepoBootstrapState
	nextAPISpecID   int64
	nextSpecRevID   int64
	revisions       map[int64]store.Revision
	revisionBySHA   map[string]int64
	artifacts       map[int64]store.SpecArtifact
	endpoints       map[int64][]store.EndpointIndexRecord
	specChanges     map[string]store.SpecChange
	apiSpecs        map[int64]store.APISpec
	apiSpecByRoot   map[string]int64
	apiSpecRevs     map[int64]store.APISpecRevision
	apiSpecRevByKey map[string]int64
	dependencies    map[int64][]string
}

func newIntegrationRevisionStore() *integrationRevisionStore {
	s := &integrationRevisionStore{
		repo: store.Repo{
			ID:              44,
			GitLabProjectID: 42,
			Namespace:       "acme", Repo: "platform-api",
			DefaultBranch: "main",
		},
		bootstrapState: store.RepoBootstrapState{
			ActiveAPICount: 1,
			ForceRescan:    false,
		},
		nextAPISpecID:   400,
		nextSpecRevID:   700,
		revisions:       make(map[int64]store.Revision),
		revisionBySHA:   make(map[string]int64),
		artifacts:       make(map[int64]store.SpecArtifact),
		endpoints:       make(map[int64][]store.EndpointIndexRecord),
		specChanges:     make(map[string]store.SpecChange),
		apiSpecs:        make(map[int64]store.APISpec),
		apiSpecByRoot:   make(map[string]int64),
		apiSpecRevs:     make(map[int64]store.APISpecRevision),
		apiSpecRevByKey: make(map[string]int64),
		dependencies:    make(map[int64][]string),
	}

	processedAt := time.Now().UTC()
	s.revisions[999] = store.Revision{
		ID:             999,
		RepoID:         s.repo.ID,
		Sha:            "1111111111111111111111111111111111111111",
		Branch:         "main",
		ProcessedAt:    &processedAt,
		OpenAPIChanged: boolPtr(true),
		Status:         "processed",
	}
	s.revisionBySHA["1111111111111111111111111111111111111111"] = 999

	s.apiSpecs[400] = store.APISpec{
		ID:       400,
		RepoID:   s.repo.ID,
		RootPath: "api/openapi.yaml",
		Status:   "active",
	}
	s.apiSpecByRoot["api/openapi.yaml"] = 400
	s.apiSpecRevs[700] = store.APISpecRevision{
		ID:                 700,
		APISpecID:          400,
		IngestEventID:      999,
		RootPathAtRevision: "api/openapi.yaml",
		BuildStatus:        apiSpecRevisionBuildStatusProcessed,
	}
	s.apiSpecRevByKey["400:999"] = 700
	s.dependencies[700] = []string{"api/components.yaml", "api/openapi.yaml"}

	return s
}

func (s *integrationRevisionStore) recordIngestEvent(event worker.QueueJob) {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision := store.Revision{
		ID:        event.EventID,
		RepoID:    event.RepoID,
		Sha:       event.Sha,
		Branch:    event.Branch,
		ParentSHA: event.ParentSha,
		Status:    "pending",
	}
	s.revisions[event.EventID] = revision
	s.revisionBySHA[event.Sha] = event.EventID
}

func (s *integrationRevisionStore) markPending(ingestEventID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision, exists := s.revisions[ingestEventID]
	if !exists {
		return fmt.Errorf("revision %d not found", ingestEventID)
	}
	revision.Status = "pending"
	s.revisions[ingestEventID] = revision
	return nil
}

func (s *integrationRevisionStore) markProcessing(ingestEventID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision, exists := s.revisions[ingestEventID]
	if !exists {
		return fmt.Errorf("revision %d not found", ingestEventID)
	}
	revision.Status = "processing"
	s.revisions[ingestEventID] = revision
	return nil
}

func (s *integrationRevisionStore) markProcessed(ingestEventID int64, openapiChanged bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision, exists := s.revisions[ingestEventID]
	if !exists {
		return fmt.Errorf("revision %d not found", ingestEventID)
	}
	processedAt := time.Now().UTC()
	revision.ProcessedAt = &processedAt
	revision.Status = "processed"
	revision.OpenAPIChanged = boolPtr(openapiChanged)
	s.revisions[ingestEventID] = revision
	return nil
}

func (s *integrationRevisionStore) markFailed(ingestEventID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision, exists := s.revisions[ingestEventID]
	if !exists {
		return fmt.Errorf("revision %d not found", ingestEventID)
	}
	revision.Status = "failed"
	s.revisions[ingestEventID] = revision
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

func (s *integrationRevisionStore) UpsertAPISpec(_ context.Context, input store.UpsertAPISpecInput) (store.APISpec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	specID, exists := s.apiSpecByRoot[input.RootPath]
	if exists {
		spec := s.apiSpecs[specID]
		spec.Status = "active"
		s.apiSpecs[specID] = spec
		return spec, nil
	}

	s.nextAPISpecID++
	spec := store.APISpec{
		ID:       s.nextAPISpecID,
		RepoID:   input.RepoID,
		RootPath: input.RootPath,
		Status:   "active",
	}
	s.apiSpecs[spec.ID] = spec
	s.apiSpecByRoot[input.RootPath] = spec.ID
	return spec, nil
}

func (s *integrationRevisionStore) ListActiveAPISpecsWithLatestDependencies(
	_ context.Context,
	repoID int64,
) ([]store.ActiveAPISpecWithLatestDependencies, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows := make([]store.ActiveAPISpecWithLatestDependencies, 0, len(s.apiSpecs))
	for _, spec := range s.apiSpecs {
		if spec.RepoID != repoID || spec.Status != "active" {
			continue
		}

		var latestProcessedRevisionID int64
		var latestSpecRevisionID int64
		for _, specRevision := range s.apiSpecRevs {
			if specRevision.APISpecID != spec.ID || specRevision.BuildStatus != apiSpecRevisionBuildStatusProcessed {
				continue
			}
			if specRevision.IngestEventID > latestProcessedRevisionID {
				latestProcessedRevisionID = specRevision.IngestEventID
				latestSpecRevisionID = specRevision.ID
			}
		}

		dependencies := make([]string, 0)
		if latestSpecRevisionID > 0 {
			dependencies = append(dependencies, s.dependencies[latestSpecRevisionID]...)
		}

		rows = append(rows, store.ActiveAPISpecWithLatestDependencies{
			APISpec:             spec,
			DependencyFilePaths: dependencies,
		})
	}

	return rows, nil
}

func (s *integrationRevisionStore) ListAPISpecListingByRepo(
	_ context.Context,
	repoID int64,
) ([]store.APISpecListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows := make([]store.APISpecListing, 0, len(s.apiSpecs))
	for _, spec := range s.apiSpecs {
		if spec.RepoID != repoID {
			continue
		}

		item := store.APISpecListing{
			API:    spec.RootPath,
			Status: spec.Status,
		}

		var (
			latestRevisionID  int64
			latestSpecRevID   int64
			latestRevisionSHA string
			latestBranch      string
		)
		for _, specRevision := range s.apiSpecRevs {
			if specRevision.APISpecID != spec.ID || specRevision.BuildStatus != apiSpecRevisionBuildStatusProcessed {
				continue
			}
			if specRevision.IngestEventID < latestRevisionID {
				continue
			}
			revision, exists := s.revisions[specRevision.IngestEventID]
			if !exists {
				continue
			}
			latestRevisionID = specRevision.IngestEventID
			latestSpecRevID = specRevision.ID
			latestRevisionSHA = revision.Sha
			latestBranch = revision.Branch
		}

		if latestSpecRevID > 0 {
			item.LastProcessedRevision = &store.APISpecRevisionMetadata{
				APISpecRevisionID: latestSpecRevID,
				IngestEventID:     latestRevisionID,
				IngestEventSHA:    latestRevisionSHA,
				IngestEventBranch: latestBranch,
			}
		}
		rows = append(rows, item)
	}

	return rows, nil
}

func (s *integrationRevisionStore) MarkAPISpecDeleted(_ context.Context, apiSpecID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	spec, exists := s.apiSpecs[apiSpecID]
	if !exists {
		return fmt.Errorf("api spec %d not found", apiSpecID)
	}
	spec.Status = "deleted"
	s.apiSpecs[apiSpecID] = spec
	return nil
}

func (s *integrationRevisionStore) CreateAPISpecRevision(
	_ context.Context,
	input store.CreateAPISpecRevisionInput,
) (store.APISpecRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	spec, exists := s.apiSpecs[input.APISpecID]
	if !exists {
		return store.APISpecRevision{}, fmt.Errorf("api spec %d not found", input.APISpecID)
	}

	key := fmt.Sprintf("%d:%d", input.APISpecID, input.IngestEventID)
	specRevisionID, exists := s.apiSpecRevByKey[key]
	if !exists {
		s.nextSpecRevID++
		specRevisionID = s.nextSpecRevID
		s.apiSpecRevByKey[key] = specRevisionID
	}
	revision := store.APISpecRevision{
		ID:                 specRevisionID,
		APISpecID:          input.APISpecID,
		IngestEventID:      input.IngestEventID,
		RootPathAtRevision: spec.RootPath,
		BuildStatus:        input.BuildStatus,
		Error:              input.Error,
	}
	s.apiSpecRevs[revision.ID] = revision
	return revision, nil
}

func (s *integrationRevisionStore) ReplaceAPISpecDependencies(
	_ context.Context,
	input store.ReplaceAPISpecDependenciesInput,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.apiSpecRevs[input.APISpecRevisionID]; !exists {
		return fmt.Errorf("api spec revision %d not found", input.APISpecRevisionID)
	}

	rows := make([]string, len(input.FilePaths))
	copy(rows, input.FilePaths)
	s.dependencies[input.APISpecRevisionID] = rows
	return nil
}

func (s *integrationRevisionStore) PersistCanonicalSpec(_ context.Context, input store.PersistCanonicalSpecInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.artifacts[input.APISpecRevisionID] = store.SpecArtifact{
		APISpecRevisionID: input.APISpecRevisionID,
		SpecJSON:          append([]byte(nil), input.SpecJSON...),
		SpecYAML:          input.SpecYAML,
		ETag:              input.ETag,
		SizeBytes:         input.SizeBytes,
	}
	rows := make([]store.EndpointIndexRecord, len(input.Endpoints))
	copy(rows, input.Endpoints)
	s.endpoints[input.APISpecRevisionID] = rows
	return nil
}

func (s *integrationRevisionStore) UpdateAPISpecRevisionVacuumState(
	_ context.Context,
	input store.UpdateAPISpecRevisionVacuumStateInput,
) (store.APISpecRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision, exists := s.apiSpecRevs[input.APISpecRevisionID]
	if !exists {
		return store.APISpecRevision{}, fmt.Errorf("api spec revision %d not found", input.APISpecRevisionID)
	}
	revision.VacuumStatus = input.VacuumStatus
	revision.VacuumError = input.VacuumError
	revision.VacuumValidatedAt = input.VacuumValidatedAt
	s.apiSpecRevs[input.APISpecRevisionID] = revision
	return revision, nil
}

func (s *integrationRevisionStore) PersistAPISpecRevisionVacuumResult(
	_ context.Context,
	input store.PersistAPISpecRevisionVacuumResultInput,
) (store.APISpecRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision, exists := s.apiSpecRevs[input.APISpecRevisionID]
	if !exists {
		return store.APISpecRevision{}, fmt.Errorf("api spec revision %d not found", input.APISpecRevisionID)
	}
	revision.VacuumStatus = input.VacuumStatus
	revision.VacuumError = input.VacuumError
	revision.VacuumValidatedAt = input.VacuumValidatedAt
	s.apiSpecRevs[input.APISpecRevisionID] = revision
	return revision, nil
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

func (s *integrationRevisionStore) ListEndpointIndexByAPISpecRevision(
	_ context.Context,
	apiSpecRevisionID int64,
) ([]store.EndpointIndexRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, exists := s.endpoints[apiSpecRevisionID]
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

	key := fmt.Sprintf("%d:%d", input.APISpecID, input.ToAPISpecRevisionID)
	s.specChanges[key] = store.SpecChange{
		APISpecID:             input.APISpecID,
		FromAPISpecRevisionID: input.FromAPISpecRevisionID,
		ToAPISpecRevisionID:   input.ToAPISpecRevisionID,
		ChangeJSON:            append([]byte(nil), input.ChangeJSON...),
	}
	return nil
}

func (s *integrationRevisionStore) GetRevisionByID(_ context.Context, ingestEventID int64) (store.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	revision, exists := s.revisions[ingestEventID]
	if !exists {
		return store.Revision{}, fmt.Errorf("revision %d not found", ingestEventID)
	}
	return revision, nil
}

func (s *integrationRevisionStore) GetSpecArtifactByAPISpecRevisionID(
	_ context.Context,
	apiSpecRevisionID int64,
) (store.SpecArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	artifact, exists := s.artifacts[apiSpecRevisionID]
	if !exists {
		return store.SpecArtifact{}, fmt.Errorf("%w: api_spec_revision_id=%d", store.ErrSpecArtifactNotFound, apiSpecRevisionID)
	}
	return artifact, nil
}

func (s *integrationRevisionStore) GetSpecChangeByAPISpecIDAndToAPISpecRevisionID(
	_ context.Context,
	apiSpecID int64,
	toAPISpecRevisionID int64,
) (store.SpecChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	specChange, exists := s.specChanges[fmt.Sprintf("%d:%d", apiSpecID, toAPISpecRevisionID)]
	if !exists {
		return store.SpecChange{}, fmt.Errorf(
			"spec change not found for api_spec_id=%d to_api_spec_revision_id=%d",
			apiSpecID,
			toAPISpecRevisionID,
		)
	}
	return specChange, nil
}

func (s *integrationRevisionStore) ListEndpointIndexByRevision(
	_ context.Context,
	ingestEventID int64,
) ([]store.EndpointIndexRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows := make([]store.EndpointIndexRecord, 0)
	for apiSpecRevisionID, apiSpecRevision := range s.apiSpecRevs {
		if apiSpecRevision.IngestEventID != ingestEventID {
			continue
		}
		endpoints := s.endpoints[apiSpecRevisionID]
		if len(endpoints) == 0 {
			continue
		}
		copied := make([]store.EndpointIndexRecord, len(endpoints))
		copy(copied, endpoints)
		rows = append(rows, copied...)
	}
	return rows, nil
}

func (s *integrationRevisionStore) GetSpecArtifactByRevisionID(
	_ context.Context,
	ingestEventID int64,
) (store.SpecArtifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var latestAPISpecRevisionID int64
	for apiSpecRevisionID, apiSpecRevision := range s.apiSpecRevs {
		if apiSpecRevision.IngestEventID != ingestEventID {
			continue
		}
		if apiSpecRevisionID > latestAPISpecRevisionID {
			latestAPISpecRevisionID = apiSpecRevisionID
		}
	}
	if latestAPISpecRevisionID == 0 {
		return store.SpecArtifact{}, fmt.Errorf("%w: ingest_event_id=%d", store.ErrSpecArtifactNotFound, ingestEventID)
	}

	artifact, exists := s.artifacts[latestAPISpecRevisionID]
	if !exists {
		return store.SpecArtifact{}, fmt.Errorf("%w: ingest_event_id=%d", store.ErrSpecArtifactNotFound, ingestEventID)
	}
	return artifact, nil
}

func (s *integrationRevisionStore) GetSpecChangeByToRevision(
	_ context.Context,
	toRevisionID int64,
) (store.SpecChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var latestAPISpecRevisionID int64
	var latestAPISpecID int64
	for _, apiSpecRevision := range s.apiSpecRevs {
		if apiSpecRevision.IngestEventID != toRevisionID {
			continue
		}
		if apiSpecRevision.ID > latestAPISpecRevisionID {
			latestAPISpecRevisionID = apiSpecRevision.ID
			latestAPISpecID = apiSpecRevision.APISpecID
		}
	}
	if latestAPISpecRevisionID == 0 {
		return store.SpecChange{}, fmt.Errorf("spec change revision %d not found", toRevisionID)
	}

	specChange, exists := s.specChanges[fmt.Sprintf("%d:%d", latestAPISpecID, latestAPISpecRevisionID)]
	if !exists {
		return store.SpecChange{}, fmt.Errorf("spec change revision %d not found", toRevisionID)
	}
	return specChange, nil
}

func (s *integrationRevisionStore) latestRevisionBySHA(sha string) (store.Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ingestEventID, exists := s.revisionBySHA[sha]
	if !exists {
		return store.Revision{}, fmt.Errorf("revision for sha %q not found", sha)
	}
	revision, exists := s.revisions[ingestEventID]
	if !exists {
		return store.Revision{}, fmt.Errorf("revision id %d not found", ingestEventID)
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
	repoID int64,
) ([]store.Subscription, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]store.Subscription, 0, len(s.subscriptions))
	for _, subscription := range s.subscriptions {
		if subscription.Enabled && subscription.RepoID == repoID {
			result = append(result, subscription)
		}
	}
	return result, nil
}

func (s *integrationNotifierStore) GetLatestDeliveryAttemptByKey(
	_ context.Context,
	subscriptionID int64,
	apiSpecID int64,
	ingestEventID int64,
	eventType string,
) (store.DeliveryAttempt, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := fmt.Sprintf("%d:%d:%d:%s", subscriptionID, apiSpecID, ingestEventID, eventType)
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
		APISpecID:      input.APISpecID,
		IngestEventID:  input.IngestEventID,
		EventType:      input.EventType,
		AttemptNo:      input.AttemptNo,
		Status:         input.Status,
		NextRetryAt:    input.NextRetryAt,
	}
	s.attempts[attempt.ID] = attempt
	key := fmt.Sprintf("%d:%d:%d:%s", input.SubscriptionID, input.APISpecID, input.IngestEventID, input.EventType)
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
