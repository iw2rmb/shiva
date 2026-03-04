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
	RepoID         int64
	FromRevisionID *int64
	ToRevisionID   int64
	ChangeJSON     []byte
}

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

func (s *Store) GetSpecChangeByToRevision(ctx context.Context, toRevisionID int64) (SpecChange, error) {
	if s == nil || !s.configured || s.pool == nil {
		return SpecChange{}, ErrStoreNotConfigured
	}
	if toRevisionID < 1 {
		return SpecChange{}, errors.New("to revision id must be positive")
	}

	row, err := sqlc.New(s.pool).GetSpecChangeByToRevision(ctx, toRevisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SpecChange{}, fmt.Errorf("spec change not found for revision %d", toRevisionID)
		}
		return SpecChange{}, fmt.Errorf("get spec change for revision %d: %w", toRevisionID, err)
	}

	var fromRevisionID *int64
	if row.FromRevisionID.Valid {
		value := row.FromRevisionID.Int64
		fromRevisionID = &value
	}

	return SpecChange{
		RepoID:         row.RepoID,
		FromRevisionID: fromRevisionID,
		ToRevisionID:   row.ToRevisionID,
		ChangeJSON:     bytesCopy(row.ChangeJson),
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
