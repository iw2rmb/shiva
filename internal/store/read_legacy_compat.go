package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// GetSpecArtifactByRevisionID is a compatibility bridge for legacy read routes.
// It resolves the most recent API-scoped artifact row persisted for the revision.
func (s *Store) GetSpecArtifactByRevisionID(ctx context.Context, revisionID int64) (SpecArtifact, error) {
	ingestEventID := revisionID

	if s == nil || !s.configured || s.pool == nil {
		return SpecArtifact{}, ErrStoreNotConfigured
	}
	if ingestEventID < 1 {
		return SpecArtifact{}, errors.New("ingest event id must be positive")
	}

	var artifact SpecArtifact
	err := s.pool.QueryRow(
		ctx,
		`
		SELECT
			spec_artifacts.api_spec_revision_id,
			spec_artifacts.spec_json,
			spec_artifacts.spec_yaml,
			spec_artifacts.etag,
			spec_artifacts.size_bytes
		FROM spec_artifacts
		JOIN api_spec_revisions ON api_spec_revisions.id = spec_artifacts.api_spec_revision_id
		WHERE api_spec_revisions.ingest_event_id = $1
		ORDER BY api_spec_revisions.id DESC
		LIMIT 1
		`,
		ingestEventID,
	).Scan(
		&artifact.APISpecRevisionID,
		&artifact.SpecJSON,
		&artifact.SpecYAML,
		&artifact.ETag,
		&artifact.SizeBytes,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SpecArtifact{}, fmt.Errorf("%w: ingest_event_id=%d", ErrSpecArtifactNotFound, ingestEventID)
		}
		return SpecArtifact{}, fmt.Errorf("get spec artifact by ingest_event_id=%d: %w", ingestEventID, err)
	}

	artifact.SpecJSON = bytesCopy(artifact.SpecJSON)
	return artifact, nil
}

// GetEndpointIndexByMethodPath is a compatibility bridge for legacy read routes.
// It resolves the endpoint from the most recent API-scoped index row for the revision.
func (s *Store) GetEndpointIndexByMethodPath(
	ctx context.Context,
	revisionID int64,
	method string,
	path string,
) (EndpointIndexRecord, bool, error) {
	ingestEventID := revisionID

	if s == nil || !s.configured || s.pool == nil {
		return EndpointIndexRecord{}, false, ErrStoreNotConfigured
	}
	if ingestEventID < 1 {
		return EndpointIndexRecord{}, false, errors.New("ingest event id must be positive")
	}

	method = strings.ToLower(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if method == "" {
		return EndpointIndexRecord{}, false, errors.New("method must not be empty")
	}
	if path == "" {
		return EndpointIndexRecord{}, false, errors.New("path must not be empty")
	}

	var (
		record      EndpointIndexRecord
		operationID *string
		summary     *string
	)

	err := s.pool.QueryRow(
		ctx,
		`
		SELECT
			endpoint_index.method,
			endpoint_index.path,
			endpoint_index.operation_id,
			endpoint_index.summary,
			endpoint_index.deprecated,
			endpoint_index.raw_json
		FROM endpoint_index
		JOIN api_spec_revisions ON api_spec_revisions.id = endpoint_index.api_spec_revision_id
		WHERE api_spec_revisions.ingest_event_id = $1
		  AND endpoint_index.method = $2
		  AND endpoint_index.path = $3
		ORDER BY api_spec_revisions.id DESC, endpoint_index.id DESC
		LIMIT 1
		`,
		ingestEventID,
		method,
		path,
	).Scan(
		&record.Method,
		&record.Path,
		&operationID,
		&summary,
		&record.Deprecated,
		&record.RawJSON,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EndpointIndexRecord{}, false, nil
		}
		return EndpointIndexRecord{}, false, fmt.Errorf(
			"get endpoint index by ingest_event_id=%d method=%s path=%s: %w",
			ingestEventID,
			method,
			path,
			err,
		)
	}

	if operationID != nil {
		record.OperationID = *operationID
	}
	if summary != nil {
		record.Summary = *summary
	}
	record.RawJSON = bytesCopy(record.RawJSON)

	return record, true, nil
}
