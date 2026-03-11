package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
)

var ErrRepoNotFound = errors.New("repo not found")

type Repo struct {
	ID                int64
	GitLabProjectID   int64
	PathWithNamespace string
	DefaultBranch     string
}

type Revision struct {
	ID             int64
	RepoID         int64
	Sha            string
	Branch         string
	ParentSHA      string
	ProcessedAt    *time.Time
	OpenAPIChanged *bool
	Status         string
	Error          string
}

type revisionCountQueries interface {
	CountRevisions(ctx context.Context) (int64, error)
}

func (s *Store) CountRevisions(ctx context.Context) (int64, error) {
	if s == nil || !s.configured || s.pool == nil {
		return 0, ErrStoreNotConfigured
	}

	return countRevisions(ctx, sqlc.New(s.pool))
}

func countRevisions(ctx context.Context, queries revisionCountQueries) (int64, error) {
	count, err := queries.CountRevisions(ctx)
	if err != nil {
		return 0, fmt.Errorf("count revisions: %w", err)
	}
	return count, nil
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
		ID:                repo.ID,
		GitLabProjectID:   repo.GitlabProjectID,
		PathWithNamespace: repo.PathWithNamespace,
		DefaultBranch:     repo.DefaultBranch,
	}, nil
}

func (s *Store) GetRevisionByID(ctx context.Context, revisionID int64) (Revision, error) {
	if s == nil || !s.configured || s.pool == nil {
		return Revision{}, ErrStoreNotConfigured
	}
	if revisionID < 1 {
		return Revision{}, errors.New("revision id must be positive")
	}

	revision, err := sqlc.New(s.pool).GetRevisionByID(ctx, revisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Revision{}, fmt.Errorf("revision not found: id=%d", revisionID)
		}
		return Revision{}, fmt.Errorf("get revision by id %d: %w", revisionID, err)
	}

	return mapRevision(revision), nil
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

func mapRevision(revision sqlc.IngestEvent) Revision {
	mapped := Revision{
		ID:     revision.ID,
		RepoID: revision.RepoID,
		Sha:    revision.Sha,
		Branch: revision.Branch,
		Status: revision.Status,
		Error:  revision.Error,
	}
	if revision.ParentSha.Valid {
		mapped.ParentSHA = revision.ParentSha.String
	}
	if revision.ProcessedAt.Valid {
		processedAt := revision.ProcessedAt.Time.UTC()
		mapped.ProcessedAt = &processedAt
	}
	if revision.OpenapiChanged.Valid {
		openAPIChanged := revision.OpenapiChanged.Bool
		mapped.OpenAPIChanged = &openAPIChanged
	}
	return mapped
}
