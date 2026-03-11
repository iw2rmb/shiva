package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetStartupIndexLastProjectID(ctx context.Context) (int64, error) {
	if s == nil || !s.configured || s.pool == nil {
		return 0, ErrStoreNotConfigured
	}

	return getStartupIndexLastProjectID(ctx, sqlc.New(s.pool))
}

type startupIndexLastProjectIDQueries interface {
	GetStartupIndexLastProjectID(ctx context.Context) (int64, error)
}

func getStartupIndexLastProjectID(ctx context.Context, queries startupIndexLastProjectIDQueries) (int64, error) {
	lastProjectID, err := queries.GetStartupIndexLastProjectID(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("get startup index last project id: %w", err)
	}
	return lastProjectID, nil
}

func (s *Store) AdvanceStartupIndexLastProjectID(ctx context.Context, lastProjectID int64) error {
	if s == nil || !s.configured || s.pool == nil {
		return ErrStoreNotConfigured
	}
	if lastProjectID < 1 {
		return errors.New("last project id must be positive")
	}

	return advanceStartupIndexLastProjectID(ctx, sqlc.New(s.pool), lastProjectID)
}

type advanceStartupIndexLastProjectIDQueries interface {
	AdvanceStartupIndexLastProjectID(ctx context.Context, lastProjectID int64) error
}

func advanceStartupIndexLastProjectID(
	ctx context.Context,
	queries advanceStartupIndexLastProjectIDQueries,
	lastProjectID int64,
) error {
	if err := queries.AdvanceStartupIndexLastProjectID(ctx, lastProjectID); err != nil {
		return fmt.Errorf("advance startup index last project id to %d: %w", lastProjectID, err)
	}
	return nil
}
