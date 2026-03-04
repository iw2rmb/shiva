package main

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/iw2rmb/shiva/internal/gitlab"
	"github.com/iw2rmb/shiva/internal/openapi"
	"github.com/iw2rmb/shiva/internal/store"
	"github.com/iw2rmb/shiva/internal/worker"
)

func TestRevisionProcessorProcess_BootstrapPersistence(t *testing.T) {
	t.Parallel()

	const (
		repoID     = int64(77)
		projectID  = int64(9001)
		revisionID = int64(1234)
	)

	tests := []struct {
		name                      string
		roots                     []openapi.RootResolution
		wantOpenAPIChanged        bool
		wantUpsertRoots           []string
		wantCreateRevisionCalls   int
		wantDependencyReplaceCall int
		wantPersistCanonicalCalls int
		wantFinalStatusByRoot     map[string]string
		wantFailedRoots           []string
	}{
		{
			name:               "first processing with unrelated diff builds discovered root",
			roots:              []openapi.RootResolution{bootstrapGoodRootResolution("apis/pets/openapi.yaml", "listPets")},
			wantOpenAPIChanged: true,
			wantUpsertRoots: []string{
				"apis/pets/openapi.yaml",
			},
			wantCreateRevisionCalls:   2,
			wantDependencyReplaceCall: 1,
			wantPersistCanonicalCalls: 1,
			wantFinalStatusByRoot: map[string]string{
				"apis/pets/openapi.yaml": apiSpecRevisionBuildStatusProcessed,
			},
		},
		{
			name:                      "zero bootstrap roots mark revision processed false",
			roots:                     []openapi.RootResolution{},
			wantOpenAPIChanged:        false,
			wantUpsertRoots:           []string{},
			wantCreateRevisionCalls:   0,
			wantDependencyReplaceCall: 0,
			wantPersistCanonicalCalls: 0,
			wantFinalStatusByRoot:     map[string]string{},
		},
		{
			name: "per root canonical failure isolated from successful root",
			roots: []openapi.RootResolution{
				bootstrapInvalidRootResolution("apis/bad/openapi.yaml"),
				bootstrapGoodRootResolution("apis/good/openapi.yaml", "listGood"),
			},
			wantOpenAPIChanged: true,
			wantUpsertRoots: []string{
				"apis/bad/openapi.yaml",
				"apis/good/openapi.yaml",
			},
			wantCreateRevisionCalls:   4,
			wantDependencyReplaceCall: 2,
			wantPersistCanonicalCalls: 1,
			wantFinalStatusByRoot: map[string]string{
				"apis/bad/openapi.yaml":  apiSpecRevisionBuildStatusFailed,
				"apis/good/openapi.yaml": apiSpecRevisionBuildStatusProcessed,
			},
			wantFailedRoots: []string{"apis/bad/openapi.yaml"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			storeFake := newBootstrapPersistenceRevisionStore(repoID, projectID, revisionID)
			resolverFake := &bootstrapPersistenceResolver{roots: tc.roots}
			processor := revisionProcessor{
				store:         storeFake,
				gitlabClient:  bootstrapPersistenceGitLabClient{},
				openapiLoader: resolverFake,
			}
			job := worker.QueueJob{
				EventID:    55,
				RepoID:     repoID,
				DeliveryID: "delivery-55",
				Sha:        "2222222222222222222222222222222222222222",
				Branch:     "main",
				ParentSha:  "1111111111111111111111111111111111111111",
			}

			if err := processor.Process(context.Background(), job); err != nil {
				t.Fatalf("Process() unexpected error: %v", err)
			}

			if len(resolverFake.bootstrapCalls) != 1 {
				t.Fatalf("expected one bootstrap resolver call, got %d", len(resolverFake.bootstrapCalls))
			}
			if len(resolverFake.incrementalCalls) != 0 {
				t.Fatalf("expected zero incremental resolver calls, got %d", len(resolverFake.incrementalCalls))
			}

			if storeFake.markProcessedCalls != 1 {
				t.Fatalf("expected one MarkRevisionProcessed call, got %d", storeFake.markProcessedCalls)
			}
			if storeFake.markProcessedOpenAPIChanged != tc.wantOpenAPIChanged {
				t.Fatalf("expected openapi_changed=%t, got %t", tc.wantOpenAPIChanged, storeFake.markProcessedOpenAPIChanged)
			}
			if storeFake.clearRepoForceRescanCalls != 1 {
				t.Fatalf("expected one ClearRepoForceRescan call, got %d", storeFake.clearRepoForceRescanCalls)
			}

			gotUpsertRoots := append([]string(nil), storeFake.upsertRoots...)
			sort.Strings(gotUpsertRoots)
			wantUpsertRoots := append([]string(nil), tc.wantUpsertRoots...)
			sort.Strings(wantUpsertRoots)
			if !reflect.DeepEqual(gotUpsertRoots, wantUpsertRoots) {
				t.Fatalf("expected upsert roots %v, got %v", wantUpsertRoots, gotUpsertRoots)
			}

			if len(storeFake.createAPISpecRevisionCalls) != tc.wantCreateRevisionCalls {
				t.Fatalf(
					"expected %d CreateAPISpecRevision calls, got %d",
					tc.wantCreateRevisionCalls,
					len(storeFake.createAPISpecRevisionCalls),
				)
			}
			if len(storeFake.replaceAPISpecDependenciesCalls) != tc.wantDependencyReplaceCall {
				t.Fatalf(
					"expected %d ReplaceAPISpecDependencies calls, got %d",
					tc.wantDependencyReplaceCall,
					len(storeFake.replaceAPISpecDependenciesCalls),
				)
			}
			if len(storeFake.persistCanonicalCalls) != tc.wantPersistCanonicalCalls {
				t.Fatalf(
					"expected %d PersistCanonicalSpec calls, got %d",
					tc.wantPersistCanonicalCalls,
					len(storeFake.persistCanonicalCalls),
				)
			}

			if !reflect.DeepEqual(storeFake.finalStatusByRoot, tc.wantFinalStatusByRoot) {
				t.Fatalf("expected final statuses %v, got %v", tc.wantFinalStatusByRoot, storeFake.finalStatusByRoot)
			}
			for _, root := range tc.wantFailedRoots {
				if strings.TrimSpace(storeFake.finalErrorByRoot[root]) == "" {
					t.Fatalf("expected failed root %q to store non-empty error", root)
				}
			}
		})
	}
}

func bootstrapGoodRootResolution(rootPath, operationID string) openapi.RootResolution {
	return openapi.RootResolution{
		RootPath: rootPath,
		Documents: map[string][]byte{
			rootPath: []byte("openapi: 3.1.0\ninfo:\n  title: Bootstrap API\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: " + operationID + "\n      responses:\n        '200':\n          description: ok\n"),
		},
		DependencyFiles: []string{rootPath},
	}
}

func bootstrapInvalidRootResolution(rootPath string) openapi.RootResolution {
	return openapi.RootResolution{
		RootPath: rootPath,
		Documents: map[string][]byte{
			rootPath: []byte("openapi: ["),
		},
		DependencyFiles: []string{rootPath},
	}
}

type bootstrapPersistenceResolver struct {
	roots            []openapi.RootResolution
	incrementalCalls []incrementalCall
	bootstrapCalls   []bootstrapCall
}

func (r *bootstrapPersistenceResolver) ResolveRootOpenAPIAtSHA(
	_ context.Context,
	_ openapi.GitLabClient,
	projectID int64,
	sha string,
	rootPath string,
) (openapi.RootResolution, error) {
	r.incrementalCalls = append(r.incrementalCalls, incrementalCall{
		projectID: projectID,
		fromSHA:   sha,
		toSHA:     rootPath,
	})
	return openapi.RootResolution{}, fmt.Errorf("unexpected ResolveRootOpenAPIAtSHA call")
}

func (r *bootstrapPersistenceResolver) ResolveDiscoveredRootsAtPaths(
	_ context.Context,
	_ openapi.GitLabClient,
	projectID int64,
	sha string,
	paths []string,
) ([]openapi.RootResolution, error) {
	r.incrementalCalls = append(r.incrementalCalls, incrementalCall{
		projectID: projectID,
		fromSHA:   sha,
		toSHA:     strings.Join(paths, ","),
	})
	return nil, fmt.Errorf("unexpected ResolveDiscoveredRootsAtPaths call")
}

func (r *bootstrapPersistenceResolver) ResolveRepositoryOpenAPIAtSHA(
	_ context.Context,
	_ openapi.GitLabBootstrapClient,
	projectID int64,
	sha string,
) ([]openapi.RootResolution, error) {
	r.bootstrapCalls = append(r.bootstrapCalls, bootstrapCall{
		projectID: projectID,
		sha:       sha,
	})
	copied := make([]openapi.RootResolution, len(r.roots))
	copy(copied, r.roots)
	return copied, nil
}

type bootstrapPersistenceGitLabClient struct{}

func (bootstrapPersistenceGitLabClient) CompareChangedPaths(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]gitlab.ChangedPath, error) {
	return []gitlab.ChangedPath{}, nil
}

func (bootstrapPersistenceGitLabClient) GetFileContent(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]byte, error) {
	return nil, nil
}

func (bootstrapPersistenceGitLabClient) ListRepositoryTree(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
	_ bool,
) ([]gitlab.TreeEntry, error) {
	return []gitlab.TreeEntry{}, nil
}

type bootstrapPersistenceRevisionStore struct {
	repo           store.Repo
	bootstrapState store.RepoBootstrapState
	revisionID     int64

	markProcessedCalls          int
	markProcessedRevisionID     int64
	markProcessedOpenAPIChanged bool
	clearRepoForceRescanCalls   int

	nextAPISpecID                   int64
	nextAPISpecRevisionID           int64
	apiSpecIDByRoot                 map[string]int64
	rootByAPISpecID                 map[int64]string
	apiSpecRevisionIDBySpecRevision map[string]int64

	upsertRoots                     []string
	createAPISpecRevisionCalls      []store.CreateAPISpecRevisionInput
	replaceAPISpecDependenciesCalls []store.ReplaceAPISpecDependenciesInput
	persistCanonicalCalls           []store.PersistCanonicalSpecInput
	persistSpecChangeCalls          []store.PersistSpecChangeInput

	finalStatusByRoot map[string]string
	finalErrorByRoot  map[string]string
	endpoints         map[int64][]store.EndpointIndexRecord
}

func newBootstrapPersistenceRevisionStore(repoID, projectID, revisionID int64) *bootstrapPersistenceRevisionStore {
	return &bootstrapPersistenceRevisionStore{
		repo: store.Repo{
			ID:              repoID,
			GitLabProjectID: projectID,
		},
		bootstrapState: store.RepoBootstrapState{
			ActiveAPICount: 0,
			ForceRescan:    true,
		},
		revisionID:                      revisionID,
		nextAPISpecID:                   100,
		nextAPISpecRevisionID:           500,
		apiSpecIDByRoot:                 make(map[string]int64),
		rootByAPISpecID:                 make(map[int64]string),
		apiSpecRevisionIDBySpecRevision: make(map[string]int64),
		finalStatusByRoot:               make(map[string]string),
		finalErrorByRoot:                make(map[string]string),
		endpoints:                       make(map[int64][]store.EndpointIndexRecord),
	}
}

func (s *bootstrapPersistenceRevisionStore) UpsertRevisionFromIngestEvent(
	_ context.Context,
	_ store.IngestQueueEvent,
) (int64, error) {
	return s.revisionID, nil
}

func (s *bootstrapPersistenceRevisionStore) MarkRevisionProcessed(
	_ context.Context,
	revisionID int64,
	openapiChanged bool,
) error {
	s.markProcessedCalls++
	s.markProcessedRevisionID = revisionID
	s.markProcessedOpenAPIChanged = openapiChanged
	return nil
}

func (s *bootstrapPersistenceRevisionStore) MarkRevisionFailed(_ context.Context, revisionID int64, _ string) error {
	return fmt.Errorf("unexpected MarkRevisionFailed call for revision %d", revisionID)
}

func (s *bootstrapPersistenceRevisionStore) GetRepoByID(_ context.Context, repoID int64) (store.Repo, error) {
	if s.repo.ID != repoID {
		return store.Repo{}, fmt.Errorf("repo %d not found", repoID)
	}
	return s.repo, nil
}

func (s *bootstrapPersistenceRevisionStore) GetRepoBootstrapState(
	_ context.Context,
	repoID int64,
) (store.RepoBootstrapState, error) {
	if s.repo.ID != repoID {
		return store.RepoBootstrapState{}, fmt.Errorf("repo %d not found", repoID)
	}
	return s.bootstrapState, nil
}

func (s *bootstrapPersistenceRevisionStore) ClearRepoForceRescan(_ context.Context, repoID int64) error {
	if s.repo.ID != repoID {
		return fmt.Errorf("repo %d not found", repoID)
	}
	s.clearRepoForceRescanCalls++
	s.bootstrapState.ForceRescan = false
	return nil
}

func (s *bootstrapPersistenceRevisionStore) UpsertAPISpec(
	_ context.Context,
	input store.UpsertAPISpecInput,
) (store.APISpec, error) {
	rootPath := strings.TrimSpace(input.RootPath)
	s.upsertRoots = append(s.upsertRoots, rootPath)

	apiSpecID, exists := s.apiSpecIDByRoot[rootPath]
	if !exists {
		s.nextAPISpecID++
		apiSpecID = s.nextAPISpecID
		s.apiSpecIDByRoot[rootPath] = apiSpecID
		s.rootByAPISpecID[apiSpecID] = rootPath
	}

	return store.APISpec{
		ID:       apiSpecID,
		RepoID:   input.RepoID,
		RootPath: rootPath,
		Status:   "active",
	}, nil
}

func (s *bootstrapPersistenceRevisionStore) ListActiveAPISpecsWithLatestDependencies(
	_ context.Context,
	_ int64,
) ([]store.ActiveAPISpecWithLatestDependencies, error) {
	return nil, fmt.Errorf("unexpected ListActiveAPISpecsWithLatestDependencies call")
}

func (s *bootstrapPersistenceRevisionStore) MarkAPISpecDeleted(_ context.Context, _ int64) error {
	return fmt.Errorf("unexpected MarkAPISpecDeleted call")
}

func (s *bootstrapPersistenceRevisionStore) CreateAPISpecRevision(
	_ context.Context,
	input store.CreateAPISpecRevisionInput,
) (store.APISpecRevision, error) {
	rootPath, exists := s.rootByAPISpecID[input.APISpecID]
	if !exists {
		return store.APISpecRevision{}, fmt.Errorf("api spec %d not found", input.APISpecID)
	}

	s.createAPISpecRevisionCalls = append(s.createAPISpecRevisionCalls, input)
	s.finalStatusByRoot[rootPath] = input.BuildStatus
	s.finalErrorByRoot[rootPath] = input.Error

	key := fmt.Sprintf("%d:%d", input.APISpecID, input.RevisionID)
	apiSpecRevisionID, exists := s.apiSpecRevisionIDBySpecRevision[key]
	if !exists {
		s.nextAPISpecRevisionID++
		apiSpecRevisionID = s.nextAPISpecRevisionID
		s.apiSpecRevisionIDBySpecRevision[key] = apiSpecRevisionID
	}

	return store.APISpecRevision{
		ID:                 apiSpecRevisionID,
		APISpecID:          input.APISpecID,
		RevisionID:         input.RevisionID,
		RootPathAtRevision: rootPath,
		BuildStatus:        input.BuildStatus,
		Error:              input.Error,
	}, nil
}

func (s *bootstrapPersistenceRevisionStore) ReplaceAPISpecDependencies(
	_ context.Context,
	input store.ReplaceAPISpecDependenciesInput,
) error {
	s.replaceAPISpecDependenciesCalls = append(s.replaceAPISpecDependenciesCalls, input)
	return nil
}

func (s *bootstrapPersistenceRevisionStore) PersistCanonicalSpec(
	_ context.Context,
	input store.PersistCanonicalSpecInput,
) error {
	s.persistCanonicalCalls = append(s.persistCanonicalCalls, input)

	rows := make([]store.EndpointIndexRecord, len(input.Endpoints))
	copy(rows, input.Endpoints)
	s.endpoints[input.RevisionID] = rows
	return nil
}

func (s *bootstrapPersistenceRevisionStore) GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
	_ context.Context,
	_ int64,
	_ string,
	_ int64,
) (store.Revision, bool, error) {
	return store.Revision{}, false, nil
}

func (s *bootstrapPersistenceRevisionStore) ListEndpointIndexByRevision(
	_ context.Context,
	revisionID int64,
) ([]store.EndpointIndexRecord, error) {
	rows, exists := s.endpoints[revisionID]
	if !exists {
		return nil, nil
	}
	copied := make([]store.EndpointIndexRecord, len(rows))
	copy(copied, rows)
	return copied, nil
}

func (s *bootstrapPersistenceRevisionStore) PersistSpecChange(_ context.Context, input store.PersistSpecChangeInput) error {
	s.persistSpecChangeCalls = append(s.persistSpecChangeCalls, input)
	return nil
}

func (s *bootstrapPersistenceRevisionStore) GetTenantByID(_ context.Context, _ int64) (store.Tenant, error) {
	return store.Tenant{}, fmt.Errorf("unexpected GetTenantByID call")
}

func (s *bootstrapPersistenceRevisionStore) GetRevisionByID(_ context.Context, _ int64) (store.Revision, error) {
	return store.Revision{}, fmt.Errorf("unexpected GetRevisionByID call")
}

func (s *bootstrapPersistenceRevisionStore) GetSpecArtifactByRevisionID(_ context.Context, _ int64) (store.SpecArtifact, error) {
	return store.SpecArtifact{}, fmt.Errorf("unexpected GetSpecArtifactByRevisionID call")
}

func (s *bootstrapPersistenceRevisionStore) GetSpecChangeByToRevision(_ context.Context, _ int64) (store.SpecChange, error) {
	return store.SpecChange{}, fmt.Errorf("unexpected GetSpecChangeByToRevision call")
}
