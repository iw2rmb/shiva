-- name: CountRevisions :one
SELECT COUNT(*)::BIGINT
FROM revisions;

-- name: CreateRevision :one
INSERT INTO revisions (
    repo_id,
    sha,
    branch,
    parent_sha
)
VALUES (
    sqlc.arg(repo_id),
    sqlc.arg(sha),
    sqlc.arg(branch),
    sqlc.narg(parent_sha)
)
ON CONFLICT (repo_id, sha) DO UPDATE
SET branch = EXCLUDED.branch,
    parent_sha = EXCLUDED.parent_sha
RETURNING id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error, created_at;

-- name: GetRevisionByRepoSHA :one
SELECT id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error, created_at
FROM revisions
WHERE repo_id = sqlc.arg(repo_id)
  AND sha = sqlc.arg(sha);

-- name: GetRevisionByRepoSHAPrefix :one
SELECT id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error, created_at
FROM revisions
WHERE repo_id = sqlc.arg(repo_id)
  AND sha LIKE sqlc.arg(sha_prefix) || '%'
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: GetRevisionByID :one
SELECT id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error, created_at
FROM revisions
WHERE id = sqlc.arg(id);

-- name: GetLatestRevisionByBranch :one
SELECT id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error, created_at
FROM revisions
WHERE repo_id = sqlc.arg(repo_id)
  AND branch = sqlc.arg(branch)
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: GetLatestProcessedRevisionByBranch :one
SELECT id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error, created_at
FROM revisions
WHERE repo_id = sqlc.arg(repo_id)
  AND branch = sqlc.arg(branch)
  AND status = 'processed'
ORDER BY processed_at DESC NULLS LAST, id DESC
LIMIT 1;

-- name: GetLatestProcessedOpenAPIRevisionByBranchExcludingID :one
SELECT id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error, created_at
FROM revisions
WHERE repo_id = sqlc.arg(repo_id)
  AND branch = sqlc.arg(branch)
  AND status = 'processed'
  AND openapi_changed = TRUE
  AND id <> sqlc.arg(exclude_revision_id)
ORDER BY processed_at DESC NULLS LAST, id DESC
LIMIT 1;

-- name: MarkRevisionProcessed :one
UPDATE revisions
SET processed_at = NOW(),
    openapi_changed = sqlc.arg(openapi_changed),
    status = 'processed',
    error = ''
WHERE id = sqlc.arg(id)
RETURNING id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error, created_at;

-- name: MarkRevisionFailed :one
UPDATE revisions
SET processed_at = NOW(),
    status = 'failed',
    error = sqlc.arg(error)
WHERE id = sqlc.arg(id)
RETURNING id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error, created_at;
