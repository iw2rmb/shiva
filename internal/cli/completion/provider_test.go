package completion

import (
	"context"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/catalog"
	"github.com/iw2rmb/shiva/internal/cli/config"
	"github.com/iw2rmb/shiva/internal/cli/profile"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestProviderListReposUsesRefreshedValuesWhenCacheIsStale(t *testing.T) {
	cacheHome := t.TempDir()
	store, err := catalog.NewStore(cacheHome)
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}
	if err := store.SaveRepos("default", []byte(`[{"repo":"cached/repo"}]`)); err != nil {
		t.Fatalf("save cached repos: %v", err)
	}

	provider := &Provider{
		ResolvePaths: func() (config.Paths, error) {
			return config.Paths{ConfigHome: t.TempDir(), CacheHome: cacheHome}, nil
		},
		LoadDocument: func(options config.LoadOptions) (config.Document, error) {
			return config.Document{
				ActiveProfile: "default",
				Profiles: map[string]profile.Source{
					"default": {Name: "default", BaseURL: "http://example.test", Timeout: time.Second},
				},
			}, nil
		},
		NewStore: func(cacheHome string) (*catalog.Store, error) {
			return store, nil
		},
		NewClient: func(source profile.Source, timeout time.Duration) (inventoryClient, error) {
			return fakeInventoryClient{
				listRepos: func(ctx context.Context) ([]byte, error) {
					return []byte(`[{"repo":"fresh/repo"}]`), nil
				},
			}, nil
		},
		Now:            func() time.Time { return time.Now().Add(10 * time.Minute) },
		RefreshTimeout: 50 * time.Millisecond,
	}

	values, err := provider.listRepos(context.Background(), "", "")
	if err != nil {
		t.Fatalf("list repos: %v", err)
	}
	if len(values) != 1 || values[0] != "fresh/repo" {
		t.Fatalf("expected refreshed repos, got %#v", values)
	}
}

func TestProviderListReposFallsBackToCachedValuesOnTimeout(t *testing.T) {
	cacheHome := t.TempDir()
	store, err := catalog.NewStore(cacheHome)
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}
	if err := store.SaveRepos("default", []byte(`[{"repo":"cached/repo"}]`)); err != nil {
		t.Fatalf("save cached repos: %v", err)
	}

	provider := &Provider{
		ResolvePaths: func() (config.Paths, error) {
			return config.Paths{ConfigHome: t.TempDir(), CacheHome: cacheHome}, nil
		},
		LoadDocument: func(options config.LoadOptions) (config.Document, error) {
			return config.Document{
				ActiveProfile: "default",
				Profiles: map[string]profile.Source{
					"default": {Name: "default", BaseURL: "http://example.test", Timeout: time.Second},
				},
			}, nil
		},
		NewStore: func(cacheHome string) (*catalog.Store, error) {
			return store, nil
		},
		NewClient: func(source profile.Source, timeout time.Duration) (inventoryClient, error) {
			return fakeInventoryClient{
				listRepos: func(ctx context.Context) ([]byte, error) {
					select {
					case <-time.After(200 * time.Millisecond):
						return []byte(`[{"repo":"fresh/repo"}]`), nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				},
			}, nil
		},
		Now:            func() time.Time { return time.Now().Add(10 * time.Minute) },
		RefreshTimeout: 20 * time.Millisecond,
	}

	startedAt := time.Now()
	values, err := provider.listRepos(context.Background(), "", "")
	if err != nil {
		t.Fatalf("list repos: %v", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 150*time.Millisecond {
		t.Fatalf("expected timeout-bounded completion, got %s", elapsed)
	}
	if len(values) != 1 || values[0] != "cached/repo" {
		t.Fatalf("expected cached repos fallback, got %#v", values)
	}
}

type fakeInventoryClient struct {
	listRepos      func(ctx context.Context) ([]byte, error)
	getStatus      func(ctx context.Context, repo string) ([]byte, error)
	listAPIs       func(ctx context.Context, selector request.Envelope) ([]byte, error)
	listOperations func(ctx context.Context, selector request.Envelope) ([]byte, error)
}

func (c fakeInventoryClient) ListRepos(ctx context.Context) ([]byte, error) {
	if c.listRepos == nil {
		return nil, nil
	}
	return c.listRepos(ctx)
}

func (c fakeInventoryClient) GetCatalogStatus(ctx context.Context, repo string) ([]byte, error) {
	if c.getStatus == nil {
		return nil, nil
	}
	return c.getStatus(ctx, repo)
}

func (c fakeInventoryClient) ListAPIs(ctx context.Context, selector request.Envelope) ([]byte, error) {
	if c.listAPIs == nil {
		return nil, nil
	}
	return c.listAPIs(ctx, selector)
}

func (c fakeInventoryClient) ListOperations(ctx context.Context, selector request.Envelope) ([]byte, error) {
	if c.listOperations == nil {
		return nil, nil
	}
	return c.listOperations(ctx, selector)
}
