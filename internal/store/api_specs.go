package store

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
)

var ErrAPISpecNotFound = errors.New("api spec not found")

type APISpec struct {
	ID          int64
	RepoID      int64
	RootPath    string
	Status      string
	DisplayName string
}

type APISpecRevision struct {
	ID                 int64
	APISpecID          int64
	RevisionID         int64
	RootPathAtRevision string
	BuildStatus        string
	Error              string
}

type ActiveAPISpecWithLatestDependencies struct {
	APISpec
	DependencyFilePaths []string
}

type UpsertAPISpecInput struct {
	RepoID   int64
	RootPath string
}

type CreateAPISpecRevisionInput struct {
	APISpecID   int64
	RevisionID  int64
	BuildStatus string
	Error       string
}

type ReplaceAPISpecDependenciesInput struct {
	APISpecRevisionID int64
	FilePaths         []string
}

type normalizedUpsertAPISpecInput struct {
	RepoID   int64
	RootPath string
}

type normalizedCreateAPISpecRevisionInput struct {
	APISpecID   int64
	RevisionID  int64
	BuildStatus string
	Error       string
}

type normalizedReplaceAPISpecDependenciesInput struct {
	APISpecRevisionID int64
	FilePaths         []string
}

func (s *Store) CountActiveAPISpecsByRepo(ctx context.Context, repoID int64) (int64, error) {
	if s == nil || !s.configured || s.pool == nil {
		return 0, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return 0, errors.New("repo id must be positive")
	}

	return countActiveAPISpecsByRepo(ctx, sqlc.New(s.pool), repoID)
}

func (s *Store) ListActiveAPISpecsWithLatestDependencies(
	ctx context.Context,
	repoID int64,
) ([]ActiveAPISpecWithLatestDependencies, error) {
	if s == nil || !s.configured || s.pool == nil {
		return nil, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return nil, errors.New("repo id must be positive")
	}

	return listActiveAPISpecsWithLatestDependencies(ctx, sqlc.New(s.pool), repoID)
}

func (s *Store) UpsertAPISpec(ctx context.Context, input UpsertAPISpecInput) (APISpec, error) {
	if s == nil || !s.configured || s.pool == nil {
		return APISpec{}, ErrStoreNotConfigured
	}

	normalized, err := normalizeUpsertAPISpecInput(input)
	if err != nil {
		return APISpec{}, err
	}

	return upsertAPISpec(ctx, sqlc.New(s.pool), normalized)
}

func (s *Store) CreateAPISpecRevision(ctx context.Context, input CreateAPISpecRevisionInput) (APISpecRevision, error) {
	if s == nil || !s.configured || s.pool == nil {
		return APISpecRevision{}, ErrStoreNotConfigured
	}

	normalized, err := normalizeCreateAPISpecRevisionInput(input)
	if err != nil {
		return APISpecRevision{}, err
	}

	return createAPISpecRevision(ctx, sqlc.New(s.pool), normalized)
}

func (s *Store) ReplaceAPISpecDependencies(ctx context.Context, input ReplaceAPISpecDependenciesInput) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}

	normalized, err := normalizeReplaceAPISpecDependenciesInput(input)
	if err != nil {
		return err
	}

	return replaceAPISpecDependencies(ctx, sqlc.New(s.pool), normalized)
}

func (s *Store) MarkAPISpecDeleted(ctx context.Context, apiSpecID int64) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}
	if apiSpecID < 1 {
		return errors.New("api spec id must be positive")
	}

	return markAPISpecDeleted(ctx, sqlc.New(s.pool), apiSpecID)
}

type apiSpecCountQueries interface {
	CountActiveAPISpecsByRepo(ctx context.Context, repoID int64) (int64, error)
}

func countActiveAPISpecsByRepo(ctx context.Context, queries apiSpecCountQueries, repoID int64) (int64, error) {
	count, err := queries.CountActiveAPISpecsByRepo(ctx, repoID)
	if err != nil {
		return 0, fmt.Errorf("count active api specs for repo %d: %w", repoID, err)
	}
	return count, nil
}

type apiSpecLatestDependencyQueries interface {
	ListActiveAPISpecsWithLatestDependencies(
		ctx context.Context,
		repoID int64,
	) ([]sqlc.ListActiveAPISpecsWithLatestDependenciesRow, error)
}

func listActiveAPISpecsWithLatestDependencies(
	ctx context.Context,
	queries apiSpecLatestDependencyQueries,
	repoID int64,
) ([]ActiveAPISpecWithLatestDependencies, error) {
	rows, err := queries.ListActiveAPISpecsWithLatestDependencies(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("list active api specs with latest dependencies for repo %d: %w", repoID, err)
	}

	result := make([]ActiveAPISpecWithLatestDependencies, 0, len(rows))
	for _, row := range rows {
		dependencyPaths := make([]string, len(row.DependencyPaths))
		copy(dependencyPaths, row.DependencyPaths)

		result = append(result, ActiveAPISpecWithLatestDependencies{
			APISpec: mapAPISpec(sqlc.ApiSpec{
				ID:          row.ID,
				RepoID:      row.RepoID,
				RootPath:    row.RootPath,
				Status:      row.Status,
				DisplayName: row.DisplayName,
				CreatedAt:   row.CreatedAt,
				UpdatedAt:   row.UpdatedAt,
			}),
			DependencyFilePaths: dependencyPaths,
		})
	}

	return result, nil
}

type apiSpecUpsertQueries interface {
	UpsertAPISpec(ctx context.Context, arg sqlc.UpsertAPISpecParams) (sqlc.ApiSpec, error)
}

func upsertAPISpec(
	ctx context.Context,
	queries apiSpecUpsertQueries,
	input normalizedUpsertAPISpecInput,
) (APISpec, error) {
	row, err := queries.UpsertAPISpec(ctx, sqlc.UpsertAPISpecParams{
		RepoID:   input.RepoID,
		RootPath: input.RootPath,
	})
	if err != nil {
		return APISpec{}, fmt.Errorf(
			"upsert api spec repo_id=%d root_path=%q: %w",
			input.RepoID,
			input.RootPath,
			err,
		)
	}

	return mapAPISpec(row), nil
}

type apiSpecRevisionQueries interface {
	CreateAPISpecRevision(
		ctx context.Context,
		arg sqlc.CreateAPISpecRevisionParams,
	) (sqlc.ApiSpecRevision, error)
}

func createAPISpecRevision(
	ctx context.Context,
	queries apiSpecRevisionQueries,
	input normalizedCreateAPISpecRevisionInput,
) (APISpecRevision, error) {
	row, err := queries.CreateAPISpecRevision(ctx, sqlc.CreateAPISpecRevisionParams{
		ApiSpecID:   input.APISpecID,
		RevisionID:  input.RevisionID,
		BuildStatus: input.BuildStatus,
		Error:       input.Error,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APISpecRevision{}, fmt.Errorf("%w: id=%d", ErrAPISpecNotFound, input.APISpecID)
		}
		return APISpecRevision{}, fmt.Errorf(
			"create api spec revision api_spec_id=%d revision_id=%d: %w",
			input.APISpecID,
			input.RevisionID,
			err,
		)
	}

	return mapAPISpecRevision(row), nil
}

type apiSpecDependencyQueries interface {
	ReplaceAPISpecDependencies(ctx context.Context, arg sqlc.ReplaceAPISpecDependenciesParams) error
}

func replaceAPISpecDependencies(
	ctx context.Context,
	queries apiSpecDependencyQueries,
	input normalizedReplaceAPISpecDependenciesInput,
) error {
	if err := queries.ReplaceAPISpecDependencies(ctx, sqlc.ReplaceAPISpecDependenciesParams{
		ApiSpecRevisionID: input.APISpecRevisionID,
		FilePaths:         input.FilePaths,
	}); err != nil {
		return fmt.Errorf("replace api spec dependencies api_spec_revision_id=%d: %w", input.APISpecRevisionID, err)
	}
	return nil
}

type apiSpecDeleteQueries interface {
	MarkAPISpecDeleted(ctx context.Context, apiSpecID int64) (int64, error)
}

func markAPISpecDeleted(ctx context.Context, queries apiSpecDeleteQueries, apiSpecID int64) error {
	rows, err := queries.MarkAPISpecDeleted(ctx, apiSpecID)
	if err != nil {
		return fmt.Errorf("mark api spec %d deleted: %w", apiSpecID, err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: id=%d", ErrAPISpecNotFound, apiSpecID)
	}
	return nil
}

func normalizeUpsertAPISpecInput(input UpsertAPISpecInput) (normalizedUpsertAPISpecInput, error) {
	if input.RepoID < 1 {
		return normalizedUpsertAPISpecInput{}, errors.New("repo id must be positive")
	}

	rootPath, err := normalizeRepoRelativePath("root path", input.RootPath)
	if err != nil {
		return normalizedUpsertAPISpecInput{}, err
	}

	return normalizedUpsertAPISpecInput{
		RepoID:   input.RepoID,
		RootPath: rootPath,
	}, nil
}

func normalizeCreateAPISpecRevisionInput(
	input CreateAPISpecRevisionInput,
) (normalizedCreateAPISpecRevisionInput, error) {
	if input.APISpecID < 1 {
		return normalizedCreateAPISpecRevisionInput{}, errors.New("api spec id must be positive")
	}
	if input.RevisionID < 1 {
		return normalizedCreateAPISpecRevisionInput{}, errors.New("revision id must be positive")
	}

	buildStatus := strings.TrimSpace(input.BuildStatus)
	if buildStatus == "" {
		return normalizedCreateAPISpecRevisionInput{}, errors.New("build status must not be empty")
	}

	return normalizedCreateAPISpecRevisionInput{
		APISpecID:   input.APISpecID,
		RevisionID:  input.RevisionID,
		BuildStatus: buildStatus,
		Error:       strings.TrimSpace(input.Error),
	}, nil
}

func normalizeReplaceAPISpecDependenciesInput(
	input ReplaceAPISpecDependenciesInput,
) (normalizedReplaceAPISpecDependenciesInput, error) {
	if input.APISpecRevisionID < 1 {
		return normalizedReplaceAPISpecDependenciesInput{}, errors.New("api spec revision id must be positive")
	}

	normalizedPaths := make([]string, 0, len(input.FilePaths))
	seen := make(map[string]struct{}, len(input.FilePaths))
	for i, rawPath := range input.FilePaths {
		normalizedPath, err := normalizeRepoRelativePath(fmt.Sprintf("file_paths[%d]", i), rawPath)
		if err != nil {
			return normalizedReplaceAPISpecDependenciesInput{}, err
		}
		if _, exists := seen[normalizedPath]; exists {
			continue
		}
		seen[normalizedPath] = struct{}{}
		normalizedPaths = append(normalizedPaths, normalizedPath)
	}

	sort.Strings(normalizedPaths)

	return normalizedReplaceAPISpecDependenciesInput{
		APISpecRevisionID: input.APISpecRevisionID,
		FilePaths:         normalizedPaths,
	}, nil
}

func normalizeRepoRelativePath(fieldName string, rawPath string) (string, error) {
	normalizedSeparators := strings.ReplaceAll(rawPath, "\\", "/")
	trimmed := strings.TrimSpace(strings.TrimPrefix(normalizedSeparators, "/"))
	if trimmed == "" {
		return "", fmt.Errorf("%s must not be empty", fieldName)
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%s must be a repo-relative path", fieldName)
	}

	return cleaned, nil
}

func mapAPISpec(row sqlc.ApiSpec) APISpec {
	mapped := APISpec{
		ID:       row.ID,
		RepoID:   row.RepoID,
		RootPath: row.RootPath,
		Status:   row.Status,
	}
	if row.DisplayName.Valid {
		mapped.DisplayName = row.DisplayName.String
	}
	return mapped
}

func mapAPISpecRevision(row sqlc.ApiSpecRevision) APISpecRevision {
	return APISpecRevision{
		ID:                 row.ID,
		APISpecID:          row.ApiSpecID,
		RevisionID:         row.RevisionID,
		RootPathAtRevision: row.RootPathAtRevision,
		BuildStatus:        row.BuildStatus,
		Error:              row.Error,
	}
}
