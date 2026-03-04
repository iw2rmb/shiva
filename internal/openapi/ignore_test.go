package openapi

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParseShivaIgnore(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       string
		want        []string
		wantErr     error
		errContains string
	}{
		{
			name:  "comments and empty lines ignored",
			input: "# top-level comment\n\n/api/openapi.yaml\n\n  # indented comment\n\nspec/swagger.yaml\n",
			want: []string{
				"api/openapi.yaml",
				"spec/swagger.yaml",
			},
		},
		{
			name:  "leading slash anchors normalized",
			input: "/api/**\n**/testfixtures/**\n",
			want:  []string{"api/**", "**/testfixtures/**"},
		},
		{
			name:    "malformed glob is rejected",
			input:   "api/[openapi.yaml\n",
			wantErr: ErrMalformedShivaIgnoreLine,
		},
		{
			name:        "unsupported negation is rejected",
			input:       "!vendor/**\n",
			wantErr:     ErrShivaIgnoreNegationUnsupported,
			errContains: "negation",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseShivaIgnore([]byte(testCase.input))
			if testCase.wantErr == nil {
				if err != nil {
					t.Fatalf("ParseShivaIgnore() unexpected error: %v", err)
				}
				if len(got) != len(testCase.want) {
					t.Fatalf("expected %d patterns, got %d; got=%v", len(testCase.want), len(got), got)
				}
				for i := range testCase.want {
					if got[i] != testCase.want[i] {
						t.Fatalf("pattern[%d]: expected %q, got %q", i, testCase.want[i], got[i])
					}
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error %v", testCase.wantErr)
			}
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("expected error %v, got %v", testCase.wantErr, err)
			}
			if testCase.errContains != "" && !strings.Contains(err.Error(), testCase.errContains) {
				t.Fatalf("expected error to contain %q, got %q", testCase.errContains, err.Error())
			}
		})
	}
}

func TestComposeIgnoreGlobs(t *testing.T) {
	t.Parallel()

	fileIgnores, err := ParseShivaIgnore([]byte("test/*.yaml\nfoo/**/*.json\n"))
	if err != nil {
		t.Fatalf("ParseShivaIgnore() unexpected error: %v", err)
	}

	effective := ComposeIgnoreGlobs(fileIgnores)
	want := append(DefaultIgnoreGlobs(), fileIgnores...)
	if len(effective) != len(want) {
		t.Fatalf("expected %d effective ignores, got %d", len(want), len(effective))
	}
	for i := range want {
		if effective[i] != want[i] {
			t.Fatalf("expected ignore pattern %q at index %d, got %q", want[i], i, effective[i])
		}
	}
}

func TestShouldIgnorePath(t *testing.T) {
	t.Parallel()

	ignore := ComposeIgnoreGlobs([]string{"manual/ignored/**"})
	testCases := []struct {
		name     string
		path     string
		expected bool
	}{
		{name: "default test directory", path: "service/__tests__/api.yaml", expected: true},
		{name: "default node_modules", path: "/a/b/node_modules/x.txt", expected: true},
		{name: "custom file pattern", path: "manual/ignored/spec.yaml", expected: true},
		{name: "not ignored path", path: "/service/src/main.yaml", expected: false},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ignored, err := ShouldIgnorePath(testCase.path, ignore)
			if err != nil {
				t.Fatalf("ShouldIgnorePath() unexpected error: %v", err)
			}
			if ignored != testCase.expected {
				t.Fatalf("expected ignored=%v, got %v", testCase.expected, ignored)
			}
		})
	}
}

func TestLoadShivaIgnoreAtSHA(t *testing.T) {
	t.Parallel()

	sha := "target-sha-123"
	client := &fakeGitLabClient{
		files: map[string]string{
			"/.shivaignore": "spec/**/*.yaml\n!/tmp\n# comment\n",
		},
	}

	_, err := LoadShivaIgnoreAtSHA(context.Background(), client, 42, sha)
	if err == nil {
		t.Fatalf("expected error from unsupported negation in fixture")
	}
	if !errors.Is(err, ErrShivaIgnoreNegationUnsupported) {
		t.Fatalf("expected ErrShivaIgnoreNegationUnsupported, got %v", err)
	}

	client.files = map[string]string{
		"/.shivaignore": "spec/**/*.yaml\n",
	}
	parsed, err := LoadShivaIgnoreAtSHA(context.Background(), client, 42, sha)
	if err != nil {
		t.Fatalf("LoadShivaIgnoreAtSHA() unexpected error: %v", err)
	}
	if len(parsed) != 1 || parsed[0] != "spec/**/*.yaml" {
		t.Fatalf("unexpected parsed ignore globs: %#v", parsed)
	}

	client.files = map[string]string{}
	missing, err := LoadShivaIgnoreAtSHA(context.Background(), client, 42, sha)
	if err != nil {
		t.Fatalf("LoadShivaIgnoreAtSHA() unexpected error: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("expected missing .shivaignore to produce no patterns, got %#v", missing)
	}
}
