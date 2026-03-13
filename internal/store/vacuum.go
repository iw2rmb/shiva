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

const (
	VacuumStatusPending    = "pending"
	VacuumStatusProcessing = "processing"
	VacuumStatusProcessed  = "processed"
	VacuumStatusFailed     = "failed"
)

type VacuumRule struct {
	RuleID       string
	Severity     string
	Type         string
	CategoryID   string
	CategoryName string
	Description  string
	HowToFix     string
	GivenPath    string
	RuleJSON     []byte
}

type VacuumIssue struct {
	ID                int64
	APISpecRevisionID int64
	RuleID            string
	Message           string
	JSONPath          string
	RangePos          []int32
	CreatedAt         time.Time
}

type VacuumIssueMutation struct {
	RuleID   string
	Message  string
	JSONPath string
	RangePos []int32
}

type CreateVacuumIssueInput struct {
	APISpecRevisionID int64
	Issue             VacuumIssueMutation
}

type ReplaceVacuumIssuesInput struct {
	APISpecRevisionID int64
	Issues            []VacuumIssueMutation
}

type UpdateAPISpecRevisionVacuumStateInput struct {
	APISpecRevisionID int64
	VacuumStatus      string
	VacuumError       string
	VacuumValidatedAt *time.Time
}

type normalizedVacuumIssueMutation struct {
	RuleID   string
	Message  string
	JSONPath string
	RangePos []int32
}

type normalizedCreateVacuumIssueInput struct {
	APISpecRevisionID int64
	Issue             normalizedVacuumIssueMutation
}

type normalizedReplaceVacuumIssuesInput struct {
	APISpecRevisionID int64
	Issues            []normalizedVacuumIssueMutation
}

type normalizedUpdateAPISpecRevisionVacuumStateInput struct {
	APISpecRevisionID int64
	VacuumStatus      string
	VacuumError       string
	VacuumValidatedAt *time.Time
}

func (s *Store) ListVacuumRules(ctx context.Context) ([]VacuumRule, error) {
	if s == nil || !s.configured || s.pool == nil {
		return nil, ErrStoreNotConfigured
	}

	rows, err := sqlc.New(s.pool).ListVacuumRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vacuum rules: %w", err)
	}

	rules := make([]VacuumRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, mapVacuumRule(row))
	}

	return rules, nil
}

func (s *Store) CreateVacuumIssue(ctx context.Context, input CreateVacuumIssueInput) (VacuumIssue, error) {
	if s == nil || !s.configured || s.pool == nil {
		return VacuumIssue{}, ErrStoreNotConfigured
	}

	normalized, err := normalizeCreateVacuumIssueInput(input)
	if err != nil {
		return VacuumIssue{}, err
	}

	row, err := createVacuumIssue(ctx, sqlc.New(s.pool), normalized)
	if err != nil {
		return VacuumIssue{}, err
	}
	return mapVacuumIssue(row), nil
}

func (s *Store) ListVacuumIssuesByAPISpecRevisionID(ctx context.Context, apiSpecRevisionID int64) ([]VacuumIssue, error) {
	if s == nil || !s.configured || s.pool == nil {
		return nil, ErrStoreNotConfigured
	}
	if apiSpecRevisionID < 1 {
		return nil, errors.New("api spec revision id must be positive")
	}

	rows, err := sqlc.New(s.pool).ListVacuumIssuesByAPISpecRevisionID(ctx, apiSpecRevisionID)
	if err != nil {
		return nil, fmt.Errorf("list vacuum issues for api_spec_revision_id=%d: %w", apiSpecRevisionID, err)
	}

	issues := make([]VacuumIssue, 0, len(rows))
	for _, row := range rows {
		issues = append(issues, mapVacuumIssue(row))
	}

	return issues, nil
}

func (s *Store) DeleteVacuumIssuesByAPISpecRevisionID(ctx context.Context, apiSpecRevisionID int64) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}
	if apiSpecRevisionID < 1 {
		return errors.New("api spec revision id must be positive")
	}

	if err := deleteVacuumIssuesByAPISpecRevisionID(ctx, sqlc.New(s.pool), apiSpecRevisionID); err != nil {
		return err
	}
	return nil
}

func (s *Store) ReplaceVacuumIssues(ctx context.Context, input ReplaceVacuumIssuesInput) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}

	normalized, err := normalizeReplaceVacuumIssuesInput(input)
	if err != nil {
		return err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin vacuum issue replacement transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := replaceVacuumIssues(ctx, sqlc.New(tx), normalized); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit vacuum issue replacement transaction: %w", err)
	}
	return nil
}

func (s *Store) UpdateAPISpecRevisionVacuumState(
	ctx context.Context,
	input UpdateAPISpecRevisionVacuumStateInput,
) (APISpecRevision, error) {
	if s == nil || !s.configured || s.pool == nil {
		return APISpecRevision{}, ErrStoreNotConfigured
	}

	normalized, err := normalizeUpdateAPISpecRevisionVacuumStateInput(input)
	if err != nil {
		return APISpecRevision{}, err
	}

	row, err := updateAPISpecRevisionVacuumState(ctx, sqlc.New(s.pool), normalized)
	if err != nil {
		return APISpecRevision{}, err
	}

	return row, nil
}

type vacuumIssueCreateQueries interface {
	CreateVacuumIssue(ctx context.Context, arg sqlc.CreateVacuumIssueParams) (sqlc.VacuumIssue, error)
}

type vacuumStateUpdateQueries interface {
	UpdateAPISpecRevisionVacuumState(
		ctx context.Context,
		arg sqlc.UpdateAPISpecRevisionVacuumStateParams,
	) (sqlc.ApiSpecRevision, error)
}

func createVacuumIssue(
	ctx context.Context,
	queries vacuumIssueCreateQueries,
	input normalizedCreateVacuumIssueInput,
) (sqlc.VacuumIssue, error) {
	row, err := queries.CreateVacuumIssue(ctx, sqlc.CreateVacuumIssueParams{
		ApiSpecRevisionID: input.APISpecRevisionID,
		RuleID:            input.Issue.RuleID,
		Message:           input.Issue.Message,
		JsonPath:          input.Issue.JSONPath,
		RangePos:          input.Issue.RangePos,
	})
	if err != nil {
		return sqlc.VacuumIssue{}, fmt.Errorf(
			"create vacuum issue for api_spec_revision_id=%d rule_id=%q: %w",
			input.APISpecRevisionID,
			input.Issue.RuleID,
			err,
		)
	}

	return row, nil
}

func updateAPISpecRevisionVacuumState(
	ctx context.Context,
	queries vacuumStateUpdateQueries,
	input normalizedUpdateAPISpecRevisionVacuumStateInput,
) (APISpecRevision, error) {
	row, err := queries.UpdateAPISpecRevisionVacuumState(ctx, sqlc.UpdateAPISpecRevisionVacuumStateParams{
		ApiSpecRevisionID: input.APISpecRevisionID,
		VacuumStatus:      input.VacuumStatus,
		VacuumError:       input.VacuumError,
		VacuumValidatedAt: nullableTimestamp(input.VacuumValidatedAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APISpecRevision{}, fmt.Errorf("api spec revision not found: id=%d", input.APISpecRevisionID)
		}
		return APISpecRevision{}, fmt.Errorf(
			"update vacuum state for api_spec_revision_id=%d: %w",
			input.APISpecRevisionID,
			err,
		)
	}

	return mapAPISpecRevision(row), nil
}

type vacuumIssueDeleteQueries interface {
	DeleteVacuumIssuesByAPISpecRevisionID(ctx context.Context, apiSpecRevisionID int64) error
}

func deleteVacuumIssuesByAPISpecRevisionID(
	ctx context.Context,
	queries vacuumIssueDeleteQueries,
	apiSpecRevisionID int64,
) error {
	if err := queries.DeleteVacuumIssuesByAPISpecRevisionID(ctx, apiSpecRevisionID); err != nil {
		return fmt.Errorf("delete vacuum issues for api_spec_revision_id=%d: %w", apiSpecRevisionID, err)
	}
	return nil
}

type vacuumIssueReplaceQueries interface {
	vacuumIssueCreateQueries
	vacuumIssueDeleteQueries
}

func replaceVacuumIssues(
	ctx context.Context,
	queries vacuumIssueReplaceQueries,
	input normalizedReplaceVacuumIssuesInput,
) error {
	if err := deleteVacuumIssuesByAPISpecRevisionID(ctx, queries, input.APISpecRevisionID); err != nil {
		return err
	}

	for _, issue := range input.Issues {
		if _, err := createVacuumIssue(ctx, queries, normalizedCreateVacuumIssueInput{
			APISpecRevisionID: input.APISpecRevisionID,
			Issue:             issue,
		}); err != nil {
			return err
		}
	}

	return nil
}

func normalizeCreateVacuumIssueInput(input CreateVacuumIssueInput) (normalizedCreateVacuumIssueInput, error) {
	if input.APISpecRevisionID < 1 {
		return normalizedCreateVacuumIssueInput{}, errors.New("api spec revision id must be positive")
	}

	issue, err := normalizeVacuumIssueMutation("issue", input.Issue)
	if err != nil {
		return normalizedCreateVacuumIssueInput{}, err
	}

	return normalizedCreateVacuumIssueInput{
		APISpecRevisionID: input.APISpecRevisionID,
		Issue:             issue,
	}, nil
}

func normalizeReplaceVacuumIssuesInput(input ReplaceVacuumIssuesInput) (normalizedReplaceVacuumIssuesInput, error) {
	if input.APISpecRevisionID < 1 {
		return normalizedReplaceVacuumIssuesInput{}, errors.New("api spec revision id must be positive")
	}

	issues := make([]normalizedVacuumIssueMutation, 0, len(input.Issues))
	for i, issue := range input.Issues {
		normalized, err := normalizeVacuumIssueMutation(fmt.Sprintf("issues[%d]", i), issue)
		if err != nil {
			return normalizedReplaceVacuumIssuesInput{}, err
		}
		issues = append(issues, normalized)
	}

	return normalizedReplaceVacuumIssuesInput{
		APISpecRevisionID: input.APISpecRevisionID,
		Issues:            issues,
	}, nil
}

func normalizeUpdateAPISpecRevisionVacuumStateInput(
	input UpdateAPISpecRevisionVacuumStateInput,
) (normalizedUpdateAPISpecRevisionVacuumStateInput, error) {
	if input.APISpecRevisionID < 1 {
		return normalizedUpdateAPISpecRevisionVacuumStateInput{}, errors.New("api spec revision id must be positive")
	}

	status := strings.TrimSpace(input.VacuumStatus)
	switch status {
	case VacuumStatusPending, VacuumStatusProcessing, VacuumStatusProcessed, VacuumStatusFailed:
	default:
		return normalizedUpdateAPISpecRevisionVacuumStateInput{}, fmt.Errorf("vacuum status %q is invalid", status)
	}

	var validatedAt *time.Time
	if input.VacuumValidatedAt != nil {
		timestamp := input.VacuumValidatedAt.UTC()
		validatedAt = &timestamp
	}

	return normalizedUpdateAPISpecRevisionVacuumStateInput{
		APISpecRevisionID: input.APISpecRevisionID,
		VacuumStatus:      status,
		VacuumError:       strings.TrimSpace(input.VacuumError),
		VacuumValidatedAt: validatedAt,
	}, nil
}

func normalizeVacuumIssueMutation(
	fieldName string,
	input VacuumIssueMutation,
) (normalizedVacuumIssueMutation, error) {
	ruleID := strings.TrimSpace(input.RuleID)
	if ruleID == "" {
		return normalizedVacuumIssueMutation{}, fmt.Errorf("%s.rule_id must not be empty", fieldName)
	}

	message := strings.TrimSpace(input.Message)
	if message == "" {
		return normalizedVacuumIssueMutation{}, fmt.Errorf("%s.message must not be empty", fieldName)
	}

	jsonPath := strings.TrimSpace(input.JSONPath)
	if jsonPath == "" {
		return normalizedVacuumIssueMutation{}, fmt.Errorf("%s.json_path must not be empty", fieldName)
	}

	rangePos := copyInt32Slice(input.RangePos)
	if len(rangePos) != 4 {
		return normalizedVacuumIssueMutation{}, fmt.Errorf("%s.range_pos must contain exactly four numbers", fieldName)
	}

	return normalizedVacuumIssueMutation{
		RuleID:   ruleID,
		Message:  message,
		JSONPath: jsonPath,
		RangePos: rangePos,
	}, nil
}

func mapVacuumRule(row sqlc.VacuumRule) VacuumRule {
	return VacuumRule{
		RuleID:       row.RuleID,
		Severity:     row.Severity,
		Type:         row.Type,
		CategoryID:   row.CategoryID,
		CategoryName: row.CategoryName,
		Description:  row.Description,
		HowToFix:     row.HowToFix,
		GivenPath:    row.GivenPath,
		RuleJSON:     bytesCopy(row.RuleJson),
	}
}

func mapVacuumIssue(row sqlc.VacuumIssue) VacuumIssue {
	issue := VacuumIssue{
		ID:                row.ID,
		APISpecRevisionID: row.ApiSpecRevisionID,
		RuleID:            row.RuleID,
		Message:           row.Message,
		JSONPath:          row.JsonPath,
		RangePos:          copyInt32Slice(row.RangePos),
	}
	if row.CreatedAt.Valid {
		issue.CreatedAt = row.CreatedAt.Time.UTC()
	}
	return issue
}

func copyInt32Slice(value []int32) []int32 {
	if len(value) == 0 {
		return nil
	}

	copied := make([]int32, len(value))
	copy(copied, value)
	return copied
}
