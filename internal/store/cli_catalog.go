package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type CatalogRevisionState struct {
	ID             int64
	SHA            string
	Status         string
	OpenAPIChanged *bool
	ReceivedAt     *time.Time
	ProcessedAt    *time.Time
}

type RepoCatalogEntry struct {
	Repo Repo

	OpenAPIForceRescan bool
	ActiveAPICount     int64
	HeadRevision       *CatalogRevisionState
	SnapshotRevision   *CatalogRevisionState
}

type RepoCatalogFreshness = RepoCatalogEntry

type APISnapshot struct {
	APISpecID int64
	API       string
	Status    string

	DisplayName string

	HasSnapshot       bool
	APISpecRevisionID int64
	IngestEventID     int64
	IngestEventSHA    string
	IngestEventBranch string
	SpecETag          string
	SpecSizeBytes     int64
	OperationCount    int64
}

type OperationSnapshot struct {
	APISpecID         int64
	API               string
	Status            string
	APISpecRevisionID int64
	IngestEventID     int64
	IngestEventSHA    string
	IngestEventBranch string
	Method            string
	Path              string
	OperationID       string
	Summary           string
	Deprecated        bool
	RawJSON           []byte
}

type ResolvedSpecSnapshots struct {
	Snapshot   ResolvedReadSnapshot
	Candidates []APISnapshot
}

type ResolveOperationByIDInput struct {
	ResolveReadSnapshotInput
	OperationID string
}

type ResolveOperationByMethodPathInput struct {
	ResolveReadSnapshotInput
	Method string
	Path   string
}

type ResolvedOperationCandidates struct {
	Snapshot   ResolvedReadSnapshot
	Candidates []OperationSnapshot
}

type repoCatalogInventoryQueries interface {
	ListRepoCatalogInventory(ctx context.Context) ([]sqlc.ListRepoCatalogInventoryRow, error)
}

type repoCatalogFreshnessQueries interface {
	GetRepoCatalogFreshnessByPath(ctx context.Context, pathWithNamespace string) (sqlc.GetRepoCatalogFreshnessByPathRow, error)
}

type apiSnapshotInventoryQueries interface {
	ListAPISnapshotInventoryByRepoRevision(
		ctx context.Context,
		arg sqlc.ListAPISnapshotInventoryByRepoRevisionParams,
	) ([]sqlc.ListAPISnapshotInventoryByRepoRevisionRow, error)
	GetAPISnapshotByRepoRevisionAndAPI(
		ctx context.Context,
		arg sqlc.GetAPISnapshotByRepoRevisionAndAPIParams,
	) (sqlc.GetAPISnapshotByRepoRevisionAndAPIRow, error)
}

type operationInventoryQueries interface {
	ListOperationInventoryByRepoRevision(
		ctx context.Context,
		arg sqlc.ListOperationInventoryByRepoRevisionParams,
	) ([]sqlc.ListOperationInventoryByRepoRevisionRow, error)
	ListOperationInventoryByRepoRevisionAndAPI(
		ctx context.Context,
		arg sqlc.ListOperationInventoryByRepoRevisionAndAPIParams,
	) ([]sqlc.ListOperationInventoryByRepoRevisionAndAPIRow, error)
}

type operationLookupByIDQueries interface {
	FindOperationCandidatesByRepoRevisionAndOperationID(
		ctx context.Context,
		arg sqlc.FindOperationCandidatesByRepoRevisionAndOperationIDParams,
	) ([]sqlc.FindOperationCandidatesByRepoRevisionAndOperationIDRow, error)
	FindOperationCandidatesByRepoRevisionAndAPIAndOperationID(
		ctx context.Context,
		arg sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndOperationIDParams,
	) ([]sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndOperationIDRow, error)
}

type operationLookupByMethodPathQueries interface {
	FindOperationCandidatesByRepoRevisionAndMethodPath(
		ctx context.Context,
		arg sqlc.FindOperationCandidatesByRepoRevisionAndMethodPathParams,
	) ([]sqlc.FindOperationCandidatesByRepoRevisionAndMethodPathRow, error)
	FindOperationCandidatesByRepoRevisionAndAPIAndMethodPath(
		ctx context.Context,
		arg sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndMethodPathParams,
	) ([]sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndMethodPathRow, error)
}

type specResolutionQueries interface {
	readSnapshotQueries
	apiSnapshotInventoryQueries
}

type operationResolutionByIDQueries interface {
	readSnapshotQueries
	operationLookupByIDQueries
}

type operationResolutionByMethodPathQueries interface {
	readSnapshotQueries
	operationLookupByMethodPathQueries
}

func (s *Store) ListRepoCatalogInventory(ctx context.Context) ([]RepoCatalogEntry, error) {
	if s == nil || !s.configured || s.pool == nil {
		return nil, ErrStoreNotConfigured
	}

	return listRepoCatalogInventory(ctx, sqlc.New(s.pool))
}

func listRepoCatalogInventory(ctx context.Context, queries repoCatalogInventoryQueries) ([]RepoCatalogEntry, error) {
	rows, err := queries.ListRepoCatalogInventory(ctx)
	if err != nil {
		return nil, fmt.Errorf("list repo catalog inventory: %w", err)
	}

	items := make([]RepoCatalogEntry, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapRepoCatalogInventoryRow(row))
	}

	return items, nil
}

func (s *Store) GetRepoCatalogFreshnessByPath(ctx context.Context, repoPath string) (RepoCatalogFreshness, error) {
	if s == nil || !s.configured || s.pool == nil {
		return RepoCatalogFreshness{}, ErrStoreNotConfigured
	}

	repoPath = strings.TrimSpace(repoPath)
	if repoPath == "" {
		return RepoCatalogFreshness{}, errors.New("repo path must not be empty")
	}

	return getRepoCatalogFreshnessByPath(ctx, sqlc.New(s.pool), repoPath)
}

func getRepoCatalogFreshnessByPath(
	ctx context.Context,
	queries repoCatalogFreshnessQueries,
	repoPath string,
) (RepoCatalogFreshness, error) {
	row, err := queries.GetRepoCatalogFreshnessByPath(ctx, repoPath)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RepoCatalogFreshness{}, fmt.Errorf("%w: path=%s", ErrRepoNotFound, repoPath)
		}
		return RepoCatalogFreshness{}, fmt.Errorf("get repo catalog freshness for %q: %w", repoPath, err)
	}

	return mapRepoCatalogFreshnessRow(row), nil
}

func (s *Store) ListAPISnapshotInventoryByRepoRevision(
	ctx context.Context,
	repoID int64,
	snapshotRevisionID int64,
) ([]APISnapshot, error) {
	if s == nil || !s.configured || s.pool == nil {
		return nil, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return nil, errors.New("repo id must be positive")
	}
	if snapshotRevisionID < 1 {
		return nil, errors.New("snapshot revision id must be positive")
	}

	return listAPISnapshotInventoryByRepoRevision(ctx, sqlc.New(s.pool), repoID, snapshotRevisionID)
}

func listAPISnapshotInventoryByRepoRevision(
	ctx context.Context,
	queries apiSnapshotInventoryQueries,
	repoID int64,
	snapshotRevisionID int64,
) ([]APISnapshot, error) {
	rows, err := queries.ListAPISnapshotInventoryByRepoRevision(ctx, sqlc.ListAPISnapshotInventoryByRepoRevisionParams{
		RepoID:             repoID,
		SnapshotRevisionID: snapshotRevisionID,
	})
	if err != nil {
		return nil, fmt.Errorf("list api snapshot inventory for repo %d revision %d: %w", repoID, snapshotRevisionID, err)
	}

	items := make([]APISnapshot, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapAPISnapshotInventoryRow(row))
	}

	return items, nil
}

func (s *Store) GetAPISnapshotByRepoRevisionAndAPI(
	ctx context.Context,
	repoID int64,
	api string,
	snapshotRevisionID int64,
) (APISnapshot, bool, error) {
	if s == nil || !s.configured || s.pool == nil {
		return APISnapshot{}, false, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return APISnapshot{}, false, errors.New("repo id must be positive")
	}
	api = strings.TrimSpace(api)
	if api == "" {
		return APISnapshot{}, false, errors.New("api must not be empty")
	}
	if snapshotRevisionID < 1 {
		return APISnapshot{}, false, errors.New("snapshot revision id must be positive")
	}

	return getAPISnapshotByRepoRevisionAndAPI(ctx, sqlc.New(s.pool), repoID, api, snapshotRevisionID)
}

func getAPISnapshotByRepoRevisionAndAPI(
	ctx context.Context,
	queries apiSnapshotInventoryQueries,
	repoID int64,
	api string,
	snapshotRevisionID int64,
) (APISnapshot, bool, error) {
	row, err := queries.GetAPISnapshotByRepoRevisionAndAPI(ctx, sqlc.GetAPISnapshotByRepoRevisionAndAPIParams{
		RepoID:             repoID,
		Api:                api,
		SnapshotRevisionID: snapshotRevisionID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APISnapshot{}, false, nil
		}
		return APISnapshot{}, false, fmt.Errorf(
			"get api snapshot for repo %d api %q revision %d: %w",
			repoID,
			api,
			snapshotRevisionID,
			err,
		)
	}

	return mapAPISnapshotSelectionRow(row), true, nil
}

func (s *Store) ListOperationInventoryByRepoRevision(
	ctx context.Context,
	repoID int64,
	snapshotRevisionID int64,
) ([]OperationSnapshot, error) {
	if s == nil || !s.configured || s.pool == nil {
		return nil, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return nil, errors.New("repo id must be positive")
	}
	if snapshotRevisionID < 1 {
		return nil, errors.New("snapshot revision id must be positive")
	}

	return listOperationInventoryByRepoRevision(ctx, sqlc.New(s.pool), repoID, snapshotRevisionID)
}

func listOperationInventoryByRepoRevision(
	ctx context.Context,
	queries operationInventoryQueries,
	repoID int64,
	snapshotRevisionID int64,
) ([]OperationSnapshot, error) {
	rows, err := queries.ListOperationInventoryByRepoRevision(ctx, sqlc.ListOperationInventoryByRepoRevisionParams{
		RepoID:             repoID,
		SnapshotRevisionID: snapshotRevisionID,
	})
	if err != nil {
		return nil, fmt.Errorf("list operation inventory for repo %d revision %d: %w", repoID, snapshotRevisionID, err)
	}

	items := make([]OperationSnapshot, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapOperationInventoryRow(row))
	}

	return items, nil
}

func (s *Store) ListOperationInventoryByRepoRevisionAndAPI(
	ctx context.Context,
	repoID int64,
	api string,
	snapshotRevisionID int64,
) ([]OperationSnapshot, error) {
	if s == nil || !s.configured || s.pool == nil {
		return nil, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return nil, errors.New("repo id must be positive")
	}
	api = strings.TrimSpace(api)
	if api == "" {
		return nil, errors.New("api must not be empty")
	}
	if snapshotRevisionID < 1 {
		return nil, errors.New("snapshot revision id must be positive")
	}

	return listOperationInventoryByRepoRevisionAndAPI(ctx, sqlc.New(s.pool), repoID, api, snapshotRevisionID)
}

func listOperationInventoryByRepoRevisionAndAPI(
	ctx context.Context,
	queries operationInventoryQueries,
	repoID int64,
	api string,
	snapshotRevisionID int64,
) ([]OperationSnapshot, error) {
	rows, err := queries.ListOperationInventoryByRepoRevisionAndAPI(ctx, sqlc.ListOperationInventoryByRepoRevisionAndAPIParams{
		RepoID:             repoID,
		Api:                api,
		SnapshotRevisionID: snapshotRevisionID,
	})
	if err != nil {
		return nil, fmt.Errorf("list operation inventory for repo %d api %q revision %d: %w", repoID, api, snapshotRevisionID, err)
	}

	items := make([]OperationSnapshot, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapOperationInventoryByAPIRow(row))
	}

	return items, nil
}

func (s *Store) ResolveSpecSnapshots(ctx context.Context, input ResolveReadSnapshotInput) (ResolvedSpecSnapshots, error) {
	if s == nil || !s.configured || s.pool == nil {
		return ResolvedSpecSnapshots{}, ErrStoreNotConfigured
	}

	normalized, err := normalizeResolveReadSnapshotInput(input)
	if err != nil {
		return ResolvedSpecSnapshots{}, err
	}

	return resolveSpecSnapshots(ctx, sqlc.New(s.pool), normalized)
}

func resolveSpecSnapshots(
	ctx context.Context,
	queries specResolutionQueries,
	input normalizedResolveReadSnapshotInput,
) (ResolvedSpecSnapshots, error) {
	snapshot, err := resolveReadSnapshot(ctx, queries, input)
	if err != nil {
		return ResolvedSpecSnapshots{}, err
	}

	var candidates []APISnapshot
	if snapshot.APIPath != "" {
		candidate, found, err := getAPISnapshotByRepoRevisionAndAPI(ctx, queries, snapshot.Repo.ID, snapshot.APIPath, snapshot.Revision.ID)
		if err != nil {
			return ResolvedSpecSnapshots{}, err
		}
		if found && candidate.HasSnapshot {
			candidates = []APISnapshot{candidate}
		}
	} else {
		inventory, err := listAPISnapshotInventoryByRepoRevision(ctx, queries, snapshot.Repo.ID, snapshot.Revision.ID)
		if err != nil {
			return ResolvedSpecSnapshots{}, err
		}
		candidates = filterAPISnapshotsWithResolvedSpec(inventory)
	}

	return ResolvedSpecSnapshots{
		Snapshot:   snapshot,
		Candidates: candidates,
	}, nil
}

func (s *Store) ResolveOperationCandidatesByOperationID(
	ctx context.Context,
	input ResolveOperationByIDInput,
) (ResolvedOperationCandidates, error) {
	if s == nil || !s.configured || s.pool == nil {
		return ResolvedOperationCandidates{}, ErrStoreNotConfigured
	}

	normalizedSnapshot, err := normalizeResolveReadSnapshotInput(input.ResolveReadSnapshotInput)
	if err != nil {
		return ResolvedOperationCandidates{}, err
	}

	operationID := strings.TrimSpace(input.OperationID)
	if operationID == "" {
		return ResolvedOperationCandidates{}, errors.New("operation_id must not be empty")
	}

	return resolveOperationCandidatesByOperationID(ctx, sqlc.New(s.pool), normalizedSnapshot, operationID)
}

func resolveOperationCandidatesByOperationID(
	ctx context.Context,
	queries operationResolutionByIDQueries,
	snapshotInput normalizedResolveReadSnapshotInput,
	operationID string,
) (ResolvedOperationCandidates, error) {
	snapshot, err := resolveReadSnapshot(ctx, queries, snapshotInput)
	if err != nil {
		return ResolvedOperationCandidates{}, err
	}

	var candidates []OperationSnapshot
	if snapshot.APIPath != "" {
		rows, err := queries.FindOperationCandidatesByRepoRevisionAndAPIAndOperationID(
			ctx,
			sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndOperationIDParams{
				OperationID:        pgtype.Text{String: operationID, Valid: true},
				RepoID:             snapshot.Repo.ID,
				Api:                snapshot.APIPath,
				SnapshotRevisionID: snapshot.Revision.ID,
			},
		)
		if err != nil {
			return ResolvedOperationCandidates{}, fmt.Errorf(
				"find operation candidates for repo %d api %q revision %d operation_id %q: %w",
				snapshot.Repo.ID,
				snapshot.APIPath,
				snapshot.Revision.ID,
				operationID,
				err,
			)
		}
		candidates = make([]OperationSnapshot, 0, len(rows))
		for _, row := range rows {
			candidates = append(candidates, mapOperationCandidateByAPIRow(row))
		}
	} else {
		rows, err := queries.FindOperationCandidatesByRepoRevisionAndOperationID(
			ctx,
			sqlc.FindOperationCandidatesByRepoRevisionAndOperationIDParams{
				OperationID:        pgtype.Text{String: operationID, Valid: true},
				RepoID:             snapshot.Repo.ID,
				SnapshotRevisionID: snapshot.Revision.ID,
			},
		)
		if err != nil {
			return ResolvedOperationCandidates{}, fmt.Errorf(
				"find operation candidates for repo %d revision %d operation_id %q: %w",
				snapshot.Repo.ID,
				snapshot.Revision.ID,
				operationID,
				err,
			)
		}
		candidates = make([]OperationSnapshot, 0, len(rows))
		for _, row := range rows {
			candidates = append(candidates, mapOperationCandidateRow(row))
		}
	}

	return ResolvedOperationCandidates{
		Snapshot:   snapshot,
		Candidates: candidates,
	}, nil
}

func (s *Store) ResolveOperationCandidatesByMethodPath(
	ctx context.Context,
	input ResolveOperationByMethodPathInput,
) (ResolvedOperationCandidates, error) {
	if s == nil || !s.configured || s.pool == nil {
		return ResolvedOperationCandidates{}, ErrStoreNotConfigured
	}

	normalizedSnapshot, err := normalizeResolveReadSnapshotInput(input.ResolveReadSnapshotInput)
	if err != nil {
		return ResolvedOperationCandidates{}, err
	}

	method := strings.ToLower(strings.TrimSpace(input.Method))
	path := strings.TrimSpace(input.Path)
	if method == "" {
		return ResolvedOperationCandidates{}, errors.New("method must not be empty")
	}
	if path == "" {
		return ResolvedOperationCandidates{}, errors.New("path must not be empty")
	}

	return resolveOperationCandidatesByMethodPath(ctx, sqlc.New(s.pool), normalizedSnapshot, method, path)
}

func resolveOperationCandidatesByMethodPath(
	ctx context.Context,
	queries operationResolutionByMethodPathQueries,
	snapshotInput normalizedResolveReadSnapshotInput,
	method string,
	path string,
) (ResolvedOperationCandidates, error) {
	snapshot, err := resolveReadSnapshot(ctx, queries, snapshotInput)
	if err != nil {
		return ResolvedOperationCandidates{}, err
	}

	var candidates []OperationSnapshot
	if snapshot.APIPath != "" {
		rows, err := queries.FindOperationCandidatesByRepoRevisionAndAPIAndMethodPath(
			ctx,
			sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndMethodPathParams{
				Method:             method,
				Path:               path,
				RepoID:             snapshot.Repo.ID,
				Api:                snapshot.APIPath,
				SnapshotRevisionID: snapshot.Revision.ID,
			},
		)
		if err != nil {
			return ResolvedOperationCandidates{}, fmt.Errorf(
				"find operation candidates for repo %d api %q revision %d method %s path %s: %w",
				snapshot.Repo.ID,
				snapshot.APIPath,
				snapshot.Revision.ID,
				method,
				path,
				err,
			)
		}
		candidates = make([]OperationSnapshot, 0, len(rows))
		for _, row := range rows {
			candidates = append(candidates, mapOperationCandidateByAPIMethodPathRow(row))
		}
	} else {
		rows, err := queries.FindOperationCandidatesByRepoRevisionAndMethodPath(
			ctx,
			sqlc.FindOperationCandidatesByRepoRevisionAndMethodPathParams{
				Method:             method,
				Path:               path,
				RepoID:             snapshot.Repo.ID,
				SnapshotRevisionID: snapshot.Revision.ID,
			},
		)
		if err != nil {
			return ResolvedOperationCandidates{}, fmt.Errorf(
				"find operation candidates for repo %d revision %d method %s path %s: %w",
				snapshot.Repo.ID,
				snapshot.Revision.ID,
				method,
				path,
				err,
			)
		}
		candidates = make([]OperationSnapshot, 0, len(rows))
		for _, row := range rows {
			candidates = append(candidates, mapOperationCandidateMethodPathRow(row))
		}
	}

	return ResolvedOperationCandidates{
		Snapshot:   snapshot,
		Candidates: candidates,
	}, nil
}

func filterAPISnapshotsWithResolvedSpec(items []APISnapshot) []APISnapshot {
	filtered := make([]APISnapshot, 0, len(items))
	for _, item := range items {
		if item.HasSnapshot {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func mapRepoCatalogInventoryRow(row sqlc.ListRepoCatalogInventoryRow) RepoCatalogEntry {
	return RepoCatalogEntry{
		Repo: Repo{
			ID:                row.ID,
			GitLabProjectID:   row.GitlabProjectID,
			PathWithNamespace: row.PathWithNamespace,
			DefaultBranch:     row.DefaultBranch,
		},
		OpenAPIForceRescan: row.OpenapiForceRescan,
		ActiveAPICount:     row.ActiveApiCount,
		HeadRevision:       mapCatalogHeadRevision(row.HeadPresent, row.HeadRevisionID, row.HeadRevisionSha, row.HeadRevisionStatus, row.HeadRevisionOpenapiChanged, row.HeadRevisionReceivedAt, row.HeadRevisionProcessedAt),
		SnapshotRevision:   mapCatalogSnapshotRevision(row.SnapshotPresent, row.SnapshotRevisionID, row.SnapshotRevisionSha, row.SnapshotRevisionProcessedAt),
	}
}

func mapRepoCatalogFreshnessRow(row sqlc.GetRepoCatalogFreshnessByPathRow) RepoCatalogEntry {
	return RepoCatalogEntry{
		Repo: Repo{
			ID:                row.ID,
			GitLabProjectID:   row.GitlabProjectID,
			PathWithNamespace: row.PathWithNamespace,
			DefaultBranch:     row.DefaultBranch,
		},
		OpenAPIForceRescan: row.OpenapiForceRescan,
		ActiveAPICount:     row.ActiveApiCount,
		HeadRevision:       mapCatalogHeadRevision(row.HeadPresent, row.HeadRevisionID, row.HeadRevisionSha, row.HeadRevisionStatus, row.HeadRevisionOpenapiChanged, row.HeadRevisionReceivedAt, row.HeadRevisionProcessedAt),
		SnapshotRevision:   mapCatalogSnapshotRevision(row.SnapshotPresent, row.SnapshotRevisionID, row.SnapshotRevisionSha, row.SnapshotRevisionProcessedAt),
	}
}

func mapCatalogHeadRevision(
	presentValue interface{},
	revisionID int64,
	sha string,
	status string,
	openAPIChanged pgtype.Bool,
	receivedAt pgtype.Timestamptz,
	processedAt pgtype.Timestamptz,
) *CatalogRevisionState {
	if !scanBool(presentValue) {
		return nil
	}

	revision := &CatalogRevisionState{
		ID:          revisionID,
		SHA:         sha,
		Status:      status,
		ReceivedAt:  timestamptzPtr(receivedAt),
		ProcessedAt: timestamptzPtr(processedAt),
	}
	if openAPIChanged.Valid {
		value := openAPIChanged.Bool
		revision.OpenAPIChanged = &value
	}
	return revision
}

func mapCatalogSnapshotRevision(
	presentValue interface{},
	revisionID int64,
	sha string,
	processedAt pgtype.Timestamptz,
) *CatalogRevisionState {
	if !scanBool(presentValue) {
		return nil
	}

	openAPIChanged := true
	return &CatalogRevisionState{
		ID:             revisionID,
		SHA:            sha,
		Status:         revisionProcessed,
		OpenAPIChanged: &openAPIChanged,
		ProcessedAt:    timestamptzPtr(processedAt),
	}
}

func mapAPISnapshotInventoryRow(row sqlc.ListAPISnapshotInventoryByRepoRevisionRow) APISnapshot {
	return APISnapshot{
		APISpecID:         row.ApiSpecID,
		API:               row.Api,
		Status:            row.Status,
		DisplayName:       textValue(row.DisplayName),
		HasSnapshot:       row.ApiSpecRevisionID.Valid,
		APISpecRevisionID: int8Value(row.ApiSpecRevisionID),
		IngestEventID:     int8Value(row.IngestEventID),
		IngestEventSHA:    textValue(row.IngestEventSha),
		IngestEventBranch: textValue(row.IngestEventBranch),
		SpecETag:          textValue(row.SpecEtag),
		SpecSizeBytes:     int8Value(row.SpecSizeBytes),
		OperationCount:    row.OperationCount,
	}
}

func mapAPISnapshotSelectionRow(row sqlc.GetAPISnapshotByRepoRevisionAndAPIRow) APISnapshot {
	return APISnapshot{
		APISpecID:         row.ApiSpecID,
		API:               row.Api,
		Status:            row.Status,
		DisplayName:       textValue(row.DisplayName),
		HasSnapshot:       row.ApiSpecRevisionID.Valid,
		APISpecRevisionID: int8Value(row.ApiSpecRevisionID),
		IngestEventID:     int8Value(row.IngestEventID),
		IngestEventSHA:    textValue(row.IngestEventSha),
		IngestEventBranch: textValue(row.IngestEventBranch),
		SpecETag:          textValue(row.SpecEtag),
		SpecSizeBytes:     int8Value(row.SpecSizeBytes),
		OperationCount:    row.OperationCount,
	}
}

func mapOperationInventoryRow(row sqlc.ListOperationInventoryByRepoRevisionRow) OperationSnapshot {
	return OperationSnapshot{
		APISpecID:         row.ApiSpecID,
		API:               row.Api,
		Status:            row.Status,
		APISpecRevisionID: row.ApiSpecRevisionID,
		IngestEventID:     row.IngestEventID,
		IngestEventSHA:    row.IngestEventSha,
		IngestEventBranch: row.IngestEventBranch,
		Method:            row.Method,
		Path:              row.Path,
		OperationID:       textValue(row.OperationID),
		Summary:           textValue(row.Summary),
		Deprecated:        row.Deprecated,
		RawJSON:           bytesCopy(row.RawJson),
	}
}

func mapOperationInventoryByAPIRow(row sqlc.ListOperationInventoryByRepoRevisionAndAPIRow) OperationSnapshot {
	return OperationSnapshot{
		APISpecID:         row.ApiSpecID,
		API:               row.Api,
		Status:            row.Status,
		APISpecRevisionID: row.ApiSpecRevisionID,
		IngestEventID:     row.IngestEventID,
		IngestEventSHA:    row.IngestEventSha,
		IngestEventBranch: row.IngestEventBranch,
		Method:            row.Method,
		Path:              row.Path,
		OperationID:       textValue(row.OperationID),
		Summary:           textValue(row.Summary),
		Deprecated:        row.Deprecated,
		RawJSON:           bytesCopy(row.RawJson),
	}
}

func mapOperationCandidateRow(row sqlc.FindOperationCandidatesByRepoRevisionAndOperationIDRow) OperationSnapshot {
	return OperationSnapshot{
		APISpecID:         row.ApiSpecID,
		API:               row.Api,
		Status:            row.Status,
		APISpecRevisionID: row.ApiSpecRevisionID,
		IngestEventID:     row.IngestEventID,
		IngestEventSHA:    row.IngestEventSha,
		IngestEventBranch: row.IngestEventBranch,
		Method:            row.Method,
		Path:              row.Path,
		OperationID:       textValue(row.OperationID),
		Summary:           textValue(row.Summary),
		Deprecated:        row.Deprecated,
		RawJSON:           bytesCopy(row.RawJson),
	}
}

func mapOperationCandidateByAPIRow(row sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndOperationIDRow) OperationSnapshot {
	return OperationSnapshot{
		APISpecID:         row.ApiSpecID,
		API:               row.Api,
		Status:            row.Status,
		APISpecRevisionID: row.ApiSpecRevisionID,
		IngestEventID:     row.IngestEventID,
		IngestEventSHA:    row.IngestEventSha,
		IngestEventBranch: row.IngestEventBranch,
		Method:            row.Method,
		Path:              row.Path,
		OperationID:       textValue(row.OperationID),
		Summary:           textValue(row.Summary),
		Deprecated:        row.Deprecated,
		RawJSON:           bytesCopy(row.RawJson),
	}
}

func mapOperationCandidateMethodPathRow(row sqlc.FindOperationCandidatesByRepoRevisionAndMethodPathRow) OperationSnapshot {
	return OperationSnapshot{
		APISpecID:         row.ApiSpecID,
		API:               row.Api,
		Status:            row.Status,
		APISpecRevisionID: row.ApiSpecRevisionID,
		IngestEventID:     row.IngestEventID,
		IngestEventSHA:    row.IngestEventSha,
		IngestEventBranch: row.IngestEventBranch,
		Method:            row.Method,
		Path:              row.Path,
		OperationID:       textValue(row.OperationID),
		Summary:           textValue(row.Summary),
		Deprecated:        row.Deprecated,
		RawJSON:           bytesCopy(row.RawJson),
	}
}

func mapOperationCandidateByAPIMethodPathRow(row sqlc.FindOperationCandidatesByRepoRevisionAndAPIAndMethodPathRow) OperationSnapshot {
	return OperationSnapshot{
		APISpecID:         row.ApiSpecID,
		API:               row.Api,
		Status:            row.Status,
		APISpecRevisionID: row.ApiSpecRevisionID,
		IngestEventID:     row.IngestEventID,
		IngestEventSHA:    row.IngestEventSha,
		IngestEventBranch: row.IngestEventBranch,
		Method:            row.Method,
		Path:              row.Path,
		OperationID:       textValue(row.OperationID),
		Summary:           textValue(row.Summary),
		Deprecated:        row.Deprecated,
		RawJSON:           bytesCopy(row.RawJson),
	}
}

func textValue(value pgtype.Text) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func int8Value(value pgtype.Int8) int64 {
	if value.Valid {
		return value.Int64
	}
	return 0
}

func timestamptzPtr(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := value.Time.UTC()
	return &timestamp
}

func scanBool(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case nil:
		return false
	default:
		return false
	}
}
