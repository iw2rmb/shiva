package store

import (
	"context"
	"errors"
	"testing"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestResolveReadSelector_NoSelectorDefaultsToMainHead(t *testing.T) {
	t.Parallel()

	queries := newSelectorTestQueries()
	queries.repo.DefaultBranch = "release"
	queries.latestByBranch = map[string]sqlc.Revision{
		mainBranchName: newSelectorTestRevision(500, "processed", boolPtr(false), mainBranchName, "main-head"),
	}
	queries.openAPIByBranch = map[string]sqlc.Revision{
		mainBranchName: newSelectorTestRevision(490, "processed", boolPtr(true), mainBranchName, "main-artifact"),
	}

	resolved, err := resolveReadSelector(context.Background(), queries, newSelectorTestInput(SelectorKindNoSelector, ""))
	if err != nil {
		t.Fatalf("resolveReadSelector() unexpected error: %v", err)
	}

	if resolved.Revision.ID != 490 {
		t.Fatalf("expected artifact revision id=490, got %d", resolved.Revision.ID)
	}
	if resolved.SelectorKind != SelectorKindNoSelector {
		t.Fatalf("expected selector kind %q, got %q", SelectorKindNoSelector, resolved.SelectorKind)
	}
	if queries.lastHeadBranch != mainBranchName {
		t.Fatalf("expected no-selector to resolve using branch %q, got %q", mainBranchName, queries.lastHeadBranch)
	}
}

func TestResolveReadSelector_NoSelectorHeadUnprocessedReturnsConflict(t *testing.T) {
	t.Parallel()

	queries := newSelectorTestQueries()
	queries.latestByBranch = map[string]sqlc.Revision{
		mainBranchName: newSelectorTestRevision(501, "pending", nil, mainBranchName, "main-head"),
	}

	_, err := resolveReadSelector(context.Background(), queries, newSelectorTestInput(SelectorKindNoSelector, ""))
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

	queries := newSelectorTestQueries()
	queries.bySHAPrefix = map[string]sqlc.Revision{
		"11111111": newSelectorTestRevision(
			700,
			"processed",
			boolPtr(false),
			"main",
			"1111111111111111111111111111111111111111",
		),
	}

	_, err := resolveReadSelector(context.Background(), queries, newSelectorTestInput(SelectorKindSHA, "11111111"))
	if err == nil {
		t.Fatalf("expected not found selector error")
	}
	if !errors.Is(err, ErrSelectorNotFound) {
		t.Fatalf("expected ErrSelectorNotFound, got %v", err)
	}
}

func TestResolveReadSelector_NotFoundShortSHA(t *testing.T) {
	t.Parallel()

	queries := newSelectorTestQueries()

	_, err := resolveReadSelector(context.Background(), queries, newSelectorTestInput(SelectorKindSHA, "deadbeef"))
	if err == nil {
		t.Fatalf("expected not found selector error")
	}
	if !errors.Is(err, ErrSelectorNotFound) {
		t.Fatalf("expected ErrSelectorNotFound, got %v", err)
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
				Selector:  "a1b2c3d4",
			},
			expectError: true,
		},
		{
			name: "repo path is required",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "   ",
				Selector:  "a1b2c3d4",
			},
			expectError: true,
		},
		{
			name: "short selector normalized to lower-case",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "a1b2c3d4",
			},
			kind:     SelectorKindSHA,
			selector: "a1b2c3d4",
		},
		{
			name: "invalid short SHA length",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "1234567",
			},
			expectError: true,
		},
		{
			name: "invalid short SHA charset",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "g1b2c3d4",
			},
			expectError: true,
		},
		{
			name: "upper-case short SHA rejected",
			input: ResolveReadSelectorInput{
				TenantKey: "tenant-a",
				RepoPath:  "acme/platform-api",
				Selector:  "A1B2C3D4",
			},
			expectError: true,
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
				Selector:   "a1b2c3d4",
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

func newSelectorTestQueries() *fakeSelectorResolutionQueries {
	return &fakeSelectorResolutionQueries{
		tenant: sqlc.Tenant{ID: 3, Key: "tenant-a"},
		repo: sqlc.Repo{
			ID:                44,
			TenantID:          3,
			PathWithNamespace: "acme/platform-api",
			DefaultBranch:     "main",
		},
	}
}

func newSelectorTestInput(kind SelectorKind, selector string) normalizedResolveReadSelectorInput {
	return normalizedResolveReadSelectorInput{
		tenantKey: "tenant-a",
		repoPath:  "acme/platform-api",
		kind:      kind,
		selector:  selector,
	}
}

type fakeSelectorResolutionQueries struct {
	tenant sqlc.Tenant
	repo   sqlc.Repo

	bySHAPrefix     map[string]sqlc.Revision
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

func (f *fakeSelectorResolutionQueries) GetRevisionByRepoSHAPrefix(
	_ context.Context,
	arg sqlc.GetRevisionByRepoSHAPrefixParams,
) (sqlc.Revision, error) {
	if f.bySHAPrefix == nil {
		return sqlc.Revision{}, pgx.ErrNoRows
	}
	revision, ok := f.bySHAPrefix[arg.ShaPrefix.String]
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
