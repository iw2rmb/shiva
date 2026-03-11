package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
)

type Subscription struct {
	ID                    int64
	RepoID                int64
	TargetURL             string
	Secret                string
	Enabled               bool
	MaxAttempts           int32
	BackoffInitialSeconds int32
	BackoffMaxSeconds     int32
}

func (s *Store) ListEnabledSubscriptionsByRepo(
	ctx context.Context,
	repoID int64,
) ([]Subscription, error) {
	if s == nil || !s.configured || s.pool == nil {
		return nil, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return nil, errors.New("repo id must be positive")
	}

	rows, err := sqlc.New(s.pool).ListEnabledSubscriptionsByRepo(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("list enabled subscriptions for repo %d: %w", repoID, err)
	}

	subscriptions := make([]Subscription, 0, len(rows))
	for _, row := range rows {
		subscriptions = append(subscriptions, mapSubscription(row))
	}
	return subscriptions, nil
}

func mapSubscription(row sqlc.Subscription) Subscription {
	return Subscription{
		ID:                    row.ID,
		RepoID:                row.RepoID,
		TargetURL:             row.TargetUrl,
		Secret:                row.Secret,
		Enabled:               row.Enabled,
		MaxAttempts:           row.MaxAttempts,
		BackoffInitialSeconds: row.BackoffInitialSeconds,
		BackoffMaxSeconds:     row.BackoffMaxSeconds,
	}
}
