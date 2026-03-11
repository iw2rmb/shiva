package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/gitlab"
	"github.com/iw2rmb/shiva/internal/store"
)

func TestEnqueueStartupIndexingIfEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		revisionCount          int64
		projects               []gitlab.Project
		branches               map[string]startupIndexBranchResult
		listProjectsErr        error
		persistErr             error
		wantErr                string
		wantVisitProjectsCalls int
		wantGetBranchCalls     []startupIndexBranchCall
		wantPersistProjectIDs  []int64
	}{
		{
			name:                   "existing revisions skip startup indexing",
			revisionCount:          3,
			wantVisitProjectsCalls: 0,
		},
		{
			name:          "empty revision history enqueues discovered projects",
			revisionCount: 0,
			projects: []gitlab.Project{
				{ID: 11, PathWithNamespace: "group/service-a", DefaultBranch: "main", NamespaceKind: "group"},
				{ID: 12, PathWithNamespace: "group/service-b", DefaultBranch: "develop", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"11:main":    {branch: gitlab.Branch{Name: "main", CommitID: "aaa111"}},
				"12:develop": {branch: gitlab.Branch{Name: "develop", CommitID: "bbb222"}},
			},
			wantVisitProjectsCalls: 1,
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 11, branch: "main"},
				{projectID: 12, branch: "develop"},
			},
			wantPersistProjectIDs: []int64{11, 12},
		},
		{
			name:          "skips personal namespace projects by default",
			revisionCount: 0,
			projects: []gitlab.Project{
				{ID: 18, PathWithNamespace: "alex/personal-repo", DefaultBranch: "main", NamespaceKind: "user"},
				{ID: 19, PathWithNamespace: "group/shared-repo", DefaultBranch: "main", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"19:main": {branch: gitlab.Branch{Name: "main", CommitID: "ggg999"}},
			},
			wantVisitProjectsCalls: 1,
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 19, branch: "main"},
			},
			wantPersistProjectIDs: []int64{19},
		},
		{
			name:          "skips projects without usable default branch head",
			revisionCount: 0,
			projects: []gitlab.Project{
				{ID: 21, PathWithNamespace: "group/no-default", DefaultBranch: "", NamespaceKind: "group"},
				{ID: 22, PathWithNamespace: "group/missing-branch", DefaultBranch: "main", NamespaceKind: "group"},
				{ID: 23, PathWithNamespace: "group/empty-head", DefaultBranch: "main", NamespaceKind: "group"},
				{ID: 24, PathWithNamespace: "group/ok", DefaultBranch: "main", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"22:main": {err: gitlab.ErrNotFound},
				"23:main": {branch: gitlab.Branch{Name: "main", CommitID: ""}},
				"24:main": {branch: gitlab.Branch{Name: "main", CommitID: "ccc333"}},
			},
			wantVisitProjectsCalls: 1,
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 22, branch: "main"},
				{projectID: 23, branch: "main"},
				{projectID: 24, branch: "main"},
			},
			wantPersistProjectIDs: []int64{24},
		},
		{
			name:                   "list projects error fails startup indexing",
			revisionCount:          0,
			listProjectsErr:        errors.New("gitlab down"),
			wantErr:                "list gitlab projects for startup indexing",
			wantVisitProjectsCalls: 1,
		},
		{
			name:          "branch load error fails startup indexing",
			revisionCount: 0,
			projects: []gitlab.Project{
				{ID: 31, PathWithNamespace: "group/service-a", DefaultBranch: "main", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"31:main": {err: errors.New("boom")},
			},
			wantErr:                "resolve startup indexing branch head",
			wantVisitProjectsCalls: 1,
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 31, branch: "main"},
			},
		},
		{
			name:          "persist error fails startup indexing",
			revisionCount: 0,
			projects: []gitlab.Project{
				{ID: 41, PathWithNamespace: "group/service-a", DefaultBranch: "main", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"41:main": {branch: gitlab.Branch{Name: "main", CommitID: "ddd444"}},
			},
			persistErr:             errors.New("db write failed"),
			wantErr:                "enqueue startup indexing event",
			wantVisitProjectsCalls: 1,
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 41, branch: "main"},
			},
			wantPersistProjectIDs: []int64{41},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			storeFake := &fakeStartupIndexingStore{
				revisionCount: tc.revisionCount,
				persistErr:    tc.persistErr,
			}
			clientFake := &fakeStartupIndexingGitLabClient{
				projects:        tc.projects,
				branches:        tc.branches,
				listProjectsErr: tc.listProjectsErr,
			}

			err := enqueueStartupIndexingIfEmpty(
				context.Background(),
				config.Config{},
				slog.Default(),
				storeFake,
				clientFake,
			)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
				}
			} else if err != nil {
				t.Fatalf("enqueueStartupIndexingIfEmpty() unexpected error: %v", err)
			}

			if clientFake.visitProjectsCalls != tc.wantVisitProjectsCalls {
				t.Fatalf("expected visitProjectsCalls=%d, got %d", tc.wantVisitProjectsCalls, clientFake.visitProjectsCalls)
			}
			if len(clientFake.getBranchCalls) != len(tc.wantGetBranchCalls) {
				t.Fatalf("expected getBranchCalls=%v, got %v", tc.wantGetBranchCalls, clientFake.getBranchCalls)
			}
			for idx, wantCall := range tc.wantGetBranchCalls {
				if clientFake.getBranchCalls[idx] != wantCall {
					t.Fatalf("expected getBranch call %d to be %+v, got %+v", idx, wantCall, clientFake.getBranchCalls[idx])
				}
			}

			gotProjectIDs := make([]int64, 0, len(storeFake.persistInputs))
			for _, input := range storeFake.persistInputs {
				gotProjectIDs = append(gotProjectIDs, input.GitLabProjectID)
				if input.EventType != startupIndexingEventType {
					t.Fatalf("expected event type %q, got %q", startupIndexingEventType, input.EventType)
				}
				if input.ParentSha != "" {
					t.Fatalf("expected empty parent sha, got %q", input.ParentSha)
				}
				expectedDeliveryID := startupIndexingDeliveryID(input.GitLabProjectID, input.Sha)
				if input.DeliveryID != expectedDeliveryID {
					t.Fatalf("expected delivery id %q, got %q", expectedDeliveryID, input.DeliveryID)
				}

				var payload startupIndexingPayload
				if err := json.Unmarshal(input.PayloadJSON, &payload); err != nil {
					t.Fatalf("failed to decode payload: %v", err)
				}
				if payload.Source != "startup_indexer" {
					t.Fatalf("expected payload source startup_indexer, got %q", payload.Source)
				}
				if payload.GitLabProjectID != input.GitLabProjectID {
					t.Fatalf("expected payload project id %d, got %d", input.GitLabProjectID, payload.GitLabProjectID)
				}
				if payload.Sha != input.Sha {
					t.Fatalf("expected payload sha %q, got %q", input.Sha, payload.Sha)
				}
			}

			if len(gotProjectIDs) != len(tc.wantPersistProjectIDs) {
				t.Fatalf("expected persist project ids %v, got %v", tc.wantPersistProjectIDs, gotProjectIDs)
			}
			for idx, wantProjectID := range tc.wantPersistProjectIDs {
				if gotProjectIDs[idx] != wantProjectID {
					t.Fatalf("expected persist project id %d at index %d, got %d", wantProjectID, idx, gotProjectIDs[idx])
				}
			}
		})
	}
}

type fakeStartupIndexingStore struct {
	revisionCount int64
	persistInputs []store.GitLabIngestInput
	persistErr    error
}

func (f *fakeStartupIndexingStore) CountRevisions(context.Context) (int64, error) {
	return f.revisionCount, nil
}

func (f *fakeStartupIndexingStore) PersistGitLabWebhook(
	_ context.Context,
	input store.GitLabIngestInput,
) (store.GitLabIngestResult, error) {
	f.persistInputs = append(f.persistInputs, input)
	if f.persistErr != nil {
		return store.GitLabIngestResult{}, f.persistErr
	}
	return store.GitLabIngestResult{EventID: int64(len(f.persistInputs)), RepoID: input.GitLabProjectID}, nil
}

type startupIndexBranchCall struct {
	projectID int64
	branch    string
}

type startupIndexBranchResult struct {
	branch gitlab.Branch
	err    error
}

type fakeStartupIndexingGitLabClient struct {
	projects           []gitlab.Project
	branches           map[string]startupIndexBranchResult
	listProjectsErr    error
	visitProjectsCalls int
	getBranchCalls     []startupIndexBranchCall
}

func (f *fakeStartupIndexingGitLabClient) VisitProjects(_ context.Context, visit func(gitlab.Project) error) (int, error) {
	f.visitProjectsCalls++
	if f.listProjectsErr != nil {
		return 0, f.listProjectsErr
	}
	for idx, project := range f.projects {
		if err := visit(project); err != nil {
			return idx + 1, err
		}
	}
	return len(f.projects), nil
}

func (f *fakeStartupIndexingGitLabClient) GetBranch(
	_ context.Context,
	projectID int64,
	branch string,
) (gitlab.Branch, error) {
	f.getBranchCalls = append(f.getBranchCalls, startupIndexBranchCall{projectID: projectID, branch: branch})
	key := startupIndexBranchKey(projectID, branch)
	result, ok := f.branches[key]
	if !ok {
		return gitlab.Branch{}, errors.New("unexpected branch lookup")
	}
	if result.err != nil {
		return gitlab.Branch{}, result.err
	}
	return result.branch, nil
}

func startupIndexBranchKey(projectID int64, branch string) string {
	return fmt.Sprintf("%d:%s", projectID, branch)
}
