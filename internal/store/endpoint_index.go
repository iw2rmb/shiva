package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
)

func (s *Store) ListEndpointIndexByRevision(ctx context.Context, revisionID int64) ([]EndpointIndexRecord, error) {
	if s == nil || !s.configured || s.pool == nil {
		return nil, ErrStoreNotConfigured
	}
	if revisionID < 1 {
		return nil, errors.New("revision id must be positive")
	}

	rows, err := sqlc.New(s.pool).ListEndpointIndexByRevision(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("list endpoint index for revision %d: %w", revisionID, err)
	}

	endpoints := make([]EndpointIndexRecord, 0, len(rows))
	for _, row := range rows {
		record := EndpointIndexRecord{
			Method:     row.Method,
			Path:       row.Path,
			Deprecated: row.Deprecated,
			RawJSON:    bytesCopy(row.RawJson),
		}
		if row.OperationID.Valid {
			record.OperationID = row.OperationID.String
		}
		if row.Summary.Valid {
			record.Summary = row.Summary.String
		}
		endpoints = append(endpoints, record)
	}

	return endpoints, nil
}
