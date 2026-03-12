package completion

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/cli/catalog"
	"github.com/iw2rmb/shiva/internal/cli/config"
	clioutput "github.com/iw2rmb/shiva/internal/cli/output"
	"github.com/iw2rmb/shiva/internal/cli/profile"
	"github.com/iw2rmb/shiva/internal/cli/request"
)

func TestProviderListReposUsesRefreshedValuesWhenCacheIsStale(t *testing.T) {
	cacheHome := t.TempDir()
	store, err := catalog.NewStore(cacheHome)
	if err != nil {
		t.Fatalf("create catalog store: %v", err)
	}
	if err := store.SaveRepos("default", []byte(`[{"namespace":"cached","repo":"repo"}]`)); err != nil {
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
					return []byte(`[{"namespace":"fresh","repo":"repo"}]`), nil
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
	if err := store.SaveRepos("default", []byte(`[{"namespace":"cached","repo":"repo"}]`)); err != nil {
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
						return []byte(`[{"namespace":"fresh","repo":"repo"}]`), nil
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

func TestCompleteRepoPrefixReturnsNamespaceHierarchy(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)
	values := []clioutput.RepoRow{
		{
			Namespace: "accounts",
			Repo:      "forms",
			HeadRevision: &clioutput.RevisionState{
				Status:      "processed",
				ProcessedAt: &updatedAt,
			},
		},
		{
			Namespace: "accounts",
			Repo:      "offers",
			HeadRevision: &clioutput.RevisionState{
				Status: "pending",
			},
		},
		{
			Namespace: "ai-platform",
			Repo:      "airflow-ui",
			HeadRevision: &clioutput.RevisionState{
				Status: "pending",
			},
		},
		{
			Namespace: "voicekit",
			Repo:      "public-api",
			HeadRevision: &clioutput.RevisionState{
				Status: "pending",
			},
		},
	}

	testCases := []struct {
		name        string
		prefix      string
		want        []string
		wantNoSpace bool
	}{
		{
			name:        "empty prefix returns top-level namespaces",
			prefix:      "",
			want:        []string{"accounts/\tnamespace, 2 repos", "ai-platform/\tnamespace, 1 repos, all pending", "voicekit/\tnamespace, 1 repos, all pending"},
			wantNoSpace: true,
		},
		{
			name:        "partial namespace returns namespace matches",
			prefix:      "acc",
			want:        []string{"accounts/\tnamespace, 2 repos"},
			wantNoSpace: true,
		},
		{
			name:        "namespace slash returns repos within namespace",
			prefix:      "accounts/",
			want:        []string{"accounts/forms\tupdated 2026-03-10", "accounts/offers\tpending"},
			wantNoSpace: false,
		},
		{
			name:        "partial repo returns matching repo leaves",
			prefix:      "accounts/of",
			want:        []string{"accounts/offers\tpending"},
			wantNoSpace: false,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, gotNoSpace := completeRepoPrefix(values, testCase.prefix)
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("completeRepoPrefix(%q) = %#v, want %#v", testCase.prefix, got, testCase.want)
			}
			if gotNoSpace != testCase.wantNoSpace {
				t.Fatalf("completeRepoPrefix(%q) noSpace = %t, want %t", testCase.prefix, gotNoSpace, testCase.wantNoSpace)
			}
		})
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
