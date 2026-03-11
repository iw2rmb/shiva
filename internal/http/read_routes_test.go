package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/store"
)

func TestReadRoutes_SelectorModesResolveExpectedInput(t *testing.T) {
	t.Parallel()

	shaSelector := "11111111"
	testCases := []struct {
		name          string
		method        string
		path          string
		expectedInput store.ResolveReadSelectorInput
	}{
		{
			name:   "no-selector spec route",
			method: http.MethodGet,
			path:   "/v1/specs/acme-platform-api/openapi.json",
			expectedInput: store.ResolveReadSelectorInput{
				RepoPath:   "acme-platform-api",
				NoSelector: true,
			},
		},
		{
			name:   "selector spec route",
			method: http.MethodGet,
			path:   "/v1/specs/acme-platform-api/" + shaSelector + "/openapi.json",
			expectedInput: store.ResolveReadSelectorInput{
				RepoPath: "acme-platform-api",
				Selector: shaSelector,
			},
		},
		{
			name:   "selector spec route with sha",
			method: http.MethodGet,
			path:   fmt.Sprintf("/v1/specs/acme-platform-api/%s/openapi.yaml", shaSelector),
			expectedInput: store.ResolveReadSelectorInput{
				RepoPath: "acme-platform-api",
				Selector: shaSelector,
			},
		},
		{
			name:   "operation slice route without selector",
			method: http.MethodGet,
			path:   "/v1/routes/acme-platform-api/%2Fpets",
			expectedInput: store.ResolveReadSelectorInput{
				RepoPath:   "acme-platform-api",
				NoSelector: true,
			},
		},
		{
			name:   "operation slice route with selector",
			method: http.MethodGet,
			path:   "/v1/routes/acme-platform-api/" + shaSelector + "/%2Fpets",
			expectedInput: store.ResolveReadSelectorInput{
				RepoPath: "acme-platform-api",
				Selector: shaSelector,
			},
		},
		{
			name:   "operation route uses request method",
			method: http.MethodPost,
			path:   "/v1/routes/acme-platform-api/" + shaSelector + "/%2Fpets",
			expectedInput: store.ResolveReadSelectorInput{
				RepoPath: "acme-platform-api",
				Selector: shaSelector,
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			readStore := &fakeReadRouteStore{
				resolved: store.ResolvedReadSelector{Revision: store.Revision{ID: 21}},
				artifact: store.SpecArtifact{
					APISpecRevisionID: 21,
					SpecJSON:          []byte(`{"openapi":"3.1.0","paths":{}}`),
					SpecYAML:          "openapi: 3.1.0\npaths: {}\n",
					ETag:              "\"etag-21\"",
				},
				endpoint: store.EndpointIndexRecord{
					Method:  "get",
					Path:    "/pets",
					RawJSON: []byte(`{"responses":{"200":{"description":"ok"}}}`),
				},
				endpointFound: true,
			}
			server := newReadRouteTestServer(readStore)

			req := httptest.NewRequest(testCase.method, testCase.path, nil)
			resp, err := server.App().Test(req, -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, string(body))
			}
			if len(readStore.resolveInputs) != 1 {
				t.Fatalf("expected one selector resolution call, got %d", len(readStore.resolveInputs))
			}
			if !reflect.DeepEqual(readStore.resolveInputs[0], testCase.expectedInput) {
				t.Fatalf("unexpected selector resolution input: %+v", readStore.resolveInputs[0])
			}
		})
	}
}

func TestReadRoutes_DelimitedMonorepoPathParsing(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                 string
		path                 string
		method               string
		expectedAPIRoot      string
		expectApiLookup      bool
		expectSingleLookup   bool
		expectEndpointLookup bool
	}{
		{
			name:                 "single-spec route ignores api delimiter",
			path:                 "/v1/routes/repo/%2Fpets",
			method:               http.MethodGet,
			expectApiLookup:      false,
			expectSingleLookup:   true,
			expectEndpointLookup: true,
		},
		{
			name:                 "monorepo route resolves api path with slash-delimited api root",
			path:                 "/v1/routes/repo/-/platform/api/-/11111111/%2Fpets",
			method:               http.MethodGet,
			expectedAPIRoot:      "platform/api",
			expectApiLookup:      true,
			expectSingleLookup:   false,
			expectEndpointLookup: true,
		},
		{
			name:                 "monorepo spec route resolves api path and artifact",
			path:                 "/v1/specs/repo/-/platform/api/-/openapi.json",
			method:               http.MethodGet,
			expectedAPIRoot:      "platform/api",
			expectApiLookup:      true,
			expectSingleLookup:   false,
			expectEndpointLookup: false,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			readStore := &fakeReadRouteStore{
				resolved: store.ResolvedReadSelector{
					RepoID:   101,
					Revision: store.Revision{ID: 44},
				},
				apiSpecRevisionIDByRepoAndRootPathResult: 401,
				artifact: store.SpecArtifact{
					APISpecRevisionID: 401,
					SpecJSON:          []byte(`{"openapi":"3.1.0","paths":{}}`),
					SpecYAML:          "openapi: 3.1.0\npaths: {}\n",
					ETag:              "\"etag-401\"",
				},
				endpoint: store.EndpointIndexRecord{
					Method:  "get",
					Path:    "/pets",
					RawJSON: []byte(`{"responses":{"200":{"description":"ok"}}}`),
				},
				endpointFound: true,
			}
			server := newReadRouteTestServer(readStore)

			resp, err := server.App().Test(httptest.NewRequest(testCase.method, testCase.path, nil), -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, string(body))
			}

			if testCase.expectApiLookup {
				if len(readStore.apiSpecLookupInputs) != 1 {
					t.Fatalf("expected one API lookup, got %d", len(readStore.apiSpecLookupInputs))
				}
				if readStore.apiSpecLookupInputs[0].APIRoot != testCase.expectedAPIRoot {
					t.Fatalf("expected api root %q, got %q", testCase.expectedAPIRoot, readStore.apiSpecLookupInputs[0].APIRoot)
				}
				if testCase.expectEndpointLookup && readStore.lastEndpointLookupAPISpec.IngestEventID != 401 {
					t.Fatalf("expected api-spec endpoint revision id 401, got %d", readStore.lastEndpointLookupAPISpec.IngestEventID)
				}
				if readStore.lastEndpointLookup.IngestEventID != 0 {
					t.Fatalf("expected no single-spec endpoint lookup, got %+v", readStore.lastEndpointLookup)
				}
				if !testCase.expectEndpointLookup && readStore.lastEndpointLookupAPISpec != (struct {
					IngestEventID int64
					Method        string
					Path          string
				}{}) {
					t.Fatalf("expected no api-spec endpoint lookup, got %+v", readStore.lastEndpointLookupAPISpec)
				}
			}

			if testCase.expectSingleLookup {
				if len(readStore.apiSpecLookupInputs) != 0 {
					t.Fatalf("expected no API lookup, got %d", len(readStore.apiSpecLookupInputs))
				}
				if readStore.lastEndpointLookup.IngestEventID != 44 {
					t.Fatalf("expected single-spec endpoint lookup by repo revision, got %d", readStore.lastEndpointLookup.IngestEventID)
				}
			}
		})
	}
}

func TestReadRoutes_APISpecListing_RouteModesAndDeletedVisibility(t *testing.T) {
	t.Parallel()

	shortSHA := "11111111"

	listing := []store.APISpecListing{
		{
			API:    "apis/pets/openapi.yaml",
			Status: "active",
			LastProcessedRevision: &store.APISpecRevisionMetadata{
				APISpecRevisionID: 1001,
				IngestEventID:     501,
				IngestEventSHA:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				IngestEventBranch: "main",
			},
		},
		{
			API:    "apis/deleted/openapi.yaml",
			Status: "deleted",
		},
	}

	testCases := []struct {
		name                    string
		path                    string
		expectedNoSelectorInput store.ResolveReadSelectorInput
		expectNoSelectorListing bool
		expectSelectorListing   bool
	}{
		{
			name: "no selector listing resolves main and includes deleted api",
			path: "/v1/specs/repo/apis",
			expectedNoSelectorInput: store.ResolveReadSelectorInput{
				RepoPath:   "repo",
				NoSelector: true,
			},
			expectNoSelectorListing: true,
		},
		{
			name: "selector listing resolves selector revision",
			path: "/v1/specs/repo/" + shortSHA + "/apis",
			expectedNoSelectorInput: store.ResolveReadSelectorInput{
				RepoPath: "repo",
				Selector: shortSHA,
			},
			expectSelectorListing: true,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			readStore := &fakeReadRouteStore{
				resolved:                store.ResolvedReadSelector{RepoID: 88, Revision: store.Revision{ID: 77}},
				listingResult:           listing,
				listingByRevisionResult: listing,
				listingByRepoErr:        nil,
				listingByRevisionErr:    nil,
			}
			server := newReadRouteTestServer(readStore)

			resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, testCase.path, nil), -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
			}

			var got []apiSpecListingResponse
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatalf("decode api listing response: %v", err)
			}
			if !reflect.DeepEqual(got, []apiSpecListingResponse{
				{
					API:    "apis/pets/openapi.yaml",
					Status: "active",
					LastProcessedRevision: &apiSpecRevisionMetadataResponse{
						APISpecRevisionID: 1001,
						IngestEventID:     501,
						IngestEventSHA:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
						IngestEventBranch: "main",
					},
				},
				{
					API:    "apis/deleted/openapi.yaml",
					Status: "deleted",
				},
			}) {
				t.Fatalf("unexpected response body: %+v", got)
			}

			if len(readStore.resolveInputs) != 1 {
				t.Fatalf("expected one selector resolution call, got %d", len(readStore.resolveInputs))
			}
			if !reflect.DeepEqual(readStore.resolveInputs[0], testCase.expectedNoSelectorInput) {
				t.Fatalf("unexpected selector resolution input: %+v", readStore.resolveInputs[0])
			}

			if testCase.expectNoSelectorListing {
				if len(readStore.listingInputs) != 1 || readStore.listingInputs[0] != 88 {
					t.Fatalf("expected one listing call with repo_id=88, got %v", readStore.listingInputs)
				}
				if len(readStore.listingByRevisionInputs) != 0 {
					t.Fatalf("did not expect selector listing call, got %d", len(readStore.listingByRevisionInputs))
				}
				return
			}

			if testCase.expectSelectorListing {
				if len(readStore.listingByRevisionInputs) != 1 {
					t.Fatalf("expected one revision listing call, got %d", len(readStore.listingByRevisionInputs))
				}
				if readStore.listingByRevisionInputs[0].repoID != 88 || readStore.listingByRevisionInputs[0].revisionID != 77 {
					t.Fatalf("expected revision listing call for repo_id=88 ingest_event_id=77, got %+v", readStore.listingByRevisionInputs[0])
				}
				if len(readStore.listingInputs) != 0 {
					t.Fatalf("did not expect no-selector listing call, got %d", len(readStore.listingInputs))
				}
			}
		})
	}
}

func TestReadRoutes_RejectMalformedMonorepoDelimiter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		path string
	}{
		{
			name: "missing closing delimiter",
			path: "/v1/routes/repo/-/platform/api/%2Fpets",
		},
		{
			name: "empty api root",
			path: "/v1/specs/repo/-/-/openapi.json",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			readStore := &fakeReadRouteStore{
				resolved: store.ResolvedReadSelector{
					RepoID:   101,
					Revision: store.Revision{ID: 44},
				},
			}
			server := newReadRouteTestServer(readStore)

			resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, testCase.path, nil), -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", resp.StatusCode)
			}
			if len(readStore.resolveInputs) != 0 {
				t.Fatalf("expected malformed delimiter path to reject before selector resolution")
			}
		})
	}
}

func TestReadRoutes_StatusMappingForSelectorAndArtifactErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		resolveErr   error
		artifactErr  error
		expectedCode int
	}{
		{
			name: "selector not found returns 404",
			resolveErr: &store.SelectorResolutionError{
				Code: store.SelectorResolutionNotFound,
			},
			expectedCode: http.StatusNotFound,
		},
		{
			name: "selector unprocessed returns 409",
			resolveErr: &store.SelectorResolutionError{
				Code: store.SelectorResolutionUnprocessed,
			},
			expectedCode: http.StatusConflict,
		},
		{
			name: "selector invalid input returns 400",
			resolveErr: &store.SelectorResolutionError{
				Code: store.SelectorResolutionInvalidInput,
			},
			expectedCode: http.StatusBadRequest,
		},
		{
			name:         "artifact missing returns 404",
			artifactErr:  fmt.Errorf("%w: ingest_event_id=%d", store.ErrSpecArtifactNotFound, 21),
			expectedCode: http.StatusNotFound,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			readStore := &fakeReadRouteStore{
				resolved:    store.ResolvedReadSelector{Revision: store.Revision{ID: 21}},
				resolveErr:  testCase.resolveErr,
				artifactErr: testCase.artifactErr,
			}
			server := newReadRouteTestServer(readStore)

			req := httptest.NewRequest(http.MethodGet, "/v1/specs/repo/deadbeef/openapi.json", nil)
			resp, err := server.App().Test(req, -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != testCase.expectedCode {
				t.Fatalf("expected status %d, got %d", testCase.expectedCode, resp.StatusCode)
			}
		})
	}
}

func TestSpecRoutes_ETagSupport(t *testing.T) {
	t.Parallel()

	readStore := &fakeReadRouteStore{
		resolved: store.ResolvedReadSelector{Revision: store.Revision{ID: 33}},
		artifact: store.SpecArtifact{
			APISpecRevisionID: 33,
			SpecJSON:          []byte(`{"openapi":"3.1.0","paths":{}}`),
			SpecYAML:          "openapi: 3.1.0\npaths: {}\n",
			ETag:              "\"etag-33\"",
		},
	}
	server := newReadRouteTestServer(readStore)

	etagRequest := httptest.NewRequest(http.MethodGet, "/v1/specs/repo/openapi.json", nil)
	etagRequest.Header.Set("If-None-Match", "\"etag-33\"")

	etagResponse, err := server.App().Test(etagRequest, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer etagResponse.Body.Close()

	if etagResponse.StatusCode != http.StatusNotModified {
		t.Fatalf("expected status 304, got %d", etagResponse.StatusCode)
	}
	if got := etagResponse.Header.Get("ETag"); got != "\"etag-33\"" {
		t.Fatalf("expected ETag header %q, got %q", "\"etag-33\"", got)
	}

	normalRequest := httptest.NewRequest(http.MethodGet, "/v1/specs/repo/openapi.yaml", nil)
	normalResponse, err := server.App().Test(normalRequest, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer normalResponse.Body.Close()

	if normalResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", normalResponse.StatusCode)
	}
	if got := normalResponse.Header.Get("ETag"); got != "\"etag-33\"" {
		t.Fatalf("expected ETag header %q, got %q", "\"etag-33\"", got)
	}

	body, err := io.ReadAll(normalResponse.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if string(body) != "openapi: 3.1.0\npaths: {}\n" {
		t.Fatalf("unexpected spec yaml body: %s", string(body))
	}
}

func TestOperationRoute_UsesRequestMethodAndFormatAddon(t *testing.T) {
	t.Parallel()

	readStore := &fakeReadRouteStore{
		resolved: store.ResolvedReadSelector{Revision: store.Revision{ID: 88}},
		endpoint: store.EndpointIndexRecord{
			Method:  "post",
			Path:    "/pets",
			RawJSON: []byte(`{"operationId":"createPet","responses":{"201":{"description":"created"}}}`),
		},
		endpointFound: true,
	}
	server := newReadRouteTestServer(readStore)

	jsonReq := httptest.NewRequest(http.MethodPost, "/v1/routes/repo/%2Fpets", nil)
	jsonResp, err := server.App().Test(jsonReq, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer jsonResp.Body.Close()

	if jsonResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", jsonResp.StatusCode)
	}
	var jsonBody map[string]any
	if err := json.NewDecoder(jsonResp.Body).Decode(&jsonBody); err != nil {
		t.Fatalf("decode json response: %v", err)
	}
	if readStore.lastEndpointLookup.Method != "post" || readStore.lastEndpointLookup.Path != "/pets" {
		t.Fatalf("unexpected endpoint lookup key: %+v", readStore.lastEndpointLookup)
	}
	if _, ok := jsonBody["paths"].(map[string]any); !ok {
		t.Fatalf("expected paths object in operation slice")
	}

	yamlReq := httptest.NewRequest(http.MethodPatch, "/v1/routes/repo/11111111/%2Fpets.yaml", nil)
	yamlResp, err := server.App().Test(yamlReq, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer yamlResp.Body.Close()

	if yamlResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", yamlResp.StatusCode)
	}
	body, _ := io.ReadAll(yamlResp.Body)
	bodyText := string(body)
	if strings.TrimSpace(bodyText) == "" || !strings.Contains(bodyText, "paths:") {
		t.Fatalf("expected yaml payload body, got %q", bodyText)
	}
	if readStore.lastEndpointLookup.Method != "patch" || readStore.lastEndpointLookup.Path != "/pets" {
		t.Fatalf("unexpected endpoint lookup key for patch: %+v", readStore.lastEndpointLookup)
	}
}

func TestOperationSlice_DefaultJSONAndYAMLAddon(t *testing.T) {
	t.Parallel()

	readStore := &fakeReadRouteStore{
		resolved: store.ResolvedReadSelector{Revision: store.Revision{ID: 99}},
		endpoint: store.EndpointIndexRecord{
			Method:  "get",
			Path:    "/pets/{id}",
			RawJSON: []byte(`{"responses":{"200":{"description":"ok"}}}`),
		},
		endpointFound: true,
	}
	server := newReadRouteTestServer(readStore)

	jsonReq := httptest.NewRequest(http.MethodGet, "/v1/routes/repo/%2Fpets%2F%7Bid%7D", nil)
	jsonResp, err := server.App().Test(jsonReq, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer jsonResp.Body.Close()

	if jsonResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", jsonResp.StatusCode)
	}
	if readStore.lastEndpointLookup.Method != "get" || readStore.lastEndpointLookup.Path != "/pets/{id}" {
		t.Fatalf("unexpected endpoint lookup key: %+v", readStore.lastEndpointLookup)
	}

	var jsonBody map[string]any
	if err := json.NewDecoder(jsonResp.Body).Decode(&jsonBody); err != nil {
		t.Fatalf("decode json operation response: %v", err)
	}
	if _, ok := jsonBody["paths"].(map[string]any); !ok {
		t.Fatalf("expected paths object in operation slice")
	}

	yamlReq := httptest.NewRequest(http.MethodGet, "/v1/routes/repo/11111111/%2Fpets%2F%7Bid%7D.yaml", nil)
	yamlResp, err := server.App().Test(yamlReq, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer yamlResp.Body.Close()

	if yamlResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", yamlResp.StatusCode)
	}
	if !strings.Contains(yamlResp.Header.Get("Content-Type"), "application/yaml") {
		t.Fatalf("expected yaml content type, got %q", yamlResp.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(yamlResp.Body)
	if !strings.Contains(string(body), "paths:") {
		t.Fatalf("expected yaml paths payload, got %q", string(body))
	}
}

func TestOperationRoute_SelectorNotFoundFallsBackToNoSelector(t *testing.T) {
	t.Parallel()

	readStore := &fakeReadRouteStore{
		resolveFn: func(input store.ResolveReadSelectorInput) (store.ResolvedReadSelector, error) {
			if input.NoSelector {
				return store.ResolvedReadSelector{Revision: store.Revision{ID: 55}}, nil
			}
			if input.Selector == "v1" {
				return store.ResolvedReadSelector{}, store.ErrSelectorNotFound
			}
			return store.ResolvedReadSelector{Revision: store.Revision{ID: 77}}, nil
		},
		endpoint: store.EndpointIndexRecord{
			Method:  "get",
			Path:    "/pets",
			RawJSON: []byte(`{"responses":{"200":{"description":"ok"}}}`),
		},
		endpointFound: true,
	}
	server := newReadRouteTestServer(readStore)

	req := httptest.NewRequest(http.MethodGet, "/v1/routes/repo/v1/%2Fpets", nil)
	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if len(readStore.resolveInputs) != 2 {
		t.Fatalf("expected two selector resolution calls, got %d", len(readStore.resolveInputs))
	}
	if readStore.resolveInputs[0].Selector != "v1" || readStore.resolveInputs[0].NoSelector {
		t.Fatalf("expected first resolution to use selector v1, got %+v", readStore.resolveInputs[0])
	}
	if !readStore.resolveInputs[1].NoSelector {
		t.Fatalf("expected second resolution to use no-selector fallback, got %+v", readStore.resolveInputs[1])
	}
	if readStore.lastEndpointLookup.Method != "get" || readStore.lastEndpointLookup.Path != "/pets" {
		t.Fatalf("unexpected endpoint lookup after fallback: %+v", readStore.lastEndpointLookup)
	}
}

func TestReadRoutes_SelectorSpecRouteBypassesOperationRoute(t *testing.T) {
	t.Parallel()

	readStore := &fakeReadRouteStore{
		resolved: store.ResolvedReadSelector{Revision: store.Revision{ID: 77}},
		artifact: store.SpecArtifact{
			APISpecRevisionID: 77,
			SpecJSON:          []byte(`{"openapi":"3.1.0","paths":{}}`),
			SpecYAML:          "openapi: 3.1.0\npaths: {}\n",
			ETag:              "\"etag-77\"",
		},
	}
	server := newReadRouteTestServer(readStore)

	req := httptest.NewRequest(http.MethodGet, "/v1/specs/repo/11111111/openapi.json", nil)
	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if len(readStore.resolveInputs) != 1 {
		t.Fatalf("expected one selector resolution call, got %d", len(readStore.resolveInputs))
	}
	if readStore.resolveInputs[0].Selector != "11111111" || readStore.resolveInputs[0].NoSelector {
		t.Fatalf("expected selector resolution for short SHA selector, got %+v", readStore.resolveInputs[0])
	}
}

func newReadRouteTestServer(readStore readRouteStore) *Server {
	cfg := config.Config{HTTPAddr: ":8080"}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Store{})
	server.readStore = readStore
	return server
}

type fakeReadRouteStore struct {
	resolveInputs []store.ResolveReadSelectorInput
	resolveErr    error
	resolved      store.ResolvedReadSelector
	resolveFn     func(input store.ResolveReadSelectorInput) (store.ResolvedReadSelector, error)

	apiSpecLookupInputs                      []apiSpecLookupInput
	apiSpecRevisionIDByRepoAndRootPathResult int64
	apiSpecRevisionIDByRepoAndRootPathErr    error

	artifactErr error
	artifact    store.SpecArtifact

	endpointErr        error
	endpoint           store.EndpointIndexRecord
	endpointFound      bool
	lastEndpointLookup struct {
		IngestEventID int64
		Method        string
		Path          string
	}
	lastEndpointLookupAPISpec struct {
		IngestEventID int64
		Method        string
		Path          string
	}

	listingInputs           []int64
	listingByRevisionInputs []apiSpecListingByRevisionInput
	listingByRepoErr        error
	listingByRevisionErr    error
	listingResult           []store.APISpecListing
	listingByRevisionResult []store.APISpecListing
}

type apiSpecListingByRevisionInput struct {
	repoID     int64
	revisionID int64
}

func (f *fakeReadRouteStore) ResolveReadSelector(
	_ context.Context,
	input store.ResolveReadSelectorInput,
) (store.ResolvedReadSelector, error) {
	f.resolveInputs = append(f.resolveInputs, input)
	if f.resolveFn != nil {
		return f.resolveFn(input)
	}
	if f.resolveErr != nil {
		return store.ResolvedReadSelector{}, f.resolveErr
	}
	return f.resolved, nil
}

func (f *fakeReadRouteStore) GetSpecArtifactByRevisionID(_ context.Context, revisionID int64) (store.SpecArtifact, error) {
	if f.artifactErr != nil {
		return store.SpecArtifact{}, f.artifactErr
	}
	if f.artifact.APISpecRevisionID == 0 {
		return store.SpecArtifact{}, fmt.Errorf("%w: ingest_event_id=%d", store.ErrSpecArtifactNotFound, revisionID)
	}
	return f.artifact, nil
}

func (f *fakeReadRouteStore) GetSpecArtifactByAPISpecRevisionID(_ context.Context, apiSpecRevisionID int64) (store.SpecArtifact, error) {
	if f.artifactErr != nil {
		return store.SpecArtifact{}, f.artifactErr
	}
	if f.artifact.APISpecRevisionID == 0 {
		return store.SpecArtifact{}, fmt.Errorf("%w: api_spec_revision_id=%d", store.ErrSpecArtifactNotFound, apiSpecRevisionID)
	}
	return f.artifact, nil
}

func (f *fakeReadRouteStore) GetAPISpecRevisionIDByRepoAndRootPath(
	_ context.Context,
	repoID int64,
	apiRootPath string,
	revisionID int64,
) (int64, error) {
	f.apiSpecLookupInputs = append(f.apiSpecLookupInputs, apiSpecLookupInput{
		RepoID:   repoID,
		APIRoot:  apiRootPath,
		Revision: revisionID,
	})
	if f.apiSpecRevisionIDByRepoAndRootPathErr != nil {
		return 0, f.apiSpecRevisionIDByRepoAndRootPathErr
	}
	if f.apiSpecRevisionIDByRepoAndRootPathResult != 0 {
		return f.apiSpecRevisionIDByRepoAndRootPathResult, nil
	}
	return 0, fmt.Errorf("%w: repo_id=%d api=%q ingest_event_id=%d", store.ErrAPISpecNotFound, repoID, apiRootPath, revisionID)
}

func (f *fakeReadRouteStore) GetEndpointIndexByMethodPath(
	_ context.Context,
	revisionID int64,
	method string,
	path string,
) (store.EndpointIndexRecord, bool, error) {
	f.lastEndpointLookup.IngestEventID = revisionID
	f.lastEndpointLookup.Method = method
	f.lastEndpointLookup.Path = path

	if f.endpointErr != nil {
		return store.EndpointIndexRecord{}, false, f.endpointErr
	}
	if !f.endpointFound {
		return store.EndpointIndexRecord{}, false, nil
	}
	return f.endpoint, true, nil
}

func (f *fakeReadRouteStore) GetEndpointIndexByMethodPathForAPISpecRevision(
	_ context.Context,
	apiSpecRevisionID int64,
	method string,
	path string,
) (store.EndpointIndexRecord, bool, error) {
	f.lastEndpointLookupAPISpec.IngestEventID = apiSpecRevisionID
	f.lastEndpointLookupAPISpec.Method = method
	f.lastEndpointLookupAPISpec.Path = path

	if f.endpointErr != nil {
		return store.EndpointIndexRecord{}, false, f.endpointErr
	}
	if !f.endpointFound {
		return store.EndpointIndexRecord{}, false, nil
	}
	return f.endpoint, true, nil
}

func (f *fakeReadRouteStore) ListAPISpecListingByRepo(
	_ context.Context,
	repoID int64,
) ([]store.APISpecListing, error) {
	f.listingInputs = append(f.listingInputs, repoID)
	if f.listingByRepoErr != nil {
		return nil, f.listingByRepoErr
	}
	result := make([]store.APISpecListing, len(f.listingResult))
	copy(result, f.listingResult)
	return result, nil
}

func (f *fakeReadRouteStore) ListAPISpecListingByRepoAtRevision(
	_ context.Context,
	repoID int64,
	revisionID int64,
) ([]store.APISpecListing, error) {
	f.listingByRevisionInputs = append(
		f.listingByRevisionInputs,
		apiSpecListingByRevisionInput{
			repoID:     repoID,
			revisionID: revisionID,
		},
	)
	if f.listingByRevisionErr != nil {
		return nil, f.listingByRevisionErr
	}
	result := make([]store.APISpecListing, len(f.listingByRevisionResult))
	copy(result, f.listingByRevisionResult)
	return result, nil
}

type apiSpecLookupInput struct {
	RepoID   int64
	APIRoot  string
	Revision int64
}

func TestIfNoneMatchMatches(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		ifNoneMatch string
		etag        string
		expected    bool
	}{
		{
			name:        "exact match",
			ifNoneMatch: "\"etag-a\"",
			etag:        "\"etag-a\"",
			expected:    true,
		},
		{
			name:        "weak match",
			ifNoneMatch: "W/\"etag-a\"",
			etag:        "\"etag-a\"",
			expected:    true,
		},
		{
			name:        "wildcard match",
			ifNoneMatch: "*",
			etag:        "\"etag-a\"",
			expected:    true,
		},
		{
			name:        "no match",
			ifNoneMatch: "\"etag-b\"",
			etag:        "\"etag-a\"",
			expected:    false,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			actual := ifNoneMatchMatches(testCase.ifNoneMatch, testCase.etag)
			if actual != testCase.expected {
				t.Fatalf("expected %t, got %t", testCase.expected, actual)
			}
		})
	}
}

func TestWriteReadRouteError_DefaultInternalServerError(t *testing.T) {
	t.Parallel()

	server := newReadRouteTestServer(&fakeReadRouteStore{})
	req := httptest.NewRequest(http.MethodGet, "/v1/specs/repo/openapi.json", nil)

	server.readStore = &fakeReadRouteStore{
		resolveErr: errors.New("unexpected"),
	}
	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", resp.StatusCode)
	}
}
