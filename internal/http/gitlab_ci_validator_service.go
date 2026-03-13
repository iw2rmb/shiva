package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/iw2rmb/shiva/internal/gitlab"
	"github.com/iw2rmb/shiva/internal/openapi"
	"github.com/iw2rmb/shiva/internal/openapi/lint"
	"github.com/iw2rmb/shiva/internal/store"
)

var ErrGitLabCIRepoProjectMismatch = errors.New("gitlab project id does not match repo")

type gitlabCIValidationStore interface {
	GetRepoByNamespaceAndRepo(ctx context.Context, namespace string, repo string) (store.Repo, error)
	ListActiveAPISpecsWithLatestDependencies(
		ctx context.Context,
		repoID int64,
	) ([]store.ActiveAPISpecWithLatestDependencies, error)
}

type gitlabCIValidationGitLabClient interface {
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

type gitlabCIValidationOpenAPIResolver interface {
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

type gitlabCISourceRunner interface {
	RunSourceLayoutRoot(
		ctx context.Context,
		rootPath string,
		documents map[string][]byte,
	) (lint.SourceExecutionResult, error)
}

type GitLabCIValidationService struct {
	store           gitlabCIValidationStore
	gitlabClient    gitlabCIValidationGitLabClient
	openapiResolver gitlabCIValidationOpenAPIResolver
	sourceRunner    gitlabCISourceRunner
	logger          *slog.Logger
}

func NewGitLabCIValidationService(
	store gitlabCIValidationStore,
	gitlabClient gitlabCIValidationGitLabClient,
	openapiResolver gitlabCIValidationOpenAPIResolver,
	sourceRunner gitlabCISourceRunner,
	logger *slog.Logger,
) *GitLabCIValidationService {
	return &GitLabCIValidationService{
		store:           store,
		gitlabClient:    gitlabClient,
		openapiResolver: openapiResolver,
		sourceRunner:    sourceRunner,
		logger:          logger,
	}
}

func (s *GitLabCIValidationService) ValidateGitLabCI(
	ctx context.Context,
	input GitLabCIValidationInput,
) (GitLabCIValidationResult, error) {
	if s.store == nil {
		return GitLabCIValidationResult{}, store.ErrStoreNotConfigured
	}
	if s.gitlabClient == nil {
		return GitLabCIValidationResult{}, errors.New("gitlab client is not configured")
	}
	if s.openapiResolver == nil {
		return GitLabCIValidationResult{}, errors.New("openapi resolver is not configured")
	}
	if s.sourceRunner == nil {
		return GitLabCIValidationResult{}, errors.New("source vacuum runner is not configured")
	}

	repo, err := s.store.GetRepoByNamespaceAndRepo(ctx, input.Namespace, input.Repo)
	if err != nil {
		return GitLabCIValidationResult{}, err
	}
	if repo.GitLabProjectID != input.GitLabProjectID {
		return GitLabCIValidationResult{}, fmt.Errorf(
			"%w: requested=%d stored=%d repo=%s/%s",
			ErrGitLabCIRepoProjectMismatch,
			input.GitLabProjectID,
			repo.GitLabProjectID,
			input.Namespace,
			input.Repo,
		)
	}

	roots, err := s.resolveValidationRoots(ctx, repo, input)
	if err != nil {
		return GitLabCIValidationResult{}, err
	}
	if len(roots) == 0 {
		return GitLabCIValidationResult{Specs: []GitLabCIValidationSpecResult{}}, nil
	}

	specs := make([]GitLabCIValidationSpecResult, 0, len(roots))
	for _, root := range roots {
		result, err := s.sourceRunner.RunSourceLayoutRoot(ctx, root.RootPath, root.Documents)
		if err != nil {
			return GitLabCIValidationResult{}, fmt.Errorf("run source vacuum for root %q: %w", root.RootPath, err)
		}
		if result.Failure != nil {
			return GitLabCIValidationResult{}, fmt.Errorf(
				"run source vacuum for root %q: %s",
				root.RootPath,
				strings.TrimSpace(result.Failure.Message),
			)
		}

		issues := make([]GitLabCIValidationIssue, 0, len(result.Issues))
		for _, issue := range result.Issues {
			issues = append(issues, GitLabCIValidationIssue{
				RuleID:   issue.RuleID,
				Severity: issue.Severity,
				Message:  issue.Message,
				JSONPath: issue.JSONPath,
				FilePath: issue.FilePath,
				RangePos: issue.RangePos,
			})
		}
		specs = append(specs, GitLabCIValidationSpecResult{
			RootPath: root.RootPath,
			Issues:   issues,
		})
	}

	return GitLabCIValidationResult{Specs: specs}, nil
}

func (s *GitLabCIValidationService) resolveValidationRoots(
	ctx context.Context,
	repo store.Repo,
	input GitLabCIValidationInput,
) ([]openapi.RootResolution, error) {
	if strings.TrimSpace(input.ParentSHA) == "" {
		roots, err := s.openapiResolver.ResolveRepositoryOpenAPIAtSHA(
			ctx,
			s.gitlabClient,
			input.GitLabProjectID,
			input.SHA,
		)
		if err != nil {
			return nil, fmt.Errorf("resolve repository roots at sha %q: %w", input.SHA, err)
		}
		return roots, nil
	}

	changedPaths, err := s.gitlabClient.CompareChangedPaths(ctx, input.GitLabProjectID, input.ParentSHA, input.SHA)
	if err != nil {
		return nil, fmt.Errorf("load changed paths: %w", err)
	}

	activeSpecs, err := s.store.ListActiveAPISpecsWithLatestDependencies(ctx, repo.ID)
	if err != nil {
		return nil, err
	}

	rootSets := make([]gitlab.OpenAPIRootDependencySet, 0, len(activeSpecs))
	for _, spec := range activeSpecs {
		rootSets = append(rootSets, gitlab.OpenAPIRootDependencySet{
			RootPath:        spec.RootPath,
			DependencyPaths: spec.DependencyFilePaths,
		})
	}

	impactedRoots := gitlab.ImpactedOpenAPIRoots(rootSets, changedPaths)
	if len(impactedRoots) > 0 {
		roots := make([]openapi.RootResolution, 0, len(impactedRoots))
		for _, impactedRoot := range impactedRoots {
			if impactedRoot.RootDeleted {
				continue
			}
			root, err := s.openapiResolver.ResolveRootOpenAPIAtSHA(
				ctx,
				s.gitlabClient,
				input.GitLabProjectID,
				input.SHA,
				impactedRoot.RootPath,
			)
			if err != nil {
				return nil, fmt.Errorf("resolve impacted root %q: %w", impactedRoot.RootPath, err)
			}
			roots = append(roots, root)
		}
		return roots, nil
	}

	candidatePaths := gitlab.FallbackDiscoveryCandidatePaths(changedPaths)
	if len(candidatePaths) == 0 {
		return []openapi.RootResolution{}, nil
	}

	roots, err := s.openapiResolver.ResolveDiscoveredRootsAtPaths(
		ctx,
		s.gitlabClient,
		input.GitLabProjectID,
		input.SHA,
		candidatePaths,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve fallback discovered roots: %w", err)
	}

	return roots, nil
}
