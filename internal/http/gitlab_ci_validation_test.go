package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/gitlab"
	"github.com/iw2rmb/shiva/internal/openapi"
	"github.com/iw2rmb/shiva/internal/openapi/lint"
	"github.com/iw2rmb/shiva/internal/store"
)

type fakeGitLabCIValidator struct {
	calls []GitLabCIValidationInput
	fn    func(context.Context, GitLabCIValidationInput) (GitLabCIValidationResult, error)
}

func (f *fakeGitLabCIValidator) ValidateGitLabCI(
	ctx context.Context,
	input GitLabCIValidationInput,
) (GitLabCIValidationResult, error) {
	f.calls = append(f.calls, input)
	if f.fn != nil {
		return f.fn(ctx, input)
	}
	return GitLabCIValidationResult{}, nil
}

func TestServer_GitLabCIValidationSurfaceIsRegistered(t *testing.T) {
	t.Parallel()

	server := New(config.Config{HTTPAddr: ":8080"}, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Store{})

	req := httptest.NewRequest(http.MethodPost, "/internal/gitlab/ci/validate", bytes.NewBufferString(`{}`))
	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Fatalf("expected registered route for %s, got 404", "/internal/gitlab/ci/validate")
	}
}

func TestGitLabCIValidateHandler_RequestValidation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		body         string
		expectedCode int
		expectedBody map[string]any
	}{
		{
			name:         "invalid json returns bad request",
			body:         `{`,
			expectedCode: http.StatusBadRequest,
			expectedBody: map[string]any{"error": "request body must be valid JSON"},
		},
		{
			name:         "missing gitlab project id returns bad request",
			body:         `{"namespace":"acme","repo":"platform","sha":"abc123","branch":"main"}`,
			expectedCode: http.StatusBadRequest,
			expectedBody: map[string]any{"error": "gitlab_project_id must be positive"},
		},
		{
			name:         "missing namespace returns bad request",
			body:         `{"gitlab_project_id":42,"repo":"platform","sha":"abc123","branch":"main"}`,
			expectedCode: http.StatusBadRequest,
			expectedBody: map[string]any{"error": "namespace must not be empty"},
		},
		{
			name:         "missing sha returns bad request",
			body:         `{"gitlab_project_id":42,"namespace":"acme","repo":"platform","branch":"main"}`,
			expectedCode: http.StatusBadRequest,
			expectedBody: map[string]any{"error": "sha must not be empty"},
		},
		{
			name:         "unsupported format returns bad request",
			body:         `{"gitlab_project_id":42,"namespace":"acme","repo":"platform","sha":"abc123","branch":"main","format":"sarif"}`,
			expectedCode: http.StatusBadRequest,
			expectedBody: map[string]any{"error": `format must be one of "shiva" or "gitlab_code_quality"`},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			validator := &fakeGitLabCIValidator{}
			server := newGitLabCIValidationTestServer(validator)

			resp := doGitLabCIValidationRequest(t, server, testCase.body)
			defer resp.Body.Close()

			if resp.StatusCode != testCase.expectedCode {
				payload, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected status %d, got %d body=%s", testCase.expectedCode, resp.StatusCode, string(payload))
			}
			assertJSONBody(t, resp, testCase.expectedBody)
			if len(validator.calls) != 0 {
				t.Fatalf("expected zero validator calls, got %d", len(validator.calls))
			}
		})
	}
}

func TestGitLabCIValidateHandler_ValidatorNotConfiguredReturns503(t *testing.T) {
	t.Parallel()

	server := New(config.Config{HTTPAddr: ":8080"}, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Store{})
	resp := doGitLabCIValidationRequest(
		t,
		server,
		`{"gitlab_project_id":42,"namespace":"acme","repo":"platform","sha":"abc123","branch":"main"}`,
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", resp.StatusCode)
	}
	assertJSONBody(t, resp, map[string]any{"error": "gitlab ci validator is not configured"})
}

func TestGitLabCIValidateHandler_NoSpecChangesReturnsEmptyShivaResponse(t *testing.T) {
	t.Parallel()

	sourceRunner := &fakeGitLabCISourceRunner{}
	validator := NewGitLabCIValidationService(
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
		sourceRunner,
		nil,
	)
	server := newGitLabCIValidationTestServer(validator)

	resp := doGitLabCIValidationRequest(
		t,
		server,
		`{"gitlab_project_id":42,"namespace":"acme","repo":"platform","sha":"deadbeef","branch":"main","parent_sha":"cafebabe"}`,
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, string(payload))
	}

	var body gitlabCIValidationShivaResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body.Status != "ok" {
		t.Fatalf("expected status ok, got %q", body.Status)
	}
	if body.Format != GitLabCIValidationFormatShiva {
		t.Fatalf("expected format %q, got %q", GitLabCIValidationFormatShiva, body.Format)
	}
	if body.Repo.GitLabProjectID != 42 || body.Repo.Namespace != "acme" || body.Repo.Repo != "platform" || body.Repo.SHA != "deadbeef" || body.Repo.Branch != "main" || body.Repo.ParentSHA != "cafebabe" {
		t.Fatalf("unexpected repo payload %+v", body.Repo)
	}
	if len(body.Specs) != 0 {
		t.Fatalf("expected empty specs, got %+v", body.Specs)
	}
	if len(sourceRunner.calls) != 0 {
		t.Fatalf("expected source runner not called, got %+v", sourceRunner.calls)
	}
}

func TestGitLabCIValidateHandler_ImpactedSpecReturnsSourceLocalizedIssueRows(t *testing.T) {
	t.Parallel()

	server, sourceRunner := newImpactedGitLabCIValidationServiceTestServer(
		lint.SourceExecutionResult{
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
	)

	resp := doGitLabCIValidationRequest(
		t,
		server,
		`{"gitlab_project_id":42,"namespace":"acme","repo":"platform","sha":"deadbeef","branch":"main","parent_sha":"cafebabe"}`,
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, string(payload))
	}

	var body gitlabCIValidationShivaResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body.Status != "ok" {
		t.Fatalf("expected status ok, got %q", body.Status)
	}
	if body.Format != GitLabCIValidationFormatShiva {
		t.Fatalf("expected format %q, got %q", GitLabCIValidationFormatShiva, body.Format)
	}
	if len(body.Specs) != 1 {
		t.Fatalf("expected one spec group, got %d", len(body.Specs))
	}
	if body.Specs[0].RootPath != "apis/pets/openapi.yaml" {
		t.Fatalf("unexpected root path %q", body.Specs[0].RootPath)
	}
	if len(body.Specs[0].Issues) != 1 {
		t.Fatalf("expected one issue row, got %d", len(body.Specs[0].Issues))
	}
	if body.Specs[0].Issues[0].FilePath != "apis/pets/paths.yaml" {
		t.Fatalf("expected source-localized file path, got %q", body.Specs[0].Issues[0].FilePath)
	}
	if body.Specs[0].Issues[0].JSONPath != "$.paths['/Bad_Path']" {
		t.Fatalf("unexpected json path %q", body.Specs[0].Issues[0].JSONPath)
	}
	if body.Specs[0].Issues[0].RangePos != [4]int32{7, 3, 7, 12} {
		t.Fatalf("unexpected range %+v", body.Specs[0].Issues[0].RangePos)
	}
	if len(sourceRunner.calls) != 1 || sourceRunner.calls[0] != "apis/pets/openapi.yaml" {
		t.Fatalf("unexpected source runner calls %+v", sourceRunner.calls)
	}
}

func TestGitLabCIValidateHandler_WritesShivaResponse(t *testing.T) {
	t.Parallel()

	validator := &fakeGitLabCIValidator{
		fn: func(_ context.Context, input GitLabCIValidationInput) (GitLabCIValidationResult, error) {
			return GitLabCIValidationResult{
				Specs: []GitLabCIValidationSpecResult{
					{
						RootPath: "apis/users/openapi.yaml",
						Issues: []GitLabCIValidationIssue{
							{
								RuleID:   "path-keys-no-trailing-slash",
								Severity: "error",
								Message:  "users path must not end with slash",
								JSONPath: "$.paths['/users/']",
								FilePath: "apis/users/openapi.yaml",
								RangePos: [4]int32{8, 3, 8, 12},
							},
						},
					},
					{
						RootPath: " apis/pets/openapi.yaml ",
						Issues: []GitLabCIValidationIssue{
							{
								RuleID:   "paths-kebab-case",
								Severity: "warn",
								Message:  "path segment should be kebab case",
								JSONPath: "$.paths['/Bad_Path']",
								FilePath: "apis/pets/openapi.yaml",
								RangePos: [4]int32{6, 3, 6, 12},
							},
						},
					},
				},
			}, nil
		},
	}
	server := newGitLabCIValidationTestServer(validator)

	resp := doGitLabCIValidationRequest(
		t,
		server,
		`{"gitlab_project_id":42,"namespace":" acme ","repo":" platform ","sha":" deadbeef ","branch":" main ","parent_sha":" cafebabe "}`,
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, string(payload))
	}

	if len(validator.calls) != 1 {
		t.Fatalf("expected one validator call, got %d", len(validator.calls))
	}
	if got := validator.calls[0]; got.Format != GitLabCIValidationFormatShiva {
		t.Fatalf("expected default format %q, got %q", GitLabCIValidationFormatShiva, got.Format)
	}
	if got := validator.calls[0]; got.Namespace != "acme" || got.Repo != "platform" || got.SHA != "deadbeef" || got.Branch != "main" || got.ParentSHA != "cafebabe" {
		t.Fatalf("unexpected normalized validator input %+v", got)
	}

	var body gitlabCIValidationShivaResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}

	if body.Status != "ok" {
		t.Fatalf("expected status ok, got %q", body.Status)
	}
	if body.Format != GitLabCIValidationFormatShiva {
		t.Fatalf("expected format %q, got %q", GitLabCIValidationFormatShiva, body.Format)
	}
	if len(body.Specs) != 2 {
		t.Fatalf("expected two spec groups, got %d", len(body.Specs))
	}
	if body.Specs[0].RootPath != "apis/pets/openapi.yaml" {
		t.Fatalf("expected specs sorted by root path, got %+v", body.Specs)
	}
	if body.Specs[0].Issues[0].Severity != "warn" {
		t.Fatalf("expected severity preserved in Shiva response, got %q", body.Specs[0].Issues[0].Severity)
	}
	if body.Specs[0].Issues[0].RangePos != [4]int32{6, 3, 6, 12} {
		t.Fatalf("unexpected range %+v", body.Specs[0].Issues[0].RangePos)
	}
}

func TestGitLabCIValidateHandler_WritesGitLabCodeQualityResponse(t *testing.T) {
	t.Parallel()

	validator := &fakeGitLabCIValidator{
		fn: func(_ context.Context, _ GitLabCIValidationInput) (GitLabCIValidationResult, error) {
			return GitLabCIValidationResult{
				Specs: []GitLabCIValidationSpecResult{
					{
						RootPath: "apis/pets/openapi.yaml",
						Issues: []GitLabCIValidationIssue{
							{
								RuleID:   "paths-kebab-case",
								Severity: "error",
								Message:  "path segment should be kebab case",
								JSONPath: "$.paths['/Bad_Path']",
								FilePath: "apis/pets/openapi.yaml",
								RangePos: [4]int32{6, 3, 7, 1},
							},
						},
					},
				},
			}, nil
		},
	}
	server := newGitLabCIValidationTestServer(validator)

	resp := doGitLabCIValidationRequest(
		t,
		server,
		`{"gitlab_project_id":42,"namespace":"acme","repo":"platform","sha":"deadbeef","branch":"main","format":"gitlab_code_quality"}`,
	)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, string(payload))
	}
	if len(validator.calls) != 1 || validator.calls[0].Format != GitLabCIValidationFormatGitLabCodeQuality {
		t.Fatalf("unexpected validator calls %+v", validator.calls)
	}

	var body []gitlabCodeQualityIssueResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("expected one code quality issue, got %d", len(body))
	}

	expectedFingerprint := gitlabCodeQualityFingerprint(
		validator.calls[0],
		"apis/pets/openapi.yaml",
		normalizedGitLabCIValidationIssue{
			ruleID:   "paths-kebab-case",
			severity: "error",
			message:  "path segment should be kebab case",
			jsonPath: "$.paths['/Bad_Path']",
			filePath: "apis/pets/openapi.yaml",
			rangePos: [4]int32{6, 3, 7, 1},
		},
	)

	if body[0].CheckName != "paths-kebab-case" {
		t.Fatalf("unexpected check name %q", body[0].CheckName)
	}
	if body[0].Severity != "major" {
		t.Fatalf("expected mapped severity major, got %q", body[0].Severity)
	}
	if body[0].Location.Path != "apis/pets/openapi.yaml" {
		t.Fatalf("unexpected location path %q", body[0].Location.Path)
	}
	if body[0].Location.Lines.Begin != 6 || body[0].Location.Lines.End != 7 {
		t.Fatalf("unexpected location lines %+v", body[0].Location.Lines)
	}
	if body[0].Fingerprint != expectedFingerprint {
		t.Fatalf("expected fingerprint %q, got %q", expectedFingerprint, body[0].Fingerprint)
	}
}

func newGitLabCIValidationTestServer(validator gitlabCIValidator) *Server {
	server := New(config.Config{HTTPAddr: ":8080"}, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Store{})
	server.gitlabCIValidator = validator
	return server
}

func newImpactedGitLabCIValidationServiceTestServer(
	sourceResult lint.SourceExecutionResult,
) (*Server, *fakeGitLabCISourceRunner) {
	sourceRunner := &fakeGitLabCISourceRunner{
		resultByRootPath: map[string]lint.SourceExecutionResult{
			"apis/pets/openapi.yaml": sourceResult,
		},
	}
	validator := NewGitLabCIValidationService(
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
		&fakeGitLabCIValidationResolver{
			rootByPath: map[string]openapi.RootResolution{
				"apis/pets/openapi.yaml": {
					RootPath: "apis/pets/openapi.yaml",
					Documents: map[string][]byte{
						"apis/pets/openapi.yaml": []byte("openapi: 3.1.0\n"),
					},
				},
			},
		},
		sourceRunner,
		nil,
	)
	return newGitLabCIValidationTestServer(validator), sourceRunner
}

func doGitLabCIValidationRequest(t *testing.T, server *Server, body string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/internal/gitlab/ci/validate", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	return resp
}

func assertJSONBody(t *testing.T, resp *http.Response, expected map[string]any) {
	t.Helper()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if len(body) != len(expected) {
		t.Fatalf("unexpected response body %+v", body)
	}
	for key, want := range expected {
		if body[key] != want {
			t.Fatalf("expected body[%q]=%v, got %v", key, want, body[key])
		}
	}
}
