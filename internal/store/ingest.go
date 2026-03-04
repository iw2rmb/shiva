package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var ErrStoreNotConfigured = errors.New("store is not configured")
var ErrInvalidIngestInput = errors.New("invalid ingest input")

type GitLabIngestInput struct {
	TenantKey         string
	GitLabProjectID   int64
	PathWithNamespace string
	DefaultBranch     string
	Sha               string
	Branch            string
	ParentSha         string
	EventType         string
	DeliveryID        string
	PayloadJSON       []byte
}

type GitLabIngestResult struct {
	EventID   int64
	RepoID    int64
	Duplicate bool
}

func (s *Store) PersistGitLabWebhook(ctx context.Context, input GitLabIngestInput) (GitLabIngestResult, error) {
	if s == nil || !s.configured || s.pool == nil {
		return GitLabIngestResult{}, ErrStoreNotConfigured
	}

	normalized, err := normalizeGitLabIngestInput(input)
	if err != nil {
		return GitLabIngestResult{}, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return GitLabIngestResult{}, fmt.Errorf("begin ingest transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	queries := sqlc.New(tx)

	tenant, err := ensureTenant(ctx, queries, normalized.TenantKey)
	if err != nil {
		return GitLabIngestResult{}, err
	}

	repo, err := ensureRepo(ctx, queries, tenant.ID, normalized)
	if err != nil {
		return GitLabIngestResult{}, err
	}

	event, err := queries.CreateIngestEvent(ctx, sqlc.CreateIngestEventParams{
		TenantID:    tenant.ID,
		RepoID:      repo.ID,
		Sha:         normalized.Sha,
		Branch:      normalized.Branch,
		ParentSha:   nullableText(normalized.ParentSha),
		EventType:   normalized.EventType,
		DeliveryID:  normalized.DeliveryID,
		PayloadJson: normalized.PayloadJSON,
	})
	if err != nil {
		if isUniqueViolation(err) {
			existing, getErr := loadDuplicateIngestEvent(ctx, queries, repo.ID, normalized.DeliveryID, normalized.Sha)
			if getErr != nil {
				return GitLabIngestResult{}, fmt.Errorf("load duplicate ingest event: %w", getErr)
			}
			if err := tx.Commit(ctx); err != nil {
				return GitLabIngestResult{}, fmt.Errorf("commit duplicate ingest transaction: %w", err)
			}
			return GitLabIngestResult{EventID: existing.ID, RepoID: existing.RepoID, Duplicate: true}, nil
		}
		return GitLabIngestResult{}, fmt.Errorf("create ingest event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return GitLabIngestResult{}, fmt.Errorf("commit ingest transaction: %w", err)
	}

	return GitLabIngestResult{EventID: event.ID, RepoID: event.RepoID, Duplicate: false}, nil
}

func normalizeGitLabIngestInput(input GitLabIngestInput) (GitLabIngestInput, error) {
	normalized := input
	normalized.TenantKey = strings.TrimSpace(input.TenantKey)
	normalized.PathWithNamespace = strings.TrimSpace(input.PathWithNamespace)
	normalized.DefaultBranch = strings.TrimSpace(input.DefaultBranch)
	normalized.Sha = strings.TrimSpace(input.Sha)
	normalized.Branch = strings.TrimSpace(input.Branch)
	normalized.ParentSha = strings.TrimSpace(input.ParentSha)
	normalized.EventType = strings.TrimSpace(input.EventType)
	normalized.DeliveryID = strings.TrimSpace(input.DeliveryID)

	switch {
	case normalized.TenantKey == "":
		return GitLabIngestInput{}, fmt.Errorf("%w: tenant key is required", ErrInvalidIngestInput)
	case normalized.GitLabProjectID <= 0:
		return GitLabIngestInput{}, fmt.Errorf("%w: gitlab project id must be positive", ErrInvalidIngestInput)
	case normalized.PathWithNamespace == "":
		return GitLabIngestInput{}, fmt.Errorf("%w: path_with_namespace is required", ErrInvalidIngestInput)
	case normalized.DefaultBranch == "":
		return GitLabIngestInput{}, fmt.Errorf("%w: default branch is required", ErrInvalidIngestInput)
	case normalized.Sha == "":
		return GitLabIngestInput{}, fmt.Errorf("%w: sha is required", ErrInvalidIngestInput)
	case normalized.Branch == "":
		return GitLabIngestInput{}, fmt.Errorf("%w: branch is required", ErrInvalidIngestInput)
	case normalized.EventType == "":
		return GitLabIngestInput{}, fmt.Errorf("%w: event type is required", ErrInvalidIngestInput)
	case normalized.DeliveryID == "":
		return GitLabIngestInput{}, fmt.Errorf("%w: delivery id is required", ErrInvalidIngestInput)
	case len(normalized.PayloadJSON) == 0:
		return GitLabIngestInput{}, fmt.Errorf("%w: payload is required", ErrInvalidIngestInput)
	}

	return normalized, nil
}

func ensureTenant(ctx context.Context, queries *sqlc.Queries, tenantKey string) (sqlc.Tenant, error) {
	tenant, err := queries.GetTenantByKey(ctx, tenantKey)
	if err == nil {
		return tenant, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Tenant{}, fmt.Errorf("load tenant %q: %w", tenantKey, err)
	}

	tenant, err = queries.CreateTenant(ctx, tenantKey)
	if err == nil {
		return tenant, nil
	}
	if !isUniqueViolation(err) {
		return sqlc.Tenant{}, fmt.Errorf("create tenant %q: %w", tenantKey, err)
	}

	tenant, err = queries.GetTenantByKey(ctx, tenantKey)
	if err != nil {
		return sqlc.Tenant{}, fmt.Errorf("load tenant %q after conflict: %w", tenantKey, err)
	}
	return tenant, nil
}

func ensureRepo(ctx context.Context, queries *sqlc.Queries, tenantID int64, input GitLabIngestInput) (sqlc.Repo, error) {
	repo, err := queries.GetRepoByTenantAndProjectID(ctx, sqlc.GetRepoByTenantAndProjectIDParams{
		TenantID:        tenantID,
		GitlabProjectID: input.GitLabProjectID,
	})
	if err == nil {
		if repo.DefaultBranch != input.DefaultBranch {
			repo, err = queries.UpdateRepoDefaultBranch(ctx, sqlc.UpdateRepoDefaultBranchParams{
				DefaultBranch: input.DefaultBranch,
				ID:            repo.ID,
			})
			if err != nil {
				return sqlc.Repo{}, fmt.Errorf("update default branch for repo %d: %w", repo.ID, err)
			}
		}
		return repo, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Repo{}, fmt.Errorf("load repo for tenant %d project %d: %w", tenantID, input.GitLabProjectID, err)
	}

	repo, err = queries.CreateRepo(ctx, sqlc.CreateRepoParams{
		TenantID:          tenantID,
		GitlabProjectID:   input.GitLabProjectID,
		PathWithNamespace: input.PathWithNamespace,
		DefaultBranch:     input.DefaultBranch,
	})
	if err == nil {
		return repo, nil
	}
	if !isUniqueViolation(err) {
		return sqlc.Repo{}, fmt.Errorf("create repo for tenant %d project %d: %w", tenantID, input.GitLabProjectID, err)
	}

	repo, err = queries.GetRepoByTenantAndProjectID(ctx, sqlc.GetRepoByTenantAndProjectIDParams{
		TenantID:        tenantID,
		GitlabProjectID: input.GitLabProjectID,
	})
	if err != nil {
		return sqlc.Repo{}, fmt.Errorf("load repo for tenant %d project %d after conflict: %w", tenantID, input.GitLabProjectID, err)
	}

	if repo.DefaultBranch != input.DefaultBranch {
		repo, err = queries.UpdateRepoDefaultBranch(ctx, sqlc.UpdateRepoDefaultBranchParams{
			DefaultBranch: input.DefaultBranch,
			ID:            repo.ID,
		})
		if err != nil {
			return sqlc.Repo{}, fmt.Errorf("update default branch for repo %d after conflict: %w", repo.ID, err)
		}
	}

	return repo, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func nullableText(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func loadDuplicateIngestEvent(
	ctx context.Context,
	queries *sqlc.Queries,
	repoID int64,
	deliveryID string,
	sha string,
) (sqlc.IngestEvent, error) {
	existing, err := queries.GetIngestEventByRepoDelivery(ctx, sqlc.GetIngestEventByRepoDeliveryParams{
		RepoID:     repoID,
		DeliveryID: deliveryID,
	})
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return sqlc.IngestEvent{}, err
	}

	existing, err = queries.GetIngestEventByRepoSHA(ctx, sqlc.GetIngestEventByRepoSHAParams{
		RepoID: repoID,
		Sha:    sha,
	})
	if err != nil {
		return sqlc.IngestEvent{}, err
	}
	return existing, nil
}
