package httpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/cli/executor"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/store"
)

func TestPostCall_NormalizesOperationIDAndMethodPathToSamePlan(t *testing.T) {
	t.Parallel()

	candidate := store.OperationSnapshot{
		API:         "apis/pets/openapi.yaml",
		Method:      "get",
		Path:        "/pets/{id}",
		OperationID: "getPet",
	}
	snapshot := store.ResolvedReadSnapshot{
		Repo:     store.Repo{PathWithNamespace: "acme/platform"},
		Revision: store.Revision{ID: 42, Sha: "deadbeef"},
	}

	t.Run("operation id", func(t *testing.T) {
		t.Parallel()

		server := newQueryTestServer(&fakeQueryReadStore{
			resolveOperationByIDResult: store.ResolvedOperationCandidates{
				Snapshot:   snapshot,
				Candidates: []store.OperationSnapshot{candidate},
			},
		})

		plan := postCallPlan(t, server, `{"repo":"acme/platform","operation_id":"getPet","path_params":{"id":"42"},"dry_run":true}`)
		expected := executor.CallPlan{
			Request: request.Envelope{
				Kind:        request.KindCall,
				Repo:        "acme/platform",
				API:         "apis/pets/openapi.yaml",
				RevisionID:  42,
				SHA:         "deadbeef",
				Target:      request.DefaultShivaTarget,
				OperationID: "getPet",
				Method:      "get",
				Path:        "/pets/{id}",
				PathParams:  map[string]string{"id": "42"},
				DryRun:      true,
			},
			Dispatch: executor.DispatchPlan{
				Mode:    executor.DispatchModeShiva,
				DryRun:  true,
				Network: false,
			},
		}
		if !reflect.DeepEqual(plan, expected) {
			t.Fatalf("expected plan %+v, got %+v", expected, plan)
		}
	})

	t.Run("method path", func(t *testing.T) {
		t.Parallel()

		server := newQueryTestServer(&fakeQueryReadStore{
			resolveOperationByMethodPathResult: store.ResolvedOperationCandidates{
				Snapshot:   snapshot,
				Candidates: []store.OperationSnapshot{candidate},
			},
		})

		plan := postCallPlan(t, server, `{"repo":"acme/platform","method":"GET","path":"pets/{id}","path_params":{"id":"42"},"dry_run":true}`)
		if plan.Request.Kind != request.KindCall ||
			plan.Request.OperationID != "getPet" ||
			plan.Request.Method != "get" ||
			plan.Request.Path != "/pets/{id}" ||
			plan.Request.API != "apis/pets/openapi.yaml" ||
			plan.Request.RevisionID != 42 ||
			plan.Request.SHA != "deadbeef" {
			t.Fatalf("unexpected normalized request %+v", plan.Request)
		}
		if !plan.Dispatch.DryRun || plan.Dispatch.Network {
			t.Fatalf("expected dry-run no-network dispatch, got %+v", plan.Dispatch)
		}
	})
}

func TestPostCall_AmbiguityIncludesCandidates(t *testing.T) {
	t.Parallel()

	server := newQueryTestServer(&fakeQueryReadStore{
		resolveOperationByIDResult: store.ResolvedOperationCandidates{
			Snapshot: store.ResolvedReadSnapshot{
				Repo: store.Repo{PathWithNamespace: "acme/platform"},
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
					RawJSON:           []byte(`{"operationId":"listPets"}`),
				},
				{
					API:               "apis/admin/openapi.yaml",
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
	})

	resp, err := server.App().Test(httptest.NewRequest(
		http.MethodPost,
		"/v1/call",
		bytes.NewBufferString(`{"repo":"acme/platform","operation_id":"listPets"}`),
	), -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 409, got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestPostCall_RejectsInvalidEnvelope(t *testing.T) {
	t.Parallel()

	testCases := []string{
		`{"repo":"acme/platform","operation_id":"listPets","method":"get","path":"/pets"}`,
		`{"repo":"acme/platform","operation_id":"listPets","target":"prod"}`,
	}

	for _, body := range testCases {
		resp, err := newQueryTestServer(&fakeQueryReadStore{}).App().Test(httptest.NewRequest(
			http.MethodPost,
			"/v1/call",
			bytes.NewBufferString(body),
		), -1)
		if err != nil {
			t.Fatalf("http test request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			payload, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 400, got %d body=%s", resp.StatusCode, string(payload))
		}
	}
}

func TestPostCall_RejectsEmptyOrNonObjectBody(t *testing.T) {
	t.Parallel()

	testCases := []string{
		"",
		`[]`,
	}

	for _, body := range testCases {
		resp, err := newQueryTestServer(&fakeQueryReadStore{}).App().Test(httptest.NewRequest(
			http.MethodPost,
			"/v1/call",
			bytes.NewBufferString(body),
		), -1)
		if err != nil {
			t.Fatalf("http test request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			payload, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 400, got %d body=%s", resp.StatusCode, string(payload))
		}
	}
}

func postCallPlan(t *testing.T, server *Server, body string) executor.CallPlan {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/v1/call", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := server.App().Test(req, -1)
	if err != nil {
		t.Fatalf("http test request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status 200, got %d body=%s", resp.StatusCode, string(payload))
	}

	var plan executor.CallPlan
	if err := json.NewDecoder(resp.Body).Decode(&plan); err != nil {
		t.Fatalf("decode call plan: %v", err)
	}
	return plan
}
