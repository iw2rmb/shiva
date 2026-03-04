package store

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPersistSpecChange(t *testing.T) {
	t.Parallel()

	fromRevisionID := int64(12)
	input, err := normalizePersistSpecChangeInput(PersistSpecChangeInput{
		RepoID:         7,
		FromRevisionID: &fromRevisionID,
		ToRevisionID:   13,
		ChangeJSON: []byte(`{
			"summary":{"changed_endpoints":1},
			"version":1,
			"endpoints":{"changed":[{"method":"get","path":"/pets"}],"added":[],"removed":[]}
		}`),
	})
	if err != nil {
		t.Fatalf("normalizePersistSpecChangeInput() unexpected error: %v", err)
	}

	queries := &fakeSpecChangePersistenceQueries{}
	if err := persistSpecChange(context.Background(), queries, input); err != nil {
		t.Fatalf("persistSpecChange() unexpected error: %v", err)
	}

	if len(queries.calls) != 1 || queries.calls[0] != "create_spec_change" {
		t.Fatalf("unexpected query calls: %v", queries.calls)
	}

	if queries.arg.RepoID != 7 {
		t.Fatalf("expected repo_id=7, got %d", queries.arg.RepoID)
	}
	if !queries.arg.FromRevisionID.Valid || queries.arg.FromRevisionID.Int64 != 12 {
		t.Fatalf("unexpected from_revision_id: %+v", queries.arg.FromRevisionID)
	}
	if queries.arg.ToRevisionID != 13 {
		t.Fatalf("expected to_revision_id=13, got %d", queries.arg.ToRevisionID)
	}
	expectedJSON := `{"endpoints":{"added":[],"changed":[{"method":"get","path":"/pets"}],"removed":[]},"summary":{"changed_endpoints":1},"version":1}`
	if string(queries.arg.ChangeJson) != expectedJSON {
		t.Fatalf("unexpected change_json: %s", string(queries.arg.ChangeJson))
	}
}

func TestNormalizePersistSpecChangeInput(t *testing.T) {
	t.Parallel()

	type expected struct {
		repoID         int64
		fromRevisionID pgtype.Int8
		toRevisionID   int64
		changeJSON     string
	}

	testCases := []struct {
		name          string
		input         PersistSpecChangeInput
		expected      expected
		expectedError string
	}{
		{
			name: "null from revision is valid",
			input: PersistSpecChangeInput{
				RepoID:       2,
				ToRevisionID: 9,
				ChangeJSON:   []byte(`{"version":1,"endpoints":{"added":[{"method":"get","path":"/a"}]}}`),
			},
			expected: expected{
				repoID:         2,
				fromRevisionID: pgtype.Int8{},
				toRevisionID:   9,
				changeJSON:     `{"endpoints":{"added":[{"method":"get","path":"/a"}]},"version":1}`,
			},
		},
		{
			name: "invalid json rejected",
			input: PersistSpecChangeInput{
				RepoID:       2,
				ToRevisionID: 9,
				ChangeJSON:   []byte(`{`),
			},
			expectedError: "spec change json is invalid",
		},
		{
			name: "non-positive from revision rejected",
			input: PersistSpecChangeInput{
				RepoID:         2,
				FromRevisionID: int64Pointer(0),
				ToRevisionID:   9,
				ChangeJSON:     []byte(`{"version":1}`),
			},
			expectedError: "from revision id must be positive",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			actual, err := normalizePersistSpecChangeInput(testCase.input)
			if testCase.expectedError != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", testCase.expectedError)
				}
				if !strings.Contains(err.Error(), testCase.expectedError) {
					t.Fatalf("expected error containing %q, got %q", testCase.expectedError, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizePersistSpecChangeInput() unexpected error: %v", err)
			}

			if actual.RepoID != testCase.expected.repoID {
				t.Fatalf("expected repo_id=%d, got %d", testCase.expected.repoID, actual.RepoID)
			}
			if !reflect.DeepEqual(actual.FromRevisionID, testCase.expected.fromRevisionID) {
				t.Fatalf(
					"unexpected from_revision_id: expected %+v, got %+v",
					testCase.expected.fromRevisionID,
					actual.FromRevisionID,
				)
			}
			if actual.ToRevisionID != testCase.expected.toRevisionID {
				t.Fatalf("expected to_revision_id=%d, got %d", testCase.expected.toRevisionID, actual.ToRevisionID)
			}
			if string(actual.ChangeJSON) != testCase.expected.changeJSON {
				t.Fatalf("unexpected change_json: %s", string(actual.ChangeJSON))
			}
		})
	}
}

type fakeSpecChangePersistenceQueries struct {
	calls []string
	arg   sqlc.CreateSpecChangeParams
}

func (f *fakeSpecChangePersistenceQueries) CreateSpecChange(
	_ context.Context,
	arg sqlc.CreateSpecChangeParams,
) (sqlc.SpecChange, error) {
	f.calls = append(f.calls, "create_spec_change")
	f.arg = arg
	return sqlc.SpecChange{}, nil
}

func int64Pointer(value int64) *int64 {
	return &value
}
