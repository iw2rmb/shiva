package store

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestReplaceVacuumIssues_ReplacesRowsByAPISpecRevisionID(t *testing.T) {
	t.Parallel()

	queries := newFakeVacuumIssueQueries()
	if _, err := createVacuumIssue(context.Background(), queries, normalizedCreateVacuumIssueInput{
		APISpecRevisionID: 11,
		Issue: normalizedVacuumIssueMutation{
			RuleID:   "duplicate-paths",
			Message:  "existing",
			JSONPath: "$.paths",
			RangePos: []int32{1, 2, 3, 4},
		},
	}); err != nil {
		t.Fatalf("createVacuumIssue() unexpected error: %v", err)
	}
	if _, err := createVacuumIssue(context.Background(), queries, normalizedCreateVacuumIssueInput{
		APISpecRevisionID: 12,
		Issue: normalizedVacuumIssueMutation{
			RuleID:   "typed-enum",
			Message:  "other revision",
			JSONPath: "$.components.schemas.Pet",
			RangePos: []int32{4, 5, 6, 7},
		},
	}); err != nil {
		t.Fatalf("createVacuumIssue() unexpected error: %v", err)
	}

	input, err := normalizeReplaceVacuumIssuesInput(ReplaceVacuumIssuesInput{
		APISpecRevisionID: 11,
		Issues: []VacuumIssueMutation{
			{
				RuleID:   "info-description",
				Message:  "missing description",
				JSONPath: "$.info",
				RangePos: []int32{10, 11, 12, 13},
			},
			{
				RuleID:   "paths-kebab-case",
				Message:  "must use kebab-case",
				JSONPath: "$.paths['/Bad_Path']",
				RangePos: []int32{14, 15, 16, 17},
			},
		},
	})
	if err != nil {
		t.Fatalf("normalizeReplaceVacuumIssuesInput() unexpected error: %v", err)
	}

	if err := replaceVacuumIssues(context.Background(), queries, input); err != nil {
		t.Fatalf("replaceVacuumIssues() unexpected error: %v", err)
	}

	expectedCalls := []string{
		"create:11:duplicate-paths",
		"create:12:typed-enum",
		"delete:11",
		"create:11:info-description",
		"create:11:paths-kebab-case",
	}
	if !reflect.DeepEqual(queries.calls, expectedCalls) {
		t.Fatalf("unexpected call order: expected %v, got %v", expectedCalls, queries.calls)
	}

	revision11 := queries.byRevision[11]
	if len(revision11) != 2 {
		t.Fatalf("expected 2 issues for revision 11, got %d", len(revision11))
	}
	if revision11[0].RuleID != "info-description" || revision11[1].RuleID != "paths-kebab-case" {
		t.Fatalf("unexpected revision 11 issues: %+v", revision11)
	}

	revision12 := queries.byRevision[12]
	if len(revision12) != 1 || revision12[0].RuleID != "typed-enum" {
		t.Fatalf("expected unrelated revision 12 issue to remain unchanged, got %+v", revision12)
	}
}

func TestReplaceVacuumIssues_ZeroIssueReplacementClearsRevisionRows(t *testing.T) {
	t.Parallel()

	queries := newFakeVacuumIssueQueries()
	for _, issue := range []normalizedCreateVacuumIssueInput{
		{
			APISpecRevisionID: 21,
			Issue: normalizedVacuumIssueMutation{
				RuleID:   "duplicate-paths",
				Message:  "first",
				JSONPath: "$.paths",
				RangePos: []int32{1, 1, 1, 2},
			},
		},
		{
			APISpecRevisionID: 21,
			Issue: normalizedVacuumIssueMutation{
				RuleID:   "tag-description",
				Message:  "second",
				JSONPath: "$.tags[0]",
				RangePos: []int32{2, 1, 2, 2},
			},
		},
	} {
		if _, err := createVacuumIssue(context.Background(), queries, issue); err != nil {
			t.Fatalf("createVacuumIssue() unexpected error: %v", err)
		}
	}

	input, err := normalizeReplaceVacuumIssuesInput(ReplaceVacuumIssuesInput{
		APISpecRevisionID: 21,
		Issues:            []VacuumIssueMutation{},
	})
	if err != nil {
		t.Fatalf("normalizeReplaceVacuumIssuesInput() unexpected error: %v", err)
	}

	if err := replaceVacuumIssues(context.Background(), queries, input); err != nil {
		t.Fatalf("replaceVacuumIssues() unexpected error: %v", err)
	}

	if issues := queries.byRevision[21]; len(issues) != 0 {
		t.Fatalf("expected revision 21 issues to be cleared, got %+v", issues)
	}
	if queries.calls[len(queries.calls)-1] != "delete:21" {
		t.Fatalf("expected zero-issue replacement to stop after delete, got call log %v", queries.calls)
	}
}

func TestUpdateAPISpecRevisionVacuumState_StateTransitions(t *testing.T) {
	t.Parallel()

	validatedAt := time.Date(2026, time.March, 13, 15, 4, 5, 0, time.FixedZone("MSK", 3*60*60))
	queries := &fakeVacuumStateQueries{
		byRevision: map[int64]sqlc.ApiSpecRevision{
			41: {
				ID:                 41,
				ApiSpecID:          7,
				IngestEventID:      8,
				RootPathAtRevision: "apis/pets/openapi.yaml",
				BuildStatus:        "processed",
			},
		},
	}

	processedInput, err := normalizeUpdateAPISpecRevisionVacuumStateInput(UpdateAPISpecRevisionVacuumStateInput{
		APISpecRevisionID: 41,
		VacuumStatus:      VacuumStatusProcessed,
		VacuumValidatedAt: &validatedAt,
	})
	if err != nil {
		t.Fatalf("normalizeUpdateAPISpecRevisionVacuumStateInput() unexpected error: %v", err)
	}

	processed, err := updateAPISpecRevisionVacuumState(context.Background(), queries, processedInput)
	if err != nil {
		t.Fatalf("updateAPISpecRevisionVacuumState() unexpected error: %v", err)
	}
	if processed.VacuumStatus != VacuumStatusProcessed {
		t.Fatalf("expected processed status, got %q", processed.VacuumStatus)
	}
	if processed.VacuumValidatedAt == nil || !processed.VacuumValidatedAt.Equal(validatedAt.UTC()) {
		t.Fatalf("expected vacuum_validated_at=%s, got %+v", validatedAt.UTC(), processed.VacuumValidatedAt)
	}
	if processed.VacuumError != "" {
		t.Fatalf("expected vacuum_error to remain empty, got %q", processed.VacuumError)
	}

	failedInput, err := normalizeUpdateAPISpecRevisionVacuumStateInput(UpdateAPISpecRevisionVacuumStateInput{
		APISpecRevisionID: 41,
		VacuumStatus:      VacuumStatusFailed,
		VacuumError:       " vacuum parse failed ",
		VacuumValidatedAt: nil,
	})
	if err != nil {
		t.Fatalf("normalizeUpdateAPISpecRevisionVacuumStateInput() unexpected error: %v", err)
	}

	failed, err := updateAPISpecRevisionVacuumState(context.Background(), queries, failedInput)
	if err != nil {
		t.Fatalf("updateAPISpecRevisionVacuumState() unexpected error: %v", err)
	}
	if failed.VacuumStatus != VacuumStatusFailed {
		t.Fatalf("expected failed status, got %q", failed.VacuumStatus)
	}
	if failed.VacuumError != "vacuum parse failed" {
		t.Fatalf("expected trimmed vacuum_error, got %q", failed.VacuumError)
	}
	if failed.VacuumValidatedAt != nil {
		t.Fatalf("expected vacuum_validated_at to be cleared, got %+v", failed.VacuumValidatedAt)
	}
}

func TestUpdateAPISpecRevisionVacuumState_NotFound(t *testing.T) {
	t.Parallel()

	queries := &fakeVacuumStateQueries{err: pgx.ErrNoRows}

	_, err := updateAPISpecRevisionVacuumState(context.Background(), queries, normalizedUpdateAPISpecRevisionVacuumStateInput{
		APISpecRevisionID: 99,
		VacuumStatus:      VacuumStatusFailed,
		VacuumError:       "boom",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "api spec revision not found: id=99" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPersistAPISpecRevisionVacuumResult_ProcessedReplacesIssuesAndUpdatesState(t *testing.T) {
	t.Parallel()

	validatedAt := time.Date(2026, time.March, 13, 16, 0, 0, 0, time.UTC)
	queries := newFakeVacuumPersistenceQueries(51)

	row, err := persistAPISpecRevisionVacuumResult(context.Background(), queries, normalizedPersistAPISpecRevisionVacuumResultInput{
		APISpecRevisionID: 51,
		Issues: []normalizedVacuumIssueMutation{
			{
				RuleID:   "info-description",
				Message:  "missing description",
				JSONPath: "$.info",
				RangePos: []int32{1, 2, 3, 4},
			},
		},
		VacuumStatus:      VacuumStatusProcessed,
		VacuumValidatedAt: &validatedAt,
	})
	if err != nil {
		t.Fatalf("persistAPISpecRevisionVacuumResult() unexpected error: %v", err)
	}
	if row.VacuumStatus != VacuumStatusProcessed {
		t.Fatalf("expected processed status, got %q", row.VacuumStatus)
	}
	if row.VacuumValidatedAt == nil || !row.VacuumValidatedAt.Equal(validatedAt) {
		t.Fatalf("expected validated_at %s, got %+v", validatedAt, row.VacuumValidatedAt)
	}
	if row.VacuumError != "" {
		t.Fatalf("expected empty vacuum_error, got %q", row.VacuumError)
	}

	expectedCalls := []string{
		"delete:51",
		"create:51:info-description",
		"state:51:processed",
	}
	if !reflect.DeepEqual(queries.calls, expectedCalls) {
		t.Fatalf("unexpected call order: expected %v, got %v", expectedCalls, queries.calls)
	}
	if len(queries.issueQueries.byRevision[51]) != 1 {
		t.Fatalf("expected one persisted issue, got %+v", queries.issueQueries.byRevision[51])
	}
}

func TestPersistAPISpecRevisionVacuumResult_FailedClearsIssuesAndUpdatesState(t *testing.T) {
	t.Parallel()

	queries := newFakeVacuumPersistenceQueries(61)
	if _, err := createVacuumIssue(context.Background(), queries, normalizedCreateVacuumIssueInput{
		APISpecRevisionID: 61,
		Issue: normalizedVacuumIssueMutation{
			RuleID:   "duplicate-paths",
			Message:  "existing",
			JSONPath: "$.paths",
			RangePos: []int32{1, 1, 1, 2},
		},
	}); err != nil {
		t.Fatalf("createVacuumIssue() unexpected error: %v", err)
	}
	queries.calls = nil

	row, err := persistAPISpecRevisionVacuumResult(context.Background(), queries, normalizedPersistAPISpecRevisionVacuumResultInput{
		APISpecRevisionID: 61,
		Issues:            nil,
		VacuumStatus:      VacuumStatusFailed,
		VacuumError:       "parse failed",
	})
	if err != nil {
		t.Fatalf("persistAPISpecRevisionVacuumResult() unexpected error: %v", err)
	}
	if row.VacuumStatus != VacuumStatusFailed {
		t.Fatalf("expected failed status, got %q", row.VacuumStatus)
	}
	if row.VacuumError != "parse failed" {
		t.Fatalf("expected persisted vacuum_error, got %q", row.VacuumError)
	}
	if row.VacuumValidatedAt != nil {
		t.Fatalf("expected validated_at to remain nil, got %+v", row.VacuumValidatedAt)
	}
	if len(queries.issueQueries.byRevision[61]) != 0 {
		t.Fatalf("expected issues to be cleared, got %+v", queries.issueQueries.byRevision[61])
	}
	expectedCalls := []string{
		"delete:61",
		"state:61:failed",
	}
	if !reflect.DeepEqual(queries.calls, expectedCalls) {
		t.Fatalf("unexpected call order: expected %v, got %v", expectedCalls, queries.calls)
	}
}

func TestNormalizePersistAPISpecRevisionVacuumResultInput_RejectsInvalidStateCombinations(t *testing.T) {
	t.Parallel()

	validatedAt := time.Date(2026, time.March, 13, 17, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		input PersistAPISpecRevisionVacuumResultInput
		want  string
	}{
		{
			name: "processed requires validated timestamp",
			input: PersistAPISpecRevisionVacuumResultInput{
				APISpecRevisionID: 71,
				VacuumStatus:      VacuumStatusProcessed,
			},
			want: "vacuum_validated_at must be set when vacuum_status is processed",
		},
		{
			name: "processed rejects vacuum error",
			input: PersistAPISpecRevisionVacuumResultInput{
				APISpecRevisionID: 71,
				VacuumStatus:      VacuumStatusProcessed,
				VacuumError:       "boom",
				VacuumValidatedAt: &validatedAt,
			},
			want: "vacuum_error must be empty when vacuum_status is processed",
		},
		{
			name: "failed requires empty issues",
			input: PersistAPISpecRevisionVacuumResultInput{
				APISpecRevisionID: 71,
				VacuumStatus:      VacuumStatusFailed,
				VacuumError:       "boom",
				Issues: []VacuumIssueMutation{
					{
						RuleID:   "info-description",
						Message:  "missing description",
						JSONPath: "$.info",
						RangePos: []int32{1, 2, 3, 4},
					},
				},
			},
			want: "issues must be empty when vacuum_status is failed",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := normalizePersistAPISpecRevisionVacuumResultInput(tc.input)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != tc.want {
				t.Fatalf("expected error %q, got %q", tc.want, err.Error())
			}
		})
	}
}

type fakeVacuumIssueQueries struct {
	nextID     int64
	calls      []string
	byRevision map[int64][]sqlc.VacuumIssue
}

func newFakeVacuumIssueQueries() *fakeVacuumIssueQueries {
	return &fakeVacuumIssueQueries{
		nextID:     1,
		byRevision: make(map[int64][]sqlc.VacuumIssue),
	}
}

func (f *fakeVacuumIssueQueries) CreateVacuumIssue(_ context.Context, arg sqlc.CreateVacuumIssueParams) (sqlc.VacuumIssue, error) {
	issue := sqlc.VacuumIssue{
		ID:                f.nextID,
		ApiSpecRevisionID: arg.ApiSpecRevisionID,
		RuleID:            arg.RuleID,
		Message:           arg.Message,
		JsonPath:          arg.JsonPath,
		RangePos:          append([]int32(nil), arg.RangePos...),
		CreatedAt:         pgtype.Timestamptz{Time: time.Unix(f.nextID, 0).UTC(), Valid: true},
	}
	f.nextID++
	f.calls = append(f.calls, "create:"+strconv.FormatInt(arg.ApiSpecRevisionID, 10)+":"+arg.RuleID)
	f.byRevision[arg.ApiSpecRevisionID] = append(f.byRevision[arg.ApiSpecRevisionID], issue)
	return issue, nil
}

func (f *fakeVacuumIssueQueries) DeleteVacuumIssuesByAPISpecRevisionID(_ context.Context, apiSpecRevisionID int64) error {
	f.calls = append(f.calls, "delete:"+strconv.FormatInt(apiSpecRevisionID, 10))
	f.byRevision[apiSpecRevisionID] = nil
	return nil
}

type fakeVacuumStateQueries struct {
	byRevision map[int64]sqlc.ApiSpecRevision
	err        error
}

func (f *fakeVacuumStateQueries) UpdateAPISpecRevisionVacuumState(
	_ context.Context,
	arg sqlc.UpdateAPISpecRevisionVacuumStateParams,
) (sqlc.ApiSpecRevision, error) {
	if f.err != nil {
		return sqlc.ApiSpecRevision{}, f.err
	}

	row, ok := f.byRevision[arg.ApiSpecRevisionID]
	if !ok {
		return sqlc.ApiSpecRevision{}, pgx.ErrNoRows
	}
	row.VacuumStatus = arg.VacuumStatus
	row.VacuumError = arg.VacuumError
	row.VacuumValidatedAt = arg.VacuumValidatedAt
	f.byRevision[arg.ApiSpecRevisionID] = row
	return row, nil
}

type fakeVacuumPersistenceQueries struct {
	calls        []string
	issueQueries *fakeVacuumIssueQueries
	stateQueries *fakeVacuumStateQueries
}

func newFakeVacuumPersistenceQueries(apiSpecRevisionID int64) *fakeVacuumPersistenceQueries {
	return &fakeVacuumPersistenceQueries{
		issueQueries: newFakeVacuumIssueQueries(),
		stateQueries: &fakeVacuumStateQueries{
			byRevision: map[int64]sqlc.ApiSpecRevision{
				apiSpecRevisionID: {
					ID:                 apiSpecRevisionID,
					ApiSpecID:          9,
					IngestEventID:      10,
					RootPathAtRevision: "apis/pets/openapi.yaml",
					BuildStatus:        "processed",
				},
			},
		},
	}
}

func (f *fakeVacuumPersistenceQueries) CreateVacuumIssue(ctx context.Context, arg sqlc.CreateVacuumIssueParams) (sqlc.VacuumIssue, error) {
	f.calls = append(f.calls, "create:"+strconv.FormatInt(arg.ApiSpecRevisionID, 10)+":"+arg.RuleID)
	return f.issueQueries.CreateVacuumIssue(ctx, arg)
}

func (f *fakeVacuumPersistenceQueries) DeleteVacuumIssuesByAPISpecRevisionID(ctx context.Context, apiSpecRevisionID int64) error {
	f.calls = append(f.calls, "delete:"+strconv.FormatInt(apiSpecRevisionID, 10))
	return f.issueQueries.DeleteVacuumIssuesByAPISpecRevisionID(ctx, apiSpecRevisionID)
}

func (f *fakeVacuumPersistenceQueries) UpdateAPISpecRevisionVacuumState(
	ctx context.Context,
	arg sqlc.UpdateAPISpecRevisionVacuumStateParams,
) (sqlc.ApiSpecRevision, error) {
	f.calls = append(f.calls, "state:"+strconv.FormatInt(arg.ApiSpecRevisionID, 10)+":"+arg.VacuumStatus)
	return f.stateQueries.UpdateAPISpecRevisionVacuumState(ctx, arg)
}
