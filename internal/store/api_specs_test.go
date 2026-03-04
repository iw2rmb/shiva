package store

import (
	"context"
	"fmt"
	"reflect"
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
