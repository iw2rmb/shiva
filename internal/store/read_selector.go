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

const (
	shortSHASelectorLength = 8
	mainBranchName         = "main"
	revisionProcessed      = "processed"
)

type SelectorKind string

const (
	SelectorKindSHA        SelectorKind = "sha"
	SelectorKindNoSelector SelectorKind = "no_selector"
)

type ResolveReadSelectorInput struct {
	TenantKey  string
	RepoPath   string
	Selector   string
	NoSelector bool
}

type ResolvedReadSelector struct {
	TenantID int64
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

	TenantKey string
	RepoPath  string
	Selector  string

	RevisionID     int64
	RevisionStatus string

	Err error
}

func (e *SelectorResolutionError) Error() string {
	if e == nil {
		return "selector resolution error"
	}

	message := fmt.Sprintf("selector resolution failed: code=%s", e.Code)
	if e.TenantKey != "" {
		message += fmt.Sprintf(" tenant=%q", e.TenantKey)
	}
	if e.RepoPath != "" {
		message += fmt.Sprintf(" repo=%q", e.RepoPath)
	}
	if e.Selector != "" {
		message += fmt.Sprintf(" selector=%q", e.Selector)
	}
	if e.RevisionID > 0 {
		message += fmt.Sprintf(" revision_id=%d", e.RevisionID)
	}
	if e.RevisionStatus != "" {
		message += fmt.Sprintf(" status=%q", e.RevisionStatus)
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
	tenantKey string
	repoPath  string
	selector  string
	kind      SelectorKind
}

type selectorResolutionQueries interface {
	GetTenantByKey(ctx context.Context, key string) (sqlc.Tenant, error)
	GetRepoByTenantAndPath(ctx context.Context, arg sqlc.GetRepoByTenantAndPathParams) (sqlc.Repo, error)
	GetRevisionByRepoSHAPrefix(ctx context.Context, arg sqlc.GetRevisionByRepoSHAPrefixParams) (sqlc.Revision, error)
	GetLatestRevisionByBranch(ctx context.Context, arg sqlc.GetLatestRevisionByBranchParams) (sqlc.Revision, error)
	GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
		ctx context.Context,
		arg sqlc.GetLatestProcessedOpenAPIRevisionByBranchExcludingIDParams,
	) (sqlc.Revision, error)
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
	tenant, err := queries.GetTenantByKey(ctx, input.tenantKey)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSelector{}, &SelectorResolutionError{
				Code:      SelectorResolutionNotFound,
				TenantKey: input.tenantKey,
				RepoPath:  input.repoPath,
				Selector:  input.selector,
			}
		}
		return ResolvedReadSelector{}, fmt.Errorf("load tenant %q: %w", input.tenantKey, err)
	}

	repo, err := queries.GetRepoByTenantAndPath(ctx, sqlc.GetRepoByTenantAndPathParams{
		TenantID:          tenant.ID,
		PathWithNamespace: input.repoPath,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSelector{}, &SelectorResolutionError{
				Code:      SelectorResolutionNotFound,
				TenantKey: input.tenantKey,
				RepoPath:  input.repoPath,
				Selector:  input.selector,
			}
		}
		return ResolvedReadSelector{}, fmt.Errorf("load repo %q for tenant %q: %w", input.repoPath, input.tenantKey, err)
	}

	switch input.kind {
	case SelectorKindSHA:
		return resolveReadSelectorBySHA(ctx, queries, tenant.ID, repo, input)
	case SelectorKindNoSelector:
		return resolveReadSelectorByBranch(ctx, queries, tenant.ID, repo, input, mainBranchName)
	default:
		return ResolvedReadSelector{}, &SelectorResolutionError{
			Code:      SelectorResolutionInvalidInput,
			TenantKey: input.tenantKey,
			RepoPath:  input.repoPath,
			Selector:  input.selector,
			Err:       fmt.Errorf("unsupported selector kind %q", input.kind),
		}
	}
}

func resolveReadSelectorBySHA(
	ctx context.Context,
	queries selectorResolutionQueries,
	tenantID int64,
	repo sqlc.Repo,
	input normalizedResolveReadSelectorInput,
) (ResolvedReadSelector, error) {
	revision, err := queries.GetRevisionByRepoSHAPrefix(ctx, sqlc.GetRevisionByRepoSHAPrefixParams{
		RepoID: repo.ID,
		ShaPrefix: pgtype.Text{
			String: input.selector,
			Valid:  true,
		},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSelector{}, &SelectorResolutionError{
				Code:      SelectorResolutionNotFound,
				TenantKey: input.tenantKey,
				RepoPath:  input.repoPath,
				Selector:  input.selector,
			}
		}
		return ResolvedReadSelector{}, fmt.Errorf(
			"load revision by sha %q for repo %q tenant %q: %w",
			input.selector,
			input.repoPath,
			input.tenantKey,
			err,
		)
	}

	if revision.Status != revisionProcessed {
		return ResolvedReadSelector{}, &SelectorResolutionError{
			Code:           SelectorResolutionUnprocessed,
			TenantKey:      input.tenantKey,
			RepoPath:       input.repoPath,
			Selector:       input.selector,
			RevisionID:     revision.ID,
			RevisionStatus: revision.Status,
		}
	}

	if !revision.OpenapiChanged.Valid || !revision.OpenapiChanged.Bool {
		return ResolvedReadSelector{}, &SelectorResolutionError{
			Code:      SelectorResolutionNotFound,
			TenantKey: input.tenantKey,
			RepoPath:  input.repoPath,
			Selector:  input.selector,
		}
	}

	return ResolvedReadSelector{
		TenantID:     tenantID,
		RepoID:       repo.ID,
		RepoPath:     repo.PathWithNamespace,
		SelectorKind: input.kind,
		Selector:     input.selector,
		Revision:     mapRevision(revision),
	}, nil
}

func resolveReadSelectorByBranch(
	ctx context.Context,
	queries selectorResolutionQueries,
	tenantID int64,
	repo sqlc.Repo,
	input normalizedResolveReadSelectorInput,
	branch string,
) (ResolvedReadSelector, error) {
	headRevision, err := queries.GetLatestRevisionByBranch(ctx, sqlc.GetLatestRevisionByBranchParams{
		RepoID: repo.ID,
		Branch: branch,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSelector{}, &SelectorResolutionError{
				Code:      SelectorResolutionNotFound,
				TenantKey: input.tenantKey,
				RepoPath:  input.repoPath,
				Selector:  input.selector,
			}
		}
		return ResolvedReadSelector{}, fmt.Errorf(
			"load latest revision for repo %q tenant %q branch %q: %w",
			input.repoPath,
			input.tenantKey,
			branch,
			err,
		)
	}

	if headRevision.Status != revisionProcessed {
		return ResolvedReadSelector{}, &SelectorResolutionError{
			Code:           SelectorResolutionUnprocessed,
			TenantKey:      input.tenantKey,
			RepoPath:       input.repoPath,
			Selector:       input.selector,
			RevisionID:     headRevision.ID,
			RevisionStatus: headRevision.Status,
		}
	}

	revision, err := queries.GetLatestProcessedOpenAPIRevisionByBranchExcludingID(
		ctx,
		sqlc.GetLatestProcessedOpenAPIRevisionByBranchExcludingIDParams{
			RepoID:            repo.ID,
			Branch:            branch,
			ExcludeRevisionID: 0,
		},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedReadSelector{}, &SelectorResolutionError{
				Code:      SelectorResolutionNotFound,
				TenantKey: input.tenantKey,
				RepoPath:  input.repoPath,
				Selector:  input.selector,
			}
		}
		return ResolvedReadSelector{}, fmt.Errorf(
			"load latest processed openapi revision for repo %q tenant %q branch %q: %w",
			input.repoPath,
			input.tenantKey,
			branch,
			err,
		)
	}

	return ResolvedReadSelector{
		TenantID:     tenantID,
		RepoID:       repo.ID,
		RepoPath:     repo.PathWithNamespace,
		SelectorKind: input.kind,
		Selector:     input.selector,
		Revision:     mapRevision(revision),
	}, nil
}

func normalizeResolveReadSelectorInput(input ResolveReadSelectorInput) (normalizedResolveReadSelectorInput, error) {
	normalized := normalizedResolveReadSelectorInput{
		tenantKey: strings.TrimSpace(input.TenantKey),
		repoPath:  strings.TrimSpace(input.RepoPath),
		selector:  strings.TrimSpace(input.Selector),
	}

	if normalized.tenantKey == "" {
		return normalizedResolveReadSelectorInput{}, &SelectorResolutionError{
			Code: SelectorResolutionInvalidInput,
			Err:  errors.New("tenant key must not be empty"),
		}
	}
	if normalized.repoPath == "" {
		return normalizedResolveReadSelectorInput{}, &SelectorResolutionError{
			Code:      SelectorResolutionInvalidInput,
			TenantKey: normalized.tenantKey,
			Err:       errors.New("repo path must not be empty"),
		}
	}

	if input.NoSelector {
		if normalized.selector != "" {
			return normalizedResolveReadSelectorInput{}, &SelectorResolutionError{
				Code:      SelectorResolutionInvalidInput,
				TenantKey: normalized.tenantKey,
				RepoPath:  normalized.repoPath,
				Selector:  normalized.selector,
				Err:       errors.New("selector must be empty when no selector route is used"),
			}
		}
		normalized.kind = SelectorKindNoSelector
		return normalized, nil
	}

	if normalized.selector == "" {
		return normalizedResolveReadSelectorInput{}, &SelectorResolutionError{
			Code:      SelectorResolutionInvalidInput,
			TenantKey: normalized.tenantKey,
			RepoPath:  normalized.repoPath,
			Err:       errors.New("selector must not be empty"),
		}
	}

	if !isShortSHA(normalized.selector) {
		return normalizedResolveReadSelectorInput{}, &SelectorResolutionError{
			Code:      SelectorResolutionInvalidInput,
			TenantKey: normalized.tenantKey,
			RepoPath:  normalized.repoPath,
			Selector:  normalized.selector,
			Err:       errors.New("selector must be exactly 8 lowercase hex characters"),
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
