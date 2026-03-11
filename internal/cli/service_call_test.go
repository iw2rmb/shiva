package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/catalog"
	"github.com/iw2rmb/shiva/internal/cli/config"
	"github.com/iw2rmb/shiva/internal/cli/profile"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/cli/target"
)

func TestRuntimeServiceExecuteCallDirectDryRunUsesCatalogResolution(t *testing.T) {
	t.Parallel()

	store, err := catalog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}

	client := &recordingTransportClient{
		reposBody:      []byte(`[{"repo":"acme/platform"}]`),
		statusBody:     []byte(`{"repo":"acme/platform","snapshot_revision":{"id":42,"sha":"deadbeef"}}`),
		apisBody:       []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`),
		operationsBody: []byte(`[{"api":"apis/pets/openapi.yaml","method":"get","path":"/pets","operation_id":"listPets","operation":{"operationId":"listPets"}}]`),
	}

	service := &RuntimeService{
		document: config.Document{
			ActiveProfile: "default",
			Profiles: map[string]profile.Source{
				"default": {Name: "default", BaseURL: "http://default.example", Timeout: 5 * time.Second},
			},
			Targets: map[string]target.Entry{
				target.BuiltinShivaName: target.BuiltinShiva(),
				"prod":                  {Name: "prod", Mode: target.ModeDirect, BaseURL: "https://api.example", Timeout: 5 * time.Second},
			},
		},
		catalogService: catalog.NewService(store),
		catalogStore:   store,
		refreshedKeys:  make(map[string]struct{}),
		newClient: func(source profile.Source) (transportClient, error) {
			_ = source
			return client, nil
		},
	}

	body, err := service.ExecuteCall(context.Background(), request.Envelope{
		Repo:        "acme/platform",
		Target:      "prod",
		OperationID: "listPets",
		DryRun:      true,
	}, RequestOptions{}, CallFormatJSON)
	if err != nil {
		t.Fatalf("execute direct dry-run failed: %v", err)
	}
	if !strings.Contains(string(body), `"mode":"direct"`) || !strings.Contains(string(body), `"url":"https://api.example/pets"`) {
		t.Fatalf("expected direct dry-run plan, got %q", string(body))
	}
}

func TestRuntimeServiceExecuteCallDirectDispatchesToTarget(t *testing.T) {
	t.Parallel()

	store, err := catalog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}

	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/pets/42" {
			t.Fatalf("expected path /pets/42, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer target-token" {
			t.Fatalf("unexpected authorization header %q", auth)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if string(body) != `{"name":"Milo"}` {
			t.Fatalf("unexpected request body %q", string(body))
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer targetServer.Close()

	client := &recordingTransportClient{
		reposBody:      []byte(`[{"repo":"acme/platform"}]`),
		statusBody:     []byte(`{"repo":"acme/platform","snapshot_revision":{"id":42,"sha":"deadbeef"}}`),
		apisBody:       []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`),
		operationsBody: []byte(`[{"api":"apis/pets/openapi.yaml","method":"post","path":"/pets/{id}","operation_id":"createPet","operation":{"operationId":"createPet"}}]`),
	}

	service := &RuntimeService{
		document: config.Document{
			ActiveProfile: "default",
			Profiles: map[string]profile.Source{
				"default": {Name: "default", BaseURL: "http://default.example", Timeout: 5 * time.Second},
			},
			Targets: map[string]target.Entry{
				target.BuiltinShivaName: target.BuiltinShiva(),
				"prod": {
					Name:    "prod",
					Mode:    target.ModeDirect,
					BaseURL: targetServer.URL,
					Token:   "target-token",
					Timeout: 5 * time.Second,
				},
			},
		},
		catalogService: catalog.NewService(store),
		catalogStore:   store,
		refreshedKeys:  make(map[string]struct{}),
		newClient: func(source profile.Source) (transportClient, error) {
			_ = source
			return client, nil
		},
	}

	body, err := service.ExecuteCall(context.Background(), request.Envelope{
		Repo:        "acme/platform",
		Target:      "prod",
		OperationID: "createPet",
		PathParams:  map[string]string{"id": "42"},
		JSONBody:    []byte(`{"name":"Milo"}`),
	}, RequestOptions{}, CallFormatBody)
	if err != nil {
		t.Fatalf("execute direct call failed: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("unexpected response body %q", string(body))
	}
}

func TestRuntimeServiceRefreshCoalescesOperationAndCallInventoryRefreshes(t *testing.T) {
	t.Parallel()

	store, err := catalog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}

	client := &recordingTransportClient{
		reposBody:      []byte(`[{"repo":"acme/platform"}]`),
		statusBody:     []byte(`{"repo":"acme/platform","snapshot_revision":{"id":42,"sha":"deadbeef"}}`),
		apisBody:       []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`),
		operationsBody: []byte(`[{"api":"apis/pets/openapi.yaml","method":"get","path":"/pets","operation_id":"listPets","operation":{"operationId":"listPets"}}]`),
		operationBody:  []byte(`{"operationId":"listPets"}`),
	}

	service := &RuntimeService{
		document: config.Document{
			ActiveProfile: "default",
			Profiles: map[string]profile.Source{
				"default": {Name: "default", BaseURL: "http://default.example", Timeout: 5 * time.Second},
			},
			Targets: map[string]target.Entry{
				target.BuiltinShivaName: target.BuiltinShiva(),
				"prod":                  {Name: "prod", Mode: target.ModeDirect, BaseURL: "https://api.example", Timeout: 5 * time.Second},
			},
		},
		catalogService: catalog.NewService(store),
		catalogStore:   store,
		refreshedKeys:  make(map[string]struct{}),
		newClient: func(source profile.Source) (transportClient, error) {
			_ = source
			return client, nil
		},
	}

	if _, err := service.GetOperation(context.Background(), request.Envelope{
		Repo:        "acme/platform",
		OperationID: "listPets",
	}, RequestOptions{Refresh: true}); err != nil {
		t.Fatalf("get operation failed: %v", err)
	}

	if _, err := service.ExecuteCall(context.Background(), request.Envelope{
		Repo:        "acme/platform",
		Target:      "prod",
		OperationID: "listPets",
		DryRun:      true,
	}, RequestOptions{Refresh: true}, CallFormatJSON); err != nil {
		t.Fatalf("execute call failed: %v", err)
	}

	if client.operationsCalls != 1 {
		t.Fatalf("expected one shared operation inventory refresh, got %d", client.operationsCalls)
	}
}
