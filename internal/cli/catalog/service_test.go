package catalog

import (
	"context"
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestServicePrepareOperationFloatingRefreshesLazily(t *testing.T) {
	t.Parallel()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	client := &fakeInventoryClient{
		reposBody:      []byte(`[{"namespace":"acme","repo":"platform"}]`),
		statusBody:     []byte(`{"namespace":"acme","repo":"platform","snapshot_revision":{"id":42,"sha":"deadbeef"}}`),
		apisBody:       []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`),
		operationsBody: []byte(`[{"api":"apis/pets/openapi.yaml","operation_id":"listPets"}]`),
	}
	service := NewService(store)
	selector := request.Envelope{Namespace: "acme", Repo: "platform"}

	first, err := service.PrepareOperation(context.Background(), client, "default", selector, RefreshOptions{})
	if err != nil {
		t.Fatalf("first prepare failed: %v", err)
	}
	if first.Fingerprint.RevisionID != 42 || first.Fingerprint.SHA != "deadbeef" {
		t.Fatalf("unexpected first fingerprint %+v", first.Fingerprint)
	}
	if client.reposCalls != 1 || client.statusCalls != 1 || client.apisCalls != 1 || client.operationsCalls != 1 {
		t.Fatalf("unexpected first refresh counts repos=%d status=%d apis=%d ops=%d",
			client.reposCalls,
			client.statusCalls,
			client.apisCalls,
			client.operationsCalls,
		)
	}

	client.reset()

	second, err := service.PrepareOperation(context.Background(), client, "default", selector, RefreshOptions{})
	if err != nil {
		t.Fatalf("second prepare failed: %v", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("expected second prepare to reuse cached fingerprint, got first=%+v second=%+v", first, second)
	}
	if client.reposCalls != 0 || client.statusCalls != 0 || client.apisCalls != 0 || client.operationsCalls != 0 {
		t.Fatalf("expected cached floating prepare to avoid refresh, got repos=%d status=%d apis=%d ops=%d",
			client.reposCalls,
			client.statusCalls,
			client.apisCalls,
			client.operationsCalls,
		)
	}
}

func TestServicePrepareOperationPinnedOfflineReusesImmutableCache(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		selector request.Envelope
	}{
		{
			name: "revision id",
			selector: request.Envelope{
				Namespace: "acme", Repo: "platform",
				RevisionID: 42,
			},
		},
		{
			name: "sha",
			selector: request.Envelope{
				Namespace: "acme", Repo: "platform",
				SHA:  "deadbeef",
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatalf("create store: %v", err)
			}

			service := NewService(store)
			onlineClient := &fakeInventoryClient{
				reposBody:      []byte(`[{"namespace":"acme","repo":"platform"}]`),
				apisBody:       []byte(`[{"api":"apis/pets/openapi.yaml","has_snapshot":true}]`),
				operationsBody: []byte(`[{"api":"apis/pets/openapi.yaml","operation_id":"listPets"}]`),
			}

			_, err = service.PrepareOperation(context.Background(), onlineClient, "default", testCase.selector, RefreshOptions{})
			if err != nil {
				t.Fatalf("prime cache failed: %v", err)
			}
			if onlineClient.reposCalls != 0 {
				t.Fatalf("expected pinned selector to skip repo inventory refresh, got %d calls", onlineClient.reposCalls)
			}
			if onlineClient.statusCalls != 0 {
				t.Fatalf("expected pinned selector to skip catalog status, got %d calls", onlineClient.statusCalls)
			}

			offlineClient := &fakeInventoryClient{}
			_, err = service.PrepareOperation(context.Background(), offlineClient, "default", testCase.selector, RefreshOptions{
				Offline: true,
			})
			if err != nil {
				t.Fatalf("offline prepare failed: %v", err)
			}
			if offlineClient.reposCalls != 0 || offlineClient.statusCalls != 0 || offlineClient.apisCalls != 0 || offlineClient.operationsCalls != 0 {
				t.Fatalf("expected offline pinned prepare to avoid network, got repos=%d status=%d apis=%d ops=%d",
					offlineClient.reposCalls,
					offlineClient.statusCalls,
					offlineClient.apisCalls,
					offlineClient.operationsCalls,
				)
			}
		})
	}
}

type fakeInventoryClient struct {
	reposBody       []byte
	statusBody      []byte
	apisBody        []byte
	operationsBody  []byte
	reposCalls      int
	statusCalls     int
	apisCalls       int
	operationsCalls int
}

func (c *fakeInventoryClient) ListRepos(ctx context.Context) ([]byte, error) {
	c.reposCalls++
	return c.reposBody, nil
}

func (c *fakeInventoryClient) GetCatalogStatus(ctx context.Context, repo string) ([]byte, error) {
	c.statusCalls++
	_ = repo
	return c.statusBody, nil
}

func (c *fakeInventoryClient) ListAPIs(ctx context.Context, selector request.Envelope) ([]byte, error) {
	c.apisCalls++
	_ = selector
	return c.apisBody, nil
}

func (c *fakeInventoryClient) ListOperations(ctx context.Context, selector request.Envelope) ([]byte, error) {
	c.operationsCalls++
	_ = selector
	return c.operationsBody, nil
}

func (c *fakeInventoryClient) reset() {
	c.reposCalls = 0
	c.statusCalls = 0
	c.apisCalls = 0
	c.operationsCalls = 0
}
