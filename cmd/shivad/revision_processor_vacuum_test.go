package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/iw2rmb/shiva/internal/gitlab"
	"github.com/iw2rmb/shiva/internal/openapi"
	"github.com/iw2rmb/shiva/internal/openapi/lint"
	"github.com/iw2rmb/shiva/internal/store"
	"github.com/iw2rmb/shiva/internal/worker"
)

func TestRevisionProcessorProcess_PersistsVacuumState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		lintResult         lint.CanonicalExecutionResult
		wantIssues         []store.VacuumIssueMutation
		wantVacuumStatus   string
		wantVacuumError    string
		wantValidatedAtSet bool
	}{
		{
			name: "successful lint persists normalized issues",
			lintResult: lint.CanonicalExecutionResult{
				Issues: []lint.CanonicalIssue{
					{
						RuleID:   "info-description",
						Message:  "missing description",
						JSONPath: "$.info",
						RangePos: [4]int32{1, 2, 3, 4},
					},
					{
						RuleID:   "paths-kebab-case",
						Message:  "bad path",
						JSONPath: "$.paths['/Bad_Path']",
						RangePos: [4]int32{6, 3, 6, 12},
					},
				},
			},
			wantIssues: []store.VacuumIssueMutation{
				{
					RuleID:   "info-description",
					Message:  "missing description",
					JSONPath: "$.info",
					RangePos: []int32{1, 2, 3, 4},
				},
				{
					RuleID:   "paths-kebab-case",
					Message:  "bad path",
					JSONPath: "$.paths['/Bad_Path']",
					RangePos: []int32{6, 3, 6, 12},
				},
			},
			wantVacuumStatus:   store.VacuumStatusProcessed,
			wantValidatedAtSet: true,
		},
		{
			name: "zero issue lint keeps revision clean",
			lintResult: lint.CanonicalExecutionResult{
				Issues: []lint.CanonicalIssue{},
			},
			wantIssues:         []store.VacuumIssueMutation{},
			wantVacuumStatus:   store.VacuumStatusProcessed,
			wantValidatedAtSet: true,
		},
		{
			name: "normalized vacuum failure marks vacuum failed",
			lintResult: lint.CanonicalExecutionResult{
				Failure: &lint.CanonicalFailure{Message: "spec type not supported by libopenapi, sorry"},
			},
			wantIssues:         []store.VacuumIssueMutation{},
			wantVacuumStatus:   store.VacuumStatusFailed,
			wantVacuumError:    "spec type not supported by libopenapi, sorry",
			wantValidatedAtSet: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			storeFake := newVacuumProcessorStore()
			processor := revisionProcessor{
				store:           storeFake,
				gitlabClient:    vacuumProcessorGitLabClient{},
				openapiLoader:   &vacuumProcessorResolver{},
				canonicalLinter: vacuumProcessorLinter{result: tc.lintResult},
			}

			result, err := processor.Process(context.Background(), worker.QueueJob{
				EventID:    501,
				RepoID:     storeFake.repo.ID,
				DeliveryID: "delivery-501",
				Sha:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Branch:     "main",
				ParentSha:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			})
			if err != nil {
				t.Fatalf("Process() unexpected error: %v", err)
			}
			if !result.OpenAPIChanged {
				t.Fatal("expected openapi_changed=true")
			}
			if len(storeFake.persistCanonicalCalls) != 1 {
				t.Fatalf("expected one canonical persistence call, got %d", len(storeFake.persistCanonicalCalls))
			}
			if len(storeFake.vacuumProcessingCalls) != 1 {
				t.Fatalf("expected one vacuum processing state update, got %d", len(storeFake.vacuumProcessingCalls))
			}
			if got := storeFake.vacuumProcessingCalls[0].VacuumStatus; got != store.VacuumStatusProcessing {
				t.Fatalf("expected intermediate vacuum status=%q, got %q", store.VacuumStatusProcessing, got)
			}
			if len(storeFake.vacuumResultCalls) != 1 {
				t.Fatalf("expected one final vacuum persistence call, got %d", len(storeFake.vacuumResultCalls))
			}

			finalRevision := storeFake.lastRevision()
			if finalRevision.BuildStatus != apiSpecRevisionBuildStatusProcessed {
				t.Fatalf("expected build_status=%q, got %q", apiSpecRevisionBuildStatusProcessed, finalRevision.BuildStatus)
			}
			if finalRevision.VacuumStatus != tc.wantVacuumStatus {
				t.Fatalf("expected vacuum_status=%q, got %q", tc.wantVacuumStatus, finalRevision.VacuumStatus)
			}
			if finalRevision.VacuumError != tc.wantVacuumError {
				t.Fatalf("expected vacuum_error=%q, got %q", tc.wantVacuumError, finalRevision.VacuumError)
			}
			if got := finalRevision.VacuumValidatedAt != nil; got != tc.wantValidatedAtSet {
				t.Fatalf("expected validated_at set=%t, got %+v", tc.wantValidatedAtSet, finalRevision.VacuumValidatedAt)
			}
			if got := storeFake.persistedVacuumIssues[finalRevision.ID]; !vacuumIssuesEqual(got, tc.wantIssues) {
				t.Fatalf("expected persisted vacuum issues %+v, got %+v", tc.wantIssues, got)
			}
		})
	}
}

type vacuumProcessorResolver struct{}

func (*vacuumProcessorResolver) ResolveRootOpenAPIAtSHA(
	_ context.Context,
	_ openapi.GitLabClient,
	_ int64,
	_ string,
	_ string,
) (openapi.RootResolution, error) {
	return openapi.RootResolution{}, fmt.Errorf("unexpected ResolveRootOpenAPIAtSHA call")
}

func (*vacuumProcessorResolver) ResolveDiscoveredRootsAtPaths(
	_ context.Context,
	_ openapi.GitLabClient,
	_ int64,
	_ string,
	_ []string,
) ([]openapi.RootResolution, error) {
	return nil, fmt.Errorf("unexpected ResolveDiscoveredRootsAtPaths call")
}

func (*vacuumProcessorResolver) ResolveRepositoryOpenAPIAtSHA(
	_ context.Context,
	_ openapi.GitLabBootstrapClient,
	_ int64,
	_ string,
) ([]openapi.RootResolution, error) {
	return []openapi.RootResolution{
		{
			RootPath: "apis/pets/openapi.yaml",
			Documents: map[string][]byte{
				"apis/pets/openapi.yaml": []byte("openapi: 3.1.0\ninfo:\n  title: Pets\n  version: 1.0.0\npaths:\n  /pets:\n    get:\n      operationId: listPets\n      responses:\n        '200':\n          description: ok\n"),
			},
			DependencyFiles: []string{"apis/pets/openapi.yaml"},
		},
	}, nil
}

type vacuumProcessorGitLabClient struct{}

func (vacuumProcessorGitLabClient) CompareChangedPaths(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]gitlab.ChangedPath, error) {
	return nil, fmt.Errorf("unexpected CompareChangedPaths call")
}

func (vacuumProcessorGitLabClient) GetFileContent(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]byte, error) {
	return nil, fmt.Errorf("unexpected GetFileContent call")
}

func (vacuumProcessorGitLabClient) ListRepositoryTree(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
	_ bool,
) ([]gitlab.TreeEntry, error) {
	return nil, fmt.Errorf("unexpected ListRepositoryTree call")
}

type vacuumProcessorLinter struct {
	result lint.CanonicalExecutionResult
	err    error
}

func (l vacuumProcessorLinter) RunCanonicalSpec(
	_ context.Context,
	_ string,
) (lint.CanonicalExecutionResult, error) {
	return l.result, l.err
}

type vacuumProcessorStore struct {
	repo store.Repo

	nextAPISpecID         int64
	nextAPISpecRevisionID int64
	apiSpecIDByRoot       map[string]int64
	rootByAPISpecID       map[int64]string
	revisions             map[int64]store.APISpecRevision
	revisionOrder         []int64
	endpoints             map[int64][]store.EndpointIndexRecord

	persistCanonicalCalls []store.PersistCanonicalSpecInput
	vacuumProcessingCalls []store.UpdateAPISpecRevisionVacuumStateInput
	vacuumResultCalls     []store.PersistAPISpecRevisionVacuumResultInput
	persistedVacuumIssues map[int64][]store.VacuumIssueMutation
}

func newVacuumProcessorStore() *vacuumProcessorStore {
	return &vacuumProcessorStore{
		repo: store.Repo{
			ID:              91,
			GitLabProjectID: 9001,
		},
		nextAPISpecID:         100,
		nextAPISpecRevisionID: 500,
		apiSpecIDByRoot:       make(map[string]int64),
		rootByAPISpecID:       make(map[int64]string),
		revisions:             make(map[int64]store.APISpecRevision),
		endpoints:             make(map[int64][]store.EndpointIndexRecord),
		persistedVacuumIssues: make(map[int64][]store.VacuumIssueMutation),
	}
}

func (s *vacuumProcessorStore) GetRepoByID(_ context.Context, repoID int64) (store.Repo, error) {
	if repoID != s.repo.ID {
		return store.Repo{}, fmt.Errorf("repo %d not found", repoID)
	}
	return s.repo, nil
}

func (*vacuumProcessorStore) GetRepoBootstrapState(_ context.Context, _ int64) (store.RepoBootstrapState, error) {
	return store.RepoBootstrapState{ActiveAPICount: 0, ForceRescan: true}, nil
}

func (*vacuumProcessorStore) ClearRepoForceRescan(_ context.Context, _ int64) error {
	return nil
}

func (s *vacuumProcessorStore) UpsertAPISpec(_ context.Context, input store.UpsertAPISpecInput) (store.APISpec, error) {
	apiSpecID, exists := s.apiSpecIDByRoot[input.RootPath]
	if !exists {
		s.nextAPISpecID++
		apiSpecID = s.nextAPISpecID
		s.apiSpecIDByRoot[input.RootPath] = apiSpecID
		s.rootByAPISpecID[apiSpecID] = input.RootPath
	}
	return store.APISpec{
		ID:       apiSpecID,
		RepoID:   input.RepoID,
		RootPath: input.RootPath,
		Status:   "active",
	}, nil
}

func (*vacuumProcessorStore) ListAPISpecListingByRepo(_ context.Context, _ int64) ([]store.APISpecListing, error) {
	return nil, nil
}

func (*vacuumProcessorStore) ListActiveAPISpecsWithLatestDependencies(
	_ context.Context,
	_ int64,
) ([]store.ActiveAPISpecWithLatestDependencies, error) {
	return nil, fmt.Errorf("unexpected ListActiveAPISpecsWithLatestDependencies call")
}

func (*vacuumProcessorStore) MarkAPISpecDeleted(_ context.Context, _ int64) error {
	return fmt.Errorf("unexpected MarkAPISpecDeleted call")
}

func (s *vacuumProcessorStore) CreateAPISpecRevision(
	_ context.Context,
	input store.CreateAPISpecRevisionInput,
) (store.APISpecRevision, error) {
	rootPath := s.rootByAPISpecID[input.APISpecID]
	for id, revision := range s.revisions {
		if revision.APISpecID == input.APISpecID && revision.IngestEventID == input.IngestEventID {
			revision.BuildStatus = input.BuildStatus
			revision.Error = input.Error
			s.revisions[id] = revision
			return revision, nil
		}
	}

	s.nextAPISpecRevisionID++
	revision := store.APISpecRevision{
		ID:                 s.nextAPISpecRevisionID,
		APISpecID:          input.APISpecID,
		IngestEventID:      input.IngestEventID,
		RootPathAtRevision: rootPath,
		BuildStatus:        input.BuildStatus,
		Error:              input.Error,
		VacuumStatus:       store.VacuumStatusPending,
	}
	s.revisions[revision.ID] = revision
	s.revisionOrder = append(s.revisionOrder, revision.ID)
	return revision, nil
}

func (*vacuumProcessorStore) ReplaceAPISpecDependencies(
	_ context.Context,
	_ store.ReplaceAPISpecDependenciesInput,
) error {
	return nil
}

func (s *vacuumProcessorStore) PersistCanonicalSpec(
	_ context.Context,
	input store.PersistCanonicalSpecInput,
) error {
	s.persistCanonicalCalls = append(s.persistCanonicalCalls, input)
	rows := make([]store.EndpointIndexRecord, len(input.Endpoints))
	copy(rows, input.Endpoints)
	s.endpoints[input.APISpecRevisionID] = rows
	return nil
}

func (s *vacuumProcessorStore) UpdateAPISpecRevisionVacuumState(
	_ context.Context,
	input store.UpdateAPISpecRevisionVacuumStateInput,
) (store.APISpecRevision, error) {
	s.vacuumProcessingCalls = append(s.vacuumProcessingCalls, input)
	revision := s.revisions[input.APISpecRevisionID]
	revision.VacuumStatus = input.VacuumStatus
	revision.VacuumError = input.VacuumError
	revision.VacuumValidatedAt = input.VacuumValidatedAt
	s.revisions[input.APISpecRevisionID] = revision
	return revision, nil
}

func (s *vacuumProcessorStore) PersistAPISpecRevisionVacuumResult(
	_ context.Context,
	input store.PersistAPISpecRevisionVacuumResultInput,
) (store.APISpecRevision, error) {
	s.vacuumResultCalls = append(s.vacuumResultCalls, input)

	issues := make([]store.VacuumIssueMutation, len(input.Issues))
	copy(issues, input.Issues)
	s.persistedVacuumIssues[input.APISpecRevisionID] = issues

	revision := s.revisions[input.APISpecRevisionID]
	revision.VacuumStatus = input.VacuumStatus
	revision.VacuumError = input.VacuumError
	revision.VacuumValidatedAt = input.VacuumValidatedAt
	s.revisions[input.APISpecRevisionID] = revision
	return revision, nil
}

func (s *vacuumProcessorStore) ListEndpointIndexByAPISpecRevision(
	_ context.Context,
	apiSpecRevisionID int64,
) ([]store.EndpointIndexRecord, error) {
	rows := s.endpoints[apiSpecRevisionID]
	copied := make([]store.EndpointIndexRecord, len(rows))
	copy(copied, rows)
	return copied, nil
}

func (*vacuumProcessorStore) PersistSpecChange(_ context.Context, _ store.PersistSpecChangeInput) error {
	return nil
}

func (*vacuumProcessorStore) GetRevisionByID(_ context.Context, _ int64) (store.Revision, error) {
	return store.Revision{}, fmt.Errorf("unexpected GetRevisionByID call")
}

func (*vacuumProcessorStore) GetSpecArtifactByAPISpecRevisionID(
	_ context.Context,
	_ int64,
) (store.SpecArtifact, error) {
	return store.SpecArtifact{}, fmt.Errorf("unexpected GetSpecArtifactByAPISpecRevisionID call")
}

func (*vacuumProcessorStore) GetSpecChangeByAPISpecIDAndToAPISpecRevisionID(
	_ context.Context,
	_ int64,
	_ int64,
) (store.SpecChange, error) {
	return store.SpecChange{}, fmt.Errorf("unexpected GetSpecChangeByAPISpecIDAndToAPISpecRevisionID call")
}

func (s *vacuumProcessorStore) lastRevision() store.APISpecRevision {
	return s.revisions[s.revisionOrder[len(s.revisionOrder)-1]]
}

func vacuumIssuesEqual(left []store.VacuumIssueMutation, right []store.VacuumIssueMutation) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if left[idx].RuleID != right[idx].RuleID {
			return false
		}
		if left[idx].Message != right[idx].Message {
			return false
		}
		if left[idx].JSONPath != right[idx].JSONPath {
			return false
		}
		if len(left[idx].RangePos) != len(right[idx].RangePos) {
			return false
		}
		for rangeIdx := range left[idx].RangePos {
			if left[idx].RangePos[rangeIdx] != right[idx].RangePos[rangeIdx] {
				return false
			}
		}
	}
	return true
}
