-- name: CreateRepo :one
WITH resolved_namespace AS (
    INSERT INTO namespaces (namespace)
    VALUES (sqlc.arg(namespace))
    ON CONFLICT (namespace) DO UPDATE
    SET updated_at = NOW()
    RETURNING id
),
inserted_repo AS (
    INSERT INTO repos (
        gitlab_project_id,
        namespace_id,
        repo,
        default_branch
    )
    SELECT
        sqlc.arg(gitlab_project_id),
        resolved_namespace.id,
        sqlc.arg(repo),
        sqlc.arg(default_branch)
    FROM resolved_namespace
    RETURNING id, gitlab_project_id, namespace_id, repo, default_branch, openapi_force_rescan, created_at, updated_at
)
SELECT
    inserted_repo.id,
    inserted_repo.gitlab_project_id,
    namespaces.namespace,
    inserted_repo.namespace_id,
    inserted_repo.repo,
    inserted_repo.default_branch,
    inserted_repo.openapi_force_rescan,
    inserted_repo.created_at,
    inserted_repo.updated_at
FROM inserted_repo
JOIN namespaces ON namespaces.id = inserted_repo.namespace_id;

-- name: GetRepoByNamespaceAndRepo :one
SELECT
    repos.id,
    repos.gitlab_project_id,
    namespaces.namespace,
    repos.namespace_id,
    repos.repo,
    repos.default_branch,
    repos.openapi_force_rescan,
    repos.created_at,
    repos.updated_at
FROM repos
JOIN namespaces ON namespaces.id = repos.namespace_id
WHERE namespaces.namespace = sqlc.arg(namespace)
  AND repos.repo = sqlc.arg(repo);

-- name: GetRepoByProjectID :one
SELECT
    repos.id,
    repos.gitlab_project_id,
    namespaces.namespace,
    repos.namespace_id,
    repos.repo,
    repos.default_branch,
    repos.openapi_force_rescan,
    repos.created_at,
    repos.updated_at
FROM repos
JOIN namespaces ON namespaces.id = repos.namespace_id
WHERE repos.gitlab_project_id = sqlc.arg(gitlab_project_id);

-- name: GetRepoByID :one
SELECT
    repos.id,
    repos.gitlab_project_id,
    namespaces.namespace,
    repos.namespace_id,
    repos.repo,
    repos.default_branch,
    repos.openapi_force_rescan,
    repos.created_at,
    repos.updated_at
FROM repos
JOIN namespaces ON namespaces.id = repos.namespace_id
WHERE repos.id = sqlc.arg(id);

-- name: UpdateRepoMetadata :one
WITH resolved_namespace AS (
    INSERT INTO namespaces (namespace)
    VALUES (sqlc.arg(namespace))
    ON CONFLICT (namespace) DO UPDATE
    SET updated_at = NOW()
    RETURNING id
),
updated_repo AS (
UPDATE repos
SET namespace_id = resolved_namespace.id,
    repo = sqlc.arg(repo),
    default_branch = sqlc.arg(default_branch),
    updated_at = NOW()
FROM resolved_namespace
WHERE repos.id = sqlc.arg(id)
RETURNING repos.id, repos.gitlab_project_id, repos.namespace_id, repos.repo, repos.default_branch, repos.openapi_force_rescan, repos.created_at, repos.updated_at
)
SELECT
    updated_repo.id,
    updated_repo.gitlab_project_id,
    namespaces.namespace,
    updated_repo.namespace_id,
    updated_repo.repo,
    updated_repo.default_branch,
    updated_repo.openapi_force_rescan,
    updated_repo.created_at,
    updated_repo.updated_at
FROM updated_repo
JOIN namespaces ON namespaces.id = updated_repo.namespace_id;

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
