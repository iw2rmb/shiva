package store

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPersistSpecChange_PerAPIScopeIsolation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		input           PersistSpecChangeInput
		expectedAPISpec int64
		expectedFrom    pgtype.Int8
		expectedTo      int64
		expectedJSON    string
	}{
		{
			name: "with previous api revision",
			input: PersistSpecChangeInput{
				APISpecID:             7,
				FromAPISpecRevisionID: int64Pointer(12),
				ToAPISpecRevisionID:   13,
				ChangeJSON: []byte(`{
					"summary":{"changed_endpoints":1},
					"version":1,
					"endpoints":{"changed":[{"method":"get","path":"/pets"}],"added":[],"removed":[]}
				}`),
			},
			expectedAPISpec: 7,
			expectedFrom:    pgtype.Int8{Int64: 12, Valid: true},
			expectedTo:      13,
			expectedJSON:    `{"endpoints":{"added":[],"changed":[{"method":"get","path":"/pets"}],"removed":[]},"summary":{"changed_endpoints":1},"version":1}`,
		},
		{
			name: "different api scope with first revision",
			input: PersistSpecChangeInput{
				APISpecID:           11,
				ToAPISpecRevisionID: 20,
				ChangeJSON:          []byte(`{"version":1,"summary":{"added_endpoints":2}}`),
			},
			expectedAPISpec: 11,
			expectedFrom:    pgtype.Int8{},
			expectedTo:      20,
			expectedJSON:    `{"summary":{"added_endpoints":2},"version":1}`,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			input, err := normalizePersistSpecChangeInput(testCase.input)
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
			if queries.arg.ApiSpecID != testCase.expectedAPISpec {
				t.Fatalf("expected api_spec_id=%d, got %d", testCase.expectedAPISpec, queries.arg.ApiSpecID)
			}
			if !reflect.DeepEqual(queries.arg.FromApiSpecRevisionID, testCase.expectedFrom) {
				t.Fatalf(
					"unexpected from_api_spec_revision_id: expected %+v, got %+v",
					testCase.expectedFrom,
					queries.arg.FromApiSpecRevisionID,
				)
			}
			if queries.arg.ToApiSpecRevisionID != testCase.expectedTo {
				t.Fatalf("expected to_api_spec_revision_id=%d, got %d", testCase.expectedTo, queries.arg.ToApiSpecRevisionID)
			}
			if string(queries.arg.ChangeJson) != testCase.expectedJSON {
				t.Fatalf("unexpected change_json: %s", string(queries.arg.ChangeJson))
			}
		})
	}
}

func TestNormalizePersistSpecChangeInput(t *testing.T) {
	t.Parallel()

	type expected struct {
		apiSpecID             int64
		fromAPISpecRevisionID pgtype.Int8
		toAPISpecRevisionID   int64
		changeJSON            string
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
				APISpecID:           2,
				ToAPISpecRevisionID: 9,
				ChangeJSON:          []byte(`{"version":1,"endpoints":{"added":[{"method":"get","path":"/a"}]}}`),
			},
			expected: expected{
				apiSpecID:             2,
				fromAPISpecRevisionID: pgtype.Int8{},
				toAPISpecRevisionID:   9,
				changeJSON:            `{"endpoints":{"added":[{"method":"get","path":"/a"}]},"version":1}`,
			},
		},
		{
			name: "invalid json rejected",
			input: PersistSpecChangeInput{
				APISpecID:           2,
				ToAPISpecRevisionID: 9,
				ChangeJSON:          []byte(`{`),
			},
			expectedError: "spec change json is invalid",
		},
		{
			name: "non-positive from revision rejected",
			input: PersistSpecChangeInput{
				APISpecID:             2,
				FromAPISpecRevisionID: int64Pointer(0),
				ToAPISpecRevisionID:   9,
				ChangeJSON:            []byte(`{"version":1}`),
			},
			expectedError: "from api spec revision id must be positive",
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

			if actual.APISpecID != testCase.expected.apiSpecID {
				t.Fatalf("expected api_spec_id=%d, got %d", testCase.expected.apiSpecID, actual.APISpecID)
			}
			if !reflect.DeepEqual(actual.FromAPISpecRevisionID, testCase.expected.fromAPISpecRevisionID) {
				t.Fatalf(
					"unexpected from_api_spec_revision_id: expected %+v, got %+v",
					testCase.expected.fromAPISpecRevisionID,
					actual.FromAPISpecRevisionID,
				)
			}
			if actual.ToAPISpecRevisionID != testCase.expected.toAPISpecRevisionID {
				t.Fatalf("expected to_api_spec_revision_id=%d, got %d", testCase.expected.toAPISpecRevisionID, actual.ToAPISpecRevisionID)
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
