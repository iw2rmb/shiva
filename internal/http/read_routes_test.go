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

	shaSelector := "1111111111111111111111111111111111111111"
	testCases := []struct {
		name          string
		method        string
		path          string
		expectedInput store.ResolveReadSelectorInput
	}{
		{
			name:   "no-selector spec route",
			method: http.MethodGet,
			path:   "/tenant-a/acme%2Fplatform-api.json",
			expectedInput: store.ResolveReadSelectorInput{
				TenantKey:  "tenant-a",
				RepoPath:   "acme/platform-api",
				NoSelector: true,
			},
		},
		{
			name:   "selector spec route",
			method: http.MethodGet,
			path:   "/tenant-a/acme%2Fplatform-api/latest.json",
			expectedInput: store.ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "latest",
			},
		},
		{
			name:   "selector spec route with sha",
			method: http.MethodGet,
			path:   fmt.Sprintf("/tenant-a/acme%%2Fplatform-api/%s.yaml", shaSelector),
			expectedInput: store.ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  shaSelector,
			},
		},
		{
			name:   "operation slice route without selector",
			method: http.MethodGet,
			path:   "/tenant-a/acme%2Fplatform-api/%2Fpets",
			expectedInput: store.ResolveReadSelectorInput{
				TenantKey:  "tenant-a",
				RepoPath:   "acme/platform-api",
				NoSelector: true,
			},
		},
		{
			name:   "operation slice route with selector",
			method: http.MethodGet,
			path:   "/tenant-a/acme%2Fplatform-api/release/%2Fpets",
			expectedInput: store.ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "release",
			},
		},
		{
			name:   "operation route uses request method",
			method: http.MethodPost,
			path:   "/tenant-a/acme%2Fplatform-api/release/%2Fpets",
			expectedInput: store.ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "release",
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
					RevisionID: 21,
					SpecJSON:   []byte(`{"openapi":"3.1.0","paths":{}}`),
					SpecYAML:   "openapi: 3.1.0\npaths: {}\n",
					ETag:       "\"etag-21\"",
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
			artifactErr:  fmt.Errorf("%w: revision_id=%d", store.ErrSpecArtifactNotFound, 21),
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

			req := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/latest.json", nil)
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
			RevisionID: 33,
			SpecJSON:   []byte(`{"openapi":"3.1.0","paths":{}}`),
			SpecYAML:   "openapi: 3.1.0\npaths: {}\n",
			ETag:       "\"etag-33\"",
		},
	}
	server := newReadRouteTestServer(readStore)

	etagRequest := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/latest.json", nil)
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

	normalRequest := httptest.NewRequest(http.MethodGet, "/tenant-a/repo.yaml", nil)
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

	jsonReq := httptest.NewRequest(http.MethodPost, "/tenant-a/repo/%2Fpets", nil)
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

	yamlReq := httptest.NewRequest(http.MethodPatch, "/tenant-a/repo/release/%2Fpets.yaml", nil)
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
	if !strings.Contains(bodyText, "paths:") {
		t.Fatalf("expected yaml to contain paths key, got %q", bodyText)
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

	jsonReq := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/%2Fpets%2F%7Bid%7D", nil)
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

	yamlReq := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/release/%2Fpets%2F%7Bid%7D.yaml", nil)
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
			Path:    "/v1/pets",
			RawJSON: []byte(`{"responses":{"200":{"description":"ok"}}}`),
		},
		endpointFound: true,
	}
	server := newReadRouteTestServer(readStore)

	req := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/v1/pets", nil)
	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
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
	if readStore.lastEndpointLookup.Method != "get" || readStore.lastEndpointLookup.Path != "/v1/pets" {
		t.Fatalf("unexpected endpoint lookup after fallback: %+v", readStore.lastEndpointLookup)
	}
}

func TestReadRoutes_SelectorSpecRouteBypassesOperationRoute(t *testing.T) {
	t.Parallel()

	readStore := &fakeReadRouteStore{
		resolved: store.ResolvedReadSelector{Revision: store.Revision{ID: 77}},
		artifact: store.SpecArtifact{
			RevisionID: 77,
			SpecJSON:   []byte(`{"openapi":"3.1.0","paths":{}}`),
			SpecYAML:   "openapi: 3.1.0\npaths: {}\n",
			ETag:       "\"etag-77\"",
		},
	}
	server := newReadRouteTestServer(readStore)

	req := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/release.json", nil)
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
	if readStore.resolveInputs[0].Selector != "release" || readStore.resolveInputs[0].NoSelector {
		t.Fatalf("expected selector resolution for release selector, got %+v", readStore.resolveInputs[0])
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

	artifactErr error
	artifact    store.SpecArtifact

	endpointErr        error
	endpoint           store.EndpointIndexRecord
	endpointFound      bool
	lastEndpointLookup struct {
		RevisionID int64
		Method     string
		Path       string
	}
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
	if f.artifact.RevisionID == 0 {
		return store.SpecArtifact{}, fmt.Errorf("%w: revision_id=%d", store.ErrSpecArtifactNotFound, revisionID)
	}
	return f.artifact, nil
}

func (f *fakeReadRouteStore) GetEndpointIndexByMethodPath(
	_ context.Context,
	revisionID int64,
	method string,
	path string,
) (store.EndpointIndexRecord, bool, error) {
	f.lastEndpointLookup.RevisionID = revisionID
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
	req := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/latest.json", nil)

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
