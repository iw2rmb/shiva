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
			IncludeGlobs:              cfg.OpenAPIPathGlobs,
			MaxFetches:                cfg.OpenAPIRefMaxFetches,
			BootstrapFetchConcurrency: cfg.OpenAPIBootstrapFetchConcurrency,
			BootstrapSniffBytes:       cfg.OpenAPIBootstrapSniffBytes,
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
	} else {
		logger.Info("worker manager disabled: database is not configured")
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
	ListActiveAPISpecsWithLatestDependencies(
		ctx context.Context,
		repoID int64,
	) ([]store.ActiveAPISpecWithLatestDependencies, error)
	MarkAPISpecDeleted(ctx context.Context, apiSpecID int64) error
	CreateAPISpecRevision(ctx context.Context, input store.CreateAPISpecRevisionInput) (store.APISpecRevision, error)
	ReplaceAPISpecDependencies(ctx context.Context, input store.ReplaceAPISpecDependenciesInput) error
	PersistCanonicalSpec(ctx context.Context, input store.PersistCanonicalSpecInput) error
	GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
		ctx context.Context,
		repoID int64,
		branch string,
		excludeRevisionID int64,
	) (store.Revision, bool, error)
	ListEndpointIndexByRevision(ctx context.Context, revisionID int64) ([]store.EndpointIndexRecord, error)
	PersistSpecChange(ctx context.Context, input store.PersistSpecChangeInput) error
	GetTenantByID(ctx context.Context, tenantID int64) (store.Tenant, error)
	GetRevisionByID(ctx context.Context, revisionID int64) (store.Revision, error)
	GetSpecArtifactByRevisionID(ctx context.Context, revisionID int64) (store.SpecArtifact, error)
	GetSpecChangeByToRevision(ctx context.Context, toRevisionID int64) (store.SpecChange, error)
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

		successfulRoots, err := p.processResolvedRoots(ctx, job, revisionID, bootstrapRoots)
		if err != nil {
			recordBuildMetric()
			processSpan.RecordError(err)
			processSpan.SetStatus(codes.Error, "bootstrap build stage failed")
			return fmt.Errorf("build bootstrap openapi roots for revision %d: %w", revisionID, err)
		}
		openAPIChanged = successfulRoots > 0

		if openAPIChanged {
			if err := p.persistSemanticDiff(ctx, job, revisionID); err != nil {
				recordBuildMetric()
				processSpan.RecordError(err)
				processSpan.SetStatus(codes.Error, "semantic diff failed")
				return fmt.Errorf("persist semantic diff for revision %d: %w", revisionID, err)
			}
			buildSuccess = true
		}
		recordBuildMetric()
	default:
		incrementalStartedAt := time.Now()
		openAPIChanged, err = p.processIncrementalRevision(ctx, job, revisionID, repo.GitLabProjectID, parentSHA)
		if err != nil {
			return p.handleRevisionError(ctx, processSpan, revisionID, err, "incremental openapi processing")
		}
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
		if err := p.emitOutboundNotifications(ctx, repo, revisionID, job); err != nil {
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

func (p revisionProcessor) processResolvedRoots(
	ctx context.Context,
	job worker.QueueJob,
	revisionID int64,
	roots []openapi.RootResolution,
) (int, error) {
	built := 0
	for _, root := range roots {
		apiSpec, err := p.store.UpsertAPISpec(ctx, store.UpsertAPISpecInput{
			RepoID:   job.RepoID,
			RootPath: root.RootPath,
		})
		if err != nil {
			return 0, fmt.Errorf("upsert api spec for root %q: %w", root.RootPath, err)
		}

		ok, err := p.processIncrementalAPI(ctx, job, revisionID, apiSpec, func(context.Context) (openapi.RootResolution, error) {
			return root, nil
		})
		if err != nil {
			return 0, err
		}
		if ok {
			built++
		}
	}
	return built, nil
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
) (bool, error) {
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
		return false, fmt.Errorf("load changed paths: %w", err)
	}

	impacted, err := p.resolveImpactedAPIs(compareCtx, job.RepoID, changedPaths)
	if err != nil {
		compareSpan.RecordError(err)
		compareSpan.SetStatus(codes.Error, "resolve impacted apis failed")
		compareSpan.End()
		return false, fmt.Errorf("resolve impacted apis: %w", err)
	}

	compareSpan.SetAttributes(
		attribute.Int("gitlab.compare.changed_path_count", len(changedPaths)),
		attribute.Int("openapi.incremental.impacted_api_count", len(impacted)),
	)
	compareSpan.SetStatus(codes.Ok, "")
	compareSpan.End()

	rebuiltAPIs := 0
	for _, item := range impacted {
		if item.rootDeleted {
			if err := p.store.MarkAPISpecDeleted(ctx, item.spec.ID); err != nil {
				return false, fmt.Errorf(
					"mark api spec %d deleted for root %q: %w",
					item.spec.ID,
					item.spec.RootPath,
					err,
				)
			}
			continue
		}

		built, err := p.processImpactedAPI(ctx, job, revisionID, projectID, item.spec)
		if err != nil {
			return false, err
		}
		if built {
			rebuiltAPIs++
		}
	}

	if len(impacted) == 0 {
		fallbackBuilt, err := p.processFallbackDiscovery(ctx, job, revisionID, projectID, changedPaths)
		if err != nil {
			return false, err
		}
		rebuiltAPIs += fallbackBuilt
	}

	if rebuiltAPIs == 0 {
		return false, nil
	}

	if err := p.persistSemanticDiff(ctx, job, revisionID); err != nil {
		return false, fmt.Errorf("persist semantic diff for revision %d: %w", revisionID, err)
	}

	return true, nil
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
) (bool, error) {
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
) (int, error) {
	candidatePaths := fallbackDiscoveryCandidatePaths(changedPaths)
	if len(candidatePaths) == 0 {
		return 0, nil
	}

	roots, err := p.openapiLoader.ResolveDiscoveredRootsAtPaths(ctx, p.gitlabClient, projectID, job.Sha, candidatePaths)
	if err != nil {
		return 0, fmt.Errorf("resolve fallback discovered roots: %w", err)
	}

	return p.processResolvedRoots(ctx, job, revisionID, roots)
}

func (p revisionProcessor) processIncrementalAPI(
	ctx context.Context,
	job worker.QueueJob,
	revisionID int64,
	apiSpec store.APISpec,
	resolveRoot func(context.Context) (openapi.RootResolution, error),
) (bool, error) {
	apiSpecRevision, err := p.store.CreateAPISpecRevision(ctx, store.CreateAPISpecRevisionInput{
		APISpecID:   apiSpec.ID,
		RevisionID:  revisionID,
		BuildStatus: apiSpecRevisionBuildStatusProcessing,
	})
	if err != nil {
		return false, fmt.Errorf("create api spec revision for root %q: %w", apiSpec.RootPath, err)
	}

	root, err := resolveRoot(ctx)
	if err != nil {
		return p.handleIncrementalAPIFailure(ctx, apiSpec, revisionID, err)
	}

	if err := p.store.ReplaceAPISpecDependencies(ctx, store.ReplaceAPISpecDependenciesInput{
		APISpecRevisionID: apiSpecRevision.ID,
		FilePaths:         root.DependencyFiles,
	}); err != nil {
		return false, fmt.Errorf("replace api spec dependencies for root %q: %w", apiSpec.RootPath, err)
	}

	if err := p.runBuildStage(ctx, job, revisionID, root); err != nil {
		return p.handleIncrementalAPIFailure(
			ctx,
			apiSpec,
			revisionID,
			fmt.Errorf("build canonical openapi for root %q: %w", apiSpec.RootPath, err),
		)
	}

	if _, err := p.store.CreateAPISpecRevision(ctx, store.CreateAPISpecRevisionInput{
		APISpecID:   apiSpec.ID,
		RevisionID:  revisionID,
		BuildStatus: apiSpecRevisionBuildStatusProcessed,
	}); err != nil {
		return false, fmt.Errorf("mark api spec revision processed for root %q: %w", apiSpec.RootPath, err)
	}

	return true, nil
}

func (p revisionProcessor) handleIncrementalAPIFailure(
	ctx context.Context,
	apiSpec store.APISpec,
	revisionID int64,
	err error,
) (bool, error) {
	if !isPermanentOpenAPIProcessingError(err) {
		return false, err
	}

	if _, markErr := p.store.CreateAPISpecRevision(ctx, store.CreateAPISpecRevisionInput{
		APISpecID:   apiSpec.ID,
		RevisionID:  revisionID,
		BuildStatus: apiSpecRevisionBuildStatusFailed,
		Error:       err.Error(),
	}); markErr != nil {
		return false, fmt.Errorf("mark api spec revision failed for root %q: %w", apiSpec.RootPath, markErr)
	}

	return false, nil
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
	root openapi.RootResolution,
) error {
	buildCtx, buildSpan := p.effectiveTracer().Start(ctx, "spec.build", trace.WithAttributes(
		attribute.Int64("repo.id", job.RepoID),
		attribute.Int64("revision.id", revisionID),
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
		RevisionID: revisionID,
		SpecJSON:   canonicalSpec.SpecJSON,
		SpecYAML:   canonicalSpec.SpecYAML,
		ETag:       canonicalSpec.ETag,
		SizeBytes:  canonicalSpec.SizeBytes,
		Endpoints:  endpoints,
	}); err != nil {
		buildSpan.RecordError(err)
		buildSpan.SetStatus(codes.Error, "persist canonical spec failed")
		return fmt.Errorf("persist canonical openapi for revision %d: %w", revisionID, err)
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

func (p revisionProcessor) persistSemanticDiff(ctx context.Context, job worker.QueueJob, toRevisionID int64) error {
	ctx, span := p.effectiveTracer().Start(ctx, "diff.compute", trace.WithAttributes(
		attribute.Int64("repo.id", job.RepoID),
		attribute.Int64("revision.id", toRevisionID),
		attribute.String("delivery.id", job.DeliveryID),
		attribute.String("revision.sha", job.Sha),
	))
	defer span.End()

	previousRevision, hasPreviousRevision, err := p.store.GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
		ctx,
		job.RepoID,
		job.Branch,
		toRevisionID,
	)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "load previous revision failed")
		return fmt.Errorf("load previous processed openapi revision: %w", err)
	}

	currentEndpoints, err := p.store.ListEndpointIndexByRevision(ctx, toRevisionID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "load current endpoints failed")
		return fmt.Errorf("load endpoint index for current revision %d: %w", toRevisionID, err)
	}

	previousEndpoints := make([]store.EndpointIndexRecord, 0)
	var fromRevisionID *int64
	if hasPreviousRevision {
		previousEndpoints, err = p.store.ListEndpointIndexByRevision(ctx, previousRevision.ID)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "load previous endpoints failed")
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
		RepoID:         job.RepoID,
		FromRevisionID: fromRevisionID,
		ToRevisionID:   toRevisionID,
		ChangeJSON:     changeJSON,
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
		DeliveryID:  job.DeliveryID,
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
