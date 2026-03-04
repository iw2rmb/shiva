package openapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/iw2rmb/shiva/internal/gitlab"
)

func TestSplitFileFixtureE2E_ResolveAndBuildCanonicalSpec(t *testing.T) {
	t.Parallel()

	resolver, err := NewResolver(ResolverConfig{
		IncludeGlobs: []string{"**/openapi*.yaml"},
		MaxFetches:   32,
	})
	if err != nil {
		t.Fatalf("NewResolver() unexpected error: %v", err)
	}

	files := loadFixtureFileMap(t, "testdata/fixtures/split-file")
	client := &fakeGitLabClient{
		changedPaths: []gitlab.ChangedPath{
			{NewPath: "openapi.yaml"},
		},
		files: files,
	}

	resolution, err := resolver.ResolveChangedOpenAPI(context.Background(), client, 101, "from-sha", "to-sha")
	if err != nil {
		t.Fatalf("ResolveChangedOpenAPI() unexpected error: %v", err)
	}
	if !resolution.OpenAPIChanged {
		t.Fatalf("expected OpenAPIChanged=true")
	}

	documentPaths := sortedKeys(resolution.Documents)
	expectedDocumentPaths := []string{
		"components/common.yaml",
		"models/pet.yaml",
		"openapi.yaml",
		"paths/pets.yaml",
	}
	if len(documentPaths) != len(expectedDocumentPaths) {
		t.Fatalf("expected %d resolved documents, got %d (%v)", len(expectedDocumentPaths), len(documentPaths), documentPaths)
	}
	for i := range expectedDocumentPaths {
		if documentPaths[i] != expectedDocumentPaths[i] {
			t.Fatalf("expected resolved path %q at index %d, got %q", expectedDocumentPaths[i], i, documentPaths[i])
		}
	}

	canonical, err := BuildCanonicalSpec(resolution)
	if err != nil {
		t.Fatalf("BuildCanonicalSpec() unexpected error: %v", err)
	}
	if canonical.RootDocument != "openapi.yaml" {
		t.Fatalf("expected root document openapi.yaml, got %q", canonical.RootDocument)
	}
	if len(canonical.Endpoints) != 1 {
		t.Fatalf("expected one endpoint, got %d", len(canonical.Endpoints))
	}
	endpoint := canonical.Endpoints[0]
	if endpoint.Method != "get" || endpoint.Path != "/pets" {
		t.Fatalf("expected endpoint get /pets, got %s %s", endpoint.Method, endpoint.Path)
	}

	var root map[string]any
	if err := json.Unmarshal(canonical.SpecJSON, &root); err != nil {
		t.Fatalf("unmarshal canonical.SpecJSON: %v", err)
	}

	getOperation := root["paths"].(map[string]any)["/pets"].(map[string]any)["get"].(map[string]any)
	schema := getOperation["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if schema["type"] != "array" {
		t.Fatalf("expected inlined response schema type=array, got %#v", schema["type"])
	}
	items := schema["items"].(map[string]any)
	properties := items["properties"].(map[string]any)
	if properties["id"].(map[string]any)["type"] != "string" {
		t.Fatalf("expected inlined Pet.id type=string")
	}
}

func loadFixtureFileMap(t *testing.T, root string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", root, err)
	}

	files := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		fullPath := filepath.Join(root, name)
		if entry.IsDir() {
			nested := loadFixtureFileMap(t, fullPath)
			for key, value := range nested {
				files[filepath.ToSlash(filepath.Join(name, key))] = value
			}
			continue
		}

		content, err := os.ReadFile(fullPath)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", fullPath, err)
		}
		files[filepath.ToSlash(name)] = string(content)
	}

	return files
}

func sortedKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, strings.TrimSpace(key))
	}
	sort.Strings(keys)
	return keys
}
