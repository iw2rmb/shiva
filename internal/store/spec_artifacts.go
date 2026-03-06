package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
)

var ErrSpecArtifactNotFound = errors.New("spec artifact not found")

type EndpointIndexRecord struct {
	Method      string
	Path        string
	OperationID string
	Summary     string
	Deprecated  bool
	RawJSON     []byte
}

type SpecArtifact struct {
	APISpecRevisionID int64
	SpecJSON          []byte
	SpecYAML          string
	ETag              string
	SizeBytes         int64
}

type PersistCanonicalSpecInput struct {
	APISpecRevisionID int64
	SpecJSON          []byte
	SpecYAML          string
	ETag              string
	SizeBytes         int64
	Endpoints         []EndpointIndexRecord
}

type normalizedPersistCanonicalSpecInput struct {
	APISpecRevisionID int64
	SpecJSON          []byte
	SpecYAML          string
	ETag              string
	SizeBytes         int64
	Endpoints         []EndpointIndexRecord
}

func (s *Store) GetSpecArtifactByAPISpecRevisionID(ctx context.Context, apiSpecRevisionID int64) (SpecArtifact, error) {
	if s == nil || !s.configured || s.pool == nil {
		return SpecArtifact{}, ErrStoreNotConfigured
	}
	if apiSpecRevisionID < 1 {
		return SpecArtifact{}, errors.New("api spec revision id must be positive")
	}

	artifact, err := sqlc.New(s.pool).GetSpecArtifactByAPISpecRevisionID(ctx, apiSpecRevisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SpecArtifact{}, fmt.Errorf("%w: api_spec_revision_id=%d", ErrSpecArtifactNotFound, apiSpecRevisionID)
		}
		return SpecArtifact{}, fmt.Errorf("get spec artifact by api_spec_revision_id=%d: %w", apiSpecRevisionID, err)
	}

	return SpecArtifact{
		APISpecRevisionID: artifact.ApiSpecRevisionID,
		SpecJSON:          bytesCopy(artifact.SpecJson),
		SpecYAML:          artifact.SpecYaml,
		ETag:              artifact.Etag,
		SizeBytes:         artifact.SizeBytes,
	}, nil
}

func (s *Store) PersistCanonicalSpec(ctx context.Context, input PersistCanonicalSpecInput) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}

	normalized, err := normalizePersistCanonicalSpecInput(input)
	if err != nil {
		return err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin canonical spec persistence transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := persistCanonicalSpec(ctx, sqlc.New(tx), normalized); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit canonical spec persistence transaction: %w", err)
	}
	return nil
}

type specPersistenceQueries interface {
	UpsertSpecArtifact(ctx context.Context, arg sqlc.UpsertSpecArtifactParams) (sqlc.SpecArtifact, error)
	DeleteEndpointIndexByAPISpecRevision(ctx context.Context, apiSpecRevisionID int64) error
	InsertEndpointIndex(ctx context.Context, arg sqlc.InsertEndpointIndexParams) (sqlc.EndpointIndex, error)
}

func persistCanonicalSpec(
	ctx context.Context,
	queries specPersistenceQueries,
	input normalizedPersistCanonicalSpecInput,
) error {
	if _, err := queries.UpsertSpecArtifact(ctx, sqlc.UpsertSpecArtifactParams{
		ApiSpecRevisionID: input.APISpecRevisionID,
		SpecJson:          input.SpecJSON,
		SpecYaml:          input.SpecYAML,
		Etag:              input.ETag,
		SizeBytes:         input.SizeBytes,
	}); err != nil {
		return fmt.Errorf("upsert spec artifact for api_spec_revision_id=%d: %w", input.APISpecRevisionID, err)
	}

	if err := queries.DeleteEndpointIndexByAPISpecRevision(ctx, input.APISpecRevisionID); err != nil {
		return fmt.Errorf("delete endpoint index for api_spec_revision_id=%d: %w", input.APISpecRevisionID, err)
	}

	for _, endpoint := range input.Endpoints {
		if _, err := queries.InsertEndpointIndex(ctx, sqlc.InsertEndpointIndexParams{
			ApiSpecRevisionID: input.APISpecRevisionID,
			Method:            endpoint.Method,
			Path:              endpoint.Path,
			OperationID:       nullableText(endpoint.OperationID),
			Summary:           nullableText(endpoint.Summary),
			Deprecated:        endpoint.Deprecated,
			RawJson:           endpoint.RawJSON,
		}); err != nil {
			return fmt.Errorf(
				"insert endpoint index for api_spec_revision_id=%d method=%s path=%s: %w",
				input.APISpecRevisionID,
				endpoint.Method,
				endpoint.Path,
				err,
			)
		}
	}

	return nil
}

func normalizePersistCanonicalSpecInput(input PersistCanonicalSpecInput) (normalizedPersistCanonicalSpecInput, error) {
	if input.APISpecRevisionID < 1 {
		return normalizedPersistCanonicalSpecInput{}, errors.New("api spec revision id must be positive")
	}

	specJSON := bytesCopy(input.SpecJSON)
	if len(specJSON) == 0 {
		return normalizedPersistCanonicalSpecInput{}, errors.New("canonical spec json must not be empty")
	}
	var jsonProbe any
	if err := json.Unmarshal(specJSON, &jsonProbe); err != nil {
		return normalizedPersistCanonicalSpecInput{}, fmt.Errorf("canonical spec json is invalid: %w", err)
	}

	specYAML := strings.TrimSpace(input.SpecYAML)
	if specYAML == "" {
		return normalizedPersistCanonicalSpecInput{}, errors.New("canonical spec yaml must not be empty")
	}
	specYAML += "\n"

	etag := strings.TrimSpace(input.ETag)
	if etag == "" {
		return normalizedPersistCanonicalSpecInput{}, errors.New("canonical spec etag must not be empty")
	}
	if input.SizeBytes < 0 {
		return normalizedPersistCanonicalSpecInput{}, errors.New("canonical spec size_bytes must be >= 0")
	}

	normalizedEndpoints := make([]EndpointIndexRecord, 0, len(input.Endpoints))
	seenKeys := make(map[string]struct{}, len(input.Endpoints))
	for _, endpoint := range input.Endpoints {
		method := strings.ToLower(strings.TrimSpace(endpoint.Method))
		path := strings.TrimSpace(endpoint.Path)
		if method == "" {
			return normalizedPersistCanonicalSpecInput{}, errors.New("endpoint method must not be empty")
		}
		if path == "" {
			return normalizedPersistCanonicalSpecInput{}, errors.New("endpoint path must not be empty")
		}

		rawJSON := bytesCopy(endpoint.RawJSON)
		if len(rawJSON) == 0 {
			return normalizedPersistCanonicalSpecInput{}, fmt.Errorf("endpoint raw_json must not be empty for %s %s", method, path)
		}
		var operation any
		if err := json.Unmarshal(rawJSON, &operation); err != nil {
			return normalizedPersistCanonicalSpecInput{}, fmt.Errorf(
				"endpoint raw_json is invalid for %s %s: %w",
				method,
				path,
				err,
			)
		}
		canonicalRawJSON, err := json.Marshal(operation)
		if err != nil {
			return normalizedPersistCanonicalSpecInput{}, fmt.Errorf(
				"marshal endpoint raw_json for %s %s: %w",
				method,
				path,
				err,
			)
		}

		key := method + "\x00" + path
		if _, exists := seenKeys[key]; exists {
			return normalizedPersistCanonicalSpecInput{}, fmt.Errorf("duplicate endpoint index key: method=%s path=%s", method, path)
		}
		seenKeys[key] = struct{}{}

		normalizedEndpoints = append(normalizedEndpoints, EndpointIndexRecord{
			Method:      method,
			Path:        path,
			OperationID: strings.TrimSpace(endpoint.OperationID),
			Summary:     strings.TrimSpace(endpoint.Summary),
			Deprecated:  endpoint.Deprecated,
			RawJSON:     canonicalRawJSON,
		})
	}

	sort.SliceStable(normalizedEndpoints, func(i, j int) bool {
		if normalizedEndpoints[i].Method == normalizedEndpoints[j].Method {
			return normalizedEndpoints[i].Path < normalizedEndpoints[j].Path
		}
		return normalizedEndpoints[i].Method < normalizedEndpoints[j].Method
	})

	return normalizedPersistCanonicalSpecInput{
		APISpecRevisionID: input.APISpecRevisionID,
		SpecJSON:          specJSON,
		SpecYAML:          specYAML,
		ETag:              etag,
		SizeBytes:         input.SizeBytes,
		Endpoints:         normalizedEndpoints,
	}, nil
}

func bytesCopy(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	copied := make([]byte, len(value))
	copy(copied, value)
	return copied
}
