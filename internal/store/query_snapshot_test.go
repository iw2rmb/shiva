package store

import (
	"context"
	"errors"
	"testing"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestResolveReadSnapshot_DefaultBranchLatestUsesLatestProcessedOpenAPISnapshot(t *testing.T) {
	t.Parallel()

	queries := newReadSnapshotTestQueries()
	queries.repo.DefaultBranch = "release"
	queries.latestByBranch = map[string]sqlc.IngestEvent{
		"release": newReadSnapshotTestRevision(500, 44, "processed", boolPtrReadSnapshot(false), "release", "release-head"),
	}
	queries.openAPIByBranch = map[string]sqlc.IngestEvent{
		"release": newReadSnapshotTestRevision(490, 44, "processed", boolPtrReadSnapshot(true), "release", "release-openapi"),
	}

	resolved, err := resolveReadSnapshot(context.Background(), queries, normalizedResolveReadSnapshotInput{
		namespace: "acme",
		repo:      "platform-api",
		repoPath:  "acme/platform-api",
		kind:      ReadSnapshotSelectorDefaultBranchLatest,
	})
	if err != nil {
		t.Fatalf("resolveReadSnapshot() unexpected error: %v", err)
	}

	if resolved.Revision.ID != 490 {
		t.Fatalf("expected latest processed openapi revision id=490, got %d", resolved.Revision.ID)
	}
	if resolved.SelectorKind != ReadSnapshotSelectorDefaultBranchLatest {
		t.Fatalf("expected selector kind %q, got %q", ReadSnapshotSelectorDefaultBranchLatest, resolved.SelectorKind)
	}
	if queries.lastHeadBranch != "release" {
		t.Fatalf("expected default-branch lookup on %q, got %q", "release", queries.lastHeadBranch)
	}
}

func TestResolveReadSnapshot_SHAAllowsProcessedSnapshotWithoutOpenAPIChange(t *testing.T) {
	t.Parallel()

	queries := newReadSnapshotTestQueries()
	queries.bySHAPrefix = map[string]sqlc.IngestEvent{
		"11111111": newReadSnapshotTestRevision(700, 44, "processed", boolPtrReadSnapshot(false), "main", "1111111111111111111111111111111111111111"),
	}

	resolved, err := resolveReadSnapshot(context.Background(), queries, normalizedResolveReadSnapshotInput{
		namespace: "acme",
		repo:      "platform-api",
		repoPath:  "acme/platform-api",
		sha:       "11111111",
		kind:      ReadSnapshotSelectorSHA,
	})
	if err != nil {
		t.Fatalf("resolveReadSnapshot() unexpected error: %v", err)
	}

	if resolved.Revision.ID != 700 {
		t.Fatalf("expected revision id=700, got %d", resolved.Revision.ID)
	}
	if resolved.Revision.OpenAPIChanged == nil || *resolved.Revision.OpenAPIChanged {
		t.Fatalf("expected openapi_changed=false, got %+v", resolved.Revision.OpenAPIChanged)
	}
}

func TestResolveReadSnapshot_RevisionIDRequiresMatchingRepoAndProcessedState(t *testing.T) {
	t.Parallel()

	t.Run("repo mismatch returns not found", func(t *testing.T) {
		t.Parallel()

		queries := newReadSnapshotTestQueries()
		queries.byID = map[int64]sqlc.IngestEvent{
			91: newReadSnapshotTestRevision(91, 999, "processed", boolPtrReadSnapshot(true), "main", "abc"),
		}

		_, err := resolveReadSnapshot(context.Background(), queries, normalizedResolveReadSnapshotInput{
			namespace:  "acme",
			repo:       "platform-api",
			repoPath:   "acme/platform-api",
			revisionID: 91,
			kind:       ReadSnapshotSelectorRevisionID,
		})
		if err == nil {
			t.Fatalf("expected not found error")
		}
		if !errors.Is(err, ErrReadSnapshotNotFound) {
			t.Fatalf("expected ErrReadSnapshotNotFound, got %v", err)
		}
	})

	t.Run("unprocessed revision returns conflict", func(t *testing.T) {
		t.Parallel()

		queries := newReadSnapshotTestQueries()
		queries.byID = map[int64]sqlc.IngestEvent{
			92: newReadSnapshotTestRevision(92, 44, "processing", nil, "main", "def"),
		}

		_, err := resolveReadSnapshot(context.Background(), queries, normalizedResolveReadSnapshotInput{
			namespace:  "acme",
			repo:       "platform-api",
			repoPath:   "acme/platform-api",
			revisionID: 92,
			kind:       ReadSnapshotSelectorRevisionID,
		})
		if err == nil {
			t.Fatalf("expected unprocessed error")
		}
		if !errors.Is(err, ErrReadSnapshotUnprocessed) {
			t.Fatalf("expected ErrReadSnapshotUnprocessed, got %v", err)
		}
	})
}

func TestNormalizeResolveReadSnapshotInput(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		input       ResolveReadSnapshotInput
		kind        ReadSnapshotSelectorKind
		expectError bool
	}{
		{
			name: "default branch latest when sha and revision are absent",
			input: ResolveReadSnapshotInput{
				Namespace: "acme", Repo: "platform-api",
			},
			kind: ReadSnapshotSelectorDefaultBranchLatest,
		},
		{
			name: "sha selector",
			input: ResolveReadSnapshotInput{
				Namespace: "acme", Repo: "platform-api",
				SHA:      "deadbeef",
			},
			kind: ReadSnapshotSelectorSHA,
		},
		{
			name: "revision selector",
			input: ResolveReadSnapshotInput{
				Namespace:  "acme",
				Repo:       "platform-api",
				RevisionID: 17,
			},
			kind: ReadSnapshotSelectorRevisionID,
		},
		{
			name: "sha and revision are mutually exclusive",
			input: ResolveReadSnapshotInput{
				Namespace:  "acme",
				Repo:       "platform-api",
				RevisionID: 17,
				SHA:        "deadbeef",
			},
			expectError: true,
		},
		{
			name: "sha must be lowercase short hex",
			input: ResolveReadSnapshotInput{
				Namespace: "acme", Repo: "platform-api",
				SHA:      "DEADBEEF",
			},
			expectError: true,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			normalized, err := normalizeResolveReadSnapshotInput(testCase.input)
			if testCase.expectError {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeResolveReadSnapshotInput() unexpected error: %v", err)
			}
			if normalized.kind != testCase.kind {
				t.Fatalf("expected kind %q, got %q", testCase.kind, normalized.kind)
			}
		})
	}
}

func newReadSnapshotTestQueries() *fakeReadSnapshotQueries {
	return &fakeReadSnapshotQueries{
		repo: sqlc.Repo{
			ID:                44,
			GitlabProjectID:   444,
			Namespace: "acme", Repo: "platform-api",
			DefaultBranch:     "main",
		},
	}
}

type fakeReadSnapshotQueries struct {
	repo sqlc.Repo

	byID            map[int64]sqlc.IngestEvent
	bySHAPrefix     map[string]sqlc.IngestEvent
	latestByBranch  map[string]sqlc.IngestEvent
	openAPIByBranch map[string]sqlc.IngestEvent

	lastHeadBranch string
}

func (f *fakeReadSnapshotQueries) GetRepoByNamespaceAndRepo(_ context.Context, arg sqlc.GetRepoByNamespaceAndRepoParams) (sqlc.Repo, error) {
	if f.repo.ID == 0 || f.repo.Namespace != arg.Namespace || f.repo.Repo != arg.Repo {
		return sqlc.Repo{}, pgx.ErrNoRows
	}
	return f.repo, nil
}

func (f *fakeReadSnapshotQueries) GetRevisionByID(_ context.Context, id int64) (sqlc.IngestEvent, error) {
	if f.byID == nil {
		return sqlc.IngestEvent{}, pgx.ErrNoRows
	}
	revision, ok := f.byID[id]
	if !ok {
		return sqlc.IngestEvent{}, pgx.ErrNoRows
	}
	return revision, nil
}

func (f *fakeReadSnapshotQueries) GetRevisionByRepoSHAPrefix(
	_ context.Context,
	arg sqlc.GetRevisionByRepoSHAPrefixParams,
) (sqlc.IngestEvent, error) {
	if f.bySHAPrefix == nil {
		return sqlc.IngestEvent{}, pgx.ErrNoRows
	}
	revision, ok := f.bySHAPrefix[arg.ShaPrefix.String]
	if !ok {
		return sqlc.IngestEvent{}, pgx.ErrNoRows
	}
	return revision, nil
}

func (f *fakeReadSnapshotQueries) GetLatestRevisionByBranch(
	_ context.Context,
	arg sqlc.GetLatestRevisionByBranchParams,
) (sqlc.IngestEvent, error) {
	f.lastHeadBranch = arg.Branch
	if f.latestByBranch == nil {
		return sqlc.IngestEvent{}, pgx.ErrNoRows
	}
	revision, ok := f.latestByBranch[arg.Branch]
	if !ok {
		return sqlc.IngestEvent{}, pgx.ErrNoRows
	}
	return revision, nil
}

func (f *fakeReadSnapshotQueries) GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
	_ context.Context,
	arg sqlc.GetLatestProcessedOpenAPIRevisionByBranchExcludingIDParams,
) (sqlc.IngestEvent, error) {
	if f.openAPIByBranch == nil {
		return sqlc.IngestEvent{}, pgx.ErrNoRows
	}
	revision, ok := f.openAPIByBranch[arg.Branch]
	if !ok {
		return sqlc.IngestEvent{}, pgx.ErrNoRows
	}
	return revision, nil
}

func newReadSnapshotTestRevision(
	id int64,
	repoID int64,
	status string,
	openAPIChanged *bool,
	branch string,
	sha string,
) sqlc.IngestEvent {
	revision := sqlc.IngestEvent{
		ID:     id,
		RepoID: repoID,
		Status: status,
		Branch: branch,
		Sha:    sha,
	}
	if openAPIChanged != nil {
		revision.OpenapiChanged = pgtype.Bool{Bool: *openAPIChanged, Valid: true}
	}
	return revision
}

func boolPtrReadSnapshot(value bool) *bool {
	return &value
}
