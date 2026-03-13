package lint

import (
	"context"
	"strings"
	"testing"
)

func TestSourceRunnerRunSourceLayoutRoot_MapsSplitFileIssuesToRepoRelativePaths(t *testing.T) {
	t.Parallel()

	runner := NewSourceRunner()
	result, err := runner.RunSourceLayoutRoot(context.Background(), "openapi.yaml", map[string][]byte{
		"openapi.yaml": []byte(`
openapi: 3.1.0
info:
  title: Split Fixture API
  version: 1.0.0
paths:
  /pets:
    $ref: ./paths/pets.yaml#/paths/~1pets
`),
		"paths/pets.yaml": []byte(`
paths:
  /pets:
    get:
      responses:
        '200':
          description: ok
`),
	})
	if err != nil {
		t.Fatalf("RunSourceLayoutRoot() unexpected error: %v", err)
	}
	if result.Failure != nil {
		t.Fatalf("expected successful lint result, got failure %+v", result.Failure)
	}
	if len(result.Issues) == 0 {
		t.Fatal("expected source-layout issues")
	}

	var matched *SourceIssue
	for idx := range result.Issues {
		if result.Issues[idx].FilePath == "paths/pets.yaml" {
			matched = &result.Issues[idx]
			break
		}
	}
	if matched == nil {
		t.Fatalf("expected at least one issue mapped to paths/pets.yaml, got %+v", result.Issues)
	}
	if strings.Contains(matched.FilePath, "shiva-vacuum-") {
		t.Fatalf("expected repo-relative file path, got %q", matched.FilePath)
	}
	if matched.RangePos[0] < 1 {
		t.Fatalf("expected positive start line, got %+v", matched.RangePos)
	}
}

func TestSourceRunnerRunSourceLayoutRoot_ValidatesInputs(t *testing.T) {
	t.Parallel()

	runner := NewSourceRunner()
	_, err := runner.RunSourceLayoutRoot(context.Background(), "", map[string][]byte{
		"openapi.yaml": []byte("openapi: 3.1.0\n"),
	})
	if err == nil {
		t.Fatal("expected input validation error")
	}
	if !strings.Contains(err.Error(), "root path must not be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}
