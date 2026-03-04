package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
)

type RepoBootstrapState struct {
	ActiveAPICount int64
	ForceRescan    bool
}

func (s *Store) GetRepoBootstrapState(ctx context.Context, repoID int64) (RepoBootstrapState, error) {
	if s == nil || !s.configured || s.pool == nil {
		return RepoBootstrapState{}, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return RepoBootstrapState{}, errors.New("repo id must be positive")
	}

	return getRepoBootstrapState(ctx, sqlc.New(s.pool), repoID)
}

type repoBootstrapStateQueries interface {
	GetRepoBootstrapState(ctx context.Context, repoID int64) (sqlc.GetRepoBootstrapStateRow, error)
}

func getRepoBootstrapState(
	ctx context.Context,
	queries repoBootstrapStateQueries,
	repoID int64,
) (RepoBootstrapState, error) {
	row, err := queries.GetRepoBootstrapState(ctx, repoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RepoBootstrapState{}, fmt.Errorf("%w: id=%d", ErrRepoNotFound, repoID)
		}
		return RepoBootstrapState{}, fmt.Errorf("get repo bootstrap state for repo %d: %w", repoID, err)
	}

	return RepoBootstrapState{
		ActiveAPICount: row.ActiveApiCount,
		ForceRescan:    row.ForceRescan,
	}, nil
}

func (s *Store) ClearRepoForceRescan(ctx context.Context, repoID int64) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}
	if repoID < 1 {
		return errors.New("repo id must be positive")
	}

	return clearRepoForceRescan(ctx, sqlc.New(s.pool), repoID)
}

type clearRepoForceRescanQueries interface {
	ClearRepoForceRescan(ctx context.Context, repoID int64) error
}

func clearRepoForceRescan(ctx context.Context, queries clearRepoForceRescanQueries, repoID int64) error {
	if err := queries.ClearRepoForceRescan(ctx, repoID); err != nil {
		return fmt.Errorf("clear repo force rescan for repo %d: %w", repoID, err)
	}
	return nil
}
