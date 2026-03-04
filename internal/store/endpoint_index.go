package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
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

func (s *Store) GetEndpointIndexByMethodPath(
	ctx context.Context,
	revisionID int64,
	method string,
	path string,
) (EndpointIndexRecord, bool, error) {
	if s == nil || !s.configured || s.pool == nil {
		return EndpointIndexRecord{}, false, ErrStoreNotConfigured
	}
	if revisionID < 1 {
		return EndpointIndexRecord{}, false, errors.New("revision id must be positive")
	}

	method = strings.ToLower(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if method == "" {
		return EndpointIndexRecord{}, false, errors.New("method must not be empty")
	}
	if path == "" {
		return EndpointIndexRecord{}, false, errors.New("path must not be empty")
	}

	row, err := sqlc.New(s.pool).GetEndpointByMethodPath(ctx, sqlc.GetEndpointByMethodPathParams{
		RevisionID: revisionID,
		Method:     method,
		Path:       path,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EndpointIndexRecord{}, false, nil
		}
		return EndpointIndexRecord{}, false, fmt.Errorf(
			"get endpoint index for revision %d method=%s path=%s: %w",
			revisionID,
			method,
			path,
			err,
		)
	}

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

	return record, true, nil
}
