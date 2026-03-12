package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/repoid"
	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type ReadSnapshotSelectorKind string

const (
	ReadSnapshotSelectorDefaultBranchLatest ReadSnapshotSelectorKind = "default_branch_latest"
	ReadSnapshotSelectorRevisionID          ReadSnapshotSelectorKind = "revision_id"
	ReadSnapshotSelectorSHA                 ReadSnapshotSelectorKind = "sha"
)

type ResolveReadSnapshotInput struct {
	Namespace  string
	Repo       string
	APIPath    string
	RevisionID int64
	SHA        string
}

type ResolvedReadSnapshot struct {
	Repo Repo

	APIPath      string
	SelectorKind ReadSnapshotSelectorKind
	Revision     Revision
}

type ReadSnapshotResolutionErrorCode string

const (
	ReadSnapshotResolutionInvalidInput ReadSnapshotResolutionErrorCode = "invalid_input"
	ReadSnapshotResolutionNotFound     ReadSnapshotResolutionErrorCode = "not_found"
	ReadSnapshotResolutionUnprocessed  ReadSnapshotResolutionErrorCode = "unprocessed"
)

type ReadSnapshotResolutionError struct {
	Code ReadSnapshotResolutionErrorCode

	RepoPath   string
	APIPath    string
	RevisionID int64
	SHA        string

	IngestEventID     int64
	IngestEventStatus string

	Err error
}

func (e *ReadSnapshotResolutionError) Error() string {
	if e == nil {
		return "read snapshot resolution error"
	}

	message := fmt.Sprintf("read snapshot resolution failed: code=%s", e.Code)
	if e.RepoPath != "" {
		message += fmt.Sprintf(" repo=%q", e.RepoPath)
	}
	if e.APIPath != "" {
		message += fmt.Sprintf(" api=%q", e.APIPath)
	}
	if e.RevisionID > 0 {
		message += fmt.Sprintf(" revision_id=%d", e.RevisionID)
	}
	if e.SHA != "" {
		message += fmt.Sprintf(" sha=%q", e.SHA)
	}
	if e.IngestEventID > 0 {
		message += fmt.Sprintf(" ingest_event_id=%d", e.IngestEventID)
	}
	if e.IngestEventStatus != "" {
		message += fmt.Sprintf(" status=%q", e.IngestEventStatus)
	}
	if e.Err != nil {
		message += fmt.Sprintf(": %v", e.Err)
	}
	return message
}

func (e *ReadSnapshotResolutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *ReadSnapshotResolutionError) Is(target error) bool {
	targetError, ok := target.(*ReadSnapshotResolutionError)
	if !ok {
		return false
	}
	return e.Code == targetError.Code
}

var (
	ErrReadSnapshotInvalidInput = &ReadSnapshotResolutionError{Code: ReadSnapshotResolutionInvalidInput}
	ErrReadSnapshotNotFound     = &ReadSnapshotResolutionError{Code: ReadSnapshotResolutionNotFound}
	ErrReadSnapshotUnprocessed  = &ReadSnapshotResolutionError{Code: ReadSnapshotResolutionUnprocessed}
)

type normalizedResolveReadSnapshotInput struct {
	namespace  string
	repo       string
	repoPath   string
	apiPath    string
	revisionID int64
	sha        string
	kind       ReadSnapshotSelectorKind
}

type readSnapshotQueries interface {
	GetRepoByNamespaceAndRepo(ctx context.Context, arg sqlc.GetRepoByNamespaceAndRepoParams) (sqlc.Repo, error)
	GetRevisionByID(ctx context.Context, id int64) (sqlc.IngestEvent, error)
	GetRevisionByRepoSHAPrefix(ctx context.Context, arg sqlc.GetRevisionByRepoSHAPrefixParams) (sqlc.IngestEvent, error)
	GetLatestRevisionByBranch(ctx context.Context, arg sqlc.GetLatestRevisionByBranchParams) (sqlc.IngestEvent, error)
	GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
		ctx context.Context,
		arg sqlc.GetLatestProcessedOpenAPIRevisionByBranchExcludingIDParams,
	) (sqlc.IngestEvent, error)
}

func (s *Store) ResolveReadSnapshot(ctx context.Context, input ResolveReadSnapshotInput) (ResolvedReadSnapshot, error) {
	if s == nil || !s.configured || s.pool == nil {
		return ResolvedReadSnapshot{}, ErrStoreNotConfigured
	}

	normalized, err := normalizeResolveReadSnapshotInput(input)
	if err != nil {
		return ResolvedReadSnapshot{}, err
	}

	return resolveReadSnapshot(ctx, sqlc.New(s.pool), normalized)
}

func resolveReadSnapshot(
	ctx context.Context,
	queries readSnapshotQueries,
	input normalizedResolveReadSnapshotInput,
) (ResolvedReadSnapshot, error) {
	repo, err := queries.GetRepoByNamespaceAndRepo(ctx, sqlc.GetRepoByNamespaceAndRepoParams{
		Namespace: input.namespace,
		Repo:      input.repo,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSnapshot{}, &ReadSnapshotResolutionError{
				Code:       ReadSnapshotResolutionNotFound,
				RepoPath:   input.repoPath,
				APIPath:    input.apiPath,
				RevisionID: input.revisionID,
				SHA:        input.sha,
			}
		}
		return ResolvedReadSnapshot{}, fmt.Errorf("load repo %q: %w", input.repoPath, err)
	}

	switch input.kind {
	case ReadSnapshotSelectorRevisionID:
		return resolveReadSnapshotByRevisionID(ctx, queries, repo, input)
	case ReadSnapshotSelectorSHA:
		return resolveReadSnapshotBySHA(ctx, queries, repo, input)
	case ReadSnapshotSelectorDefaultBranchLatest:
		return resolveReadSnapshotByDefaultBranchLatest(ctx, queries, repo, input)
	default:
		return ResolvedReadSnapshot{}, &ReadSnapshotResolutionError{
			Code:       ReadSnapshotResolutionInvalidInput,
			RepoPath:   input.repoPath,
			APIPath:    input.apiPath,
			RevisionID: input.revisionID,
			SHA:        input.sha,
			Err:        fmt.Errorf("unsupported selector kind %q", input.kind),
		}
	}
}

func resolveReadSnapshotByRevisionID(
	ctx context.Context,
	queries readSnapshotQueries,
	repo sqlc.Repo,
	input normalizedResolveReadSnapshotInput,
) (ResolvedReadSnapshot, error) {
	revision, err := queries.GetRevisionByID(ctx, input.revisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSnapshot{}, &ReadSnapshotResolutionError{
				Code:       ReadSnapshotResolutionNotFound,
				RepoPath:   input.repoPath,
				APIPath:    input.apiPath,
				RevisionID: input.revisionID,
				SHA:        input.sha,
			}
		}
		return ResolvedReadSnapshot{}, fmt.Errorf("load revision %d for repo %q: %w", input.revisionID, input.repoPath, err)
	}

	if revision.RepoID != repo.ID {
		return ResolvedReadSnapshot{}, &ReadSnapshotResolutionError{
			Code:       ReadSnapshotResolutionNotFound,
			RepoPath:   input.repoPath,
			APIPath:    input.apiPath,
			RevisionID: input.revisionID,
			SHA:        input.sha,
		}
	}

	return buildResolvedReadSnapshot(repo, input, revision)
}

func resolveReadSnapshotBySHA(
	ctx context.Context,
	queries readSnapshotQueries,
	repo sqlc.Repo,
	input normalizedResolveReadSnapshotInput,
) (ResolvedReadSnapshot, error) {
	revision, err := queries.GetRevisionByRepoSHAPrefix(ctx, sqlc.GetRevisionByRepoSHAPrefixParams{
		RepoID: repo.ID,
		ShaPrefix: pgtype.Text{
			String: input.sha,
			Valid:  true,
		},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSnapshot{}, &ReadSnapshotResolutionError{
				Code:       ReadSnapshotResolutionNotFound,
				RepoPath:   input.repoPath,
				APIPath:    input.apiPath,
				RevisionID: input.revisionID,
				SHA:        input.sha,
			}
		}
		return ResolvedReadSnapshot{}, fmt.Errorf("load revision by sha %q for repo %q: %w", input.sha, input.repoPath, err)
	}

	return buildResolvedReadSnapshot(repo, input, revision)
}

func resolveReadSnapshotByDefaultBranchLatest(
	ctx context.Context,
	queries readSnapshotQueries,
	repo sqlc.Repo,
	input normalizedResolveReadSnapshotInput,
) (ResolvedReadSnapshot, error) {
	headRevision, err := queries.GetLatestRevisionByBranch(ctx, sqlc.GetLatestRevisionByBranchParams{
		RepoID: repo.ID,
		Branch: repo.DefaultBranch,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSnapshot{}, &ReadSnapshotResolutionError{
				Code:       ReadSnapshotResolutionNotFound,
				RepoPath:   input.repoPath,
				APIPath:    input.apiPath,
				RevisionID: input.revisionID,
				SHA:        input.sha,
			}
		}
		return ResolvedReadSnapshot{}, fmt.Errorf(
			"load latest revision for repo %q branch %q: %w",
			input.repoPath,
			repo.DefaultBranch,
			err,
		)
	}

	if headRevision.Status != revisionProcessed {
		return ResolvedReadSnapshot{}, &ReadSnapshotResolutionError{
			Code:              ReadSnapshotResolutionUnprocessed,
			RepoPath:          input.repoPath,
			APIPath:           input.apiPath,
			RevisionID:        input.revisionID,
			SHA:               input.sha,
			IngestEventID:     headRevision.ID,
			IngestEventStatus: headRevision.Status,
		}
	}

	revision, err := queries.GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
		ctx,
		sqlc.GetLatestProcessedOpenAPIRevisionByBranchExcludingIDParams{
			RepoID:            repo.ID,
			Branch:            repo.DefaultBranch,
			ExcludeRevisionID: 0,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSnapshot{}, &ReadSnapshotResolutionError{
				Code:       ReadSnapshotResolutionNotFound,
				RepoPath:   input.repoPath,
				APIPath:    input.apiPath,
				RevisionID: input.revisionID,
				SHA:        input.sha,
			}
		}
		return ResolvedReadSnapshot{}, fmt.Errorf(
			"load latest processed openapi revision for repo %q branch %q: %w",
			input.repoPath,
			repo.DefaultBranch,
			err,
		)
	}

	return ResolvedReadSnapshot{
		Repo: Repo{
			ID:              repo.ID,
			GitLabProjectID: repo.GitlabProjectID,
			Namespace:       repo.Namespace,
			Repo:            repo.Repo,
			DefaultBranch:   repo.DefaultBranch,
		},
		APIPath:      input.apiPath,
		SelectorKind: input.kind,
		Revision:     mapRevision(revision),
	}, nil
}

func buildResolvedReadSnapshot(
	repo sqlc.Repo,
	input normalizedResolveReadSnapshotInput,
	revision sqlc.IngestEvent,
) (ResolvedReadSnapshot, error) {
	if revision.Status != revisionProcessed {
		return ResolvedReadSnapshot{}, &ReadSnapshotResolutionError{
			Code:              ReadSnapshotResolutionUnprocessed,
			RepoPath:          input.repoPath,
			APIPath:           input.apiPath,
			RevisionID:        input.revisionID,
			SHA:               input.sha,
			IngestEventID:     revision.ID,
			IngestEventStatus: revision.Status,
		}
	}

	return ResolvedReadSnapshot{
		Repo: Repo{
			ID:              repo.ID,
			GitLabProjectID: repo.GitlabProjectID,
			Namespace:       repo.Namespace,
			Repo:            repo.Repo,
			DefaultBranch:   repo.DefaultBranch,
		},
		APIPath:      input.apiPath,
		SelectorKind: input.kind,
		Revision:     mapRevision(revision),
	}, nil
}

func normalizeResolveReadSnapshotInput(input ResolveReadSnapshotInput) (normalizedResolveReadSnapshotInput, error) {
	normalized := normalizedResolveReadSnapshotInput{
		namespace:  strings.TrimSpace(input.Namespace),
		repo:       strings.TrimSpace(input.Repo),
		apiPath:    strings.TrimSpace(input.APIPath),
		revisionID: input.RevisionID,
		sha:        strings.TrimSpace(input.SHA),
	}
	normalized.repoPath = repoid.Identity{Namespace: normalized.namespace, Repo: normalized.repo}.Path()

	if normalized.namespace == "" {
		return normalizedResolveReadSnapshotInput{}, &ReadSnapshotResolutionError{
			Code: ReadSnapshotResolutionInvalidInput,
			Err:  errors.New("namespace must not be empty"),
		}
	}

	if normalized.repo == "" {
		return normalizedResolveReadSnapshotInput{}, &ReadSnapshotResolutionError{
			Code: ReadSnapshotResolutionInvalidInput,
			Err:  errors.New("repo must not be empty"),
		}
	}

	if normalized.revisionID > 0 && normalized.sha != "" {
		return normalizedResolveReadSnapshotInput{}, &ReadSnapshotResolutionError{
			Code:       ReadSnapshotResolutionInvalidInput,
			RepoPath:   normalized.repoPath,
			APIPath:    normalized.apiPath,
			RevisionID: normalized.revisionID,
			SHA:        normalized.sha,
			Err:        errors.New("revision_id and sha are mutually exclusive"),
		}
	}

	if normalized.revisionID < 0 {
		return normalizedResolveReadSnapshotInput{}, &ReadSnapshotResolutionError{
			Code:       ReadSnapshotResolutionInvalidInput,
			RepoPath:   normalized.repoPath,
			APIPath:    normalized.apiPath,
			RevisionID: normalized.revisionID,
			SHA:        normalized.sha,
			Err:        errors.New("revision_id must be positive"),
		}
	}

	switch {
	case normalized.revisionID > 0:
		normalized.kind = ReadSnapshotSelectorRevisionID
	case normalized.sha != "":
		if !isShortSHA(normalized.sha) {
			return normalizedResolveReadSnapshotInput{}, &ReadSnapshotResolutionError{
				Code:       ReadSnapshotResolutionInvalidInput,
				RepoPath:   normalized.repoPath,
				APIPath:    normalized.apiPath,
				RevisionID: normalized.revisionID,
				SHA:        normalized.sha,
				Err:        errors.New("sha must be exactly 8 lowercase hex characters"),
			}
		}
		normalized.kind = ReadSnapshotSelectorSHA
	default:
		normalized.kind = ReadSnapshotSelectorDefaultBranchLatest
	}

	return normalized, nil
}
