-- name: GetRevisionByRepoSHA :one
SELECT id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, processed_at, openapi_changed, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND sha = sqlc.arg(sha);

-- name: GetRevisionByRepoSHAPrefix :one
SELECT id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, processed_at, openapi_changed, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND sha LIKE sqlc.arg(sha_prefix) || '%'
ORDER BY received_at DESC, id DESC
LIMIT 1;

-- name: GetRevisionStateByRepoSHAPrefix :one
SELECT id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND sha LIKE sqlc.arg(sha_prefix) || '%'
ORDER BY received_at DESC, id DESC
LIMIT 1;

-- name: GetRevisionByID :one
SELECT id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, processed_at, openapi_changed, status, error
FROM ingest_events
WHERE id = sqlc.arg(id);

-- name: GetRevisionStateByID :one
SELECT id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error
FROM ingest_events
WHERE id = sqlc.arg(id);

-- name: GetLatestRevisionByBranch :one
SELECT id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, processed_at, openapi_changed, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND branch = sqlc.arg(branch)
ORDER BY received_at DESC, id DESC
LIMIT 1;

-- name: GetLatestRevisionStateByBranch :one
SELECT id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND branch = sqlc.arg(branch)
ORDER BY received_at DESC, id DESC
LIMIT 1;

-- name: GetLatestProcessedRevisionByBranch :one
SELECT id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, processed_at, openapi_changed, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND branch = sqlc.arg(branch)
  AND status = 'processed'
ORDER BY processed_at DESC NULLS LAST, id DESC
LIMIT 1;

-- name: GetLatestProcessedOpenAPIRevisionByBranchExcludingID :one
SELECT id, repo_id, sha, branch, parent_sha, event_type, delivery_id, payload_json, received_at, attempt_count, next_retry_at, processed_at, openapi_changed, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND branch = sqlc.arg(branch)
  AND status = 'processed'
  AND openapi_changed = TRUE
  AND id <> sqlc.arg(exclude_revision_id)
ORDER BY processed_at DESC NULLS LAST, id DESC
LIMIT 1;

-- name: GetLatestProcessedOpenAPIRevisionStateByBranchExcludingID :one
SELECT id, repo_id, sha, branch, parent_sha, processed_at, openapi_changed, status, error
FROM ingest_events
WHERE repo_id = sqlc.arg(repo_id)
  AND branch = sqlc.arg(branch)
  AND status = 'processed'
  AND openapi_changed = TRUE
  AND id <> sqlc.arg(exclude_revision_id)
ORDER BY processed_at DESC NULLS LAST, id DESC
LIMIT 1;
