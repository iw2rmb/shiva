package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Store) GetAPISpecRevisionIDByRepoAndRootPath(
	ctx context.Context,
	repoID int64,
	apiRootPath string,
	revisionID int64,
) (int64, error) {
	if s == nil || !s.configured || s.pool == nil {
		return 0, ErrStoreNotConfigured
	}
	if repoID < 1 {
		return 0, errors.New("repo id must be positive")
	}
	apiRootPath = strings.TrimSpace(apiRootPath)
	if apiRootPath == "" {
		return 0, errors.New("api root path must not be empty")
	}
	if revisionID < 1 {
		return 0, errors.New("revision id must be positive")
	}

	var apiSpecRevisionID int64
	err := s.pool.QueryRow(
		ctx,
		`
		SELECT api_spec_revisions.id
		FROM api_spec_revisions
		JOIN api_specs ON api_specs.id = api_spec_revisions.api_spec_id
		WHERE api_specs.repo_id = $1
		  AND api_specs.root_path = $2
		  AND api_spec_revisions.ingest_event_id = $3
		  AND api_specs.status = 'active'
		  AND api_spec_revisions.build_status = 'processed'
		ORDER BY api_spec_revisions.id DESC
		LIMIT 1
		`,
		repoID,
		apiRootPath,
		revisionID,
	).Scan(&apiSpecRevisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf(
				"%w: repo_id=%d api=%q ingest_event_id=%d",
				ErrAPISpecNotFound,
				repoID,
				apiRootPath,
				revisionID,
			)
		}
		return 0, fmt.Errorf(
			"resolve api spec revision for repo_id=%d api=%q ingest_event_id=%d: %w",
			repoID,
			apiRootPath,
			revisionID,
			err,
		)
	}

	return apiSpecRevisionID, nil
}
