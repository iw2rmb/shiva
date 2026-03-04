package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
)

type Tenant struct {
	ID  int64
	Key string
}

func (s *Store) GetTenantByID(ctx context.Context, tenantID int64) (Tenant, error) {
	if s == nil || !s.configured || s.pool == nil {
		return Tenant{}, ErrStoreNotConfigured
	}
	if tenantID < 1 {
		return Tenant{}, errors.New("tenant id must be positive")
	}

	tenant, err := sqlc.New(s.pool).GetTenantByID(ctx, tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Tenant{}, fmt.Errorf("tenant not found: id=%d", tenantID)
		}
		return Tenant{}, fmt.Errorf("get tenant by id %d: %w", tenantID, err)
	}

	return Tenant{
		ID:  tenant.ID,
		Key: tenant.Key,
	}, nil
}
