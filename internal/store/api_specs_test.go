package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
)

func TestUpsertAPISpec_UniquenessByRepoAndRootPath(t *testing.T) {
	t.Parallel()

	queries := &fakeAPISpecUpsertQueries{
		byRepoRoot: make(map[string]sqlc.ApiSpec),
	}

	firstInput, err := normalizeUpsertAPISpecInput(UpsertAPISpecInput{
		RepoID:   7,
		RootPath: "/apis/pets/./openapi.yaml",
	})
	if err != nil {
		t.Fatalf("normalizeUpsertAPISpecInput() unexpected error: %v", err)
	}
	secondInput, err := normalizeUpsertAPISpecInput(UpsertAPISpecInput{
		RepoID:   7,
		RootPath: "apis/pets/openapi.yaml",
	})
	if err != nil {
		t.Fatalf("normalizeUpsertAPISpecInput() unexpected error: %v", err)
	}
	thirdInput, err := normalizeUpsertAPISpecInput(UpsertAPISpecInput{
		RepoID:   8,
		RootPath: "apis/pets/openapi.yaml",
	})
	if err != nil {
		t.Fatalf("normalizeUpsertAPISpecInput() unexpected error: %v", err)
	}

	first, err := upsertAPISpec(context.Background(), queries, firstInput)
	if err != nil {
		t.Fatalf("upsertAPISpec() unexpected error: %v", err)
	}
	second, err := upsertAPISpec(context.Background(), queries, secondInput)
	if err != nil {
		t.Fatalf("upsertAPISpec() unexpected error: %v", err)
	}
	third, err := upsertAPISpec(context.Background(), queries, thirdInput)
	if err != nil {
		t.Fatalf("upsertAPISpec() unexpected error: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected same id for same (repo_id, root_path), got %d and %d", first.ID, second.ID)
	}
	if first.ID == third.ID {
		t.Fatalf("expected different id for different repo_id, got %d", first.ID)
	}
	if first.RootPath != "apis/pets/openapi.yaml" {
		t.Fatalf("expected normalized root path, got %q", first.RootPath)
	}
}

func TestReplaceAPISpecDependencies_ReplacesSet(t *testing.T) {
	t.Parallel()

	queries := &fakeAPISpecDependencyQueries{
		byRevision: make(map[int64][]string),
	}

	firstInput, err := normalizeReplaceAPISpecDependenciesInput(ReplaceAPISpecDependenciesInput{
		APISpecRevisionID: 12,
		FilePaths: []string{
			"/apis/pets/openapi.yaml",
			"apis/common/../common/schemas.yaml",
			"apis/pets/openapi.yaml",
		},
	})
	if err != nil {
		t.Fatalf("normalizeReplaceAPISpecDependenciesInput() unexpected error: %v", err)
	}
	if err := replaceAPISpecDependencies(context.Background(), queries, firstInput); err != nil {
		t.Fatalf("replaceAPISpecDependencies() unexpected error: %v", err)
	}

	secondInput, err := normalizeReplaceAPISpecDependenciesInput(ReplaceAPISpecDependenciesInput{
		APISpecRevisionID: 12,
		FilePaths: []string{
			"apis/new/openapi.yaml",
			"apis/pets/openapi.yaml",
		},
	})
	if err != nil {
		t.Fatalf("normalizeReplaceAPISpecDependenciesInput() unexpected error: %v", err)
	}
	if err := replaceAPISpecDependencies(context.Background(), queries, secondInput); err != nil {
		t.Fatalf("replaceAPISpecDependencies() unexpected error: %v", err)
	}

	clearInput, err := normalizeReplaceAPISpecDependenciesInput(ReplaceAPISpecDependenciesInput{
		APISpecRevisionID: 12,
		FilePaths:         []string{},
	})
	if err != nil {
		t.Fatalf("normalizeReplaceAPISpecDependenciesInput() unexpected error: %v", err)
	}
	if err := replaceAPISpecDependencies(context.Background(), queries, clearInput); err != nil {
		t.Fatalf("replaceAPISpecDependencies() unexpected error: %v", err)
	}

	expectedCalls := []sqlc.ReplaceAPISpecDependenciesParams{
		{
			ApiSpecRevisionID: 12,
			FilePaths:         []string{"apis/common/schemas.yaml", "apis/pets/openapi.yaml"},
		},
		{
			ApiSpecRevisionID: 12,
			FilePaths:         []string{"apis/new/openapi.yaml", "apis/pets/openapi.yaml"},
		},
		{
			ApiSpecRevisionID: 12,
			FilePaths:         []string{},
		},
	}
	if !reflect.DeepEqual(queries.calls, expectedCalls) {
		t.Fatalf("unexpected dependency replacement calls: expected %+v, got %+v", expectedCalls, queries.calls)
	}

	if dependencies := queries.byRevision[12]; len(dependencies) != 0 {
		t.Fatalf("expected dependencies to be cleared, got %v", dependencies)
	}
}

func TestCountActiveAPISpecsByRepo_ActiveAndDeletedSemantics(t *testing.T) {
	t.Parallel()

	queries := &fakeAPISpecCountQueries{
		specs: []sqlc.ApiSpec{
			{RepoID: 9, RootPath: "apis/pets/openapi.yaml", Status: "active"},
			{RepoID: 9, RootPath: "apis/store/openapi.yaml", Status: "deleted"},
			{RepoID: 9, RootPath: "apis/orders/openapi.yaml", Status: "active"},
			{RepoID: 10, RootPath: "apis/other/openapi.yaml", Status: "active"},
		},
	}

	count, err := countActiveAPISpecsByRepo(context.Background(), queries, 9)
	if err != nil {
		t.Fatalf("countActiveAPISpecsByRepo() unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected active count=2 for repo_id=9, got %d", count)
	}

	countOtherRepo, err := countActiveAPISpecsByRepo(context.Background(), queries, 10)
	if err != nil {
		t.Fatalf("countActiveAPISpecsByRepo() unexpected error: %v", err)
	}
	if countOtherRepo != 1 {
		t.Fatalf("expected active count=1 for repo_id=10, got %d", countOtherRepo)
	}
}

func TestListActiveAPISpecsWithLatestDependencies(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		repoID   int64
		queries  fakeAPISpecLatestDependencyQueries
		expected []ActiveAPISpecWithLatestDependencies
	}{
		{
			name:   "uses latest processed revision dependencies and deterministic order",
			repoID: 77,
			queries: fakeAPISpecLatestDependencyQueries{
				specs: []sqlc.ApiSpec{
					{ID: 1, RepoID: 77, RootPath: "apis/pets/openapi.yaml", Status: "active"},
					{ID: 2, RepoID: 77, RootPath: "apis/orders/openapi.yaml", Status: "active"},
					{ID: 3, RepoID: 77, RootPath: "apis/deleted/openapi.yaml", Status: "deleted"},
					{ID: 4, RepoID: 88, RootPath: "apis/other/openapi.yaml", Status: "active"},
				},
				revisions: []sqlc.ApiSpecRevision{
					{ID: 11, ApiSpecID: 1, RevisionID: 100, BuildStatus: "processed"},
					{ID: 12, ApiSpecID: 1, RevisionID: 101, BuildStatus: "failed"},
					{ID: 21, ApiSpecID: 2, RevisionID: 200, BuildStatus: "processed"},
					{ID: 22, ApiSpecID: 2, RevisionID: 201, BuildStatus: "processed"},
					{ID: 31, ApiSpecID: 3, RevisionID: 300, BuildStatus: "processed"},
				},
				dependencies: []sqlc.ApiSpecDependency{
					{ApiSpecRevisionID: 11, FilePath: "apis/common/pets.yaml"},
					{ApiSpecRevisionID: 11, FilePath: "apis/pets/openapi.yaml"},
					{ApiSpecRevisionID: 12, FilePath: "apis/pets/failed.yaml"},
					{ApiSpecRevisionID: 21, FilePath: "apis/orders/older.yaml"},
					{ApiSpecRevisionID: 22, FilePath: "apis/orders/openapi.yaml"},
					{ApiSpecRevisionID: 22, FilePath: "apis/orders/schemas.yaml"},
					{ApiSpecRevisionID: 31, FilePath: "apis/deleted/openapi.yaml"},
				},
			},
			expected: []ActiveAPISpecWithLatestDependencies{
				{
					APISpec: APISpec{
						ID:       2,
						RepoID:   77,
						RootPath: "apis/orders/openapi.yaml",
						Status:   "active",
					},
					DependencyFilePaths: []string{
						"apis/orders/openapi.yaml",
						"apis/orders/schemas.yaml",
					},
				},
				{
					APISpec: APISpec{
						ID:       1,
						RepoID:   77,
						RootPath: "apis/pets/openapi.yaml",
						Status:   "active",
					},
					DependencyFilePaths: []string{
						"apis/common/pets.yaml",
						"apis/pets/openapi.yaml",
					},
				},
			},
		},
		{
			name:   "active spec without processed revision gets empty dependency list",
			repoID: 99,
			queries: fakeAPISpecLatestDependencyQueries{
				specs: []sqlc.ApiSpec{
					{ID: 7, RepoID: 99, RootPath: "apis/new/openapi.yaml", Status: "active"},
				},
				revisions: []sqlc.ApiSpecRevision{
					{ID: 71, ApiSpecID: 7, RevisionID: 700, BuildStatus: "failed"},
					{ID: 72, ApiSpecID: 7, RevisionID: 701, BuildStatus: "processing"},
				},
				dependencies: []sqlc.ApiSpecDependency{
					{ApiSpecRevisionID: 71, FilePath: "apis/new/failed.yaml"},
				},
			},
			expected: []ActiveAPISpecWithLatestDependencies{
				{
					APISpec: APISpec{
						ID:       7,
						RepoID:   99,
						RootPath: "apis/new/openapi.yaml",
						Status:   "active",
					},
					DependencyFilePaths: []string{},
				},
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := listActiveAPISpecsWithLatestDependencies(context.Background(), &testCase.queries, testCase.repoID)
			if err != nil {
				t.Fatalf("listActiveAPISpecsWithLatestDependencies() unexpected error: %v", err)
			}

			if !reflect.DeepEqual(actual, testCase.expected) {
				t.Fatalf("unexpected active api specs: expected %+v, got %+v", testCase.expected, actual)
			}
		})
	}
}

func TestMarkAPISpecDeleted_StatusTransitions(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		apiSpecID     int64
		initialStatus map[int64]string
		expectedError error
		expected      map[int64]string
	}{
		{
			name:      "active becomes deleted",
			apiSpecID: 5,
			initialStatus: map[int64]string{
				5: "active",
			},
			expected: map[int64]string{
				5: "deleted",
			},
		},
		{
			name:      "already deleted remains deleted",
			apiSpecID: 6,
			initialStatus: map[int64]string{
				6: "deleted",
			},
			expected: map[int64]string{
				6: "deleted",
			},
		},
		{
			name:          "missing id returns not found",
			apiSpecID:     9,
			initialStatus: map[int64]string{},
			expectedError: ErrAPISpecNotFound,
			expected:      map[int64]string{},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			queries := newFakeAPISpecDeleteQueries(testCase.initialStatus)
			err := markAPISpecDeleted(context.Background(), queries, testCase.apiSpecID)
			if testCase.expectedError != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", testCase.expectedError)
				}
				if !errors.Is(err, testCase.expectedError) {
					t.Fatalf("expected error %v, got %v", testCase.expectedError, err)
				}
			} else if err != nil {
				t.Fatalf("markAPISpecDeleted() unexpected error: %v", err)
			}

			if !reflect.DeepEqual(queries.statusByID, testCase.expected) {
				t.Fatalf("unexpected statuses: expected %+v, got %+v", testCase.expected, queries.statusByID)
			}
		})
	}
}

type fakeAPISpecUpsertQueries struct {
	nextID     int64
	byRepoRoot map[string]sqlc.ApiSpec
}

func (f *fakeAPISpecUpsertQueries) UpsertAPISpec(_ context.Context, arg sqlc.UpsertAPISpecParams) (sqlc.ApiSpec, error) {
	key := fmt.Sprintf("%d\x00%s", arg.RepoID, arg.RootPath)
	if existing, exists := f.byRepoRoot[key]; exists {
		return existing, nil
	}

	f.nextID++
	created := sqlc.ApiSpec{
		ID:       f.nextID,
		RepoID:   arg.RepoID,
		RootPath: arg.RootPath,
		Status:   "active",
	}
	f.byRepoRoot[key] = created
	return created, nil
}

type fakeAPISpecDependencyQueries struct {
	calls      []sqlc.ReplaceAPISpecDependenciesParams
	byRevision map[int64][]string
}

func (f *fakeAPISpecDependencyQueries) ReplaceAPISpecDependencies(
	_ context.Context,
	arg sqlc.ReplaceAPISpecDependenciesParams,
) error {
	copied := make([]string, len(arg.FilePaths))
	copy(copied, arg.FilePaths)
	f.calls = append(f.calls, sqlc.ReplaceAPISpecDependenciesParams{
		ApiSpecRevisionID: arg.ApiSpecRevisionID,
		FilePaths:         copied,
	})
	f.byRevision[arg.ApiSpecRevisionID] = copied
	return nil
}

type fakeAPISpecCountQueries struct {
	specs []sqlc.ApiSpec
}

func (f *fakeAPISpecCountQueries) CountActiveAPISpecsByRepo(_ context.Context, repoID int64) (int64, error) {
	var count int64
	for _, spec := range f.specs {
		if spec.RepoID == repoID && spec.Status == "active" {
			count++
		}
	}
	return count, nil
}

type fakeAPISpecLatestDependencyQueries struct {
	specs        []sqlc.ApiSpec
	revisions    []sqlc.ApiSpecRevision
	dependencies []sqlc.ApiSpecDependency
}

func (f *fakeAPISpecLatestDependencyQueries) ListActiveAPISpecsWithLatestDependencies(
	_ context.Context,
	repoID int64,
) ([]sqlc.ListActiveAPISpecsWithLatestDependenciesRow, error) {
	activeSpecs := make([]sqlc.ApiSpec, 0, len(f.specs))
	for _, spec := range f.specs {
		if spec.RepoID == repoID && spec.Status == "active" {
			activeSpecs = append(activeSpecs, spec)
		}
	}

	sort.Slice(activeSpecs, func(i, j int) bool {
		return activeSpecs[i].RootPath < activeSpecs[j].RootPath
	})

	latestProcessedRevisionBySpecID := make(map[int64]sqlc.ApiSpecRevision, len(activeSpecs))
	for _, revision := range f.revisions {
		if revision.BuildStatus != "processed" {
			continue
		}

		current, exists := latestProcessedRevisionBySpecID[revision.ApiSpecID]
		if !exists || revision.RevisionID > current.RevisionID ||
			(revision.RevisionID == current.RevisionID && revision.ID > current.ID) {
			latestProcessedRevisionBySpecID[revision.ApiSpecID] = revision
		}
	}

	dependencyPathsByRevisionID := make(map[int64][]string, len(f.dependencies))
	for _, dependency := range f.dependencies {
		dependencyPathsByRevisionID[dependency.ApiSpecRevisionID] = append(
			dependencyPathsByRevisionID[dependency.ApiSpecRevisionID],
			dependency.FilePath,
		)
	}

	rows := make([]sqlc.ListActiveAPISpecsWithLatestDependenciesRow, 0, len(activeSpecs))
	for _, spec := range activeSpecs {
		dependencyPaths := []string{}
		if revision, exists := latestProcessedRevisionBySpecID[spec.ID]; exists {
			dependencyPaths = append(dependencyPaths, dependencyPathsByRevisionID[revision.ID]...)
		}
		sort.Strings(dependencyPaths)

		rows = append(rows, sqlc.ListActiveAPISpecsWithLatestDependenciesRow{
			ID:              spec.ID,
			RepoID:          spec.RepoID,
			RootPath:        spec.RootPath,
			Status:          spec.Status,
			DisplayName:     spec.DisplayName,
			CreatedAt:       spec.CreatedAt,
			UpdatedAt:       spec.UpdatedAt,
			DependencyPaths: dependencyPaths,
		})
	}

	return rows, nil
}

type fakeAPISpecDeleteQueries struct {
	statusByID map[int64]string
}

func newFakeAPISpecDeleteQueries(initialStatus map[int64]string) *fakeAPISpecDeleteQueries {
	statusByID := make(map[int64]string, len(initialStatus))
	for id, status := range initialStatus {
		statusByID[id] = status
	}
	return &fakeAPISpecDeleteQueries{statusByID: statusByID}
}

func (f *fakeAPISpecDeleteQueries) MarkAPISpecDeleted(_ context.Context, apiSpecID int64) (int64, error) {
	if _, exists := f.statusByID[apiSpecID]; !exists {
		return 0, nil
	}
	f.statusByID[apiSpecID] = "deleted"
	return 1, nil
}
