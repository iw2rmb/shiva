package store

import (
	"context"
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPersistCanonicalSpec_PerAPIRevisionIsolation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                     string
		input                    PersistCanonicalSpecInput
		expectedAPISpecRevision  int64
		expectedInsertedEndpoint []sqlc.InsertEndpointIndexParams
	}{
		{
			name: "single api revision writes only own artifact and index",
			input: PersistCanonicalSpecInput{
				APISpecRevisionID: 42,
				SpecJSON:          []byte(`{"openapi":"3.1.0","paths":{}}`),
				SpecYAML:          "openapi: 3.1.0\npaths: {}\n",
				ETag:              "\"etag-value\"",
				SizeBytes:         1234,
				Endpoints: []EndpointIndexRecord{
					{
						Method:      "GET",
						Path:        "/pets",
						OperationID: "listPets",
						Summary:     "List pets",
						Deprecated:  false,
						RawJSON:     []byte(`{"responses":{"200":{"description":"ok"}},"summary":"List pets","operationId":"listPets"}`),
					},
					{
						Method:     "delete",
						Path:       "/pets/{id}",
						Deprecated: true,
						RawJSON:    []byte(`{"responses":{"204":{"description":"deleted"}},"deprecated":true}`),
					},
				},
			},
			expectedAPISpecRevision: 42,
			expectedInsertedEndpoint: []sqlc.InsertEndpointIndexParams{
				{
					ApiSpecRevisionID: 42,
					Method:            "delete",
					Path:              "/pets/{id}",
					OperationID:       pgtype.Text{},
					Summary:           pgtype.Text{},
					Deprecated:        true,
					RawJson:           []byte(`{"deprecated":true,"responses":{"204":{"description":"deleted"}}}`),
				},
				{
					ApiSpecRevisionID: 42,
					Method:            "get",
					Path:              "/pets",
					OperationID:       pgtype.Text{String: "listPets", Valid: true},
					Summary:           pgtype.Text{String: "List pets", Valid: true},
					Deprecated:        false,
					RawJson:           []byte(`{"operationId":"listPets","responses":{"200":{"description":"ok"}},"summary":"List pets"}`),
				},
			},
		},
		{
			name: "different api revision keeps independent key space",
			input: PersistCanonicalSpecInput{
				APISpecRevisionID: 73,
				SpecJSON:          []byte(`{"openapi":"3.0.3","paths":{"/orders":{}}}`),
				SpecYAML:          "openapi: 3.0.3\npaths:\n  /orders: {}\n",
				ETag:              "\"etag-orders\"",
				SizeBytes:         512,
				Endpoints: []EndpointIndexRecord{
					{
						Method:      "post",
						Path:        "/orders",
						OperationID: "createOrder",
						RawJSON:     []byte(`{"operationId":"createOrder","responses":{"201":{"description":"created"}}}`),
					},
				},
			},
			expectedAPISpecRevision: 73,
			expectedInsertedEndpoint: []sqlc.InsertEndpointIndexParams{
				{
					ApiSpecRevisionID: 73,
					Method:            "post",
					Path:              "/orders",
					OperationID:       pgtype.Text{String: "createOrder", Valid: true},
					Summary:           pgtype.Text{},
					Deprecated:        false,
					RawJson:           []byte(`{"operationId":"createOrder","responses":{"201":{"description":"created"}}}`),
				},
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			input, err := normalizePersistCanonicalSpecInput(testCase.input)
			if err != nil {
				t.Fatalf("normalizePersistCanonicalSpecInput() unexpected error: %v", err)
			}

			queries := &fakeSpecPersistenceQueries{}
			if err := persistCanonicalSpec(context.Background(), queries, input); err != nil {
				t.Fatalf("persistCanonicalSpec() unexpected error: %v", err)
			}

			expectedCalls := []string{"upsert_artifact", "delete_endpoint_index"}
			for range testCase.expectedInsertedEndpoint {
				expectedCalls = append(expectedCalls, "insert_endpoint_index")
			}
			if !reflect.DeepEqual(queries.calls, expectedCalls) {
				t.Fatalf("unexpected query call order: expected %v, got %v", expectedCalls, queries.calls)
			}
			if queries.upsert.ApiSpecRevisionID != testCase.expectedAPISpecRevision {
				t.Fatalf("expected artifact api_spec_revision_id=%d, got %d", testCase.expectedAPISpecRevision, queries.upsert.ApiSpecRevisionID)
			}
			if queries.deletedAPISpecRevisionID != testCase.expectedAPISpecRevision {
				t.Fatalf(
					"expected delete endpoint index api_spec_revision_id=%d, got %d",
					testCase.expectedAPISpecRevision,
					queries.deletedAPISpecRevisionID,
				)
			}
			if !reflect.DeepEqual(queries.inserted, testCase.expectedInsertedEndpoint) {
				t.Fatalf("unexpected inserted endpoint payloads: expected %+v, got %+v", testCase.expectedInsertedEndpoint, queries.inserted)
			}
		})
	}
}

func TestNormalizePersistCanonicalSpecInput_DuplicateEndpointFails(t *testing.T) {
	t.Parallel()

	_, err := normalizePersistCanonicalSpecInput(PersistCanonicalSpecInput{
		APISpecRevisionID: 8,
		SpecJSON:          []byte(`{"openapi":"3.0.3"}`),
		SpecYAML:          "openapi: 3.0.3\n",
		ETag:              "\"abc\"",
		SizeBytes:         5,
		Endpoints: []EndpointIndexRecord{
			{
				Method:  "get",
				Path:    "/pets",
				RawJSON: []byte(`{"responses":{"200":{"description":"ok"}}}`),
			},
			{
				Method:  "GET",
				Path:    "/pets",
				RawJSON: []byte(`{"responses":{"200":{"description":"still ok"}}}`),
			},
		},
	})
	if err == nil {
		t.Fatalf("expected duplicate endpoint error")
	}
}

type fakeSpecPersistenceQueries struct {
	calls                    []string
	upsert                   sqlc.UpsertSpecArtifactParams
	deletedAPISpecRevisionID int64
	inserted                 []sqlc.InsertEndpointIndexParams
}

func (f *fakeSpecPersistenceQueries) UpsertSpecArtifact(
	_ context.Context,
	arg sqlc.UpsertSpecArtifactParams,
) (sqlc.SpecArtifact, error) {
	f.calls = append(f.calls, "upsert_artifact")
	f.upsert = arg
	return sqlc.SpecArtifact{}, nil
}

func (f *fakeSpecPersistenceQueries) DeleteEndpointIndexByAPISpecRevision(_ context.Context, apiSpecRevisionID int64) error {
	f.calls = append(f.calls, "delete_endpoint_index")
	f.deletedAPISpecRevisionID = apiSpecRevisionID
	return nil
}

func (f *fakeSpecPersistenceQueries) InsertEndpointIndex(
	_ context.Context,
	arg sqlc.InsertEndpointIndexParams,
) (sqlc.EndpointIndex, error) {
	f.calls = append(f.calls, "insert_endpoint_index")
	f.inserted = append(f.inserted, arg)
	return sqlc.EndpointIndex{
		ID:                int64(len(f.inserted)),
		ApiSpecRevisionID: arg.ApiSpecRevisionID,
		Method:            arg.Method,
		Path:              arg.Path,
		OperationID:       pgtype.Text{String: arg.OperationID.String, Valid: arg.OperationID.Valid},
		Summary:           pgtype.Text{String: arg.Summary.String, Valid: arg.Summary.Valid},
		Deprecated:        arg.Deprecated,
		RawJson:           arg.RawJson,
	}, nil
}
