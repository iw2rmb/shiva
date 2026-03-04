package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type PersistSpecChangeInput struct {
	RepoID         int64
	FromRevisionID *int64
	ToRevisionID   int64
	ChangeJSON     []byte
}

type normalizedPersistSpecChangeInput struct {
	RepoID         int64
	FromRevisionID pgtype.Int8
	ToRevisionID   int64
	ChangeJSON     []byte
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
		RepoID:         input.RepoID,
		FromRevisionID: input.FromRevisionID,
		ToRevisionID:   input.ToRevisionID,
		ChangeJson:     input.ChangeJSON,
	}); err != nil {
		return fmt.Errorf("persist spec change for revision %d: %w", input.ToRevisionID, err)
	}
	return nil
}

func normalizePersistSpecChangeInput(input PersistSpecChangeInput) (normalizedPersistSpecChangeInput, error) {
	if input.RepoID < 1 {
		return normalizedPersistSpecChangeInput{}, errors.New("repo id must be positive")
	}
	if input.ToRevisionID < 1 {
		return normalizedPersistSpecChangeInput{}, errors.New("to revision id must be positive")
	}

	fromRevision := pgtype.Int8{}
	if input.FromRevisionID != nil {
		if *input.FromRevisionID < 1 {
			return normalizedPersistSpecChangeInput{}, errors.New("from revision id must be positive when set")
		}
		fromRevision = pgtype.Int8{
			Int64: *input.FromRevisionID,
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
		RepoID:         input.RepoID,
		FromRevisionID: fromRevision,
		ToRevisionID:   input.ToRevisionID,
		ChangeJSON:     canonicalChangeJSON,
	}, nil
}
