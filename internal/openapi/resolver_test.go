package openapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/iw2rmb/shiva/internal/gitlab"
)

type fakeGitLabClient struct {
	changedPaths []gitlab.ChangedPath
	files        map[string]string
}

func (f *fakeGitLabClient) CompareChangedPaths(_ context.Context, _ int64, _ string, _ string) ([]gitlab.ChangedPath, error) {
	return f.changedPaths, nil
}

func (f *fakeGitLabClient) GetFileContent(_ context.Context, _ int64, filePath, _ string) ([]byte, error) {
	content, exists := f.files[filePath]
	if !exists {
		return nil, fmt.Errorf("%w: path=%s", gitlab.ErrNotFound, filePath)
	}
	return []byte(content), nil
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
