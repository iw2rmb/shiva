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
	"github.com/jackc/pgx/v5"
)

func TestEnqueueStartupIndexing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                                string
		configuredNamespaces                []string
		lastProjectID                       int64
		getCheckpointErr                    error
		projects                            []gitlab.Project
		namespaceProjects                   map[string][]gitlab.Project
		branches                            map[string]startupIndexBranchResult
		listProjectsErr                     error
		listNamespaceProjectsErr            error
		listNamespaceProjectsErrByNamespace map[string]error
		persistErr                          error
		advanceErrProjectID                 int64
		advanceErr                          error
		wantErr                             string
		wantVisitProjectsCalls              int
		wantVisitProjectsOption             gitlab.ProjectListOptions
		wantVisitNamespaceCalls             []string
		wantGetBranchCalls                  []startupIndexBranchCall
		wantPersistProjectIDs               []int64
		wantAdvanceProjectIDs               []int64
	}{
		{
			name: "missing checkpoint starts from zero",
			projects: []gitlab.Project{
				{ID: 11, PathWithNamespace: "group/service-a", DefaultBranch: "main", NamespaceKind: "group"},
				{ID: 12, PathWithNamespace: "group/service-b", DefaultBranch: "develop", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"11:main":    {branch: gitlab.Branch{Name: "main", CommitID: "aaa111"}},
				"12:develop": {branch: gitlab.Branch{Name: "develop", CommitID: "bbb222"}},
			},
			wantVisitProjectsCalls:  1,
			wantVisitProjectsOption: gitlab.ProjectListOptions{},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 11, branch: "main"},
				{projectID: 12, branch: "develop"},
			},
			wantPersistProjectIDs: []int64{11, 12},
			wantAdvanceProjectIDs: []int64{11, 12},
		},
		{
			name:          "existing checkpoint resumes after last project id",
			lastProjectID: 12,
			projects: []gitlab.Project{
				{ID: 11, PathWithNamespace: "group/old-a", DefaultBranch: "main", NamespaceKind: "group"},
				{ID: 12, PathWithNamespace: "group/old-b", DefaultBranch: "main", NamespaceKind: "group"},
				{ID: 13, PathWithNamespace: "group/service-a", DefaultBranch: "main", NamespaceKind: "group"},
				{ID: 14, PathWithNamespace: "group/service-b", DefaultBranch: "develop", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"13:main":    {branch: gitlab.Branch{Name: "main", CommitID: "ccc333"}},
				"14:develop": {branch: gitlab.Branch{Name: "develop", CommitID: "ddd444"}},
			},
			wantVisitProjectsCalls:  1,
			wantVisitProjectsOption: gitlab.ProjectListOptions{IDAfter: 12},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 13, branch: "main"},
				{projectID: 14, branch: "develop"},
			},
			wantPersistProjectIDs: []int64{13, 14},
			wantAdvanceProjectIDs: []int64{13, 14},
		},
		{
			name: "skips personal namespace projects by default",
			projects: []gitlab.Project{
				{ID: 18, PathWithNamespace: "alex/personal-repo", DefaultBranch: "main", NamespaceKind: "user"},
				{ID: 19, PathWithNamespace: "group/shared-repo", DefaultBranch: "main", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"19:main": {branch: gitlab.Branch{Name: "main", CommitID: "ggg999"}},
			},
			wantVisitProjectsCalls:  1,
			wantVisitProjectsOption: gitlab.ProjectListOptions{},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 19, branch: "main"},
			},
			wantPersistProjectIDs: []int64{19},
			wantAdvanceProjectIDs: []int64{18, 19},
		},
		{
			name:                 "uses namespace traversal when configured",
			configuredNamespaces: []string{"acme/core", "acme/tools"},
			namespaceProjects: map[string][]gitlab.Project{
				"acme/core": {
					{ID: 25, PathWithNamespace: "acme/core/svc-a", DefaultBranch: "main", NamespaceKind: "group"},
				},
				"acme/tools": {
					{ID: 27, PathWithNamespace: "acme/tools/sub/svc-c", DefaultBranch: "main", NamespaceKind: "group"},
				},
			},
			branches: map[string]startupIndexBranchResult{
				"25:main": {branch: gitlab.Branch{Name: "main", CommitID: "aaa111"}},
				"27:main": {branch: gitlab.Branch{Name: "main", CommitID: "bbb222"}},
			},
			wantVisitProjectsCalls:  0,
			wantVisitNamespaceCalls: []string{"acme/core", "acme/tools"},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 25, branch: "main"},
				{projectID: 27, branch: "main"},
			},
			wantPersistProjectIDs: []int64{25, 27},
			wantAdvanceProjectIDs: []int64{25, 27},
		},
		{
			name:                 "namespace traversal honors checkpoint window",
			configuredNamespaces: []string{"acme/core"},
			lastProjectID:        30,
			namespaceProjects: map[string][]gitlab.Project{
				"acme/core": {
					{ID: 29, PathWithNamespace: "acme/core/old", DefaultBranch: "main", NamespaceKind: "group"},
					{ID: 31, PathWithNamespace: "acme/core/new", DefaultBranch: "main", NamespaceKind: "group"},
				},
			},
			branches: map[string]startupIndexBranchResult{
				"31:main": {branch: gitlab.Branch{Name: "main", CommitID: "new31"}},
			},
			wantVisitProjectsCalls:  0,
			wantVisitNamespaceCalls: []string{"acme/core"},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 31, branch: "main"},
			},
			wantPersistProjectIDs: []int64{31},
			wantAdvanceProjectIDs: []int64{31},
		},
		{
			name: "skips projects without usable default branch head",
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
			wantVisitProjectsCalls:  1,
			wantVisitProjectsOption: gitlab.ProjectListOptions{},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 22, branch: "main"},
				{projectID: 23, branch: "main"},
				{projectID: 24, branch: "main"},
			},
			wantPersistProjectIDs: []int64{24},
			wantAdvanceProjectIDs: []int64{21, 22, 23, 24},
		},
		{
			name:             "checkpoint load error fails startup indexing",
			getCheckpointErr: errors.New("db down"),
			wantErr:          "load startup indexing checkpoint",
		},
		{
			name:                   "list projects error fails startup indexing",
			lastProjectID:          55,
			listProjectsErr:        errors.New("gitlab down"),
			wantErr:                "list gitlab projects for startup indexing",
			wantVisitProjectsCalls: 1,
			wantVisitProjectsOption: gitlab.ProjectListOptions{
				IDAfter: 55,
			},
		},
		{
			name:                 "namespace projects error does not stop other namespaces",
			configuredNamespaces: []string{"acme/core", "acme/tools"},
			listNamespaceProjectsErrByNamespace: map[string]error{
				"acme/core": errors.New("gitlab namespace down"),
			},
			namespaceProjects: map[string][]gitlab.Project{
				"acme/tools": {
					{ID: 77, PathWithNamespace: "acme/tools/svc-c", DefaultBranch: "main", NamespaceKind: "group"},
				},
			},
			branches: map[string]startupIndexBranchResult{
				"77:main": {branch: gitlab.Branch{Name: "main", CommitID: "sha77"}},
			},
			wantVisitNamespaceCalls: []string{"acme/core", "acme/tools"},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 77, branch: "main"},
			},
			wantPersistProjectIDs: []int64{77},
			wantAdvanceProjectIDs: []int64{77},
		},
		{
			name: "branch load error fails startup indexing without advancing failing project",
			projects: []gitlab.Project{
				{ID: 31, PathWithNamespace: "group/service-a", DefaultBranch: "main", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"31:main": {err: errors.New("boom")},
			},
			wantErr:                 "resolve startup indexing branch head",
			wantVisitProjectsCalls:  1,
			wantVisitProjectsOption: gitlab.ProjectListOptions{},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 31, branch: "main"},
			},
		},
		{
			name: "persist error fails startup indexing without advancing failing project",
			projects: []gitlab.Project{
				{ID: 41, PathWithNamespace: "group/service-a", DefaultBranch: "main", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"41:main": {branch: gitlab.Branch{Name: "main", CommitID: "ddd444"}},
			},
			persistErr:              errors.New("db write failed"),
			wantErr:                 "enqueue startup indexing event",
			wantVisitProjectsCalls:  1,
			wantVisitProjectsOption: gitlab.ProjectListOptions{},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 41, branch: "main"},
			},
			wantPersistProjectIDs: []int64{41},
		},
		{
			name: "persist no rows error skips project and continues",
			projects: []gitlab.Project{
				{ID: 61, PathWithNamespace: "group/service-a", DefaultBranch: "main", NamespaceKind: "group"},
				{ID: 62, PathWithNamespace: "group/service-b", DefaultBranch: "main", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"61:main": {branch: gitlab.Branch{Name: "main", CommitID: "fff666"}},
				"62:main": {branch: gitlab.Branch{Name: "main", CommitID: "ggg777"}},
			},
			persistErr:              pgx.ErrNoRows,
			wantVisitProjectsCalls:  1,
			wantVisitProjectsOption: gitlab.ProjectListOptions{},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 61, branch: "main"},
				{projectID: 62, branch: "main"},
			},
			wantPersistProjectIDs: []int64{61, 62},
			wantAdvanceProjectIDs: []int64{61, 62},
		},
		{
			name: "checkpoint advance error fails startup indexing",
			projects: []gitlab.Project{
				{ID: 51, PathWithNamespace: "group/service-a", DefaultBranch: "main", NamespaceKind: "group"},
			},
			branches: map[string]startupIndexBranchResult{
				"51:main": {branch: gitlab.Branch{Name: "main", CommitID: "eee555"}},
			},
			advanceErrProjectID:     51,
			advanceErr:              errors.New("checkpoint write failed"),
			wantErr:                 "advance startup indexing checkpoint",
			wantVisitProjectsCalls:  1,
			wantVisitProjectsOption: gitlab.ProjectListOptions{},
			wantGetBranchCalls: []startupIndexBranchCall{
				{projectID: 51, branch: "main"},
			},
			wantPersistProjectIDs: []int64{51},
			wantAdvanceProjectIDs: []int64{51},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			storeFake := &fakeStartupIndexingStore{
				lastProjectID:       tc.lastProjectID,
				getCheckpointErr:    tc.getCheckpointErr,
				persistErr:          tc.persistErr,
				advanceErrProjectID: tc.advanceErrProjectID,
				advanceErr:          tc.advanceErr,
			}
			clientFake := &fakeStartupIndexingGitLabClient{
				projects:                            tc.projects,
				namespaceProjects:                   tc.namespaceProjects,
				branches:                            tc.branches,
				listProjectsErr:                     tc.listProjectsErr,
				listNamespaceProjectsErr:            tc.listNamespaceProjectsErr,
				listNamespaceProjectsErrByNamespace: tc.listNamespaceProjectsErrByNamespace,
			}

			err := enqueueStartupIndexing(
				context.Background(),
				config.Config{GitLabNamespaces: tc.configuredNamespaces},
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
				t.Fatalf("enqueueStartupIndexing() unexpected error: %v", err)
			}

			if clientFake.visitProjectsCalls != tc.wantVisitProjectsCalls {
				t.Fatalf("expected visitProjectsCalls=%d, got %d", tc.wantVisitProjectsCalls, clientFake.visitProjectsCalls)
			}
			if tc.wantVisitProjectsCalls > 0 {
				if len(clientFake.visitProjectsOptions) != tc.wantVisitProjectsCalls {
					t.Fatalf("expected visitProjectsOptions count %d, got %d", tc.wantVisitProjectsCalls, len(clientFake.visitProjectsOptions))
				}
				if clientFake.visitProjectsOptions[0] != tc.wantVisitProjectsOption {
					t.Fatalf(
						"expected visitProjectsOptions[0]=%+v, got %+v",
						tc.wantVisitProjectsOption,
						clientFake.visitProjectsOptions[0],
					)
				}
			}
			if len(clientFake.visitNamespaceProjectsCalls) != len(tc.wantVisitNamespaceCalls) {
				t.Fatalf(
					"expected visitNamespaceProjectsCalls=%v, got %v",
					tc.wantVisitNamespaceCalls,
					clientFake.visitNamespaceProjectsCalls,
				)
			}
			for idx, wantNamespace := range tc.wantVisitNamespaceCalls {
				if clientFake.visitNamespaceProjectsCalls[idx] != wantNamespace {
					t.Fatalf(
						"expected visitNamespaceProjects call %d to be %q, got %q",
						idx,
						wantNamespace,
						clientFake.visitNamespaceProjectsCalls[idx],
					)
				}
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

			if len(storeFake.advanceCalls) != len(tc.wantAdvanceProjectIDs) {
				t.Fatalf("expected advance project ids %v, got %v", tc.wantAdvanceProjectIDs, storeFake.advanceCalls)
			}
			for idx, wantProjectID := range tc.wantAdvanceProjectIDs {
				if storeFake.advanceCalls[idx] != wantProjectID {
					t.Fatalf("expected advance project id %d at index %d, got %d", wantProjectID, idx, storeFake.advanceCalls[idx])
				}
			}
		})
	}
}

type fakeStartupIndexingStore struct {
	lastProjectID       int64
	getCheckpointErr    error
	persistInputs       []store.GitLabIngestInput
	persistErr          error
	advanceCalls        []int64
	advanceErrProjectID int64
	advanceErr          error
}

func (f *fakeStartupIndexingStore) GetStartupIndexLastProjectID(context.Context) (int64, error) {
	if f.getCheckpointErr != nil {
		return 0, f.getCheckpointErr
	}
	return f.lastProjectID, nil
}

func (f *fakeStartupIndexingStore) AdvanceStartupIndexLastProjectID(_ context.Context, lastProjectID int64) error {
	f.advanceCalls = append(f.advanceCalls, lastProjectID)
	if f.advanceErr != nil && f.advanceErrProjectID == lastProjectID {
		return f.advanceErr
	}
	if lastProjectID > f.lastProjectID {
		f.lastProjectID = lastProjectID
	}
	return nil
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
	projects                            []gitlab.Project
	namespaceProjects                   map[string][]gitlab.Project
	branches                            map[string]startupIndexBranchResult
	listProjectsErr                     error
	listNamespaceProjectsErr            error
	listNamespaceProjectsErrByNamespace map[string]error
	visitProjectsCalls                  int
	visitProjectsOptions                []gitlab.ProjectListOptions
	visitNamespaceProjectsCalls         []string
	getBranchCalls                      []startupIndexBranchCall
}

func (f *fakeStartupIndexingGitLabClient) VisitProjects(
	_ context.Context,
	options gitlab.ProjectListOptions,
	visit func(gitlab.Project) error,
) (int, error) {
	f.visitProjectsCalls++
	f.visitProjectsOptions = append(f.visitProjectsOptions, options)
	if f.listProjectsErr != nil {
		return 0, f.listProjectsErr
	}
	count := 0
	for _, project := range f.projects {
		if project.ID <= options.IDAfter {
			continue
		}
		count++
		if err := visit(project); err != nil {
			return count, err
		}
	}
	return count, nil
}

func (f *fakeStartupIndexingGitLabClient) VisitNamespaceProjects(
	_ context.Context,
	namespace string,
	visit func(gitlab.Project) error,
) (int, error) {
	f.visitNamespaceProjectsCalls = append(f.visitNamespaceProjectsCalls, namespace)
	if f.listNamespaceProjectsErrByNamespace != nil {
		if err, exists := f.listNamespaceProjectsErrByNamespace[namespace]; exists && err != nil {
			return 0, err
		}
	}
	if f.listNamespaceProjectsErr != nil {
		return 0, f.listNamespaceProjectsErr
	}

	projects := f.namespaceProjects[namespace]
	count := 0
	for _, project := range projects {
		count++
		if err := visit(project); err != nil {
			return count, err
		}
	}

	return count, nil
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
