package openapi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/shiva/internal/gitlab"
)

type fakeGitLabClient struct {
	changedPaths []gitlab.ChangedPath
	files        map[string]string
	treeEntries  []gitlab.TreeEntry
	compareErr   error
	treeErr      error
}

func (f *fakeGitLabClient) CompareChangedPaths(_ context.Context, _ int64, _ string, _ string) ([]gitlab.ChangedPath, error) {
	if f.compareErr != nil {
		return nil, f.compareErr
	}
	return f.changedPaths, nil
}

func (f *fakeGitLabClient) GetFileContent(_ context.Context, _ int64, filePath, _ string) ([]byte, error) {
	content, exists := f.files[filePath]
	if !exists {
		return nil, fmt.Errorf("%w: path=%s", gitlab.ErrNotFound, filePath)
	}
	return []byte(content), nil
}

func (f *fakeGitLabClient) ListRepositoryTree(
	_ context.Context,
	_ int64,
	_ string,
	_ string,
	_ bool,
) ([]gitlab.TreeEntry, error) {
	if f.treeErr != nil {
		return nil, f.treeErr
	}
	entries := make([]gitlab.TreeEntry, len(f.treeEntries))
	copy(entries, f.treeEntries)
	return entries, nil
}

func TestResolverResolveChangedOpenAPI_MultiFileSuccess(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(ResolverConfig{
		IncludeGlobs: []string{"api/**/*.yaml"},
		MaxFetches:   16,
	})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	client := &fakeGitLabClient{
		changedPaths: []gitlab.ChangedPath{
			{NewPath: "api/openapi.yaml"},
		},
		files: map[string]string{
			"api/openapi.yaml":    "openapi: 3.1.0\ninfo:\n  title: Demo\npaths:\n  /pets:\n    get:\n      responses:\n        '200':\n          description: ok\n          content:\n            application/json:\n              schema:\n                $ref: ./components.yaml#/components/schemas/Pet\n",
			"api/components.yaml": "components:\n  schemas:\n    Pet:\n      $ref: ./models/pet.yaml#/Pet\n",
			"api/models/pet.yaml": "Pet:\n  type: object\n  properties:\n    id:\n      type: string\n",
		},
	}

	result, err := resolver.ResolveChangedOpenAPI(context.Background(), client, 42, "from-sha", "to-sha")
	if err != nil {
		t.Fatalf("ResolveChangedOpenAPI() unexpected error: %v", err)
	}

	if !result.OpenAPIChanged {
		t.Fatalf("expected OpenAPIChanged=true")
	}
	if len(result.CandidateFiles) != 1 || result.CandidateFiles[0] != "api/openapi.yaml" {
		t.Fatalf("unexpected candidate files: %#v", result.CandidateFiles)
	}
	for _, requiredPath := range []string{
		"api/openapi.yaml",
		"api/components.yaml",
		"api/models/pet.yaml",
	} {
		if _, exists := result.Documents[requiredPath]; !exists {
			t.Fatalf("expected resolved documents to include %q", requiredPath)
		}
	}
}

func TestResolverResolveChangedOpenAPI_InvalidTopLevelDocument(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(ResolverConfig{
		IncludeGlobs: []string{"specs/*.yaml"},
	})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	client := &fakeGitLabClient{
		changedPaths: []gitlab.ChangedPath{
			{NewPath: "specs/service.yaml"},
		},
		files: map[string]string{
			"specs/service.yaml": "info:\n  title: Missing Header\n",
		},
	}

	_, err = resolver.ResolveChangedOpenAPI(context.Background(), client, 7, "from-sha", "to-sha")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrInvalidOpenAPIDocument) {
		t.Fatalf("expected ErrInvalidOpenAPIDocument, got %v", err)
	}
}

func TestResolverResolveChangedOpenAPI_ReferenceCycle(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(ResolverConfig{
		IncludeGlobs: []string{"**/*.yaml"},
		MaxFetches:   16,
	})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	client := &fakeGitLabClient{
		changedPaths: []gitlab.ChangedPath{
			{NewPath: "spec/openapi.yaml"},
		},
		files: map[string]string{
			"spec/openapi.yaml": "openapi: 3.0.3\ncomponents:\n  schemas:\n    A:\n      $ref: ./a.yaml#/A\n",
			"spec/a.yaml":       "A:\n  $ref: ./b.yaml#/B\n",
			"spec/b.yaml":       "B:\n  $ref: ./a.yaml#/A\n",
		},
	}

	_, err = resolver.ResolveChangedOpenAPI(context.Background(), client, 7, "from-sha", "to-sha")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrReferenceCycle) {
		t.Fatalf("expected ErrReferenceCycle, got %v", err)
	}
}

func TestResolverResolveChangedOpenAPI_ReferenceFetchLimit(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(ResolverConfig{
		IncludeGlobs: []string{"spec/**/*.yaml"},
		MaxFetches:   2,
	})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	client := &fakeGitLabClient{
		changedPaths: []gitlab.ChangedPath{
			{NewPath: "spec/openapi.yaml"},
		},
		files: map[string]string{
			"spec/openapi.yaml": "openapi: 3.0.3\ncomponents:\n  schemas:\n    A:\n      $ref: ./a.yaml#/A\n",
			"spec/a.yaml":       "A:\n  $ref: ./b.yaml#/B\n",
			"spec/b.yaml":       "B:\n  type: object\n",
		},
	}

	_, err = resolver.ResolveChangedOpenAPI(context.Background(), client, 7, "from-sha", "to-sha")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrFetchLimitExceeded) {
		t.Fatalf("expected ErrFetchLimitExceeded, got %v", err)
	}
}

func TestResolverResolveChangedOpenAPI_NoCandidateMatches(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(ResolverConfig{
		IncludeGlobs: []string{"spec/**/*.yaml"},
		MaxFetches:   16,
	})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	client := &fakeGitLabClient{
		changedPaths: []gitlab.ChangedPath{
			{NewPath: "README.md"},
		},
		files: map[string]string{},
	}

	result, err := resolver.ResolveChangedOpenAPI(context.Background(), client, 7, "from-sha", "to-sha")
	if err != nil {
		t.Fatalf("ResolveChangedOpenAPI() unexpected error: %v", err)
	}
	if result.OpenAPIChanged {
		t.Fatalf("expected OpenAPIChanged=false")
	}
	if len(result.CandidateFiles) != 0 {
		t.Fatalf("expected no candidates, got %#v", result.CandidateFiles)
	}
	if len(result.Documents) != 0 {
		t.Fatalf("expected no resolved documents, got %#v", result.Documents)
	}
}

func TestResolverResolveChangedOpenAPI_DeletedCandidateSignalsOpenAPIChange(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(ResolverConfig{
		IncludeGlobs: []string{"spec/**/*.yaml"},
		MaxFetches:   16,
	})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	client := &fakeGitLabClient{
		changedPaths: []gitlab.ChangedPath{
			{DeletedFile: true, OldPath: "spec/openapi.yaml"},
		},
		files: map[string]string{},
	}

	result, err := resolver.ResolveChangedOpenAPI(context.Background(), client, 7, "from-sha", "to-sha")
	if err != nil {
		t.Fatalf("ResolveChangedOpenAPI() unexpected error: %v", err)
	}
	if !result.OpenAPIChanged {
		t.Fatalf("expected OpenAPIChanged=true when candidate is deleted")
	}
	if len(result.CandidateFiles) != 0 {
		t.Fatalf("expected no non-deleted candidates, got %#v", result.CandidateFiles)
	}
	if len(result.Documents) != 0 {
		t.Fatalf("expected no resolved documents, got %#v", result.Documents)
	}
}

func TestResolverResolveChangedOpenAPI_InvalidReferences(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		openapi  string
		contains string
	}{
		{
			name: "external reference rejected",
			openapi: "openapi: 3.0.3\npaths:\n  /pets:\n    get:\n      responses:\n        '200':\n" +
				"          content:\n            application/json:\n              schema:\n" +
				"                $ref: https://example.com/spec.yaml#/components/schemas/Pet\n",
			contains: "external reference",
		},
		{
			name: "path traversal rejected",
			openapi: "openapi: 3.0.3\npaths:\n  /pets:\n    get:\n      responses:\n        '200':\n" +
				"          content:\n            application/json:\n              schema:\n" +
				"                $ref: ../../shared/pet.yaml#/Pet\n",
			contains: "escapes repository root",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			resolver, err := NewResolver(ResolverConfig{
				IncludeGlobs: []string{"spec/**/*.yaml"},
				MaxFetches:   16,
			})
			if err != nil {
				t.Fatalf("NewResolver() unexpected error: %v", err)
			}

			client := &fakeGitLabClient{
				changedPaths: []gitlab.ChangedPath{
					{NewPath: "spec/openapi.yaml"},
				},
				files: map[string]string{
					"spec/openapi.yaml": testCase.openapi,
				},
			}

			_, err = resolver.ResolveChangedOpenAPI(context.Background(), client, 7, "from-sha", "to-sha")
			if err == nil {
				t.Fatalf("expected error")
			}
			if !errors.Is(err, ErrInvalidReference) {
				t.Fatalf("expected ErrInvalidReference, got %v", err)
			}
			if !strings.Contains(err.Error(), testCase.contains) {
				t.Fatalf("expected error to contain %q, got %q", testCase.contains, err.Error())
			}
		})
	}
}

func TestResolverResolveRepositoryOpenAPIAtSHA_FullTreeDiscoveryWithoutChangedSignal(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(ResolverConfig{MaxFetches: 16})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	client := &fakeGitLabClient{
		compareErr: errors.New("compare should not be called for bootstrap discovery"),
		treeEntries: []gitlab.TreeEntry{
			{Path: "README.md", Type: "file"},
			{Path: "services/pets/openapi.yaml", Type: "file"},
			{Path: "services/pets/components.yaml", Type: "file"},
			{Path: "services/pets/models/pet.yaml", Type: "file"},
		},
		files: map[string]string{
			"services/pets/openapi.yaml":    "openapi: 3.1.0\ninfo:\n  title: Pets\npaths:\n  /pets:\n    get:\n      responses:\n        '200':\n          description: ok\n          content:\n            application/json:\n              schema:\n                $ref: ./components.yaml#/components/schemas/Pet\n",
			"services/pets/components.yaml": "components:\n  schemas:\n    Pet:\n      $ref: ./models/pet.yaml#/Pet\n",
			"services/pets/models/pet.yaml": "Pet:\n  type: object\n  properties:\n    id:\n      type: string\n",
		},
	}

	roots, err := resolver.ResolveRepositoryOpenAPIAtSHA(context.Background(), client, 42, "target-sha")
	if err != nil {
		t.Fatalf("ResolveRepositoryOpenAPIAtSHA() unexpected error: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("expected one discovered root, got %#v", roots)
	}

	root := roots[0]
	if root.RootPath != "services/pets/openapi.yaml" {
		t.Fatalf("expected root services/pets/openapi.yaml, got %q", root.RootPath)
	}
	for _, expected := range []string{
		"services/pets/openapi.yaml",
		"services/pets/components.yaml",
		"services/pets/models/pet.yaml",
	} {
		if _, ok := root.Documents[expected]; !ok {
			t.Fatalf("expected documents to include %q", expected)
		}
	}
	wantDependencies := []string{
		"services/pets/components.yaml",
		"services/pets/models/pet.yaml",
	}
	if !reflect.DeepEqual(root.DependencyFiles, wantDependencies) {
		t.Fatalf("expected dependency files %#v, got %#v", wantDependencies, root.DependencyFiles)
	}
}

func TestResolverResolveRepositoryOpenAPIAtSHA_ShivaIgnoreExclusion(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(ResolverConfig{MaxFetches: 16})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	client := &fakeGitLabClient{
		treeEntries: []gitlab.TreeEntry{
			{Path: "ignored/openapi.yaml", Type: "file"},
			{Path: "spec/openapi.yaml", Type: "file"},
		},
		files: map[string]string{
			"/.shivaignore":        "ignored/**\n",
			"ignored/openapi.yaml": "openapi: 3.1.0\ninfo:\n  title: Ignored\npaths: {}\n",
			"spec/openapi.yaml":    "openapi: 3.1.0\ninfo:\n  title: Included\npaths: {}\n",
		},
	}

	roots, err := resolver.ResolveRepositoryOpenAPIAtSHA(context.Background(), client, 42, "target-sha")
	if err != nil {
		t.Fatalf("ResolveRepositoryOpenAPIAtSHA() unexpected error: %v", err)
	}

	if len(roots) != 1 {
		t.Fatalf("expected one discovered root, got %#v", roots)
	}
	if roots[0].RootPath != "spec/openapi.yaml" {
		t.Fatalf("expected spec/openapi.yaml root, got %q", roots[0].RootPath)
	}
}

func TestResolverResolveRepositoryOpenAPIAtSHA_ZeroRoots(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(ResolverConfig{MaxFetches: 16})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	client := &fakeGitLabClient{
		treeEntries: []gitlab.TreeEntry{
			{Path: "docs/service.yaml", Type: "file"},
			{Path: "docs/README.md", Type: "file"},
		},
		files: map[string]string{
			"docs/service.yaml": "info:\n  title: Not OpenAPI\n",
		},
	}

	roots, err := resolver.ResolveRepositoryOpenAPIAtSHA(context.Background(), client, 42, "target-sha")
	if err != nil {
		t.Fatalf("ResolveRepositoryOpenAPIAtSHA() unexpected error: %v", err)
	}
	if len(roots) != 0 {
		t.Fatalf("expected zero discovered roots, got %#v", roots)
	}
}
