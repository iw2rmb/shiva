-- name: CountActiveAPISpecsByRepo :one
SELECT COUNT(*)::BIGINT
FROM api_specs
WHERE repo_id = sqlc.arg(repo_id)
  AND status = 'active';

-- name: UpsertAPISpec :one
INSERT INTO api_specs (
    repo_id,
    root_path,
    status
)
VALUES (
    sqlc.arg(repo_id),
    sqlc.arg(root_path),
    'active'
)
ON CONFLICT (repo_id, root_path) DO UPDATE
SET status = 'active',
    updated_at = NOW()
RETURNING id, repo_id, root_path, status, display_name, created_at, updated_at;

-- name: CreateAPISpecRevision :one
INSERT INTO api_spec_revisions (
    api_spec_id,
    revision_id,
    root_path_at_revision,
    build_status,
    error
)
SELECT
    sqlc.arg(api_spec_id),
    sqlc.arg(revision_id),
    api_specs.root_path,
    sqlc.arg(build_status),
    sqlc.arg(error)
FROM api_specs
WHERE api_specs.id = sqlc.arg(api_spec_id)
ON CONFLICT (api_spec_id, revision_id) DO UPDATE
SET root_path_at_revision = EXCLUDED.root_path_at_revision,
    build_status = EXCLUDED.build_status,
    error = EXCLUDED.error,
    updated_at = NOW()
RETURNING id, api_spec_id, revision_id, root_path_at_revision, build_status, error, created_at, updated_at;

-- name: ReplaceAPISpecDependencies :exec
WITH replaced AS (
    DELETE FROM api_spec_dependencies
    WHERE api_spec_revision_id = sqlc.arg(api_spec_revision_id)
)
INSERT INTO api_spec_dependencies (
    api_spec_revision_id,
    file_path
)
SELECT
    sqlc.arg(api_spec_revision_id),
    dependency_path
FROM UNNEST(sqlc.arg(file_paths)::TEXT[]) AS dependency_path
ON CONFLICT (api_spec_revision_id, file_path) DO NOTHING;

-- name: ListActiveAPISpecsWithLatestDependencies :many
WITH active_specs AS (
    SELECT id, repo_id, root_path, status, display_name, created_at, updated_at
    FROM api_specs
    WHERE repo_id = sqlc.arg(repo_id)
      AND status = 'active'
),
latest_processed AS (
    SELECT DISTINCT ON (api_spec_revisions.api_spec_id)
        api_spec_revisions.id,
        api_spec_revisions.api_spec_id
    FROM api_spec_revisions
    JOIN active_specs ON active_specs.id = api_spec_revisions.api_spec_id
    WHERE api_spec_revisions.build_status = 'processed'
    ORDER BY api_spec_revisions.api_spec_id, api_spec_revisions.revision_id DESC, api_spec_revisions.id DESC
)
SELECT
    active_specs.id,
    active_specs.repo_id,
    active_specs.root_path,
    active_specs.status,
    active_specs.display_name,
    active_specs.created_at,
    active_specs.updated_at,
    COALESCE(dependencies.dependency_paths, ARRAY[]::TEXT[])::TEXT[] AS dependency_paths
FROM active_specs
LEFT JOIN latest_processed ON latest_processed.api_spec_id = active_specs.id
LEFT JOIN LATERAL (
    SELECT ARRAY_AGG(api_spec_dependencies.file_path ORDER BY api_spec_dependencies.file_path)::TEXT[] AS dependency_paths
    FROM api_spec_dependencies
    WHERE api_spec_dependencies.api_spec_revision_id = latest_processed.id
) AS dependencies ON TRUE
ORDER BY active_specs.root_path ASC;

-- name: MarkAPISpecDeleted :execrows
UPDATE api_specs
SET status = 'deleted',
    updated_at = NOW()
WHERE id = sqlc.arg(api_spec_id);

-- name: ListAPISpecListingByRepo :many
WITH repo_specs AS (
    SELECT id, root_path, status
    FROM api_specs
    WHERE api_specs.repo_id = sqlc.arg(target_repo_id)
),
latest_processed AS (
    SELECT DISTINCT ON (api_spec_revisions.api_spec_id)
        api_spec_revisions.api_spec_id,
        api_spec_revisions.id AS api_spec_revision_id,
        api_spec_revisions.revision_id
    FROM api_spec_revisions
    JOIN repo_specs ON repo_specs.id = api_spec_revisions.api_spec_id
    WHERE api_spec_revisions.build_status = 'processed'
    ORDER BY api_spec_revisions.api_spec_id, api_spec_revisions.revision_id DESC, api_spec_revisions.id DESC
)
SELECT
    repo_specs.id AS api_spec_id,
    repo_specs.root_path AS api,
    repo_specs.status,
    latest_processed.api_spec_revision_id,
    ingest_events.id AS revision_id,
    ingest_events.sha AS revision_sha,
    ingest_events.branch AS revision_branch
FROM repo_specs
LEFT JOIN latest_processed ON latest_processed.api_spec_id = repo_specs.id
LEFT JOIN ingest_events ON ingest_events.id = latest_processed.revision_id
ORDER BY repo_specs.root_path ASC;
