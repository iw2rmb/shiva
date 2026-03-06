package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type SpecChange struct {
	APISpecID             int64
	FromAPISpecRevisionID *int64
	ToAPISpecRevisionID   int64
	ChangeJSON            []byte
}

type PersistSpecChangeInput struct {
	APISpecID             int64
	FromAPISpecRevisionID *int64
	ToAPISpecRevisionID   int64
	ChangeJSON            []byte
}

type normalizedPersistSpecChangeInput struct {
	APISpecID             int64
	FromAPISpecRevisionID pgtype.Int8
	ToAPISpecRevisionID   int64
	ChangeJSON            []byte
}

func (s *Store) GetSpecChangeByToAPISpecRevision(ctx context.Context, toAPISpecRevisionID int64) (SpecChange, error) {
	if s == nil || !s.configured || s.pool == nil {
		return SpecChange{}, ErrStoreNotConfigured
	}
	if toAPISpecRevisionID < 1 {
		return SpecChange{}, errors.New("to api spec revision id must be positive")
	}

	row, err := sqlc.New(s.pool).GetSpecChangeByToAPISpecRevision(ctx, toAPISpecRevisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SpecChange{}, fmt.Errorf("spec change not found for api_spec_revision_id=%d", toAPISpecRevisionID)
		}
		return SpecChange{}, fmt.Errorf("get spec change for api_spec_revision_id=%d: %w", toAPISpecRevisionID, err)
	}

	var fromAPISpecRevisionID *int64
	if row.FromApiSpecRevisionID.Valid {
		value := row.FromApiSpecRevisionID.Int64
		fromAPISpecRevisionID = &value
	}

	return SpecChange{
		APISpecID:             row.ApiSpecID,
		FromAPISpecRevisionID: fromAPISpecRevisionID,
		ToAPISpecRevisionID:   row.ToApiSpecRevisionID,
		ChangeJSON:            bytesCopy(row.ChangeJson),
	}, nil
}

func (s *Store) PersistSpecChange(ctx context.Context, input PersistSpecChangeInput) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}

	normalized, err := normalizePersistSpecChangeInput(input)
	if err != nil {
		return err
	}

	if err := persistSpecChange(ctx, sqlc.New(s.pool), normalized); err != nil {
		return err
	}
	return nil
}

type specChangePersistenceQueries interface {
	CreateSpecChange(ctx context.Context, arg sqlc.CreateSpecChangeParams) (sqlc.SpecChange, error)
}

func persistSpecChange(
	ctx context.Context,
	queries specChangePersistenceQueries,
	input normalizedPersistSpecChangeInput,
) error {
	if _, err := queries.CreateSpecChange(ctx, sqlc.CreateSpecChangeParams{
		ApiSpecID:             input.APISpecID,
		FromApiSpecRevisionID: input.FromAPISpecRevisionID,
		ToApiSpecRevisionID:   input.ToAPISpecRevisionID,
		ChangeJson:            input.ChangeJSON,
	}); err != nil {
		return fmt.Errorf("persist spec change for to_api_spec_revision_id=%d: %w", input.ToAPISpecRevisionID, err)
	}
	return nil
}

func normalizePersistSpecChangeInput(input PersistSpecChangeInput) (normalizedPersistSpecChangeInput, error) {
	if input.APISpecID < 1 {
		return normalizedPersistSpecChangeInput{}, errors.New("api spec id must be positive")
	}
	if input.ToAPISpecRevisionID < 1 {
		return normalizedPersistSpecChangeInput{}, errors.New("to api spec revision id must be positive")
	}

	fromRevision := pgtype.Int8{}
	if input.FromAPISpecRevisionID != nil {
		if *input.FromAPISpecRevisionID < 1 {
			return normalizedPersistSpecChangeInput{}, errors.New("from api spec revision id must be positive when set")
		}
		fromRevision = pgtype.Int8{
			Int64: *input.FromAPISpecRevisionID,
			Valid: true,
		}
	}

	changeJSON := bytesCopy(input.ChangeJSON)
	if len(changeJSON) == 0 {
		return normalizedPersistSpecChangeInput{}, errors.New("spec change json must not be empty")
	}
	var probe any
	if err := json.Unmarshal(changeJSON, &probe); err != nil {
		return normalizedPersistSpecChangeInput{}, fmt.Errorf("spec change json is invalid: %w", err)
	}

	canonicalChangeJSON, err := json.Marshal(probe)
	if err != nil {
		return normalizedPersistSpecChangeInput{}, fmt.Errorf("marshal spec change json: %w", err)
	}

	return normalizedPersistSpecChangeInput{
		APISpecID:             input.APISpecID,
		FromAPISpecRevisionID: fromRevision,
		ToAPISpecRevisionID:   input.ToAPISpecRevisionID,
		ChangeJSON:            canonicalChangeJSON,
	}, nil
}
