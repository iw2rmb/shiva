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
	"testing"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/store"
)

func TestReadRoutes_SelectorModesResolveExpectedInput(t *testing.T) {
	t.Parallel()

	shaSelector := "1111111111111111111111111111111111111111"
	testCases := []struct {
		name          string
		path          string
		expectedInput store.ResolveReadSelectorInput
	}{
		{
			name: "sha selector route",
			path: fmt.Sprintf("/tenant-a/acme%%2Fplatform-api/%s/endpoints", shaSelector),
			expectedInput: store.ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  shaSelector,
			},
		},
		{
			name: "branch selector route",
			path: "/tenant-a/acme%2Fplatform-api/release/endpoints",
			expectedInput: store.ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "release",
			},
		},
		{
			name: "latest selector route",
			path: "/tenant-a/acme%2Fplatform-api/latest/endpoints",
			expectedInput: store.ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "latest",
			},
		},
		{
			name: "no-selector route uses main selector mode",
			path: "/tenant-a/acme%2Fplatform-api/endpoints",
			expectedInput: store.ResolveReadSelectorInput{
				TenantKey:  "tenant-a",
				RepoPath:   "acme/platform-api",
				NoSelector: true,
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			readStore := &fakeReadRouteStore{
				resolved: store.ResolvedReadSelector{Revision: store.Revision{ID: 21}},
				list: []store.EndpointIndexRecord{
					{
						Method:  "get",
						Path:    "/pets",
						RawJSON: []byte(`{"responses":{"200":{"description":"ok"}}}`),
					},
				},
			}
			server := newReadRouteTestServer(readStore)

			req := httptest.NewRequest(http.MethodGet, testCase.path, nil)
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

			req := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/latest/spec.json", nil)
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

func TestGetSpecJSON_ETagSupport(t *testing.T) {
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

	etagRequest := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/latest/spec.json", nil)
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

	normalRequest := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/latest/spec.json", nil)
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
	if string(body) != `{"openapi":"3.1.0","paths":{}}` {
		t.Fatalf("unexpected spec.json body: %s", string(body))
	}
}

func TestGetSpecYAML_ETagSupport(t *testing.T) {
	t.Parallel()

	readStore := &fakeReadRouteStore{
		resolved: store.ResolvedReadSelector{Revision: store.Revision{ID: 34}},
		artifact: store.SpecArtifact{
			RevisionID: 34,
			SpecJSON:   []byte(`{"openapi":"3.1.0","paths":{}}`),
			SpecYAML:   "openapi: 3.1.0\npaths: {}\n",
			ETag:       "\"etag-34\"",
		},
	}
	server := newReadRouteTestServer(readStore)

	etagRequest := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/latest/spec.yaml", nil)
	etagRequest.Header.Set("If-None-Match", "W/\"etag-34\"")

	etagResponse, err := server.App().Test(etagRequest, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer etagResponse.Body.Close()

	if etagResponse.StatusCode != http.StatusNotModified {
		t.Fatalf("expected status 304, got %d", etagResponse.StatusCode)
	}
	if got := etagResponse.Header.Get("ETag"); got != "\"etag-34\"" {
		t.Fatalf("expected ETag header %q, got %q", "\"etag-34\"", got)
	}

	normalRequest := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/latest/spec.yaml", nil)
	normalResponse, err := server.App().Test(normalRequest, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer normalResponse.Body.Close()

	if normalResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", normalResponse.StatusCode)
	}

	body, err := io.ReadAll(normalResponse.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	if string(body) != "openapi: 3.1.0\npaths: {}\n" {
		t.Fatalf("unexpected spec.yaml body: %s", string(body))
	}
}

func TestGetEndpointBySelector_DecodesMethodAndPath(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/latest/endpoints/GET/%2Fpets%2F%7Bid%7D", nil)
	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if readStore.lastEndpointLookup.Method != "get" || readStore.lastEndpointLookup.Path != "/pets/{id}" {
		t.Fatalf("unexpected endpoint lookup key: %+v", readStore.lastEndpointLookup)
	}

	var body endpointResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode endpoint response: %v", err)
	}
	if body.Path != "/pets/{id}" || body.Method != "get" {
		t.Fatalf("unexpected endpoint response: %+v", body)
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

	artifactErr error
	artifact    store.SpecArtifact

	listErr error
	list    []store.EndpointIndexRecord

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

func (f *fakeReadRouteStore) ListEndpointIndexByRevision(_ context.Context, _ int64) ([]store.EndpointIndexRecord, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.list, nil
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
	req := httptest.NewRequest(http.MethodGet, "/tenant-a/repo/latest/spec.json", nil)

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
