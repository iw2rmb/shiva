package main

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/iw2rmb/shiva/internal/gitlab"
	"github.com/iw2rmb/shiva/internal/openapi"
	"github.com/iw2rmb/shiva/internal/store"
	"github.com/iw2rmb/shiva/internal/worker"
)

func TestRevisionProcessorProcess_ModeSelectionMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		parentSHA      string
		activeAPICount int64
		forceRescan    bool
		wantMode       ingestionMode
	}{
		{
			name:           "parent empty active zero force false selects bootstrap",
			parentSHA:      "",
			activeAPICount: 0,
			forceRescan:    false,
			wantMode:       ingestionModeBootstrap,
		},
		{
			name:           "parent empty active zero force true selects bootstrap",
			parentSHA:      "",
			activeAPICount: 0,
			forceRescan:    true,
			wantMode:       ingestionModeBootstrap,
		},
		{
			name:           "parent empty active non zero force false selects incremental",
			parentSHA:      "",
			activeAPICount: 2,
			forceRescan:    false,
			wantMode:       ingestionModeIncremental,
		},
		{
			name:           "parent empty active non zero force true selects bootstrap",
			parentSHA:      "",
			activeAPICount: 2,
			forceRescan:    true,
			wantMode:       ingestionModeBootstrap,
		},
		{
			name:           "parent present active zero force false selects bootstrap",
			parentSHA:      "1111111111111111111111111111111111111111",
			activeAPICount: 0,
			forceRescan:    false,
			wantMode:       ingestionModeBootstrap,
		},
		{
			name:           "parent present active zero force true selects bootstrap",
			parentSHA:      "1111111111111111111111111111111111111111",
			activeAPICount: 0,
			forceRescan:    true,
			wantMode:       ingestionModeBootstrap,
		},
		{
			name:           "parent present active non zero force false selects incremental",
			parentSHA:      "1111111111111111111111111111111111111111",
			activeAPICount: 2,
			forceRescan:    false,
			wantMode:       ingestionModeIncremental,
		},
		{
			name:           "parent present active non zero force true selects bootstrap",
			parentSHA:      "1111111111111111111111111111111111111111",
			activeAPICount: 2,
			forceRescan:    true,
			wantMode:       ingestionModeBootstrap,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repoID := int64(77)
			projectID := int64(987)
			revisionID := int64(1234)

			storeFake := &modeSelectionRevisionStore{
				repo: store.Repo{
					ID:              repoID,
					GitLabProjectID: projectID,
				},
				bootstrapState: store.RepoBootstrapState{
					ActiveAPICount: tc.activeAPICount,
					ForceRescan:    tc.forceRescan,
				},
				revisionID: revisionID,
			}
			resolverFake := &modeSelectionResolver{}

			processor := revisionProcessor{
				store:         storeFake,
				gitlabClient:  modeSelectionGitLabClient{},
				openapiLoader: resolverFake,
			}
			job := worker.QueueJob{
				EventID:    10,
				RepoID:     repoID,
				DeliveryID: "delivery-1",
				Sha:        "2222222222222222222222222222222222222222",
				Branch:     "main",
				ParentSha:  tc.parentSHA,
			}

			err := processor.Process(context.Background(), job)
			if err != nil {
				t.Fatalf("Process() unexpected error: %v", err)
			}

			if storeFake.markProcessedCalls != 1 {
				t.Fatalf("expected exactly one MarkRevisionProcessed call, got %d", storeFake.markProcessedCalls)
			}
			if storeFake.markProcessedRevisionID != revisionID {
				t.Fatalf("expected revision id %d, got %d", revisionID, storeFake.markProcessedRevisionID)
			}
			if storeFake.markProcessedOpenAPIChanged {
				t.Fatal("expected openapi_changed=false in mode routing test")
			}

			switch tc.wantMode {
			case ingestionModeBootstrap:
				if len(resolverFake.bootstrapCalls) != 1 {
					t.Fatalf("expected one bootstrap resolver call, got %d", len(resolverFake.bootstrapCalls))
				}
				if len(resolverFake.incrementalCalls) != 0 {
					t.Fatalf("expected zero incremental resolver calls, got %d", len(resolverFake.incrementalCalls))
				}
				call := resolverFake.bootstrapCalls[0]
				if call.projectID != projectID {
					t.Fatalf("expected bootstrap project id %d, got %d", projectID, call.projectID)
				}
				if call.sha != job.Sha {
					t.Fatalf("expected bootstrap sha=%q, got %q", job.Sha, call.sha)
				}
			case ingestionModeIncremental:
				if len(resolverFake.incrementalCalls) != 1 {
					t.Fatalf("expected one incremental resolver call, got %d", len(resolverFake.incrementalCalls))
				}
				if len(resolverFake.bootstrapCalls) != 0 {
					t.Fatalf("expected zero bootstrap resolver calls, got %d", len(resolverFake.bootstrapCalls))
				}
				call := resolverFake.incrementalCalls[0]
				if call.projectID != projectID {
					t.Fatalf("expected incremental project id %d, got %d", projectID, call.projectID)
				}
				if call.fromSHA != tc.parentSHA {
					t.Fatalf("expected incremental from sha=%q, got %q", tc.parentSHA, call.fromSHA)
				}
				if call.toSHA != job.Sha {
					t.Fatalf("expected incremental to sha=%q, got %q", job.Sha, call.toSHA)
				}
			default:
				t.Fatalf("unexpected mode %q", tc.wantMode)
			}
		})
	}
}

type incrementalCall struct {
	projectID int64
	fromSHA   string
	toSHA     string
}

type bootstrapCall struct {
	projectID int64
	sha       string
}

type modeSelectionResolver struct {
	incrementalCalls []incrementalCall
	bootstrapCalls   []bootstrapCall
}

func (r *modeSelectionResolver) ResolveChangedOpenAPI(
	_ context.Context,
	_ openapi.GitLabClient,
	projectID int64,
	fromSHA string,
	toSHA string,
) (openapi.ResolutionResult, error) {
	r.incrementalCalls = append(r.incrementalCalls, incrementalCall{
		projectID: projectID,
		fromSHA:   fromSHA,
		toSHA:     toSHA,
	})
	return openapi.ResolutionResult{
		OpenAPIChanged: false,
		CandidateFiles: []string{},
		Documents:      map[string][]byte{},
	}, nil
}

func (r *modeSelectionResolver) ResolveRepositoryOpenAPIAtSHA(
	_ context.Context,
	_ openapi.GitLabBootstrapClient,
	projectID int64,
	sha string,
) ([]openapi.RootResolution, error) {
	r.bootstrapCalls = append(r.bootstrapCalls, bootstrapCall{
		projectID: projectID,
		sha:       sha,
	})
	return []openapi.RootResolution{}, nil
}

type modeSelectionGitLabClient struct{}

func (modeSelectionGitLabClient) CompareChangedPaths(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]gitlab.ChangedPath, error) {
	return []gitlab.ChangedPath{}, nil
}

func (modeSelectionGitLabClient) GetFileContent(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]byte, error) {
	return nil, nil
}

func (modeSelectionGitLabClient) ListRepositoryTree(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
	_ bool,
) ([]gitlab.TreeEntry, error) {
	return []gitlab.TreeEntry{}, nil
}

type modeSelectionRevisionStore struct {
	repo           store.Repo
	bootstrapState store.RepoBootstrapState
	revisionID     int64

	markProcessedCalls          int
	markProcessedRevisionID     int64
	markProcessedOpenAPIChanged bool
}

func (s *modeSelectionRevisionStore) UpsertRevisionFromIngestEvent(
	_ context.Context,
	_ store.IngestQueueEvent,
) (int64, error) {
	return s.revisionID, nil
}

func (s *modeSelectionRevisionStore) MarkRevisionProcessed(
	_ context.Context,
	revisionID int64,
	openapiChanged bool,
) error {
	s.markProcessedCalls++
	s.markProcessedRevisionID = revisionID
	s.markProcessedOpenAPIChanged = openapiChanged
	return nil
}

func (s *modeSelectionRevisionStore) MarkRevisionFailed(_ context.Context, revisionID int64, _ string) error {
	return fmt.Errorf("unexpected MarkRevisionFailed call for revision %d", revisionID)
}

func (s *modeSelectionRevisionStore) GetRepoByID(_ context.Context, repoID int64) (store.Repo, error) {
	if s.repo.ID != repoID {
		return store.Repo{}, fmt.Errorf("repo %d not found", repoID)
	}
	return s.repo, nil
}

func (s *modeSelectionRevisionStore) GetRepoBootstrapState(
	_ context.Context,
	repoID int64,
) (store.RepoBootstrapState, error) {
	if s.repo.ID != repoID {
		return store.RepoBootstrapState{}, fmt.Errorf("repo %d not found", repoID)
	}
	return s.bootstrapState, nil
}

func (s *modeSelectionRevisionStore) PersistCanonicalSpec(_ context.Context, _ store.PersistCanonicalSpecInput) error {
	return errors.New("unexpected PersistCanonicalSpec call")
}

func (s *modeSelectionRevisionStore) GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
	_ context.Context,
	_ int64,
	_ string,
	_ int64,
) (store.Revision, bool, error) {
	return store.Revision{}, false, errors.New("unexpected GetLatestProcessedOpenAPIRevisionByBranchExcludingID call")
}

func (s *modeSelectionRevisionStore) ListEndpointIndexByRevision(
	_ context.Context,
	_ int64,
) ([]store.EndpointIndexRecord, error) {
	return nil, errors.New("unexpected ListEndpointIndexByRevision call")
}

func (s *modeSelectionRevisionStore) PersistSpecChange(_ context.Context, _ store.PersistSpecChangeInput) error {
	return errors.New("unexpected PersistSpecChange call")
}

func (s *modeSelectionRevisionStore) GetTenantByID(_ context.Context, _ int64) (store.Tenant, error) {
	return store.Tenant{}, errors.New("unexpected GetTenantByID call")
}

func (s *modeSelectionRevisionStore) GetRevisionByID(_ context.Context, _ int64) (store.Revision, error) {
	return store.Revision{}, errors.New("unexpected GetRevisionByID call")
}

func (s *modeSelectionRevisionStore) GetSpecArtifactByRevisionID(_ context.Context, _ int64) (store.SpecArtifact, error) {
	return store.SpecArtifact{}, errors.New("unexpected GetSpecArtifactByRevisionID call")
}

func (s *modeSelectionRevisionStore) GetSpecChangeByToRevision(_ context.Context, _ int64) (store.SpecChange, error) {
	return store.SpecChange{}, errors.New("unexpected GetSpecChangeByToRevision call")
}
