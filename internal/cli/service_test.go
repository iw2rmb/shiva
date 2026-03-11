package cli

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/catalog"
	"github.com/iw2rmb/shiva/internal/cli/config"
	"github.com/iw2rmb/shiva/internal/cli/profile"
	"github.com/iw2rmb/shiva/internal/cli/request"
	"github.com/iw2rmb/shiva/internal/cli/target"
)

func TestRuntimeServicePlanCallUsesTargetSourceProfile(t *testing.T) {
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
		callBody:       []byte(`{"kind":"call"}`),
	}

	var resolvedProfiles []string
	service := &RuntimeService{
		document: config.Document{
			ActiveProfile: "default",
			Profiles: map[string]profile.Source{
				"default": {Name: "default", BaseURL: "http://default.example", Timeout: 5 * time.Second},
				"prod":    {Name: "prod", BaseURL: "http://prod.example", Timeout: 5 * time.Second},
			},
			Targets: map[string]target.Entry{
				target.BuiltinShivaName: target.BuiltinShiva(),
				"prod":                  {Name: "prod", Mode: target.ModeDirect, SourceProfile: "prod", BaseURL: "https://api.example", Timeout: 5 * time.Second},
			},
		},
		catalogService: catalog.NewService(store),
		catalogStore:   store,
		newClient: func(source profile.Source) (transportClient, error) {
			resolvedProfiles = append(resolvedProfiles, source.Name)
			return client, nil
		},
	}

	_, err = service.PlanCall(context.Background(), request.Envelope{
		Repo:        "acme/platform",
		Target:      "prod",
		OperationID: "listPets",
	}, RequestOptions{})
	if err != nil {
		t.Fatalf("plan call failed: %v", err)
	}

	if !reflect.DeepEqual(resolvedProfiles, []string{"prod"}) {
		t.Fatalf("expected target source-profile override to select prod, got %+v", resolvedProfiles)
	}
	expected := request.Envelope{
		Kind:        request.KindCall,
		Repo:        "acme/platform",
		RevisionID:  42,
		Target:      "prod",
		OperationID: "listPets",
	}
	if !reflect.DeepEqual(client.lastCallSelector, expected) {
		t.Fatalf("expected normalized call selector %+v, got %+v", expected, client.lastCallSelector)
	}
}

func TestRuntimeServicePinsFloatingRequestsToPreparedSnapshot(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		run    func(t *testing.T, service *RuntimeService) error
		assert func(t *testing.T, client *recordingTransportClient)
		client *recordingTransportClient
	}{
		{
			name: "spec",
			client: &recordingTransportClient{
				reposBody:  []byte(`[{"repo":"acme/platform"}]`),
				statusBody: []byte(`{"repo":"acme/platform","snapshot_revision":{"id":42,"sha":"deadbeef"}}`),
				apisBody:   []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`),
				specBody:   []byte("openapi: 3.1.0\n"),
			},
			run: func(t *testing.T, service *RuntimeService) error {
				_, err := service.GetSpec(context.Background(), request.Envelope{
					Repo: "acme/platform",
				}, RequestOptions{}, SpecFormatYAML)
				return err
			},
			assert: func(t *testing.T, client *recordingTransportClient) {
				if client.lastSpecSelector.RevisionID != 42 {
					t.Fatalf("expected floating spec request to pin revision 42, got %+v", client.lastSpecSelector)
				}
			},
		},
		{
			name: "operation",
			client: &recordingTransportClient{
				reposBody:      []byte(`[{"repo":"acme/platform"}]`),
				statusBody:     []byte(`{"repo":"acme/platform","snapshot_revision":{"id":42,"sha":"deadbeef"}}`),
				apisBody:       []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`),
				operationsBody: []byte(`[{"api":"apis/pets/openapi.yaml","method":"get","path":"/pets","operation_id":"listPets","operation":{"operationId":"listPets"}}]`),
				operationBody:  []byte(`{"operationId":"listPets"}`),
			},
			run: func(t *testing.T, service *RuntimeService) error {
				_, err := service.GetOperation(context.Background(), request.Envelope{
					Repo:        "acme/platform",
					OperationID: "listPets",
				}, RequestOptions{})
				return err
			},
			assert: func(t *testing.T, client *recordingTransportClient) {
				if client.lastOperationSelector.RevisionID != 42 {
					t.Fatalf("expected floating operation request to pin revision 42, got %+v", client.lastOperationSelector)
				}
			},
		},
		{
			name: "call",
			client: &recordingTransportClient{
				reposBody:      []byte(`[{"repo":"acme/platform"}]`),
				statusBody:     []byte(`{"repo":"acme/platform","snapshot_revision":{"id":42,"sha":"deadbeef"}}`),
				apisBody:       []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`),
				operationsBody: []byte(`[{"api":"apis/pets/openapi.yaml","method":"get","path":"/pets","operation_id":"listPets","operation":{"operationId":"listPets"}}]`),
				callBody:       []byte(`{"kind":"call"}`),
			},
			run: func(t *testing.T, service *RuntimeService) error {
				_, err := service.PlanCall(context.Background(), request.Envelope{
					Repo:        "acme/platform",
					OperationID: "listPets",
					Target:      "shiva",
				}, RequestOptions{})
				return err
			},
			assert: func(t *testing.T, client *recordingTransportClient) {
				if client.lastCallSelector.RevisionID != 42 {
					t.Fatalf("expected floating call request to pin revision 42, got %+v", client.lastCallSelector)
				}
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store, err := catalog.NewStore(t.TempDir())
			if err != nil {
				t.Fatalf("create catalog store: %v", err)
			}

			service := &RuntimeService{
				document: config.Document{
					ActiveProfile: "default",
					Profiles: map[string]profile.Source{
						"default": {Name: "default", BaseURL: "http://default.example", Timeout: 5 * time.Second},
					},
					Targets: map[string]target.Entry{
						target.BuiltinShivaName: target.BuiltinShiva(),
					},
				},
				catalogService: catalog.NewService(store),
				catalogStore:   store,
				newClient: func(source profile.Source) (transportClient, error) {
					_ = source
					return testCase.client, nil
				},
			}

			if err := testCase.run(t, service); err != nil {
				t.Fatalf("run service method failed: %v", err)
			}
			testCase.assert(t, testCase.client)
		})
	}
}

func TestRuntimeServiceGetOperationOfflineUsesCachedResponseOnly(t *testing.T) {
	t.Parallel()

	store, err := catalog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}

	onlineClient := &recordingTransportClient{
		reposBody:      []byte(`[{"repo":"acme/platform"}]`),
		apisBody:       []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`),
		operationsBody: []byte(`[{"api":"apis/pets/openapi.yaml","method":"get","path":"/pets","operation_id":"listPets","operation":{"operationId":"listPets"}}]`),
		operationBody:  []byte(`{"operationId":"listPets"}`),
	}
	offlineClient := &recordingTransportClient{}

	currentClient := onlineClient
	service := &RuntimeService{
		document: config.Document{
			ActiveProfile: "default",
			Profiles: map[string]profile.Source{
				"default": {Name: "default", BaseURL: "http://default.example", Timeout: 5 * time.Second},
			},
			Targets: map[string]target.Entry{
				target.BuiltinShivaName: target.BuiltinShiva(),
			},
		},
		catalogService: catalog.NewService(store),
		catalogStore:   store,
		newClient: func(source profile.Source) (transportClient, error) {
			_ = source
			return currentClient, nil
		},
	}

	selector := request.Envelope{
		Repo:        "acme/platform",
		RevisionID:  42,
		OperationID: "listPets",
	}

	body, err := service.GetOperation(context.Background(), selector, RequestOptions{})
	if err != nil {
		t.Fatalf("online get operation failed: %v", err)
	}
	if string(body) != `{"operationId":"listPets"}` {
		t.Fatalf("unexpected online body %q", string(body))
	}

	currentClient = offlineClient

	cachedBody, err := service.GetOperation(context.Background(), selector, RequestOptions{
		Offline: true,
	})
	if err != nil {
		t.Fatalf("offline get operation failed: %v", err)
	}
	if string(cachedBody) != `{"operationId":"listPets"}` {
		t.Fatalf("unexpected cached body %q", string(cachedBody))
	}
	if offlineClient.reposCalls != 0 || offlineClient.apisCalls != 0 || offlineClient.operationsCalls != 0 || offlineClient.operationCalls != 0 {
		t.Fatalf("expected offline call to avoid network, got repos=%d apis=%d ops=%d op=%d",
			offlineClient.reposCalls,
			offlineClient.apisCalls,
			offlineClient.operationsCalls,
			offlineClient.operationCalls,
		)
	}
}

func TestRuntimeServiceGetOperationOfflineUsesExplicitCacheWithoutCatalogSlices(t *testing.T) {
	t.Parallel()

	store, err := catalog.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}

	scope := catalog.ScopeFromSelector(42, "")
	if err := store.SaveOperationResponse(
		"default",
		"acme/platform",
		"",
		scope,
		"operation_id:listPets",
		[]byte(`{"operationId":"listPets"}`),
		catalog.SnapshotFingerprint{RevisionID: 42},
	); err != nil {
		t.Fatalf("seed cached operation response: %v", err)
	}

	client := &recordingTransportClient{}
	service := &RuntimeService{
		document: config.Document{
			ActiveProfile: "default",
			Profiles: map[string]profile.Source{
				"default": {Name: "default", BaseURL: "http://default.example", Timeout: 5 * time.Second},
			},
			Targets: map[string]target.Entry{
				target.BuiltinShivaName: target.BuiltinShiva(),
			},
		},
		catalogService: catalog.NewService(store),
		catalogStore:   store,
		newClient: func(source profile.Source) (transportClient, error) {
			_ = source
			return client, nil
		},
	}

	body, err := service.GetOperation(context.Background(), request.Envelope{
		Repo:        "acme/platform",
		RevisionID:  42,
		OperationID: "listPets",
	}, RequestOptions{
		Offline: true,
	})
	if err != nil {
		t.Fatalf("offline get operation failed: %v", err)
	}
	if string(body) != `{"operationId":"listPets"}` {
		t.Fatalf("unexpected cached body %q", string(body))
	}
	if client.reposCalls != 0 || client.apisCalls != 0 || client.operationsCalls != 0 || client.operationCalls != 0 {
		t.Fatalf("expected explicit offline cache hit to avoid network, got repos=%d apis=%d ops=%d op=%d",
			client.reposCalls,
			client.apisCalls,
			client.operationsCalls,
			client.operationCalls,
		)
	}
}

func TestConvertJSONToYAML(t *testing.T) {
	t.Parallel()

	body, err := ConvertJSONToYAML([]byte(`{"operationId":"patchPet"}`))
	if err != nil {
		t.Fatalf("convert json to yaml failed: %v", err)
	}
	if string(body) != "operationId: patchPet\n" {
		t.Fatalf("unexpected yaml body %q", string(body))
	}
}

type recordingTransportClient struct {
	reposBody             []byte
	statusBody            []byte
	apisBody              []byte
	operationsBody        []byte
	specBody              []byte
	operationBody         []byte
	callBody              []byte
	healthBody            []byte
	reposCalls            int
	statusCalls           int
	apisCalls             int
	operationsCalls       int
	specCalls             int
	operationCalls        int
	callCalls             int
	lastSpecSelector      request.Envelope
	lastOperationSelector request.Envelope
	lastCallSelector      request.Envelope
	lastCatalogRepo       string
}

func (c *recordingTransportClient) GetSpec(ctx context.Context, selector request.Envelope, format SpecFormat) ([]byte, error) {
	c.specCalls++
	c.lastSpecSelector = selector
	_ = format
	return c.specBody, nil
}

func (c *recordingTransportClient) GetOperation(ctx context.Context, selector request.Envelope) ([]byte, error) {
	c.operationCalls++
	c.lastOperationSelector = selector
	return c.operationBody, nil
}

func (c *recordingTransportClient) PlanCall(ctx context.Context, selector request.Envelope) ([]byte, error) {
	c.callCalls++
	c.lastCallSelector = selector
	return c.callBody, nil
}

func (c *recordingTransportClient) ListRepos(ctx context.Context) ([]byte, error) {
	c.reposCalls++
	return c.reposBody, nil
}

func (c *recordingTransportClient) GetCatalogStatus(ctx context.Context, repo string) ([]byte, error) {
	c.statusCalls++
	c.lastCatalogRepo = repo
	return c.statusBody, nil
}

func (c *recordingTransportClient) ListAPIs(ctx context.Context, selector request.Envelope) ([]byte, error) {
	c.apisCalls++
	c.lastSpecSelector = selector
	return c.apisBody, nil
}

func (c *recordingTransportClient) ListOperations(ctx context.Context, selector request.Envelope) ([]byte, error) {
	c.operationsCalls++
	c.lastOperationSelector = selector
	return c.operationsBody, nil
}

func (c *recordingTransportClient) Health(ctx context.Context) ([]byte, error) {
	return c.healthBody, nil
}
