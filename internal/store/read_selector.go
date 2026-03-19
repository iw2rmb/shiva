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

const (
	shortSHASelectorLength = 8
	revisionProcessed      = "processed"
)

type SelectorKind string

const (
	SelectorKindSHA        SelectorKind = "sha"
	SelectorKindNoSelector SelectorKind = "no_selector"
)

type ResolveReadSelectorInput struct {
	Namespace  string
	Repo       string
	Selector   string
	NoSelector bool
}

type ResolvedReadSelector struct {
	RepoID   int64
	RepoPath string

	SelectorKind SelectorKind
	Selector     string
	Revision     Revision
}

type SelectorResolutionErrorCode string

const (
	SelectorResolutionInvalidInput SelectorResolutionErrorCode = "invalid_input"
	SelectorResolutionNotFound     SelectorResolutionErrorCode = "not_found"
	SelectorResolutionUnprocessed  SelectorResolutionErrorCode = "unprocessed"
)

type SelectorResolutionError struct {
	Code SelectorResolutionErrorCode

	RepoPath string
	Selector string

	IngestEventID     int64
	IngestEventStatus string

	Err error
}

func (e *SelectorResolutionError) Error() string {
	if e == nil {
		return "selector resolution error"
	}

	message := fmt.Sprintf("selector resolution failed: code=%s", e.Code)
	if e.RepoPath != "" {
		message += fmt.Sprintf(" repo=%q", e.RepoPath)
	}
	if e.Selector != "" {
		message += fmt.Sprintf(" selector=%q", e.Selector)
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

func (e *SelectorResolutionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *SelectorResolutionError) Is(target error) bool {
	targetError, ok := target.(*SelectorResolutionError)
	if !ok {
		return false
	}
	return e.Code == targetError.Code
}

var (
	ErrSelectorInvalidInput = &SelectorResolutionError{Code: SelectorResolutionInvalidInput}
	ErrSelectorNotFound     = &SelectorResolutionError{Code: SelectorResolutionNotFound}
	ErrSelectorUnprocessed  = &SelectorResolutionError{Code: SelectorResolutionUnprocessed}
)

type normalizedResolveReadSelectorInput struct {
	namespace string
	repo      string
	repoPath  string
	selector  string
	kind      SelectorKind
}

type selectorResolutionQueries interface {
	GetRepoByNamespaceAndRepo(ctx context.Context, arg sqlc.GetRepoByNamespaceAndRepoParams) (sqlc.Repo, error)
	GetRevisionStateByRepoSHAPrefix(
		ctx context.Context,
		arg sqlc.GetRevisionStateByRepoSHAPrefixParams,
	) (sqlc.GetRevisionStateByRepoSHAPrefixRow, error)
	GetLatestRevisionStateByBranch(
		ctx context.Context,
		arg sqlc.GetLatestRevisionStateByBranchParams,
	) (sqlc.GetLatestRevisionStateByBranchRow, error)
	GetLatestProcessedOpenAPIRevisionStateByBranchExcludingID(
		ctx context.Context,
		arg sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDParams,
	) (sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDRow, error)
}

func (s *Store) ResolveReadSelector(ctx context.Context, input ResolveReadSelectorInput) (ResolvedReadSelector, error) {
	if s == nil || !s.configured || s.pool == nil {
		return ResolvedReadSelector{}, ErrStoreNotConfigured
	}

	normalized, err := normalizeResolveReadSelectorInput(input)
	if err != nil {
		return ResolvedReadSelector{}, err
	}

	resolved, err := resolveReadSelector(ctx, sqlc.New(s.pool), normalized)
	if err != nil {
		return ResolvedReadSelector{}, err
	}
	return resolved, nil
}

func resolveReadSelector(
	ctx context.Context,
	queries selectorResolutionQueries,
	input normalizedResolveReadSelectorInput,
) (ResolvedReadSelector, error) {
	repo, err := queries.GetRepoByNamespaceAndRepo(ctx, sqlc.GetRepoByNamespaceAndRepoParams{
		Namespace: input.namespace,
		Repo:      input.repo,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSelector{}, &SelectorResolutionError{
				Code:     SelectorResolutionNotFound,
				RepoPath: input.repoPath,
				Selector: input.selector,
			}
		}
		return ResolvedReadSelector{}, fmt.Errorf("load repo %q: %w", input.repoPath, err)
	}

	switch input.kind {
	case SelectorKindSHA:
		return resolveReadSelectorBySHA(ctx, queries, repo, input)
	case SelectorKindNoSelector:
		return resolveReadSelectorByBranch(ctx, queries, repo, input, repo.DefaultBranch)
	default:
		return ResolvedReadSelector{}, &SelectorResolutionError{
			Code:     SelectorResolutionInvalidInput,
			RepoPath: input.repoPath,
			Selector: input.selector,
			Err:      fmt.Errorf("unsupported selector kind %q", input.kind),
		}
	}
}

func resolveReadSelectorBySHA(
	ctx context.Context,
	queries selectorResolutionQueries,
	repo sqlc.Repo,
	input normalizedResolveReadSelectorInput,
) (ResolvedReadSelector, error) {
	revisionRow, err := queries.GetRevisionStateByRepoSHAPrefix(ctx, sqlc.GetRevisionStateByRepoSHAPrefixParams{
		RepoID: repo.ID,
		ShaPrefix: pgtype.Text{
			String: input.selector,
			Valid:  true,
		},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSelector{}, &SelectorResolutionError{
				Code:     SelectorResolutionNotFound,
				RepoPath: input.repoPath,
				Selector: input.selector,
			}
		}
		return ResolvedReadSelector{}, fmt.Errorf(
			"load revision by sha %q for repo %q: %w",
			input.selector,
			input.repoPath,
			err,
		)
	}

	revision := mapRevisionStateBySHAPrefixRow(revisionRow)
	if revision.Status != revisionProcessed {
		return ResolvedReadSelector{}, &SelectorResolutionError{
			Code:              SelectorResolutionUnprocessed,
			RepoPath:          input.repoPath,
			Selector:          input.selector,
			IngestEventID:     revision.ID,
			IngestEventStatus: revision.Status,
		}
	}

	if revision.OpenAPIChanged == nil || !*revision.OpenAPIChanged {
		return ResolvedReadSelector{}, &SelectorResolutionError{
			Code:     SelectorResolutionNotFound,
			RepoPath: input.repoPath,
			Selector: input.selector,
		}
	}

	return ResolvedReadSelector{
		RepoID:       repo.ID,
		RepoPath:     repoid.Identity{Namespace: repo.Namespace, Repo: repo.Repo}.Path(),
		SelectorKind: input.kind,
		Selector:     input.selector,
		Revision:     revision,
	}, nil
}

func resolveReadSelectorByBranch(
	ctx context.Context,
	queries selectorResolutionQueries,
	repo sqlc.Repo,
	input normalizedResolveReadSelectorInput,
	branch string,
) (ResolvedReadSelector, error) {
	headRevisionRow, err := queries.GetLatestRevisionStateByBranch(ctx, sqlc.GetLatestRevisionStateByBranchParams{
		RepoID: repo.ID,
		Branch: branch,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSelector{}, &SelectorResolutionError{
				Code:     SelectorResolutionNotFound,
				RepoPath: input.repoPath,
				Selector: input.selector,
			}
		}
		return ResolvedReadSelector{}, fmt.Errorf(
			"load latest revision for repo %q branch %q: %w",
			input.repoPath,
			branch,
			err,
		)
	}

	headRevision := mapRevisionStateByLatestBranchRow(headRevisionRow)
	if headRevision.Status != revisionProcessed {
		return ResolvedReadSelector{}, &SelectorResolutionError{
			Code:              SelectorResolutionUnprocessed,
			RepoPath:          input.repoPath,
			Selector:          input.selector,
			IngestEventID:     headRevision.ID,
			IngestEventStatus: headRevision.Status,
		}
	}

	revisionRow, err := queries.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingID(
		ctx,
		sqlc.GetLatestProcessedOpenAPIRevisionStateByBranchExcludingIDParams{
			RepoID:            repo.ID,
			Branch:            branch,
			ExcludeRevisionID: 0,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSelector{}, &SelectorResolutionError{
				Code:     SelectorResolutionNotFound,
				RepoPath: input.repoPath,
				Selector: input.selector,
			}
		}
		return ResolvedReadSelector{}, fmt.Errorf(
			"load latest processed openapi revision for repo %q branch %q: %w",
			input.repoPath,
			branch,
			err,
		)
	}

	revision := mapRevisionStateByLatestProcessedOpenAPIBranchRow(revisionRow)
	return ResolvedReadSelector{
		RepoID:       repo.ID,
		RepoPath:     repoid.Identity{Namespace: repo.Namespace, Repo: repo.Repo}.Path(),
		SelectorKind: input.kind,
		Selector:     input.selector,
		Revision:     revision,
	}, nil
}

func normalizeResolveReadSelectorInput(input ResolveReadSelectorInput) (normalizedResolveReadSelectorInput, error) {
	normalized := normalizedResolveReadSelectorInput{
		namespace: strings.TrimSpace(input.Namespace),
		repo:      strings.TrimSpace(input.Repo),
		selector:  strings.TrimSpace(input.Selector),
	}
	normalized.repoPath = repoid.Identity{Namespace: normalized.namespace, Repo: normalized.repo}.Path()

	if normalized.namespace == "" {
		return normalizedResolveReadSelectorInput{}, &SelectorResolutionError{
			Code: SelectorResolutionInvalidInput,
			Err:  errors.New("namespace must not be empty"),
		}
	}

	if normalized.repo == "" {
		return normalizedResolveReadSelectorInput{}, &SelectorResolutionError{
			Code: SelectorResolutionInvalidInput,
			Err:  errors.New("repo must not be empty"),
		}
	}

	if input.NoSelector {
		if normalized.selector != "" {
			return normalizedResolveReadSelectorInput{}, &SelectorResolutionError{
				Code:     SelectorResolutionInvalidInput,
				RepoPath: normalized.repoPath,
				Selector: normalized.selector,
				Err:      errors.New("selector must be empty when no selector route is used"),
			}
		}
		normalized.kind = SelectorKindNoSelector
		return normalized, nil
	}

	if normalized.selector == "" {
		return normalizedResolveReadSelectorInput{}, &SelectorResolutionError{
			Code:     SelectorResolutionInvalidInput,
			RepoPath: normalized.repoPath,
			Err:      errors.New("selector must not be empty"),
		}
	}

	if !isShortSHA(normalized.selector) {
		return normalizedResolveReadSelectorInput{}, &SelectorResolutionError{
			Code:     SelectorResolutionInvalidInput,
			RepoPath: normalized.repoPath,
			Selector: normalized.selector,
			Err:      errors.New("selector must be exactly 8 lowercase hex characters"),
		}
	}

	normalized.kind = SelectorKindSHA
	return normalized, nil
}

func isShortSHA(value string) bool {
	if len(value) != shortSHASelectorLength {
		return false
	}
	for _, character := range value {
		switch {
		case character >= '0' && character <= '9':
			continue
		case character >= 'a' && character <= 'f':
			continue
		default:
			return false
		}
	}
	return true
}
