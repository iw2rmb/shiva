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

type Revision struct {
	ID        int64
	RepoID    int64
	Sha       string
	Branch    string
	ParentSHA string
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

func (s *Store) GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
	ctx context.Context,
	repoID int64,
	branch string,
	excludeRevisionID int64,
) (Revision, bool, error) {
	if s == nil || !s.configured || s.pool == nil {
		return Revision{}, false, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return Revision{}, false, errors.New("repo id must be positive")
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return Revision{}, false, errors.New("branch must not be empty")
	}
	if excludeRevisionID < 1 {
		return Revision{}, false, errors.New("exclude revision id must be positive")
	}

	revision, err := sqlc.New(s.pool).GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
		ctx,
		sqlc.GetLatestProcessedOpenAPIRevisionByBranchExcludingIDParams{
			RepoID:            repoID,
			Branch:            branch,
			ExcludeRevisionID: excludeRevisionID,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Revision{}, false, nil
		}
		return Revision{}, false, fmt.Errorf(
			"get latest processed openapi revision for repo %d branch %q excluding %d: %w",
			repoID,
			branch,
			excludeRevisionID,
			err,
		)
	}

	return mapRevision(revision), true, nil
}

func mapRevision(revision sqlc.Revision) Revision {
	mapped := Revision{
		ID:     revision.ID,
		RepoID: revision.RepoID,
		Sha:    revision.Sha,
		Branch: revision.Branch,
	}
	if revision.ParentSha.Valid {
		mapped.ParentSHA = revision.ParentSha.String
	}
	return mapped
}
