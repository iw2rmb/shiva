package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/gitlab"
	httpserver "github.com/iw2rmb/shiva/internal/http"
	"github.com/iw2rmb/shiva/internal/notify"
	"github.com/iw2rmb/shiva/internal/openapi"
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
		if strings.TrimSpace(cfg.GitLabBaseURL) == "" {
			return errors.New("SHIVA_GITLAB_BASE_URL must be configured when database worker is enabled")
		}

		gitLabClient, err := gitlab.NewClient(cfg.GitLabBaseURL, cfg.GitLabToken)
		if err != nil {
			return fmt.Errorf("initialize gitlab client: %w", err)
		}

		openAPIResolver, err := openapi.NewResolver(openapi.ResolverConfig{
			IncludeGlobs: cfg.OpenAPIPathGlobs,
			MaxFetches:   cfg.OpenAPIRefMaxFetches,
		})
		if err != nil {
			return fmt.Errorf("initialize openapi resolver: %w", err)
		}

		workerManager = worker.New(
			cfg.WorkerConcurrency,
			logger,
			worker.WithQueue(storeQueueAdapter{store: storeInstance}),
			worker.WithProcessor(revisionProcessor{
				store:         storeInstance,
				gitlabClient:  gitLabClient,
				openapiLoader: openAPIResolver,
				notifier: notify.New(
					storeInstance,
					notify.WithHTTPClient(&http.Client{Timeout: cfg.OutboundTimeout}),
				),
				logger: logger,
			}),
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
	store         *store.Store
	gitlabClient  *gitlab.Client
	openapiLoader *openapi.Resolver
	notifier      revisionNotifier
	logger        *slog.Logger
}

type revisionNotifier interface {
	NotifyRevision(ctx context.Context, notification notify.RevisionNotification) error
}

func (p revisionProcessor) Process(ctx context.Context, job worker.QueueJob) error {
	if p.store == nil {
		return errors.New("revision processor store is not configured")
	}
	if p.gitlabClient == nil {
		return errors.New("revision processor gitlab client is not configured")
	}
	if p.openapiLoader == nil {
		return errors.New("revision processor openapi loader is not configured")
	}

	revisionID, err := p.store.UpsertRevisionFromIngestEvent(ctx, store.IngestQueueEvent{
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

	parentSHA := strings.TrimSpace(job.ParentSha)
	if parentSHA == "" {
		if err := p.store.MarkRevisionProcessed(ctx, revisionID, false); err != nil {
			return fmt.Errorf("mark revision %d processed without compare baseline: %w", revisionID, err)
		}
		if p.logger != nil {
			p.logger.Debug(
				"revision processed without openapi compare baseline",
				"revision_id", revisionID,
				"repo_id", job.RepoID,
				"sha", job.Sha,
			)
		}
		return nil
	}

	repo, err := p.store.GetRepoByID(ctx, job.RepoID)
	if err != nil {
		return fmt.Errorf("load repo %d for ingest event %d: %w", job.RepoID, job.EventID, err)
	}

	resolution, err := p.openapiLoader.ResolveChangedOpenAPI(
		ctx,
		p.gitlabClient,
		repo.GitLabProjectID,
		parentSHA,
		job.Sha,
	)
	if err != nil {
		if isPermanentOpenAPIProcessingError(err) {
			if markErr := p.store.MarkRevisionFailed(ctx, revisionID, err.Error()); markErr != nil {
				return fmt.Errorf("mark revision %d failed: %w", revisionID, markErr)
			}
			return worker.Permanent(fmt.Errorf("openapi resolution failed: %w", err))
		}
		return fmt.Errorf("resolve openapi candidates for revision %d: %w", revisionID, err)
	}

	if resolution.OpenAPIChanged {
		canonicalSpec, err := openapi.BuildCanonicalSpec(resolution)
		if err != nil {
			if isPermanentOpenAPIProcessingError(err) {
				if markErr := p.store.MarkRevisionFailed(ctx, revisionID, err.Error()); markErr != nil {
					return fmt.Errorf("mark revision %d failed: %w", revisionID, markErr)
				}
				return worker.Permanent(fmt.Errorf("canonical openapi build failed: %w", err))
			}
			return fmt.Errorf("build canonical openapi for revision %d: %w", revisionID, err)
		}

		endpoints := make([]store.EndpointIndexRecord, 0, len(canonicalSpec.Endpoints))
		for _, endpoint := range canonicalSpec.Endpoints {
			endpoints = append(endpoints, store.EndpointIndexRecord{
				Method:      endpoint.Method,
				Path:        endpoint.Path,
				OperationID: endpoint.OperationID,
				Summary:     endpoint.Summary,
				Deprecated:  endpoint.Deprecated,
				RawJSON:     endpoint.RawJSON,
			})
		}

		if err := p.store.PersistCanonicalSpec(ctx, store.PersistCanonicalSpecInput{
			RevisionID: revisionID,
			SpecJSON:   canonicalSpec.SpecJSON,
			SpecYAML:   canonicalSpec.SpecYAML,
			ETag:       canonicalSpec.ETag,
			SizeBytes:  canonicalSpec.SizeBytes,
			Endpoints:  endpoints,
		}); err != nil {
			return fmt.Errorf("persist canonical openapi for revision %d: %w", revisionID, err)
		}

		if err := p.persistSemanticDiff(ctx, job, revisionID); err != nil {
			return fmt.Errorf("persist semantic diff for revision %d: %w", revisionID, err)
		}
	}

	if err := p.store.MarkRevisionProcessed(ctx, revisionID, resolution.OpenAPIChanged); err != nil {
		return fmt.Errorf("mark revision %d processed: %w", revisionID, err)
	}
	if resolution.OpenAPIChanged {
		if err := p.emitOutboundNotifications(ctx, repo, revisionID, job); err != nil {
			return fmt.Errorf("emit outbound notifications for revision %d: %w", revisionID, err)
		}
	}

	return nil
}

func isPermanentOpenAPIProcessingError(err error) bool {
	if errors.Is(err, openapi.ErrInvalidOpenAPIDocument) {
		return true
	}
	if errors.Is(err, openapi.ErrReferenceCycle) {
		return true
	}
	if errors.Is(err, openapi.ErrFetchLimitExceeded) {
		return true
	}
	if errors.Is(err, openapi.ErrInvalidReference) {
		return true
	}
	if errors.Is(err, openapi.ErrCanonicalRootNotFound) {
		return true
	}
	if errors.Is(err, openapi.ErrCanonicalDocumentNotFound) {
		return true
	}
	if errors.Is(err, openapi.ErrReferencePointerNotFound) {
		return true
	}
	if errors.Is(err, gitlab.ErrNotFound) {
		return true
	}

	var apiErr *gitlab.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
		return true
	}

	return false
}

func (p revisionProcessor) persistSemanticDiff(ctx context.Context, job worker.QueueJob, toRevisionID int64) error {
	previousRevision, hasPreviousRevision, err := p.store.GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
		ctx,
		job.RepoID,
		job.Branch,
		toRevisionID,
	)
	if err != nil {
		return fmt.Errorf("load previous processed openapi revision: %w", err)
	}

	currentEndpoints, err := p.store.ListEndpointIndexByRevision(ctx, toRevisionID)
	if err != nil {
		return fmt.Errorf("load endpoint index for current revision %d: %w", toRevisionID, err)
	}

	previousEndpoints := make([]store.EndpointIndexRecord, 0)
	var fromRevisionID *int64
	if hasPreviousRevision {
		previousEndpoints, err = p.store.ListEndpointIndexByRevision(ctx, previousRevision.ID)
		if err != nil {
			return fmt.Errorf(
				"load endpoint index for previous revision %d: %w",
				previousRevision.ID,
				err,
			)
		}
		fromRevisionIDValue := previousRevision.ID
		fromRevisionID = &fromRevisionIDValue
	}

	changes, err := openapi.ComputeSemanticDiff(
		endpointSnapshots(previousEndpoints),
		endpointSnapshots(currentEndpoints),
	)
	if err != nil {
		return fmt.Errorf("diff endpoint snapshots: %w", err)
	}

	changeJSON, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("marshal spec change json: %w", err)
	}

	if err := p.store.PersistSpecChange(ctx, store.PersistSpecChangeInput{
		RepoID:         job.RepoID,
		FromRevisionID: fromRevisionID,
		ToRevisionID:   toRevisionID,
		ChangeJSON:     changeJSON,
	}); err != nil {
		return fmt.Errorf("persist spec change row: %w", err)
	}

	return nil
}

func (p revisionProcessor) emitOutboundNotifications(
	ctx context.Context,
	repo store.Repo,
	revisionID int64,
	job worker.QueueJob,
) error {
	if p.notifier == nil {
		return nil
	}

	tenant, err := p.store.GetTenantByID(ctx, repo.TenantID)
	if err != nil {
		return fmt.Errorf("load tenant %d: %w", repo.TenantID, err)
	}

	revision, err := p.store.GetRevisionByID(ctx, revisionID)
	if err != nil {
		return fmt.Errorf("load revision %d: %w", revisionID, err)
	}

	artifact, err := p.store.GetSpecArtifactByRevisionID(ctx, revisionID)
	if err != nil {
		return fmt.Errorf("load spec artifact for revision %d: %w", revisionID, err)
	}

	specChange, err := p.store.GetSpecChangeByToRevision(ctx, revisionID)
	if err != nil {
		return fmt.Errorf("load spec change for revision %d: %w", revisionID, err)
	}

	var fromSHA string
	if specChange.FromRevisionID != nil {
		fromRevision, err := p.store.GetRevisionByID(ctx, *specChange.FromRevisionID)
		if err != nil {
			return fmt.Errorf("load from revision %d for spec change: %w", *specChange.FromRevisionID, err)
		}
		fromSHA = fromRevision.Sha
	}

	processedAt := time.Now().UTC()
	if revision.ProcessedAt != nil {
		processedAt = revision.ProcessedAt.UTC()
	}

	if err := p.notifier.NotifyRevision(ctx, notify.RevisionNotification{
		TenantID:    tenant.ID,
		TenantKey:   tenant.Key,
		RepoID:      repo.ID,
		RepoPath:    repo.PathWithNamespace,
		RevisionID:  revisionID,
		Sha:         revision.Sha,
		Branch:      revision.Branch,
		ProcessedAt: processedAt,
		Artifact:    artifact,
		SpecChange:  specChange,
		FromSHA:     fromSHA,
	}); err != nil {
		return err
	}

	return nil
}

func endpointSnapshots(endpoints []store.EndpointIndexRecord) []openapi.EndpointSnapshot {
	snapshots := make([]openapi.EndpointSnapshot, 0, len(endpoints))
	for _, endpoint := range endpoints {
		snapshots = append(snapshots, openapi.EndpointSnapshot{
			Method:  endpoint.Method,
			Path:    endpoint.Path,
			RawJSON: endpoint.RawJSON,
		})
	}
	return snapshots
}
