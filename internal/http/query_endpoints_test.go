package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/config"
	"github.com/iw2rmb/shiva/internal/store"
)

func TestQueryEndpoints_GetSpec_ResolvesQueryAndWritesRequestedFormat(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		resolveSpecSnapshotsResult: store.ResolvedSpecSnapshots{
			Snapshot: store.ResolvedReadSnapshot{
				Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform"},
				Revision: store.Revision{ID: 42},
			},
			Candidates: []store.APISnapshot{
				{
					API:               "apis/pets/openapi.yaml",
					HasSnapshot:       true,
					APISpecRevisionID: 501,
				},
			},
		},
		specArtifactResult: store.SpecArtifact{
			APISpecRevisionID: 501,
			SpecJSON:          []byte(`{"openapi":"3.1.0","paths":{}}`),
			SpecYAML:          "openapi: 3.1.0\npaths: {}\n",
			ETag:              "\"etag-501\"",
		},
	}
	server := newQueryTestServer(readStore)

	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/spec?namespace=acme&repo=platform&api=apis%2Fpets%2Fopenapi.yaml&revision_id=42&format=yaml",
		nil,
	)
	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if !reflect.DeepEqual(readStore.resolveSpecSnapshotsInputs, []store.ResolveReadSnapshotInput{
		{
			Namespace:  "acme",
			Repo:       "platform",
			APIPath:    "apis/pets/openapi.yaml",
			RevisionID: 42,
		},
	}) {
		t.Fatalf("unexpected spec query input: %+v", readStore.resolveSpecSnapshotsInputs)
	}
	if !reflect.DeepEqual(readStore.specArtifactInputs, []int64{501}) {
		t.Fatalf("unexpected spec artifact lookup inputs: %+v", readStore.specArtifactInputs)
	}
	if got := resp.Header.Get("ETag"); got != "\"etag-501\"" {
		t.Fatalf("expected ETag header to be propagated, got %q", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "openapi: 3.1.0\npaths: {}\n" {
		t.Fatalf("unexpected response body %q", string(body))
	}
}

func TestQueryEndpoints_GetSpec_ETagShortCircuits(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		resolveSpecSnapshotsResult: store.ResolvedSpecSnapshots{
			Snapshot: store.ResolvedReadSnapshot{
				Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform"},
				Revision: store.Revision{ID: 42},
			},
			Candidates: []store.APISnapshot{
				{
					API:               "apis/pets/openapi.yaml",
					HasSnapshot:       true,
					APISpecRevisionID: 501,
				},
			},
		},
		specArtifactResult: store.SpecArtifact{
			APISpecRevisionID: 501,
			SpecJSON:          []byte(`{"openapi":"3.1.0","paths":{}}`),
			SpecYAML:          "openapi: 3.1.0\npaths: {}\n",
			ETag:              "\"etag-501\"",
		},
	}
	server := newQueryTestServer(readStore)

	req := httptest.NewRequest(http.MethodGet, "/v1/spec?namespace=acme&repo=platform", nil)
	req.Header.Set("If-None-Match", "\"etag-501\"")

	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("expected status 304, got %d", resp.StatusCode)
	}
}

func TestQueryEndpoints_GetSpec_AmbiguousWithoutAPI(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		resolveSpecSnapshotsResult: store.ResolvedSpecSnapshots{
			Snapshot: store.ResolvedReadSnapshot{
				Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform"},
				Revision: store.Revision{ID: 42},
			},
			Candidates: []store.APISnapshot{
				{API: "apis/pets/openapi.yaml", Status: "active", HasSnapshot: true, APISpecRevisionID: 501},
				{API: "apis/orders/openapi.yaml", Status: "active", HasSnapshot: true, APISpecRevisionID: 502},
			},
		},
	}
	server := newQueryTestServer(readStore)

	resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, "/v1/spec?namespace=acme&repo=platform", nil), -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}

	var body struct {
		Error      string                `json:"error"`
		Candidates []apiSnapshotResponse `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode ambiguity response: %v", err)
	}
	if body.Error == "" || len(body.Candidates) != 2 {
		t.Fatalf("unexpected ambiguity response: %+v", body)
	}
}

func TestQueryEndpoints_GetOperation_UsesOperationIDAndMethodPathSelectors(t *testing.T) {
	t.Parallel()

	t.Run("operation id", func(t *testing.T) {
		t.Parallel()

		readStore := &fakeQueryReadStore{
			resolveOperationByIDResult: store.ResolvedOperationCandidates{
				Snapshot: store.ResolvedReadSnapshot{
					Repo: store.Repo{Namespace: "acme", Repo: "platform"},
				},
				Candidates: []store.OperationSnapshot{
					{
						API:         "apis/pets/openapi.yaml",
						Method:      "get",
						Path:        "/pets",
						OperationID: "listPets",
						RawJSON:     []byte(`{"operationId":"listPets","responses":{"200":{"description":"ok"}}}`),
					},
				},
			},
		}
		server := newQueryTestServer(readStore)

		resp, err := server.App().Test(
			httptest.NewRequest(http.MethodGet, "/v1/operation?namespace=acme&repo=platform&sha=deadbeef&operation_id=listPets", nil),
			-1,
		)
		if err != nil {
			t.Fatalf("http test request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		if !reflect.DeepEqual(readStore.resolveOperationByIDInputs, []store.ResolveOperationByIDInput{
			{
				ResolveReadSnapshotInput: store.ResolveReadSnapshotInput{
					Namespace: "acme", Repo: "platform",
					SHA: "deadbeef",
				},
				OperationID: "listPets",
			},
		}) {
			t.Fatalf("unexpected operation id query input: %+v", readStore.resolveOperationByIDInputs)
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), `"operationId":"listPets"`) {
			t.Fatalf("unexpected response body %s", string(body))
		}
	})

	t.Run("method path", func(t *testing.T) {
		t.Parallel()

		readStore := &fakeQueryReadStore{
			resolveOperationByMethodPathResult: store.ResolvedOperationCandidates{
				Snapshot: store.ResolvedReadSnapshot{
					Repo: store.Repo{Namespace: "acme", Repo: "platform"},
				},
				Candidates: []store.OperationSnapshot{
					{
						API:         "apis/pets/openapi.yaml",
						Method:      "patch",
						Path:        "/pets/{id}",
						OperationID: "patchPet",
						RawJSON:     []byte(`{"operationId":"patchPet","responses":{"200":{"description":"ok"}}}`),
					},
				},
			},
		}
		server := newQueryTestServer(readStore)

		resp, err := server.App().Test(
			httptest.NewRequest(http.MethodGet, "/v1/operation?namespace=acme&repo=platform&method=PATCH&path=pets%2F%7Bid%7D", nil),
			-1,
		)
		if err != nil {
			t.Fatalf("http test request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		if !reflect.DeepEqual(readStore.resolveOperationByMethodPathInputs, []store.ResolveOperationByMethodPathInput{
			{
				ResolveReadSnapshotInput: store.ResolveReadSnapshotInput{
					Namespace: "acme", Repo: "platform",
				},
				Method: "patch",
				Path:   "/pets/{id}",
			},
		}) {
			t.Fatalf("unexpected method/path query input: %+v", readStore.resolveOperationByMethodPathInputs)
		}
	})
}

func TestQueryEndpoints_GetOperation_RejectsInvalidSelectorCombination(t *testing.T) {
	t.Parallel()

	server := newQueryTestServer(&fakeQueryReadStore{})

	resp, err := server.App().Test(
		httptest.NewRequest(http.MethodGet, "/v1/operation?namespace=acme&repo=platform&operation_id=listPets&method=get&path=%2Fpets", nil),
		-1,
	)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestQueryEndpoints_GetOperation_AmbiguityIncludesCandidates(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		resolveOperationByIDResult: store.ResolvedOperationCandidates{
			Snapshot: store.ResolvedReadSnapshot{
				Repo: store.Repo{Namespace: "acme", Repo: "platform"},
			},
			Candidates: []store.OperationSnapshot{
				{
					API:               "apis/pets/openapi.yaml",
					APISpecRevisionID: 501,
					IngestEventID:     42,
					IngestEventSHA:    "aaaaaaaa",
					IngestEventBranch: "main",
					Method:            "get",
					Path:              "/pets",
					OperationID:       "listPets",
					RawJSON:           []byte(`{"operationId":"listPets"}`),
				},
				{
					API:               "apis/orders/openapi.yaml",
					APISpecRevisionID: 502,
					IngestEventID:     42,
					IngestEventSHA:    "aaaaaaaa",
					IngestEventBranch: "main",
					Method:            "get",
					Path:              "/pets",
					OperationID:       "listPets",
					RawJSON:           []byte(`{"operationId":"listPets"}`),
				},
			},
		},
	}
	server := newQueryTestServer(readStore)

	resp, err := server.App().Test(
		httptest.NewRequest(http.MethodGet, "/v1/operation?namespace=acme&repo=platform&operation_id=listPets", nil),
		-1,
	)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}
}

func TestQueryEndpoints_ListAPIs_UsesResolvedSnapshot(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		resolveReadSnapshotResult: store.ResolvedReadSnapshot{
			Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform"},
			Revision: store.Revision{ID: 42},
		},
		apiInventoryResult: []store.APISnapshot{
			{
				API:               "apis/pets/openapi.yaml",
				Status:            "active",
				DisplayName:       "Pets API",
				HasSnapshot:       true,
				APISpecRevisionID: 501,
				IngestEventID:     42,
				IngestEventSHA:    "aaaaaaaa",
				IngestEventBranch: "main",
				SpecETag:          "\"etag-501\"",
				SpecSizeBytes:     123,
				OperationCount:    2,
			},
			{
				API:            "apis/deleted/openapi.yaml",
				Status:         "deleted",
				DisplayName:    "Deleted API",
				HasSnapshot:    false,
				OperationCount: 0,
			},
		},
	}
	server := newQueryTestServer(readStore)

	resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, "/v1/apis?namespace=acme&repo=platform&sha=deadbeef", nil), -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if !reflect.DeepEqual(readStore.resolveReadSnapshotInputs, []store.ResolveReadSnapshotInput{
		{
			Namespace: "acme", Repo: "platform",
			SHA: "deadbeef",
		},
	}) {
		t.Fatalf("unexpected snapshot query input: %+v", readStore.resolveReadSnapshotInputs)
	}
	if !reflect.DeepEqual(readStore.apiInventoryInputs, []apiInventoryInput{
		{RepoID: 77, RevisionID: 42},
	}) {
		t.Fatalf("unexpected api inventory input: %+v", readStore.apiInventoryInputs)
	}
}

func TestQueryEndpoints_ListOperations_ValidatesExplicitAPI(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		resolveReadSnapshotResult: store.ResolvedReadSnapshot{
			Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform"},
			Revision: store.Revision{ID: 42},
		},
		apiSnapshotResult: store.APISnapshot{
			API:               "apis/pets/openapi.yaml",
			HasSnapshot:       true,
			APISpecRevisionID: 501,
		},
		apiSnapshotFound: true,
		operationInventoryByAPIResult: []store.OperationSnapshot{
			{
				Namespace:         "acme",
				Repo:              "platform",
				API:               "apis/pets/openapi.yaml",
				Status:            "active",
				APISpecRevisionID: 501,
				IngestEventID:     42,
				IngestEventSHA:    "aaaaaaaa",
				IngestEventBranch: "main",
				Method:            "get",
				Path:              "/pets",
				OperationID:       "listPets",
				Summary:           "List pets",
				RawJSON:           []byte(`{"operationId":"listPets"}`),
			},
		},
	}
	server := newQueryTestServer(readStore)

	resp, err := server.App().Test(
		httptest.NewRequest(http.MethodGet, "/v1/operations?namespace=acme&repo=platform&api=apis%2Fpets%2Fopenapi.yaml", nil),
		-1,
	)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if !reflect.DeepEqual(readStore.apiSnapshotInputs, []apiSnapshotSelectionInput{
		{RepoID: 77, API: "apis/pets/openapi.yaml", RevisionID: 42},
	}) {
		t.Fatalf("unexpected explicit api validation inputs: %+v", readStore.apiSnapshotInputs)
	}
	if !reflect.DeepEqual(readStore.operationInventoryByAPIPageInputs, []operationInventoryByAPIPageInput{
		{
			RepoID:      77,
			API:         "apis/pets/openapi.yaml",
			RevisionID:  42,
			QueryPrefix: "",
			Limit:       200,
			Offset:      0,
		},
	}) {
		t.Fatalf("unexpected api-scoped operation inventory page inputs: %+v", readStore.operationInventoryByAPIPageInputs)
	}

	var rows []operationSnapshotResponse
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode operations response: %v", err)
	}
	if len(rows) != 1 || len(rows[0].Operation) == 0 {
		t.Fatalf("unexpected operations payload: %+v", rows)
	}
	if rows[0].Namespace != "acme" || rows[0].Repo != "platform" {
		t.Fatalf("expected operation row identity acme/platform, got namespace=%q repo=%q", rows[0].Namespace, rows[0].Repo)
	}
}

func TestQueryEndpoints_ListOperations_AllowsGlobalAndNamespaceScope(t *testing.T) {
	t.Parallel()

	t.Run("global", func(t *testing.T) {
		t.Parallel()

		readStore := &fakeQueryReadStore{
			operationCatalogInventoryResult: []store.OperationSnapshot{
				{
					Namespace:         "acme",
					Repo:              "platform",
					API:               "apis/pets/openapi.yaml",
					Status:            "active",
					APISpecRevisionID: 501,
					IngestEventID:     42,
					IngestEventSHA:    "aaaaaaaa",
					IngestEventBranch: "main",
					Method:            "get",
					Path:              "/pets",
					OperationID:       "listPets",
					RawJSON:           []byte(`{"operationId":"listPets"}`),
				},
			},
		}
		server := newQueryTestServer(readStore)

		resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, "/v1/operations", nil), -1)
		if err != nil {
			t.Fatalf("http test request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		if !reflect.DeepEqual(readStore.operationCatalogPageInputs, []operationCatalogPageInput{{
			Namespace:   "",
			Repo:        "",
			QueryPrefix: "",
			Limit:       200,
			Offset:      0,
		}}) {
			t.Fatalf("unexpected operation catalog page input: %+v", readStore.operationCatalogPageInputs)
		}
		if len(readStore.resolveReadSnapshotInputs) != 0 {
			t.Fatalf("did not expect snapshot resolution, got %+v", readStore.resolveReadSnapshotInputs)
		}
		var rows []operationSnapshotResponse
		if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
			t.Fatalf("decode operations response: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected one operation row, got %d", len(rows))
		}
		if rows[0].Namespace != "acme" || rows[0].Repo != "platform" {
			t.Fatalf("expected operation row identity acme/platform, got namespace=%q repo=%q", rows[0].Namespace, rows[0].Repo)
		}
	})

	t.Run("namespace scoped", func(t *testing.T) {
		t.Parallel()

		readStore := &fakeQueryReadStore{
			operationCatalogInventoryResult: []store.OperationSnapshot{
				{
					Namespace:         "acme",
					Repo:              "gateway",
					API:               "apis/gateway/openapi.yaml",
					Status:            "active",
					APISpecRevisionID: 9001,
					IngestEventID:     777,
					IngestEventSHA:    "deadbeef",
					IngestEventBranch: "main",
					Method:            "get",
					Path:              "/health",
					OperationID:       "health",
					RawJSON:           []byte(`{"operationId":"health"}`),
				},
			},
		}
		server := newQueryTestServer(readStore)

		resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, "/v1/operations?namespace=acme", nil), -1)
		if err != nil {
			t.Fatalf("http test request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}
		if !reflect.DeepEqual(readStore.operationCatalogPageInputs, []operationCatalogPageInput{{
			Namespace:   "acme",
			Repo:        "",
			QueryPrefix: "",
			Limit:       200,
			Offset:      0,
		}}) {
			t.Fatalf("unexpected operation catalog page input: %+v", readStore.operationCatalogPageInputs)
		}
		if len(readStore.resolveReadSnapshotInputs) != 0 {
			t.Fatalf("did not expect snapshot resolution, got %+v", readStore.resolveReadSnapshotInputs)
		}
		var rows []operationSnapshotResponse
		if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
			t.Fatalf("decode operations response: %v", err)
		}
		if len(rows) != 1 {
			t.Fatalf("expected one operation row, got %d", len(rows))
		}
		if rows[0].Namespace != "acme" || rows[0].Repo != "gateway" {
			t.Fatalf("expected operation row identity acme/gateway, got namespace=%q repo=%q", rows[0].Namespace, rows[0].Repo)
		}
	})
}

func TestQueryEndpoints_ListReposAndCatalogStatus_ReturnCatalogShapes(t *testing.T) {
	t.Parallel()

	now := time.Unix(1710000000, 0).UTC()
	openAPIChanged := true

	readStore := &fakeQueryReadStore{
		namespaceInventoryResult: store.NamespaceCatalogListResult{
			Items: []store.NamespaceCatalogEntry{
				{Namespace: "acme", RepoCount: 1, AllPending: false},
			},
			TotalCount: 1,
		},
		repoInventoryResult: []store.RepoCatalogEntry{
			{
				Repo: store.Repo{
					ID:              77,
					GitLabProjectID: 1001,
					Namespace:       "acme", Repo: "platform",
					DefaultBranch: "main",
				},
				OpenAPIForceRescan: true,
				ActiveAPICount:     2,
				HeadRevision: &store.CatalogRevisionState{
					ID:             91,
					SHA:            "aaaaaaaa",
					Status:         "processed",
					OpenAPIChanged: &openAPIChanged,
					ReceivedAt:     &now,
					ProcessedAt:    &now,
				},
				SnapshotRevision: &store.CatalogRevisionState{
					ID:             90,
					SHA:            "bbbbbbbb",
					Status:         "processed",
					OpenAPIChanged: &openAPIChanged,
					ProcessedAt:    &now,
				},
			},
		},
		catalogStatusResult: store.RepoCatalogFreshness{
			Repo: store.Repo{
				ID:              77,
				GitLabProjectID: 1001,
				Namespace:       "acme", Repo: "platform",
				DefaultBranch: "main",
			},
			OpenAPIForceRescan: true,
			ActiveAPICount:     2,
			HeadRevision: &store.CatalogRevisionState{
				ID:             91,
				SHA:            "aaaaaaaa",
				Status:         "processed",
				OpenAPIChanged: &openAPIChanged,
				ReceivedAt:     &now,
				ProcessedAt:    &now,
			},
			SnapshotRevision: &store.CatalogRevisionState{
				ID:             90,
				SHA:            "bbbbbbbb",
				Status:         "processed",
				OpenAPIChanged: &openAPIChanged,
				ProcessedAt:    &now,
			},
		},
	}
	server := newQueryTestServer(readStore)

	namespacesResp, err := server.App().Test(httptest.NewRequest(http.MethodGet, "/v1/namespaces", nil), -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer namespacesResp.Body.Close()
	if namespacesResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for namespaces, got %d", namespacesResp.StatusCode)
	}
	if !reflect.DeepEqual(readStore.namespaceInventoryInputs, []store.NamespaceCatalogListInput{{
		QueryPrefix: "",
		Limit:       100,
		Offset:      0,
	}}) {
		t.Fatalf("unexpected namespace inventory inputs: %+v", readStore.namespaceInventoryInputs)
	}
	if namespacesResp.Header.Get("X-Total-Count") != "1" {
		t.Fatalf("expected X-Total-Count=1, got %q", namespacesResp.Header.Get("X-Total-Count"))
	}

	reposResp, err := server.App().Test(httptest.NewRequest(http.MethodGet, "/v1/repos", nil), -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer reposResp.Body.Close()

	if reposResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for repos, got %d", reposResp.StatusCode)
	}

	statusResp, err := server.App().Test(
		httptest.NewRequest(http.MethodGet, "/v1/catalog/status?namespace=acme&repo=platform", nil),
		-1,
	)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer statusResp.Body.Close()

	if statusResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for catalog status, got %d", statusResp.StatusCode)
	}
	if !reflect.DeepEqual(readStore.catalogStatusInputs, []string{"acme/platform"}) {
		t.Fatalf("unexpected catalog status input: %+v", readStore.catalogStatusInputs)
	}
}

func TestQueryEndpoints_StatusMapping(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		store        *fakeQueryReadStore
		path         string
		expectedCode int
	}{
		{
			name:         "invalid query returns 400",
			store:        &fakeQueryReadStore{},
			path:         "/v1/spec?namespace=acme&repo=platform&revision_id=1&sha=deadbeef",
			expectedCode: http.StatusBadRequest,
		},
		{
			name: "snapshot unprocessed returns 409",
			store: &fakeQueryReadStore{
				resolveReadSnapshotErr: &store.ReadSnapshotResolutionError{
					Code: store.ReadSnapshotResolutionUnprocessed,
				},
			},
			path:         "/v1/apis?namespace=acme&repo=platform",
			expectedCode: http.StatusConflict,
		},
		{
			name: "store not configured returns 503",
			store: &fakeQueryReadStore{
				repoInventoryErr: store.ErrStoreNotConfigured,
			},
			path:         "/v1/repos",
			expectedCode: http.StatusServiceUnavailable,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			server := newQueryTestServer(testCase.store)
			resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, testCase.path, nil), -1)
			if err != nil {
				t.Fatalf("http test request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != testCase.expectedCode {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("expected status %d, got %d body=%s", testCase.expectedCode, resp.StatusCode, strings.TrimSpace(string(body)))
			}
		})
	}
}

func TestQueryEndpoints_ListNamespaces_UsesQueryAndPaginationInputs(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		namespaceInventoryResult: store.NamespaceCatalogListResult{
			Items: []store.NamespaceCatalogEntry{
				{Namespace: "acme", RepoCount: 3, AllPending: false},
			},
			TotalCount: 11,
		},
	}
	server := newQueryTestServer(readStore)

	resp, err := server.App().Test(
		httptest.NewRequest(http.MethodGet, "/v1/namespaces?query=ac&limit=5&offset=10", nil),
		-1,
	)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if !reflect.DeepEqual(readStore.namespaceInventoryInputs, []store.NamespaceCatalogListInput{{
		QueryPrefix: "ac",
		Limit:       5,
		Offset:      10,
	}}) {
		t.Fatalf("unexpected namespace inventory inputs: %+v", readStore.namespaceInventoryInputs)
	}
	if resp.Header.Get("X-Total-Count") != "11" {
		t.Fatalf("expected X-Total-Count=11, got %q", resp.Header.Get("X-Total-Count"))
	}
}

func TestQueryEndpoints_CountNamespaces_UsesQueryPrefixOnly(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		repoInventoryResult: []store.RepoCatalogEntry{
			{Repo: store.Repo{Namespace: "acme", Repo: "gateway"}},
			{Repo: store.Repo{Namespace: "acme", Repo: "platform"}},
			{Repo: store.Repo{Namespace: "beta", Repo: "payments"}},
		},
	}
	server := newQueryTestServer(readStore)

	resp, err := server.App().Test(
		httptest.NewRequest(http.MethodGet, "/v1/namespaces/count?query=ac", nil),
		-1,
	)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	var body struct {
		TotalCount    int64 `json:"total_count"`
		MaxItemLength int64 `json:"max_item_length"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode count response: %v", err)
	}
	if body.TotalCount != 1 {
		t.Fatalf("expected total_count=1, got %d", body.TotalCount)
	}
	if body.MaxItemLength != 4 {
		t.Fatalf("expected max_item_length=4, got %d", body.MaxItemLength)
	}
}

func TestQueryEndpoints_CountRepos_UsesNamespaceScope(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		repoInventoryResult: []store.RepoCatalogEntry{
			{Repo: store.Repo{Namespace: "acme", Repo: "gateway"}},
			{Repo: store.Repo{Namespace: "acme", Repo: "platform"}},
			{Repo: store.Repo{Namespace: "beta", Repo: "payments"}},
		},
	}
	server := newQueryTestServer(readStore)

	resp, err := server.App().Test(
		httptest.NewRequest(http.MethodGet, "/v1/repos/count?namespace=acme", nil),
		-1,
	)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	var body struct {
		TotalCount    int64 `json:"total_count"`
		MaxItemLength int64 `json:"max_item_length"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode count response: %v", err)
	}
	if body.TotalCount != 2 {
		t.Fatalf("expected total_count=2, got %d", body.TotalCount)
	}
	if body.MaxItemLength != 8 {
		t.Fatalf("expected max_item_length=8, got %d", body.MaxItemLength)
	}
}

func TestQueryEndpoints_CountOperations_UsesRepoScope(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		resolveReadSnapshotResult: store.ResolvedReadSnapshot{
			Repo:     store.Repo{ID: 2, Namespace: "acme", Repo: "platform"},
			Revision: store.Revision{ID: 21},
		},
		operationInventoryCountResult: store.OperationCatalogCount{
			TotalCount:    2,
			MaxItemLength: int64(len("POST /accounts/{id}")),
		},
	}
	server := newQueryTestServer(readStore)

	resp, err := server.App().Test(
		httptest.NewRequest(http.MethodGet, "/v1/operations/count?namespace=acme&repo=platform", nil),
		-1,
	)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if !reflect.DeepEqual(readStore.resolveReadSnapshotInputs, []store.ResolveReadSnapshotInput{{
		Namespace: "acme",
		Repo:      "platform",
	}}) {
		t.Fatalf("unexpected snapshot resolution inputs: %+v", readStore.resolveReadSnapshotInputs)
	}
	if !reflect.DeepEqual(readStore.operationInventoryCountInputs, []operationInventoryCountInput{{
		RepoID:      2,
		RevisionID:  21,
		QueryPrefix: "",
	}}) {
		t.Fatalf("unexpected operation inventory count inputs: %+v", readStore.operationInventoryCountInputs)
	}
	var body struct {
		TotalCount    int64 `json:"total_count"`
		MaxItemLength int64 `json:"max_item_length"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode count response: %v", err)
	}
	if body.TotalCount != 2 {
		t.Fatalf("expected total_count=2, got %d", body.TotalCount)
	}
	if body.MaxItemLength != int64(len("POST /accounts/{id}")) {
		t.Fatalf("expected max_item_length=%d, got %d", len("POST /accounts/{id}"), body.MaxItemLength)
	}
}

func TestQueryEndpoints_CountOperations_UsesCatalogScope(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		operationCatalogCountResult: store.OperationCatalogCount{
			TotalCount:    3,
			MaxItemLength: int64(len("POST /accounts/{id}")),
		},
	}
	server := newQueryTestServer(readStore)

	resp, err := server.App().Test(
		httptest.NewRequest(http.MethodGet, "/v1/operations/count?namespace=acme&query=get%20/", nil),
		-1,
	)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if len(readStore.resolveReadSnapshotInputs) != 0 {
		t.Fatalf("did not expect snapshot resolution, got %+v", readStore.resolveReadSnapshotInputs)
	}
	if !reflect.DeepEqual(readStore.operationCatalogCountInputs, []operationCatalogCountInput{{
		Namespace:   "acme",
		Repo:        "",
		QueryPrefix: "get /",
	}}) {
		t.Fatalf("unexpected operation catalog count inputs: %+v", readStore.operationCatalogCountInputs)
	}
}

func newQueryTestServer(readStore queryReadStore) *Server {
	cfg := config.Config{HTTPAddr: ":8080"}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), &store.Store{})
	server.readStore = readStore
	return server
}

type fakeQueryReadStore struct {
	repoLookupInputs       []string
	repoLookupResultByPath map[string]store.Repo
	repoLookupErr          error

	resolveReadSnapshotInputs []store.ResolveReadSnapshotInput
	resolveReadSnapshotResult store.ResolvedReadSnapshot
	resolveReadSnapshotErr    error

	resolveSpecSnapshotsInputs []store.ResolveReadSnapshotInput
	resolveSpecSnapshotsResult store.ResolvedSpecSnapshots
	resolveSpecSnapshotsErr    error

	resolveOperationByIDInputs         []store.ResolveOperationByIDInput
	resolveOperationByIDResult         store.ResolvedOperationCandidates
	resolveOperationByIDErr            error
	resolveOperationByMethodPathInputs []store.ResolveOperationByMethodPathInput
	resolveOperationByMethodPathResult store.ResolvedOperationCandidates
	resolveOperationByMethodPathErr    error

	specArtifactInputs []int64
	specArtifactResult store.SpecArtifact
	specArtifactErr    error

	apiInventoryInputs []apiInventoryInput
	apiInventoryResult []store.APISnapshot
	apiInventoryErr    error

	apiSnapshotInputs []apiSnapshotSelectionInput
	apiSnapshotResult store.APISnapshot
	apiSnapshotFound  bool
	apiSnapshotErr    error

	operationInventoryInputs          []operationInventoryInput
	operationInventoryResult          []store.OperationSnapshot
	operationInventoryErr             error
	operationCatalogInventoryInputs   []string
	operationCatalogInventoryResult   []store.OperationSnapshot
	operationCatalogInventoryErr      error
	operationInventoryPageInputs      []operationInventoryPageInput
	operationCatalogPageInputs        []operationCatalogPageInput
	operationInventoryByAPIPageInputs []operationInventoryByAPIPageInput
	operationInventoryCountInputs     []operationInventoryCountInput
	operationInventoryCountResult     store.OperationCatalogCount
	operationInventoryCountErr        error
	operationCatalogCountInputs       []operationCatalogCountInput
	operationCatalogCountResult       store.OperationCatalogCount
	operationCatalogCountErr          error
	operationInventoryByAPIInputs     []operationInventoryByAPIInput
	operationInventoryByAPIResult     []store.OperationSnapshot
	operationInventoryByAPIErr        error

	repoInventoryResult      []store.RepoCatalogEntry
	repoInventoryErr         error
	namespaceInventoryInputs []store.NamespaceCatalogListInput
	namespaceInventoryResult store.NamespaceCatalogListResult
	namespaceInventoryErr    error

	catalogStatusInputs []string
	catalogStatusResult store.RepoCatalogFreshness
	catalogStatusErr    error
}

func (f *fakeQueryReadStore) GetRepoByNamespaceAndRepo(
	_ context.Context,
	namespace string,
	repo string,
) (store.Repo, error) {
	f.repoLookupInputs = append(f.repoLookupInputs, namespace+"/"+repo)
	if f.repoLookupErr != nil {
		return store.Repo{}, f.repoLookupErr
	}
	if result, ok := f.repoLookupResultByPath[namespace+"/"+repo]; ok {
		return result, nil
	}
	return store.Repo{}, store.ErrRepoNotFound
}

type apiInventoryInput struct {
	RepoID     int64
	RevisionID int64
}

type apiSnapshotSelectionInput struct {
	RepoID     int64
	API        string
	RevisionID int64
}

type operationInventoryInput struct {
	RepoID     int64
	RevisionID int64
}

type operationInventoryPageInput struct {
	RepoID      int64
	RevisionID  int64
	QueryPrefix string
	Limit       int32
	Offset      int32
}

type operationInventoryCountInput struct {
	RepoID      int64
	RevisionID  int64
	QueryPrefix string
}

type operationCatalogPageInput struct {
	Namespace   string
	Repo        string
	QueryPrefix string
	Limit       int32
	Offset      int32
}

type operationCatalogCountInput struct {
	Namespace   string
	Repo        string
	QueryPrefix string
}

type operationInventoryByAPIInput struct {
	RepoID     int64
	API        string
	RevisionID int64
}

type operationInventoryByAPIPageInput struct {
	RepoID      int64
	API         string
	RevisionID  int64
	QueryPrefix string
	Limit       int32
	Offset      int32
}

func (f *fakeQueryReadStore) ResolveReadSnapshot(
	_ context.Context,
	input store.ResolveReadSnapshotInput,
) (store.ResolvedReadSnapshot, error) {
	f.resolveReadSnapshotInputs = append(f.resolveReadSnapshotInputs, input)
	if f.resolveReadSnapshotErr != nil {
		return store.ResolvedReadSnapshot{}, f.resolveReadSnapshotErr
	}
	return f.resolveReadSnapshotResult, nil
}

func (f *fakeQueryReadStore) ResolveSpecSnapshots(
	_ context.Context,
	input store.ResolveReadSnapshotInput,
) (store.ResolvedSpecSnapshots, error) {
	f.resolveSpecSnapshotsInputs = append(f.resolveSpecSnapshotsInputs, input)
	if f.resolveSpecSnapshotsErr != nil {
		return store.ResolvedSpecSnapshots{}, f.resolveSpecSnapshotsErr
	}
	return f.resolveSpecSnapshotsResult, nil
}

func (f *fakeQueryReadStore) ResolveOperationCandidatesByOperationID(
	_ context.Context,
	input store.ResolveOperationByIDInput,
) (store.ResolvedOperationCandidates, error) {
	f.resolveOperationByIDInputs = append(f.resolveOperationByIDInputs, input)
	if f.resolveOperationByIDErr != nil {
		return store.ResolvedOperationCandidates{}, f.resolveOperationByIDErr
	}
	return f.resolveOperationByIDResult, nil
}

func (f *fakeQueryReadStore) ResolveOperationCandidatesByMethodPath(
	_ context.Context,
	input store.ResolveOperationByMethodPathInput,
) (store.ResolvedOperationCandidates, error) {
	f.resolveOperationByMethodPathInputs = append(f.resolveOperationByMethodPathInputs, input)
	if f.resolveOperationByMethodPathErr != nil {
		return store.ResolvedOperationCandidates{}, f.resolveOperationByMethodPathErr
	}
	return f.resolveOperationByMethodPathResult, nil
}

func (f *fakeQueryReadStore) GetSpecArtifactByAPISpecRevisionID(
	_ context.Context,
	apiSpecRevisionID int64,
) (store.SpecArtifact, error) {
	f.specArtifactInputs = append(f.specArtifactInputs, apiSpecRevisionID)
	if f.specArtifactErr != nil {
		return store.SpecArtifact{}, f.specArtifactErr
	}
	return f.specArtifactResult, nil
}

func (f *fakeQueryReadStore) ListAPISnapshotInventoryByRepoRevision(
	_ context.Context,
	repoID int64,
	snapshotRevisionID int64,
) ([]store.APISnapshot, error) {
	f.apiInventoryInputs = append(f.apiInventoryInputs, apiInventoryInput{
		RepoID:     repoID,
		RevisionID: snapshotRevisionID,
	})
	if f.apiInventoryErr != nil {
		return nil, f.apiInventoryErr
	}
	result := make([]store.APISnapshot, len(f.apiInventoryResult))
	copy(result, f.apiInventoryResult)
	return result, nil
}

func (f *fakeQueryReadStore) GetAPISnapshotByRepoRevisionAndAPI(
	_ context.Context,
	repoID int64,
	api string,
	snapshotRevisionID int64,
) (store.APISnapshot, bool, error) {
	f.apiSnapshotInputs = append(f.apiSnapshotInputs, apiSnapshotSelectionInput{
		RepoID:     repoID,
		API:        api,
		RevisionID: snapshotRevisionID,
	})
	if f.apiSnapshotErr != nil {
		return store.APISnapshot{}, false, f.apiSnapshotErr
	}
	return f.apiSnapshotResult, f.apiSnapshotFound, nil
}

func (f *fakeQueryReadStore) ListOperationInventoryByRepoRevision(
	_ context.Context,
	repoID int64,
	snapshotRevisionID int64,
) ([]store.OperationSnapshot, error) {
	f.operationInventoryInputs = append(f.operationInventoryInputs, operationInventoryInput{
		RepoID:     repoID,
		RevisionID: snapshotRevisionID,
	})
	if f.operationInventoryErr != nil {
		return nil, f.operationInventoryErr
	}
	result := make([]store.OperationSnapshot, len(f.operationInventoryResult))
	copy(result, f.operationInventoryResult)
	return result, nil
}

func (f *fakeQueryReadStore) ListOperationInventoryByRepoRevisionPage(
	_ context.Context,
	repoID int64,
	snapshotRevisionID int64,
	queryPrefix string,
	limit int32,
	offset int32,
) ([]store.OperationSnapshot, error) {
	f.operationInventoryPageInputs = append(f.operationInventoryPageInputs, operationInventoryPageInput{
		RepoID:      repoID,
		RevisionID:  snapshotRevisionID,
		QueryPrefix: queryPrefix,
		Limit:       limit,
		Offset:      offset,
	})
	if f.operationInventoryErr != nil {
		return nil, f.operationInventoryErr
	}
	result := make([]store.OperationSnapshot, len(f.operationInventoryResult))
	copy(result, f.operationInventoryResult)
	return result, nil
}

func (f *fakeQueryReadStore) CountOperationInventoryByRepoRevision(
	_ context.Context,
	repoID int64,
	snapshotRevisionID int64,
	queryPrefix string,
) (store.OperationCatalogCount, error) {
	f.operationInventoryCountInputs = append(f.operationInventoryCountInputs, operationInventoryCountInput{
		RepoID:      repoID,
		RevisionID:  snapshotRevisionID,
		QueryPrefix: queryPrefix,
	})
	if f.operationInventoryCountErr != nil {
		return store.OperationCatalogCount{}, f.operationInventoryCountErr
	}
	return f.operationInventoryCountResult, nil
}

func (f *fakeQueryReadStore) ListOperationCatalogInventory(
	_ context.Context,
	namespace string,
) ([]store.OperationSnapshot, error) {
	f.operationCatalogInventoryInputs = append(f.operationCatalogInventoryInputs, namespace)
	if f.operationCatalogInventoryErr != nil {
		return nil, f.operationCatalogInventoryErr
	}
	result := make([]store.OperationSnapshot, len(f.operationCatalogInventoryResult))
	copy(result, f.operationCatalogInventoryResult)
	return result, nil
}

func (f *fakeQueryReadStore) ListOperationCatalogInventoryPage(
	_ context.Context,
	namespace string,
	repo string,
	queryPrefix string,
	limit int32,
	offset int32,
) ([]store.OperationSnapshot, error) {
	f.operationCatalogPageInputs = append(f.operationCatalogPageInputs, operationCatalogPageInput{
		Namespace:   namespace,
		Repo:        repo,
		QueryPrefix: queryPrefix,
		Limit:       limit,
		Offset:      offset,
	})
	if f.operationCatalogInventoryErr != nil {
		return nil, f.operationCatalogInventoryErr
	}
	result := make([]store.OperationSnapshot, len(f.operationCatalogInventoryResult))
	copy(result, f.operationCatalogInventoryResult)
	return result, nil
}

func (f *fakeQueryReadStore) CountOperationCatalogInventory(
	_ context.Context,
	namespace string,
	repo string,
	queryPrefix string,
) (store.OperationCatalogCount, error) {
	f.operationCatalogCountInputs = append(f.operationCatalogCountInputs, operationCatalogCountInput{
		Namespace:   namespace,
		Repo:        repo,
		QueryPrefix: queryPrefix,
	})
	if f.operationCatalogCountErr != nil {
		return store.OperationCatalogCount{}, f.operationCatalogCountErr
	}
	return f.operationCatalogCountResult, nil
}

func (f *fakeQueryReadStore) ListOperationInventoryByRepoRevisionAndAPI(
	_ context.Context,
	repoID int64,
	api string,
	snapshotRevisionID int64,
) ([]store.OperationSnapshot, error) {
	f.operationInventoryByAPIInputs = append(f.operationInventoryByAPIInputs, operationInventoryByAPIInput{
		RepoID:     repoID,
		API:        api,
		RevisionID: snapshotRevisionID,
	})
	if f.operationInventoryByAPIErr != nil {
		return nil, f.operationInventoryByAPIErr
	}
	result := make([]store.OperationSnapshot, len(f.operationInventoryByAPIResult))
	copy(result, f.operationInventoryByAPIResult)
	return result, nil
}

func (f *fakeQueryReadStore) ListOperationInventoryByRepoRevisionAndAPIPage(
	_ context.Context,
	repoID int64,
	api string,
	snapshotRevisionID int64,
	queryPrefix string,
	limit int32,
	offset int32,
) ([]store.OperationSnapshot, error) {
	f.operationInventoryByAPIPageInputs = append(f.operationInventoryByAPIPageInputs, operationInventoryByAPIPageInput{
		RepoID:      repoID,
		API:         api,
		RevisionID:  snapshotRevisionID,
		QueryPrefix: queryPrefix,
		Limit:       limit,
		Offset:      offset,
	})
	if f.operationInventoryByAPIErr != nil {
		return nil, f.operationInventoryByAPIErr
	}
	result := make([]store.OperationSnapshot, len(f.operationInventoryByAPIResult))
	copy(result, f.operationInventoryByAPIResult)
	return result, nil
}

func (f *fakeQueryReadStore) ListRepoCatalogInventory(_ context.Context) ([]store.RepoCatalogEntry, error) {
	if f.repoInventoryErr != nil {
		return nil, f.repoInventoryErr
	}
	result := make([]store.RepoCatalogEntry, len(f.repoInventoryResult))
	copy(result, f.repoInventoryResult)
	return result, nil
}

func (f *fakeQueryReadStore) ListNamespaceCatalogInventory(
	_ context.Context,
	input store.NamespaceCatalogListInput,
) (store.NamespaceCatalogListResult, error) {
	f.namespaceInventoryInputs = append(f.namespaceInventoryInputs, input)
	if f.namespaceInventoryErr != nil {
		return store.NamespaceCatalogListResult{}, f.namespaceInventoryErr
	}
	result := store.NamespaceCatalogListResult{
		Items:      make([]store.NamespaceCatalogEntry, len(f.namespaceInventoryResult.Items)),
		TotalCount: f.namespaceInventoryResult.TotalCount,
	}
	copy(result.Items, f.namespaceInventoryResult.Items)
	return result, nil
}

func (f *fakeQueryReadStore) CountNamespaceCatalogInventory(
	_ context.Context,
	_ store.NamespaceCatalogCountInput,
) (int64, error) {
	return 0, nil
}

func (f *fakeQueryReadStore) GetRepoCatalogFreshness(_ context.Context, namespace string, repo string) (store.RepoCatalogFreshness, error) {
	f.catalogStatusInputs = append(f.catalogStatusInputs, namespace+"/"+repo)
	if f.catalogStatusErr != nil {
		return store.RepoCatalogFreshness{}, f.catalogStatusErr
	}
	return f.catalogStatusResult, nil
}
