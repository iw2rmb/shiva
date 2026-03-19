package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iw2rmb/shiva/internal/repoid"
	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var ErrRepoNotFound = errors.New("repo not found")

type Repo struct {
	ID              int64
	GitLabProjectID int64
	Namespace       string
	Repo            string
	DefaultBranch   string
}

func (r Repo) Path() string {
	return repoid.Identity{Namespace: r.Namespace, Repo: r.Repo}.Path()
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
		GitLabProjectID: repo.GitlabProjectID,
		Namespace:       repo.Namespace,
		Repo:            repo.Repo,
		DefaultBranch:   repo.DefaultBranch,
	}, nil
}

func (s *Store) GetRepoByNamespaceAndRepo(ctx context.Context, namespace string, repo string) (Repo, error) {
	if s == nil || !s.configured || s.pool == nil {
		return Repo{}, ErrStoreNotConfigured
	}

	namespace = strings.TrimSpace(namespace)
	repo = strings.TrimSpace(repo)
	if namespace == "" {
		return Repo{}, errors.New("namespace must not be empty")
	}
	if repo == "" {
		return Repo{}, errors.New("repo must not be empty")
	}

	row, err := sqlc.New(s.pool).GetRepoByNamespaceAndRepo(ctx, sqlc.GetRepoByNamespaceAndRepoParams{
		Namespace: namespace,
		Repo:      repo,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Repo{}, fmt.Errorf("%w: path=%s/%s", ErrRepoNotFound, namespace, repo)
		}
		return Repo{}, fmt.Errorf("get repo by path %q: %w", namespace+"/"+repo, err)
	}

	return Repo{
		ID:              row.ID,
		GitLabProjectID: row.GitlabProjectID,
		Namespace:       row.Namespace,
		Repo:            row.Repo,
		DefaultBranch:   row.DefaultBranch,
	}, nil
}

func (s *Store) GetRevisionByID(ctx context.Context, revisionID int64) (Revision, error) {
	if s == nil || !s.configured || s.pool == nil {
		return Revision{}, ErrStoreNotConfigured
	}
	if revisionID < 1 {
		return Revision{}, errors.New("revision id must be positive")
	}

	revision, err := sqlc.New(s.pool).GetRevisionStateByID(ctx, revisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Revision{}, fmt.Errorf("revision not found: id=%d", revisionID)
		}
		return Revision{}, fmt.Errorf("get revision by id %d: %w", revisionID, err)
	}

	return mapRevisionStateByIDRow(revision), nil
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

	revision, err := sqlc.New(s.pool).GetLatestProcessedOpenAPIRevisionStateByBranchExcludingID(
		ctx,
		sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDParams{
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

	return mapRevisionStateByLatestProcessedOpenAPIBranchRow(revision), true, nil
}

func mapRevision(revision sqlc.IngestEvent) Revision {
	return mapRevisionStateFields(
		revision.ID,
		revision.RepoID,
		revision.Sha,
		revision.Branch,
		revision.ParentSha,
		revision.ProcessedAt,
		revision.OpenapiChanged,
		revision.Status,
		revision.Error,
	)
}

func mapRevisionStateByIDRow(revision sqlc.GetRevisionStateByIDRow) Revision {
	return mapRevisionStateFields(
		revision.ID,
		revision.RepoID,
		revision.Sha,
		revision.Branch,
		revision.ParentSha,
		revision.ProcessedAt,
		revision.OpenapiChanged,
		revision.Status,
		revision.Error,
	)
}

func mapRevisionStateBySHAPrefixRow(revision sqlc.GetRevisionStateByRepoSHAPrefixRow) Revision {
	return mapRevisionStateFields(
		revision.ID,
		revision.RepoID,
		revision.Sha,
		revision.Branch,
		revision.ParentSha,
		revision.ProcessedAt,
		revision.OpenapiChanged,
		revision.Status,
		revision.Error,
	)
}

func mapRevisionStateByLatestBranchRow(revision sqlc.GetLatestRevisionStateByBranchRow) Revision {
	return mapRevisionStateFields(
		revision.ID,
		revision.RepoID,
		revision.Sha,
		revision.Branch,
		revision.ParentSha,
		revision.ProcessedAt,
		revision.OpenapiChanged,
		revision.Status,
		revision.Error,
	)
}

func mapRevisionStateByLatestProcessedOpenAPIBranchRow(
	revision sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDRow,
) Revision {
	return mapRevisionStateFields(
		revision.ID,
		revision.RepoID,
		revision.Sha,
		revision.Branch,
		revision.ParentSha,
		revision.ProcessedAt,
		revision.OpenapiChanged,
		revision.Status,
		revision.Error,
	)
}

func mapRevisionStateFields(
	id int64,
	repoID int64,
	sha string,
	branch string,
	parentSHA pgtype.Text,
	processedAt pgtype.Timestamptz,
	openAPIChanged pgtype.Bool,
	status string,
	errMessage string,
) Revision {
	mapped := Revision{
		ID:     id,
		RepoID: repoID,
		Sha:    sha,
		Branch: branch,
		Status: status,
		Error:  errMessage,
	}
	if parentSHA.Valid {
		mapped.ParentSHA = parentSHA.String
	}
	if processedAt.Valid {
		value := processedAt.Time.UTC()
		mapped.ProcessedAt = &value
	}
	if openAPIChanged.Valid {
		value := openAPIChanged.Bool
		mapped.OpenAPIChanged = &value
	}
	return mapped
}
