package httpserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/store"
)

func TestParseRuntimeRoute_LongestRepoPrefixAndSelectorResolution(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                  string
		path                  string
		existingRepos         map[string]store.Repo
		expectedLookupOrder   []string
		expectedOpenAPIPath   string
		expectedSnapshotInput store.ResolveReadSnapshotInput
		expectedSelector      string
	}{
		{
			name: "repo path without selector",
			path: "/gl/group/repo/pets",
			existingRepos: map[string]store.Repo{
				"group/repo": {Namespace: "group", Repo: "repo", DefaultBranch: "main"},
			},
			expectedLookupOrder: []string{
				"group/repo",
			},
			expectedOpenAPIPath: "/pets",
			expectedSnapshotInput: store.ResolveReadSnapshotInput{
				Namespace: "group",
				Repo:      "repo",
			},
		},
		{
			name: "latest selector resolves after longest candidate misses",
			path: "/gl/group/repo/@latest/pets",
			existingRepos: map[string]store.Repo{
				"group/repo": {Namespace: "group", Repo: "repo", DefaultBranch: "main"},
			},
			expectedLookupOrder: []string{
				"group/repo/@latest",
				"group/repo",
			},
			expectedOpenAPIPath: "/pets",
			expectedSnapshotInput: store.ResolveReadSnapshotInput{
				Namespace: "group",
				Repo:      "repo",
			},
			expectedSelector: "latest",
		},
		{
			name: "sha selector resolves nested namespace repo",
			path: "/gl/group/subgroup/repo/@deadbeef/pets",
			existingRepos: map[string]store.Repo{
				"group/subgroup/repo": {Namespace: "group/subgroup", Repo: "repo", DefaultBranch: "main"},
			},
			expectedLookupOrder: []string{
				"group/subgroup/repo/@deadbeef",
				"group/subgroup/repo",
			},
			expectedOpenAPIPath: "/pets",
			expectedSnapshotInput: store.ResolveReadSnapshotInput{
				Namespace: "group/subgroup",
				Repo:      "repo",
				SHA:       "deadbeef",
			},
			expectedSelector: "deadbeef",
		},
		{
			name: "openapi path is normalized with leading slash",
			path: "/gl/group/repo/nested/pets/{id}",
			existingRepos: map[string]store.Repo{
				"group/repo": {Namespace: "group", Repo: "repo", DefaultBranch: "main"},
			},
			expectedLookupOrder: []string{
				"group/repo/nested/pets",
				"group/repo/nested",
				"group/repo",
			},
			expectedOpenAPIPath: "/nested/pets/{id}",
			expectedSnapshotInput: store.ResolveReadSnapshotInput{
				Namespace: "group",
				Repo:      "repo",
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			lookup := &fakeRuntimeRepoLookup{repos: testCase.existingRepos}
			route, err := parseRuntimeRoute(context.Background(), testCase.path, lookup)
			if err != nil {
				t.Fatalf("parseRuntimeRoute() unexpected error: %v", err)
			}

			if !reflect.DeepEqual(lookup.inputs, testCase.expectedLookupOrder) {
				t.Fatalf("unexpected lookup order: %+v", lookup.inputs)
			}
			if route.OpenAPIPath != testCase.expectedOpenAPIPath {
				t.Fatalf("expected OpenAPI path %q, got %q", testCase.expectedOpenAPIPath, route.OpenAPIPath)
			}
			if route.ExplicitSelector != testCase.expectedSelector {
				t.Fatalf("expected selector %q, got %q", testCase.expectedSelector, route.ExplicitSelector)
			}
			if !reflect.DeepEqual(route.SnapshotInput, testCase.expectedSnapshotInput) {
				t.Fatalf("unexpected snapshot input: %+v", route.SnapshotInput)
			}
		})
	}
}

func TestParseRuntimeRoute_RejectsInvalidSelector(t *testing.T) {
	t.Parallel()

	lookup := &fakeRuntimeRepoLookup{
		repos: map[string]store.Repo{
			"group/repo": {Namespace: "group", Repo: "repo", DefaultBranch: "main"},
		},
	}

	_, err := parseRuntimeRoute(context.Background(), "/gl/group/repo/@main/pets", lookup)
	if err == nil {
		t.Fatal("parseRuntimeRoute() expected error, got nil")
	}
	if err.Error() != "runtime selector must be latest or exactly 8 lowercase hex characters" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRuntimeRouteHandler_ResolvesSnapshotSelectors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                  string
		path                  string
		existingRepos         map[string]store.Repo
		expectedSnapshotInput store.ResolveReadSnapshotInput
	}{
		{
			name: "default branch latest without selector",
			path: "/gl/group/repo/pets",
			existingRepos: map[string]store.Repo{
				"group/repo": {ID: 11, Namespace: "group", Repo: "repo", DefaultBranch: "main"},
			},
			expectedSnapshotInput: store.ResolveReadSnapshotInput{
				Namespace: "group",
				Repo:      "repo",
			},
		},
		{
			name: "explicit latest selector",
			path: "/gl/group/repo/@latest/pets",
			existingRepos: map[string]store.Repo{
				"group/repo": {ID: 11, Namespace: "group", Repo: "repo", DefaultBranch: "main"},
			},
			expectedSnapshotInput: store.ResolveReadSnapshotInput{
				Namespace: "group",
				Repo:      "repo",
			},
		},
		{
			name: "sha selector",
			path: "/gl/group/subgroup/repo/@deadbeef/pets",
			existingRepos: map[string]store.Repo{
				"group/subgroup/repo": {ID: 12, Namespace: "group/subgroup", Repo: "repo", DefaultBranch: "main"},
			},
			expectedSnapshotInput: store.ResolveReadSnapshotInput{
				Namespace: "group/subgroup",
				Repo:      "repo",
				SHA:       "deadbeef",
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			readStore := &fakeQueryReadStore{
				repoLookupResultByPath: testCase.existingRepos,
				resolveReadSnapshotResult: store.ResolvedReadSnapshot{
					Repo:     store.Repo{ID: 77, Namespace: testCase.expectedSnapshotInput.Namespace, Repo: testCase.expectedSnapshotInput.Repo},
					Revision: store.Revision{ID: 42},
				},
				resolveOperationByMethodPathResult: store.ResolvedOperationCandidates{
					Candidates: []store.OperationSnapshot{
						{
							API:               "apis/pets/openapi.yaml",
							APISpecRevisionID: 501,
							Method:            "get",
							Path:              "/pets",
							OperationID:       "listPets",
							RawJSON:           []byte(`{"operationId":"listPets","responses":{"200":{"description":"ok"}}}`),
						},
					},
				},
				specArtifactResult: store.SpecArtifact{
					APISpecRevisionID: 501,
					SpecJSON: []byte(`{
						"openapi":"3.1.0",
						"info":{"title":"Pets","version":"1.0.0"},
						"paths":{
							"/pets":{
								"get":{
									"operationId":"listPets",
									"responses":{"200":{"description":"ok"}}
								}
							}
						}
					}`),
				},
			}
			server := newQueryTestServer(readStore)

			resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, testCase.path, nil), -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, string(body))
			}
			if !reflect.DeepEqual(readStore.resolveReadSnapshotInputs, []store.ResolveReadSnapshotInput{testCase.expectedSnapshotInput}) {
				t.Fatalf("unexpected snapshot input: %+v", readStore.resolveReadSnapshotInputs)
			}
		})
	}
}

type fakeRuntimeRepoLookup struct {
	inputs []string
	repos  map[string]store.Repo
}

func (f *fakeRuntimeRepoLookup) GetRepoByNamespaceAndRepo(_ context.Context, namespace string, repo string) (store.Repo, error) {
	path := namespace + "/" + repo
	f.inputs = append(f.inputs, path)

	repoRecord, ok := f.repos[path]
	if !ok {
		return store.Repo{}, store.ErrRepoNotFound
	}
	return repoRecord, nil
}
