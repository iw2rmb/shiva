package store

import (
	"context"
	"reflect"
	"testing"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestPersistCanonicalSpec_UpsertsArtifactAndReplacesEndpointIndex(t *testing.T) {
	t.Parallel()

	input, err := normalizePersistCanonicalSpecInput(PersistCanonicalSpecInput{
		RevisionID: 42,
		SpecJSON:   []byte(`{"openapi":"3.1.0","paths":{}}`),
		SpecYAML:   "openapi: 3.1.0\npaths: {}\n",
		ETag:       "\"etag-value\"",
		SizeBytes:  1234,
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
	})
	if err != nil {
		t.Fatalf("normalizePersistCanonicalSpecInput() unexpected error: %v", err)
	}

	queries := &fakeSpecPersistenceQueries{}
	if err := persistCanonicalSpec(context.Background(), queries, input); err != nil {
		t.Fatalf("persistCanonicalSpec() unexpected error: %v", err)
	}

	expectedCalls := []string{"upsert_artifact", "delete_endpoint_index", "insert_endpoint_index", "insert_endpoint_index"}
	if !reflect.DeepEqual(queries.calls, expectedCalls) {
		t.Fatalf("unexpected query call order: expected %v, got %v", expectedCalls, queries.calls)
	}

	if queries.upsert.RevisionID != 42 {
		t.Fatalf("expected artifact revision_id=42, got %d", queries.upsert.RevisionID)
	}
	if string(queries.upsert.SpecJson) != `{"openapi":"3.1.0","paths":{}}` {
		t.Fatalf("unexpected spec_json: %s", string(queries.upsert.SpecJson))
	}
	if queries.upsert.Etag != "\"etag-value\"" {
		t.Fatalf("unexpected etag: %s", queries.upsert.Etag)
	}
	if queries.deletedRevisionID != 42 {
		t.Fatalf("expected delete endpoint index revision_id=42, got %d", queries.deletedRevisionID)
	}

	if len(queries.inserted) != 2 {
		t.Fatalf("expected 2 inserted endpoints, got %d", len(queries.inserted))
	}

	first := queries.inserted[0]
	if first.Method != "delete" || first.Path != "/pets/{id}" {
		t.Fatalf("expected first insert to be delete /pets/{id}, got %s %s", first.Method, first.Path)
	}
	if first.OperationID.Valid {
		t.Fatalf("expected delete endpoint operation_id to be NULL")
	}
	if first.Summary.Valid {
		t.Fatalf("expected delete endpoint summary to be NULL")
	}

	second := queries.inserted[1]
	if second.Method != "get" || second.Path != "/pets" {
		t.Fatalf("expected second insert to be get /pets, got %s %s", second.Method, second.Path)
	}
	if !second.OperationID.Valid || second.OperationID.String != "listPets" {
		t.Fatalf("unexpected operation_id for get /pets: %+v", second.OperationID)
	}
	if !second.Summary.Valid || second.Summary.String != "List pets" {
		t.Fatalf("unexpected summary for get /pets: %+v", second.Summary)
	}
}

func TestNormalizePersistCanonicalSpecInput_DuplicateEndpointFails(t *testing.T) {
	t.Parallel()

	_, err := normalizePersistCanonicalSpecInput(PersistCanonicalSpecInput{
		RevisionID: 8,
		SpecJSON:   []byte(`{"openapi":"3.0.3"}`),
		SpecYAML:   "openapi: 3.0.3\n",
		ETag:       "\"abc\"",
		SizeBytes:  5,
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
	calls             []string
	upsert            sqlc.UpsertSpecArtifactParams
	deletedRevisionID int64
	inserted          []sqlc.InsertEndpointIndexParams
}

func (f *fakeSpecPersistenceQueries) UpsertSpecArtifact(
	_ context.Context,
	arg sqlc.UpsertSpecArtifactParams,
) (sqlc.SpecArtifact, error) {
	f.calls = append(f.calls, "upsert_artifact")
	f.upsert = arg
	return sqlc.SpecArtifact{}, nil
}

func (f *fakeSpecPersistenceQueries) DeleteEndpointIndexByRevision(_ context.Context, revisionID int64) error {
	f.calls = append(f.calls, "delete_endpoint_index")
	f.deletedRevisionID = revisionID
	return nil
}

func (f *fakeSpecPersistenceQueries) InsertEndpointIndex(
	_ context.Context,
	arg sqlc.InsertEndpointIndexParams,
) (sqlc.EndpointIndex, error) {
	f.calls = append(f.calls, "insert_endpoint_index")
	f.inserted = append(f.inserted, arg)
	return sqlc.EndpointIndex{
		ID:          int64(len(f.inserted)),
		RevisionID:  arg.RevisionID,
		Method:      arg.Method,
		Path:        arg.Path,
		OperationID: pgtype.Text{String: arg.OperationID.String, Valid: arg.OperationID.Valid},
		Summary:     pgtype.Text{String: arg.Summary.String, Valid: arg.Summary.Valid},
		Deprecated:  arg.Deprecated,
		RawJson:     arg.RawJson,
	}, nil
}
