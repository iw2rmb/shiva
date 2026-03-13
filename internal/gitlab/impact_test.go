package gitlab

import (
	"reflect"
	"testing"
)

func TestImpactedOpenAPIRoots(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		roots    []OpenAPIRootDependencySet
		changed  []ChangedPath
		expected []ImpactedOpenAPIRoot
	}{
		{
			name: "rename on dependency impacts root",
			roots: []OpenAPIRootDependencySet{
				{
					RootPath:        "apis/pets/openapi.yaml",
					DependencyPaths: []string{"shared/common.yaml"},
				},
			},
			changed: []ChangedPath{
				{
					OldPath:     "shared/legacy.yaml",
					NewPath:     "shared/common.yaml",
					RenamedFile: true,
				},
			},
			expected: []ImpactedOpenAPIRoot{
				{RootPath: "apis/pets/openapi.yaml"},
			},
		},
		{
			name: "deleted root is marked",
			roots: []OpenAPIRootDependencySet{
				{RootPath: "apis/pets/openapi.yaml"},
			},
			changed: []ChangedPath{
				{
					OldPath:     "apis/pets/openapi.yaml",
					DeletedFile: true,
				},
			},
			expected: []ImpactedOpenAPIRoot{
				{RootPath: "apis/pets/openapi.yaml", RootDeleted: true},
			},
		},
		{
			name: "unrelated change returns empty",
			roots: []OpenAPIRootDependencySet{
				{
					RootPath:        "apis/pets/openapi.yaml",
					DependencyPaths: []string{"shared/common.yaml"},
				},
			},
			changed: []ChangedPath{
				{NewPath: "docs/readme.md"},
			},
			expected: []ImpactedOpenAPIRoot{},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual := ImpactedOpenAPIRoots(testCase.roots, testCase.changed)
			if !reflect.DeepEqual(actual, testCase.expected) {
				t.Fatalf("expected impacted roots %+v, got %+v", testCase.expected, actual)
			}
		})
	}
}

func TestFallbackDiscoveryCandidatePaths(t *testing.T) {
	t.Parallel()

	changedPaths := []ChangedPath{
		{NewPath: "apis/new/openapi.yaml", NewFile: true},
		{OldPath: "apis/old/openapi.yaml", NewPath: "apis/newer/openapi.yaml", RenamedFile: true},
		{NewPath: "apis/new/openapi.yaml", NewFile: true},
		{NewPath: "docs/readme.md", NewFile: true},
	}

	expected := []string{
		"apis/new/openapi.yaml",
		"apis/newer/openapi.yaml",
		"docs/readme.md",
	}

	actual := FallbackDiscoveryCandidatePaths(changedPaths)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("expected discovery candidates %+v, got %+v", expected, actual)
	}
}

func TestNormalizeRepoPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		raw      string
		expected string
	}{
		{raw: " /apis/pets/openapi.yaml ", expected: "apis/pets/openapi.yaml"},
		{raw: "../etc/passwd", expected: ""},
		{raw: "", expected: ""},
	}

	for _, testCase := range testCases {
		if actual := NormalizeRepoPath(testCase.raw); actual != testCase.expected {
			t.Fatalf("NormalizeRepoPath(%q) expected %q, got %q", testCase.raw, testCase.expected, actual)
		}
	}
}
