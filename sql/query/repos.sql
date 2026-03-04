-- name: CreateRepo :one
INSERT INTO repos (
    tenant_id,
    gitlab_project_id,
    path_with_namespace,
    default_branch
)
VALUES (
    sqlc.arg(tenant_id),
    sqlc.arg(gitlab_project_id),
    sqlc.arg(path_with_namespace),
    sqlc.arg(default_branch)
)
RETURNING id, tenant_id, gitlab_project_id, path_with_namespace, default_branch, openapi_force_rescan, created_at, updated_at;

-- name: GetRepoByTenantAndPath :one
SELECT id, tenant_id, gitlab_project_id, path_with_namespace, default_branch, openapi_force_rescan, created_at, updated_at
FROM repos
WHERE tenant_id = sqlc.arg(tenant_id)
  AND path_with_namespace = sqlc.arg(path_with_namespace);

-- name: GetRepoByTenantAndProjectID :one
SELECT id, tenant_id, gitlab_project_id, path_with_namespace, default_branch, openapi_force_rescan, created_at, updated_at
FROM repos
WHERE tenant_id = sqlc.arg(tenant_id)
  AND gitlab_project_id = sqlc.arg(gitlab_project_id);

-- name: GetRepoByID :one
SELECT id, tenant_id, gitlab_project_id, path_with_namespace, default_branch, openapi_force_rescan, created_at, updated_at
FROM repos
WHERE id = sqlc.arg(id);

-- name: UpdateRepoDefaultBranch :one
UPDATE repos
SET default_branch = sqlc.arg(default_branch),
    updated_at = NOW()
WHERE id = sqlc.arg(id)
RETURNING id, tenant_id, gitlab_project_id, path_with_namespace, default_branch, openapi_force_rescan, created_at, updated_at;

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
