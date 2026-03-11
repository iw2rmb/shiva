-- name: CreateRepo :one
INSERT INTO repos (
    gitlab_project_id,
    path_with_namespace,
    default_branch
)
VALUES (
    sqlc.arg(gitlab_project_id),
    sqlc.arg(path_with_namespace),
    sqlc.arg(default_branch)
)
RETURNING id, gitlab_project_id, path_with_namespace, default_branch, openapi_force_rescan, created_at, updated_at;

-- name: GetRepoByPath :one
SELECT id, gitlab_project_id, path_with_namespace, default_branch, openapi_force_rescan, created_at, updated_at
FROM repos
WHERE path_with_namespace = sqlc.arg(path_with_namespace);

-- name: GetRepoByProjectID :one
SELECT id, gitlab_project_id, path_with_namespace, default_branch, openapi_force_rescan, created_at, updated_at
FROM repos
WHERE gitlab_project_id = sqlc.arg(gitlab_project_id);

-- name: GetRepoByID :one
SELECT id, gitlab_project_id, path_with_namespace, default_branch, openapi_force_rescan, created_at, updated_at
FROM repos
WHERE id = sqlc.arg(id);

-- name: UpdateRepoMetadata :one
UPDATE repos
SET path_with_namespace = sqlc.arg(path_with_namespace),
    default_branch = sqlc.arg(default_branch),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING id, gitlab_project_id, path_with_namespace, default_branch, openapi_force_rescan, created_at, updated_at;

-- name: GetRepoBootstrapState :one
SELECT
    (
        SELECT COUNT(*)::BIGINT
        FROM api_specs
        WHERE repo_id = sqlc.arg(repo_id)
          AND status = 'active'
    ) AS active_api_count,
    openapi_force_rescan AS force_rescan
FROM repos
WHERE id = sqlc.arg(repo_id);

-- name: ClearRepoForceRescan :exec
UPDATE repos
SET openapi_force_rescan = FALSE,
    updated_at = NOW()
WHERE id = sqlc.arg(repo_id);
