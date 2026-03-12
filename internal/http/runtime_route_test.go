package httpserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/store"
)

func TestRuntimeRouteHandler_ResolvesOperationAndCachesParsedSpec(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		repoLookupResultByPath: map[string]store.Repo{
			"acme/platform": {ID: 77, Namespace: "acme", Repo: "platform", DefaultBranch: "main"},
		},
		resolveReadSnapshotResult: store.ResolvedReadSnapshot{
			Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform", DefaultBranch: "main"},
			Revision: store.Revision{ID: 42, Sha: "deadbeef", Branch: "main"},
		},
		resolveOperationByMethodPathResult: store.ResolvedOperationCandidates{
			Snapshot: store.ResolvedReadSnapshot{
				Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform", DefaultBranch: "main"},
				Revision: store.Revision{ID: 42, Sha: "deadbeef", Branch: "main"},
			},
			Candidates: []store.OperationSnapshot{
				{
					API:               "apis/pets/openapi.yaml",
					APISpecRevisionID: 501,
					IngestEventID:     42,
					IngestEventSHA:    "deadbeef",
					IngestEventBranch: "main",
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

	for i := 0; i < 2; i++ {
		resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, "/gl/acme/platform/pets", nil), -1)
		if err != nil {
			t.Fatalf("http test request failed: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, string(body))
		}
	}

	if !reflect.DeepEqual(readStore.resolveReadSnapshotInputs, []store.ResolveReadSnapshotInput{
		{Namespace: "acme", Repo: "platform"},
		{Namespace: "acme", Repo: "platform"},
	}) {
		t.Fatalf("unexpected snapshot inputs: %+v", readStore.resolveReadSnapshotInputs)
	}
	if !reflect.DeepEqual(readStore.resolveOperationByMethodPathInputs, []store.ResolveOperationByMethodPathInput{
		{
			ResolveReadSnapshotInput: store.ResolveReadSnapshotInput{
				Namespace:  "acme",
				Repo:       "platform",
				RevisionID: 42,
			},
			Method: "get",
			Path:   "/pets",
		},
		{
			ResolveReadSnapshotInput: store.ResolveReadSnapshotInput{
				Namespace:  "acme",
				Repo:       "platform",
				RevisionID: 42,
			},
			Method: "get",
			Path:   "/pets",
		},
	}) {
		t.Fatalf("unexpected operation inputs: %+v", readStore.resolveOperationByMethodPathInputs)
	}
	if !reflect.DeepEqual(readStore.specArtifactInputs, []int64{501}) {
		t.Fatalf("expected one spec artifact lookup, got %+v", readStore.specArtifactInputs)
	}
}

func TestRuntimeRouteHandler_Returns404WhenOperationDoesNotMatch(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		repoLookupResultByPath: map[string]store.Repo{
			"acme/platform": {ID: 77, Namespace: "acme", Repo: "platform", DefaultBranch: "main"},
		},
		resolveReadSnapshotResult: store.ResolvedReadSnapshot{
			Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform", DefaultBranch: "main"},
			Revision: store.Revision{ID: 42, Sha: "deadbeef", Branch: "main"},
		},
	}
	server := newQueryTestServer(readStore)

	resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, "/gl/acme/platform/pets", nil), -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 404, got %d body=%s", resp.StatusCode, string(body))
	}
	if len(readStore.specArtifactInputs) != 0 {
		t.Fatalf("expected no spec artifact lookup, got %+v", readStore.specArtifactInputs)
	}
}

func TestRuntimeRouteHandler_Returns409WhenOperationIsAmbiguous(t *testing.T) {
	t.Parallel()

	readStore := &fakeQueryReadStore{
		repoLookupResultByPath: map[string]store.Repo{
			"acme/platform": {ID: 77, Namespace: "acme", Repo: "platform", DefaultBranch: "main"},
		},
		resolveReadSnapshotResult: store.ResolvedReadSnapshot{
			Repo:     store.Repo{ID: 77, Namespace: "acme", Repo: "platform", DefaultBranch: "main"},
			Revision: store.Revision{ID: 42, Sha: "deadbeef", Branch: "main"},
		},
		resolveOperationByMethodPathResult: store.ResolvedOperationCandidates{
			Candidates: []store.OperationSnapshot{
				{
					API:               "apis/pets/openapi.yaml",
					APISpecRevisionID: 501,
					IngestEventID:     42,
					IngestEventSHA:    "deadbeef",
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
					IngestEventSHA:    "deadbeef",
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

	resp, err := server.App().Test(httptest.NewRequest(http.MethodGet, "/gl/acme/platform/pets", nil), -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 409, got %d body=%s", resp.StatusCode, string(body))
	}

	var body struct {
		Error      string                      `json:"error"`
		Candidates []operationSnapshotResponse `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode ambiguity response: %v", err)
	}
	if body.Error == "" || len(body.Candidates) != 2 {
		t.Fatalf("unexpected ambiguity response: %+v", body)
	}
	if len(readStore.specArtifactInputs) != 0 {
		t.Fatalf("expected no spec artifact lookup, got %+v", readStore.specArtifactInputs)
	}
}
