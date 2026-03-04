package store

import (
	"context"
	"errors"
	"testing"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestResolveReadSelector_BranchPrefersLatestProcessedOpenAPIRevision(t *testing.T) {
	t.Parallel()

	queries := &fakeSelectorResolutionQueries{
		tenant: sqlc.Tenant{ID: 3, Key: "tenant-a"},
		repo: sqlc.Repo{
			ID:                44,
			TenantID:          3,
			PathWithNamespace: "acme/platform-api",
			DefaultBranch:     "main",
		},
		latestByBranch: map[string]sqlc.Revision{
			"release": newSelectorTestRevision(500, "processed", boolPtr(false), "release", "sha-head"),
		},
		openAPIByBranch: map[string]sqlc.Revision{
			"release": newSelectorTestRevision(490, "processed", boolPtr(true), "release", "sha-artifact"),
		},
	}

	resolved, err := resolveReadSelector(context.Background(), queries, normalizedResolveReadSelectorInput{
		tenantKey: "tenant-a",
		repoPath:  "acme/platform-api",
		selector:  "release",
		kind:      SelectorKindBranch,
	})
	if err != nil {
		t.Fatalf("resolveReadSelector() unexpected error: %v", err)
	}

	if resolved.Revision.ID != 490 {
		t.Fatalf("expected artifact revision id=490, got %d", resolved.Revision.ID)
	}
	if resolved.SelectorKind != SelectorKindBranch {
		t.Fatalf("expected selector kind %q, got %q", SelectorKindBranch, resolved.SelectorKind)
	}
}

func TestResolveReadSelector_BranchHeadUnprocessedReturnsConflict(t *testing.T) {
	t.Parallel()

	queries := &fakeSelectorResolutionQueries{
		tenant: sqlc.Tenant{ID: 3, Key: "tenant-a"},
		repo: sqlc.Repo{
			ID:                44,
			TenantID:          3,
			PathWithNamespace: "acme/platform-api",
			DefaultBranch:     "main",
		},
		latestByBranch: map[string]sqlc.Revision{
			"main": newSelectorTestRevision(501, "pending", nil, "main", "sha-head"),
		},
	}

	_, err := resolveReadSelector(context.Background(), queries, normalizedResolveReadSelectorInput{
		tenantKey: "tenant-a",
		repoPath:  "acme/platform-api",
		selector:  "main",
		kind:      SelectorKindBranch,
	})
	if err == nil {
		t.Fatalf("expected unprocessed selector error")
	}
	if !errors.Is(err, ErrSelectorUnprocessed) {
		t.Fatalf("expected ErrSelectorUnprocessed, got %v", err)
	}

	var selectorErr *SelectorResolutionError
	if !errors.As(err, &selectorErr) {
		t.Fatalf("expected SelectorResolutionError, got %T", err)
	}
	if selectorErr.RevisionID != 501 || selectorErr.RevisionStatus != "pending" {
		t.Fatalf("unexpected selector error metadata: %+v", selectorErr)
	}
}

func TestResolveReadSelector_SHAProcessedWithoutOpenAPIArtifactReturnsNotFound(t *testing.T) {
	t.Parallel()

	queries := &fakeSelectorResolutionQueries{
		tenant: sqlc.Tenant{ID: 3, Key: "tenant-a"},
		repo: sqlc.Repo{
			ID:                44,
			TenantID:          3,
			PathWithNamespace: "acme/platform-api",
			DefaultBranch:     "main",
		},
		bySHA: map[string]sqlc.Revision{
			"1111111111111111111111111111111111111111": newSelectorTestRevision(
				700,
				"processed",
				boolPtr(false),
				"main",
				"1111111111111111111111111111111111111111",
			),
		},
	}

	_, err := resolveReadSelector(context.Background(), queries, normalizedResolveReadSelectorInput{
		tenantKey: "tenant-a",
		repoPath:  "acme/platform-api",
		selector:  "1111111111111111111111111111111111111111",
		kind:      SelectorKindSHA,
	})
	if err == nil {
		t.Fatalf("expected not found selector error")
	}
	if !errors.Is(err, ErrSelectorNotFound) {
		t.Fatalf("expected ErrSelectorNotFound, got %v", err)
	}
}

func TestResolveReadSelector_LatestUsesRepoDefaultBranch(t *testing.T) {
	t.Parallel()

	queries := &fakeSelectorResolutionQueries{
		tenant: sqlc.Tenant{ID: 3, Key: "tenant-a"},
		repo: sqlc.Repo{
			ID:                44,
			TenantID:          3,
			PathWithNamespace: "acme/platform-api",
			DefaultBranch:     "release",
		},
		latestByBranch: map[string]sqlc.Revision{
			"release": newSelectorTestRevision(800, "processed", boolPtr(true), "release", "sha-head"),
		},
		openAPIByBranch: map[string]sqlc.Revision{
			"release": newSelectorTestRevision(800, "processed", boolPtr(true), "release", "sha-head"),
		},
	}

	resolved, err := resolveReadSelector(context.Background(), queries, normalizedResolveReadSelectorInput{
		tenantKey: "tenant-a",
		repoPath:  "acme/platform-api",
		selector:  "latest",
		kind:      SelectorKindLatest,
	})
	if err != nil {
		t.Fatalf("resolveReadSelector() unexpected error: %v", err)
	}

	if resolved.Revision.ID != 800 {
		t.Fatalf("expected revision id=800, got %d", resolved.Revision.ID)
	}
	if queries.lastHeadBranch != "release" {
		t.Fatalf("expected latest to resolve against repo default branch release, got %q", queries.lastHeadBranch)
	}
}

func TestNormalizeResolveReadSelectorInput(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       ResolveReadSelectorInput
		kind        SelectorKind
		selector    string
		expectError bool
	}{
		{
			name: "tenant key is required",
			input: ResolveReadSelectorInput{
				TenantKey: "",
				RepoPath:  "acme/platform-api",
				Selector:  "main",
			},
			expectError: true,
		},
		{
			name: "repo path is required",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "   ",
				Selector:  "main",
			},
			expectError: true,
		},
		{
			name: "sha selector normalized to lower-case",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
			},
			kind:     SelectorKindSHA,
			selector: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		{
			name: "latest selector is case-insensitive",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "LaTeSt",
			},
			kind:     SelectorKindLatest,
			selector: "latest",
		},
		{
			name: "branch selector keeps value",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "release/v1",
			},
			kind:     SelectorKindBranch,
			selector: "release/v1",
		},
		{
			name: "no selector mode",
			input: ResolveReadSelectorInput{
				TenantKey:  "tenant-a",
				RepoPath:   "acme/platform-api",
				NoSelector: true,
			},
			kind: SelectorKindNoSelector,
		},
		{
			name: "no selector mode rejects explicit selector",
			input: ResolveReadSelectorInput{
				TenantKey:  "tenant-a",
				RepoPath:   "acme/platform-api",
				Selector:   "main",
				NoSelector: true,
			},
			expectError: true,
		},
		{
			name: "selector required when no-selector is false",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
			},
			expectError: true,
		},
		{
			name: "40-char non-hex selector is treated as branch",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "gggggggggggggggggggggggggggggggggggggggg",
			},
			kind:     SelectorKindBranch,
			selector: "gggggggggggggggggggggggggggggggggggggggg",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			normalized, err := normalizeResolveReadSelectorInput(testCase.input)
			if testCase.expectError {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeResolveReadSelectorInput() unexpected error: %v", err)
			}

			if normalized.kind != testCase.kind {
				t.Fatalf("expected kind %q, got %q", testCase.kind, normalized.kind)
			}
			if normalized.selector != testCase.selector {
				t.Fatalf("expected selector %q, got %q", testCase.selector, normalized.selector)
			}
		})
	}
}

type fakeSelectorResolutionQueries struct {
	tenant sqlc.Tenant
	repo   sqlc.Repo

	bySHA           map[string]sqlc.Revision
	latestByBranch  map[string]sqlc.Revision
	openAPIByBranch map[string]sqlc.Revision

	lastHeadBranch string
}

func (f *fakeSelectorResolutionQueries) GetTenantByKey(_ context.Context, key string) (sqlc.Tenant, error) {
	if f.tenant.ID == 0 || f.tenant.Key != key {
		return sqlc.Tenant{}, pgx.ErrNoRows
	}
	return f.tenant, nil
}

func (f *fakeSelectorResolutionQueries) GetRepoByTenantAndPath(
	_ context.Context,
	arg sqlc.GetRepoByTenantAndPathParams,
) (sqlc.Repo, error) {
	if f.repo.ID == 0 || f.repo.TenantID != arg.TenantID || f.repo.PathWithNamespace != arg.PathWithNamespace {
		return sqlc.Repo{}, pgx.ErrNoRows
	}
	return f.repo, nil
}

func (f *fakeSelectorResolutionQueries) GetRevisionByRepoSHA(
	_ context.Context,
	arg sqlc.GetRevisionByRepoSHAParams,
) (sqlc.Revision, error) {
	if f.bySHA == nil {
		return sqlc.Revision{}, pgx.ErrNoRows
	}
	revision, ok := f.bySHA[arg.Sha]
	if !ok {
		return sqlc.Revision{}, pgx.ErrNoRows
	}
	return revision, nil
}

func (f *fakeSelectorResolutionQueries) GetLatestRevisionByBranch(
	_ context.Context,
	arg sqlc.GetLatestRevisionByBranchParams,
) (sqlc.Revision, error) {
	f.lastHeadBranch = arg.Branch
	if f.latestByBranch == nil {
		return sqlc.Revision{}, pgx.ErrNoRows
	}
	revision, ok := f.latestByBranch[arg.Branch]
	if !ok {
		return sqlc.Revision{}, pgx.ErrNoRows
	}
	return revision, nil
}

func (f *fakeSelectorResolutionQueries) GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
	_ context.Context,
	arg sqlc.GetLatestProcessedOpenAPIRevisionByBranchExcludingIDParams,
) (sqlc.Revision, error) {
	if f.openAPIByBranch == nil {
		return sqlc.Revision{}, pgx.ErrNoRows
	}
	revision, ok := f.openAPIByBranch[arg.Branch]
	if !ok {
		return sqlc.Revision{}, pgx.ErrNoRows
	}
	return revision, nil
}

func newSelectorTestRevision(id int64, status string, openAPIChanged *bool, branch string, sha string) sqlc.Revision {
	revision := sqlc.Revision{
		ID:     id,
		Status: status,
		Branch: branch,
		Sha:    sha,
	}
	if openAPIChanged != nil {
		revision.OpenapiChanged = pgtype.Bool{Bool: *openAPIChanged, Valid: true}
	}
	return revision
}

func boolPtr(value bool) *bool {
	return &value
}
