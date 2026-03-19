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
	queries.latestByBranch = map[string]sqlc.GetLatestRevisionStateByBranchRow{
		"release": newSelectorTestLatestBranchRevision(500, "processed", boolPtr(false), "release", "release-head"),
	}
	queries.openAPIByBranch = map[string]sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDRow{
		"release": newSelectorTestLatestProcessedOpenAPIRevision(490, "processed", boolPtr(true), "release", "release-artifact"),
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
	if queries.lastHeadBranch != "release" {
		t.Fatalf("expected no-selector to resolve using branch %q, got %q", "release", queries.lastHeadBranch)
	}
}

func TestResolveReadSelector_NoSelectorHeadUnprocessedReturnsConflict(t *testing.T) {
	t.Parallel()

	queries := newSelectorTestQueries()
	queries.latestByBranch = map[string]sqlc.GetLatestRevisionStateByBranchRow{
		"main": newSelectorTestLatestBranchRevision(501, "pending", nil, "main", "main-head"),
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
	if selectorErr.IngestEventID != 501 || selectorErr.IngestEventStatus != "pending" {
		t.Fatalf("unexpected selector error metadata: %+v", selectorErr)
	}
}

func TestResolveReadSelector_SHAProcessedWithoutOpenAPIArtifactReturnsNotFound(t *testing.T) {
	t.Parallel()

	queries := newSelectorTestQueries()
	queries.bySHAPrefix = map[string]sqlc.GetRevisionStateByRepoSHAPrefixRow{
		"11111111": newSelectorTestSHAPrefixRevision(
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
			name: "repo path is required",
			input: ResolveReadSelectorInput{
				Repo:     "   ",
				Selector: "a1b2c3d4",
			},
			expectError: true,
		},
		{
			name: "short selector normalized to lower-case",
			input: ResolveReadSelectorInput{
				Namespace: "acme", Repo: "platform-api",
				Selector: "a1b2c3d4",
			},
			kind:     SelectorKindSHA,
			selector: "a1b2c3d4",
		},
		{
			name: "invalid short SHA length",
			input: ResolveReadSelectorInput{
				Namespace: "acme", Repo: "platform-api",
				Selector: "1234567",
			},
			expectError: true,
		},
		{
			name: "invalid short SHA charset",
			input: ResolveReadSelectorInput{
				Namespace: "acme", Repo: "platform-api",
				Selector: "g1b2c3d4",
			},
			expectError: true,
		},
		{
			name: "upper-case short SHA rejected",
			input: ResolveReadSelectorInput{
				Namespace: "acme", Repo: "platform-api",
				Selector: "A1B2C3D4",
			},
			expectError: true,
		},
		{
			name: "no selector mode",
			input: ResolveReadSelectorInput{
				Namespace:  "acme",
				Repo:       "platform-api",
				NoSelector: true,
			},
			kind: SelectorKindNoSelector,
		},
		{
			name: "no selector mode rejects explicit selector",
			input: ResolveReadSelectorInput{
				Namespace:  "acme",
				Repo:       "platform-api",
				Selector:   "a1b2c3d4",
				NoSelector: true,
			},
			expectError: true,
		},
		{
			name: "selector required when no-selector is false",
			input: ResolveReadSelectorInput{
				Namespace: "acme", Repo: "platform-api",
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
		repo: sqlc.GetRepoByNamespaceAndRepoRow{
			ID:        44,
			Namespace: "acme", Repo: "platform-api",
			DefaultBranch: "main",
		},
	}
}

func newSelectorTestInput(kind SelectorKind, selector string) normalizedResolveReadSelectorInput {
	return normalizedResolveReadSelectorInput{
		namespace: "acme",
		repo:      "platform-api",
		repoPath:  "acme/platform-api",
		kind:      kind,
		selector:  selector,
	}
}

type fakeSelectorResolutionQueries struct {
	repo sqlc.GetRepoByNamespaceAndRepoRow

	bySHAPrefix     map[string]sqlc.GetRevisionStateByRepoSHAPrefixRow
	latestByBranch  map[string]sqlc.GetLatestRevisionStateByBranchRow
	openAPIByBranch map[string]sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDRow

	lastHeadBranch string
}

func (f *fakeSelectorResolutionQueries) GetRepoByNamespaceAndRepo(
	_ context.Context,
	arg sqlc.GetRepoByNamespaceAndRepoParams,
) (sqlc.GetRepoByNamespaceAndRepoRow, error) {
	if f.repo.ID == 0 || f.repo.Namespace != arg.Namespace || f.repo.Repo != arg.Repo {
		return sqlc.GetRepoByNamespaceAndRepoRow{}, pgx.ErrNoRows
	}
	return f.repo, nil
}

func (f *fakeSelectorResolutionQueries) GetRevisionStateByRepoSHAPrefix(
	_ context.Context,
	arg sqlc.GetRevisionStateByRepoSHAPrefixParams,
) (sqlc.GetRevisionStateByRepoSHAPrefixRow, error) {
	if f.bySHAPrefix == nil {
		return sqlc.GetRevisionStateByRepoSHAPrefixRow{}, pgx.ErrNoRows
	}
	revision, ok := f.bySHAPrefix[arg.ShaPrefix.String]
	if !ok {
		return sqlc.GetRevisionStateByRepoSHAPrefixRow{}, pgx.ErrNoRows
	}
	return revision, nil
}

func (f *fakeSelectorResolutionQueries) GetLatestRevisionStateByBranch(
	_ context.Context,
	arg sqlc.GetLatestRevisionStateByBranchParams,
) (sqlc.GetLatestRevisionStateByBranchRow, error) {
	f.lastHeadBranch = arg.Branch
	if f.latestByBranch == nil {
		return sqlc.GetLatestRevisionStateByBranchRow{}, pgx.ErrNoRows
	}
	revision, ok := f.latestByBranch[arg.Branch]
	if !ok {
		return sqlc.GetLatestRevisionStateByBranchRow{}, pgx.ErrNoRows
	}
	return revision, nil
}

func (f *fakeSelectorResolutionQueries) GetLatestProcessedOpenAPIRevisionStateByBranchExcludingID(
	_ context.Context,
	arg sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDParams,
) (sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDRow, error) {
	if f.openAPIByBranch == nil {
		return sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDRow{}, pgx.ErrNoRows
	}
	revision, ok := f.openAPIByBranch[arg.Branch]
	if !ok {
		return sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDRow{}, pgx.ErrNoRows
	}
	return revision, nil
}

func newSelectorTestSHAPrefixRevision(
	id int64,
	status string,
	openAPIChanged *bool,
	branch string,
	sha string,
) sqlc.GetRevisionStateByRepoSHAPrefixRow {
	revision := sqlc.GetRevisionStateByRepoSHAPrefixRow{
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

func newSelectorTestLatestBranchRevision(
	id int64,
	status string,
	openAPIChanged *bool,
	branch string,
	sha string,
) sqlc.GetLatestRevisionStateByBranchRow {
	revision := sqlc.GetLatestRevisionStateByBranchRow{
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

func newSelectorTestLatestProcessedOpenAPIRevision(
	id int64,
	status string,
	openAPIChanged *bool,
	branch string,
	sha string,
) sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDRow {
	revision := sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDRow{
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
