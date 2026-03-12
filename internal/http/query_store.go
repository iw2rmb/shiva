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
	ListAPISnapshotInventoryByRepoRevision(
		ctx context.Context,
		repoID int64,
		snapshotRevisionID int64,
	) ([]store.APISnapshot, error)
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
	ListOperationInventoryByRepoRevisionAndAPI(
		ctx context.Context,
		repoID int64,
		api string,
		snapshotRevisionID int64,
	) ([]store.OperationSnapshot, error)
	ListRepoCatalogInventory(ctx context.Context) ([]store.RepoCatalogEntry, error)
	GetRepoCatalogFreshness(ctx context.Context, namespace string, repo string) (store.RepoCatalogFreshness, error)
}
