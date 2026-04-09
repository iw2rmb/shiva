package httpserver

import (
	"context"

	"github.com/iw2rmb/shiva/internal/store"
)

type queryReadStore interface {
	GetRepoByNamespaceAndRepo(ctx context.Context, namespace string, repo string) (store.Repo, error)
	ResolveReadSnapshot(ctx context.Context, input store.ResolveReadSnapshotInput) (store.ResolvedReadSnapshot, error)
	ResolveSpecSnapshots(ctx context.Context, input store.ResolveReadSnapshotInput) (store.ResolvedSpecSnapshots, error)
	ResolveOperationCandidatesByOperationID(
		ctx context.Context,
		input store.ResolveOperationByIDInput,
	) (store.ResolvedOperationCandidates, error)
	ResolveOperationCandidatesByMethodPath(
		ctx context.Context,
		input store.ResolveOperationByMethodPathInput,
	) (store.ResolvedOperationCandidates, error)
	GetSpecArtifactByAPISpecRevisionID(ctx context.Context, apiSpecRevisionID int64) (store.SpecArtifact, error)
	GetAPISpecRevisionByID(ctx context.Context, apiSpecRevisionID int64) (store.APISpecRevision, error)
	ListAPISnapshotInventoryByRepoRevision(
		ctx context.Context,
		repoID int64,
		snapshotRevisionID int64,
	) ([]store.APISnapshot, error)
	ListAPISnapshotInventoryByRepoRevisionPage(
		ctx context.Context,
		repoID int64,
		snapshotRevisionID int64,
		queryPrefix string,
		limit int32,
		offset int32,
	) ([]store.APISnapshot, error)
	ListAPICatalogInventory(ctx context.Context, namespace string, repo string) ([]store.APISnapshot, error)
	ListAPICatalogInventoryPage(ctx context.Context, input store.APIInventoryListInput) ([]store.APISnapshot, error)
	GetAPISnapshotByRepoRevisionAndAPI(
		ctx context.Context,
		repoID int64,
		api string,
		snapshotRevisionID int64,
	) (store.APISnapshot, bool, error)
	ListOperationInventoryByRepoRevision(
		ctx context.Context,
		repoID int64,
		snapshotRevisionID int64,
	) ([]store.OperationSnapshot, error)
	ListOperationInventoryByRepoRevisionPage(
		ctx context.Context,
		repoID int64,
		snapshotRevisionID int64,
		queryPrefix string,
		limit int32,
		offset int32,
	) ([]store.OperationSnapshot, error)
	CountOperationInventoryByRepoRevision(
		ctx context.Context,
		repoID int64,
		snapshotRevisionID int64,
		queryPrefix string,
	) (store.OperationCatalogCount, error)
	ListOperationCatalogInventory(
		ctx context.Context,
		namespace string,
	) ([]store.OperationSnapshot, error)
	ListOperationCatalogInventoryPage(
		ctx context.Context,
		namespace string,
		repo string,
		queryPrefix string,
		limit int32,
		offset int32,
	) ([]store.OperationSnapshot, error)
	CountOperationCatalogInventory(
		ctx context.Context,
		namespace string,
		repo string,
		queryPrefix string,
	) (store.OperationCatalogCount, error)
	CountAPICatalogInventory(ctx context.Context, input store.APIInventoryCountInput) (store.OperationCatalogCount, error)
	CountAPIInventoryByRepoRevision(
		ctx context.Context,
		repoID int64,
		snapshotRevisionID int64,
		queryPrefix string,
	) (store.OperationCatalogCount, error)
	ListOperationInventoryByRepoRevisionAndAPI(
		ctx context.Context,
		repoID int64,
		api string,
		snapshotRevisionID int64,
	) ([]store.OperationSnapshot, error)
	ListOperationInventoryByRepoRevisionAndAPIPage(
		ctx context.Context,
		repoID int64,
		api string,
		snapshotRevisionID int64,
		queryPrefix string,
		limit int32,
		offset int32,
	) ([]store.OperationSnapshot, error)
	ListNamespaceCatalogInventory(
		ctx context.Context,
		input store.NamespaceCatalogListInput,
	) (store.NamespaceCatalogListResult, error)
	CountNamespaceCatalogInventory(
		ctx context.Context,
		input store.NamespaceCatalogCountInput,
	) (int64, error)
	ListVacuumIssuesByAPISpecRevisionID(ctx context.Context, apiSpecRevisionID int64) ([]store.VacuumIssue, error)
	ListRepoCatalogInventory(ctx context.Context) ([]store.RepoCatalogEntry, error)
	GetRepoCatalogFreshness(ctx context.Context, namespace string, repo string) (store.RepoCatalogFreshness, error)
}
