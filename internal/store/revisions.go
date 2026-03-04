package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var ErrRepoNotFound = errors.New("repo not found")

type Repo struct {
	ID              int64
	TenantID        int64
	GitLabProjectID int64
	DefaultBranch   string
}

func (s *Store) GetRepoByID(ctx context.Context, repoID int64) (Repo, error) {
	if s == nil || !s.configured || s.pool == nil {
		return Repo{}, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return Repo{}, errors.New("repo id must be positive")
	}

	repo, err := sqlc.New(s.pool).GetRepoByID(ctx, repoID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Repo{}, fmt.Errorf("%w: id=%d", ErrRepoNotFound, repoID)
		}
		return Repo{}, fmt.Errorf("get repo by id %d: %w", repoID, err)
	}

	return Repo{
		ID:              repo.ID,
		TenantID:        repo.TenantID,
		GitLabProjectID: repo.GitlabProjectID,
		DefaultBranch:   repo.DefaultBranch,
	}, nil
}

func (s *Store) MarkRevisionProcessed(ctx context.Context, revisionID int64, openapiChanged bool) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}
	if revisionID < 1 {
		return errors.New("revision id must be positive")
	}

	if _, err := sqlc.New(s.pool).MarkRevisionProcessed(ctx, sqlc.MarkRevisionProcessedParams{
		OpenapiChanged: pgtype.Bool{Bool: openapiChanged, Valid: true},
		ID:             revisionID,
	}); err != nil {
		return fmt.Errorf("mark revision %d processed: %w", revisionID, err)
	}
	return nil
}

func (s *Store) MarkRevisionFailed(ctx context.Context, revisionID int64, errorMessage string) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}
	if revisionID < 1 {
		return errors.New("revision id must be positive")
	}

	if _, err := sqlc.New(s.pool).MarkRevisionFailed(ctx, sqlc.MarkRevisionFailedParams{
		Error: strings.TrimSpace(errorMessage),
		ID:    revisionID,
	}); err != nil {
		return fmt.Errorf("mark revision %d failed: %w", revisionID, err)
	}
	return nil
}
