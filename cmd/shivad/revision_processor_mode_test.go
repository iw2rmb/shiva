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
			gitlabClient := &modeSelectionGitLabClient{}

			processor := revisionProcessor{
				store:         storeFake,
				gitlabClient:  gitlabClient,
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
				if len(gitlabClient.compareCalls) != 0 {
					t.Fatalf("expected zero compare calls, got %d", len(gitlabClient.compareCalls))
				}
				call := resolverFake.bootstrapCalls[0]
				if call.projectID != projectID {
					t.Fatalf("expected bootstrap project id %d, got %d", projectID, call.projectID)
				}
				if call.sha != job.Sha {
					t.Fatalf("expected bootstrap sha=%q, got %q", job.Sha, call.sha)
				}
			case ingestionModeIncremental:
				if len(gitlabClient.compareCalls) != 1 {
					t.Fatalf("expected one compare call, got %d", len(gitlabClient.compareCalls))
				}
				if len(resolverFake.bootstrapCalls) != 0 {
					t.Fatalf("expected zero bootstrap resolver calls, got %d", len(resolverFake.bootstrapCalls))
				}
				call := gitlabClient.compareCalls[0]
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
	bootstrapCalls []bootstrapCall
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

func (modeSelectionResolver) ResolveRootOpenAPIAtSHA(
	_ context.Context,
	_ openapi.GitLabClient,
	_ int64,
	_ string,
	_ string,
) (openapi.RootResolution, error) {
	return openapi.RootResolution{}, errors.New("unexpected ResolveRootOpenAPIAtSHA call")
}

func (modeSelectionResolver) ResolveDiscoveredRootsAtPaths(
	_ context.Context,
	_ openapi.GitLabClient,
	_ int64,
	_ string,
	_ []string,
) ([]openapi.RootResolution, error) {
	return nil, errors.New("unexpected ResolveDiscoveredRootsAtPaths call")
}

type modeSelectionGitLabClient struct {
	compareCalls []incrementalCall
}

func (c *modeSelectionGitLabClient) CompareChangedPaths(
	_ context.Context,
	projectID int64,
	fromSHA string,
	toSHA string,
) ([]gitlab.ChangedPath, error) {
	c.compareCalls = append(c.compareCalls, incrementalCall{
		projectID: projectID,
		fromSHA:   fromSHA,
		toSHA:     toSHA,
	})
	return []gitlab.ChangedPath{}, nil
}

func (*modeSelectionGitLabClient) GetFileContent(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]byte, error) {
	return nil, nil
}

func (*modeSelectionGitLabClient) ListRepositoryTree(
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

func (s *modeSelectionRevisionStore) ClearRepoForceRescan(_ context.Context, repoID int64) error {
	if s.repo.ID != repoID {
		return fmt.Errorf("repo %d not found", repoID)
	}
	return nil
}

func (s *modeSelectionRevisionStore) UpsertAPISpec(_ context.Context, _ store.UpsertAPISpecInput) (store.APISpec, error) {
	return store.APISpec{}, errors.New("unexpected UpsertAPISpec call")
}

func (s *modeSelectionRevisionStore) ListActiveAPISpecsWithLatestDependencies(
	_ context.Context,
	_ int64,
) ([]store.ActiveAPISpecWithLatestDependencies, error) {
	return []store.ActiveAPISpecWithLatestDependencies{}, nil
}

func (s *modeSelectionRevisionStore) ListAPISpecListingByRepo(
	_ context.Context,
	_ int64,
) ([]store.APISpecListing, error) {
	return nil, nil
}

func (s *modeSelectionRevisionStore) MarkAPISpecDeleted(_ context.Context, _ int64) error {
	return errors.New("unexpected MarkAPISpecDeleted call")
}

func (s *modeSelectionRevisionStore) CreateAPISpecRevision(
	_ context.Context,
	_ store.CreateAPISpecRevisionInput,
) (store.APISpecRevision, error) {
	return store.APISpecRevision{}, errors.New("unexpected CreateAPISpecRevision call")
}

func (s *modeSelectionRevisionStore) ReplaceAPISpecDependencies(
	_ context.Context,
	_ store.ReplaceAPISpecDependenciesInput,
) error {
	return errors.New("unexpected ReplaceAPISpecDependencies call")
}

func (s *modeSelectionRevisionStore) PersistCanonicalSpec(_ context.Context, _ store.PersistCanonicalSpecInput) error {
	return errors.New("unexpected PersistCanonicalSpec call")
}

func (s *modeSelectionRevisionStore) ListEndpointIndexByAPISpecRevision(
	_ context.Context,
	_ int64,
) ([]store.EndpointIndexRecord, error) {
	return nil, errors.New("unexpected ListEndpointIndexByAPISpecRevision call")
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

func (s *modeSelectionRevisionStore) GetSpecArtifactByAPISpecRevisionID(
	_ context.Context,
	_ int64,
) (store.SpecArtifact, error) {
	return store.SpecArtifact{}, errors.New("unexpected GetSpecArtifactByAPISpecRevisionID call")
}

func (s *modeSelectionRevisionStore) GetSpecChangeByAPISpecIDAndToAPISpecRevisionID(
	_ context.Context,
	_ int64,
	_ int64,
) (store.SpecChange, error) {
	return store.SpecChange{}, errors.New("unexpected GetSpecChangeByAPISpecIDAndToAPISpecRevisionID call")
}
