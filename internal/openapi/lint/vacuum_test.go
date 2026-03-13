package lint

import (
	"context"
	"strings"
	"testing"
)

func TestCanonicalRunnerRunCanonicalSpec_NormalizesIssues(t *testing.T) {
	t.Parallel()

	runner := NewCanonicalRunner()
	result, err := runner.RunCanonicalSpec(context.Background(), `
openapi: 3.1.0
info:
  title: Test API
  version: 1.0.0
paths:
  /Bad_Path:
    get:
      responses:
        '200':
          description: ok
`)
	if err != nil {
		t.Fatalf("RunCanonicalSpec() unexpected error: %v", err)
	}
	if result.Failure != nil {
		t.Fatalf("expected successful lint result, got failure %+v", result.Failure)
	}
	if len(result.Issues) == 0 {
		t.Fatal("expected normalized issues")
	}

	var matched *CanonicalIssue
	for idx := range result.Issues {
		if result.Issues[idx].RuleID == "paths-kebab-case" {
			matched = &result.Issues[idx]
			break
		}
	}
	if matched == nil {
		t.Fatalf("expected paths-kebab-case issue in %+v", result.Issues)
	}
	if matched.JSONPath != "$.paths['/Bad_Path']" {
		t.Fatalf("expected json path to be normalized, got %q", matched.JSONPath)
	}
	if matched.Message == "" {
		t.Fatal("expected normalized message")
	}
	if matched.RangePos != [4]int32{6, 3, 6, 12} {
		t.Fatalf("expected range [6 3 6 12], got %v", matched.RangePos)
	}
}

func TestCanonicalRunnerRunCanonicalSpec_NormalizesParserFailureSeparately(t *testing.T) {
	t.Parallel()

	runner := NewCanonicalRunner()
	result, err := runner.RunCanonicalSpec(context.Background(), `
app:
  batch_length: 1
`)
	if err != nil {
		t.Fatalf("RunCanonicalSpec() unexpected error: %v", err)
	}
	if result.Failure == nil {
		t.Fatal("expected normalized failure")
	}
	if len(result.Issues) != 0 {
		t.Fatalf("expected parser failure to produce zero issues, got %+v", result.Issues)
	}
	if !strings.Contains(result.Failure.Message, "spec type not supported") {
		t.Fatalf("unexpected failure message: %q", result.Failure.Message)
	}
}
