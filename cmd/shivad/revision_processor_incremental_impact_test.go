package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/iw2rmb/shiva/internal/gitlab"
	"github.com/iw2rmb/shiva/internal/openapi"
	"github.com/iw2rmb/shiva/internal/store"
	"github.com/iw2rmb/shiva/internal/worker"
)

func TestRevisionProcessorProcess_IncrementalImpactResolution(t *testing.T) {
	t.Parallel()

	const (
		repoID    = int64(77)
		projectID = int64(9001)
	)

	tests := []struct {
		name                    string
		changedPaths            []gitlab.ChangedPath
		activeSpecs             []store.ActiveAPISpecWithLatestDependencies
		resolvedRootByPath      map[string]openapi.RootResolution
		discoveredRoots         []openapi.RootResolution
		wantRootResolvePaths    []string
		wantFallbackPaths       []string
		wantMarkedDeletedIDs    []int64
		wantPersistCanonical    int
		wantPersistSpecChange   int
		wantCreateSpecRevisions int
		wantUpsertRoots         []string
		wantOpenAPIChanged      bool
	}{
		{
			name: "impact only rebuild resolves only intersected api",
			changedPaths: []gitlab.ChangedPath{
				{NewPath: "apis/pets/components.yaml"},
			},
			activeSpecs: []store.ActiveAPISpecWithLatestDependencies{
				{
					APISpec: store.APISpec{
						ID:       11,
						RepoID:   repoID,
						RootPath: "apis/pets/openapi.yaml",
						Status:   "active",
					},
					DependencyFilePaths: []string{"apis/pets/openapi.yaml", "apis/pets/components.yaml"},
				},
				{
					APISpec: store.APISpec{
						ID:       12,
						RepoID:   repoID,
						RootPath: "apis/dogs/openapi.yaml",
						Status:   "active",
					},
					DependencyFilePaths: []string{"apis/dogs/openapi.yaml", "apis/dogs/components.yaml"},
				},
			},
			resolvedRootByPath: map[string]openapi.RootResolution{
				"apis/pets/openapi.yaml": incrementalGoodRootResolution("apis/pets/openapi.yaml", "listPets"),
			},
			wantRootResolvePaths:    []string{"apis/pets/openapi.yaml"},
			wantMarkedDeletedIDs:    []int64{},
			wantPersistCanonical:    1,
			wantPersistSpecChange:   1,
			wantCreateSpecRevisions: 2,
			wantUpsertRoots:         []string{},
			wantOpenAPIChanged:      true,
		},
		{
			name: "unrelated changes do not rebuild",
			changedPaths: []gitlab.ChangedPath{
				{NewPath: "README.md"},
			},
			activeSpecs: []store.ActiveAPISpecWithLatestDependencies{
				{
					APISpec: store.APISpec{
						ID:       11,
						RepoID:   repoID,
						RootPath: "apis/pets/openapi.yaml",
						Status:   "active",
					},
					DependencyFilePaths: []string{"apis/pets/openapi.yaml", "apis/pets/components.yaml"},
				},
			},
			wantRootResolvePaths:    []string{},
			wantMarkedDeletedIDs:    []int64{},
			wantPersistCanonical:    0,
			wantPersistSpecChange:   0,
			wantCreateSpecRevisions: 0,
			wantUpsertRoots:         []string{},
			wantOpenAPIChanged:      false,
		},
		{
			name: "deleted root is deactivated without rebuild",
			changedPaths: []gitlab.ChangedPath{
				{OldPath: "apis/pets/openapi.yaml", DeletedFile: true},
			},
			activeSpecs: []store.ActiveAPISpecWithLatestDependencies{
				{
					APISpec: store.APISpec{
						ID:       11,
						RepoID:   repoID,
						RootPath: "apis/pets/openapi.yaml",
						Status:   "active",
					},
					DependencyFilePaths: []string{"apis/pets/openapi.yaml", "apis/pets/components.yaml"},
				},
			},
			wantRootResolvePaths:    []string{},
			wantMarkedDeletedIDs:    []int64{11},
			wantPersistCanonical:    0,
			wantPersistSpecChange:   1,
			wantCreateSpecRevisions: 1,
			wantUpsertRoots:         []string{},
			wantOpenAPIChanged:      true,
		},
		{
			name: "fallback discovery builds create rename candidate when no impacted apis",
			changedPaths: []gitlab.ChangedPath{
				{OldPath: "legacy/spec.yaml", NewPath: "apis/new/openapi.yaml", RenamedFile: true},
			},
			activeSpecs: []store.ActiveAPISpecWithLatestDependencies{
				{
					APISpec: store.APISpec{
						ID:       11,
						RepoID:   repoID,
						RootPath: "apis/pets/openapi.yaml",
						Status:   "active",
					},
					DependencyFilePaths: []string{"apis/pets/openapi.yaml", "apis/pets/components.yaml"},
				},
			},
			discoveredRoots: []openapi.RootResolution{
				incrementalGoodRootResolution("apis/new/openapi.yaml", "listNew"),
			},
			wantRootResolvePaths:    []string{},
			wantFallbackPaths:       []string{"apis/new/openapi.yaml"},
			wantMarkedDeletedIDs:    []int64{},
			wantPersistCanonical:    1,
			wantPersistSpecChange:   1,
			wantCreateSpecRevisions: 2,
			wantUpsertRoots:         []string{"apis/new/openapi.yaml"},
			wantOpenAPIChanged:      true,
		},
		{
			name: "rename intersects old dependency path and rebuilds impacted api",
			changedPaths: []gitlab.ChangedPath{
				{OldPath: "apis/pets/components.yaml", NewPath: "apis/pets/components-v2.yaml", RenamedFile: true},
			},
			activeSpecs: []store.ActiveAPISpecWithLatestDependencies{
				{
					APISpec: store.APISpec{
						ID:       11,
						RepoID:   repoID,
						RootPath: "apis/pets/openapi.yaml",
						Status:   "active",
					},
					DependencyFilePaths: []string{"apis/pets/openapi.yaml", "apis/pets/components.yaml"},
				},
			},
			resolvedRootByPath: map[string]openapi.RootResolution{
				"apis/pets/openapi.yaml": incrementalGoodRootResolution("apis/pets/openapi.yaml", "listPetsRenamedDep"),
			},
			wantRootResolvePaths:    []string{"apis/pets/openapi.yaml"},
			wantMarkedDeletedIDs:    []int64{},
			wantPersistCanonical:    1,
			wantPersistSpecChange:   1,
			wantCreateSpecRevisions: 2,
			wantUpsertRoots:         []string{},
			wantOpenAPIChanged:      true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			storeFake := newIncrementalImpactRevisionStore(repoID, projectID, tc.activeSpecs)
			resolverFake := &incrementalImpactResolver{
				resolvedRootByPath: tc.resolvedRootByPath,
				discoveredRoots:    tc.discoveredRoots,
			}
			gitlabClient := &incrementalImpactGitLabClient{changedPaths: tc.changedPaths}

			processor := revisionProcessor{
				store:         storeFake,
				gitlabClient:  gitlabClient,
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

			result, err := processor.Process(context.Background(), job)
			if err != nil {
				t.Fatalf("Process() unexpected error: %v", err)
			}

			if gitlabClient.compareCalls != 1 {
				t.Fatalf("expected one compare call, got %d", gitlabClient.compareCalls)
			}
			if resolverFake.bootstrapCalls != 0 {
				t.Fatalf("expected zero bootstrap resolver calls, got %d", resolverFake.bootstrapCalls)
			}

			assertSortedStringsEqual(t, "resolved root paths", resolverFake.resolveRootPaths, tc.wantRootResolvePaths)

			if len(tc.wantFallbackPaths) == 0 {
				if len(resolverFake.discoveryCalls) != 0 {
					t.Fatalf("expected no fallback discovery calls, got %d", len(resolverFake.discoveryCalls))
				}
			} else {
				if len(resolverFake.discoveryCalls) != 1 {
					t.Fatalf("expected one fallback discovery call, got %d", len(resolverFake.discoveryCalls))
				}
				assertSortedStringsEqual(t, "fallback paths", resolverFake.discoveryCalls[0], tc.wantFallbackPaths)
			}

			assertSortedInt64sEqual(t, "marked deleted ids", storeFake.markDeletedIDs, tc.wantMarkedDeletedIDs)
			if len(storeFake.persistCanonicalCalls) != tc.wantPersistCanonical {
				t.Fatalf("expected %d PersistCanonicalSpec calls, got %d", tc.wantPersistCanonical, len(storeFake.persistCanonicalCalls))
			}
			if len(storeFake.persistSpecChangeCalls) != tc.wantPersistSpecChange {
				t.Fatalf("expected %d PersistSpecChange calls, got %d", tc.wantPersistSpecChange, len(storeFake.persistSpecChangeCalls))
			}
			if len(storeFake.createAPISpecRevisionCalls) != tc.wantCreateSpecRevisions {
				t.Fatalf(
					"expected %d CreateAPISpecRevision calls, got %d",
					tc.wantCreateSpecRevisions,
					len(storeFake.createAPISpecRevisionCalls),
				)
			}

			assertSortedStringsEqual(t, "upsert roots", storeFake.upsertRoots, tc.wantUpsertRoots)

			if result.OpenAPIChanged != tc.wantOpenAPIChanged {
				t.Fatalf("expected openapi_changed=%t, got %t", tc.wantOpenAPIChanged, result.OpenAPIChanged)
			}
		})
	}
}

func TestRevisionProcessorProcess_IncrementalImpactResolution_PermanentFailureIsolatedPerAPI(t *testing.T) {
	t.Parallel()

	const (
		repoID    = int64(77)
		projectID = int64(9001)
	)

	storeFake := newIncrementalImpactRevisionStore(
		repoID,
		projectID,
		[]store.ActiveAPISpecWithLatestDependencies{
			{
				APISpec: store.APISpec{
					ID:       11,
					RepoID:   repoID,
					RootPath: "apis/bad/openapi.yaml",
					Status:   "active",
				},
				DependencyFilePaths: []string{
					"apis/bad/openapi.yaml",
					"apis/bad/components.yaml",
				},
			},
			{
				APISpec: store.APISpec{
					ID:       12,
					RepoID:   repoID,
					RootPath: "apis/good/openapi.yaml",
					Status:   "active",
				},
				DependencyFilePaths: []string{
					"apis/good/openapi.yaml",
					"apis/good/components.yaml",
				},
			},
		},
	)
	resolverFake := &incrementalImpactResolver{
		resolveRootErrByPath: map[string]error{
			"apis/bad/openapi.yaml": openapi.ErrCanonicalRootNotFound,
		},
		resolvedRootByPath: map[string]openapi.RootResolution{
			"apis/good/openapi.yaml": incrementalGoodRootResolution("apis/good/openapi.yaml", "listGood"),
		},
	}
	gitlabClient := &incrementalImpactGitLabClient{
		changedPaths: []gitlab.ChangedPath{
			{NewPath: "apis/bad/components.yaml"},
			{NewPath: "apis/good/components.yaml"},
		},
	}
	processor := revisionProcessor{
		store:         storeFake,
		gitlabClient:  gitlabClient,
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

	result, err := processor.Process(context.Background(), job)
	if err != nil {
		t.Fatalf("Process() unexpected error: %v", err)
	}

	assertSortedStringsEqual(t, "resolved root paths", resolverFake.resolveRootPaths, []string{
		"apis/bad/openapi.yaml",
		"apis/good/openapi.yaml",
	})

	if len(storeFake.persistCanonicalCalls) != 1 {
		t.Fatalf("expected one PersistCanonicalSpec call, got %d", len(storeFake.persistCanonicalCalls))
	}
	if len(storeFake.persistSpecChangeCalls) != 1 {
		t.Fatalf("expected one PersistSpecChange call, got %d", len(storeFake.persistSpecChangeCalls))
	}
	if !result.OpenAPIChanged {
		t.Fatalf("expected openapi_changed=true")
	}
	if len(storeFake.createAPISpecRevisionCalls) != 4 {
		t.Fatalf("expected 4 CreateAPISpecRevision calls, got %d", len(storeFake.createAPISpecRevisionCalls))
	}

	wantFinalStatusByRoot := map[string]string{
		"apis/bad/openapi.yaml":  apiSpecRevisionBuildStatusFailed,
		"apis/good/openapi.yaml": apiSpecRevisionBuildStatusProcessed,
	}
	if !reflect.DeepEqual(storeFake.finalStatusByRoot, wantFinalStatusByRoot) {
		t.Fatalf("expected final statuses %v, got %v", wantFinalStatusByRoot, storeFake.finalStatusByRoot)
	}
	if storeFake.finalErrorByRoot["apis/bad/openapi.yaml"] == "" {
		t.Fatalf("expected failed root to persist non-empty error")
	}
}

func assertSortedStringsEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotCopy := append([]string(nil), got...)
	wantCopy := append([]string(nil), want...)
	sort.Strings(gotCopy)
	sort.Strings(wantCopy)
	if !reflect.DeepEqual(gotCopy, wantCopy) {
		t.Fatalf("expected %s %v, got %v", label, wantCopy, gotCopy)
	}
}

func assertSortedInt64sEqual(t *testing.T, label string, got, want []int64) {
	t.Helper()
	gotCopy := append([]int64(nil), got...)
	wantCopy := append([]int64(nil), want...)
	sort.Slice(gotCopy, func(i, j int) bool { return gotCopy[i] < gotCopy[j] })
	sort.Slice(wantCopy, func(i, j int) bool { return wantCopy[i] < wantCopy[j] })
	if !reflect.DeepEqual(gotCopy, wantCopy) {
		t.Fatalf("expected %s %v, got %v", label, wantCopy, gotCopy)
	}
}

func incrementalGoodRootResolution(rootPath, operationID string) openapi.RootResolution {
	return openapi.RootResolution{
		RootPath: rootPath,
		Documents: map[string][]byte{
			rootPath: []byte("openapi: 3.1.0\ninfo:\n  title: Incremental API\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: " + operationID + "\n      responses:\n        '200':\n          description: ok\n"),
		},
		DependencyFiles: []string{rootPath},
	}
}

type incrementalImpactResolver struct {
	resolvedRootByPath   map[string]openapi.RootResolution
	resolveRootErrByPath map[string]error
	discoveredRoots      []openapi.RootResolution

	resolveRootPaths []string
	discoveryCalls   [][]string
	bootstrapCalls   int
}

func (r *incrementalImpactResolver) ResolveRootOpenAPIAtSHA(
	_ context.Context,
	_ openapi.GitLabClient,
	_ int64,
	_ string,
	rootPath string,
) (openapi.RootResolution, error) {
	r.resolveRootPaths = append(r.resolveRootPaths, rootPath)
	if err, exists := r.resolveRootErrByPath[rootPath]; exists {
		return openapi.RootResolution{}, err
	}
	root, exists := r.resolvedRootByPath[rootPath]
	if !exists {
		return openapi.RootResolution{}, fmt.Errorf("unexpected ResolveRootOpenAPIAtSHA root %q", rootPath)
	}
	return root, nil
}

func (r *incrementalImpactResolver) ResolveDiscoveredRootsAtPaths(
	_ context.Context,
	_ openapi.GitLabClient,
	_ int64,
	_ string,
	paths []string,
) ([]openapi.RootResolution, error) {
	copiedPaths := make([]string, len(paths))
	copy(copiedPaths, paths)
	r.discoveryCalls = append(r.discoveryCalls, copiedPaths)

	roots := make([]openapi.RootResolution, len(r.discoveredRoots))
	copy(roots, r.discoveredRoots)
	return roots, nil
}

func (r *incrementalImpactResolver) ResolveRepositoryOpenAPIAtSHA(
	_ context.Context,
	_ openapi.GitLabBootstrapClient,
	_ int64,
	_ string,
) ([]openapi.RootResolution, error) {
	r.bootstrapCalls++
	return []openapi.RootResolution{}, nil
}

type incrementalImpactGitLabClient struct {
	changedPaths []gitlab.ChangedPath
	compareCalls int
}

func (c *incrementalImpactGitLabClient) CompareChangedPaths(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]gitlab.ChangedPath, error) {
	c.compareCalls++
	rows := make([]gitlab.ChangedPath, len(c.changedPaths))
	copy(rows, c.changedPaths)
	return rows, nil
}

func (*incrementalImpactGitLabClient) GetFileContent(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]byte, error) {
	return nil, nil
}

func (*incrementalImpactGitLabClient) ListRepositoryTree(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
	_ bool,
) ([]gitlab.TreeEntry, error) {
	return []gitlab.TreeEntry{}, nil
}

type incrementalImpactRevisionStore struct {
	repo           store.Repo
	bootstrapState store.RepoBootstrapState

	activeSpecs []store.ActiveAPISpecWithLatestDependencies

	nextAPISpecID          int64
	nextAPISpecRevisionID  int64
	rootByAPISpecID        map[int64]string
	apiSpecRevisionIDByKey map[string]int64

	upsertRoots                []string
	markDeletedIDs             []int64
	createAPISpecRevisionCalls []store.CreateAPISpecRevisionInput
	replaceDependenciesCalls   []store.ReplaceAPISpecDependenciesInput
	persistCanonicalCalls      []store.PersistCanonicalSpecInput
	persistSpecChangeCalls     []store.PersistSpecChangeInput

	finalStatusByRoot map[string]string
	finalErrorByRoot  map[string]string
	endpoints         map[int64][]store.EndpointIndexRecord
}

func newIncrementalImpactRevisionStore(
	repoID int64,
	projectID int64,
	activeSpecs []store.ActiveAPISpecWithLatestDependencies,
) *incrementalImpactRevisionStore {
	copiedActiveSpecs := make([]store.ActiveAPISpecWithLatestDependencies, len(activeSpecs))
	copy(copiedActiveSpecs, activeSpecs)

	rootBySpecID := make(map[int64]string, len(activeSpecs))
	for _, spec := range activeSpecs {
		rootBySpecID[spec.ID] = spec.RootPath
	}

	return &incrementalImpactRevisionStore{
		repo: store.Repo{
			ID:              repoID,
			GitLabProjectID: projectID,
		},
		bootstrapState: store.RepoBootstrapState{
			ActiveAPICount: int64(len(activeSpecs)),
			ForceRescan:    false,
		},
		activeSpecs:            copiedActiveSpecs,
		nextAPISpecID:          1000,
		nextAPISpecRevisionID:  5000,
		rootByAPISpecID:        rootBySpecID,
		apiSpecRevisionIDByKey: make(map[string]int64),
		finalStatusByRoot:      make(map[string]string),
		finalErrorByRoot:       make(map[string]string),
		endpoints:              make(map[int64][]store.EndpointIndexRecord),
	}
}

func (s *incrementalImpactRevisionStore) GetRepoByID(_ context.Context, repoID int64) (store.Repo, error) {
	if s.repo.ID != repoID {
		return store.Repo{}, fmt.Errorf("repo %d not found", repoID)
	}
	return s.repo, nil
}

func (s *incrementalImpactRevisionStore) GetRepoBootstrapState(
	_ context.Context,
	repoID int64,
) (store.RepoBootstrapState, error) {
	if s.repo.ID != repoID {
		return store.RepoBootstrapState{}, fmt.Errorf("repo %d not found", repoID)
	}
	return s.bootstrapState, nil
}

func (s *incrementalImpactRevisionStore) ClearRepoForceRescan(_ context.Context, repoID int64) error {
	if s.repo.ID != repoID {
		return fmt.Errorf("repo %d not found", repoID)
	}
	return nil
}

func (s *incrementalImpactRevisionStore) UpsertAPISpec(
	_ context.Context,
	input store.UpsertAPISpecInput,
) (store.APISpec, error) {
	rootPath := input.RootPath
	s.upsertRoots = append(s.upsertRoots, rootPath)

	for _, spec := range s.activeSpecs {
		if spec.RootPath == rootPath {
			return spec.APISpec, nil
		}
	}

	s.nextAPISpecID++
	spec := store.APISpec{
		ID:       s.nextAPISpecID,
		RepoID:   input.RepoID,
		RootPath: rootPath,
		Status:   "active",
	}
	s.activeSpecs = append(s.activeSpecs, store.ActiveAPISpecWithLatestDependencies{
		APISpec: spec,
		DependencyFilePaths: []string{
			rootPath,
		},
	})
	s.rootByAPISpecID[spec.ID] = rootPath
	return spec, nil
}

func (s *incrementalImpactRevisionStore) ListActiveAPISpecsWithLatestDependencies(
	_ context.Context,
	_ int64,
) ([]store.ActiveAPISpecWithLatestDependencies, error) {
	rows := make([]store.ActiveAPISpecWithLatestDependencies, len(s.activeSpecs))
	copy(rows, s.activeSpecs)
	return rows, nil
}

func (s *incrementalImpactRevisionStore) ListAPISpecListingByRepo(
	_ context.Context,
	_ int64,
) ([]store.APISpecListing, error) {
	return nil, nil
}

func (s *incrementalImpactRevisionStore) MarkAPISpecDeleted(_ context.Context, apiSpecID int64) error {
	s.markDeletedIDs = append(s.markDeletedIDs, apiSpecID)
	for i, spec := range s.activeSpecs {
		if spec.ID == apiSpecID {
			spec.Status = "deleted"
			s.activeSpecs[i] = spec
			break
		}
	}
	return nil
}

func (s *incrementalImpactRevisionStore) CreateAPISpecRevision(
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

	key := fmt.Sprintf("%d:%d", input.APISpecID, input.IngestEventID)
	apiSpecRevisionID, exists := s.apiSpecRevisionIDByKey[key]
	if !exists {
		s.nextAPISpecRevisionID++
		apiSpecRevisionID = s.nextAPISpecRevisionID
		s.apiSpecRevisionIDByKey[key] = apiSpecRevisionID
	}
	return store.APISpecRevision{
		ID:                 apiSpecRevisionID,
		APISpecID:          input.APISpecID,
		IngestEventID:      input.IngestEventID,
		RootPathAtRevision: rootPath,
		BuildStatus:        input.BuildStatus,
		Error:              input.Error,
	}, nil
}

func (s *incrementalImpactRevisionStore) ReplaceAPISpecDependencies(
	_ context.Context,
	input store.ReplaceAPISpecDependenciesInput,
) error {
	s.replaceDependenciesCalls = append(s.replaceDependenciesCalls, input)
	return nil
}

func (s *incrementalImpactRevisionStore) PersistCanonicalSpec(
	_ context.Context,
	input store.PersistCanonicalSpecInput,
) error {
	s.persistCanonicalCalls = append(s.persistCanonicalCalls, input)

	rows := make([]store.EndpointIndexRecord, len(input.Endpoints))
	copy(rows, input.Endpoints)
	s.endpoints[input.APISpecRevisionID] = rows
	return nil
}

func (s *incrementalImpactRevisionStore) ListEndpointIndexByAPISpecRevision(
	_ context.Context,
	apiSpecRevisionID int64,
) ([]store.EndpointIndexRecord, error) {
	rows, exists := s.endpoints[apiSpecRevisionID]
	if !exists {
		return nil, nil
	}
	copied := make([]store.EndpointIndexRecord, len(rows))
	copy(copied, rows)
	return copied, nil
}

func (s *incrementalImpactRevisionStore) PersistSpecChange(_ context.Context, input store.PersistSpecChangeInput) error {
	s.persistSpecChangeCalls = append(s.persistSpecChangeCalls, input)
	return nil
}

func (*incrementalImpactRevisionStore) GetTenantByID(_ context.Context, _ int64) (store.Tenant, error) {
	return store.Tenant{}, errors.New("unexpected GetTenantByID call")
}

func (*incrementalImpactRevisionStore) GetRevisionByID(_ context.Context, _ int64) (store.Revision, error) {
	return store.Revision{}, errors.New("unexpected GetRevisionByID call")
}

func (*incrementalImpactRevisionStore) GetSpecArtifactByAPISpecRevisionID(
	_ context.Context,
	_ int64,
) (store.SpecArtifact, error) {
	return store.SpecArtifact{}, errors.New("unexpected GetSpecArtifactByAPISpecRevisionID call")
}

func (*incrementalImpactRevisionStore) GetSpecChangeByAPISpecIDAndToAPISpecRevisionID(
	_ context.Context,
	_ int64,
	_ int64,
) (store.SpecChange, error) {
	return store.SpecChange{}, errors.New("unexpected GetSpecChangeByAPISpecIDAndToAPISpecRevisionID call")
}
