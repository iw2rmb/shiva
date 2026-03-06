package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iw2rmb/shiva/internal/store/sqlc"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ListEndpointIndexByAPISpecRevision(ctx context.Context, apiSpecRevisionID int64) ([]EndpointIndexRecord, error) {
	if s == nil || !s.configured || s.pool == nil {
		return nil, ErrStoreNotConfigured
	}
	if apiSpecRevisionID < 1 {
		return nil, errors.New("api spec revision id must be positive")
	}

	rows, err := sqlc.New(s.pool).ListEndpointIndexByAPISpecRevision(ctx, apiSpecRevisionID)
	if err != nil {
		return nil, fmt.Errorf("list endpoint index for api_spec_revision_id=%d: %w", apiSpecRevisionID, err)
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

func (s *Store) GetEndpointIndexByMethodPathForAPISpecRevision(
	ctx context.Context,
	apiSpecRevisionID int64,
	method string,
	path string,
) (EndpointIndexRecord, bool, error) {
	if s == nil || !s.configured || s.pool == nil {
		return EndpointIndexRecord{}, false, ErrStoreNotConfigured
	}
	if apiSpecRevisionID < 1 {
		return EndpointIndexRecord{}, false, errors.New("api spec revision id must be positive")
	}

	method = strings.ToLower(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if method == "" {
		return EndpointIndexRecord{}, false, errors.New("method must not be empty")
	}
	if path == "" {
		return EndpointIndexRecord{}, false, errors.New("path must not be empty")
	}

	row, err := sqlc.New(s.pool).GetEndpointByMethodPathForAPISpecRevision(ctx, sqlc.GetEndpointByMethodPathForAPISpecRevisionParams{
		ApiSpecRevisionID: apiSpecRevisionID,
		Method:            method,
		Path:              path,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EndpointIndexRecord{}, false, nil
		}
		return EndpointIndexRecord{}, false, fmt.Errorf(
			"get endpoint index for api_spec_revision_id=%d method=%s path=%s: %w",
			apiSpecRevisionID,
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
