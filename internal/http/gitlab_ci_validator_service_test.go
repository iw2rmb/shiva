package httpserver

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/gitlab"
	"github.com/iw2rmb/shiva/internal/openapi"
	"github.com/iw2rmb/shiva/internal/openapi/lint"
	"github.com/iw2rmb/shiva/internal/store"
)

func TestGitLabCIValidationServiceValidateGitLabCI_NoSpecChangesReturnsEmptyResult(t *testing.T) {
	t.Parallel()

	service := NewGitLabCIValidationService(
		&fakeGitLabCIValidationStore{
			repo: store.Repo{ID: 7, GitLabProjectID: 42, Namespace: "acme", Repo: "platform"},
			activeSpecs: []store.ActiveAPISpecWithLatestDependencies{
				{
					APISpec: store.APISpec{ID: 11, RepoID: 7, RootPath: "apis/pets/openapi.yaml"},
					DependencyFilePaths: []string{
						"shared/common.yaml",
					},
				},
			},
		},
		&fakeGitLabCIValidationGitLabClient{
			changedPaths: []gitlab.ChangedPath{{NewPath: "docs/readme.md"}},
		},
		&fakeGitLabCIValidationResolver{},
		&fakeGitLabCISourceRunner{},
		nil,
	)

	result, err := service.ValidateGitLabCI(context.Background(), GitLabCIValidationInput{
		GitLabProjectID: 42,
		Namespace:       "acme",
		Repo:            "platform",
		SHA:             "deadbeef",
		Branch:          "main",
		ParentSHA:       "cafebabe",
	})
	if err != nil {
		t.Fatalf("ValidateGitLabCI() unexpected error: %v", err)
	}
	if len(result.Specs) != 0 {
		t.Fatalf("expected empty specs result, got %+v", result.Specs)
	}
}

func TestGitLabCIValidationServiceValidateGitLabCI_UsesImpactedRootsAndSourceIssues(t *testing.T) {
	t.Parallel()

	resolver := &fakeGitLabCIValidationResolver{
		rootByPath: map[string]openapi.RootResolution{
			"apis/pets/openapi.yaml": {
				RootPath: "apis/pets/openapi.yaml",
				Documents: map[string][]byte{
					"apis/pets/openapi.yaml": []byte("openapi: 3.1.0\n"),
				},
			},
		},
	}
	sourceRunner := &fakeGitLabCISourceRunner{
		resultByRootPath: map[string]lint.SourceExecutionResult{
			"apis/pets/openapi.yaml": {
				Issues: []lint.SourceIssue{
					{
						RuleID:   "paths-kebab-case",
						Severity: "error",
						Message:  "path segment should be kebab case",
						JSONPath: "$.paths['/Bad_Path']",
						FilePath: "apis/pets/paths.yaml",
						RangePos: [4]int32{7, 3, 7, 12},
					},
				},
			},
		},
	}

	service := NewGitLabCIValidationService(
		&fakeGitLabCIValidationStore{
			repo: store.Repo{ID: 7, GitLabProjectID: 42, Namespace: "acme", Repo: "platform"},
			activeSpecs: []store.ActiveAPISpecWithLatestDependencies{
				{
					APISpec: store.APISpec{ID: 11, RepoID: 7, RootPath: "apis/pets/openapi.yaml"},
					DependencyFilePaths: []string{
						"shared/common.yaml",
					},
				},
			},
		},
		&fakeGitLabCIValidationGitLabClient{
			changedPaths: []gitlab.ChangedPath{{NewPath: "shared/common.yaml"}},
		},
		resolver,
		sourceRunner,
		nil,
	)

	result, err := service.ValidateGitLabCI(context.Background(), GitLabCIValidationInput{
		GitLabProjectID: 42,
		Namespace:       "acme",
		Repo:            "platform",
		SHA:             "deadbeef",
		Branch:          "main",
		ParentSHA:       "cafebabe",
	})
	if err != nil {
		t.Fatalf("ValidateGitLabCI() unexpected error: %v", err)
	}

	expected := GitLabCIValidationResult{
		Specs: []GitLabCIValidationSpecResult{
			{
				RootPath: "apis/pets/openapi.yaml",
				Issues: []GitLabCIValidationIssue{
					{
						RuleID:   "paths-kebab-case",
						Severity: "error",
						Message:  "path segment should be kebab case",
						JSONPath: "$.paths['/Bad_Path']",
						FilePath: "apis/pets/paths.yaml",
						RangePos: [4]int32{7, 3, 7, 12},
					},
				},
			},
		},
	}
	if !reflect.DeepEqual(result, expected) {
		t.Fatalf("expected result %+v, got %+v", expected, result)
	}
	if !reflect.DeepEqual(resolver.resolveRootCalls, []string{"apis/pets/openapi.yaml"}) {
		t.Fatalf("expected impacted root resolution call, got %+v", resolver.resolveRootCalls)
	}
	if !reflect.DeepEqual(sourceRunner.calls, []string{"apis/pets/openapi.yaml"}) {
		t.Fatalf("expected source runner call, got %+v", sourceRunner.calls)
	}
}

func TestGitLabCIValidationServiceValidateGitLabCI_UsesFallbackDiscoveryWhenNoImpacts(t *testing.T) {
	t.Parallel()

	resolver := &fakeGitLabCIValidationResolver{
		discoveredRoots: []openapi.RootResolution{
			{
				RootPath: "apis/new/openapi.yaml",
				Documents: map[string][]byte{
					"apis/new/openapi.yaml": []byte("openapi: 3.1.0\n"),
				},
			},
		},
	}
	sourceRunner := &fakeGitLabCISourceRunner{
		resultByRootPath: map[string]lint.SourceExecutionResult{
			"apis/new/openapi.yaml": {},
		},
	}

	service := NewGitLabCIValidationService(
		&fakeGitLabCIValidationStore{
			repo: store.Repo{ID: 7, GitLabProjectID: 42, Namespace: "acme", Repo: "platform"},
		},
		&fakeGitLabCIValidationGitLabClient{
			changedPaths: []gitlab.ChangedPath{{NewPath: "apis/new/openapi.yaml", NewFile: true}},
		},
		resolver,
		sourceRunner,
		nil,
	)

	result, err := service.ValidateGitLabCI(context.Background(), GitLabCIValidationInput{
		GitLabProjectID: 42,
		Namespace:       "acme",
		Repo:            "platform",
		SHA:             "deadbeef",
		Branch:          "main",
		ParentSHA:       "cafebabe",
	})
	if err != nil {
		t.Fatalf("ValidateGitLabCI() unexpected error: %v", err)
	}
	if len(result.Specs) != 1 || result.Specs[0].RootPath != "apis/new/openapi.yaml" {
		t.Fatalf("expected discovered root result, got %+v", result.Specs)
	}
	if !reflect.DeepEqual(resolver.discoverCalls, [][]string{{"apis/new/openapi.yaml"}}) {
		t.Fatalf("expected fallback discovery call, got %+v", resolver.discoverCalls)
	}
}

func TestGitLabCIValidationServiceValidateGitLabCI_UsesRepositoryDiscoveryWithoutParentSHA(t *testing.T) {
	t.Parallel()

	resolver := &fakeGitLabCIValidationResolver{
		repositoryRoots: []openapi.RootResolution{
			{
				RootPath: "apis/pets/openapi.yaml",
				Documents: map[string][]byte{
					"apis/pets/openapi.yaml": []byte("openapi: 3.1.0\n"),
				},
			},
		},
	}
	sourceRunner := &fakeGitLabCISourceRunner{
		resultByRootPath: map[string]lint.SourceExecutionResult{
			"apis/pets/openapi.yaml": {},
		},
	}

	service := NewGitLabCIValidationService(
		&fakeGitLabCIValidationStore{
			repo: store.Repo{ID: 7, GitLabProjectID: 42, Namespace: "acme", Repo: "platform"},
		},
		&fakeGitLabCIValidationGitLabClient{},
		resolver,
		sourceRunner,
		nil,
	)

	result, err := service.ValidateGitLabCI(context.Background(), GitLabCIValidationInput{
		GitLabProjectID: 42,
		Namespace:       "acme",
		Repo:            "platform",
		SHA:             "deadbeef",
		Branch:          "main",
	})
	if err != nil {
		t.Fatalf("ValidateGitLabCI() unexpected error: %v", err)
	}
	if len(result.Specs) != 1 || result.Specs[0].RootPath != "apis/pets/openapi.yaml" {
		t.Fatalf("expected repository discovery result, got %+v", result.Specs)
	}
	if resolver.repositoryCalls != 1 {
		t.Fatalf("expected one repository discovery call, got %d", resolver.repositoryCalls)
	}
}

type fakeGitLabCIValidationStore struct {
	repo        store.Repo
	repoErr     error
	activeSpecs []store.ActiveAPISpecWithLatestDependencies
	activeErr   error
}

func (f *fakeGitLabCIValidationStore) GetRepoByNamespaceAndRepo(_ context.Context, _, _ string) (store.Repo, error) {
	if f.repoErr != nil {
		return store.Repo{}, f.repoErr
	}
	return f.repo, nil
}

func (f *fakeGitLabCIValidationStore) ListActiveAPISpecsWithLatestDependencies(
	_ context.Context,
	_ int64,
) ([]store.ActiveAPISpecWithLatestDependencies, error) {
	if f.activeErr != nil {
		return nil, f.activeErr
	}
	result := make([]store.ActiveAPISpecWithLatestDependencies, len(f.activeSpecs))
	copy(result, f.activeSpecs)
	return result, nil
}

type fakeGitLabCIValidationGitLabClient struct {
	changedPaths []gitlab.ChangedPath
	compareErr   error
}

func (f *fakeGitLabCIValidationGitLabClient) CompareChangedPaths(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
) ([]gitlab.ChangedPath, error) {
	if f.compareErr != nil {
		return nil, f.compareErr
	}
	result := make([]gitlab.ChangedPath, len(f.changedPaths))
	copy(result, f.changedPaths)
	return result, nil
}

func (*fakeGitLabCIValidationGitLabClient) GetFileContent(context.Context, int64, string, string) ([]byte, error) {
	return nil, errors.New("GetFileContent should not be called in service tests")
}

func (*fakeGitLabCIValidationGitLabClient) ListRepositoryTree(
	context.Context,
	int64,
	string,
	string,
	bool,
) ([]gitlab.TreeEntry, error) {
	return nil, errors.New("ListRepositoryTree should not be called in service tests")
}

type fakeGitLabCIValidationResolver struct {
	rootByPath       map[string]openapi.RootResolution
	discoveredRoots  []openapi.RootResolution
	repositoryRoots  []openapi.RootResolution
	resolveRootCalls []string
	discoverCalls    [][]string
	repositoryCalls  int
}

func (f *fakeGitLabCIValidationResolver) ResolveRootOpenAPIAtSHA(
	_ context.Context,
	_ openapi.GitLabClient,
	_ int64,
	_ string,
	rootPath string,
) (openapi.RootResolution, error) {
	f.resolveRootCalls = append(f.resolveRootCalls, rootPath)
	if result, ok := f.rootByPath[rootPath]; ok {
		return result, nil
	}
	return openapi.RootResolution{}, errors.New("root not found in fake resolver")
}

func (f *fakeGitLabCIValidationResolver) ResolveDiscoveredRootsAtPaths(
	_ context.Context,
	_ openapi.GitLabClient,
	_ int64,
	_ string,
	paths []string,
) ([]openapi.RootResolution, error) {
	copied := make([]string, len(paths))
	copy(copied, paths)
	f.discoverCalls = append(f.discoverCalls, copied)

	result := make([]openapi.RootResolution, len(f.discoveredRoots))
	copy(result, f.discoveredRoots)
	return result, nil
}

func (f *fakeGitLabCIValidationResolver) ResolveRepositoryOpenAPIAtSHA(
	_ context.Context,
	_ openapi.GitLabBootstrapClient,
	_ int64,
	_ string,
) ([]openapi.RootResolution, error) {
	f.repositoryCalls++
	result := make([]openapi.RootResolution, len(f.repositoryRoots))
	copy(result, f.repositoryRoots)
	return result, nil
}

type fakeGitLabCISourceRunner struct {
	resultByRootPath map[string]lint.SourceExecutionResult
	errByRootPath    map[string]error
	calls            []string
}

func (f *fakeGitLabCISourceRunner) RunSourceLayoutRoot(
	_ context.Context,
	rootPath string,
	_ map[string][]byte,
) (lint.SourceExecutionResult, error) {
	f.calls = append(f.calls, rootPath)
	if err, ok := f.errByRootPath[rootPath]; ok {
		return lint.SourceExecutionResult{}, err
	}
	if result, ok := f.resultByRootPath[rootPath]; ok {
		return result, nil
	}
	return lint.SourceExecutionResult{}, errors.New("root not found in fake source runner")
}
