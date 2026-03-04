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
