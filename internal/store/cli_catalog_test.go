package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestListRepoCatalogInventory_MapsHeadAndSnapshotMetadata(t *testing.T) {
	t.Parallel()

	receivedAt := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)
	processedAt := receivedAt.Add(2 * time.Minute)

	queries := &fakeCLICatalogQueries{
		repoInventoryRows: []sqlc.ListRepoCatalogInventoryRow{
			{
				ID:                          44,
				GitlabProjectID:             444,
				Namespace:                   "acme",
				Repo:                        "platform-api",
				DefaultBranch:               "main",
				OpenapiForceRescan:          true,
				ActiveApiCount:              3,
				HeadPresent:                 true,
				HeadRevisionID:              500,
				HeadRevisionSha:             "head-sha",
				HeadRevisionStatus:          "processed",
				HeadRevisionOpenapiChanged:  pgBool(false),
				HeadRevisionReceivedAt:      pgTime(receivedAt),
				HeadRevisionProcessedAt:     pgTime(processedAt),
				SnapshotPresent:             true,
				SnapshotRevisionID:          490,
				SnapshotRevisionSha:         "snapshot-sha",
				SnapshotRevisionProcessedAt: pgTime(processedAt.Add(-time.Minute)),
			},
		},
	}

	items, err := listRepoCatalogInventory(context.Background(), queries)
	if err != nil {
		t.Fatalf("listRepoCatalogInventory() unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one repo catalog item, got %d", len(items))
	}

	item := items[0]
	if item.Repo.Namespace != "acme" || item.Repo.Repo != "platform-api" {
		t.Fatalf("unexpected repo identity %+v", item.Repo)
	}
	if item.HeadRevision == nil || item.HeadRevision.ID != 500 {
		t.Fatalf("expected head revision id=500, got %+v", item.HeadRevision)
	}
	if item.SnapshotRevision == nil || item.SnapshotRevision.ID != 490 {
		t.Fatalf("expected snapshot revision id=490, got %+v", item.SnapshotRevision)
	}
	if item.HeadRevision.OpenAPIChanged == nil || *item.HeadRevision.OpenAPIChanged {
		t.Fatalf("expected head openapi_changed=false, got %+v", item.HeadRevision.OpenAPIChanged)
	}
	if item.ActiveAPICount != 3 || !item.OpenAPIForceRescan {
		t.Fatalf("unexpected repo catalog metadata: %+v", item)
	}
}

func TestGetRepoCatalogFreshness_NotFound(t *testing.T) {
	t.Parallel()

	queries := &fakeCLICatalogQueries{freshnessErr: pgx.ErrNoRows}

	_, err := getRepoCatalogFreshness(context.Background(), queries, "missing", "repo")
	if err == nil {
		t.Fatalf("expected not found error")
	}
	if !errors.Is(err, ErrRepoNotFound) {
		t.Fatalf("expected ErrRepoNotFound, got %v", err)
	}
}

func TestResolveSpecSnapshots_FiltersToResolvedSpecCandidates(t *testing.T) {
	t.Parallel()

	queries := newFakeCLICatalogQueries()
	queries.byID = map[int64]sqlc.IngestEvent{
		71: newReadSnapshotTestRevision(71, 44, "processed", boolPtrReadSnapshot(false), "main", "sha"),
	}
	queries.apiInventoryRows = []sqlc.ListAPISnapshotInventoryByRepoRevisionRow{
		{
			ApiSpecID:         1,
			Api:               "apis/one.yaml",
			Status:            "active",
			DisplayName:       pgText("One"),
			ApiSpecRevisionID: pgInt8(101),
			IngestEventID:     pgInt8(71),
			IngestEventSha:    pgText("sha"),
			IngestEventBranch: pgText("main"),
			SpecEtag:          pgText("\"one\""),
			SpecSizeBytes:     pgInt8(123),
			OperationCount:    4,
		},
		{
			ApiSpecID:      2,
			Api:            "apis/two.yaml",
			Status:         "active",
			DisplayName:    pgText("Two"),
			OperationCount: 0,
		},
	}

	resolved, err := resolveSpecSnapshots(context.Background(), queries, normalizedResolveReadSnapshotInput{
			namespace:  "acme",
			repo:       "platform-api",
			repoPath:   "acme/platform-api",
			revisionID: 71,
		kind:       ReadSnapshotSelectorRevisionID,
	})
	if err != nil {
		t.Fatalf("resolveSpecSnapshots() unexpected error: %v", err)
	}

	if len(resolved.Candidates) != 1 {
		t.Fatalf("expected one resolved spec candidate, got %d", len(resolved.Candidates))
	}
	if resolved.Candidates[0].API != "apis/one.yaml" {
		t.Fatalf("unexpected candidate api %q", resolved.Candidates[0].API)
	}
}

func TestResolveOperationCandidatesByOperationID_PreservesRepoSnapshotAmbiguity(t *testing.T) {
	t.Parallel()

	queries := newFakeCLICatalogQueries()
	queries.byID = map[int64]sqlc.IngestEvent{
		81: newReadSnapshotTestRevision(81, 44, "processed", boolPtrReadSnapshot(true), "main", "sha"),
	}
	queries.operationRowsByID = []sqlc.FindOperationCandidatesByRepoRevisionAndOperationIDRow{
		{
			ApiSpecID:         1,
			Api:               "apis/one.yaml",
			Status:            "active",
			ApiSpecRevisionID: 101,
			IngestEventID:     81,
			IngestEventSha:    "sha",
			IngestEventBranch: "main",
			Method:            "get",
			Path:              "/users",
			OperationID:       pgText("getUsers"),
			Summary:           pgText("Get users"),
			RawJson:           []byte(`{"operationId":"getUsers"}`),
		},
		{
			ApiSpecID:         2,
			Api:               "apis/two.yaml",
			Status:            "active",
			ApiSpecRevisionID: 102,
			IngestEventID:     81,
			IngestEventSha:    "sha",
			IngestEventBranch: "main",
			Method:            "get",
			Path:              "/users",
			OperationID:       pgText("getUsers"),
			Summary:           pgText("Get users v2"),
			RawJson:           []byte(`{"operationId":"getUsers","x-version":"2"}`),
		},
	}

	resolved, err := resolveOperationCandidatesByOperationID(
		context.Background(),
		queries,
		normalizedResolveReadSnapshotInput{
			namespace:  "acme",
			repo:       "platform-api",
			repoPath:   "acme/platform-api",
			revisionID: 81,
			kind:       ReadSnapshotSelectorRevisionID,
		},
		"getUsers",
	)
	if err != nil {
		t.Fatalf("resolveOperationCandidatesByOperationID() unexpected error: %v", err)
	}

	if len(resolved.Candidates) != 2 {
		t.Fatalf("expected two candidates for ambiguity, got %d", len(resolved.Candidates))
	}
	if resolved.Candidates[0].API != "apis/one.yaml" || resolved.Candidates[1].API != "apis/two.yaml" {
		t.Fatalf("unexpected candidate ordering: %+v", resolved.Candidates)
	}
}

func TestResolveOperationCandidatesByMethodPath_ScopesToExplicitAPI(t *testing.T) {
	t.Parallel()

	queries := newFakeCLICatalogQueries()
	queries.bySHAPrefix = map[string]sqlc.IngestEvent{
		"deadbeef": newReadSnapshotTestRevision(91, 44, "processed", boolPtrReadSnapshot(true), "main", "deadbeef0000"),
	}
	queries.operationRowsByAPIMethodPath = []sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndMethodPathRow{
		{
			ApiSpecID:         7,
			Api:               "apis/users.yaml",
			Status:            "active",
			ApiSpecRevisionID: 701,
			IngestEventID:     91,
			IngestEventSha:    "deadbeef0000",
			IngestEventBranch: "main",
			Method:            "get",
			Path:              "/users/{id}",
			OperationID:       pgText("getUser"),
			Summary:           pgText("Get user"),
			RawJson:           []byte(`{"operationId":"getUser"}`),
		},
	}

	resolved, err := resolveOperationCandidatesByMethodPath(
		context.Background(),
		queries,
		normalizedResolveReadSnapshotInput{
			namespace: "acme",
			repo:      "platform-api",
			repoPath:  "acme/platform-api",
			apiPath:  "apis/users.yaml",
			sha:      "deadbeef",
			kind:     ReadSnapshotSelectorSHA,
		},
		"get",
		"/users/{id}",
	)
	if err != nil {
		t.Fatalf("resolveOperationCandidatesByMethodPath() unexpected error: %v", err)
	}

	if len(resolved.Candidates) != 1 {
		t.Fatalf("expected one api-scoped candidate, got %d", len(resolved.Candidates))
	}
	if resolved.Candidates[0].API != "apis/users.yaml" {
		t.Fatalf("unexpected candidate api %q", resolved.Candidates[0].API)
	}
}

type fakeCLICatalogQueries struct {
	*fakeReadSnapshotQueries

	repoInventoryRows []sqlc.ListRepoCatalogInventoryRow
	freshnessRow      sqlc.GetRepoCatalogFreshnessRow
	freshnessErr      error

	apiInventoryRows []sqlc.ListAPISnapshotInventoryByRepoRevisionRow
	apiSelectionRow  sqlc.GetAPISnapshotByRepoRevisionAndAPIRow
	apiSelectionErr  error

	operationInventoryRows      []sqlc.ListOperationInventoryByRepoRevisionRow
	operationInventoryByAPIRows []sqlc.ListOperationInventoryByRepoRevisionAndAPIRow

	operationRowsByID            []sqlc.FindOperationCandidatesByRepoRevisionAndOperationIDRow
	operationRowsByAPIID         []sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndOperationIDRow
	operationRowsByMethodPath    []sqlc.FindOperationCandidatesByRepoRevisionAndMethodPathRow
	operationRowsByAPIMethodPath []sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndMethodPathRow
}

func newFakeCLICatalogQueries() *fakeCLICatalogQueries {
	return &fakeCLICatalogQueries{
		fakeReadSnapshotQueries: newReadSnapshotTestQueries(),
	}
}

func (f *fakeCLICatalogQueries) ListRepoCatalogInventory(_ context.Context) ([]sqlc.ListRepoCatalogInventoryRow, error) {
	return f.repoInventoryRows, nil
}

func (f *fakeCLICatalogQueries) GetRepoCatalogFreshness(_ context.Context, _ sqlc.GetRepoCatalogFreshnessParams) (sqlc.GetRepoCatalogFreshnessRow, error) {
	if f.freshnessErr != nil {
		return sqlc.GetRepoCatalogFreshnessRow{}, f.freshnessErr
	}
	return f.freshnessRow, nil
}

func (f *fakeCLICatalogQueries) ListAPISnapshotInventoryByRepoRevision(
	_ context.Context,
	_ sqlc.ListAPISnapshotInventoryByRepoRevisionParams,
) ([]sqlc.ListAPISnapshotInventoryByRepoRevisionRow, error) {
	return f.apiInventoryRows, nil
}

func (f *fakeCLICatalogQueries) GetAPISnapshotByRepoRevisionAndAPI(
	_ context.Context,
	_ sqlc.GetAPISnapshotByRepoRevisionAndAPIParams,
) (sqlc.GetAPISnapshotByRepoRevisionAndAPIRow, error) {
	if f.apiSelectionErr != nil {
		return sqlc.GetAPISnapshotByRepoRevisionAndAPIRow{}, f.apiSelectionErr
	}
	return f.apiSelectionRow, nil
}

func (f *fakeCLICatalogQueries) ListOperationInventoryByRepoRevision(
	_ context.Context,
	_ sqlc.ListOperationInventoryByRepoRevisionParams,
) ([]sqlc.ListOperationInventoryByRepoRevisionRow, error) {
	return f.operationInventoryRows, nil
}

func (f *fakeCLICatalogQueries) ListOperationInventoryByRepoRevisionAndAPI(
	_ context.Context,
	_ sqlc.ListOperationInventoryByRepoRevisionAndAPIParams,
) ([]sqlc.ListOperationInventoryByRepoRevisionAndAPIRow, error) {
	return f.operationInventoryByAPIRows, nil
}

func (f *fakeCLICatalogQueries) FindOperationCandidatesByRepoRevisionAndOperationID(
	_ context.Context,
	_ sqlc.FindOperationCandidatesByRepoRevisionAndOperationIDParams,
) ([]sqlc.FindOperationCandidatesByRepoRevisionAndOperationIDRow, error) {
	return f.operationRowsByID, nil
}

func (f *fakeCLICatalogQueries) FindOperationCandidatesByRepoRevisionAndAPIAndOperationID(
	_ context.Context,
	_ sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndOperationIDParams,
) ([]sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndOperationIDRow, error) {
	return f.operationRowsByAPIID, nil
}

func (f *fakeCLICatalogQueries) FindOperationCandidatesByRepoRevisionAndMethodPath(
	_ context.Context,
	_ sqlc.FindOperationCandidatesByRepoRevisionAndMethodPathParams,
) ([]sqlc.FindOperationCandidatesByRepoRevisionAndMethodPathRow, error) {
	return f.operationRowsByMethodPath, nil
}

func (f *fakeCLICatalogQueries) FindOperationCandidatesByRepoRevisionAndAPIAndMethodPath(
	_ context.Context,
	_ sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndMethodPathParams,
) ([]sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndMethodPathRow, error) {
	return f.operationRowsByAPIMethodPath, nil
}

func pgText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func pgInt8(value int64) pgtype.Int8 {
	return pgtype.Int8{Int64: value, Valid: true}
}

func pgBool(value bool) pgtype.Bool {
	return pgtype.Bool{Bool: value, Valid: true}
}

func pgTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
