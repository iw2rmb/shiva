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
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/gitlab"
	httpserver "github.com/iw2rmb/shiva/internal/http"
	"github.com/iw2rmb/shiva/internal/notify"
	"github.com/iw2rmb/shiva/internal/observability"
	"github.com/iw2rmb/shiva/internal/openapi"
	"github.com/iw2rmb/shiva/internal/store"
	"github.com/iw2rmb/shiva/internal/worker"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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

	telemetry, err := observability.New(cfg, logger)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	storeInstance, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer storeInstance.Close()
	defer telemetry.Shutdown(context.Background())

	if strings.TrimSpace(cfg.GitLabBaseURL) == "" {
		return errors.New("SHIVA_GITLAB_BASE_URL must be configured")
	}

	gitLabClient, err := gitlab.NewClient(cfg.GitLabBaseURL, cfg.GitLabToken)
	if err != nil {
		return fmt.Errorf("initialize gitlab client: %w", err)
	}

	if err := enqueueStartupIndexingIfEmpty(ctx, cfg, logger, storeInstance, gitLabClient); err != nil {
		return err
	}

	openAPIResolver, err := openapi.NewResolver(openapi.ResolverConfig{
		IncludeGlobs:              cfg.OpenAPIPathGlobs,
		MaxFetches:                cfg.OpenAPIRefMaxFetches,
		BootstrapFetchConcurrency: cfg.OpenAPIBootstrapFetchConcurrency,
		BootstrapSniffBytes:       cfg.OpenAPIBootstrapSniffBytes,
	})
	if err != nil {
		return fmt.Errorf("initialize openapi resolver: %w", err)
	}

	workerManager := worker.New(
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
				notify.WithLogger(logger),
				notify.WithMetrics(telemetry.Metrics()),
				notify.WithTracer(telemetry.Tracer()),
			),
			logger:  logger,
			metrics: telemetry.Metrics(),
			tracer:  telemetry.Tracer(),
		}),
	)
	if err := workerManager.Start(ctx); err != nil {
		return err
	}

	server := httpserver.New(cfg, logger, storeInstance, httpserver.WithTelemetry(telemetry))
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

	if err := workerManager.Stop(shutdownCtx); err != nil {
		logger.Warn("worker shutdown returned error", "error", err)
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
		DeliveryID:   event.DeliveryID,
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
	store         revisionStore
	gitlabClient  gitlabResolverClient
	openapiLoader openapiResolver
	notifier      revisionNotifier
	logger        *slog.Logger
	metrics       *observability.Metrics
	tracer        trace.Tracer
}

type revisionNotifier interface {
	NotifyRevision(ctx context.Context, notification notify.RevisionNotification) error
}

type gitlabResolverClient interface {
	CompareChangedPaths(
		ctx context.Context,
		projectID int64,
		fromSHA string,
		toSHA string,
	) ([]gitlab.ChangedPath, error)
	GetFileContent(ctx context.Context, projectID int64, filePath, ref string) ([]byte, error)
	ListRepositoryTree(
		ctx context.Context,
		projectID int64,
		sha string,
		path string,
		recursive bool,
	) ([]gitlab.TreeEntry, error)
}

type openapiResolver interface {
	ResolveRootOpenAPIAtSHA(
		ctx context.Context,
		client openapi.GitLabClient,
		projectID int64,
		sha string,
		rootPath string,
	) (openapi.RootResolution, error)
	ResolveDiscoveredRootsAtPaths(
		ctx context.Context,
		client openapi.GitLabClient,
		projectID int64,
		sha string,
		paths []string,
	) ([]openapi.RootResolution, error)
	ResolveRepositoryOpenAPIAtSHA(
		ctx context.Context,
		client openapi.GitLabBootstrapClient,
		projectID int64,
		sha string,
	) ([]openapi.RootResolution, error)
}

type revisionStore interface {
	UpsertRevisionFromIngestEvent(ctx context.Context, event store.IngestQueueEvent) (int64, error)
	MarkRevisionProcessed(ctx context.Context, revisionID int64, openapiChanged bool) error
	MarkRevisionFailed(ctx context.Context, revisionID int64, errorMessage string) error
	GetRepoByID(ctx context.Context, repoID int64) (store.Repo, error)
	GetRepoBootstrapState(ctx context.Context, repoID int64) (store.RepoBootstrapState, error)
	ClearRepoForceRescan(ctx context.Context, repoID int64) error
	UpsertAPISpec(ctx context.Context, input store.UpsertAPISpecInput) (store.APISpec, error)
	ListAPISpecListingByRepo(ctx context.Context, repoID int64) ([]store.APISpecListing, error)
	ListActiveAPISpecsWithLatestDependencies(
		ctx context.Context,
		repoID int64,
	) ([]store.ActiveAPISpecWithLatestDependencies, error)
	MarkAPISpecDeleted(ctx context.Context, apiSpecID int64) error
	CreateAPISpecRevision(ctx context.Context, input store.CreateAPISpecRevisionInput) (store.APISpecRevision, error)
	ReplaceAPISpecDependencies(ctx context.Context, input store.ReplaceAPISpecDependenciesInput) error
	PersistCanonicalSpec(ctx context.Context, input store.PersistCanonicalSpecInput) error
	ListEndpointIndexByAPISpecRevision(ctx context.Context, apiSpecRevisionID int64) ([]store.EndpointIndexRecord, error)
	PersistSpecChange(ctx context.Context, input store.PersistSpecChangeInput) error
	GetTenantByID(ctx context.Context, tenantID int64) (store.Tenant, error)
	GetRevisionByID(ctx context.Context, revisionID int64) (store.Revision, error)
	GetSpecArtifactByAPISpecRevisionID(ctx context.Context, apiSpecRevisionID int64) (store.SpecArtifact, error)
	GetSpecChangeByAPISpecIDAndToAPISpecRevisionID(
		ctx context.Context,
		apiSpecID int64,
		toAPISpecRevisionID int64,
	) (store.SpecChange, error)
}

type ingestionMode string

const (
	ingestionModeIncremental ingestionMode = "incremental"
	ingestionModeBootstrap   ingestionMode = "bootstrap"

	apiSpecRevisionBuildStatusProcessing = "processing"
	apiSpecRevisionBuildStatusProcessed  = "processed"
	apiSpecRevisionBuildStatusFailed     = "failed"
)

func decideIngestionMode(state store.RepoBootstrapState) ingestionMode {
	if state.ActiveAPICount == 0 || state.ForceRescan {
		return ingestionModeBootstrap
	}
	return ingestionModeIncremental
}

func (p revisionProcessor) handleRevisionError(
	ctx context.Context,
	span trace.Span,
	revisionID int64,
	err error,
	label string,
) error {
	if isPermanentOpenAPIProcessingError(err) {
		if markErr := p.store.MarkRevisionFailed(ctx, revisionID, err.Error()); markErr != nil {
			span.RecordError(markErr)
			span.SetStatus(codes.Error, "mark revision failed")
			return fmt.Errorf("mark revision %d failed: %w", revisionID, markErr)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, label+" permanent failure")
		return worker.Permanent(fmt.Errorf("%s failed: %w", label, err))
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, label+" failed")
	return fmt.Errorf("%s for revision %d: %w", label, revisionID, err)
}

func (p revisionProcessor) effectiveTracer() trace.Tracer {
	if p.tracer != nil {
		return p.tracer
	}
	return trace.NewNoopTracerProvider().Tracer("github.com/iw2rmb/shiva")
}

func (p revisionProcessor) Process(ctx context.Context, job worker.QueueJob) error {
	tracer := p.effectiveTracer()
	ctx, processSpan := tracer.Start(ctx, "process.revision", trace.WithAttributes(
		attribute.Int64("event.id", job.EventID),
		attribute.Int64("repo.id", job.RepoID),
		attribute.String("delivery.id", job.DeliveryID),
		attribute.String("revision.sha", job.Sha),
	))
	defer processSpan.End()

	if p.store == nil {
		processSpan.SetStatus(codes.Error, "store not configured")
		return errors.New("revision processor store is not configured")
	}
	if p.gitlabClient == nil {
		processSpan.SetStatus(codes.Error, "gitlab client not configured")
		return errors.New("revision processor gitlab client is not configured")
	}
	if p.openapiLoader == nil {
		processSpan.SetStatus(codes.Error, "openapi loader not configured")
		return errors.New("revision processor openapi loader is not configured")
	}

	logger := p.logger
	if logger != nil {
		logger = logger.With(
			"event_id", job.EventID,
			"delivery_id", job.DeliveryID,
			"repo_id", job.RepoID,
			"sha", job.Sha,
		)
		logger.Debug("revision processing started")
	}

	revisionID, err := p.store.UpsertRevisionFromIngestEvent(ctx, store.IngestQueueEvent{
		ID:           job.EventID,
		RepoID:       job.RepoID,
		DeliveryID:   job.DeliveryID,
		Sha:          job.Sha,
		Branch:       job.Branch,
		ParentSha:    job.ParentSha,
		AttemptCount: job.AttemptCount,
	})
	if err != nil {
		processSpan.RecordError(err)
		processSpan.SetStatus(codes.Error, "upsert revision failed")
		return fmt.Errorf("upsert revision from ingest event %d: %w", job.EventID, err)
	}

	if logger != nil {
		logger = logger.With("revision_id", revisionID)
	}
	processSpan.SetAttributes(attribute.Int64("revision.id", revisionID))

	parentSHA := strings.TrimSpace(job.ParentSha)

	bootstrapState, err := p.store.GetRepoBootstrapState(ctx, job.RepoID)
	if err != nil {
		processSpan.RecordError(err)
		processSpan.SetStatus(codes.Error, "load repo bootstrap state failed")
		return fmt.Errorf("load repo bootstrap state for repo %d: %w", job.RepoID, err)
	}
	mode := decideIngestionMode(bootstrapState)
	processSpan.SetAttributes(
		attribute.String("ingestion.mode", string(mode)),
		attribute.Int64("repo.active_api_count", bootstrapState.ActiveAPICount),
		attribute.Bool("repo.force_rescan", bootstrapState.ForceRescan),
	)

	repo, err := p.store.GetRepoByID(ctx, job.RepoID)
	if err != nil {
		processSpan.RecordError(err)
		processSpan.SetStatus(codes.Error, "load repo failed")
		return fmt.Errorf("load repo %d for ingest event %d: %w", job.RepoID, job.EventID, err)
	}
	previousAPISpecRevisionByRoot, err := p.loadPreviousAPISpecRevisionsByRoot(ctx, job.RepoID)
	if err != nil {
		processSpan.RecordError(err)
		processSpan.SetStatus(codes.Error, "load previous api spec revisions failed")
		return fmt.Errorf("load previous api spec revisions for repo %d: %w", job.RepoID, err)
	}

	var bootstrapRoots []openapi.RootResolution
	switch mode {
	case ingestionModeBootstrap:
		bootstrapCtx, bootstrapSpan := tracer.Start(ctx, "gitlab.bootstrap", trace.WithAttributes(
			attribute.Int64("repo.id", job.RepoID),
			attribute.String("delivery.id", job.DeliveryID),
			attribute.String("revision.sha", job.Sha),
			attribute.Int64("gitlab.project_id", repo.GitLabProjectID),
		))
		roots, resolveErr := p.openapiLoader.ResolveRepositoryOpenAPIAtSHA(
			bootstrapCtx,
			p.gitlabClient,
			repo.GitLabProjectID,
			job.Sha,
		)
		if resolveErr != nil {
			bootstrapSpan.RecordError(resolveErr)
			bootstrapSpan.SetStatus(codes.Error, "openapi bootstrap failed")
		} else {
			bootstrapRoots = roots
			bootstrapSpan.SetAttributes(
				attribute.Int("openapi.bootstrap.root_count", len(roots)),
			)
			bootstrapSpan.SetStatus(codes.Ok, "")
		}
		bootstrapSpan.End()
		err = resolveErr
	}
	if err != nil {
		return p.handleRevisionError(ctx, processSpan, revisionID, err, "openapi resolution")
	}

	changedAPIs := make([]changedAPISpecRevision, 0)
	openAPIChanged := false
	switch mode {
	case ingestionModeBootstrap:
		buildStartedAt := time.Now()
		buildSuccess := false
		recordBuildMetric := func() {
			if p.metrics != nil {
				p.metrics.ObserveBuild(time.Since(buildStartedAt), buildSuccess)
			}
		}

		changedAPIs, err = p.processResolvedRoots(
			ctx,
			job,
			revisionID,
			bootstrapRoots,
			previousAPISpecRevisionByRoot,
		)
		if err != nil {
			recordBuildMetric()
			processSpan.RecordError(err)
			processSpan.SetStatus(codes.Error, "bootstrap build stage failed")
			return fmt.Errorf("build bootstrap openapi roots for revision %d: %w", revisionID, err)
		}

		if err := p.persistSemanticDiffsForAPIs(ctx, job, changedAPIs); err != nil {
			recordBuildMetric()
			processSpan.RecordError(err)
			processSpan.SetStatus(codes.Error, "semantic diff failed")
			return fmt.Errorf("persist semantic diff for revision %d: %w", revisionID, err)
		}
		openAPIChanged = len(changedAPIs) > 0
		buildSuccess = openAPIChanged
		recordBuildMetric()
	default:
		incrementalStartedAt := time.Now()
		changedAPIs, err = p.processIncrementalRevision(
			ctx,
			job,
			revisionID,
			repo.GitLabProjectID,
			parentSHA,
			previousAPISpecRevisionByRoot,
		)
		if err != nil {
			return p.handleRevisionError(ctx, processSpan, revisionID, err, "incremental openapi processing")
		}
		if err := p.persistSemanticDiffsForAPIs(ctx, job, changedAPIs); err != nil {
			return p.handleRevisionError(ctx, processSpan, revisionID, err, "incremental semantic diff")
		}
		openAPIChanged = len(changedAPIs) > 0
		if openAPIChanged && p.metrics != nil {
			p.metrics.ObserveBuild(time.Since(incrementalStartedAt), true)
		}
	}

	if err := p.store.MarkRevisionProcessed(ctx, revisionID, openAPIChanged); err != nil {
		processSpan.RecordError(err)
		processSpan.SetStatus(codes.Error, "mark revision processed failed")
		return fmt.Errorf("mark revision %d processed: %w", revisionID, err)
	}
	if openAPIChanged {
		if err := p.emitOutboundNotifications(ctx, repo, revisionID, job, changedAPIs); err != nil {
			processSpan.RecordError(err)
			processSpan.SetStatus(codes.Error, "notify dispatch failed")
			return fmt.Errorf("emit outbound notifications for revision %d: %w", revisionID, err)
		}
	}
	if mode == ingestionModeBootstrap {
		if err := p.store.ClearRepoForceRescan(ctx, job.RepoID); err != nil {
			processSpan.RecordError(err)
			processSpan.SetStatus(codes.Error, "clear repo force rescan failed")
			return fmt.Errorf("clear repo %d force-rescan: %w", job.RepoID, err)
		}
	}
	if logger != nil {
		logger.Info("revision processed", "openapi_changed", openAPIChanged)
	}

	processSpan.SetStatus(codes.Ok, "")
	return nil
}

type changedAPISpecRevision struct {
	apiSpec               store.APISpec
	fromAPISpecRevisionID *int64
	toAPISpecRevisionID   int64
}

func (p revisionProcessor) processResolvedRoots(
	ctx context.Context,
	job worker.QueueJob,
	revisionID int64,
	roots []openapi.RootResolution,
	previousAPISpecRevisionByRoot map[string]int64,
) ([]changedAPISpecRevision, error) {
	changed := make([]changedAPISpecRevision, 0, len(roots))
	for _, root := range roots {
		apiSpec, err := p.store.UpsertAPISpec(ctx, store.UpsertAPISpecInput{
			RepoID:   job.RepoID,
			RootPath: root.RootPath,
		})
		if err != nil {
			return nil, fmt.Errorf("upsert api spec for root %q: %w", root.RootPath, err)
		}

		apiSpecRevision, ok, err := p.processIncrementalAPI(ctx, job, revisionID, apiSpec, func(context.Context) (openapi.RootResolution, error) {
			return root, nil
		})
		if err != nil {
			return nil, err
		}
		if ok {
			changed = append(changed, changedAPISpecRevision{
				apiSpec:               apiSpec,
				fromAPISpecRevisionID: previousAPISpecRevisionForRoot(previousAPISpecRevisionByRoot, apiSpec.RootPath),
				toAPISpecRevisionID:   apiSpecRevision.ID,
			})
		}
	}
	return changed, nil
}

type impactedAPISpec struct {
	spec        store.ActiveAPISpecWithLatestDependencies
	rootDeleted bool
}

func (p revisionProcessor) processIncrementalRevision(
	ctx context.Context,
	job worker.QueueJob,
	revisionID int64,
	projectID int64,
	parentSHA string,
	previousAPISpecRevisionByRoot map[string]int64,
) ([]changedAPISpecRevision, error) {
	compareCtx, compareSpan := p.effectiveTracer().Start(ctx, "gitlab.compare", trace.WithAttributes(
		attribute.Int64("repo.id", job.RepoID),
		attribute.String("delivery.id", job.DeliveryID),
		attribute.String("revision.sha", job.Sha),
		attribute.String("revision.parent_sha", parentSHA),
		attribute.Int64("gitlab.project_id", projectID),
	))
	changedPaths, err := p.gitlabClient.CompareChangedPaths(compareCtx, projectID, parentSHA, job.Sha)
	if err != nil {
		compareSpan.RecordError(err)
		compareSpan.SetStatus(codes.Error, "load changed paths failed")
		compareSpan.End()
		return nil, fmt.Errorf("load changed paths: %w", err)
	}

	impacted, err := p.resolveImpactedAPIs(compareCtx, job.RepoID, changedPaths)
	if err != nil {
		compareSpan.RecordError(err)
		compareSpan.SetStatus(codes.Error, "resolve impacted apis failed")
		compareSpan.End()
		return nil, fmt.Errorf("resolve impacted apis: %w", err)
	}

	compareSpan.SetAttributes(
		attribute.Int("gitlab.compare.changed_path_count", len(changedPaths)),
		attribute.Int("openapi.incremental.impacted_api_count", len(impacted)),
	)
	compareSpan.SetStatus(codes.Ok, "")
	compareSpan.End()

	changed := make([]changedAPISpecRevision, 0, len(impacted))
	for _, item := range impacted {
		if item.rootDeleted {
			changedAPI, err := p.processDeletedImpactedAPI(
				ctx,
				revisionID,
				item,
				previousAPISpecRevisionByRoot,
			)
			if err != nil {
				return nil, err
			}
			changed = append(changed, changedAPI)
			continue
		}

		apiSpecRevision, built, err := p.processImpactedAPI(ctx, job, revisionID, projectID, item.spec)
		if err != nil {
			return nil, err
		}
		if built {
			changed = append(changed, changedAPISpecRevision{
				apiSpec:               item.spec.APISpec,
				fromAPISpecRevisionID: previousAPISpecRevisionForRoot(previousAPISpecRevisionByRoot, item.spec.RootPath),
				toAPISpecRevisionID:   apiSpecRevision.ID,
			})
		}
	}

	if len(impacted) == 0 {
		discovered, err := p.processFallbackDiscovery(
			ctx,
			job,
			revisionID,
			projectID,
			changedPaths,
			previousAPISpecRevisionByRoot,
		)
		if err != nil {
			return nil, err
		}
		changed = append(changed, discovered...)
	}

	return changed, nil
}

func (p revisionProcessor) processDeletedImpactedAPI(
	ctx context.Context,
	revisionID int64,
	item impactedAPISpec,
	previousAPISpecRevisionByRoot map[string]int64,
) (changedAPISpecRevision, error) {
	if err := p.store.MarkAPISpecDeleted(ctx, item.spec.ID); err != nil {
		return changedAPISpecRevision{}, fmt.Errorf(
			"mark api spec %d deleted for root %q: %w",
			item.spec.ID,
			item.spec.RootPath,
			err,
		)
	}

	apiSpecRevision, err := p.store.CreateAPISpecRevision(ctx, store.CreateAPISpecRevisionInput{
		APISpecID:   item.spec.ID,
		RevisionID:  revisionID,
		BuildStatus: apiSpecRevisionBuildStatusProcessed,
	})
	if err != nil {
		return changedAPISpecRevision{}, fmt.Errorf(
			"create deleted api spec revision for root %q: %w",
			item.spec.RootPath,
			err,
		)
	}

	return changedAPISpecRevision{
		apiSpec:               item.spec.APISpec,
		fromAPISpecRevisionID: previousAPISpecRevisionForRoot(previousAPISpecRevisionByRoot, item.spec.RootPath),
		toAPISpecRevisionID:   apiSpecRevision.ID,
	}, nil
}

func (p revisionProcessor) loadPreviousAPISpecRevisionsByRoot(
	ctx context.Context,
	repoID int64,
) (map[string]int64, error) {
	listing, err := p.store.ListAPISpecListingByRepo(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("list api specs for repo %d: %w", repoID, err)
	}

	previousByRoot := make(map[string]int64, len(listing))
	for _, item := range listing {
		if item.LastProcessedRevision == nil {
			continue
		}
		rootPath := strings.TrimSpace(item.API)
		if rootPath == "" {
			continue
		}
		previousByRoot[rootPath] = item.LastProcessedRevision.APISpecRevisionID
	}

	return previousByRoot, nil
}

func previousAPISpecRevisionForRoot(previousByRoot map[string]int64, rootPath string) *int64 {
	if len(previousByRoot) == 0 {
		return nil
	}

	revisionID, exists := previousByRoot[strings.TrimSpace(rootPath)]
	if !exists {
		return nil
	}

	value := revisionID
	return &value
}

func (p revisionProcessor) persistSemanticDiffsForAPIs(
	ctx context.Context,
	job worker.QueueJob,
	changed []changedAPISpecRevision,
) error {
	for _, item := range changed {
		if err := p.persistSemanticDiffForAPI(
			ctx,
			job,
			item.apiSpec.ID,
			item.fromAPISpecRevisionID,
			item.toAPISpecRevisionID,
		); err != nil {
			return err
		}
	}

	return nil
}

func (p revisionProcessor) resolveImpactedAPIs(
	ctx context.Context,
	repoID int64,
	changedPaths []gitlab.ChangedPath,
) ([]impactedAPISpec, error) {
	activeSpecs, err := p.store.ListActiveAPISpecsWithLatestDependencies(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("list active api specs with dependencies for repo %d: %w", repoID, err)
	}

	changedPathSet := make(map[string]struct{}, len(changedPaths)*2)
	deletedRoots := make(map[string]struct{}, len(changedPaths))
	for _, changedPath := range changedPaths {
		for _, impactPath := range incrementalImpactPaths(changedPath) {
			changedPathSet[impactPath] = struct{}{}
		}
		deletedPath := incrementalDeletedPath(changedPath)
		if deletedPath != "" {
			deletedRoots[deletedPath] = struct{}{}
		}
	}

	impacted := make([]impactedAPISpec, 0, len(activeSpecs))
	for _, spec := range activeSpecs {
		rootPath := normalizeIncrementalRepoPath(spec.RootPath)
		if rootPath == "" {
			continue
		}

		isImpacted := false
		if _, exists := changedPathSet[rootPath]; exists {
			isImpacted = true
		}
		if !isImpacted {
			for _, dependencyPath := range spec.DependencyFilePaths {
				normalizedDependencyPath := normalizeIncrementalRepoPath(dependencyPath)
				if normalizedDependencyPath == "" {
					continue
				}
				if _, exists := changedPathSet[normalizedDependencyPath]; exists {
					isImpacted = true
					break
				}
			}
		}

		if !isImpacted {
			continue
		}

		_, rootDeleted := deletedRoots[rootPath]
		impacted = append(impacted, impactedAPISpec{
			spec:        spec,
			rootDeleted: rootDeleted,
		})
	}

	return impacted, nil
}

func (p revisionProcessor) processImpactedAPI(
	ctx context.Context,
	job worker.QueueJob,
	revisionID int64,
	projectID int64,
	spec store.ActiveAPISpecWithLatestDependencies,
) (store.APISpecRevision, bool, error) {
	return p.processIncrementalAPI(
		ctx,
		job,
		revisionID,
		spec.APISpec,
		func(callCtx context.Context) (openapi.RootResolution, error) {
			root, err := p.openapiLoader.ResolveRootOpenAPIAtSHA(
				callCtx,
				p.gitlabClient,
				projectID,
				job.Sha,
				spec.RootPath,
			)
			if err != nil {
				return openapi.RootResolution{}, fmt.Errorf("resolve impacted root %q: %w", spec.RootPath, err)
			}
			return root, nil
		},
	)
}

func (p revisionProcessor) processFallbackDiscovery(
	ctx context.Context,
	job worker.QueueJob,
	revisionID int64,
	projectID int64,
	changedPaths []gitlab.ChangedPath,
	previousAPISpecRevisionByRoot map[string]int64,
) ([]changedAPISpecRevision, error) {
	candidatePaths := fallbackDiscoveryCandidatePaths(changedPaths)
	if len(candidatePaths) == 0 {
		return nil, nil
	}

	roots, err := p.openapiLoader.ResolveDiscoveredRootsAtPaths(ctx, p.gitlabClient, projectID, job.Sha, candidatePaths)
	if err != nil {
		return nil, fmt.Errorf("resolve fallback discovered roots: %w", err)
	}

	return p.processResolvedRoots(ctx, job, revisionID, roots, previousAPISpecRevisionByRoot)
}

func (p revisionProcessor) processIncrementalAPI(
	ctx context.Context,
	job worker.QueueJob,
	revisionID int64,
	apiSpec store.APISpec,
	resolveRoot func(context.Context) (openapi.RootResolution, error),
) (store.APISpecRevision, bool, error) {
	apiSpecRevision, err := p.store.CreateAPISpecRevision(ctx, store.CreateAPISpecRevisionInput{
		APISpecID:   apiSpec.ID,
		RevisionID:  revisionID,
		BuildStatus: apiSpecRevisionBuildStatusProcessing,
	})
	if err != nil {
		return store.APISpecRevision{}, false, fmt.Errorf("create api spec revision for root %q: %w", apiSpec.RootPath, err)
	}

	root, err := resolveRoot(ctx)
	if err != nil {
		return p.handleIncrementalAPIFailure(ctx, apiSpec, revisionID, apiSpecRevision, err)
	}

	if err := p.store.ReplaceAPISpecDependencies(ctx, store.ReplaceAPISpecDependenciesInput{
		APISpecRevisionID: apiSpecRevision.ID,
		FilePaths:         root.DependencyFiles,
	}); err != nil {
		return store.APISpecRevision{}, false, fmt.Errorf("replace api spec dependencies for root %q: %w", apiSpec.RootPath, err)
	}

	if err := p.runBuildStage(ctx, job, revisionID, apiSpecRevision.ID, root); err != nil {
		return p.handleIncrementalAPIFailure(
			ctx,
			apiSpec,
			revisionID,
			apiSpecRevision,
			fmt.Errorf("build canonical openapi for root %q: %w", apiSpec.RootPath, err),
		)
	}

	processedRevision, err := p.store.CreateAPISpecRevision(ctx, store.CreateAPISpecRevisionInput{
		APISpecID:   apiSpec.ID,
		RevisionID:  revisionID,
		BuildStatus: apiSpecRevisionBuildStatusProcessed,
	})
	if err != nil {
		return store.APISpecRevision{}, false, fmt.Errorf("mark api spec revision processed for root %q: %w", apiSpec.RootPath, err)
	}

	return processedRevision, true, nil
}

func (p revisionProcessor) handleIncrementalAPIFailure(
	ctx context.Context,
	apiSpec store.APISpec,
	revisionID int64,
	apiSpecRevision store.APISpecRevision,
	err error,
) (store.APISpecRevision, bool, error) {
	if !isPermanentOpenAPIProcessingError(err) {
		return store.APISpecRevision{}, false, err
	}

	failedRevision, markErr := p.store.CreateAPISpecRevision(ctx, store.CreateAPISpecRevisionInput{
		APISpecID:   apiSpec.ID,
		RevisionID:  revisionID,
		BuildStatus: apiSpecRevisionBuildStatusFailed,
		Error:       err.Error(),
	})
	if markErr != nil {
		return store.APISpecRevision{}, false, fmt.Errorf("mark api spec revision failed for root %q: %w", apiSpec.RootPath, markErr)
	}
	if failedRevision.ID == 0 {
		failedRevision = apiSpecRevision
	}

	return failedRevision, false, nil
}

func incrementalImpactPaths(changedPath gitlab.ChangedPath) []string {
	paths := make([]string, 0, 2)
	addPath := func(raw string) {
		normalized := normalizeIncrementalRepoPath(raw)
		if normalized == "" {
			return
		}
		for _, existing := range paths {
			if existing == normalized {
				return
			}
		}
		paths = append(paths, normalized)
	}

	// For rename/update we track both old and new paths so dependency/root
	// intersections are evaluated against either side of the change.
	addPath(changedPath.NewPath)
	addPath(changedPath.OldPath)
	return paths
}

func incrementalDeletedPath(changedPath gitlab.ChangedPath) string {
	if !changedPath.DeletedFile {
		return ""
	}

	pathCandidate := strings.TrimSpace(changedPath.OldPath)
	if pathCandidate == "" {
		pathCandidate = strings.TrimSpace(changedPath.NewPath)
	}
	return normalizeIncrementalRepoPath(pathCandidate)
}

func fallbackDiscoveryCandidatePaths(changedPaths []gitlab.ChangedPath) []string {
	candidates := make([]string, 0, len(changedPaths))
	seen := make(map[string]struct{}, len(changedPaths))

	for _, changedPath := range changedPaths {
		if !changedPath.NewFile && !changedPath.RenamedFile {
			continue
		}

		pathCandidate := normalizeIncrementalRepoPath(changedPath.NewPath)
		if pathCandidate == "" {
			pathCandidate = normalizeIncrementalRepoPath(changedPath.OldPath)
		}
		if pathCandidate == "" {
			continue
		}

		if _, exists := seen[pathCandidate]; exists {
			continue
		}
		seen[pathCandidate] = struct{}{}
		candidates = append(candidates, pathCandidate)
	}

	return candidates
}

func normalizeIncrementalRepoPath(raw string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "/"))
	if trimmed == "" {
		return ""
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func (p revisionProcessor) runBuildStage(
	ctx context.Context,
	job worker.QueueJob,
	revisionID int64,
	apiSpecRevisionID int64,
	root openapi.RootResolution,
) error {
	buildCtx, buildSpan := p.effectiveTracer().Start(ctx, "spec.build", trace.WithAttributes(
		attribute.Int64("repo.id", job.RepoID),
		attribute.Int64("revision.id", revisionID),
		attribute.Int64("api_spec_revision.id", apiSpecRevisionID),
		attribute.String("delivery.id", job.DeliveryID),
		attribute.String("revision.sha", job.Sha),
	))
	success := false
	defer func() {
		if success {
			buildSpan.SetStatus(codes.Ok, "")
		}
		buildSpan.End()
	}()

	canonicalSpec, err := openapi.BuildCanonicalSpec(openapi.ResolutionResult{
		OpenAPIChanged: true,
		CandidateFiles: []string{root.RootPath},
		Documents:      root.Documents,
	})
	if err != nil {
		buildSpan.RecordError(err)
		buildSpan.SetStatus(codes.Error, "canonical build failed")
		return err
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

	if err := p.store.PersistCanonicalSpec(buildCtx, store.PersistCanonicalSpecInput{
		APISpecRevisionID: apiSpecRevisionID,
		SpecJSON:          canonicalSpec.SpecJSON,
		SpecYAML:          canonicalSpec.SpecYAML,
		ETag:              canonicalSpec.ETag,
		SizeBytes:         canonicalSpec.SizeBytes,
		Endpoints:         endpoints,
	}); err != nil {
		buildSpan.RecordError(err)
		buildSpan.SetStatus(codes.Error, "persist canonical spec failed")
		return fmt.Errorf("persist canonical openapi for api spec revision %d: %w", apiSpecRevisionID, err)
	}

	success = true

	return nil
}

func isPermanentOpenAPIProcessingError(err error) bool {
	for _, target := range []error{
		openapi.ErrInvalidOpenAPIDocument,
		openapi.ErrReferenceCycle,
		openapi.ErrFetchLimitExceeded,
		openapi.ErrInvalidReference,
		openapi.ErrCanonicalRootNotFound,
		openapi.ErrCanonicalDocumentNotFound,
		openapi.ErrReferencePointerNotFound,
		gitlab.ErrNotFound,
	} {
		if errors.Is(err, target) {
			return true
		}
	}

	var apiErr *gitlab.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
		return true
	}

	return false
}

func (p revisionProcessor) persistSemanticDiffForAPI(
	ctx context.Context,
	job worker.QueueJob,
	apiSpecID int64,
	fromAPISpecRevisionID *int64,
	toAPISpecRevisionID int64,
) error {
	ctx, span := p.effectiveTracer().Start(ctx, "diff.compute", trace.WithAttributes(
		attribute.Int64("repo.id", job.RepoID),
		attribute.Int64("api_spec.id", apiSpecID),
		attribute.Int64("to_api_spec_revision.id", toAPISpecRevisionID),
		attribute.String("delivery.id", job.DeliveryID),
		attribute.String("revision.sha", job.Sha),
	))
	defer span.End()

	currentEndpoints, err := p.store.ListEndpointIndexByAPISpecRevision(ctx, toAPISpecRevisionID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "load current endpoints failed")
		return fmt.Errorf("load endpoint index for api spec revision %d: %w", toAPISpecRevisionID, err)
	}

	previousEndpoints := make([]store.EndpointIndexRecord, 0)
	if fromAPISpecRevisionID != nil {
		previousEndpoints, err = p.store.ListEndpointIndexByAPISpecRevision(ctx, *fromAPISpecRevisionID)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "load previous endpoints failed")
			return fmt.Errorf(
				"load endpoint index for previous api spec revision %d: %w",
				*fromAPISpecRevisionID,
				err,
			)
		}
	}

	changes, err := openapi.ComputeSemanticDiff(
		endpointSnapshots(previousEndpoints),
		endpointSnapshots(currentEndpoints),
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "compute semantic diff failed")
		return fmt.Errorf("diff endpoint snapshots: %w", err)
	}

	changeJSON, err := json.Marshal(changes)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "marshal semantic diff failed")
		return fmt.Errorf("marshal spec change json: %w", err)
	}

	if err := p.store.PersistSpecChange(ctx, store.PersistSpecChangeInput{
		APISpecID:             apiSpecID,
		FromAPISpecRevisionID: fromAPISpecRevisionID,
		ToAPISpecRevisionID:   toAPISpecRevisionID,
		ChangeJSON:            changeJSON,
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "persist semantic diff failed")
		return fmt.Errorf("persist spec change row: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (p revisionProcessor) emitOutboundNotifications(
	ctx context.Context,
	repo store.Repo,
	revisionID int64,
	job worker.QueueJob,
	changedAPIs []changedAPISpecRevision,
) error {
	if p.notifier == nil || len(changedAPIs) == 0 {
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

	processedAt := time.Now().UTC()
	if revision.ProcessedAt != nil {
		processedAt = revision.ProcessedAt.UTC()
	}

	for _, item := range changedAPIs {
		artifact, err := p.store.GetSpecArtifactByAPISpecRevisionID(ctx, item.toAPISpecRevisionID)
		includeFullEvent := true
		if err != nil {
			if errors.Is(err, store.ErrSpecArtifactNotFound) {
				includeFullEvent = false
			} else {
				return fmt.Errorf(
					"load spec artifact for api_spec_id=%d api_spec_revision_id=%d: %w",
					item.apiSpec.ID,
					item.toAPISpecRevisionID,
					err,
				)
			}
		}

		specChange, err := p.store.GetSpecChangeByAPISpecIDAndToAPISpecRevisionID(
			ctx,
			item.apiSpec.ID,
			item.toAPISpecRevisionID,
		)
		if err != nil {
			return fmt.Errorf(
				"load spec change for api_spec_id=%d to_api_spec_revision_id=%d: %w",
				item.apiSpec.ID,
				item.toAPISpecRevisionID,
				err,
			)
		}

		if err := p.notifier.NotifyRevision(ctx, notify.RevisionNotification{
			TenantID:          tenant.ID,
			TenantKey:         tenant.Key,
			RepoID:            repo.ID,
			RepoPath:          repo.PathWithNamespace,
			APISpecID:         item.apiSpec.ID,
			API:               item.apiSpec.RootPath,
			APISpecRevisionID: item.toAPISpecRevisionID,
			RevisionID:        revisionID,
			DeliveryID:        job.DeliveryID,
			Sha:               revision.Sha,
			Branch:            revision.Branch,
			ProcessedAt:       processedAt,
			Artifact:          artifact,
			IncludeFull:       includeFullEvent,
			SpecChange:        specChange,
		}); err != nil {
			return err
		}
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
